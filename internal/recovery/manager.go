// SPDX-License-Identifier: MPL-2.0

// Package recovery manages a candidate vault without modifying the old database.
package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lefalock/internal/domain"
	"lefalock/internal/durable"
	"lefalock/internal/filelock"
	"lefalock/internal/localdb"
	"lefalock/internal/secure"
	"lefalock/internal/service"
	"lefalock/internal/vault"
)

type Keys interface {
	VerifyReference(string, []byte) error
	CreateFreshReference(string) ([]byte, error)
	LoadReference(string) ([]byte, error)
	DeleteReference(string) error
}

type attempt struct {
	Info        vault.Info `json:"vault"`
	Original    []byte     `json:"original_discovery,omitempty"`
	HadOriginal bool       `json:"had_original"`
	Empty       bool       `json:"empty,omitempty"`
	// A pointer distinguishes a valid false state from a missing required field.
	ActivationStarted *bool `json:"activation_started"`
}

type Status struct {
	Pending  bool
	Imported bool
	Empty    bool
	// Ready allows activation after either a committed import or empty setup.
	Ready             bool
	Active            bool
	CanDiscard        bool
	DiscardReason     string
	CanRepairPointer  bool
	ActivationBlocked string
}

type Manager struct {
	dir      string
	keys     Keys
	crypto   *secure.Crypto
	database *localdb.Store
	backup   *service.Backup
}

func New(dir string, keys Keys, crypto *secure.Crypto) *Manager {
	return &Manager{dir: dir, keys: keys, crypto: crypto}
}

func (m *Manager) journalPath() string { return filepath.Join(m.dir, "vault-recovery.json") }

func (m *Manager) readAttempt() (attempt, error) {
	var a attempt
	raw, err := os.ReadFile(m.journalPath())
	if err != nil {
		return a, err
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, fmt.Errorf("recovery journal is corrupt: %w", err)
	}
	if a.ActivationStarted == nil {
		return a, errors.New("recovery journal is missing activation state")
	}
	if err := vault.Validate(a.Info); err != nil {
		return a, fmt.Errorf("invalid recovery journal: %w", err)
	}
	// The candidate key must belong to its vault identity.
	if a.Info.KeyRef != secure.KeyReference(a.Info.ID) {
		return a, errors.New("recovery journal has invalid key ownership")
	}
	return a, nil
}

func (m *Manager) Status(ctx context.Context) (Status, error) {
	a, err := m.readAttempt()
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	status := Status{Pending: true, Empty: a.Empty}
	if err := m.checkDiscard(a); err == nil {
		status.CanDiscard = true
	} else {
		status.DiscardReason = err.Error()
	}
	active, err := vault.Discover(m.dir)
	status.Active = err == nil && active.ID == a.Info.ID
	db, err := localdb.OpenExisting(vault.DatabasePath(m.dir, a.Info))
	if errors.Is(err, os.ErrNotExist) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	defer db.Close()
	// Only a safely discardable empty-creation attempt may resume from a
	// schema-less SQLite file. Do not suppress arbitrary schema/read errors.
	if a.Empty && status.CanDiscard {
		uninitialized, err := db.RecoverySchemaUninitialized(ctx)
		if err != nil {
			return status, err
		}
		if uninitialized {
			return status, nil
		}
	}
	// A crash can precede InitializeRecovery; a candidate not fully initialized
	// can be discarded, but it cannot be treated as an imported vault.
	var id, ref string
	id, ref, err = db.Vault(ctx)
	if errors.Is(err, localdb.ErrNotFound) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	if id != a.Info.ID || ref != a.Info.KeyRef {
		return status, errors.New("recovery database identity changed")
	}
	status.Imported, err = db.RecoveryImported(ctx)
	if err != nil {
		return status, err
	}
	status.Ready = status.Imported
	if a.Empty {
		status.Ready, err = db.EmptyRecoveryReady(ctx)
		if err != nil {
			return status, err
		}
	}
	if status.Ready {
		pointer, err := m.readPointer()
		if err != nil {
			status.ActivationBlocked = err.Error()
			return status, nil
		}
		if !pointer.valid || (pointer.info.ID == a.Info.ID && pointer.info.KeyRef != a.Info.KeyRef) {
			status.CanRepairPointer = true
		} else if pointer.info != a.Info && (!a.HadOriginal || !bytes.Equal(pointer.raw, a.Original)) {
			status.ActivationBlocked = "The active pointer refers to another vault and will not be overwritten."
		}
	}
	return status, nil
}

func (m *Manager) Begin(ctx context.Context) error {
	return m.begin(ctx, false)
}

// BeginEmpty persists a distinct intent; it never marks a backup as imported.
func (m *Manager) BeginEmpty(ctx context.Context) error {
	return m.begin(ctx, true)
}

func (m *Manager) begin(ctx context.Context, empty bool) error {
	if _, err := m.readAttempt(); err == nil {
		return errors.New("an unfinished recovery already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	id, err := domain.NewID()
	if err != nil {
		return err
	}
	notStarted := false
	a := attempt{Info: vault.Info{ID: id, KeyRef: secure.KeyReference(id)}, Empty: empty, ActivationStarted: &notStarted}
	if _, err := os.Lstat(filepath.Dir(vault.DatabasePath(m.dir, a.Info))); err == nil {
		return errors.New("new vault directory already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	a.Original, err = os.ReadFile(filepath.Join(m.dir, "active-vault.json"))
	a.HadOriginal = err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("preserve original discovery: %w", err)
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	// Persist ownership before a key backend can leave a partial share.
	if err := saveFile(m.journalPath(), raw); err != nil {
		return err
	}
	key, err := m.keys.CreateFreshReference(a.Info.KeyRef)
	clear(key)
	if err != nil {
		if errors.Is(err, secure.ErrKeyExists) {
			// No resource belongs to this attempt when fresh creation rejected it.
			if removeErr := os.Remove(m.journalPath()); removeErr != nil {
				return errors.Join(err, removeErr)
			}
			return errors.Join(err, durable.SyncDirectory(m.dir))
		}
		return err
	}
	return m.openCandidate(ctx, a)
}

func (m *Manager) openCandidate(ctx context.Context, a attempt) error {
	if m.database != nil {
		return nil
	}
	key, err := m.keys.LoadReference(a.Info.KeyRef)
	clear(key)
	if err != nil {
		return err
	}
	db, err := localdb.Open(vault.DatabasePath(m.dir, a.Info))
	if err != nil {
		return err
	}
	if err := db.InitializeVault(ctx, a.Info.ID, a.Info.KeyRef); err != nil {
		db.Close()
		return err
	}
	if err := db.InitializeRecovery(ctx); err != nil {
		db.Close()
		return err
	}
	for _, dir := range []string{filepath.Dir(vault.DatabasePath(m.dir, a.Info)), filepath.Join(m.dir, "vaults"), m.dir} {
		if err := durable.SyncDirectory(dir); err != nil {
			db.Close()
			return err
		}
	}
	backup, err := service.NewLocalBackup(db, m.crypto.ForVault(a.Info.ID), a.Info.KeyRef, filepath.Join(m.dir, "backups"))
	if err != nil {
		db.Close()
		return err
	}
	m.database, m.backup = db, backup
	return nil
}

func (m *Manager) Preview(ctx context.Context, path, phrase string) (*service.RestorePreview, error) {
	a, err := m.readAttempt()
	if err != nil {
		return nil, err
	}
	if a.Empty {
		return nil, errors.New("this attempt creates an empty vault; backup import is unavailable")
	}
	if err := m.openCandidate(ctx, a); err != nil {
		return nil, err
	}
	imported, err := m.database.RecoveryImported(ctx)
	if err != nil {
		return nil, err
	}
	if imported {
		return nil, errors.New("backup is already imported; finish activation")
	}
	return m.backup.PreviewRestore(ctx, path, phrase)
}

func (m *Manager) Summary(preview *service.RestorePreview) (service.RestoreSummary, error) {
	if m.backup == nil {
		return service.RestoreSummary{}, errors.New("recovery is not initialized")
	}
	return m.backup.RestoreSummaryFor(preview, nil)
}

func (m *Manager) Apply(ctx context.Context, preview *service.RestorePreview) error {
	a, err := m.readAttempt()
	if err != nil {
		return err
	}
	if a.Empty {
		return errors.New("this attempt creates an empty vault; backup import is unavailable")
	}
	if m.backup == nil {
		return errors.New("recovery is not initialized")
	}
	imported, err := m.database.RecoveryImported(ctx)
	if err != nil {
		return err
	}
	if imported {
		return errors.New("backup is already imported; finish activation")
	}
	_, err = m.backup.ApplyRestore(ctx, preview, nil)
	return err
}

func (m *Manager) Activate(ctx context.Context) error {
	a, err := m.readAttempt()
	if err != nil {
		return err
	}
	status, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if !status.Ready {
		return errors.New("the candidate vault must be prepared before activation")
	}
	key, err := m.keys.LoadReference(a.Info.KeyRef)
	clear(key)
	if err != nil {
		return err
	}
	if err := m.Close(); err != nil {
		return err
	}
	unlock, err := filelock.Acquire(filepath.Join(m.dir, ".vault.lock"), 10*time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	lockedAttempt, err := m.readAttempt()
	if err != nil {
		return err
	}
	if lockedAttempt.Info != a.Info || lockedAttempt.Empty != a.Empty {
		return errors.New("recovery attempt changed during activation")
	}
	a = lockedAttempt
	if a.Empty {
		if _, err := m.verifyPreparedVault(ctx, a); err != nil {
			return err
		}
	}
	pointer := filepath.Join(m.dir, "active-vault.json")
	current, err := os.ReadFile(pointer)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	active, activeErr := vault.Discover(m.dir)
	alreadyActive := activeErr == nil && active == a.Info
	if !alreadyActive && (a.HadOriginal != (err == nil) || !bytes.Equal(current, a.Original)) {
		return errors.New("active vault changed during recovery")
	}
	return m.activateLocked(a, alreadyActive)
}

// ContinueEmpty finishes initialization after a crash without recreating a key
// or overwriting data. Missing/partial keys remain available for safe discard.
func (m *Manager) ContinueEmpty(ctx context.Context) error {
	a, err := m.readAttempt()
	if err != nil {
		return err
	}
	if !a.Empty {
		return errors.New("this attempt requires a backup")
	}
	if a.ActivationStarted == nil || *a.ActivationStarted {
		return m.Activate(ctx)
	}
	if err := m.openCandidate(ctx, a); err != nil {
		return err
	}
	if _, err := m.verifyPreparedVault(ctx, a); err != nil {
		return err
	}
	return m.Activate(ctx)
}

// The caller holds .vault.lock and has checked the candidate and pointer.
func (m *Manager) activateLocked(a attempt, alreadyActive bool) error {
	pointer := filepath.Join(m.dir, "active-vault.json")
	// Persist intent before replacing discovery. Even a failed directory sync
	// can leave the pointer switched; cancellation must remain forbidden.
	started := true
	a.ActivationStarted = &started
	journal, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if err := saveFile(m.journalPath(), journal); err != nil {
		return err
	}
	archive := filepath.Join(m.dir, "recovery", a.Info.ID)
	if err := os.MkdirAll(archive, 0700); err != nil {
		return err
	}
	// Keep the snapshot even when discovery was absent. It contains no key shares.
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if a.HadOriginal {
		if err := saveFile(filepath.Join(archive, "active-vault.json"), a.Original); err != nil {
			return fmt.Errorf("archive original discovery: %w", err)
		}
	}
	if err := saveFile(filepath.Join(archive, "original-discovery.json"), raw); err != nil {
		return fmt.Errorf("archive original discovery: %w", err)
	}
	if err := durable.SyncDirectory(filepath.Dir(archive)); err != nil {
		return err
	}
	if err := durable.SyncDirectory(m.dir); err != nil {
		return err
	}
	if !alreadyActive {
		raw, err := json.Marshal(a.Info)
		if err != nil {
			return err
		}
		if err := saveFile(pointer, raw); err != nil {
			return err
		}
	} else if err := durable.SyncDirectory(m.dir); err != nil {
		return err
	}
	if err := os.Remove(m.journalPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return durable.SyncDirectory(m.dir)
}

func (m *Manager) Cancel(ctx context.Context) error {
	a, err := m.readAttempt()
	if errors.Is(err, os.ErrNotExist) {
		return m.Close()
	}
	if err != nil {
		return err
	}
	if err := m.Close(); err != nil {
		return err
	}
	unlock, err := filelock.Acquire(filepath.Join(m.dir, ".vault.lock"), 10*time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	// Re-read the journal under the same lock as activation. UI status alone
	// cannot authorize deletion, and a failed pointer read is not inactivity.
	a, err = m.readAttempt()
	if err != nil {
		return err
	}
	if err := m.checkDiscard(a); err != nil {
		return err
	}
	if err := m.keys.DeleteReference(a.Info.KeyRef); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Dir(vault.DatabasePath(m.dir, a.Info))); err != nil {
		return err
	}
	if err := durable.SyncDirectory(filepath.Join(m.dir, "vaults")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(m.journalPath()); err != nil {
		return err
	}
	return durable.SyncDirectory(m.dir)
}

// checkDiscard is also used for advisory UI status. Cancel repeats it under
// .vault.lock immediately before deleting any resources.
func (m *Manager) checkDiscard(a attempt) error {
	if a.ActivationStarted == nil {
		return errors.New("the recovery journal does not record activation state; cancellation is unsafe")
	}
	if *a.ActivationStarted {
		return errors.New("recovery activation has started; cancellation is unsafe, finish recovery")
	}
	current, err := os.ReadFile(filepath.Join(m.dir, "active-vault.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("confirm original active vault before cancellation: %w", err)
	}
	if a.HadOriginal != (err == nil) || !bytes.Equal(current, a.Original) {
		return errors.New("active vault metadata changed; cancellation is unsafe")
	}
	// A candidate can never be discarded when discovery points to its ID.
	var active vault.Info
	if json.Unmarshal(current, &active) == nil && active.ID == a.Info.ID {
		return errors.New("recovery vault is already active; finish recovery")
	}
	return nil
}

func (m *Manager) Close() error {
	if m.database == nil {
		return nil
	}
	err := m.database.Close()
	m.database, m.backup = nil, nil
	return err
}

// saveFile replaces a file durably without truncating an existing destination.
func saveFile(path string, raw []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".recovery-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return durable.SyncDirectory(filepath.Dir(path))
}
