// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (u *App) confirm(title, message string, accepted func()) {
	if u.navigationLocked.Load() || u.pages.HasPage("confirmation") {
		return
	}
	restore := u.tview.GetFocus()
	const pageName = "confirmation"
	close := func() {
		u.pages.RemovePage(pageName)
		u.popModal()
		u.tview.SetFocus(restore)
	}
	text := tview.NewTextView().SetText(message).SetWrap(true)
	text.SetBorder(true).SetTitle(" " + title + " ").SetBorderColor(commandTheme.Danger)
	confirmButton := actionButton(title, func() {
		close()
		accepted()
	})
	confirmButton.SetStyle(tcell.StyleDefault.Foreground(commandTheme.Danger).Background(commandTheme.Panel))
	confirmButton.SetActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(commandTheme.Danger))
	buttons := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(tview.NewBox(), 0, 1, false).
		AddItem(confirmButton, 0, 1, false).
		AddItem(tview.NewBox(), 0, 1, false)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(text, 0, 1, true).
		AddItem(buttons, 1, 0, false)
	panel.SetBorder(true).SetBorderColor(commandTheme.Danger)
	addModalCloseMark(panel.Box, close)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			close()
			return nil
		}
		switch event.Key() {
		case tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyUp, tcell.KeyDown:
			text.InputHandler()(event, func(tview.Primitive) {})
			return nil
		case tcell.KeyTab, tcell.KeyBacktab, tcell.KeyLeft, tcell.KeyRight:
			if u.tview.GetFocus() == text {
				u.tview.SetFocus(confirmButton)
			} else {
				u.tview.SetFocus(text)
			}
			return nil
		}
		return event
	})
	u.pages.AddPage(pageName, centered(70, 12, panel), true, true)
	u.pushModal()
	u.tview.SetFocus(text)
}
