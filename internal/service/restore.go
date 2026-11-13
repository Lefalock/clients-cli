// SPDX-License-Identifier: MPL-2.0

package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/tyler-smith/go-bip39"
	"lefalock/internal/domain"
)

const maxBackupSize = 64 << 20

type RestoreChoice string

const (
	RestoreKeepCurrent RestoreChoice = "keep-current"
	RestoreReplace     RestoreChoice = "replace"
	RestoreKeepBoth    RestoreChoice = "keep-both"
	RestoreNewService  RestoreChoice = "new-service"
)

type RestoreDecision struct {
	Choice         RestoreChoice
	NewName        string
	NewServiceName string
}

type RestoreConflict struct {
	Standalone       bool
	CurrentType      domain.SecretType
	BackupType       domain.SecretType
	Key              string
	ServiceIndex     int
	SecretIndex      int
	Service          string
	Secret           string
	CurrentServiceID string
	CurrentSecretID  string
	CurrentCreatedAt time.Time
	BackupCreatedAt  time.Time
}

type RestoreSummary struct {
	DirectoriesAdded int
	ServicesAdded    int
	SecretsAdded     int
	SecretsReplaced  int
	SecretsKept      int
	RenamedImports   []RestoreServiceRename
}

type RestoreServiceRename struct {
	From string
	To   string
}

type RestorePreview struct {
	Conflicts           []RestoreConflict
	Initial             RestoreSummary
	SourcePath          string
	document            backupDocument
	baseline            []domain.Service
	baselineDirectories []domain.Directory
	baselineNotes       []domain.Secret
}

func (b *Backup) PreviewRestore(ctx context.Context, path, mnemonic string) (*RestorePreview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	document, err := readBackup(ctx, path, mnemonic)
	if err != nil {
		return nil, err
	}
	current, directories, notes, err := b.repository.BackupSnapshotWithNotes(ctx)
	if err != nil {
		return nil, err
	}
	preview := &RestorePreview{SourcePath: path, document: document, baseline: current, baselineDirectories: directories, baselineNotes: notes}
	if err := b.inspectRestore(preview); err != nil {
		return nil, err
	}
	return preview, nil
}

func readBackup(ctx context.Context, path, mnemonic string) (backupDocument, error) {
	var document backupDocument
	key, err := bip39.EntropyFromMnemonic(strings.Join(strings.Fields(strings.TrimSpace(mnemonic)), " "))
	if err != nil || len(key) != 32 {
		return document, errors.New("invalid recovery phrase")
	}
	defer clearBytes(key)
	file, err := os.Open(path)
	if err != nil {
		return document, fmt.Errorf("open backup: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return document, fmt.Errorf("inspect backup: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxBackupSize || info.Size() < 5+12+4+16 {
		return document, errors.New("invalid backup file size or type")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBackupSize+1))
	if err != nil {
		return document, fmt.Errorf("read backup: %w", err)
	}
	if len(data) > maxBackupSize {
		return document, errors.New("backup is too large")
	}
	if len(data) < 17 || string(data[:4]) != backupMagic || data[4] != backupVersion {
		return document, errors.New("unsupported backup format")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return document, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return document, err
	}
	header := data[:5+gcm.NonceSize()]
	nonce := header[5:]
	plaintext := make([]byte, 0, len(data))
	defer func() { clearBytes(plaintext) }()
	for offset, counter := len(header), uint64(0); offset < len(data); counter++ {
		if err := ctx.Err(); err != nil {
			return document, err
		}
		if len(data)-offset < 4 {
			return document, errors.New("truncated backup chunk length")
		}
		length := int(binary.BigEndian.Uint32(data[offset:]))
		offset += 4
		if length < gcm.Overhead()+1 || length > backupChunkSize+gcm.Overhead() || length > len(data)-offset {
			return document, errors.New("invalid or truncated backup chunk")
		}
		chunkNonce := append([]byte(nil), nonce...)
		binary.BigEndian.PutUint64(chunkNonce[len(chunkNonce)-8:], binary.BigEndian.Uint64(nonce[len(nonce)-8:])+counter)
		chunk, err := gcm.Open(nil, chunkNonce, data[offset:offset+length], header)
		clearBytes(chunkNonce)
		if err != nil {
			return document, errors.New("backup authentication failed; check the file and recovery phrase")
		}
		plaintext = append(plaintext, chunk...)
		clearBytes(chunk)
		offset += length
	}
	if len(plaintext) == 0 {
		return document, errors.New("backup is empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return document, fmt.Errorf("decode backup: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return document, errors.New("backup has trailing data")
	}
	if document.Format != "lefalock-backup" || document.Version != 1 {
		return document, errors.New("unsupported backup document version")
	}
	for _, item := range document.Services {
		for _, secret := range item.Secrets {
			if !secret.Type.Valid() {
				return document, errors.New("backup contains an invalid secret type")
			}
		}
	}
	for _, note := range document.Notes {
		if note.Type != domain.SecretNote || strings.TrimSpace(note.Secret) == "" {
			return document, errors.New("invalid standalone note")
		}
	}
	if err := validateBackupDirectoryTree(document); err != nil {
		return document, err
	}
	for _, item := range document.Services {
		if item.Name == "" {
			return document, errors.New("backup contains an empty service name")
		}
		for _, account := range item.Secrets {
			if account.Secret == "" || (account.Password == "" && account.Type != domain.SecretNote) {
				return document, fmt.Errorf("backup contains an invalid secret in %q", item.Name)
			}
		}
	}
	return document, nil
}

type restoreMatch struct {
	service      domain.Service
	found        bool
	accounts     map[int]domain.Secret
	accountNames map[string]bool
}

// Match repeated names one-to-one in snapshot order. Extra backup entries are
// added separately, so an archive made from a vault with duplicates remains restorable.
func (b *Backup) matchRestore(preview *RestorePreview) ([]restoreMatch, error) {
	servicesByName := make(map[string][]domain.Service)
	for _, item := range preview.baseline {
		name, err := decryptField(b.crypto, b.keyRef, "service", item.ID, "name", item.NameCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt current service: %w", err)
		}
		servicesByName[name] = append(servicesByName[name], item)
	}
	servicePosition := make(map[string]int)
	matches := make([]restoreMatch, len(preview.document.Services))
	for serviceIndex, item := range preview.document.Services {
		position := servicePosition[item.Name]
		servicePosition[item.Name]++
		candidates := servicesByName[item.Name]
		if position >= len(candidates) {
			continue
		}
		local := candidates[position]
		match := restoreMatch{service: local, found: true, accounts: make(map[int]domain.Secret), accountNames: make(map[string]bool)}
		accountsByName := make(map[string][]domain.Secret)
		for _, account := range local.Secrets {
			name, err := decryptField(b.crypto, b.keyRef, "secret", account.ID, "name", account.SecretNameCiphertext)
			if err != nil {
				return nil, fmt.Errorf("decrypt current secret: %w", err)
			}
			match.accountNames[name] = true
			accountsByName[name] = append(accountsByName[name], account)
		}
		accountPosition := make(map[string]int)
		for accountIndex, account := range item.Secrets {
			position := accountPosition[account.Secret]
			accountPosition[account.Secret]++
			if candidates := accountsByName[account.Secret]; position < len(candidates) {
				match.accounts[accountIndex] = candidates[position]
			}
		}
		matches[serviceIndex] = match
	}
	return matches, nil
}

func restoreKey(serviceIndex, accountIndex int) string {
	return fmt.Sprintf("%d:%d", serviceIndex, accountIndex)
}

func (b *Backup) inspectRestore(preview *RestorePreview) error {
	preview.Initial.DirectoriesAdded = len(preview.document.Directories)
	matches, err := b.matchRestore(preview)
	if err != nil {
		return err
	}
	for serviceIndex, item := range preview.document.Services {
		match := matches[serviceIndex]
		if !match.found {
			preview.Initial.ServicesAdded++
		}
		for accountIndex, account := range item.Secrets {
			if local, collision := match.accounts[accountIndex]; collision {
				preview.Conflicts = append(preview.Conflicts, RestoreConflict{Key: restoreKey(serviceIndex, accountIndex), ServiceIndex: serviceIndex, SecretIndex: accountIndex,
					CurrentType: local.Type, BackupType: account.Type, Service: item.Name, Secret: account.Secret, CurrentServiceID: match.service.ID, CurrentSecretID: local.ID,
					CurrentCreatedAt: local.CreatedAt, BackupCreatedAt: account.Created})
				continue
			}
			preview.Initial.SecretsAdded++
		}
	}
	matchesNotes, err := b.matchStandaloneNotes(preview)
	if err != nil {
		return err
	}
	for i, note := range preview.document.Notes {
		if local, found := matchesNotes[i]; found {
			preview.Conflicts = append(preview.Conflicts, RestoreConflict{Standalone: true, Key: noteRestoreKey(i), ServiceIndex: len(preview.document.Services) + i, SecretIndex: i, Service: restoreNoteLocation(preview.document, note.DirectoryID), Secret: note.Secret, CurrentSecretID: local.ID, CurrentType: domain.SecretNote, BackupType: domain.SecretNote, CurrentCreatedAt: local.CreatedAt, BackupCreatedAt: note.Created})
		} else {
			preview.Initial.SecretsAdded++
		}
	}
	return nil
}

// RestoreSummaryFor validates every decision and computes the final preview.
func (b *Backup) RestoreSummaryFor(preview *RestorePreview, decisions map[string]RestoreDecision) (RestoreSummary, error) {
	if preview == nil {
		return RestoreSummary{}, errors.New("restore preview is required")
	}
	result := RestoreSummary{DirectoriesAdded: len(preview.document.Directories)}
	matches, err := b.matchRestore(preview)
	if err != nil {
		return result, err
	}
	renamed, err := b.restoreServiceRenames(preview, decisions)
	if err != nil {
		return result, err
	}
	used := make([]map[string]bool, len(preview.document.Services))
	for serviceIndex, item := range preview.document.Services {
		names := make(map[string]bool)
		for name := range matches[serviceIndex].accountNames {
			names[name] = true
		}
		for _, account := range item.Secrets {
			names[account.Secret] = true
		}
		used[serviceIndex] = names
	}
	for serviceIndex, item := range preview.document.Services {
		if renamed[serviceIndex] != "" {
			result.ServicesAdded++
			result.SecretsAdded += len(item.Secrets)
			result.RenamedImports = append(result.RenamedImports, RestoreServiceRename{From: item.Name, To: renamed[serviceIndex]})
			continue
		}
		match := matches[serviceIndex]
		if !match.found {
			result.ServicesAdded++
		}
		for accountIndex, account := range item.Secrets {
			if !match.found {
				result.SecretsAdded++
				continue
			}
			if _, collision := match.accounts[accountIndex]; !collision {
				result.SecretsAdded++
				continue
			}
			decision := decisions[restoreKey(serviceIndex, accountIndex)]
			switch decision.Choice {
			case RestoreKeepCurrent:
				result.SecretsKept++
			case RestoreReplace:
				if match.accounts[accountIndex].Type != account.Type {
					return result, fmt.Errorf("cannot replace %s / %s with a different secret type; choose Keep current or Keep both", item.Name, account.Secret)
				}
				result.SecretsReplaced++
			case RestoreKeepBoth:
				name := strings.TrimSpace(decision.NewName)
				if name == "" || used[serviceIndex][name] {
					return result, fmt.Errorf("choose a unique new name for %s / %s", item.Name, account.Secret)
				}
				used[serviceIndex][name] = true
				result.SecretsAdded++
			default:
				return result, fmt.Errorf("invalid restore decision for %s / %s", item.Name, account.Secret)
			}
		}
	}
	if err := b.summarizeStandaloneNotes(preview, decisions, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (b *Backup) restoreServiceRenames(preview *RestorePreview, decisions map[string]RestoreDecision) (map[int]string, error) {
	renamed := make(map[int]string)
	if len(decisions) != len(preview.Conflicts) {
		return nil, errors.New("restore decisions do not match conflicts")
	}
	for _, conflict := range preview.Conflicts {
		decision, ok := decisions[conflict.Key]
		if !ok {
			return nil, fmt.Errorf("choose how to resolve %s / %s", conflict.Service, conflict.Secret)
		}
		if conflict.Standalone && decision.Choice == RestoreNewService {
			return nil, errors.New("folder notes: choose Keep current, Use backup or Keep both: rename secret")
		}
		if decision.Choice != RestoreNewService {
			continue
		}
		name := strings.TrimSpace(decision.NewServiceName)
		if name == "" {
			return nil, fmt.Errorf("choose a new service name for %s", conflict.Service)
		}
		if previous := renamed[conflict.ServiceIndex]; previous != "" && previous != name {
			return nil, fmt.Errorf("use one new service name for all secrets in %s", conflict.Service)
		}
		renamed[conflict.ServiceIndex] = name
	}
	for _, conflict := range preview.Conflicts {
		if name := renamed[conflict.ServiceIndex]; name != "" {
			decision := decisions[conflict.Key]
			if decision.Choice != RestoreNewService || strings.TrimSpace(decision.NewServiceName) != name {
				return nil, fmt.Errorf("all secrets in %s must use the same new service", conflict.Service)
			}
		}
	}
	used := make(map[string]bool)
	for _, item := range preview.baseline {
		name, err := decryptField(b.crypto, b.keyRef, "service", item.ID, "name", item.NameCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt current service: %w", err)
		}
		used[name] = true
	}
	for _, item := range preview.document.Services {
		used[item.Name] = true
	}
	for serviceIndex, item := range preview.document.Services {
		if name := renamed[serviceIndex]; name != "" {
			if used[name] {
				return nil, fmt.Errorf("choose a unique new service name for %s", item.Name)
			}
			used[name] = true
		}
	}
	return renamed, nil
}

// SuggestRestoreName provides a unique name for the "keep both" action.
func (b *Backup) SuggestRestoreName(preview *RestorePreview, conflict RestoreConflict, decisions map[string]RestoreDecision) string {
	used := make(map[string]bool)
	if conflict.Standalone {
		for _, note := range preview.baselineNotes {
			name, err := decryptField(b.crypto, b.keyRef, "secret", note.ID, "name", note.SecretNameCiphertext)
			if err == nil {
				used[name] = true
			}
		}
		for _, note := range preview.document.Notes {
			used[note.Secret] = true
		}
	}
	matches, err := b.matchRestore(preview)
	if err == nil && conflict.ServiceIndex < len(matches) {
		for name := range matches[conflict.ServiceIndex].accountNames {
			used[name] = true
		}
	}
	if conflict.ServiceIndex < len(preview.document.Services) {
		for _, account := range preview.document.Services[conflict.ServiceIndex].Secrets {
			used[account.Secret] = true
		}
	}
	for _, decision := range decisions {
		if decision.Choice == RestoreKeepBoth {
			used[decision.NewName] = true
		}
	}
	base := conflict.Secret + " (restored)"
	if !used[base] {
		return base
	}
	for index := 2; ; index++ {
		name := fmt.Sprintf("%s %d", base, index)
		if !used[name] {
			return name
		}
	}
}

// SuggestRestoreServiceName finds an unused name for a separate imported service.
func (b *Backup) SuggestRestoreServiceName(preview *RestorePreview, conflict RestoreConflict, decisions map[string]RestoreDecision) string {
	used := make(map[string]bool)
	for _, item := range preview.baseline {
		name, err := decryptField(b.crypto, b.keyRef, "service", item.ID, "name", item.NameCiphertext)
		if err == nil {
			used[name] = true
		}
	}
	for _, item := range preview.document.Services {
		used[item.Name] = true
	}
	for _, decision := range decisions {
		if decision.Choice == RestoreNewService {
			used[decision.NewServiceName] = true
		}
	}
	base := conflict.Service + " (restored)"
	if !used[base] {
		return base
	}
	for index := 2; ; index++ {
		name := fmt.Sprintf("%s %d", base, index)
		if !used[name] {
			return name
		}
	}
}

func (b *Backup) ApplyRestore(ctx context.Context, preview *RestorePreview, decisions map[string]RestoreDecision) (RestoreSummary, error) {
	summary, err := b.RestoreSummaryFor(preview, decisions)
	if err != nil {
		return summary, err
	}
	renamed, err := b.restoreServiceRenames(preview, decisions)
	if err != nil {
		return summary, err
	}
	unlock, err := b.repository.AcquireOperationLock(ctx)
	if err != nil {
		return summary, err
	}
	defer unlock()
	current, currentDirectories, currentNotes, err := b.repository.BackupSnapshotWithNotes(ctx)
	if err != nil {
		return summary, err
	}
	if !reflect.DeepEqual(current, preview.baseline) || !reflect.DeepEqual(currentDirectories, preview.baselineDirectories) || !reflect.DeepEqual(currentNotes, preview.baselineNotes) {
		return summary, errors.New("vault changed after restore preview; open the backup again")
	}
	matches, err := b.matchRestore(preview)
	if err != nil {
		return summary, err
	}
	directoryMap := make(map[string]string)
	for _, item := range preview.document.Directories {
		id, err := domain.NewID()
		if err != nil {
			return summary, err
		}
		directoryMap[item.ID] = id
	}
	importedDirectories := make([]domain.Directory, 0, len(preview.document.Directories))
	for _, item := range preview.document.Directories {
		ciphertext, err := encryptField(b.crypto, b.keyRef, "directory", directoryMap[item.ID], "name", item.Name)
		if err != nil {
			return summary, err
		}
		now := time.Now().UTC()
		importedDirectories = append(importedDirectories, domain.Directory{ID: directoryMap[item.ID], ParentID: directoryMap[item.ParentID], NameCiphertext: ciphertext, CreatedAt: now, UpdatedAt: now})
	}
	operations := make([]domain.RestoreOperation, 0)
	for serviceIndex, item := range preview.document.Services {
		match := matches[serviceIndex]
		found := match.found
		serviceName := item.Name
		if newName := renamed[serviceIndex]; newName != "" {
			serviceName, found = newName, false
		}
		serviceID := match.service.ID
		if !found {
			serviceID, err = domain.NewID()
			if err != nil {
				return summary, err
			}
			ciphertext, err := encryptField(b.crypto, b.keyRef, "service", serviceID, "name", serviceName)
			if err != nil {
				return summary, err
			}
			created := item.Created
			if created.IsZero() {
				created = time.Now().UTC()
			}
			operations = append(operations, domain.RestoreOperation{Service: &domain.Service{ID: serviceID, DirectoryID: directoryMap[item.DirectoryID], NameCiphertext: ciphertext, CreatedAt: created, UpdatedAt: time.Now().UTC()}})
		}
		for accountIndex, account := range item.Secrets {
			name := account.Secret
			local, collision := match.accounts[accountIndex]
			choice := RestoreDecision{}
			if found && collision {
				choice = decisions[restoreKey(serviceIndex, accountIndex)]
				if choice.Choice == RestoreKeepCurrent {
					continue
				}
				if choice.Choice == RestoreKeepBoth {
					name = strings.TrimSpace(choice.NewName)
				}
			}
			id := local.ID
			if !(found && collision && choice.Choice == RestoreReplace) {
				id, err = domain.NewID()
				if err != nil {
					return summary, err
				}
			}
			password, err := encryptField(b.crypto, b.keyRef, "secret", id, secretValueField(account.Type), account.Password)
			if err != nil {
				return summary, err
			}
			if found && collision && choice.Choice == RestoreReplace {
				localType := local.Type
				if localType == "" {
					localType = domain.SecretPassword
				}
				if localType != account.Type {
					return summary, fmt.Errorf("cannot replace %s / %s with a different secret type; choose Keep both", item.Name, account.Secret)
				}
				operations = append(operations, domain.RestoreOperation{Secret: &domain.Secret{ID: local.ID, ServiceID: serviceID, Type: account.Type, PasswordCiphertext: password, UpdatedAt: time.Now().UTC()}, Replace: true})
				continue
			}
			accountCiphertext, err := encryptField(b.crypto, b.keyRef, "secret", id, "name", name)
			if err != nil {
				return summary, err
			}
			created := account.Created
			if created.IsZero() {
				created = time.Now().UTC()
			}
			operations = append(operations, domain.RestoreOperation{Secret: &domain.Secret{ID: id, ServiceID: serviceID, DirectoryID: directoryMap[account.DirectoryID], Type: account.Type, SecretNameCiphertext: accountCiphertext, PasswordCiphertext: password, CreatedAt: created, UpdatedAt: time.Now().UTC()}})
		}
	}
	noteOperations, err := b.restoreStandaloneNotes(preview, decisions, directoryMap)
	if err != nil {
		return summary, err
	}
	operations = append(operations, noteOperations...)
	if err := b.repository.ApplyRestoreWithDirectories(ctx, preview.baseline, preview.baselineDirectories, importedDirectories, operations, preview.baselineNotes); err != nil {
		return summary, err
	}
	return summary, nil
}
