// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/domain"
)

type creationTreePicker struct {
	tree        *tview.TreeView
	destination *tview.TextView
}

func (p *creationTreePicker) directoryID() string {
	if p == nil || p.tree.GetCurrentNode() == nil {
		return ""
	}
	switch target := p.tree.GetCurrentNode().GetReference().(type) {
	case string:
		return target
	case accountTreeTarget:
		return target.directoryID
	default:
		return ""
	}
}

func (p *creationTreePicker) serviceID() string {
	if p == nil || p.tree.GetCurrentNode() == nil {
		return ""
	}
	target, _ := p.tree.GetCurrentNode().GetReference().(accountTreeTarget)
	return target.serviceID
}

type accountTreeTarget struct {
	serviceID   string
	directoryID string
}

func (u *App) creationFolderPicker(initialID string) *creationTreePicker {
	root := tview.NewTreeNode("Root").SetReference("").SetExpanded(true)
	tree := tview.NewTreeView().SetRoot(root).SetCurrentNode(root)
	if u.services != nil {
		tree = u.services.folderTree("", initialID)
	}
	tree.SetSelectedFunc(nil)
	tree.SetBackgroundColor(commandTheme.Background)
	destination := tview.NewTextView().SetWrap(false)
	destination.SetBackgroundColor(commandTheme.Background)
	update := func(node *tview.TreeNode) {
		if node == nil {
			return
		}
		path := "Root"
		if u.services != nil {
			path = u.services.directoryPath(node.GetReference().(string))
		}
		destination.SetText("Create in: " + tview.Escape(displayText(path)))
	}
	tree.SetChangedFunc(update)
	update(tree.GetCurrentNode())
	return &creationTreePicker{tree: tree, destination: destination}
}

func (u *App) creationSecretPicker(initialServiceID, initialFolderID string) *creationTreePicker {
	root := tview.NewTreeNode("Root").SetReference("").SetExpanded(true)
	tree := tview.NewTreeView().SetRoot(root).SetCurrentNode(root)
	destination := tview.NewTextView().SetWrap(false)
	destination.SetBackgroundColor(commandTheme.Background)
	if u.services == nil {
		root.SetSelectable(false).SetColor(commandTheme.Muted)
		destination.SetText("Choose a service for the secret")
		return &creationTreePicker{tree: tree, destination: destination}
	}

	tree = u.services.folderTree("", initialFolderID)
	root = tree.GetRoot()
	folders := map[string]*tview.TreeNode{}
	root.Walk(func(node, _ *tview.TreeNode) bool {
		id := node.GetReference().(string)
		folders[id] = node
		node.SetSelectable(false).SetColor(commandTheme.Muted)
		return true
	})
	var first, inFolder, selected *tview.TreeNode
	for _, item := range u.services.allItems {
		parent := folders[item.DirectoryID]
		if parent == nil {
			parent = root
		}
		serviceNode := tview.NewTreeNode(vaultIconPrefix(serviceIcon) + tview.Escape(displayText(item.Name))).
			SetReference(accountTreeTarget{serviceID: item.ID, directoryID: item.DirectoryID}).
			SetColor(tcell.ColorWhite).
			SetSelectedTextStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(commandTheme.Border))
		parent.AddChild(serviceNode)
		if first == nil {
			first = serviceNode
		}
		if inFolder == nil && item.DirectoryID == initialFolderID {
			inFolder = serviceNode
		}
		if item.ID == initialServiceID {
			selected = serviceNode
		}
	}
	if selected == nil {
		selected = inFolder
	}
	if selected == nil {
		selected = first
	}
	if selected != nil {
		tree.SetCurrentNode(selected)
	}
	tree.SetSelectedFunc(nil)
	tree.SetBackgroundColor(commandTheme.Background)
	update := func(node *tview.TreeNode) {
		if node == nil {
			destination.SetText("Choose a service for the secret")
			return
		}
		target, ok := node.GetReference().(accountTreeTarget)
		if !ok {
			destination.SetText("Choose a service for the secret")
			return
		}
		destination.SetText("Service: " + node.GetText() + " · Folder: " + tview.Escape(displayText(u.services.directoryPath(target.directoryID))))
	}
	tree.SetChangedFunc(update)
	update(tree.GetCurrentNode())
	return &creationTreePicker{tree: tree, destination: destination}
}

func formField(label string, secret bool) *tview.InputField {
	field := tview.NewInputField().SetLabel(label + ": ").SetFieldWidth(0)
	field.SetLabelColor(commandTheme.Text).SetFieldBackgroundColor(commandTheme.Panel).SetFieldTextColor(commandTheme.Text)
	if secret {
		field.SetMaskCharacter('*')
	}
	return field
}

func (u *App) formPanel(page route, title, description string, fields []tview.Primitive, submitLabel string, submit, cancel func(), picker *creationTreePicker, activate func(tview.Primitive) bool, canSubmit ...func() bool) tview.Primitive {
	panel := newResponsiveFlex()
	panel.SetDirection(tview.FlexRow)
	panel.SetBorder(true).SetTitle(" "+title+" ").SetBorderColor(commandTheme.Border).SetBorderPadding(0, 0, 1, 1)
	modal := page == routeCreateService || page == routeCreateSecret
	if modal {
		panel.SetDrawFunc(func(_ tcell.Screen, x, y, width, height int) (int, int, int, int) {
			return x + 2, y + 1, max(width-4, 0), max(height-2, 0)
		})
		styleModalFrame(panel.Box, title, cancel)
	}
	descriptionView := tview.NewTextView().SetText(description).SetWrap(true)
	if modal {
		descriptionView.SetBackgroundColor(commandTheme.Background)
	}
	if description != "" {
		panel.AddItem(descriptionView, 2, 0, false)
	}
	controls := []tview.Primitive{}
	for _, field := range fields {
		if input, ok := field.(*tview.InputField); ok && modal {
			styleModalInput(input)
		}
		panel.AddItem(field, 1, 0, len(controls) == 0)
		controls = append(controls, field)
	}
	if picker != nil {
		panel.AddItem(picker.tree, 0, 1, false)
		panel.AddItem(picker.destination, 1, 0, false)
		controls = append(controls, picker.tree)
	}
	submitButton := actionButton(submitLabel, submit)
	spacer := tview.NewBox()
	buttons := tview.NewFlex().AddItem(submitButton, 0, 1, false)
	cancelButton := actionButton("Cancel", cancel)
	cancelVisible := !modal
	if !modal {
		buttons.AddItem(cancelButton, 0, 1, false)
	}
	if modal {
		spacer.SetBackgroundColor(commandTheme.Background)
		buttons.SetBackgroundColor(commandTheme.Background)
	}
	panel.AddItem(spacer, 1, 0, false)
	panel.AddItem(buttons, 1, 0, false)
	if !modal {
		panel.AddItem(tview.NewTextView().SetText("Tab Next · Enter Next/submit · Esc Cancel").SetWrap(false), 1, 0, false)
	}
	controls = append(controls, submitButton)
	if cancelVisible {
		controls = append(controls, cancelButton)
	}
	if description != "" {
		controls = append(controls, descriptionView)
	}
	formHeight := 5 + len(fields)
	if modal {
		formHeight--
	}
	if description != "" {
		formHeight += 2
	}
	if picker != nil {
		formHeight = max(formHeight+7, 18)
	}
	dialog := centered(72, formHeight, panel).(*boundedDialog)
	dialog.prepare = func(int, int) {
		noteHeight := 0
		busy := u.navigationLocked.Load()
		for _, field := range fields {
			switch field := field.(type) {
			case *tview.InputField:
				field.SetDisabled(busy)
			case *tview.DropDown:
				field.SetDisabled(busy)
			case *secretValueInput:
				field.disable(busy)
				height := 1
				if field.typ == domain.SecretPassword {
					height = 3
					noteHeight = 2
				}
				if field.typ == domain.SecretNote {
					height = 6
					noteHeight = 5
				}
				panel.ResizeItem(field, height, 0)
			}
		}
		if modal && (noteHeight == 5) != cancelVisible {
			cancelVisible = noteHeight == 5
			if cancelVisible {
				buttons.AddItem(cancelButton, 0, 1, false)
				controls = append(controls, cancelButton)
			} else {
				buttons.RemoveItem(cancelButton)
				for i, control := range controls {
					if control == cancelButton {
						controls = append(controls[:i], controls[i+1:]...)
						break
					}
				}
			}
			u.setFocusOrder(page, controls...)
		}
		dialog.height = formHeight + noteHeight
		available := len(canSubmit) == 0 || canSubmit[0]()
		submitButton.SetDisabled(busy || !available)
		if cancelButton != nil {
			cancelButton.SetDisabled(busy)
		}
		if busy {
			panel.SetTitle(" Working... ")
		} else {
			panel.SetTitle(" " + title + " ")
		}
	}
	u.setFocusOrder(page, controls...)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		for _, field := range fields {
			if dropdown, ok := field.(*tview.DropDown); ok && dropdown.IsOpen() {
				return event
			}
		}
		if event.Key() == tcell.KeyEscape && !u.navigationLocked.Load() {
			cancel()
			return nil
		}
		if u.navigationLocked.Load() {
			return nil
		}
		if !modal || u.modalDepth.Load() == 0 {
			return event
		}
		activeControls := []tview.Primitive{}
		for _, control := range controls {
			if value, ok := control.(*secretValueInput); ok {
				if value.typ == domain.SecretPassword {
					activeControls = append(activeControls, value.input, value.password.repeat, value.password.view, value.password.generate)
				} else if value.typ == domain.SecretNote {
					activeControls = append(activeControls, value.note)
				} else {
					activeControls = append(activeControls, value.input)
				}
			} else {
				activeControls = append(activeControls, control)
			}
		}
		index := focusIndex(activeControls, u.tview.GetFocus())
		switch event.Key() {
		case tcell.KeyCtrlS:
			if !u.navigationLocked.Load() {
				submit()
			}
			return nil
		case tcell.KeyEnter:
			if isNoteTextArea(u.tview.GetFocus()) {
				return event
			}
			if activate != nil && activate(u.tview.GetFocus()) {
				return nil
			}
			if _, editing := u.tview.GetFocus().(*tview.InputField); editing {
				u.focusRelative(activeControls, index, 1)
				return nil
			}
		case tcell.KeyTab, tcell.KeyBacktab, tcell.KeyUp, tcell.KeyDown:
			if isNoteTextArea(u.tview.GetFocus()) && (event.Key() == tcell.KeyUp || event.Key() == tcell.KeyDown || event.Modifiers()&tcell.ModAlt != 0) {
				return event
			}
			if picker != nil && u.tview.GetFocus() == picker.tree && (event.Key() == tcell.KeyUp || event.Key() == tcell.KeyDown) {
				return event
			}
			step := 1
			if event.Key() == tcell.KeyBacktab || event.Key() == tcell.KeyUp {
				step = -1
			}
			u.focusRelative(activeControls, index, step)
			return nil
		case tcell.KeyLeft, tcell.KeyRight:
			if isButton(u.tview.GetFocus()) {
				step := 1
				if event.Key() == tcell.KeyLeft {
					step = -1
				}
				u.focusRelative(activeControls, index, step)
				return nil
			}
		}
		return event
	})
	return dialog
}
