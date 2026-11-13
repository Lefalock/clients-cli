// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type command struct {
	label string
	hint  string
	run   func()
}

func (u *App) openCommandPalette() {
	if u.navigationLocked.Load() || u.modalDepth.Load() > 0 {
		return
	}
	restoreFocus := u.tview.GetFocus()
	const pageName = "command-palette"
	commands := []command{
		{label: "Open vault", hint: "Browse services and secrets", run: func() { u.navigate(routeServices) }},
		{label: "Add service", hint: "Add a service and its first secret", run: func() {
			u.createDirectoryID = ""
			if u.currentRoute == routeServices && u.services != nil {
				u.createDirectoryID = u.services.directoryID
			}
			u.navigate(routeCreateService)
		}},
		{label: "Create backup", hint: "Create an encrypted local backup", run: func() { u.navigate(routeBackup) }},
		{label: "Settings", hint: "Manage the local vault key", run: func() { u.navigate(routeSettings) }},
	}

	if u.shell.update != "" || u.shell.updateError != "" {
		commands = append(commands, command{label: "Update details", hint: "Version, error and update command", run: u.openUpdateDetails})
	}
	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorder(true).SetTitle(" Commands ").SetBorderColor(commandTheme.Focus)
	close := func() {
		u.pages.RemovePage(pageName)
		u.popModal()
		u.tview.SetFocus(restoreFocus)
	}
	addModalCloseMark(list.Box, close)
	for _, item := range commands {
		item := item
		list.AddItem(item.label, item.hint, 0, func() {
			u.pages.RemovePage(pageName)
			u.popModal()
			u.tview.SetFocus(restoreFocus)
			item.run()
		})
	}
	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape, tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyCtrlK:
			close()
			return nil
		}
		return event
	})
	u.pages.AddPage(pageName, centered(72, 18, list), true, true)
	u.pushModal()
	u.tview.SetFocus(list)
}
