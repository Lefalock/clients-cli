// SPDX-License-Identifier: MPL-2.0

package localdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"lefalock/internal/domain"
)

func (s *Store) Services(ctx context.Context) ([]domain.Service, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	items, err := servicesFrom(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) BackupSnapshotWithNotes(ctx context.Context) ([]domain.Service, []domain.Directory, []domain.Secret, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("begin backup snapshot: %w", err)
	}
	defer tx.Rollback()
	services, err := servicesFrom(ctx, tx)
	if err != nil {
		return nil, nil, nil, err
	}
	directories, err := directoriesFrom(ctx, tx)
	if err != nil {
		return nil, nil, nil, err
	}
	notes, err := standaloneNotesFrom(ctx, tx)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, nil, fmt.Errorf("commit backup snapshot: %w", err)
	}
	return services, directories, notes, nil
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func normalizedSecretType(value domain.SecretType) (domain.SecretType, error) {
	if value == "" {
		return domain.SecretPassword, nil
	}
	if !value.Valid() {
		return "", errors.New("invalid secret type")
	}
	return value, nil
}

func servicesFrom(ctx context.Context, database queryer) ([]domain.Service, error) {
	rows, err := database.QueryContext(ctx, `
        SELECT id, COALESCE(directory_id, ''), name_ciphertext, created_at, updated_at, deleted_at
        FROM services WHERE deleted_at IS NULL ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list local services: %w", err)
	}
	var services []domain.Service
	for rows.Next() {
		var item domain.Service
		var created, updated string
		var deleted sql.NullString
		if err := rows.Scan(&item.ID, &item.DirectoryID, &item.NameCiphertext, &created, &updated, &deleted); err != nil {
			return nil, fmt.Errorf("scan local service: %w", err)
		}
		if item.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		if item.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, err
		}
		if item.DeletedAt, err = parseNullableTime(deleted); err != nil {
			return nil, err
		}
		services = append(services, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate local services: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close local service rows: %w", err)
	}
	index := make(map[string]int, len(services))
	for i := range services {
		index[services[i].ID] = i
	}
	secrets, err := secretsFrom(ctx, database, "service_id IN (SELECT id FROM services WHERE deleted_at IS NULL)")
	if err != nil {
		return nil, err
	}
	for _, secret := range secrets {
		i := index[secret.ServiceID]
		services[i].Secrets = append(services[i].Secrets, secret)
	}
	return services, nil
}

func secretsFrom(ctx context.Context, database queryer, where string, args ...any) ([]domain.Secret, error) {
	rows, err := database.QueryContext(ctx, `SELECT id, COALESCE(service_id,''), COALESCE(directory_id,''), secret_name_ciphertext, password_ciphertext, secret_type, created_at,updated_at,deleted_at FROM secrets WHERE deleted_at IS NULL AND `+where+` ORDER BY created_at,id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list local secrets: %w", err)
	}
	defer rows.Close()
	var secrets []domain.Secret
	for rows.Next() {
		var item domain.Secret
		var created, updated string
		var deleted sql.NullString
		if err := rows.Scan(&item.ID, &item.ServiceID, &item.DirectoryID, &item.SecretNameCiphertext, &item.PasswordCiphertext, &item.Type,
			&created, &updated, &deleted); err != nil {
			return nil, fmt.Errorf("scan local secret: %w", err)
		}
		if item.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		if item.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, err
		}
		if item.DeletedAt, err = parseNullableTime(deleted); err != nil {
			return nil, err
		}
		secrets = append(secrets, item)
	}
	return secrets, rows.Err()
}

func (s *Store) CreateService(ctx context.Context, service domain.Service, account domain.Secret) error {
	secretType, err := normalizedSecretType(account.Type)
	if err != nil {
		return err
	}
	if secretType == domain.SecretNote {
		account.DirectoryID = ""
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if err := validDirectoryParent(ctx, tx, service.DirectoryID, ""); err != nil {
			return err
		}
		if err := validDirectoryParent(ctx, tx, account.DirectoryID, ""); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO services(id, directory_id, name_ciphertext, created_at, updated_at)
			VALUES (?, NULLIF(?, ''), ?, ?, ?)`, service.ID, service.DirectoryID, service.NameCiphertext,
			service.CreatedAt.Format(timestampLayout), service.UpdatedAt.Format(timestampLayout)); err != nil {
			return fmt.Errorf("insert local service: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO secrets(id, service_id, directory_id, secret_name_ciphertext, password_ciphertext, secret_type, created_at, updated_at)
			VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?)`, account.ID, service.ID, account.DirectoryID, account.SecretNameCiphertext,
			account.PasswordCiphertext, secretType, account.CreatedAt.Format(timestampLayout), account.UpdatedAt.Format(timestampLayout)); err != nil {
			return fmt.Errorf("insert initial local secret: %w", err)
		}
		return nil
	})
}

func (s *Store) CreateSecret(ctx context.Context, account domain.Secret) error {
	secretType, err := normalizedSecretType(account.Type)
	if err != nil {
		return err
	}
	if secretType == domain.SecretNote {
		account.DirectoryID = ""
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if err := validDirectoryParent(ctx, tx, account.DirectoryID, ""); err != nil {
			return err
		}
		var deleted sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT deleted_at FROM services WHERE id = ?`, account.ServiceID).Scan(&deleted); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if deleted.Valid {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO secrets(id, service_id, directory_id, secret_name_ciphertext, password_ciphertext, secret_type, created_at, updated_at)
			VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?)`, account.ID, account.ServiceID, account.DirectoryID, account.SecretNameCiphertext,
			account.PasswordCiphertext, secretType, account.CreatedAt.Format(timestampLayout), account.UpdatedAt.Format(timestampLayout)); err != nil {
			return fmt.Errorf("insert local secret: %w", err)
		}
		return nil
	})
}

func (s *Store) RenameService(ctx context.Context, id, ciphertext string) error {
	if id == "" || ciphertext == "" {
		return errors.New("service id and encrypted name are required")
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var foundID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM services WHERE id = ? AND deleted_at IS NULL`, id).Scan(&foundID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE services SET name_ciphertext = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, ciphertext, nowString(), id)
		if err := affectedOne(result, err); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) RenameSecret(ctx context.Context, serviceID, accountID, ciphertext string) error {
	if serviceID == "" || accountID == "" || ciphertext == "" {
		return errors.New("service id, secret id and encrypted name are required")
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var foundID string
		var secretType domain.SecretType
		if err := tx.QueryRowContext(ctx, `SELECT id, secret_type FROM secrets WHERE id = ? AND service_id = ? AND deleted_at IS NULL`, accountID, serviceID).Scan(&foundID, &secretType); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE secrets SET secret_name_ciphertext = ?, updated_at = ? WHERE id = ? AND service_id = ? AND deleted_at IS NULL`, ciphertext, nowString(), accountID, serviceID)
		if err := affectedOne(result, err); err != nil {
			return err
		}
		return nil
	})
}

// SecretType reads authoritative metadata for an active secret and service.
func (s *Store) SecretType(ctx context.Context, serviceID, secretID string) (domain.SecretType, error) {
	var secretType domain.SecretType
	err := s.db.QueryRowContext(ctx, `SELECT a.secret_type FROM secrets a
		JOIN services s ON s.id = a.service_id
		WHERE a.id = ? AND a.service_id = ? AND a.deleted_at IS NULL AND s.deleted_at IS NULL`, secretID, serviceID).Scan(&secretType)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return secretType, err
}

func (s *Store) UpdateSecretValue(ctx context.Context, serviceID, secretID, ciphertext string) error {
	if serviceID == "" || secretID == "" || ciphertext == "" {
		return errors.New("service id, secret id and encrypted value are required")
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var foundID string
		var secretType domain.SecretType
		err := tx.QueryRowContext(ctx, `SELECT a.id, a.secret_type FROM secrets a
			JOIN services s ON s.id = a.service_id
			WHERE a.id = ? AND a.service_id = ? AND a.deleted_at IS NULL AND s.deleted_at IS NULL`, secretID, serviceID).Scan(&foundID, &secretType)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !secretType.Valid() {
			return errors.New("invalid secret type")
		}
		result, err := tx.ExecContext(ctx, `UPDATE secrets SET password_ciphertext = ?, updated_at = ?
			WHERE id = ? AND service_id = ? AND deleted_at IS NULL`, ciphertext, nowString(), secretID, serviceID)
		if err := affectedOne(result, err); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) DeleteService(ctx context.Context, id string) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		now := nowString()
		var foundID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM services WHERE id = ? AND deleted_at IS NULL`, id).Scan(&foundID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE services SET deleted_at = ?, updated_at = ? WHERE id = ?`, now, now, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE secrets SET deleted_at = ?, updated_at = ? WHERE service_id = ? AND deleted_at IS NULL`, now, now, id); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) DeleteSecret(ctx context.Context, serviceID, accountID string) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var foundID string
		var secretType domain.SecretType
		if err := tx.QueryRowContext(ctx, `SELECT id, secret_type FROM secrets WHERE id = ? AND service_id = ? AND deleted_at IS NULL`, accountID, serviceID).Scan(&foundID, &secretType); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		now := nowString()
		if _, err := tx.ExecContext(ctx, `UPDATE secrets SET deleted_at = ?, updated_at = ? WHERE id = ?`, now, now, accountID); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit local database transaction: %w", err)
	}
	return nil
}

// Secret reads only the selected live record, including standalone notes.
func (s *Store) Secret(ctx context.Context, id string) (domain.Secret, error) {
	items, err := secretsFrom(ctx, s.db, "id = ? AND (service_id IS NULL OR EXISTS (SELECT 1 FROM services s WHERE s.id=secrets.service_id AND s.deleted_at IS NULL))", id)
	if err != nil {
		return domain.Secret{}, err
	}
	if len(items) == 0 {
		return domain.Secret{}, ErrNotFound
	}
	return items[0], nil
}
