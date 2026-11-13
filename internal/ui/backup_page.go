// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"
	"os"
	"path/filepath"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/config"
	"lefalock/internal/service"
)

func (u *App) backupPage() tview.Primitive {
	panel := tview.NewFlex().SetDirection(tview.FlexRow)
	panel.SetBorder(true).SetTitle(" Backups ").SetBorderColor(commandTheme.Border)
	panel.AddItem(tview.NewTextView().SetText("Create or restore an encrypted copy of your local vault.\nKeep recovery words separate from the backup.").SetWrap(true), 3, 0, false)
	create := actionButton("Create backup", nil)
	busy := false
	create.SetSelectedFunc(func() {
		if busy {
			return
		}
		directory, err := u.backup.DefaultDirectory()
		if err != nil {
			u.showError(err)
			return
		}
		u.chooseDirectory("Save backup", directory, create, func(selected string) {
			busy = true
			create.SetLabel("[Creating encrypted backup...]")
			create.SetDisabled(true)
			var result service.BackupResult
			u.beginProtectedTask(func(ctx context.Context) error {
				var createErr error
				result, createErr = u.backup.Create(ctx, selected)
				return createErr
			}, func(err error) {
				busy = false
				create.SetLabel("[Create backup]")
				create.SetDisabled(false)
				if err != nil {
					u.showError(err)
					return
				}
				u.showBackupCreated(result)
			})
		}, func() { u.focusFirst(routeBackup) })
	})
	panel.AddItem(create, 1, 0, true)
	var restore *tview.Button
	restore = actionButton("Restore backup", func() {
		if busy {
			return
		}
		directory, err := u.backup.DefaultDirectory()
		if err != nil {
			u.showError(err)
			return
		}
		u.chooseBackupFile(u.restoreStartDirectory(directory), restore, u.askRecoveryPhrase)
	})
	panel.AddItem(restore, 1, 0, false)
	backButton := actionButton("Return to Vault", func() {
		if !busy {
			u.navigate(routeServices)
		}
	})
	panel.AddItem(backButton, 1, 0, false)
	u.setFocusOrder(routeBackup, create, restore, backButton)
	return centered(72, 11, panel)
}

func (u *App) restoreStartDirectory(defaultDirectory string) string {
	if u.configPath == "" {
		return defaultDirectory
	}
	last, err := config.LastRestoreDirectory(u.configPath)
	if err != nil || last == "" || !filepath.IsAbs(last) {
		return defaultDirectory
	}
	info, err := os.Stat(last)
	if err != nil || !info.IsDir() {
		return defaultDirectory
	}
	return last
}

func (u *App) showBackupCreated(result service.BackupResult) {
	message := tview.NewTextView().SetText(
		"Backup created\n\n" + result.Path + "\n\nRecovery mnemonic — store it separately from the backup:\n\n" + result.Mnemonic,
	).SetWrap(true)
	message.SetBorder(true).SetTitle("Backup created")
	var save, closeButton *tview.Button
	finish := func() {
		if u.navigationLocked.Load() {
			return
		}
		u.pages.RemovePage("backup-created")
		// The backup dialog is the final modal in this flow. Clear the modal
		// state explicitly so a completed mnemonic save cannot leave global
		// navigation disabled by a stale nested directory-picker state.
		u.modalDepth.Store(0)
		u.modalFocus = nil
		u.navigationLocked.Store(false)
		u.focusFirst(routeBackup)
	}
	closeButton = actionButton("Finish", finish)
	save = actionButton("Save mnemonic to file", func() {
		directory := filepath.Dir(result.Path)
		u.chooseDirectory("Save recovery mnemonic", directory, save, func(selected string) {
			save.SetDisabled(true)
			closeButton.SetDisabled(true)
			var saved string
			u.beginProtectedTask(func(ctx context.Context) error {
				var saveErr error
				saved, saveErr = u.backup.SaveMnemonic(ctx, selected, result.MnemonicName, result.Mnemonic)
				return saveErr
			}, func(err error) {
				if err != nil {
					save.SetDisabled(false)
					closeButton.SetDisabled(false)
					u.showError(err)
					return
				}
				message.SetText("Backup created\n\n" + result.Path + "\n\nRecovery mnemonic saved to:\n\n" + saved + "\n\nKeep the mnemonic file separate from the backup.")
				save.SetDisabled(true)
				closeButton.SetDisabled(false)
				u.tview.SetFocus(closeButton)
			})
		}, func() { u.tview.SetFocus(save) })
	})
	buttons := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(save, 0, 1, false).
		AddItem(closeButton, 0, 1, false)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(message, 0, 1, false).
		AddItem(buttons, 1, 0, true)
	panel.SetBorder(true).SetBorderColor(tcell.ColorAqua)
	addModalCloseMark(panel.Box, finish)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if u.navigationLocked.Load() {
			return nil
		}
		switch event.Key() {
		case tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyUp, tcell.KeyDown:
			message.InputHandler()(event, func(tview.Primitive) {})
			return nil
		case tcell.KeyTab, tcell.KeyBacktab, tcell.KeyLeft, tcell.KeyRight:
			if u.tview.GetFocus() == save || save.IsDisabled() {
				u.tview.SetFocus(closeButton)
			} else {
				u.tview.SetFocus(save)
			}
			return nil
		case tcell.KeyEscape:
			finish()
			return nil
		}
		return event
	})
	u.pages.AddPage("backup-created", centered(84, 20, panel), true, true)
	u.pushModal()
	u.modalFocus = save
	u.tview.SetFocus(save)
}
