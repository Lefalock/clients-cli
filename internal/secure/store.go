// SPDX-License-Identifier: MPL-2.0

package secure

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
	"lefalock/internal/durable"
	"lefalock/internal/filelock"
)

var (
	ErrKeyNotFound = errors.New("encryption key not found")
	ErrPartialKey  = errors.New("encryption key is incomplete")
	ErrCorruptKey  = errors.New("encryption key is corrupt")
	ErrKeyExists   = errors.New("encryption key already exists")
	ErrKeyChanged  = errors.New("encryption key changed after creation")
)

type KeyStore struct {
	dataDir        string
	keyringService string
}

func NewKeyStore(dataDir, keyringService string) (*KeyStore, error) {
	if dataDir == "" || keyringService == "" {
		return nil, errors.New("secure data directory and keyring service are required")
	}
	return &KeyStore{dataDir: dataDir, keyringService: keyringService}, nil
}

func (s *KeyStore) EnsureReference(reference string) ([]byte, error) {
	if err := validateReference(reference); err != nil {
		return nil, err
	}
	unlock, err := s.lock(reference)
	if err != nil {
		return nil, err
	}
	defer unlock()

	key, err := s.loadReference(reference)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, ErrKeyNotFound) {
		return nil, err
	}

	filePart, err := randomKey()
	if err != nil {
		return nil, err
	}
	osPart, err := randomKey()
	if err != nil {
		return nil, err
	}
	if err := s.saveParts(reference, filePart, osPart); err != nil {
		return nil, err
	}
	key, err = s.loadReference(reference)
	if err != nil {
		return nil, fmt.Errorf("verify saved encryption key: %w", err)
	}
	return key, nil
}

// CreateFreshReference refuses every existing share. Rotation must never adopt
// a key that appeared at its newly generated reference before creation.
func (s *KeyStore) CreateFreshReference(reference string) ([]byte, error) {
	if err := validateReference(reference); err != nil {
		return nil, err
	}
	unlock, err := s.lock(reference)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := os.Lstat(filepath.Join(s.dataDir, ".key-"+reference)); err == nil {
		return nil, ErrKeyExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect new encryption key file: %w", err)
	}
	if _, err := keyring.Get(s.keyringService, "key-"+reference); err == nil {
		return nil, ErrKeyExists
	} else if !errors.Is(err, keyring.ErrNotFound) {
		return nil, fmt.Errorf("inspect new encryption key in keyring: %w", err)
	}
	filePart, err := randomKey()
	if err != nil {
		return nil, err
	}
	defer clearPart(filePart)
	osPart, err := randomKey()
	if err != nil {
		return nil, err
	}
	defer clearPart(osPart)
	key := xor(filePart, osPart)
	if err := s.writeFreshFilePart(reference, filePart); err != nil {
		clearPart(key)
		return nil, fmt.Errorf("save new encryption key file: %w", err)
	}
	if err := keyring.Set(s.keyringService, "key-"+reference, hex.EncodeToString(osPart)); err != nil {
		clearPart(key)
		return nil, fmt.Errorf("save new encryption key in keyring: %w", err)
	}
	if err := s.verifyReference(reference, key); err != nil {
		clearPart(key)
		return nil, err
	}
	return key, nil
}

func (s *KeyStore) VerifyReference(reference string, expected []byte) error {
	if err := validateReference(reference); err != nil {
		return err
	}
	unlock, err := s.lock(reference)
	if err != nil {
		return err
	}
	defer unlock()
	return s.verifyReference(reference, expected)
}

func (s *KeyStore) verifyReference(reference string, expected []byte) error {
	actual, err := s.loadReference(reference)
	if err != nil {
		return fmt.Errorf("verify new encryption key: %w", err)
	}
	defer clearPart(actual)
	if len(expected) != 32 || subtle.ConstantTimeCompare(actual, expected) != 1 {
		return ErrKeyChanged
	}
	return nil
}

func (s *KeyStore) writeFreshFilePart(reference string, part []byte) error {
	path := filepath.Join(s.dataDir, ".key-"+reference)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return ErrKeyExists
	}
	if err != nil {
		return err
	}
	if _, err := file.WriteString(hex.EncodeToString(part)); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return durable.SyncDirectory(s.dataDir)
}

func clearPart(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func (s *KeyStore) LoadReference(reference string) ([]byte, error) {
	if err := validateReference(reference); err != nil {
		return nil, err
	}
	unlock, err := s.lock(reference)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.loadReference(reference)
}

// DeleteReference removes both shares. It is safe to retry after a partial
// deletion, which is necessary when finalizing a committed rotation.
func (s *KeyStore) DeleteReference(reference string) error {
	if err := validateReference(reference); err != nil {
		return err
	}
	unlock, err := s.lock(reference)
	if err != nil {
		return err
	}
	defer unlock()
	if err := keyring.Delete(s.keyringService, "key-"+reference); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete encryption key from keyring: %w", err)
	}
	removeErr := os.Remove(filepath.Join(s.dataDir, ".key-"+reference))
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return fmt.Errorf("delete encryption key file: %w", removeErr)
	}
	if removeErr == nil {
		if err := durable.SyncDirectory(s.dataDir); err != nil {
			return fmt.Errorf("sync deleted key file: %w", err)
		}
	}
	return nil
}

func (s *KeyStore) loadReference(reference string) ([]byte, error) {
	newPath := filepath.Join(s.dataDir, ".key-"+reference)
	fileHex, fileErr := os.ReadFile(newPath)
	osHex, osErr := keyring.Get(s.keyringService, "key-"+reference)

	fileMissing := errors.Is(fileErr, os.ErrNotExist)
	osMissing := errors.Is(osErr, keyring.ErrNotFound)
	// An unavailable backend is not evidence that a share has been lost.
	if fileErr != nil && !fileMissing {
		return nil, fmt.Errorf("read encryption key file: %w", fileErr)
	}
	if osErr != nil && !osMissing {
		return nil, fmt.Errorf("read encryption key from keyring: %w", osErr)
	}
	if fileMissing && osMissing {
		return nil, ErrKeyNotFound
	}
	if fileMissing || osMissing {
		return nil, ErrPartialKey
	}

	filePart, err := decodePart(fileHex)
	if err != nil {
		return nil, err
	}
	osPart, err := decodePart([]byte(osHex))
	if err != nil {
		return nil, err
	}
	return xor(filePart, osPart), nil
}

func (s *KeyStore) lock(reference string) (func(), error) {
	if err := os.MkdirAll(s.dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create secure data directory: %w", err)
	}
	if err := os.Chmod(s.dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("protect secure data directory: %w", err)
	}
	unlock, err := filelock.Acquire(filepath.Join(s.dataDir, ".key-lock-"+reference), 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("lock key store: %w", err)
	}
	return unlock, nil
}

func (s *KeyStore) saveParts(reference string, filePart, osPart []byte) error {
	if err := validateReference(reference); err != nil {
		return err
	}
	path := filepath.Join(s.dataDir, ".key-"+reference)
	fileHex := []byte(hex.EncodeToString(filePart))
	if err := s.writeFilePart(path, fileHex); err != nil {
		return fmt.Errorf("save encryption key file: %w", err)
	}
	if err := keyring.Set(s.keyringService, "key-"+reference, hex.EncodeToString(osPart)); err != nil {
		if rollbackErr := os.Remove(path); rollbackErr != nil && !errors.Is(rollbackErr, os.ErrNotExist) {
			return fmt.Errorf("save encryption key in keyring: %w; rollback key file: %v", err, rollbackErr)
		}
		return fmt.Errorf("save encryption key in keyring: %w", err)
	}
	return nil
}

func (s *KeyStore) writeFilePart(path string, data []byte) error {
	if err := os.MkdirAll(s.dataDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.dataDir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dataDir, ".key-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return durable.SyncDirectory(s.dataDir)
}

func decodePart(value []byte) ([]byte, error) {
	decoded, err := hex.DecodeString(string(value))
	if err != nil || len(decoded) != 32 {
		return nil, ErrCorruptKey
	}
	return decoded, nil
}

func randomKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate encryption key: %w", err)
	}
	return key, nil
}

func xor(left, right []byte) []byte {
	result := make([]byte, len(left))
	for i := range left {
		result[i] = left[i] ^ right[i]
	}
	return result
}

func KeyReference(user string) string {
	hash := sha256.Sum256([]byte(user))
	return hex.EncodeToString(hash[:])
}

func validateReference(reference string) error {
	decoded, err := hex.DecodeString(reference)
	if err != nil || len(decoded) != sha256.Size {
		return errors.New("invalid key reference")
	}
	return nil
}
