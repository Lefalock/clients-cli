// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"lefalock/internal/config"
	"lefalock/internal/console"
	"lefalock/internal/legal"
	"lefalock/internal/localdb"
	"lefalock/internal/secure"
	"lefalock/internal/ui"
	"lefalock/internal/update"
	"lefalock/internal/vault"
)

func Run(args []string, version string) error {
	if handled, err := legal.RunCommand(args, version, os.Stdout); handled {
		return err
	}
	if handled, err := update.RunPrivilegedInstall(args); handled {
		return err
	}
	if handled := runVersionCommand(args, version, os.Stdout); handled {
		return nil
	}
	handled, err := config.RunCommand(args, version, os.Stdout)
	if err != nil || handled {
		return err
	}
	if len(args) > 0 && args[0] == "update" {
		conf, loadErr := config.Load(nil, version)
		if loadErr != nil {
			return loadErr
		}
		updateManager, managerErr := newUpdateManager(conf, version)
		if managerErr != nil {
			return managerErr
		}
		_, commandErr := update.RunCommand(context.Background(), updateManager, args, os.Stdout)
		return commandErr
	}
	if console.Handles(args) {
		return runConsole(args, version)
	}
	conf, err := config.Load(args, version)
	if err != nil {
		return err
	}
	shutdownCtx, stopSignals := shutdownContext()
	defer stopSignals()
	session, err := prepareVaultSession(conf)
	if err != nil {
		return err
	}
	defer session.Close()
	keyStore, recoveryManager := session.keys, session.recovery
	pending, pendingErr := recoveryManager.Status(shutdownCtx)
	var vaultInfo vault.Info
	if !pending.Pending && pendingErr == nil {
		vaultInfo, err = openActiveVault(shutdownCtx, conf.DataDir, keyStore)
	}
	if err != nil || pending.Pending || pendingErr != nil {
		cause := err
		if pendingErr != nil {
			cause = pendingErr
		}
		initial := filepath.Join(conf.DataDir, "backups")
		if remembered, loadErr := config.LastRestoreDirectory(conf.ConfigPath); loadErr == nil && remembered != "" {
			initial = remembered
		}
		proceed, uiErr := ui.RunStartupRecovery(cause, recoveryManager, func() error {
			_, openErr := openActiveVault(shutdownCtx, conf.DataDir, keyStore)
			return openErr
		}, needsVaultRecovery, initial)
		if uiErr != nil {
			return uiErr
		}
		if !proceed {
			return nil
		}
		vaultInfo, err = openActiveVault(shutdownCtx, conf.DataDir, keyStore)
		if err != nil {
			return err
		}
	}
	if err := session.open(shutdownCtx, vaultInfo); err != nil {
		return err
	}
	if session.warning != nil {
		fmt.Fprintf(os.Stderr, "lefalock: previous key cleanup is pending: %v\n", session.warning)
	}
	updateManager, err := newUpdateManager(conf, version)
	if err != nil {
		return err
	}
	clipboard := ui.NewSystemClipboard(30 * time.Second)
	defer clipboard.Close()
	userInterface, err := ui.New(session.credentials, session.backup, updateManager, clipboard, conf.ConfigPath)
	if err != nil {
		return err
	}
	userInterface.SetKeyRotation(session.rotation)
	runErr := userInterface.RunContext(shutdownCtx)
	stopSignals()
	closeErr := clipboard.Close()
	return errors.Join(runErr, closeErr)
}

func recoverVaultKey(ctx context.Context, dataDir string, info vault.Info, database *localdb.Store, keys *secure.KeyStore) (vault.Info, error) {
	dbVaultID, dbKeyRef, err := database.Vault(ctx)
	if errors.Is(err, localdb.ErrNotFound) {
		return info, nil
	}
	if err != nil {
		return info, err
	}
	if dbVaultID != info.ID {
		return info, errors.New("local database belongs to another vault")
	}
	if dbKeyRef == info.KeyRef {
		return info, nil
	}
	if _, err := keys.LoadReference(dbKeyRef); err != nil {
		return info, fmt.Errorf("recover rotated encryption key: %w", err)
	}
	if err := vault.SetKeyRef(dataDir, info.ID, dbKeyRef); err != nil {
		return info, fmt.Errorf("recover active vault reference: %w", err)
	}
	info.KeyRef = dbKeyRef
	return info, nil
}

func runVersionCommand(args []string, version string, output io.Writer) bool {
	if len(args) != 1 || (args[0] != "--version" && args[0] != "-v") {
		return false
	}
	fmt.Fprintf(output, "lefalock %s\n", version)
	return true
}

func newUpdateManager(conf config.Config, version string) (*update.Manager, error) {
	return update.New(update.Config{
		Version: version,
		DataDir: conf.DataDir,
	})
}
