// SPDX-License-Identifier: MPL-2.0

package localdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"lefalock/internal/domain"
)

var ErrDirectoryCycle = errors.New("directory cannot be moved into itself or its descendant")
var ErrDirectoryNotEmpty = errors.New("directory is not empty")

func (s *Store) Directories(ctx context.Context) ([]domain.Directory, error) {
	return directoriesFrom(ctx, s.db)
}

func directoriesFrom(ctx context.Context, database queryer) ([]domain.Directory, error) {
	rows, err := database.QueryContext(ctx, `SELECT id, COALESCE(parent_id, ''), name_ciphertext, created_at, updated_at FROM directories ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list directories: %w", err)
	}
	var result []domain.Directory
	for rows.Next() {
		var item domain.Directory
		var created, updated string
		if err := rows.Scan(&item.ID, &item.ParentID, &item.NameCiphertext, &created, &updated); err != nil {
			return nil, err
		}
		if item.CreatedAt, err = parseTime(created); err != nil {
			return nil, err
		}
		if item.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	parents := make(map[string]string, len(result))
	for _, item := range result {
		parents[item.ID] = item.ParentID
	}
	for id := range parents {
		seen := make(map[string]bool)
		for cursor := id; cursor != ""; cursor = parents[cursor] {
			if seen[cursor] {
				return nil, errors.New("local directory tree contains a cycle")
			}
			seen[cursor] = true
			if _, ok := parents[cursor]; !ok {
				return nil, fmt.Errorf("directory %s has a missing parent", id)
			}
		}
	}
	for _, table := range []string{"services", "secrets"} {
		references, err := database.QueryContext(ctx, `SELECT directory_id FROM `+table+` WHERE directory_id IS NOT NULL AND deleted_at IS NULL`)
		if err != nil {
			return nil, err
		}
		for references.Next() {
			var id string
			if err := references.Scan(&id); err != nil {
				references.Close()
				return nil, err
			}
			if _, ok := parents[id]; !ok {
				references.Close()
				return nil, fmt.Errorf("%s references missing directory %s", table, id)
			}
		}
		if err := references.Err(); err != nil {
			references.Close()
			return nil, err
		}
		if err := references.Close(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func validDirectoryParent(ctx context.Context, tx *sql.Tx, parentID, childID string) error {
	seen := map[string]bool{}
	for parentID != "" {
		if parentID == childID {
			return ErrDirectoryCycle
		}
		if seen[parentID] {
			return errors.New("directory tree contains a cycle")
		}
		seen[parentID] = true
		var next sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT parent_id FROM directories WHERE id = ?`, parentID).Scan(&next); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		parentID = next.String
	}
	return nil
}

func (s *Store) CreateDirectory(ctx context.Context, item domain.Directory) error {
	if item.ID == "" || item.NameCiphertext == "" {
		return errors.New("directory id and encrypted name are required")
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if err := validDirectoryParent(ctx, tx, item.ParentID, item.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO directories(id, parent_id, name_ciphertext, created_at, updated_at) VALUES (?, NULLIF(?, ''), ?, ?, ?)`,
			item.ID, item.ParentID, item.NameCiphertext, item.CreatedAt.Format(timestampLayout), item.UpdatedAt.Format(timestampLayout))
		return err
	})
}

func (s *Store) RenameDirectory(ctx context.Context, id, ciphertext string) error {
	if ciphertext == "" {
		return errors.New("encrypted directory name is required")
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE directories SET name_ciphertext = ?, updated_at = ? WHERE id = ?`, ciphertext, nowString(), id)
		return affectedOne(result, err)
	})
}

func (s *Store) MoveDirectory(ctx context.Context, id, parentID string) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if err := validDirectoryParent(ctx, tx, parentID, id); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE directories SET parent_id = NULLIF(?, ''), updated_at = ? WHERE id = ?`, parentID, nowString(), id)
		return affectedOne(result, err)
	})
}

func (s *Store) DeleteDirectory(ctx context.Context, id string) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT
			(SELECT COUNT(*) FROM directories WHERE parent_id = ?) +
			(SELECT COUNT(*) FROM services WHERE directory_id = ? AND deleted_at IS NULL) +
			(SELECT COUNT(*) FROM secrets WHERE directory_id = ? AND deleted_at IS NULL)`, id, id, id).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return ErrDirectoryNotEmpty
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM directories WHERE id = ?`, id)
		return affectedOne(result, err)
	})
}

func (s *Store) MoveService(ctx context.Context, serviceID, directoryID string) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if err := validDirectoryParent(ctx, tx, directoryID, ""); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE services SET directory_id = NULLIF(?, '') WHERE id = ? AND deleted_at IS NULL`, directoryID, serviceID)
		return affectedOne(result, err)
	})
}

func (s *Store) MoveSecret(ctx context.Context, accountID, directoryID string) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if err := validDirectoryParent(ctx, tx, directoryID, ""); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE secrets SET directory_id = NULLIF(?, '') WHERE id = ? AND deleted_at IS NULL`, directoryID, accountID)
		return affectedOne(result, err)
	})
}

func affectedOne(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}
