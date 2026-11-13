// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/config"
	"lefalock/internal/service"
)

const restoreModal = "restore-dialog"

func (u *App) openRestoreDialog(title string, body *tview.Flex, controls []tview.Primitive, first tview.Primitive, cancel func()) {
	u.openRestoreDialogDynamic(title, body, func() []tview.Primitive { return controls }, first, cancel)
}

func (u *App) openRestoreDialogDynamic(title string, body *tview.Flex, controls func() []tview.Primitive, first tview.Primitive, cancel func()) {
	body.SetBorder(true).SetTitle(" " + title + " ").SetBorderColor(commandTheme.Border)
	addModalCloseMark(body.Box, func() {
		if u.navigationLocked.Load() {
			return
		}
		if dropdown, ok := u.tview.GetFocus().(*tview.DropDown); ok && dropdown.IsOpen() {
			return
		}
		cancel()
	})
	body.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if u.navigationLocked.Load() {
			return nil
		}
		if dropdown, ok := u.tview.GetFocus().(*tview.DropDown); ok && dropdown.IsOpen() {
			return event
		}
		if event.Key() == tcell.KeyEscape {
			cancel()
			return nil
		}
		if event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab {
			step := 1
			if event.Key() == tcell.KeyBacktab {
				step = -1
			}
			active := controls()
			u.focusRelative(active, focusIndex(active, u.tview.GetFocus()), step)
			return nil
		}
		return event
	})
	u.pushModal()
	u.pages.AddPage(restoreModal, centered(84, 18, body), true, true)
	u.modalFocus = first
	u.tview.SetFocus(first)
}

func (u *App) closeRestoreDialog() {
	u.pages.RemovePage(restoreModal)
	u.popModal()
	u.modalFocus = nil
	u.focusFirst(routeBackup)
}

func (u *App) askRecoveryPhrase(path string) {
	phrase := formField("Recovery phrase", true)
	message := tview.NewTextView().SetText("File: " + tview.Escape(displayText(path)) + "\nEnter the recovery words saved when this backup was created.").SetWrap(true)
	busy := false
	var preview *service.RestorePreview
	cancel := func() {
		if !busy {
			phrase.SetText("")
			u.closeRestoreDialog()
		}
	}
	open := actionButton("Inspect backup", func() {
		if busy {
			return
		}
		words := phrase.GetText()
		phrase.SetText("")
		busy = true
		u.beginProtectedTask(func(ctx context.Context) error {
			var err error
			preview, err = u.backup.PreviewRestore(ctx, path, words)
			return err
		}, func(err error) {
			busy = false
			if err != nil {
				u.showError(err)
				return
			}
			u.closeRestoreDialog()
			u.restoreUndoPreview = nil
			u.restoreUndoDecisions = nil
			u.showRestoreConflicts(preview, make(map[string]service.RestoreDecision))
		})
	})
	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(message, 0, 1, false).
		AddItem(phrase, 1, 0, true).
		AddItem(open, 1, 0, false)
	u.openRestoreDialog("Unlock backup", body, []tview.Primitive{phrase, open}, phrase, cancel)
}

func (u *App) showRestoreSummary(preview *service.RestorePreview, decisions map[string]service.RestoreDecision) {
	if restoreUnresolvedConflicts(preview, decisions) > 0 {
		u.showRestoreConflicts(preview, decisions)
		return
	}
	summary, err := u.backup.RestoreSummaryFor(preview, decisions)
	if err != nil {
		u.showError(err)
		return
	}
	message := fmt.Sprintf("Review restore\n\nFolders to add: %d\nServices to add: %d\nSecrets to add: %d\nPasswords to replace: %d\nCurrent passwords to keep: %d\n\nOther local secrets will remain.",
		summary.DirectoriesAdded, summary.ServicesAdded, summary.SecretsAdded, summary.SecretsReplaced, summary.SecretsKept)
	if summary.DirectoriesAdded > 0 {
		message += "\nFolders from this backup will be imported as a separate tree."
	}
	for _, renamed := range summary.RenamedImports {
		message += "\nImport " + tview.Escape(displayText(renamed.From)) + " as " + tview.Escape(displayText(renamed.To))
	}
	info := tview.NewTextView().SetText(message).SetWrap(true)
	busy := false
	cancel := func() {
		if !busy {
			u.restoreUndoPreview = nil
			u.restoreUndoDecisions = nil
			u.closeRestoreDialog()
		}
	}
	apply := actionButton("Apply restore", func() {
		if busy {
			return
		}
		if restoreUnresolvedConflicts(preview, decisions) > 0 {
			u.closeRestoreDialog()
			u.showRestoreConflicts(preview, decisions)
			return
		}
		busy = true
		u.beginProtectedTask(func(ctx context.Context) error {
			_, err := u.backup.ApplyRestore(ctx, preview, decisions)
			return err
		}, func(err error) {
			busy = false
			if err != nil {
				u.showError(err)
				return
			}
			var rememberWarning string
			if preview.SourcePath != "" && u.configPath != "" {
				if err := config.RememberRestoreDirectory(u.configPath, filepath.Dir(preview.SourcePath)); err != nil {
					rememberWarning = fmt.Sprintf("\n\nCould not remember the backup folder: %v", err)
				}
			}
			u.closeRestoreDialog()
			u.restoreUndoPreview = nil
			u.restoreUndoDecisions = nil
			u.navigate(routeServices)
			u.showRestoreComplete(summary, rememberWarning)
		})
	})
	back := actionButton("Review conflicts", func() {
		u.closeRestoreDialog()
		if len(preview.Conflicts) > 0 {
			u.showRestoreConflicts(preview, decisions)
		} else {
			u.showRestoreSummary(preview, decisions)
		}
	})
	buttons := tview.NewFlex().AddItem(apply, 0, 1, false).AddItem(back, 0, 1, false)
	if len(preview.Conflicts) == 0 {
		back.SetDisabled(true)
	}
	body := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(info, 0, 1, false).AddItem(buttons, 1, 0, true)
	u.openRestoreDialog("Confirm restore", body, []tview.Primitive{info, apply, back}, apply, cancel)
}

func (u *App) showRestoreComplete(summary service.RestoreSummary, warning string) {
	message := fmt.Sprintf("Restore completed.\n\nAdded %d folders, %d services and %d secrets. Replaced %d passwords. Kept %d current passwords.",
		summary.DirectoriesAdded, summary.ServicesAdded, summary.SecretsAdded, summary.SecretsReplaced, summary.SecretsKept)
	for _, renamed := range summary.RenamedImports {
		message += "\nImported " + tview.Escape(displayText(renamed.From)) + " as " + tview.Escape(displayText(renamed.To))
	}
	message += tview.Escape(warning)
	info := tview.NewTextView().SetText(message).SetWrap(true)
	closeDialog := func() {
		u.pages.RemovePage(restoreModal)
		u.popModal()
		u.modalFocus = nil
		u.focusFirst(routeServices)
	}
	closeButton := actionButton("Close", closeDialog)
	body := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(info, 0, 1, false).AddItem(closeButton, 1, 0, true)
	u.openRestoreDialog("Restore complete", body, []tview.Primitive{info, closeButton}, closeButton, closeDialog)
}
