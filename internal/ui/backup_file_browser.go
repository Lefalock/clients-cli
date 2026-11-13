// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (u *App) chooseBackupFile(initial string, restoreFocus tview.Primitive, selected func(string)) {
	const pageName = "backup-file-browser"
	closePicker := func() {
		u.pages.RemovePage(pageName)
		u.popModal()
		u.modalFocus = restoreFocus
		u.tview.SetFocus(restoreFocus)
	}
	panel, list, err := buildBackupFileBrowser(initial, selected, closePicker, u.showError)
	if err != nil {
		u.showError(err)
		return
	}
	u.pushModal()
	u.pages.AddPage(pageName, centered(84, 25, panel), true, true)
	u.modalFocus = list
	u.tview.SetFocus(list)
}

// buildBackupFileBrowser is shared by normal restore and startup recovery.
func buildBackupFileBrowser(initial string, selected func(string), cancel func(), onError func(error)) (tview.Primitive, tview.Primitive, error) {
	path := filepath.Clean(initial)
	list := tview.NewList().ShowSecondaryText(false)
	list.SetBorder(true).SetTitle(" Select .lbcp file ")
	pathView := tview.NewTextView().SetWrap(true).SetText(path)
	closePicker := cancel
	var refresh func() error
	refresh = func() error {
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("read backup directory: %w", err)
		}
		list.Clear()
		pathView.SetText(path)
		if parent := filepath.Dir(path); parent != path {
			list.AddItem("../", "", 0, func() {
				previous := path
				path = parent
				if err := refresh(); err != nil {
					path = previous
					onError(err)
				}
			})
		}
		var folders, files []string
		for _, entry := range entries {
			if entry.IsDir() {
				folders = append(folders, entry.Name())
			} else if strings.HasSuffix(strings.ToLower(entry.Name()), ".lbcp") {
				files = append(files, entry.Name())
			}
		}
		sort.Strings(folders)
		sort.Strings(files)
		for _, name := range folders {
			name := name
			list.AddItem(tview.Escape(displayText(name))+"/", "", 0, func() {
				previous := path
				path = filepath.Join(path, name)
				if err := refresh(); err != nil {
					path = previous
					onError(err)
				}
			})
		}
		for _, name := range files {
			name := name
			list.AddItem(tview.Escape(displayText(name)), "", 0, func() {
				closePicker()
				selected(filepath.Join(path, name))
			})
		}
		return nil
	}
	if err := refresh(); err != nil {
		return nil, nil, err
	}
	panel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(pathView, 3, 0, false).
		AddItem(list, 0, 1, true)
	panel.SetBorder(true).SetBorderColor(commandTheme.Border)
	addModalCloseMark(panel.Box, closePicker)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			closePicker()
			return nil
		}
		return event
	})
	return panel, list, nil
}
