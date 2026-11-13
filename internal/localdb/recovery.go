// SPDX-License-Identifier: MPL-2.0

package localdb

import (
	"context"
	"database/sql"
	"errors"
)

// RecoverySchemaUninitialized identifies SQLite files created before the first
// schema transaction. Unknown schemas and damaged files must not qualify.
func (s *Store) RecoverySchemaUninitialized(ctx context.Context) (bool, error) {
	var uninitialized bool
	err := s.db.QueryRowContext(ctx, `SELECT
  (SELECT user_version FROM pragma_user_version) = 0
  AND (SELECT application_id FROM pragma_application_id) = 0
  AND NOT EXISTS (SELECT 1 FROM sqlite_schema)`).Scan(&uninitialized)
	return uninitialized, err
}

// InitializeRecovery marks only a new candidate database; regular vaults have no row.
func (s *Store) InitializeRecovery(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO recovery_state(singleton) VALUES (1) ON CONFLICT DO NOTHING`)
	return err
}

func (s *Store) RecoveryImported(ctx context.Context) (bool, error) {
	var imported bool
	err := s.db.QueryRowContext(ctx, `SELECT imported FROM recovery_state WHERE singleton = 1`).Scan(&imported)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return imported, err
}

// EmptyRecoveryReady requires an initialized, local candidate with no records,
// including deleted records.
func (s *Store) EmptyRecoveryReady(ctx context.Context) (bool, error) {
	if err := s.validateSchema(ctx); err != nil {
		return false, err
	}
	var ready bool
	err := s.db.QueryRowContext(ctx, `SELECT
  imported = 0
  AND NOT EXISTS (SELECT 1 FROM services)
  AND NOT EXISTS (SELECT 1 FROM secrets)
  AND NOT EXISTS (SELECT 1 FROM directories)
  AND EXISTS (SELECT 1 FROM vault_metadata)
  FROM recovery_state WHERE singleton = 1`).Scan(&ready)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return ready, err
}
