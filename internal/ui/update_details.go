// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (u *App) openUpdateDetails() {
	if u.modalDepth.Load() > 0 || u.shell == nil {
		return
	}
	const name = "update-details"
	restore := u.tview.GetFocus()
	text := "Current version: " + u.shell.version
	if u.shell.update != "" {
		text += "\nUpdate available: " + u.shell.update
	}
	if u.shell.updateError != "" {
		text += "\nUpdate failed: " + u.shell.updateError
	}
	text += "\n\nRun in your shell:\nlefalock update"
	view := tview.NewTextView().SetText(text).SetWrap(true)
	view.SetBorder(true).SetTitle(" Update details ")
	close := func() { u.pages.RemovePage(name); u.popModal(); u.tview.SetFocus(restore) }
	addModalCloseMark(view.Box, close)
	button := actionButton("Close", close)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(view, 0, 1, false).AddItem(button, 1, 0, true)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			if view.HasFocus() {
				u.tview.SetFocus(button)
			} else {
				u.tview.SetFocus(view)
			}
			return nil
		}
		return event
	})
	u.pushModal()
	u.pages.AddPage(name, centered(72, 14, panel), true, true)
	u.tview.SetFocus(button)
}
