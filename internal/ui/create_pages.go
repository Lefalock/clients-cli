// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"
	"errors"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/domain"
)

func secretTypeSelect() *tview.DropDown {
	field := tview.NewDropDown().SetLabel("Secret type: ").SetFieldWidth(0)
	field.SetOptions([]string{"Password", "Auth code"}, nil)
	field.SetCurrentOption(0)
	field.SetLabelColor(commandTheme.Text).SetFieldBackgroundColor(commandTheme.Background).SetFieldTextColor(commandTheme.Text)
	field.SetBackgroundColor(commandTheme.Background)
	return field
}

func selectedSecretType(field *tview.DropDown) domain.SecretType {
	index, _ := field.GetCurrentOption()
	switch index {
	case 0:
		return domain.SecretPassword
	case 1:
		return domain.SecretAuthCode
	}
	return ""
}

type typedCredentialService interface {
	CreateServiceWithType(context.Context, string, string, string, string, domain.SecretType) error
	CreateSecretWithType(context.Context, string, string, string, string, domain.SecretType) error
}

func (u *App) openCreateModal(page route) {
	restore := u.tview.GetFocus()
	var dialog tview.Primitive
	switch page {
	case routeCreateService:
		dialog = u.createServicePage(restore)
	case routeCreateSecret:
		dialog = u.createSecretPage(restore)
	default:
		return
	}
	u.pushModal()
	u.pages.AddPage(string(page), dialog, true, true)
	u.focusFirst(page)
}

func (u *App) closeCreateModal(page route, restore tview.Primitive) {
	u.pages.RemovePage(string(page))
	u.clearNote = nil
	u.popModal()
	if restore != nil {
		u.tview.SetFocus(restore)
	} else {
		u.focusFirst(routeServices)
	}
}

func (u *App) createServicePage(restore tview.Primitive) tview.Primitive {
	name, account := formField("Service name", false), formField("First secret name", false)
	password := newSecretValueInput()
	u.clearNote = password.clear
	secretType := secretTypeSelect()
	secretType.SetSelectedFunc(func(_ string, _ int) {
		password.selectType(selectedSecretType(secretType))
		account.SetLabel("Secret name: ")
	})
	folder := u.creationFolderPicker(u.createDirectoryID)
	busy := false
	password.password.bind(u, func() bool { return !busy && password.typ == domain.SecretPassword }, nil)
	cancel := func() {
		if !busy && !u.navigationLocked.Load() {
			password.clear()
			u.closeCreateModal(routeCreateService, restore)
		}
	}
	submit := func() {
		if busy {
			return
		}
		if password.typ == domain.SecretPassword && !password.password.validate(u) {
			return
		}
		busy = true
		nameValue, accountValue, passwordValue := name.GetText(), account.GetText(), password.text()
		directoryID := folder.directoryID()
		selectedType := selectedSecretType(secretType)
		u.beginProtectedTask(func(ctx context.Context) error {
			if selectedType != domain.SecretPassword {
				if typed, ok := u.credentials.(typedCredentialService); ok {
					return typed.CreateServiceWithType(ctx, directoryID, nameValue, accountValue, passwordValue, selectedType)
				}
				return errors.New("Selected secret type storage is unavailable")
			}
			if directoryService, ok := u.credentials.(DirectoryCredentialService); ok {
				return directoryService.CreateServiceInDirectory(ctx, directoryID, nameValue, accountValue, passwordValue)
			}
			return u.credentials.CreateService(ctx, nameValue, accountValue, passwordValue)
		}, func(err error) {
			busy = false
			if err != nil {
				u.showError(err)
				return
			}
			u.services.preferredService, u.services.preferredSecret = nameValue, accountValue
			u.createDirectoryID = directoryID
			u.services.expanded[directoryID] = true
			password.clear()
			u.closeCreateModal(routeCreateService, restore)
			u.services.load()
		})
	}
	description := ""
	return u.formPanel(routeCreateService, "Add service and first secret", description, []tview.Primitive{name, account, secretType, password}, "Create service", submit, cancel, folder, nil, func() bool { return true })
}

func (u *App) createSecretPage(restore tview.Primitive) tview.Primitive {
	account := formField("Secret name", false)
	password := newSecretValueInput()
	path := formField("Path", false)
	if u.services != nil {
		path.SetText(u.services.contentBreadcrumb())
	}
	path.SetAcceptanceFunc(func(string, rune) bool { return false })
	u.clearNote = password.clear
	secretType := secretTypeSelect()
	secretType.SetSelectedFunc(func(_ string, _ int) {
		password.selectType(selectedSecretType(secretType))
		account.SetLabel("Secret name: ")
	})
	serviceID, directoryID := u.accountServiceID, u.createDirectoryID
	openPath := func() {
		if u.navigationLocked.Load() || u.pages.HasPage("secret-path-picker") {
			return
		}
		u.openSecretPathPicker(path, &serviceID, &directoryID)
	}
	path.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftClick && event != nil && path.InRect(event.Position()) {
			openPath()
			return tview.MouseConsumed, nil
		}
		return action, event
	})
	busy := false
	password.password.bind(u, func() bool { return !busy && password.typ == domain.SecretPassword }, nil)
	cancel := func() {
		if !busy && !u.navigationLocked.Load() {
			password.clear()
			u.closeCreateModal(routeCreateSecret, restore)
		}
	}
	submit := func() {
		if busy || serviceID == "" {
			return
		}
		if password.typ == domain.SecretPassword && !password.password.validate(u) {
			return
		}
		busy = true
		accountValue, passwordValue := account.GetText(), password.text()
		selectedType := selectedSecretType(secretType)
		u.beginProtectedTask(func(ctx context.Context) error {
			if selectedType != domain.SecretPassword {
				if typed, ok := u.credentials.(typedCredentialService); ok {
					return typed.CreateSecretWithType(ctx, serviceID, directoryID, accountValue, passwordValue, selectedType)
				}
				return errors.New("Selected secret type storage is unavailable")
			}
			if directoryService, ok := u.credentials.(DirectoryCredentialService); ok {
				return directoryService.CreateSecretInDirectory(ctx, serviceID, directoryID, accountValue, passwordValue)
			}
			return u.credentials.CreateSecret(ctx, serviceID, accountValue, passwordValue)
		}, func(err error) {
			busy = false
			if err != nil {
				u.showError(err)
				return
			}
			u.services.preferredSecret = accountValue
			u.accountServiceID = serviceID
			u.createDirectoryID = directoryID
			password.clear()
			u.closeCreateModal(routeCreateSecret, restore)
			u.services.load()
		})
	}
	return u.formPanel(routeCreateSecret, "Add secret", "", []tview.Primitive{account, path, secretType, password}, "Create secret", submit, cancel, nil, func(focus tview.Primitive) bool {
		if focus != path {
			return false
		}
		openPath()
		return true
	}, func() bool { return serviceID != "" })
}

func (u *App) openSecretPathPicker(path *tview.InputField, serviceID, directoryID *string) {
	const pageName = "secret-path-picker"
	target := u.creationSecretPicker(*serviceID, *directoryID)
	close := func() {
		u.pages.RemovePage(pageName)
		u.popModal()
		u.tview.SetFocus(path)
	}
	choose := func() {
		selectedServiceID := target.serviceID()
		if selectedServiceID == "" {
			return
		}
		*serviceID, *directoryID = selectedServiceID, target.directoryID()
		if u.services != nil {
			for _, item := range u.services.allItems {
				if item.ID == selectedServiceID {
					breadcrumb := u.services.directoryBreadcrumbFor(*directoryID)
					if breadcrumb != "/" {
						breadcrumb += " › "
					} else {
						breadcrumb = ""
					}
					path.SetText(breadcrumb + item.Name)
					break
				}
			}
		}
		close()
	}
	target.tree.SetSelectedFunc(func(*tview.TreeNode) { choose() })
	selectButton := actionButton("Select", choose)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(target.tree, 0, 1, true).
		AddItem(target.destination, 1, 0, false).
		AddItem(selectButton, 1, 0, false)
	styleModalFrame(panel.Box, "Choose path", close)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			if u.tview.GetFocus() == target.tree {
				u.tview.SetFocus(selectButton)
			} else {
				u.tview.SetFocus(target.tree)
			}
			return nil
		}
		return event
	})
	u.pushModal()
	u.pages.AddPage(pageName, centered(68, 20, panel), true, true)
	u.tview.SetFocus(target.tree)
}
