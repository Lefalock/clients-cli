// SPDX-License-Identifier: MPL-2.0

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
	"lefalock/internal/vault"
)

type pointerSnapshot struct {
	raw     []byte
	present bool
	valid   bool
	info    vault.Info
}

func (m *Manager) readPointer() (pointerSnapshot, error) {
	var p pointerSnapshot
	raw, err := os.ReadFile(filepath.Join(m.dir, "active-vault.json"))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, fmt.Errorf("read active vault pointer: %w", err)
	}
	p.raw, p.present = raw, true
	p.valid = json.Unmarshal(raw, &p.info) == nil && vault.Validate(p.info) == nil
	return p, nil
}

// PointerRepairPreview contains no secret values or key material. Its private
// snapshot binds confirmation to the exact pointer state the user inspected.
type PointerRepairPreview struct {
	VaultID        string
	Directories    int
	Services       int
	Secrets        int
	MissingPointer bool
	info           vault.Info
	pointer        pointerSnapshot
}

func (m *Manager) InspectPointerRepair(ctx context.Context) (*PointerRepairPreview, error) {
	a, err := m.readAttempt()
	if err != nil {
		return nil, err
	}
	pointer, err := m.readPointer()
	if err != nil {
		return nil, err
	}
	if pointer.valid && pointer.info.ID != a.Info.ID {
		return nil, errors.New("the active pointer refers to another vault and will not be overwritten")
	}
	if pointer.valid && pointer.info == a.Info {
		return nil, errors.New("the recovered vault is already active; finish recovery")
	}
	// A matching ID with an incorrect reference is damaged discovery. The
	// database and journal must agree before its reference can be repaired.
	preview, err := m.verifyPreparedVault(ctx, a)
	if err != nil {
		return nil, err
	}
	preview.pointer, preview.MissingPointer = pointer, !pointer.present
	return preview, nil
}

// verifyPreparedVault checks identity and either the committed import and all
// ciphertexts, or the strictly empty, local candidate created without backup.
func (m *Manager) verifyPreparedVault(ctx context.Context, a attempt) (*PointerRepairPreview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := localdb.OpenExisting(vault.DatabasePath(m.dir, a.Info))
	if err != nil {
		return nil, fmt.Errorf("inspect recovered database: %w", err)
	}
	defer db.Close()
	id, ref, err := db.Vault(ctx)
	if err != nil {
		return nil, err
	}
	if id != a.Info.ID || ref != a.Info.KeyRef {
		return nil, errors.New("recovered database identity does not match the journal")
	}
	imported, err := db.RecoveryImported(ctx)
	if err != nil {
		return nil, err
	}
	if a.Empty {
		ready, err := db.EmptyRecoveryReady(ctx)
		if err != nil {
			return nil, err
		}
		if !ready {
			return nil, errors.New("the candidate is not an empty local vault")
		}
	} else if !imported {
		return nil, errors.New("backup must be imported before repairing the pointer")
	}
	key, err := m.keys.LoadReference(a.Info.KeyRef)
	if err != nil {
		return nil, fmt.Errorf("load recovered vault key: %w", err)
	}
	defer clear(key)
	services, directories, notes, err := db.BackupSnapshotWithNotes(ctx)
	if err != nil {
		return nil, err
	}
	check := func(values ...string) error {
		for _, value := range values {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := secure.DecryptWithKey(key, value); err != nil {
				return fmt.Errorf("recovered vault data cannot be decrypted: %w", err)
			}
		}
		return nil
	}
	preview := &PointerRepairPreview{VaultID: id, info: a.Info, Directories: len(directories), Services: len(services), Secrets: len(notes)}
	for _, directory := range directories {
		if err := check(directory.NameCiphertext); err != nil {
			return nil, err
		}
	}
	for _, item := range services {
		if err := check(item.NameCiphertext); err != nil {
			return nil, err
		}
		for _, secret := range item.Secrets {
			if err := check(secret.SecretNameCiphertext, secret.PasswordCiphertext); err != nil {
				return nil, err
			}
			preview.Secrets++
		}
	}
	for _, note := range notes {
		if err := check(note.SecretNameCiphertext, note.PasswordCiphertext); err != nil {
			return nil, err
		}
	}
	return preview, nil
}

// RepairPointer is an explicit, confirmed operation. A healthy pointer to a
// different vault is never overwritten, even if it appeared after preview.
func (m *Manager) RepairPointer(ctx context.Context, preview *PointerRepairPreview) error {
	if preview == nil {
		return errors.New("inspect the recovered vault before repairing its pointer")
	}
	if err := m.Close(); err != nil {
		return err
	}
	unlock, err := filelock.Acquire(filepath.Join(m.dir, ".vault.lock"), 10*time.Second)
	if err != nil {
		return err
	}
	defer unlock()
	a, err := m.readAttempt()
	if err != nil {
		return err
	}
	if a.Info != preview.info {
		return errors.New("recovery attempt changed; inspect it again")
	}
	current, err := m.readPointer()
	if err != nil {
		return err
	}
	if current.valid && current.info.ID != a.Info.ID {
		return errors.New("the active pointer refers to another vault and will not be overwritten")
	}
	if current.present != preview.pointer.present || !bytes.Equal(current.raw, preview.pointer.raw) {
		return errors.New("active pointer changed after inspection; inspect it again")
	}
	if _, err := m.verifyPreparedVault(ctx, a); err != nil {
		return err
	}
	if err := m.archiveDamagedPointer(a, current); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.activateLocked(a, current.valid && current.info == a.Info)
}

// Each attempt gets a distinct archive; a retry cannot overwrite evidence from
// an earlier interruption. Missing pointers have a durable presence manifest.
func (m *Manager) archiveDamagedPointer(a attempt, pointer pointerSnapshot) error {
	id, err := domain.NewID()
	if err != nil {
		return err
	}
	root := filepath.Join(m.dir, "recovery")
	vaultArchive := filepath.Join(root, a.Info.ID)
	repairs := filepath.Join(vaultArchive, "pointer-repairs")
	if err := os.MkdirAll(repairs, 0700); err != nil {
		return err
	}
	archive := filepath.Join(repairs, id)
	if err := os.Mkdir(archive, 0700); err != nil {
		return err
	}
	if pointer.present {
		if err := saveFile(filepath.Join(archive, "active-vault.json"), pointer.raw); err != nil {
			return fmt.Errorf("archive damaged pointer: %w", err)
		}
	}
	manifest, err := json.Marshal(struct {
		Vault      vault.Info `json:"vault"`
		HadPointer bool       `json:"had_pointer"`
	}{a.Info, pointer.present})
	if err != nil {
		return err
	}
	if err := saveFile(filepath.Join(archive, "pointer-state.json"), manifest); err != nil {
		return err
	}
	for _, directory := range []string{archive, repairs, vaultArchive, root, m.dir} {
		if err := durable.SyncDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}
