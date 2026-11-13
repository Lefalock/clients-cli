// SPDX-License-Identifier: MPL-2.0

package localdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"lefalock/internal/domain"
	"lefalock/internal/filelock"
)

const timestampLayout = time.RFC3339Nano

var ErrNotFound = domain.ErrNotFound

type Store struct {
	db            *sql.DB
	path          string
	diagnosticDir string
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("protect database directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open local database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, path: path}
	if err := store.initialize(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("protect local database: %w", err)
	}
	return store, nil
}

// OpenExisting reads an existing database without creating it or running migrations.
// mode=ro retains SQLite's handling of committed WAL records.
func OpenExisting(path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	// SQLite can create or update a shared-memory sidecar even with mode=ro.
	// Inspect a private snapshot, including committed WAL frames, to preserve
	// every file in the source vault. Startup holds the client process lock.
	dir, err := os.MkdirTemp("", "lefalock-diagnosis-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	snapshot := filepath.Join(dir, "vault.db")
	for _, suffix := range []string{"", "-wal"} {
		source, err := os.Open(path + suffix)
		if errors.Is(err, os.ErrNotExist) && suffix != "" {
			continue
		}
		if err != nil {
			cleanup()
			return nil, err
		}
		destination, err := os.OpenFile(snapshot+suffix, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			source.Close()
			cleanup()
			return nil, err
		}
		_, copyErr := io.Copy(destination, source)
		closeErr := destination.Close()
		source.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			cleanup()
			return nil, err
		}
	}
	uri := (&url.URL{Scheme: "file", Path: snapshot, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		cleanup()
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		cleanup()
		return nil, err
	}
	return &Store{db: db, path: path, diagnosticDir: dir}, nil
}

func (s *Store) Close() error {
	err := s.db.Close()
	if s.diagnosticDir != "" {
		err = errors.Join(err, os.RemoveAll(s.diagnosticDir))
	}
	return err
}

// AcquireOperationLock serializes backup, restore and key rotation.
func (s *Store) AcquireOperationLock(ctx context.Context) (func(), error) {
	timeout := 10 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, ctx.Err()
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	unlock, err := filelock.Acquire(s.path+".operation.lock", timeout)
	if err != nil {
		return nil, fmt.Errorf("lock vault operation: %w", err)
	}
	return unlock, nil
}

func (s *Store) initialize(ctx context.Context) error {
	version, appID, err := s.schemaIdentity(ctx)
	if err != nil {
		return fmt.Errorf("read local database format: %w", err)
	}
	fresh := version == 0 && appID == 0
	if fresh {
		var objects int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema`).Scan(&objects); err != nil {
			return err
		}
		fresh = objects == 0
	}
	if !fresh {
		if err := s.validateSchema(ctx); err != nil {
			return err
		}
	}
	for _, statement := range []string{
		`PRAGMA foreign_keys = ON`, `PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`, `PRAGMA synchronous = FULL`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure local database: %w", err)
		}
	}
	if !fresh {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, initialSchema); err != nil {
		return fmt.Errorf("initialize local database: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", applicationID, schemaVersion)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) InitializeVault(ctx context.Context, vaultID, keyRef string) error {
	if vaultID == "" || keyRef == "" {
		return errors.New("vault id and key reference are required")
	}
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO vault_metadata(singleton, vault_id, key_ref, created_at)
        VALUES (1, ?, ?, ?)
        ON CONFLICT(singleton) DO NOTHING`, vaultID, keyRef, nowString())
	if err != nil {
		return fmt.Errorf("initialize vault metadata: %w", err)
	}
	var existingVault, existingKey string
	if err := s.db.QueryRowContext(ctx, `SELECT vault_id, key_ref FROM vault_metadata WHERE singleton = 1`).Scan(&existingVault, &existingKey); err != nil {
		return fmt.Errorf("read vault metadata: %w", err)
	}
	if existingVault != vaultID || existingKey != keyRef {
		return errors.New("local database belongs to another vault")
	}
	return nil
}

func (s *Store) Vault(ctx context.Context) (vaultID, keyRef string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT vault_id, key_ref FROM vault_metadata WHERE singleton = 1`).Scan(&vaultID, &keyRef)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("read vault metadata: %w", err)
	}
	return vaultID, keyRef, nil
}

func (s *Store) PreviousKeyRef(ctx context.Context) (string, error) {
	var reference string
	if err := s.db.QueryRowContext(ctx, `SELECT previous_key_ref FROM vault_metadata WHERE singleton = 1`).Scan(&reference); err != nil {
		return "", fmt.Errorf("read previous key reference: %w", err)
	}
	return reference, nil
}

func (s *Store) ClearPreviousKeyRef(ctx context.Context, reference string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE vault_metadata SET previous_key_ref = '' WHERE singleton = 1 AND previous_key_ref = ?`, reference)
	return err
}

func (s *Store) PreparedKeyRef(ctx context.Context) (string, error) {
	var reference string
	if err := s.db.QueryRowContext(ctx, `SELECT prepared_key_ref FROM vault_metadata WHERE singleton = 1`).Scan(&reference); err != nil {
		return "", fmt.Errorf("read prepared key reference: %w", err)
	}
	return reference, nil
}

func (s *Store) PrepareKeyRotation(ctx context.Context, oldRef, newRef string) error {
	if oldRef == "" || newRef == "" || oldRef == newRef {
		return errors.New("invalid key rotation")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE vault_metadata SET prepared_key_ref = ? WHERE singleton = 1 AND key_ref = ? AND previous_key_ref = '' AND prepared_key_ref = ''`, newRef, oldRef)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errors.New("vault key changed or previous rotation needs cleanup")
	}
	return nil
}

func (s *Store) ClearPreparedKeyRef(ctx context.Context, reference string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE vault_metadata SET prepared_key_ref = '' WHERE singleton = 1 AND prepared_key_ref = ?`, reference)
	return err
}

func nowString() string { return time.Now().UTC().Format(timestampLayout) }

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(timestampLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse database timestamp: %w", err)
	}
	return parsed, nil
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
