// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/domain"
)

func (p *servicesPage) directoryService() DirectoryCredentialService {
	service, _ := p.ui.credentials.(DirectoryCredentialService)
	return service
}

func (p *servicesPage) newFolder() {
	p.openCreateFolder()
}

func (p *servicesPage) newRootFolder() {
	p.openCreateFolder()
}

func (p *servicesPage) openCreateFolder() {
	if p.busy || p.loadError || p.directoryService() == nil {
		return
	}
	p.promptCreateFolder("")
}

func (p *servicesPage) promptCreateFolder(initialID string) {
	const pageName = "create-folder"
	if p.ui.modalDepth.Load() > 0 || p.ui.navigationLocked.Load() {
		return
	}
	field := formField("Name", false)
	styleModalInput(field)
	tree := p.folderTree("", initialID)
	tree.SetSelectedFunc(nil)
	tree.SetBackgroundColor(commandTheme.Background)
	destination := tview.NewTextView().SetWrap(false)
	destination.SetBackgroundColor(commandTheme.Background)
	updateDestination := func(node *tview.TreeNode) {
		if node != nil {
			destination.SetText("Create in: " + tview.Escape(displayText(p.directoryPath(node.GetReference().(string)))))
		}
	}
	tree.SetChangedFunc(updateDestination)
	updateDestination(tree.GetCurrentNode())
	restore := p.ui.tview.GetFocus()
	close := func() {
		p.ui.pages.RemovePage(pageName)
		p.ui.popModal()
		p.ui.tview.SetFocus(restore)
	}
	create := actionButton("Create", func() {
		name := strings.TrimSpace(field.GetText())
		if name == "" {
			p.ui.showError(fmt.Errorf("folder name is required"))
			return
		}
		node := tree.GetCurrentNode()
		if node == nil {
			return
		}
		parentID := node.GetReference().(string)
		close()
		p.mutate(func(ctx context.Context) error { return p.directoryService().CreateDirectory(ctx, parentID, name) }, func() {
			p.expanded[parentID] = true
			p.preferredDirectoryName, p.preferredDirectoryParent = name, parentID
		})
	})
	buttons := tview.NewFlex().AddItem(create, 0, 1, false)
	buttons.SetBackgroundColor(commandTheme.Background)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(field, 1, 0, true).
		AddItem(tree, 0, 1, false).
		AddItem(destination, 1, 0, false).
		AddItem(buttons, 1, 0, false)
	styleModalFrame(panel.Box, "New folder", close)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyEnter:
			if p.ui.tview.GetFocus() == field {
				create.InputHandler()(event, func(tview.Primitive) {})
				return nil
			}
		case tcell.KeyTab, tcell.KeyBacktab:
			controls := []tview.Primitive{field, tree, create}
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
	p.ui.pages.AddPage(pageName, centered(68, 20, panel), true, true)
	p.ui.tview.SetFocus(field)
}

func (p *servicesPage) promptFolderName(title, initial string, submit func(string)) {
	const pageName = "folder-name"
	field := formField("Name", false)
	field.SetText(initial)
	styleModalInput(field)
	restore := p.ui.tview.GetFocus()
	close := func() {
		p.ui.pages.RemovePage(pageName)
		p.ui.popModal()
		p.ui.tview.SetFocus(restore)
	}
	save := actionButton("Save", func() {
		name := strings.TrimSpace(field.GetText())
		if name == "" {
			p.ui.showError(fmt.Errorf("folder name is required"))
			return
		}
		close()
		submit(name)
	})
	buttons := tview.NewFlex().AddItem(save, 0, 1, false)
	buttons.SetBackgroundColor(commandTheme.Background)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(field, 1, 0, true).
		AddItem(buttons, 1, 0, false)
	styleModalFrame(panel.Box, title, close)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			close()
			return nil
		}
		if event.Key() == tcell.KeyEnter && p.ui.tview.GetFocus() == field {
			save.InputHandler()(event, func(tview.Primitive) {})
			return nil
		}
		return event
	})
	p.ui.pushModal()
	p.ui.pages.AddPage(pageName, centered(56, 4, panel), true, true)
	p.ui.tview.SetFocus(field)
}

func (p *servicesPage) chooseFolder(title, excludedID, initialID string, selected func(string)) {
	if p.ui.modalDepth.Load() > 0 || p.ui.navigationLocked.Load() {
		return
	}
	const pageName = "move-folder"
	restore := p.ui.tview.GetFocus()
	tree := p.folderTree(excludedID, initialID)
	current := tree.GetCurrentNode()
	destination := tview.NewTextView().SetWrap(false)
	updateDestination := func(node *tview.TreeNode) {
		if node != nil {
			destination.SetText("Destination: " + tview.Escape(displayText(p.directoryPath(node.GetReference().(string)))))
		}
	}
	tree.SetChangedFunc(updateDestination)
	updateDestination(current)
	close := func() {
		p.ui.pages.RemovePage(pageName)
		p.ui.popModal()
		p.ui.tview.SetFocus(restore)
	}
	move := actionButton("Move", func() {
		node := tree.GetCurrentNode()
		if node == nil {
			return
		}
		id := node.GetReference().(string)
		close()
		selected(id)
	})
	buttons := tview.NewFlex().AddItem(move, 0, 1, false)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tree, 0, 1, true).
		AddItem(destination, 1, 0, false).
		AddItem(buttons, 1, 0, false)
	panel.SetBorder(true).SetTitle(" " + title + " ").SetBorderColor(commandTheme.Border)
	addModalCloseMark(panel.Box, close)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			controls := []tview.Primitive{tree, move}
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
	p.ui.pages.AddPage(pageName, centered(68, 20, panel), true, true)
	p.ui.tview.SetFocus(tree)
}

func (p *servicesPage) folderTree(excludedID, initialID string) *tview.TreeView {
	root := tview.NewTreeNode("Root").SetReference("").SetExpanded(true)
	current := root
	children := make(map[string][]string, len(p.directories))
	names := make(map[string]string, len(p.directories))
	for _, folder := range p.directories {
		if excludedID != "" && p.isDirectoryDescendant(folder.ID, excludedID) {
			continue
		}
		names[folder.ID] = folder.Name
		children[folder.ParentID] = append(children[folder.ParentID], folder.ID)
	}
	seen := make(map[string]bool, len(names))
	var addChildren func(string, *tview.TreeNode)
	addChildren = func(parentID string, parent *tview.TreeNode) {
		for _, id := range children[parentID] {
			if seen[id] {
				continue
			}
			seen[id] = true
			label := tview.Escape(displayText(names[id]))
			node := tview.NewTreeNode(vaultIconPrefix(folderExpandedIcon) + label).SetReference(id).SetExpanded(true)
			parent.AddChild(node)
			if id == initialID {
				current = node
			}
			addChildren(id, node)
		}
	}
	addChildren("", root)
	tree := tview.NewTreeView().SetRoot(root).SetCurrentNode(current)
	tree.SetSelectedFunc(func(node *tview.TreeNode) {
		id := node.GetReference().(string)
		if id == "" {
			return
		}
		expanded := !node.IsExpanded()
		node.SetExpanded(expanded)
		icon := folderClosedIcon
		if expanded {
			icon = folderExpandedIcon
		}
		node.SetText(vaultIconPrefix(icon) + tview.Escape(displayText(names[id])))
	})
	return tree
}

func (p *servicesPage) isDirectoryDescendant(id, ancestorID string) bool {
	parents := make(map[string]string, len(p.directories))
	for _, folder := range p.directories {
		parents[folder.ID] = folder.ParentID
	}
	seen := make(map[string]bool)
	for id != "" && !seen[id] {
		if id == ancestorID {
			return true
		}
		seen[id] = true
		id = parents[id]
	}
	return false
}

func (p *servicesPage) renameFolder() {
	if !p.selectedDirectory || p.directoryID == "" || p.directoryService() == nil || p.busy || p.loadError {
		return
	}
	id := p.directoryID
	p.promptFolderName("Rename folder", p.directoryName(id), func(name string) {
		p.mutate(func(ctx context.Context) error { return p.directoryService().RenameDirectory(ctx, id, name) }, func() {})
	})
}

func (p *servicesPage) renameService() {
	if p.busy || p.loadError || p.selectedService < 0 {
		return
	}
	item := p.fullService()
	if item == nil {
		return
	}
	id := item.ID
	p.promptFolderName("Rename service", item.Name, func(name string) {
		p.mutate(func(ctx context.Context) error { return p.ui.credentials.RenameService(ctx, id, name) }, func() {})
	})
}

func (p *servicesPage) renameSecret() {
	if p.busy || p.loadError {
		return
	}
	account, _, err := p.selectedSecret()
	if err != nil {
		return
	}
	serviceID := account.ServiceID
	if serviceID == "" {
		serviceID = p.serviceID
	}
	id := account.ID
	title := "Rename secret"
	isNote := account.Type == domain.SecretNote
	if isNote {
		title = "Rename note"
	}
	p.promptFolderName(title, account.Name, func(name string) {
		p.mutate(func(ctx context.Context) error {
			if isNote && p.noteService() != nil {
				return p.noteService().RenameNote(ctx, id, name)
			}
			return p.ui.credentials.RenameSecret(ctx, serviceID, id, name)
		}, func() {})
	})
}

func (p *servicesPage) moveFolder() {
	if !p.selectedDirectory || p.directoryID == "" || p.directoryService() == nil || p.busy || p.loadError {
		return
	}
	id := p.directoryID
	parentID := ""
	for _, folder := range p.directories {
		if folder.ID == id {
			parentID = folder.ParentID
			break
		}
	}
	p.chooseFolder("Move folder", id, parentID, func(parentID string) {
		p.mutate(func(ctx context.Context) error { return p.directoryService().MoveDirectory(ctx, id, parentID) }, func() { p.expanded[parentID] = true })
	})
}

func (p *servicesPage) deleteFolder() {
	if !p.selectedDirectory || p.directoryID == "" || p.directoryService() == nil || p.busy || p.loadError {
		return
	}
	id := p.directoryID
	p.ui.confirm("Delete folder", "Delete empty folder "+displayText(p.directoryName(id))+"?", func() {
		p.mutate(func(ctx context.Context) error { return p.directoryService().DeleteDirectory(ctx, id) }, func() {
			p.selectedDirectory = true
			p.directoryID = ""
		})
	})
}

func (p *servicesPage) moveSelectedService() {
	if p.serviceID == "" || p.directoryService() == nil {
		return
	}
	id := p.serviceID
	initialID := ""
	if item := p.fullService(); item != nil {
		initialID = item.DirectoryID
	}
	p.chooseFolder("Move service", "", initialID, func(directoryID string) {
		p.mutate(func(ctx context.Context) error { return p.directoryService().MoveService(ctx, id, directoryID) }, func() { p.expanded[directoryID] = true })
	})
}

func (p *servicesPage) moveSelectedSecret() {
	account, _, err := p.selectedSecret()
	if err != nil {
		return
	}
	if account.Type == domain.SecretNote && p.noteService() != nil {
		p.moveNote(account)
		return
	}
	if p.directoryService() == nil {
		return
	}
	id := account.ID
	p.chooseFolder("Move secret", "", account.DirectoryID, func(directoryID string) {
		p.mutate(func(ctx context.Context) error { return p.directoryService().MoveSecret(ctx, id, directoryID) }, func() {})
	})
}
