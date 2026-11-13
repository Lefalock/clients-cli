// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/domain"
)

func (p *servicesPage) editSecretValue() {
	if p.busy || p.loadError || p.ui.navigationLocked.Load() || p.ui.modalDepth.Load() > 0 {
		return
	}
	secret, _, err := p.selectedSecret()
	if err != nil {
		return
	}
	if secret.Type == domain.SecretNote {
		p.showNote(true)
		return
	}
	serviceID, secretID := secret.ServiceID, secret.ID
	if serviceID == "" {
		serviceID = p.serviceID
	}
	title, label := "Change password", "New password"
	if secret.Type == domain.SecretAuthCode {
		title, label = "Change TOTP key", "New Base32 key"
	}
	const pageName = "edit-secret-value"
	field := formField(label, true)
	styleModalInput(field)
	password := newPasswordFields(field)
	isPassword := secret.Type != domain.SecretAuthCode
	restore := p.ui.tview.GetFocus()
	p.maskPasswords()
	busy := false
	var save, show, showNew *tview.Button
	oldValue := tview.NewTextView().SetWrap(false)
	oldValue.SetBackgroundColor(commandTheme.Background)
	oldLabel := "Old password: "
	if secret.Type == domain.SecretAuthCode {
		oldLabel = "Old Base32 key: "
	}
	revealed := false
	refreshShow := func() {
		if show == nil {
			return
		}
		label := "Show"
		if revealed {
			label = "Hide"
		}
		label = tview.Escape("[" + label + "]")
		if show.HasFocus() {
			label = "> " + label + " <"
		}
		show.SetLabel(label)
	}
	hideOld := func() {
		revealed = false
		oldValue.SetText(oldLabel + "********")
		refreshShow()
	}
	hideOld()
	newRevealed := false
	refreshShowNew := func() {
		if showNew == nil {
			return
		}
		label := "Show"
		if newRevealed {
			label = "Hide"
		}
		label = tview.Escape("[" + label + "]")
		if showNew.HasFocus() {
			label = "> " + label + " <"
		}
		showNew.SetLabel(label)
	}
	hideNew := func() {
		newRevealed = false
		password.mask()
		refreshShowNew()
	}
	showNew = actionButton("Show", func() {
		if busy || p.ui.navigationLocked.Load() {
			return
		}
		newRevealed = !newRevealed
		if newRevealed {
			field.SetMaskCharacter(0)
			password.repeat.SetMaskCharacter(0)
		} else {
			password.mask()
		}
		refreshShowNew()
	})
	showNew.SetFocusFunc(refreshShowNew)
	showNew.SetBlurFunc(refreshShowNew)
	setBusy := func(value bool) {
		busy, p.busy = value, value
		password.disable(value)
		save.SetDisabled(value)
		show.SetDisabled(value)
		showNew.SetDisabled(value)
		p.updateActions()
	}
	close := func() {
		if busy || p.ui.navigationLocked.Load() {
			return
		}
		password.clear()
		p.ui.clearNote = nil
		hideOld()
		hideNew()
		p.ui.pages.RemovePage(pageName)
		p.ui.popModal()
		p.ui.tview.SetFocus(restore)
	}
	encryptedOld := secret.EncryptedPassword
	show = actionButton("Show", func() {
		if busy || p.ui.navigationLocked.Load() {
			return
		}
		if revealed {
			hideOld()
			return
		}
		setBusy(true)
		var plaintext string
		p.ui.beginProtectedTask(func(ctx context.Context) error {
			var err error
			plaintext, err = p.ui.credentials.RevealPassword(ctx, encryptedOld)
			return err
		}, func(err error) {
			setBusy(false)
			if err != nil {
				hideOld()
				p.ui.showError(err)
				return
			}
			oldValue.SetText(oldLabel + displayText(plaintext))
			plaintext = ""
			revealed = true
			refreshShow()
		})
	})
	show.SetFocusFunc(refreshShow)
	show.SetBlurFunc(refreshShow)
	submit := func() {
		if busy || p.ui.navigationLocked.Load() {
			return
		}
		if isPassword && !password.validate(p.ui) {
			return
		}
		value := field.GetText()
		hideOld()
		hideNew()
		setBusy(true)
		p.ui.beginProtectedTask(func(ctx context.Context) error {
			return p.ui.credentials.UpdateSecretValue(ctx, serviceID, secretID, value)
		}, func(err error) {
			setBusy(false)
			if err != nil {
				p.ui.showError(err)
				return
			}
			p.stopAuthCode()
			p.maskPasswords()
			close()
			p.returnFocus = restore
			p.load()
		})
	}
	password.bind(p.ui, func() bool { return !busy && isPassword }, hideNew)
	p.ui.clearNote = func() { password.clear(); hideOld(); hideNew() }
	save = actionButton("Save", submit)
	cancel := actionButton("Cancel", close)
	buttons := tview.NewFlex().AddItem(save, 0, 1, false).AddItem(cancel, 0, 1, false)
	buttons.SetBackgroundColor(commandTheme.Background)
	oldRow := tview.NewFlex().AddItem(oldValue, 0, 1, false).AddItem(show, 12, 0, false)
	oldRow.SetBackgroundColor(commandTheme.Background)
	newRow := tview.NewFlex().AddItem(field, 0, 1, true).AddItem(showNew, 12, 0, false)
	newRow.SetBackgroundColor(commandTheme.Background)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(oldRow, 1, 0, false).
		AddItem(newRow, 1, 0, true)
	if isPassword {
		panel.AddItem(password.repeat, 1, 0, false).AddItem(password.generate, 1, 0, false)
	}
	panel.AddItem(tview.NewBox().SetBackgroundColor(commandTheme.Background), 1, 0, false).
		AddItem(buttons, 1, 0, false)
	styleModalFrame(panel.Box, title, close)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if busy {
			return nil
		}
		switch event.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyEnter:
			if p.ui.tview.GetFocus() == field {
				if isPassword {
					p.ui.tview.SetFocus(password.repeat)
				} else {
					submit()
				}
				return nil
			}
			if isPassword && p.ui.tview.GetFocus() == password.repeat {
				submit()
				return nil
			}
		case tcell.KeyTab, tcell.KeyBacktab:
			controls := []tview.Primitive{field, showNew}
			if isPassword {
				controls = append(controls, password.repeat, password.generate)
			}
			controls = append(controls, show, save, cancel)
			step := 1
			if event.Key() == tcell.KeyBacktab {
				step = -1
			}
			p.ui.focusRelative(controls, focusIndex(controls, p.ui.tview.GetFocus()), step)
			return nil
		}
		return event
	})
	p.ui.pushModal()
	height := 6
	if isPassword {
		height = 8
	}
	p.ui.pages.AddPage(pageName, centered(64, height, panel), true, true)
	p.ui.tview.SetFocus(field)
}
