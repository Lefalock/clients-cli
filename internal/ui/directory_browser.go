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

// chooseDirectory presents a filesystem-only picker so backups never need a
// platform-specific GUI integration. It permits selecting the displayed path.
func (u *App) chooseDirectory(title, initial string, restoreFocus tview.Primitive, selected func(string), cancelled func()) {
	const pageName = "directory-browser"
	path := filepath.Clean(initial)
	list := tview.NewList().ShowSecondaryText(false)
	list.SetBorder(true)
	pathView := tview.NewTextView().SetWrap(true)
	pathView.SetBorder(true).SetTitle(title)
	var refresh func() error
	refresh = func() error {
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("read directory %s: %w", path, err)
		}
		list.Clear()
		pathView.SetText(path)
		if parent := filepath.Dir(path); parent != path {
			list.AddItem("..", "Parent directory", 0, func() {
				previous := path
				path = parent
				if err := refresh(); err != nil {
					path = previous
					u.showError(err)
				}
			})
		}
		directories := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				directories = append(directories, entry.Name())
			}
		}
		sort.Strings(directories)
		for _, name := range directories {
			directory := name
			list.AddItem(tview.Escape(displayText(directory))+"/", "", 0, func() {
				previous := path
				path = filepath.Join(path, directory)
				if err := refresh(); err != nil {
					path = previous
					u.showError(err)
				}
			})
		}
		return nil
	}
	if err := refresh(); err != nil {
		u.showError(err)
		return
	}
	u.pushModal()
	cancel := func() {
		u.pages.RemovePage(pageName)
		u.popModal()
		u.modalFocus = restoreFocus
		cancelled()
	}
	choose := actionButton("Save here", func() {
		u.pages.RemovePage(pageName)
		u.popModal()
		u.modalFocus = restoreFocus
		u.tview.SetFocus(restoreFocus)
		selected(path)
	})
	newFolder := actionButton("New folder", func() {
		u.promptNewDirectory(path, list, refresh)
	})
	buttons := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(newFolder, 0, 1, false).
		AddItem(choose, 0, 1, false)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(pathView, 4, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(buttons, 1, 0, false)
	panel.SetBorder(true).SetBorderColor(tcell.ColorWhite)
	addModalCloseMark(panel.Box, cancel)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2 {
			cancel()
			return nil
		}
		if event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab {
			controls := []tview.Primitive{list, newFolder, choose, pathView}
			step := 1
			if event.Key() == tcell.KeyBacktab {
				step = -1
			}
			u.focusRelative(controls, focusIndex(controls, u.tview.GetFocus()), step)
			return nil
		}
		return event
	})
	u.pages.AddPage(pageName, centered(84, 24, panel), true, true)
	u.modalFocus = list
	u.tview.SetFocus(list)
}

func (u *App) promptNewDirectory(parent string, list *tview.List, refresh func() error) {
	const pageName = "new-directory"
	field := formField("Folder name", false).SetPlaceholder("New folder")
	styleModalInput(field)
	cancel := func() {
		u.pages.RemovePage(pageName)
		u.popModal()
		u.modalFocus = list
		u.tview.SetFocus(list)
	}
	create := actionButton("Create", func() {
		name := strings.TrimSpace(field.GetText())
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
			u.showError(fmt.Errorf("enter a valid folder name"))
			u.tview.SetFocus(field)
			return
		}
		if err := os.Mkdir(filepath.Join(parent, name), 0o700); err != nil {
			u.showError(fmt.Errorf("create folder: %w", err))
			u.tview.SetFocus(field)
			return
		}
		u.pages.RemovePage(pageName)
		u.popModal()
		if err := refresh(); err != nil {
			u.showError(err)
		}
		entries, _ := os.ReadDir(parent)
		index := 0
		if filepath.Dir(parent) != parent {
			index = 1
		}
		for _, entry := range entries {
			if entry.IsDir() {
				if entry.Name() == name {
					break
				}
				index++
			}
		}
		list.SetCurrentItem(index)
		u.modalFocus = list
		u.tview.SetFocus(list)
	})
	buttons := tview.NewFlex().AddItem(create, 0, 1, false)
	buttons.SetBackgroundColor(commandTheme.Background)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(field, 1, 0, true).
		AddItem(buttons, 1, 0, false)
	styleModalFrame(panel.Box, "Create folder", cancel)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			cancel()
			return nil
		}
		if event.Key() == tcell.KeyTab || event.Key() == tcell.KeyBacktab {
			step := 1
			if event.Key() == tcell.KeyBacktab {
				step = -1
			}
			controls := []tview.Primitive{field, create}
			u.focusRelative(controls, focusIndex(controls, u.tview.GetFocus()), step)
			return nil
		}
		return event
	})
	u.pushModal()
	u.pages.AddPage(pageName, centered(56, 4, panel), true, true)
	u.modalFocus = field
	u.tview.SetFocus(field)
}
