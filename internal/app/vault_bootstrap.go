// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"lefalock/internal/localdb"
	"lefalock/internal/secure"
	"lefalock/internal/vault"
)

// openActiveVault checks the database's authoritative key before the discovery
// key, so a committed rotation is recoverable even after the old key is gone.
func openActiveVault(ctx context.Context, dataDir string, keys *secure.KeyStore) (vault.Info, error) {
	info, err := vault.Discover(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return vault.Open(dataDir, keys)
	}
	if err != nil {
		return vault.Info{}, err
	}
	database, err := localdb.OpenExisting(vault.DatabasePath(dataDir, info))
	if err == nil {
		id, ref, readErr := database.Vault(ctx)
		closeErr := database.Close()
		if readErr != nil && !errors.Is(readErr, localdb.ErrNotFound) {
			return info, readErr
		}
		if closeErr != nil {
			return info, closeErr
		}
		if readErr == nil {
			if id != info.ID {
				return info, fmt.Errorf("database vault id does not match discovery: %w", vault.ErrMetadataCorrupt)
			}
			// Validate the database reference using the same rules as discovery.
			if err := vault.Validate(vault.Info{ID: id, KeyRef: ref}); err != nil {
				return info, err
			}
			if _, err := keys.LoadReference(ref); err != nil {
				return info, fmt.Errorf("load database vault key: %w", err)
			}
			if ref != info.KeyRef {
				if err := vault.SetKeyRef(dataDir, info.ID, ref); err != nil {
					return info, err
				}
				info.KeyRef = ref
			}
			return info, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return info, err
	}
	if _, err := keys.LoadReference(info.KeyRef); err != nil {
		return info, fmt.Errorf("load active vault key: %w", err)
	}
	return info, nil
}

func needsVaultRecovery(err error) bool {
	return errors.Is(err, vault.ErrMetadataCorrupt) || errors.Is(err, vault.ErrMetadataMissing) ||
		errors.Is(err, secure.ErrKeyNotFound) || errors.Is(err, secure.ErrPartialKey) || errors.Is(err, secure.ErrCorruptKey)
}
