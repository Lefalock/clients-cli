// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/domain"
)

// secretValueInput keeps the single-line and multiline editors separate.
type secretValueInput struct {
	*tview.Flex
	input    *tview.InputField
	note     *noteTextArea
	typ      domain.SecretType
	password *passwordFields
}

type noteTextArea struct {
	*tview.TextArea
	clipboard string
}

func (a *noteTextArea) clear() { a.SetText("", false); a.clipboard = "" }

func (a *noteTextArea) PasteHandler() func(string, func(tview.Primitive)) {
	return func(text string, focus func(tview.Primitive)) {
		if !a.GetDisabled() {
			a.TextArea.PasteHandler()(text, focus)
		}
	}
}

func newNoteTextArea() *noteTextArea {
	area := tview.NewTextArea().SetLabel("Note text: ").SetWrap(true)
	area.SetBackgroundColor(commandTheme.Background)
	area.SetTextStyle(tcell.StyleDefault.Foreground(commandTheme.Text).Background(commandTheme.Background))
	area.SetLabelStyle(tcell.StyleDefault.Foreground(commandTheme.Text).Background(commandTheme.Background))
	// The parent handles Tab as focus navigation; Alt+Tab inserts a literal tab.
	area.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab && event.Modifiers()&tcell.ModAlt != 0 {
			return tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
		}
		return event
	})
	result := &noteTextArea{TextArea: area}
	area.SetClipboard(func(text string) { result.clipboard = text }, func() string { return result.clipboard })
	return result
}

func newSecretValueInput() *secretValueInput {
	field := &secretValueInput{Flex: tview.NewFlex(), input: formField("Password", true), note: newNoteTextArea()}
	field.SetBackgroundColor(commandTheme.Background)
	styleModalInput(field.input)
	field.password = newPasswordFields(field.input)
	field.password.addViewButton()
	field.selectType(domain.SecretPassword)
	return field
}

func (f *secretValueInput) clear() { f.password.clear(); f.note.clear() }
func (f *secretValueInput) selectType(typ domain.SecretType) {
	f.clear()
	f.typ = typ
	f.Clear()
	if typ == domain.SecretNote {
		f.SetDirection(tview.FlexRow)
		f.AddItem(f.note, 0, 1, true)
		hint := tview.NewTextView().SetText("Use buttons below to create or cancel.").SetWrap(true)
		hint.SetBackgroundColor(commandTheme.Background)
		f.AddItem(hint, 1, 0, false)
	} else {
		label := "Password: "
		if typ == domain.SecretAuthCode {
			label = "Base32 key: "
		}
		f.input.SetLabel(label)
		if typ == domain.SecretPassword {
			f.SetDirection(tview.FlexRow)
			fields := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(f.input, 1, 0, true).AddItem(f.password.repeat, 1, 0, false)
			fields.SetBackgroundColor(commandTheme.Background)
			spacer := tview.NewBox().SetBackgroundColor(commandTheme.Background)
			row := tview.NewFlex().AddItem(fields, 0, 1, true).AddItem(spacer, 2, 0, false).AddItem(f.password.view, 12, 0, false)
			row.SetBackgroundColor(commandTheme.Background)
			f.AddItem(row, 2, 0, true).AddItem(f.password.generate, 1, 0, false)
		} else {
			f.SetDirection(tview.FlexColumn)
			f.AddItem(f.input, 0, 1, true)
		}
	}
}
func (f *secretValueInput) text() string {
	if f.typ == domain.SecretNote {
		return f.note.GetText()
	}
	return f.input.GetText()
}
func (f *secretValueInput) disable(busy bool) {
	f.password.disable(busy)
	f.note.SetDisabled(busy)
}

// TextArea.PasteHandler does not check SetDisabled itself.
func (f *secretValueInput) PasteHandler() func(string, func(tview.Primitive)) {
	return func(text string, focus func(tview.Primitive)) {
		if f.typ == domain.SecretNote && !f.note.GetDisabled() {
			f.note.PasteHandler()(text, focus)
		}
		if f.typ != domain.SecretNote {
			f.input.PasteHandler()(text, focus)
		}
	}
}

func noteDisplay(text string) string {
	var out strings.Builder
	for _, r := range text {
		if (r < 32 || r >= 127 && r < 160) && r != '\n' && r != '\r' && r != '\t' {
			escaped := strconv.QuoteRune(r)
			out.WriteString(escaped[1 : len(escaped)-1])
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func (p *servicesPage) showNote(edit bool) {
	secret, _, err := p.selectedSecret()
	if err != nil || p.busy || p.ui.navigationLocked.Load() || p.ui.modalDepth.Load() > 0 {
		return
	}
	serviceID, secretID, encrypted := secret.ServiceID, secret.ID, secret.EncryptedPassword
	if serviceID == "" {
		serviceID = p.serviceID
	}
	restore := p.ui.tview.GetFocus()
	name, title := "note-details", "Note: "+displayText(secret.Name)
	if edit {
		name, title = "edit-note", "Edit note: "+displayText(secret.Name)
	}
	area := newNoteTextArea()
	view := tview.NewTextView().SetWrap(true).SetDynamicColors(false)
	view.SetBackgroundColor(commandTheme.Background)
	var content tview.Primitive = view
	if edit {
		content = area
	}
	busy := false
	var save, copyButton, cancel, editButton *tview.Button
	setBusy := func(value bool) {
		busy, p.busy = value, value
		area.SetDisabled(value)
		if save != nil {
			save.SetDisabled(value)
		}
		if copyButton != nil {
			copyButton.SetDisabled(value)
		}
		if cancel != nil {
			cancel.SetDisabled(value)
		}
		if editButton != nil {
			editButton.SetDisabled(value)
		}
		p.updateActions()
	}
	clear := func() { area.clear(); view.SetText("") }
	close := func() {
		if busy || p.ui.navigationLocked.Load() {
			return
		}
		clear()
		p.ui.clearNote = nil
		p.closeDetails = nil
		p.ui.pages.RemovePage(name)
		p.ui.popModal()
		p.ui.tview.SetFocus(restore)
	}
	buttons := tview.NewFlex()
	buttons.SetBackgroundColor(commandTheme.Background)
	controls := []tview.Primitive{content}
	submit := func() {
		if busy || p.ui.navigationLocked.Load() {
			return
		}
		value := area.GetText()
		setBusy(true)
		p.ui.beginProtectedTask(func(ctx context.Context) error {
			if ns := p.noteService(); ns != nil {
				return ns.UpdateNote(ctx, secretID, value)
			}
			return p.ui.credentials.UpdateSecretValue(ctx, serviceID, secretID, value)
		}, func(err error) {
			setBusy(false)
			if err != nil {
				p.ui.showError(err)
				return
			}
			close()
			p.returnFocus = restore
			p.load()
		})
	}
	if edit {
		cancel = actionButton("Cancel", close)
		save = actionButton("Save", submit)
		buttons.AddItem(save, 0, 1, false).AddItem(cancel, 0, 1, false)
		controls = append(controls, save, cancel)
	} else {
		copyButton = actionButton("Copy note", p.copyPassword)
		editButton = actionButton("Edit", func() {
			if busy || p.busy || p.ui.navigationLocked.Load() {
				return
			}
			close()
			p.showNote(true)
		})
		buttons.AddItem(copyButton, 0, 1, false).AddItem(editButton, 0, 1, false)
		controls = append(controls, copyButton, editButton)
	}
	hint := "Scroll with the mouse wheel. Use Copy note or Edit. Close with X or Esc."
	if edit {
		hint = "Click in the text to position the cursor. Use Save or Cancel."
	}
	hintView := tview.NewTextView().SetText(hint).SetWrap(true)
	hintView.SetBackgroundColor(commandTheme.Background)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(content, 0, 1, true).AddItem(buttons, 1, 0, false).AddItem(hintView, 2, 0, false)
	styleModalFrame(panel.Box, tview.Escape(title), close)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if busy || p.ui.navigationLocked.Load() {
			return nil
		}
		switch event.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyCtrlS:
			if edit {
				submit()
				return nil
			}
		case tcell.KeyTab, tcell.KeyBacktab:
			if edit && event.Modifiers()&tcell.ModAlt != 0 {
				return event
			}
			step := 1
			if event.Key() == tcell.KeyBacktab {
				step = -1
			}
			p.ui.focusRelative(controls, focusIndex(controls, p.ui.tview.GetFocus()), step)
			return nil
		}
		return event
	})

	p.stopAuthCode()
	p.maskPasswords()
	p.ui.clearNote = clear
	p.closeDetails = close
	p.ui.pushModal()
	p.ui.pages.AddPage(name, centered(88, 24, panel), true, true)
	p.ui.tview.SetFocus(content)
	setBusy(true)
	p.ui.beginProtectedTask(func(ctx context.Context) error {
		var err error
		encrypted, err = p.ui.credentials.RevealPassword(ctx, encrypted)
		return err
	}, func(err error) {
		setBusy(false)
		if err != nil {
			close()
			p.ui.showError(err)
			return
		}
		if edit {
			area.SetText(encrypted, false)
		} else {
			view.SetText(noteDisplay(encrypted))
		}
		encrypted = ""
	})
}

func isNoteTextArea(p tview.Primitive) bool {
	switch p.(type) {
	case *noteTextArea, *tview.TextArea:
		return true
	}
	return false
}
