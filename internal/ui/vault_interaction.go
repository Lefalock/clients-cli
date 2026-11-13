// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/domain"
)

func (p *servicesPage) ownsActionFocus(value tview.Primitive) bool {
	return value == p.actionBar || focusIndex(p.actions, value) >= 0
}

func (p *servicesPage) actionGroup(value tview.Primitive) *vaultActionGroup {
	for _, group := range []*vaultActionGroup{p.serviceActions, p.folderActions, p.actionBar} {
		if group == nil {
			continue
		}
		if value == group {
			return group
		}
		for _, button := range group.buttons {
			if value == button {
				return group
			}
		}
	}
	return nil
}

func (p *servicesPage) selectFirstTableRow(focus tview.Primitive) {
	switch focus {
	case p.servicesTable:
		if len(p.items) > 0 {
			p.servicesTable.Select(0, 0)
		}
	case p.accountsTable:
		if len(p.contentRows) > 0 {
			row := 0
			if p.contentRows[0].kind == contentParent && len(p.contentRows) > 1 {
				row = 1
			}
			p.accountsTable.Select(row, 0)
		}
	}
}

func (p *servicesPage) openContentRow(row int) {
	if row < 0 || row >= len(p.contentRows) {
		return
	}
	entry := p.contentRows[row]
	if entry.kind == contentSecret {
		p.openDetails()
		return
	}
	targetDirectory := entry.id
	if entry.kind == contentService {
		targetDirectory = p.directoryID
		p.expanded[targetDirectory] = true
	}
	parents := make(map[string]string, len(p.directories))
	for _, folder := range p.directories {
		parents[folder.ID] = folder.ParentID
	}
	seen := make(map[string]bool)
	for id := targetDirectory; id != "" && !seen[id]; id = parents[id] {
		seen[id] = true
		parent := parents[id]
		if parent == "" {
			break
		}
		p.expanded[parent] = true
	}
	if p.search.GetText() != "" {
		p.rendering = true
		p.search.SetText("")
		p.rendering = false
	}
	p.contentDirectoryID, p.contentServiceID = "", ""
	if entry.kind == contentParent || entry.kind == contentDirectory {
		p.selectedDirectory, p.directoryID, p.serviceID = true, entry.id, ""
	} else {
		p.selectedDirectory, p.serviceID = false, entry.id
	}
	p.applyFilter()
	p.accountsVisible = true
	p.ui.tview.SetFocus(p.accountsTable)
}

func (p *servicesPage) focusOrder() []tview.Primitive {
	if p.loadError {
		order := []tview.Primitive{p.retry}
		if p.search.GetText() != "" {
			order = append(order, p.clearSearch)
		}
		return append(order, p.search)
	}
	order := []tview.Primitive{p.servicesTable, p.accountsTable}
	if p.directoryService() != nil && p.folderActions.available() {
		order = append(order, p.folderActions)
	}
	if p.serviceActions.available() {
		order = append(order, p.serviceActions)
	}
	if p.actionBar.available() {
		order = append(order, p.actionBar)
	}
	if p.compact {
		order = append(order, p.backServices)
	}
	if p.search.GetText() != "" {
		order = append(order, p.clearSearch)
	}
	return append(order, p.search)
}

func (p *servicesPage) captureKeyboard(event *tcell.EventKey) *tcell.EventKey {
	if p.small {
		return nil
	}
	focus := p.ui.tview.GetFocus()
	switch event.Key() {
	case tcell.KeyDelete:
		group := p.actionGroup(focus)
		var button *tview.Button
		switch {
		case focus == p.accountsTable || group == p.actionBar:
			button = p.deleteSecretButton
		case group == p.folderActions || (focus == p.servicesTable && p.selectedDirectory):
			button = p.folderButtons[3]
		case focus == p.servicesTable || group == p.serviceActions:
			button = p.deleteServiceButton
		}
		if button != nil {
			if !button.IsDisabled() {
				button.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(next tview.Primitive) { p.ui.tview.SetFocus(next) })
			}
			return nil
		}
	case tcell.KeyTab, tcell.KeyBacktab, tcell.KeyLeft, tcell.KeyRight:
		if focus == p.servicesTable && (event.Key() == tcell.KeyLeft || event.Key() == tcell.KeyRight) {
			row, _ := p.servicesTable.GetSelection()
			if row >= 0 && row < len(p.treeRows) && p.treeRows[row].directory {
				id := p.treeRows[row].directoryID
				if event.Key() == tcell.KeyRight && !p.expanded[id] || event.Key() == tcell.KeyLeft && p.expanded[id] {
					p.toggleDirectory(row)
					return nil
				}
			}
		}
		if (event.Key() == tcell.KeyLeft || event.Key() == tcell.KeyRight) && p.actionGroup(focus) != nil {
			group := p.actionGroup(focus)
			if button, ok := focus.(*tview.Button); ok {
				group.selectButton(button)
				p.ui.tview.SetFocus(group)
			}
			group.refreshSelection()
			row := group.selectedRow()
			step, entryColumn := 1, 0
			if event.Key() == tcell.KeyLeft {
				step, entryColumn = -1, 1
			}
			if group.moveHorizontal(step) {
				return nil
			}
			order := p.focusOrder()
			p.ui.focusRelative(order, focusIndex(order, group), step)
			if next := p.ui.tview.GetFocus(); next != group {
				if nextGroup := p.actionGroup(next); nextGroup != nil {
					nextGroup.selectEntry(row, entryColumn)
				} else {
					p.selectFirstTableRow(next)
				}
			}
			return nil
		}
		order := p.focusOrder()
		current := focus
		if group := p.actionGroup(focus); group != nil {
			current = group
		}
		index := focusIndex(order, current)
		if index < 0 {
			index = 0
		}
		step := 1
		if event.Key() == tcell.KeyBacktab || event.Key() == tcell.KeyLeft {
			step = -1
		}
		p.ui.focusRelative(order, index, step)
		if next := p.ui.tview.GetFocus(); next != focus {
			p.selectFirstTableRow(next)
		}
		return nil
	case tcell.KeyUp, tcell.KeyDown:
		if group := p.actionGroup(focus); group != nil {
			if button, ok := focus.(*tview.Button); ok {
				group.selectButton(button)
				p.ui.tview.SetFocus(group)
			}
			step := 1
			if event.Key() == tcell.KeyUp {
				step = -1
			}
			group.moveVertical(step)
			return nil
		}
	case tcell.KeyEnter:
		if focus == p.search {
			if len(p.items) > 0 {
				p.selectFirstTableRow(p.servicesTable)
				p.ui.tview.SetFocus(p.servicesTable)
			} else if p.search.GetText() != "" {
				p.ui.tview.SetFocus(p.clearSearch)
			} else {
				p.ui.tview.SetFocus(p.serviceActions)
			}
			return nil
		}
	case tcell.KeyEscape:
		p.maskPasswords()
		if focus == p.search && p.search.GetText() != "" {
			p.search.SetText("")
		} else {
			p.accountsVisible = false
			p.selectFirstTableRow(p.servicesTable)
			p.ui.tview.SetFocus(p.servicesTable)
		}
		return nil
	case tcell.KeyRune:
		if _, editing := focus.(*tview.InputField); editing {
			return event
		}
		switch event.Rune() {
		case '/':
			p.ui.tview.SetFocus(p.search)
			return nil
		case 'c':
			if focus == p.accountsTable || p.ownsActionFocus(focus) {
				p.copyPassword()
			}
			return nil
		}
	}
	return event
}

func (p *servicesPage) updateFooter() {
	if p.ui.shell == nil {
		return
	}
	text := "Tab Next · / Search · F6 Sections · Ctrl+K Commands"
	switch {
	case p.search.HasFocus():
		text = "Enter Results · Esc Clear · Tab Next · Ctrl+K Menu"
	case p.servicesTable.HasFocus():
		text = "Enter Open folder / secrets · Tab Next · F6 Sections · Ctrl+K Menu"
	case p.accountsTable.HasFocus():
		if p.selectedDirectory {
			text = "Enter Open folder/service or secret details · Esc Back · F6 · Ctrl+K Menu"
		} else {
			text = "Enter Details · c Copy · Esc Back · F6 · Ctrl+K Menu"
		}
	}
	p.ui.shell.footer.SetText(text)
}

func (p *servicesPage) openDetails() { p.showDetails(false) }

func (p *servicesPage) showDetails(reveal bool) {
	if p.busy || p.loadError || p.modalDetails != nil {
		return
	}
	secret, _, err := p.selectedSecret()
	if err != nil {
		return
	}
	if secret.Type == domain.SecretNote {
		p.showNote(false)
		return
	}
	p.stopAuthCode()
	const name = "secret-details"
	restore := p.ui.tview.GetFocus()
	p.ui.pushModal()
	p.modalDetails = tview.NewTextView().SetWrap(true).SetDynamicColors(secret.Type == domain.SecretAuthCode)
	p.refreshDetails()
	close := func() {
		p.stopAuthCode()
		p.maskPasswords()
		p.modalDetails = nil
		p.detailButtons = nil
		p.closeDetails = nil
		p.ui.pages.RemovePage(name)
		p.ui.popModal()
		p.ui.tview.SetFocus(restore)
	}
	p.closeDetails = close
	styleModalFrame(p.modalDetails.Box, "Secret details", close)
	copyLabel, revealLabel := "Copy password", "Show password (15s)"
	copyAction := p.copyPassword
	if secret.Type == domain.SecretAuthCode {
		copyLabel, revealLabel = "Copy code", "Show key (15s)"
		copyAction = p.copyAuthCode
	}
	copyButton := actionButton(copyLabel, copyAction)
	revealButton := actionButton(revealLabel, p.showPassword)
	p.detailButtons = []*tview.Button{copyButton, revealButton}
	controls := []tview.Primitive{copyButton, revealButton, p.modalDetails}
	p.modalDetails.SetBlurFunc(p.maskPasswords)
	for _, button := range p.detailButtons {
		p.maskButtonOnBlur(button)
	}
	buttons := tview.NewFlex().AddItem(copyButton, 0, 1, false).AddItem(revealButton, 0, 1, false)
	hint := tview.NewTextView().SetText("Show: PgUp/PgDn scroll · Tab hides").SetWrap(false)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(p.modalDetails, 0, 1, false).AddItem(buttons, 1, 0, true).AddItem(hint, 1, 0, false)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyPgUp, tcell.KeyPgDn:
			if p.ui.tview.GetFocus() == revealButton {
				p.modalDetails.InputHandler()(event, func(tview.Primitive) {})
				return nil
			}
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyTab, tcell.KeyRight, tcell.KeyBacktab, tcell.KeyLeft:
			index := focusIndex(controls, p.ui.tview.GetFocus())
			step := 1
			if event.Key() == tcell.KeyBacktab || event.Key() == tcell.KeyLeft {
				step = -1
			}
			p.ui.focusRelative(controls, index, step)
			return nil
		case tcell.KeyRune:
			if event.Rune() == 'c' {
				copyAction()
				return nil
			}
		}
		return event
	})
	p.ui.pages.AddPage(name, centered(72, 16, panel), true, true)
	if secret.Type == domain.SecretAuthCode {
		p.startAuthCode(*secret)
	}
	p.ui.tview.SetFocus(copyButton)
	if reveal {
		p.ui.tview.SetFocus(revealButton)
		p.showPassword()
	}
}

func (p *servicesPage) maskButtonOnBlur(button *tview.Button) {
	normal := button.GetLabel()
	button.SetBlurFunc(func() { button.SetLabel(normal); p.maskPasswords() })
}
