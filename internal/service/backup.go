// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tyler-smith/go-bip39"
	"lefalock/internal/domain"
)

const (
	backupMagic     = "DSBK"
	backupVersion   = byte(1)
	backupChunkSize = 64 * 1024
	backupExtension = ".lbcp"
	mnemonicSuffix  = "-key.txt"
)

type backupRepository interface {
	BackupSnapshotWithNotes(context.Context) ([]domain.Service, []domain.Directory, []domain.Secret, error)
	AcquireOperationLock(context.Context) (func(), error)
	ApplyRestoreWithDirectories(context.Context, []domain.Service, []domain.Directory, []domain.Directory, []domain.RestoreOperation, ...[]domain.Secret) error
}

type backupCrypto interface {
	DecryptReference(string, string) (string, error)
	EncryptReference(string, string) (string, error)
}

type Backup struct {
	repository backupRepository
	crypto     backupCrypto
	keyRef     string
	defaultDir string
}

type BackupResult struct {
	Path         string
	Mnemonic     string
	MnemonicName string
}

type backupDocument struct {
	Notes       []backupSecret    `json:"notes,omitempty"`
	Format      string            `json:"format"`
	Version     int               `json:"version"`
	CreatedAt   time.Time         `json:"created_at"`
	Services    []backupService   `json:"services"`
	Directories []backupDirectory `json:"directories,omitempty"`
}

type backupDirectory struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id,omitempty"`
	Name     string `json:"name"`
}

type backupService struct {
	Name        string         `json:"name"`
	DirectoryID string         `json:"directory_id,omitempty"`
	Created     time.Time      `json:"created_at"`
	Updated     time.Time      `json:"updated_at"`
	Secrets     []backupSecret `json:"secrets,omitempty"`
}

type backupSecret struct {
	Secret      string            `json:"name"`
	Type        domain.SecretType `json:"type,omitempty"`
	DirectoryID string            `json:"directory_id,omitempty"`
	Password    string            `json:"password"`
	Created     time.Time         `json:"created_at"`
}

func NewLocalBackup(repository backupRepository, crypto backupCrypto, keyRef, defaultDir string) (*Backup, error) {
	if repository == nil || crypto == nil || keyRef == "" || defaultDir == "" {
		return nil, errors.New("local backup dependencies are required")
	}
	return &Backup{repository: repository, crypto: crypto, keyRef: keyRef, defaultDir: defaultDir}, nil
}

func (b *Backup) DefaultDirectory() (string, error) {
	if err := os.MkdirAll(b.defaultDir, 0o700); err != nil {
		return "", fmt.Errorf("create default backup directory: %w", err)
	}
	if err := os.Chmod(b.defaultDir, 0o700); err != nil {
		return "", fmt.Errorf("protect default backup directory: %w", err)
	}
	return b.defaultDir, nil
}

func (b *Backup) Create(ctx context.Context, directory string) (BackupResult, error) {
	if err := ctx.Err(); err != nil {
		return BackupResult{}, err
	}
	if err := writableDirectory(directory); err != nil {
		return BackupResult{}, err
	}
	document, err := b.snapshot(ctx)
	if err != nil {
		return BackupResult{}, err
	}
	plaintext, err := json.Marshal(document)
	if err != nil {
		return BackupResult{}, fmt.Errorf("encode backup: %w", err)
	}
	defer clearBytes(plaintext)
	// Header plus one length and GCM tag for every encrypted chunk.
	chunks := (len(plaintext) + backupChunkSize - 1) / backupChunkSize
	if 17+len(plaintext)+chunks*20 > maxBackupSize {
		return BackupResult{}, errors.New("backup is too large (maximum 64 MiB)")
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return BackupResult{}, fmt.Errorf("generate backup key: %w", err)
	}
	defer clearBytes(key)
	mnemonic, err := bip39.NewMnemonic(key)
	if err != nil {
		return BackupResult{}, fmt.Errorf("generate recovery mnemonic: %w", err)
	}
	name := "lefalock-backup-" + time.Now().UTC().Format("20060102T150405Z") + backupExtension
	path := filepath.Join(directory, name)
	if err := writeEncrypted(ctx, path, key, plaintext); err != nil {
		return BackupResult{}, err
	}
	return BackupResult{Path: path, Mnemonic: mnemonic, MnemonicName: strings.TrimSuffix(name, backupExtension) + mnemonicSuffix}, nil
}

func (b *Backup) SaveMnemonic(ctx context.Context, directory, name, mnemonic string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := writableDirectory(directory); err != nil {
		return "", err
	}
	if mnemonic == "" || filepath.Base(name) != name || !strings.HasSuffix(name, mnemonicSuffix) {
		return "", errors.New("invalid recovery mnemonic file name")
	}
	path := filepath.Join(directory, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("recovery mnemonic file already exists: %s", path)
		}
		return "", fmt.Errorf("create recovery mnemonic file: %w", err)
	}
	success := false
	defer func() {
		if !success {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	content := []byte("Lefalock backup recovery mnemonic\n\n" + mnemonic + "\n")
	defer clearBytes(content)
	if _, err := file.Write(content); err != nil {
		return "", fmt.Errorf("write recovery mnemonic file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync recovery mnemonic file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close recovery mnemonic file: %w", err)
	}
	success = true
	return path, nil
}

func (b *Backup) snapshot(ctx context.Context) (backupDocument, error) {
	services, directories, notes, err := b.repository.BackupSnapshotWithNotes(ctx)
	if err != nil {
		return backupDocument{}, err
	}
	document := backupDocument{Format: "lefalock-backup", Version: 1, CreatedAt: time.Now().UTC(), Services: make([]backupService, 0, len(services))}
	for _, directory := range directories {
		name, err := decryptField(b.crypto, b.keyRef, "directory", directory.ID, "name", directory.NameCiphertext)
		if err != nil {
			return backupDocument{}, fmt.Errorf("decrypt backup directory: %w", err)
		}
		document.Directories = append(document.Directories, backupDirectory{ID: directory.ID, ParentID: directory.ParentID, Name: name})
	}
	for _, service := range services {
		if err := ctx.Err(); err != nil {
			return backupDocument{}, err
		}
		name, err := decryptField(b.crypto, b.keyRef, "service", service.ID, "name", service.NameCiphertext)
		if err != nil {
			return backupDocument{}, fmt.Errorf("decrypt backup service: %w", err)
		}
		item := backupService{Name: name, DirectoryID: service.DirectoryID, Created: service.CreatedAt, Updated: service.UpdatedAt, Secrets: make([]backupSecret, 0, len(service.Secrets))}
		for _, account := range service.Secrets {
			accountName, err := decryptField(b.crypto, b.keyRef, "secret", account.ID, "name", account.SecretNameCiphertext)
			if err != nil {
				return backupDocument{}, fmt.Errorf("decrypt backup secret name: %w", err)
			}
			password, err := decryptField(b.crypto, b.keyRef, "secret", account.ID, secretValueField(account.Type), account.PasswordCiphertext)
			if err != nil {
				return backupDocument{}, fmt.Errorf("decrypt backup password: %w", err)
			}
			secretType := account.Type
			if secretType == "" {
				secretType = domain.SecretPassword
			}
			item.Secrets = append(item.Secrets, backupSecret{Secret: accountName, Type: secretType, DirectoryID: account.DirectoryID, Password: password, Created: account.CreatedAt})
		}
		document.Services = append(document.Services, item)
	}
	for _, note := range notes {
		title, err := decryptField(b.crypto, b.keyRef, "secret", note.ID, "name", note.SecretNameCiphertext)
		if err != nil {
			return backupDocument{}, err
		}
		text, err := decryptField(b.crypto, b.keyRef, "secret", note.ID, secretValueField(note.Type), note.PasswordCiphertext)
		if err != nil {
			return backupDocument{}, err
		}
		document.Notes = append(document.Notes, backupSecret{Secret: title, Type: domain.SecretNote, DirectoryID: note.DirectoryID, Password: text, Created: note.CreatedAt})
	}
	return document, nil
}

func writableDirectory(directory string) error {
	if directory == "" {
		return errors.New("backup directory is required")
	}
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("access backup directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("backup destination is not a directory")
	}
	return nil
}

func writeEncrypted(ctx context.Context, path string, key, plaintext []byte) (resultErr error) {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("backup file already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect backup file: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary backup file: %w", err)
	}
	temporaryPath := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("protect temporary backup file: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			closeErr := file.Close()
			closed = true
			if resultErr == nil && closeErr != nil {
				resultErr = fmt.Errorf("close backup file: %w", closeErr)
			}
		}
		if resultErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("create backup cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("create backup GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generate backup nonce: %w", err)
	}
	header := append([]byte(backupMagic), backupVersion)
	header = append(header, nonce...)
	if _, err := file.Write(header); err != nil {
		return fmt.Errorf("write backup header: %w", err)
	}
	for offset, counter := 0, uint64(0); offset < len(plaintext); counter++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := offset + backupChunkSize
		if end > len(plaintext) {
			end = len(plaintext)
		}
		chunkNonce := append([]byte(nil), nonce...)
		binary.BigEndian.PutUint64(chunkNonce[len(chunkNonce)-8:], binary.BigEndian.Uint64(nonce[len(nonce)-8:])+counter)
		ciphertext := gcm.Seal(nil, chunkNonce, plaintext[offset:end], header)
		clearBytes(chunkNonce)
		if err := binary.Write(file, binary.BigEndian, uint32(len(ciphertext))); err != nil {
			clearBytes(ciphertext)
			return fmt.Errorf("write backup chunk size: %w", err)
		}
		if _, err := file.Write(ciphertext); err != nil {
			clearBytes(ciphertext)
			return fmt.Errorf("write backup chunk: %w", err)
		}
		clearBytes(ciphertext)
		offset = end
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync backup file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close backup file: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish backup file: %w", err)
	}
	return nil
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
