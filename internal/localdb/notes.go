// SPDX-License-Identifier: MPL-2.0

package localdb

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"lefalock/internal/domain"
)

func (s *Store) StandaloneNotes(ctx context.Context) ([]domain.Secret, error) {
	return standaloneNotesFrom(ctx, s.db)
}
func standaloneNotesFrom(ctx context.Context, database queryer) ([]domain.Secret, error) {
	rows, err := database.QueryContext(ctx, `SELECT id, COALESCE(directory_id,''), secret_name_ciphertext, password_ciphertext, created_at, updated_at FROM secrets WHERE secret_type='note' AND service_id IS NULL AND deleted_at IS NULL ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.Secret
	for rows.Next() {
		n := domain.Secret{Type: domain.SecretNote}
		var created, updated string
		if err := rows.Scan(&n.ID, &n.DirectoryID, &n.SecretNameCiphertext, &n.PasswordCiphertext, &created, &updated); err != nil {
			return nil, err
		}
		if n.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		if n.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

func validNoteParent(ctx context.Context, tx *sql.Tx, serviceID, directoryID string) error {
	if serviceID != "" {
		if directoryID != "" {
			return errors.New("choose a folder or a service for the note")
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM services WHERE id=? AND deleted_at IS NULL)`, serviceID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	return validDirectoryParent(ctx, tx, directoryID, "")
}

func (s *Store) CreateNote(ctx context.Context, n domain.Secret) error {
	if n.ID == "" || n.SecretNameCiphertext == "" || n.PasswordCiphertext == "" {
		return errors.New("note id and encrypted title/text are required")
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if err := validNoteParent(ctx, tx, n.ServiceID, n.DirectoryID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO secrets(id,service_id,directory_id,secret_name_ciphertext,password_ciphertext,secret_type,created_at,updated_at) VALUES (?,NULLIF(?,''),NULLIF(?,''),?,?,'note',?,?)`, n.ID, n.ServiceID, n.DirectoryID, n.SecretNameCiphertext, n.PasswordCiphertext, n.CreatedAt.Format(timestampLayout), n.UpdatedAt.Format(timestampLayout))
		return err
	})
}

func (s *Store) writeNote(ctx context.Context, id string, write func(*sql.Tx) error) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var active bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM secrets n LEFT JOIN services s ON s.id=n.service_id WHERE n.id=? AND n.secret_type='note' AND n.deleted_at IS NULL AND (n.service_id IS NULL OR s.deleted_at IS NULL))`, id).Scan(&active); err != nil {
			return err
		}
		if !active {
			return ErrNotFound
		}
		return write(tx)
	})
}
func (s *Store) UpdateNote(ctx context.Context, id, ciphertext string) error {
	if ciphertext == "" {
		return errors.New("encrypted note text is required")
	}
	return s.writeNote(ctx, id, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `UPDATE secrets SET password_ciphertext=?,updated_at=? WHERE id=?`, ciphertext, nowString(), id)
		return affectedOne(r, e)
	})
}
func (s *Store) RenameNote(ctx context.Context, id, ciphertext string) error {
	if ciphertext == "" {
		return errors.New("encrypted note title is required")
	}
	return s.writeNote(ctx, id, func(tx *sql.Tx) error {
		r, e := tx.ExecContext(ctx, `UPDATE secrets SET secret_name_ciphertext=?,updated_at=? WHERE id=?`, ciphertext, nowString(), id)
		return affectedOne(r, e)
	})
}
func (s *Store) MoveNote(ctx context.Context, id, serviceID, directoryID string) error {
	return s.writeNote(ctx, id, func(tx *sql.Tx) error {
		if err := validNoteParent(ctx, tx, serviceID, directoryID); err != nil {
			return err
		}
		r, e := tx.ExecContext(ctx, `UPDATE secrets SET service_id=NULLIF(?,''),directory_id=NULLIF(?,''),updated_at=? WHERE id=?`, serviceID, directoryID, nowString(), id)
		return affectedOne(r, e)
	})
}
func (s *Store) DeleteNote(ctx context.Context, id string) error {
	return s.writeNote(ctx, id, func(tx *sql.Tx) error {
		now := time.Now().UTC().Format(timestampLayout)
		r, e := tx.ExecContext(ctx, `UPDATE secrets SET deleted_at=?,updated_at=? WHERE id=?`, now, now, id)
		return affectedOne(r, e)
	})
}
