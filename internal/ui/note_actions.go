// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"
	"errors"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/service"
)

type noteParent struct{ ServiceID, DirectoryID string }

func (p *servicesPage) noteService() NoteCredentialService {
	ns, _ := p.ui.credentials.(NoteCredentialService)
	return ns
}

func (p *servicesPage) noteParentTree(initial noteParent) *tview.TreeView {
	tree := p.folderTree("", initial.DirectoryID)
	tree.SetSelectedFunc(nil)
	nodes := map[string]*tview.TreeNode{}
	tree.GetRoot().Walk(func(node, parent *tview.TreeNode) bool {
		id := node.GetReference().(string)
		nodes[id] = node
		node.SetReference(noteParent{DirectoryID: id})
		return true
	})
	for _, item := range p.allItems {
		parent := nodes[item.DirectoryID]
		if parent == nil {
			continue
		}
		node := tview.NewTreeNode(vaultIconPrefix(serviceIcon) + tview.Escape(displayText(item.Name))).SetReference(noteParent{ServiceID: item.ID})
		parent.AddChild(node)
		if item.ID == initial.ServiceID {
			tree.SetCurrentNode(node)
		}
	}
	tree.SetBackgroundColor(commandTheme.Background)
	return tree
}

func (p *servicesPage) selectNoteParent(parent noteParent) {
	p.selectedDirectory = parent.ServiceID == ""
	p.serviceID, p.directoryID = parent.ServiceID, parent.DirectoryID
	p.selectedService = -1
	p.accountID, p.contentDirectoryID, p.contentServiceID = "", "", ""
	p.expanded[parent.DirectoryID] = true
	if parent.ServiceID != "" {
		for _, item := range p.allItems {
			if item.ID == parent.ServiceID {
				p.directoryID = item.DirectoryID
				p.expanded[item.DirectoryID] = true
			}
		}
	}
	if p.search.GetText() != "" {
		p.search.SetText("")
	}
}

func (p *servicesPage) createNoteIn(initial noteParent) {
	if p.noteService() == nil || p.busy || p.loadError || p.ui.modalDepth.Load() > 0 || p.ui.navigationLocked.Load() {
		return
	}
	const pageName = "create-note"
	tree := p.noteParentTree(initial)
	title := formField("Title", false)
	styleModalInput(title)
	text := newNoteTextArea()
	restore := p.ui.tview.GetFocus()
	busy := false
	var create, cancel *tview.Button
	clear := func() { title.SetText(""); text.clear() }
	close := func() {
		if busy || p.ui.navigationLocked.Load() {
			return
		}
		clear()
		p.ui.clearNote = nil
		p.ui.pages.RemovePage(pageName)
		p.ui.popModal()
		p.ui.tview.SetFocus(restore)
	}
	create = actionButton("Create note", func() {
		if busy || p.ui.navigationLocked.Load() {
			return
		}
		name := strings.TrimSpace(title.GetText())
		if name == "" {
			p.ui.showError(errors.New("note title is required"))
			return
		}
		node := tree.GetCurrentNode()
		if node == nil {
			return
		}
		parent := node.GetReference().(noteParent)
		value := text.GetText()
		busy, p.busy = true, true
		title.SetDisabled(true)
		text.SetDisabled(true)
		create.SetDisabled(true)
		cancel.SetDisabled(true)
		p.updateActions()
		p.ui.beginProtectedTask(func(ctx context.Context) error {
			return p.noteService().CreateNote(ctx, parent.ServiceID, parent.DirectoryID, name, value)
		}, func(err error) {
			busy, p.busy = false, false
			title.SetDisabled(false)
			text.SetDisabled(false)
			create.SetDisabled(false)
			cancel.SetDisabled(false)
			p.updateActions()
			if err != nil {
				p.ui.showError(err)
				return
			}
			close()
			p.selectNoteParent(parent)
			p.preferredNote, p.preferredNoteParent = name, parent
			p.accountsVisible = true
			p.returnFocus = p.accountsTable
			p.load()
		})
	})
	cancel = actionButton("Cancel", close)
	buttons := tview.NewFlex().AddItem(create, 0, 1, false).AddItem(cancel, 0, 1, false)
	buttons.SetBackgroundColor(commandTheme.Background)
	location := tview.NewTextView().SetText("Place in:")
	location.SetBackgroundColor(commandTheme.Background)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(title, 1, 0, true).AddItem(location, 1, 0, false).AddItem(tree, 6, 0, false).AddItem(text, 0, 1, false).AddItem(buttons, 1, 0, false)
	styleModalFrame(panel.Box, "New note", close)
	controls := []tview.Primitive{title, tree, text, create, cancel}
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if busy || p.ui.navigationLocked.Load() {
			return nil
		}
		switch event.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyCtrlS:
			create.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			if event.Modifiers()&tcell.ModAlt != 0 && text.HasFocus() {
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
	p.ui.clearNote = clear
	p.ui.pushModal()
	p.ui.pages.AddPage(pageName, centered(88, 28, panel), true, true)
	p.ui.tview.SetFocus(title)
}

func (p *servicesPage) moveNote(note *service.Secret) {
	if p.busy || p.ui.navigationLocked.Load() || p.ui.modalDepth.Load() > 0 {
		return
	}
	const pageName = "move-note"
	initial := noteParent{ServiceID: note.ServiceID}
	if note.ServiceID == "" {
		initial.DirectoryID = note.DirectoryID
	}
	tree := p.noteParentTree(initial)
	id := note.ID
	restore := p.ui.tview.GetFocus()
	close := func() { p.ui.pages.RemovePage(pageName); p.ui.popModal(); p.ui.tview.SetFocus(restore) }
	move := actionButton("Move", func() {
		node := tree.GetCurrentNode()
		if node == nil {
			return
		}
		parent := node.GetReference().(noteParent)
		close()
		p.mutate(func(ctx context.Context) error {
			return p.noteService().MoveNote(ctx, id, parent.ServiceID, parent.DirectoryID)
		}, func() {
			p.selectNoteParent(parent)
			key := parent.ServiceID
			if key == "" {
				key = "directory:" + parent.DirectoryID
			}
			p.accountSelections[key] = id
		})
	})
	cancel := actionButton("Cancel", close)
	buttons := tview.NewFlex().AddItem(move, 0, 1, false).AddItem(cancel, 0, 1, false)
	buttons.SetBackgroundColor(commandTheme.Background)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(tree, 0, 1, true).AddItem(buttons, 1, 0, false)
	styleModalFrame(panel.Box, "Move note", close)
	controls := []tview.Primitive{tree, move, cancel}
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
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

func (p *servicesPage) resolveCreatedNote(previous []service.Secret) {
	if p.preferredNote == "" {
		return
	}
	old := map[string]bool{}
	for _, n := range previous {
		old[n.ID] = true
	}
	notes := append([]service.Secret(nil), p.standaloneNotes...)
	for _, item := range p.allItems {
		notes = append(notes, item.Secrets...)
	}
	parent := p.preferredNoteParent
	for _, n := range notes {
		if n.Type == "note" && !old[n.ID] && n.Name == p.preferredNote && n.ServiceID == parent.ServiceID && (parent.ServiceID != "" || n.DirectoryID == parent.DirectoryID) {
			key := parent.ServiceID
			if key == "" {
				key = "directory:" + parent.DirectoryID
			}
			p.accountSelections[key] = n.ID
			break
		}
	}
	p.preferredNote = ""
}
func (p *servicesPage) noteMatchesSearch() bool {
	query := strings.ToLower(strings.TrimSpace(p.search.GetText()))
	for _, n := range p.standaloneNotes {
		if query != "" && strings.Contains(strings.ToLower(n.Name), query) {
			return true
		}
	}
	return false
}
func (p *servicesPage) findNoteSearchParent() {
	query := strings.ToLower(strings.TrimSpace(p.search.GetText()))
	if query == "" {
		return
	}
	for _, n := range p.standaloneNotes {
		if strings.Contains(strings.ToLower(n.Name), query) {
			p.selectedDirectory, p.directoryID, p.serviceID = true, n.DirectoryID, ""
			// Reveal the folder's ancestors so its row can be selected.
			for _, d := range p.directories {
				if p.isDirectoryDescendant(n.DirectoryID, d.ID) {
					p.expanded[d.ID] = true
				}
			}
			p.buildTreeRows()
			return
		}
	}
}
