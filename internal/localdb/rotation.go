// SPDX-License-Identifier: MPL-2.0

package localdb

import (
	"context"
	"errors"
	"fmt"
	"lefalock/internal/ciphertext"
)

type cipherTable struct {
	name    string
	columns []string
}

var rotationTables = []cipherTable{
	{"directories", []string{"name_ciphertext"}},
	{"services", []string{"name_ciphertext"}},
	{"secrets", []string{"secret_name_ciphertext", "password_ciphertext"}},
}

// RotateKey keeps every ciphertext update and the active key reference in one
// transaction. The caller must have durably stored newRef before calling it.
func (s *Store) RotateKey(ctx context.Context, oldRef, newRef string, convert func(string) (string, error)) error {
	if oldRef == "" || newRef == "" || oldRef == newRef || convert == nil {
		return errors.New("invalid key rotation")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var activeRef, previousRef, preparedRef string
	if err := tx.QueryRowContext(ctx, `SELECT key_ref, previous_key_ref, prepared_key_ref FROM vault_metadata WHERE singleton = 1`).Scan(&activeRef, &previousRef, &preparedRef); err != nil {
		return fmt.Errorf("read rotation state: %w", err)
	}
	if activeRef != oldRef || previousRef != "" || preparedRef != newRef {
		return errors.New("vault key changed or previous rotation needs cleanup")
	}
	var vaultID string
	if err := tx.QueryRowContext(ctx, `SELECT vault_id FROM vault_metadata WHERE singleton=1`).Scan(&vaultID); err != nil {
		return err
	}
	for _, table := range rotationTables {
		err := transformFields(ctx, tx, table, vaultID, func(field ciphertext.Field, value string) (string, error) {
			if err := ciphertext.Check(value, field); err != nil {
				return "", err
			}
			return convert(value)
		})
		if err != nil {
			return fmt.Errorf("rotate %s: %w", table.name, err)
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE vault_metadata SET key_ref = ?, previous_key_ref = ?, prepared_key_ref = '' WHERE singleton = 1 AND key_ref = ? AND prepared_key_ref = ?`, newRef, oldRef, oldRef, newRef); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit key rotation: %w", err)
	}
	return nil
}

func joinColumns(values []string, separator string) string {
	result := ""
	for i, value := range values {
		if i > 0 {
			result += separator
		}
		result += value
	}
	return result
}
