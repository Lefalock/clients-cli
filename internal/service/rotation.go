// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"lefalock/internal/secure"
	"lefalock/internal/vault"
)

type rotationState interface {
	Vault(context.Context) (string, string, error)
	PreparedKeyRef(context.Context) (string, error)
	ClearPreparedKeyRef(context.Context, string) error
	PreviousKeyRef(context.Context) (string, error)
	ClearPreviousKeyRef(context.Context, string) error
}

type rotationRepository interface {
	rotationState
	AcquireOperationLock(context.Context) (func(), error)
	PrepareKeyRotation(context.Context, string, string) error
	RotateKey(context.Context, string, string, func(string) (string, error)) error
}

type rotationKeys interface {
	LoadReference(string) ([]byte, error)
	CreateFreshReference(string) ([]byte, error)
	VerifyReference(string, []byte) error
	DeleteReference(string) error
}

type KeyRotation struct {
	database rotationRepository
	keys     rotationKeys
	dataDir  string
	vaultID  string
	oldRef   string
}

type RotationResult struct {
	Committed bool
	Warning   error
}

func NewKeyRotation(database rotationRepository, keys rotationKeys, dataDir, vaultID, keyRef string) (*KeyRotation, error) {
	if database == nil || keys == nil || dataDir == "" || vaultID == "" || keyRef == "" {
		return nil, errors.New("key rotation dependencies are required")
	}
	return &KeyRotation{database: database, keys: keys, dataDir: dataDir, vaultID: vaultID, oldRef: keyRef}, nil
}

func (r *KeyRotation) Rotate(ctx context.Context) (RotationResult, error) {
	unlock, err := r.database.AcquireOperationLock(ctx)
	if err != nil {
		return RotationResult{}, err
	}
	defer unlock()
	oldKey, err := r.keys.LoadReference(r.oldRef)
	if err != nil {
		return RotationResult{}, fmt.Errorf("load current encryption key: %w", err)
	}
	defer clearBytes(oldKey)
	referenceBytes := make([]byte, 32)
	if _, err := rand.Read(referenceBytes); err != nil {
		return RotationResult{}, fmt.Errorf("generate key reference: %w", err)
	}
	newRef := hex.EncodeToString(referenceBytes)
	if err := r.database.PrepareKeyRotation(ctx, r.oldRef, newRef); err != nil {
		return RotationResult{}, fmt.Errorf("prepare key rotation: %w", err)
	}
	newKey, err := r.keys.CreateFreshReference(newRef)
	if err != nil {
		return RotationResult{}, errors.Join(fmt.Errorf("store new encryption key: %w", err), FinishPreparedKeyRotation(context.Background(), r.database, r.keys))
	}
	defer clearBytes(newKey)
	if err := r.keys.VerifyReference(newRef, newKey); err != nil {
		return RotationResult{}, errors.Join(err, FinishPreparedKeyRotation(context.Background(), r.database, r.keys))
	}
	convert := func(ciphertext string) (string, error) {
		return secure.ReencryptWithKey(oldKey, newKey, ciphertext)
	}
	commitWarning := error(nil)
	if err := r.database.RotateKey(ctx, r.oldRef, newRef, convert); err != nil {
		// A failed COMMIT can have an uncertain outcome. Never discard the new
		// key until the authoritative database reference has been checked.
		_, activeRef, inspectErr := r.database.Vault(context.Background())
		if inspectErr != nil {
			return RotationResult{}, errors.Join(err, fmt.Errorf("inspect rotation outcome: %w", inspectErr))
		}
		if activeRef != newRef {
			if cleanupErr := FinishPreparedKeyRotation(context.Background(), r.database, r.keys); cleanupErr != nil {
				return RotationResult{}, errors.Join(err, fmt.Errorf("remove unused new key: %w", cleanupErr))
			}
			return RotationResult{}, err
		}
		commitWarning = err
	}
	result := RotationResult{Committed: true, Warning: commitWarning}
	if err := vault.SetKeyRef(r.dataDir, r.vaultID, newRef); err != nil {
		result.Warning = errors.Join(result.Warning, fmt.Errorf("save active vault reference: %w; it will be repaired on restart", err))
		return result, nil
	}
	if err := FinishKeyRotation(context.Background(), r.database, r.keys); err != nil {
		result.Warning = errors.Join(result.Warning, fmt.Errorf("remove old key: %w; cleanup will retry on restart", err))
	}
	return result, nil
}

func FinishPreparedKeyRotation(ctx context.Context, database rotationState, keys interface{ DeleteReference(string) error }) error {
	prepared, err := database.PreparedKeyRef(ctx)
	if err != nil || prepared == "" {
		return err
	}
	_, active, err := database.Vault(ctx)
	if err != nil {
		return err
	}
	if active == prepared {
		return errors.New("prepared key is now active")
	}
	if err := keys.DeleteReference(prepared); err != nil {
		return err
	}
	return database.ClearPreparedKeyRef(ctx, prepared)
}

func FinishKeyRotation(ctx context.Context, database rotationState, keys interface{ DeleteReference(string) error }) error {
	previous, err := database.PreviousKeyRef(ctx)
	if err != nil || previous == "" {
		return err
	}
	_, active, err := database.Vault(ctx)
	if err != nil {
		return err
	}
	if previous == active {
		return errors.New("previous key is still active")
	}
	if err := keys.DeleteReference(previous); err != nil {
		return err
	}
	return database.ClearPreviousKeyRef(ctx, previous)
}
