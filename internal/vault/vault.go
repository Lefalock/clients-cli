// SPDX-License-Identifier: MPL-2.0

package vault

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lefalock/internal/domain"
	"lefalock/internal/durable"
	"lefalock/internal/filelock"
	"lefalock/internal/secure"
)

var ErrMetadataCorrupt = errors.New("active vault metadata is corrupt")
var ErrMetadataMissing = errors.New("active vault metadata is missing but existing vault files remain")

// Discover reads the active pointer without loading keys or changing files.
func Discover(dataDir string) (Info, error) {
	return read(filepath.Join(dataDir, "active-vault.json"))
}

// HasStoredVaults distinguishes a lost pointer from a new installation.
func HasStoredVaults(dataDir string) (bool, error) {
	entries, err := os.ReadDir(filepath.Join(dataDir, "vaults"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

func validInfo(info Info) bool {
	if len(info.ID) != 36 {
		return false
	}
	for _, index := range []int{8, 13, 18, 23} {
		if info.ID[index] != '-' {
			return false
		}
	}
	id, err := hex.DecodeString(strings.ReplaceAll(info.ID, "-", ""))
	if err != nil || len(id) != 16 {
		return false
	}
	ref, err := hex.DecodeString(info.KeyRef)
	return err == nil && len(ref) == 32
}

// Validate checks metadata before using an ID in a filesystem path.
func Validate(info Info) error {
	if !validInfo(info) {
		return ErrMetadataCorrupt
	}
	return nil
}

type keyStore interface {
	EnsureReference(string) ([]byte, error)
	LoadReference(string) ([]byte, error)
}

type Info struct {
	ID     string `json:"vault_id"`
	KeyRef string `json:"key_ref"`
}

func Open(dataDir string, keys keyStore) (Info, error) {
	if dataDir == "" || keys == nil {
		return Info{}, errors.New("vault directory and key store are required")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return Info{}, fmt.Errorf("create vault directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return Info{}, fmt.Errorf("protect vault directory: %w", err)
	}
	unlock, err := filelock.Acquire(filepath.Join(dataDir, ".vault.lock"), 10*time.Second)
	if err != nil {
		return Info{}, fmt.Errorf("lock active vault: %w", err)
	}
	defer unlock()
	path := filepath.Join(dataDir, "active-vault.json")
	info, err := read(path)
	if err == nil {
		if _, err := keys.LoadReference(info.KeyRef); err != nil {
			return Info{}, fmt.Errorf("load active vault key: %w", err)
		}
		return info, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Info{}, err
	}

	if existing, err := HasStoredVaults(dataDir); err != nil {
		return Info{}, err
	} else if existing {
		return Info{}, ErrMetadataMissing
	}

	id, err := domain.NewID()
	if err != nil {
		return Info{}, err
	}
	info = Info{ID: id}
	info.KeyRef = secure.KeyReference(info.ID)
	if _, err := keys.EnsureReference(info.KeyRef); err != nil {
		return Info{}, fmt.Errorf("create local vault key: %w", err)
	}
	if err := write(path, info); err != nil {
		return Info{}, err
	}
	return info, nil
}

func DatabasePath(dataDir string, info Info) string {
	return filepath.Join(dataDir, "vaults", info.ID, "vault.db")
}

// SetKeyRef updates the discovery file after the database transaction has
// selected a new key. The database remains authoritative after a crash.
func SetKeyRef(dataDir, vaultID, keyRef string) error {
	if dataDir == "" || vaultID == "" || keyRef == "" {
		return errors.New("vault id and key reference are required")
	}
	unlock, err := filelock.Acquire(filepath.Join(dataDir, ".vault.lock"), 10*time.Second)
	if err != nil {
		return fmt.Errorf("lock active vault: %w", err)
	}
	defer unlock()
	path := filepath.Join(dataDir, "active-vault.json")
	info, err := read(path)
	if err != nil {
		return err
	}
	if info.ID != vaultID {
		return errors.New("active vault changed during key rotation")
	}
	if info.KeyRef == keyRef {
		return nil
	}
	info.KeyRef = keyRef
	return write(path, info)
}

func read(path string) (Info, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := json.Unmarshal(raw, &info); err != nil || !validInfo(info) {
		return Info{}, ErrMetadataCorrupt
	}
	return info, nil
}

func write(path string, info Info) error {
	raw, err := json.Marshal(info)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".active-vault-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
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
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("save active vault: %w", err)
	}
	return durable.SyncDirectory(filepath.Dir(path))
}
