// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lefalock/internal/config"
	"lefalock/internal/filelock"
	"lefalock/internal/localdb"
	"lefalock/internal/recovery"
	"lefalock/internal/secure"
	"lefalock/internal/service"
	"lefalock/internal/vault"
)

// vaultSession owns resources in acquisition order, including the process lock.
// Preparation deliberately does not open the database: recovery must work when
// the database or active key cannot be opened.
type vaultSession struct {
	conf        config.Config
	keys        *secure.KeyStore
	crypto      *secure.Crypto
	recovery    *recovery.Manager
	database    *localdb.Store
	unlock      func()
	credentials *service.Credentials
	backup      *service.Backup
	rotation    *service.KeyRotation
	warning     error
}

func prepareVaultSession(conf config.Config) (_ *vaultSession, err error) {
	s := &vaultSession{conf: conf}
	defer func() {
		if err != nil {
			err = errors.Join(err, s.Close())
		}
	}()
	if err = os.MkdirAll(conf.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("create vault directory: %w", err)
	}
	s.unlock, err = filelock.Acquire(filepath.Join(conf.DataDir, ".client.lock"), 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("another client is using this vault: %w", err)
	}
	s.keys, err = secure.NewKeyStore(conf.DataDir, conf.KeyringService)
	if err != nil {
		return nil, err
	}
	s.crypto, err = secure.NewCrypto(s.keys)
	if err != nil {
		return nil, err
	}
	s.recovery = recovery.New(conf.DataDir, s.keys, s.crypto)
	return s, nil
}

func (s *vaultSession) open(ctx context.Context, info vault.Info) (err error) {
	if s.database != nil {
		return errors.New("vault session is already open")
	}
	db, err := localdb.Open(vault.DatabasePath(s.conf.DataDir, info))
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, db.Close())
		}
	}()
	info, err = recoverVaultKey(ctx, s.conf.DataDir, info, db, s.keys)
	if err != nil {
		return err
	}
	if err = db.InitializeVault(ctx, info.ID, info.KeyRef); err != nil {
		return err
	}
	if err = service.FinishPreparedKeyRotation(ctx, db, s.keys); err != nil {
		return fmt.Errorf("clean up interrupted key rotation: %w", err)
	}
	s.warning = service.FinishKeyRotation(ctx, db, s.keys)
	s.crypto = s.crypto.ForVault(info.ID)
	s.credentials, err = service.NewCredentials(db, s.crypto, info.KeyRef)
	if err != nil {
		return err
	}
	s.backup, err = service.NewLocalBackup(db, s.crypto, info.KeyRef, filepath.Join(s.conf.DataDir, "backups"))
	if err != nil {
		return err
	}
	s.rotation, err = service.NewKeyRotation(db, s.keys, s.conf.DataDir, info.ID, info.KeyRef)
	if err != nil {
		return err
	}
	s.database = db
	return nil
}

func (s *vaultSession) Close() error {
	var err error
	if s.database != nil {
		err = s.database.Close()
		s.database = nil
	}
	if s.recovery != nil {
		err = errors.Join(err, s.recovery.Close())
		s.recovery = nil
	}
	if s.unlock != nil {
		s.unlock()
		s.unlock = nil
	}
	return err
}
