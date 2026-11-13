// SPDX-License-Identifier: MPL-2.0

package localdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"

	"lefalock/internal/domain"
)

// ApplyRestore commits all restored records together.
func (s *Store) ApplyRestore(ctx context.Context, expected []domain.Service, operations []domain.RestoreOperation) error {
	return s.ApplyRestoreWithDirectories(ctx, expected, nil, nil, operations)
}

func (s *Store) ApplyRestoreWithDirectories(ctx context.Context, expected []domain.Service, expectedDirectories, directories []domain.Directory, operations []domain.RestoreOperation, expectedNotes ...[]domain.Secret) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		current, err := servicesFrom(ctx, tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, expected) {
			return errors.New("vault changed after restore preview; open the backup again")
		}
		if len(expectedNotes) > 0 {
			notes, err := standaloneNotesFrom(ctx, tx)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(notes, expectedNotes[0]) {
				return errors.New("vault notes changed after restore preview; open the backup again")
			}
		}
		currentDirectories, err := directoriesFrom(ctx, tx)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(currentDirectories, expectedDirectories) {
			return errors.New("vault directories changed after restore preview; open the backup again")
		}
		for _, item := range directories {
			if _, err := tx.ExecContext(ctx, `INSERT INTO directories(id, parent_id, name_ciphertext, created_at, updated_at)
				VALUES (?, NULLIF(?, ''), ?, ?, ?)`, item.ID, item.ParentID, item.NameCiphertext,
				item.CreatedAt.Format(timestampLayout), item.UpdatedAt.Format(timestampLayout)); err != nil {
				return fmt.Errorf("restore directory: %w", err)
			}
		}
		for _, item := range directories {
			if err := validDirectoryParent(ctx, tx, item.ParentID, item.ID); err != nil {
				return err
			}
		}
		for _, operation := range operations {
			if operation.Service != nil {
				item := operation.Service
				if err := validDirectoryParent(ctx, tx, item.DirectoryID, ""); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO services(id, directory_id, name_ciphertext, created_at, updated_at)
					VALUES (?, NULLIF(?, ''), ?, ?, ?)`, item.ID, item.DirectoryID, item.NameCiphertext,
					item.CreatedAt.Format(timestampLayout), item.UpdatedAt.Format(timestampLayout)); err != nil {
					return fmt.Errorf("restore service: %w", err)
				}
			}
			if operation.Secret == nil {
				continue
			}
			item := operation.Secret
			if item.Type == domain.SecretNote && item.ServiceID != "" {
				copy := *item
				copy.DirectoryID = ""
				item = &copy
			}
			secretType, err := normalizedSecretType(item.Type)
			if err != nil {
				return err
			}
			if operation.Replace {
				var currentType domain.SecretType
				if err := tx.QueryRowContext(ctx, `SELECT secret_type FROM secrets WHERE id = ? AND COALESCE(service_id,'') = ? AND deleted_at IS NULL`, item.ID, item.ServiceID).Scan(&currentType); err != nil {
					return fmt.Errorf("find secret to replace: %w", err)
				}
				if currentType != secretType {
					return errors.New("cannot replace a secret with a different type")
				}
				if _, err := tx.ExecContext(ctx, `UPDATE secrets SET password_ciphertext = ?, updated_at = ? WHERE id = ?`, item.PasswordCiphertext, item.UpdatedAt.Format(timestampLayout), item.ID); err != nil {
					return fmt.Errorf("replace secret password: %w", err)
				}
				continue
			}
			if err := validDirectoryParent(ctx, tx, item.DirectoryID, ""); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO secrets(id, service_id, directory_id, secret_name_ciphertext, password_ciphertext, secret_type, created_at, updated_at)
				VALUES (?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?)`, item.ID, item.ServiceID, item.DirectoryID, item.SecretNameCiphertext, item.PasswordCiphertext, secretType,
				item.CreatedAt.Format(timestampLayout), item.UpdatedAt.Format(timestampLayout)); err != nil {
				return fmt.Errorf("restore secret: %w", err)
			}
		}
		// Recovery completion shares the import transaction, including empty backups.
		if _, err := tx.ExecContext(ctx, `UPDATE recovery_state SET imported = 1 WHERE singleton = 1`); err != nil {
			return err
		}
		return nil
	})
}
