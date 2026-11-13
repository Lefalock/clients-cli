// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/domain"
	"lefalock/internal/service"
)

const passwordRevealDuration = 15 * time.Second

const (
	serviceIcon        = "🔧"
	accountIcon        = "🔑"
	noteIcon           = "📝"
	folderClosedIcon   = "▶"
	folderExpandedIcon = "▼"
	vaultIconWidth     = 3
)

func vaultIconPrefix(icon string) string {
	return icon + strings.Repeat(" ", max(1, vaultIconWidth-tview.TaggedStringWidth(icon)))
}

type servicesPage struct {
	preferredNote                                                                                             string
	preferredNoteParent                                                                                       noteParent
	addNote, addServiceNote                                                                                   *tview.Button
	standaloneNotes                                                                                           []service.Secret
	editSecretButton                                                                                          *tview.Button
	detailButtons                                                                                             []*tview.Button
	ui                                                                                                        *App
	root                                                                                                      *responsiveFlex
	searchBar, columns, accountPane, servicePane, contentPane, actionsPane                                    *tview.Flex
	serviceActions, actionBar, folderActions                                                                  *vaultActionGroup
	header, modalDetails, contentPath                                                                         *tview.TextView
	search                                                                                                    *tview.InputField
	servicesTable, accountsTable                                                                              *tview.Table
	create, copy, accountDetails, serviceDetails, addSecret, backServices, clearSearch, retry                 *tview.Button
	renameServiceButton, moveService, deleteServiceButton, renameSecretButton, moveSecret, deleteSecretButton *tview.Button
	returnFocus                                                                                               tview.Primitive
	closeDetails                                                                                              func()
	lastWidth, lastHeight                                                                                     int
	filterCleared                                                                                             bool
	items, allItems                                                                                           []service.Service
	directories                                                                                               []service.Directory
	treeRows                                                                                                  []vaultTreeRow
	contentRows                                                                                               []vaultContentRow
	expanded                                                                                                  map[string]bool
	directoryID                                                                                               string
	contentDirectoryID, contentServiceID                                                                      string
	selectedDirectory                                                                                         bool
	createFolder                                                                                              *tview.Button
	folderButtons                                                                                             []*tview.Button
	selectedService                                                                                           int
	serviceID, accountID                                                                                      string
	accountSelections                                                                                         map[string]string
	busy, rendering, loaded, compact, accountsVisible, small                                                  bool
	loadError                                                                                                 bool
	actions                                                                                                   []tview.Primitive
	revealEpoch                                                                                               uint64
	revealTimer                                                                                               *time.Timer
	password                                                                                                  string
	authCode, authCodeSecretID                                                                                string
	authCodeSecondsLeft                                                                                       int
	authCodeEpoch                                                                                             uint64
	authCodeCancel                                                                                            context.CancelFunc
	authCodeNow                                                                                               func() time.Time
	preferredService, preferredSecret                                                                         string
	preferredDirectoryName, preferredDirectoryParent                                                          string
}

type vaultTreeRow struct {
	directory    bool
	directoryID  string
	serviceIndex int
	prefix       string
}

type vaultContentKind uint8

const (
	contentParent vaultContentKind = iota
	contentDirectory
	contentService
	contentSecret
)

type vaultContentRow struct {
	kind vaultContentKind
	id   string
}

func newServicesPage(ui *App) *servicesPage {
	p := &servicesPage{ui: ui, selectedService: -1, accountSelections: make(map[string]string), expanded: make(map[string]bool)}
	p.root = newResponsiveFlex()
	p.root.SetDirection(tview.FlexRow)
	placeholder := "Search services and secrets…"
	if _, ok := ui.credentials.(DirectoryCredentialService); ok {
		placeholder = "Search folders, services and secrets…"
	}
	p.search = tview.NewInputField().SetLabel(" / ").SetPlaceholder(placeholder)
	p.search.SetBackgroundColor(commandTheme.Background)
	p.search.SetFieldBackgroundColor(commandTheme.Background).SetFieldTextColor(commandTheme.Text)
	p.search.SetPlaceholderStyle(tcell.StyleDefault.Background(commandTheme.Background).Foreground(commandTheme.Muted))
	p.search.SetLabelColor(commandTheme.Muted)
	p.create = actionButton("Add", func() {
		ui.createDirectoryID = ""
		if p.selectedDirectory {
			ui.createDirectoryID = p.directoryID
		}
		ui.navigate(routeCreateService)
	})
	p.createFolder = actionButton("Add", p.newRootFolder)
	p.clearSearch = actionButton("Clear", func() { p.search.SetText(""); ui.tview.SetFocus(p.search) })
	p.searchBar = tview.NewFlex().AddItem(p.search, 0, 1, true)
	p.searchBar.SetBackgroundColor(commandTheme.Background)
	p.header = tview.NewTextView().SetTextColor(commandTheme.Muted).SetWrap(false)
	p.servicesTable = vaultTable(" Services ")
	p.accountsTable = vaultTable(" Contents ")
	p.contentPath = tview.NewTextView().SetWrap(false).SetTextColor(commandTheme.Muted)
	p.contentPath.SetBackgroundColor(commandTheme.Background)
	p.contentPane = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.accountsTable, 0, 1, true).AddItem(p.contentPath, 1, 0, false)
	p.contentPane.SetBorder(true).SetBorderColor(commandTheme.Border).SetBackgroundColor(commandTheme.Background)
	p.copy = actionButton("Copy", p.copyPassword)
	p.accountDetails = actionButton("Details", p.openDetails)
	p.serviceDetails = actionButton("Details", p.openServiceDetails)
	p.addSecret = actionButton("Add", p.createSecret)
	p.addNote = actionButton("Note", func() { p.createNoteIn(noteParent{DirectoryID: p.directoryID}) })
	p.addServiceNote = actionButton("Note", func() {
		if p.selectedService >= 0 {
			p.createNoteIn(noteParent{ServiceID: p.serviceID})
		}
	})
	p.renameServiceButton = actionButton("Rename", p.renameService)
	p.moveService = actionButton("Move", p.moveSelectedService)
	p.deleteServiceButton = actionButton("Delete", p.deleteService)
	p.renameSecretButton = actionButton("Rename", p.renameSecret)
	p.editSecretButton = actionButton("Edit", p.editSecretValue)
	p.moveSecret = actionButton("Move", p.moveSelectedSecret)
	p.deleteSecretButton = actionButton("Delete", p.deleteSecret)
	p.deleteServiceButton.SetLabelColor(commandTheme.Danger)
	p.deleteSecretButton.SetLabelColor(commandTheme.Danger)
	p.backServices = actionButton("Back to services", func() {
		p.accountsVisible = false
		p.selectFirstTableRow(p.servicesTable)
		ui.tview.SetFocus(p.servicesTable)
	})
	p.retry = actionButton("Retry loading", p.load)
	p.actions = []tview.Primitive{p.addSecret, p.accountDetails, p.renameSecretButton, p.copy, p.moveSecret, p.deleteSecretButton, p.editSecretButton}
	p.actionBar = newVaultActionGroup(p.addSecret, p.accountDetails, p.renameSecretButton, p.copy, p.moveSecret, p.deleteSecretButton, p.editSecretButton)
	p.actionBar.SetBlurFunc(p.maskPasswords)
	layoutVaultActionGrid(p.actionBar, "Secrets", [8]*tview.Button{p.addSecret, p.moveSecret, p.renameSecretButton, p.deleteSecretButton, p.accountDetails, p.copy, p.editSecretButton})
	p.folderButtons = []*tview.Button{
		p.createFolder,
		actionButton("Rename", p.renameFolder),
		actionButton("Move", p.moveFolder),
		actionButton("Delete", p.deleteFolder),
	}
	p.folderButtons[3].SetLabelColor(commandTheme.Danger)
	p.folderActions = newVaultActionGroup(p.createFolder, p.folderButtons[1], p.folderButtons[2], p.folderButtons[3], p.addNote)
	layoutVaultActionGrid(p.folderActions, "Folders", [8]*tview.Button{p.createFolder, p.folderButtons[2], p.folderButtons[1], p.folderButtons[3], 6: p.addNote})
	p.serviceActions = newVaultActionGroup(p.create, p.serviceDetails, p.renameServiceButton, p.moveService, p.deleteServiceButton, p.addServiceNote)
	layoutVaultActionGrid(p.serviceActions, "Services", [8]*tview.Button{p.create, p.moveService, p.renameServiceButton, p.deleteServiceButton, p.serviceDetails, 6: p.addServiceNote})
	p.accountPane = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.contentPane, 0, 1, true)
	p.servicePane = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(p.servicesTable, 0, 1, true)
	p.columns = tview.NewFlex()
	p.actionsPane = tview.NewFlex()
	p.actionsPane.SetBorder(true).SetTitle(" Actions ").SetBorderColor(commandTheme.Border).SetBackgroundColor(commandTheme.Background)
	p.root.layout = p.layout
	p.search.SetChangedFunc(func(string) {
		if !p.rendering {
			p.applyFilter()
		}
	})
	p.servicesTable.SetSelectionChangedFunc(func(row, _ int) {
		if !p.rendering {
			p.selectService(row)
		}
	})
	p.servicesTable.SetSelectedFunc(func(row, _ int) {
		if row >= 0 && row < len(p.treeRows) && p.treeRows[row].directory {
			p.toggleDirectory(row)
			return
		}
		if p.selectService(row) {
			p.accountsVisible = true
			p.selectFirstTableRow(p.accountsTable)
			p.ui.tview.SetFocus(p.accountsTable)
		}
	})
	p.accountsTable.SetSelectionChangedFunc(func(row, _ int) {
		if p.rendering {
			return
		}
		p.maskPasswords()
		p.accountID = ""
		p.contentDirectoryID, p.contentServiceID = "", ""
		if row >= 0 && row < len(p.contentRows) {
			entry := p.contentRows[row]
			switch entry.kind {
			case contentDirectory:
				p.contentDirectoryID = entry.id
			case contentService:
				p.contentServiceID = entry.id
			case contentSecret:
				p.accountID = entry.id
				p.accountSelections[p.accountSelectionKey()] = p.accountID
			}
		}
		p.refreshDetails()
	})
	p.accountsTable.SetSelectedFunc(func(row, _ int) { p.openContentRow(row) })
	p.servicesTable.SetMouseCapture(p.detailsDoubleClick(p.servicesTable, func() int { return len(p.treeRows) }, p.openServiceDetails, func(row int) bool {
		if !p.treeRows[row].directory {
			return false
		}
		alreadySelected := p.selectedDirectory && p.directoryID == p.treeRows[row].directoryID
		p.servicesTable.Select(row, 0)
		p.ui.tview.SetFocus(p.servicesTable)
		if alreadySelected {
			p.toggleDirectory(row)
		}
		return true
	}))
	p.accountsTable.SetMouseCapture(p.detailsDoubleClick(p.accountsTable, func() int { return len(p.contentRows) }, func() {
		row, _ := p.accountsTable.GetSelection()
		p.openContentRow(row)
	}, nil))
	p.accountsTable.SetBlurFunc(p.maskPasswords)
	p.maskButtonOnBlur(p.copy)
	p.maskButtonOnBlur(p.accountDetails)
	p.renderServices()
	return p
}

func layoutVaultActionGrid(group *vaultActionGroup, title string, slots [8]*tview.Button) {
	group.SetBorder(true).SetTitle(" " + title + " ").SetBorderColor(commandTheme.Border)
	group.slots = slots
	group.Clear()
	for row := 0; row < group.rows(); row++ {
		line := tview.NewFlex()
		line.SetBackgroundColor(commandTheme.Background)
		for column := 0; column < 2; column++ {
			var item tview.Primitive = tview.NewBox().SetBackgroundColor(commandTheme.Background)
			if button := slots[column*group.rows()+row]; button != nil {
				item = button
			}
			line.AddItem(item, 0, 1, false)
		}
		group.AddItem(line, 1, 0, false)
	}
}

func (p *servicesPage) toggleDirectory(row int) {
	if row < 0 || row >= len(p.treeRows) || !p.treeRows[row].directory {
		return
	}
	id := p.treeRows[row].directoryID
	if p.search.GetText() != "" {
		p.rendering = true
		p.search.SetText("")
		p.rendering = false
	}
	p.expanded[id] = !p.expanded[id]
	p.applyFilter()
}

func (p *servicesPage) detailsDoubleClick(table *tview.Table, rowCount func() int, open func(), single func(int) bool) func(tview.MouseAction, *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	lastRow := -1
	var lastClick time.Time
	folderPressHandled := false
	return func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if single != nil && folderPressHandled && (action == tview.MouseLeftClick || action == tview.MouseLeftDoubleClick) {
			folderPressHandled = false
			return tview.MouseConsumed, nil
		}
		if action != tview.MouseLeftDown && action != tview.MouseLeftClick && action != tview.MouseLeftDoubleClick {
			return action, event
		}
		x, y := event.Position()
		row, _ := table.CellAt(x, y)
		if !table.InInnerRect(x, y) || row < 0 || row >= rowCount() {
			lastRow = -1
			if action == tview.MouseLeftDown {
				folderPressHandled = false
			}
			return action, event
		}
		if single != nil && p.treeRows[row].directory {
			lastRow = -1
			if action == tview.MouseLeftDown {
				folderPressHandled = true
			}
			single(row)
			return tview.MouseConsumed, nil
		}
		if action == tview.MouseLeftDown {
			folderPressHandled = false
			return action, event
		}
		if action == tview.MouseLeftClick {
			lastRow, lastClick = row, time.Now()
			return action, event
		}
		if row == lastRow && time.Since(lastClick) <= tview.DoubleClickInterval {
			lastRow = -1
			table.Select(row, 0)
			p.ui.tview.SetFocus(table)
			open()
			return tview.MouseConsumed, nil
		}
		// tview can report a double click for two fast clicks on different rows.
		// Treat the second one as a normal selection in that case.
		lastRow, lastClick = row, time.Now()
		return tview.MouseLeftClick, event
	}
}

func vaultTable(title string) *tview.Table {
	table := tview.NewTable().SetSelectable(true, false).SetWrapSelection(true, false)
	table.SetBorder(true).SetTitle(title).SetBorderColor(commandTheme.Border)
	table.SetSelectedStyle(tcell.StyleDefault.Background(commandTheme.Focus).Foreground(tcell.ColorBlack))
	return table
}

func (p *servicesPage) primitive() tview.Primitive { return p.root }

func (p *servicesPage) layout(width, height int) {
	if p.lastWidth != 0 && (p.lastWidth != width || p.lastHeight != height) {
		p.maskPasswords()
	}
	p.lastWidth, p.lastHeight = width, height
	p.small = width < 60 || height < 14
	if p.small {
		return
	}
	p.compact = width < 80
	detailsLabel := "[Details]"
	if p.compact && p.directoryService() != nil {
		detailsLabel = "[Info]"
	}
	p.serviceActions.labels[1] = tview.Escape(detailsLabel)
	p.actionBar.labels[1] = tview.Escape(detailsLabel)
	p.searchBar.Clear().AddItem(p.search, 0, 1, true)
	if p.search.GetText() != "" {
		p.searchBar.AddItem(p.clearSearch, 12, 0, false)
	}
	for _, table := range []*tview.Table{p.servicesTable, p.accountsTable} {
		border, background, foreground := commandTheme.Border, commandTheme.Panel, commandTheme.Text
		if table.HasFocus() {
			border, background, foreground = commandTheme.Focus, commandTheme.Focus, tcell.ColorBlack
		}
		table.SetBorderColor(border)
		table.SetSelectedStyle(tcell.StyleDefault.Background(background).Foreground(foreground))
	}
	p.accountsTable.SetBorder(false)
	contentBorder := commandTheme.Border
	if p.accountsTable.HasFocus() {
		contentBorder = commandTheme.Focus
	}
	p.contentPane.SetBorderColor(contentBorder)
	serviceTitle := fmt.Sprintf(" Services · %d ", len(p.treeRows))
	if p.directoryService() != nil {
		serviceTitle = fmt.Sprintf(" Folders & services · %d ", len(p.treeRows))
	}
	accountTitle := " Contents "
	if p.selectedService >= 0 {
		accountTitle = " Contents in " + displayText(p.items[p.selectedService].Name) + " "
	} else if p.selectedDirectory {
		accountTitle = " Contents of " + displayText(p.directoryName(p.directoryID)) + " "
		if p.directoryID == "" {
			accountTitle = " Contents / "
		}
	}
	if p.servicesTable.HasFocus() {
		serviceTitle = " Services [FOCUS] "
		if p.directoryService() != nil {
			serviceTitle = " Folders & services [FOCUS] "
		}
	}
	if p.accountsTable.HasFocus() {
		if p.selectedDirectory {
			accountTitle = " Contents [FOCUS] "
		} else {
			accountTitle = " Contents [FOCUS] "
		}
	}
	p.servicesTable.SetTitle(tview.Escape(serviceTitle))
	p.accountsTable.SetTitle(tview.Escape(accountTitle))
	p.contentPane.SetTitle(tview.Escape(accountTitle))
	if p.servicesTable.HasFocus() || p.serviceActions.HasFocus() {
		p.accountsVisible = false
	}
	if p.accountsTable.HasFocus() || p.actionBar.HasFocus() {
		p.accountsVisible = true
	}
	if p.folderActions.HasFocus() {
		p.accountsVisible = true
	}
	// Actions are shared below both lists, leaving the lists at the same height.
	p.servicePane.Clear().AddItem(p.servicesTable, 0, 1, true)
	p.accountPane.Clear().AddItem(p.contentPane, 0, 1, true)
	p.actionsPane.Clear()
	if p.directoryService() != nil {
		p.actionsPane.AddItem(p.folderActions, 0, 1, false)
	}
	p.actionsPane.AddItem(p.serviceActions, 0, 1, false).
		AddItem(p.actionBar, 0, 1, false)
	p.columns.Clear()
	if !p.compact || !p.accountsVisible {
		p.columns.AddItem(p.servicePane, 0, 1, true)
	}
	if !p.compact || p.accountsVisible {
		p.columns.AddItem(p.accountPane, 0, 2, false)
	}
	p.root.Clear().AddItem(p.columns, 0, 1, true).AddItem(p.actionsPane, 8, 0, false)
	if (p.compact && p.accountsVisible) || p.backServices.HasFocus() {
		p.root.AddItem(p.backServices, 1, 0, false)
	}
	if p.loadError {
		p.root.AddItem(p.retry, 1, 0, false)
	}
	p.root.AddItem(p.searchBar, 1, 0, false)
	p.root.AddItem(p.header, 1, 0, false)
	if p.ui.modalDepth.Load() == 0 && !p.ui.shell.navigation.HasFocus() {
		p.updateFooter()
	}
}

func (p *servicesPage) load() {
	if p.busy {
		return
	}
	p.busy = true
	p.loadError = false
	p.maskPasswords()
	p.header.SetText("Loading vault…")
	p.updateActions()
	var result []service.Service
	var directories []service.Directory
	var notes []service.Secret
	p.ui.beginTask(func(ctx context.Context) error {
		var err error
		result, err = p.ui.credentials.List(ctx)
		if err != nil {
			return err
		}
		if ns := p.noteService(); ns != nil {
			notes, err = ns.ListStandaloneNotes(ctx)
			if err != nil {
				return err
			}
		}
		if directoryService, ok := p.ui.credentials.(DirectoryCredentialService); ok {
			directories, err = directoryService.ListDirectories(ctx)
		}
		return err
	}, func(err error) {
		p.busy = false
		if err != nil {
			p.loadError = true
			p.header.SetText("Load failed: " + err.Error() + " · Retry loading")
			p.updateActions()
			if p.ui.modalDepth.Load() == 0 {
				p.ui.tview.SetFocus(p.retry)
			}
			return
		}
		previousNotes := append([]service.Secret(nil), p.standaloneNotes...)
		for _, item := range p.allItems {
			previousNotes = append(previousNotes, item.Secrets...)
		}
		previous := p.allItems
		previousDirectories := p.directories
		p.allItems = result
		p.standaloneNotes = notes
		p.directories = directories
		p.loaded = true
		p.resolveCreatedDirectorySelection(previousDirectories)
		p.resolveCreatedSelection(previous)
		p.resolveCreatedNote(previousNotes)
		p.applyFilter()
		focus := p.returnFocus
		p.returnFocus = nil
		if focus == nil {
			focus = p.servicesTable
			if p.accountsVisible {
				focus = p.accountsTable
			}
		}
		if len(p.allItems) == 0 && len(p.directories) == 0 && len(p.standaloneNotes) == 0 {
			focus = p.create
		} else if len(p.contentRows) == 0 && p.accountsVisible {
			if p.selectedDirectory {
				focus = p.folderActions
			} else {
				focus = p.addSecret
			}
		}
		if p.filterCleared {
			p.header.SetText("Filter cleared to show the new secret")
			p.filterCleared = false
		}
		if p.ui.modalDepth.Load() == 0 {
			p.ui.tview.SetFocus(focus)
		}
	})
}

func (p *servicesPage) applyFilter() {
	p.maskPasswords()
	p.items = p.filterItems(p.search.GetText())
	p.renderServices()
	if p.busy {
		p.header.SetText("Loading vault…")
		return
	}
	if p.loadError {
		return
	}
	count := 0
	for _, item := range p.items {
		count += len(item.Secrets)
	}
	switch {
	case len(p.allItems) == 0 && len(p.directories) == 0 && len(p.standaloneNotes) == 0:
		p.header.SetText("Add a service and its first secret to begin. Folders are available here too.")
	case len(p.items) == 0 && p.search.GetText() != "" && !p.folderMatchesSearch() && !p.noteMatchesSearch():
		p.header.SetText("No matches · Clear search to see all entries.")
	case len(p.items) == 0 && p.search.GetText() == "":
		p.header.SetText(fmt.Sprintf("%d folders / 0 services shown", len(p.directories)))
	case p.search.GetText() != "":
		total := 0
		for _, item := range p.allItems {
			total += len(item.Secrets)
		}
		p.header.SetText(fmt.Sprintf("Services %d/%d · Secrets %d/%d shown", len(p.items), len(p.allItems), count, total))
	default:
		p.header.SetText(fmt.Sprintf("%d services / %d secrets shown", len(p.items), count))
	}
}

func (p *servicesPage) renderServices() {
	p.rendering = true
	p.servicesTable.Clear()
	p.buildTreeRows()
	if p.serviceID == "" && !p.selectedDirectory && len(p.allItems) == 0 && len(p.standaloneNotes) > 0 {
		p.selectedDirectory = true
	}
	p.findNoteSearchParent()
	rootContents := p.selectedDirectory && p.directoryID == ""
	selected := 0
	selectedFound := false
	firstServiceRow := -1
	firstMatchingFolderRow := -1
	searchQuery := strings.ToLower(strings.TrimSpace(p.search.GetText()))
	for row, node := range p.treeRows {
		if node.directory {
			name := p.directoryName(node.directoryID)
			if searchQuery != "" && firstMatchingFolderRow < 0 && (strings.Contains(strings.ToLower(name), searchQuery) || (p.noteMatchesSearch() && node.directoryID == p.directoryID)) {
				firstMatchingFolderRow = row
			}
			marker := vaultIconPrefix(folderClosedIcon)
			if p.expanded[node.directoryID] {
				marker = vaultIconPrefix(folderExpandedIcon)
			}
			p.servicesTable.SetCell(row, 1, tview.NewTableCell(tview.Escape(node.prefix+marker+displayText(name))).SetExpansion(1))
			if p.selectedDirectory && p.directoryID == node.directoryID {
				selected = row
				selectedFound = true
			}
		} else {
			if firstServiceRow < 0 {
				firstServiceRow = row
			}
			item := p.items[node.serviceIndex]
			p.servicesTable.SetCell(row, 1, tview.NewTableCell(tview.Escape(node.prefix+vaultIconPrefix(serviceIcon)+displayText(item.Name))).SetExpansion(1))
			p.servicesTable.SetCell(row, 2, tview.NewTableCell(fmt.Sprint(len(item.Secrets))).SetAlign(tview.AlignRight))
			if !p.selectedDirectory && item.ID == p.serviceID {
				selected = row
				selectedFound = true
			}
		}
		p.servicesTable.SetCell(row, 0, tview.NewTableCell(" ").SetExpansion(0))
	}
	if searchQuery != "" && (!selectedFound || p.selectedDirectory) {
		if firstMatchingFolderRow >= 0 {
			selected = firstMatchingFolderRow
		} else if firstServiceRow >= 0 {
			selected = firstServiceRow
		}
	}
	if p.directoryService() == nil {
		p.servicesTable.SetTitle(fmt.Sprintf(" Services · %d ", len(p.treeRows)))
	} else {
		p.servicesTable.SetTitle(fmt.Sprintf(" Folders & services · %d ", len(p.treeRows)))
	}
	p.selectedService = -1
	if len(p.treeRows) == 0 {
		if !rootContents {
			p.selectedDirectory, p.directoryID = false, ""
			p.serviceID, p.accountID = "", ""
		}
		text := "No results"
		if len(p.allItems) == 0 && len(p.directories) == 0 && len(p.standaloneNotes) == 0 {
			text = "No folders or services"
		}
		p.servicesTable.SetCell(0, 1, tview.NewTableCell(text).SetSelectable(false))
	}
	p.servicesTable.Select(selected, 0)
	p.rendering = false
	if rootContents {
		p.serviceID, p.accountID = "", p.accountSelections[p.accountSelectionKey()]
		p.contentDirectoryID, p.contentServiceID = "", ""
		p.renderSecrets()
	} else if len(p.treeRows) > 0 {
		p.selectService(selected)
	} else {
		p.renderSecrets()
	}
}

func (p *servicesPage) selectService(row int) bool {
	if row < 0 || row >= len(p.treeRows) {
		return false
	}
	p.maskPasswords()
	node := p.treeRows[row]
	p.selectedDirectory = node.directory
	p.directoryID = node.directoryID
	p.selectedService = node.serviceIndex
	if p.selectedDirectory {
		p.serviceID = ""
	} else {
		p.serviceID = p.items[node.serviceIndex].ID
		p.directoryID = p.items[node.serviceIndex].DirectoryID
	}
	p.accountID = p.accountSelections[p.accountSelectionKey()]
	p.contentDirectoryID, p.contentServiceID = "", ""
	p.renderSecrets()
	return true
}

func (p *servicesPage) setContentRow(row int, label, count string) {
	p.accountsTable.SetCell(row, 0, tview.NewTableCell(" "))
	p.accountsTable.SetCell(row, 1, tview.NewTableCell(tview.Escape(label)).SetExpansion(1))
	p.accountsTable.SetCell(row, 2, tview.NewTableCell(count).SetAlign(tview.AlignRight))
}

func (p *servicesPage) renderSecrets() {
	p.rendering = true
	p.accountsTable.Clear()
	title := " Contents "
	accounts := p.visibleSecrets()
	p.contentRows = p.contentRows[:0]
	if p.selectedService >= 0 {
		title = " Contents · " + tview.Escape(displayText(p.items[p.selectedService].Name)) + " "
	}
	if p.selectedDirectory {
		title = " Contents of " + tview.Escape(displayText(p.directoryName(p.directoryID))) + " "
		if p.directoryID == "" {
			title = " Contents / "
		}
	}
	p.contentPath.SetText(" " + tview.Escape(displayText(p.contentBreadcrumb())))
	p.accountsTable.SetTitle(title)
	p.contentPane.SetTitle(title)
	selected := 0
	hasParent := (p.selectedDirectory && p.directoryID != "") || p.selectedService >= 0
	if hasParent {
		parentID := p.directoryID
		if p.selectedDirectory {
			parentID = p.parentDirectoryID()
		}
		p.contentRows = append(p.contentRows, vaultContentRow{kind: contentParent, id: parentID})
		p.setContentRow(0, "..", "")
	}
	if p.selectedDirectory {
		for _, folder := range p.visibleChildDirectories() {
			row := len(p.contentRows)
			p.contentRows = append(p.contentRows, vaultContentRow{kind: contentDirectory, id: folder.ID})
			p.setContentRow(row, vaultIconPrefix(folderClosedIcon)+displayText(folder.Name), "")
			if folder.ID == p.contentDirectoryID {
				selected = row
			}
		}
		for _, item := range p.visibleServicesInDirectory() {
			row := len(p.contentRows)
			p.contentRows = append(p.contentRows, vaultContentRow{kind: contentService, id: item.ID})
			p.setContentRow(row, vaultIconPrefix(serviceIcon)+displayText(item.Name), fmt.Sprint(len(item.Secrets)))
			if item.ID == p.contentServiceID {
				selected = row
			}
		}
	}
	for _, account := range accounts {
		row := len(p.contentRows)
		p.contentRows = append(p.contentRows, vaultContentRow{kind: contentSecret, id: account.ID})
		if account.ID == p.accountID && p.contentDirectoryID == "" && p.contentServiceID == "" {
			selected = row
		}
		label := account.Name
		icon := accountIcon
		if account.Type == domain.SecretNote {
			icon = noteIcon
		}
		if p.selectedDirectory && account.ServiceID != "" {
			label += " · " + p.serviceName(account.ServiceID)
		} else if account.Type != domain.SecretNote && account.DirectoryID != "" && account.DirectoryID != p.directoryID {
			label += " · " + p.directoryPath(account.DirectoryID)
		}
		p.setContentRow(row, vaultIconPrefix(icon)+displayText(label), "")
	}
	if hasParent && len(p.contentRows) > 1 && selected == 0 {
		selected = 1
	}
	p.accountID = ""
	p.contentDirectoryID, p.contentServiceID = "", ""
	if len(p.contentRows) > 0 {
		entry := p.contentRows[selected]
		switch entry.kind {
		case contentParent:
		case contentDirectory:
			p.contentDirectoryID = entry.id
		case contentService:
			p.contentServiceID = entry.id
		case contentSecret:
			p.accountID = entry.id
			p.accountSelections[p.accountSelectionKey()] = p.accountID
		}
		p.accountsTable.Select(selected, 0)
	} else {
		message := "No entries"
		if p.selectedDirectory {
			message = "Folder is empty"
		}
		p.accountsTable.SetCell(0, 1, tview.NewTableCell(message).SetSelectable(false))
	}
	p.rendering = false
	p.refreshDetails()
}

func (p *servicesPage) selectedSecret() (*service.Secret, int, error) {
	if p.selectedDirectory {
		for i := range p.standaloneNotes {
			n := &p.standaloneNotes[i]
			if n.ID == p.accountID && n.DirectoryID == p.directoryID {
				return n, i, nil
			}
		}
		for _, item := range p.allItems {
			for index := range item.Secrets {
				if item.Secrets[index].ID == p.accountID && item.Secrets[index].DirectoryID == p.directoryID {
					return &item.Secrets[index], index, nil
				}
			}
		}
		return nil, 0, errors.New("select a secret first")
	}
	if p.selectedService < 0 || p.selectedService >= len(p.items) {
		return nil, 0, errors.New("select a service first")
	}
	for row := range p.items[p.selectedService].Secrets {
		account := &p.items[p.selectedService].Secrets[row]
		if account.ID == p.accountID {
			return account, row, nil
		}
	}
	return nil, 0, errors.New("select a secret first")
}

func (p *servicesPage) refreshDetails() {
	account, _, err := p.selectedSecret()
	if err == nil && p.modalDetails != nil {
		password := "********"
		if p.password != "" {
			password = p.password
		}
		serviceID := account.ServiceID
		if serviceID == "" {
			serviceID = p.serviceID
		}
		serviceName := p.serviceName(serviceID)
		secretType := "Password"
		codeLine := ""
		formatText := func(value string) string { return value }
		if account.Type == domain.SecretAuthCode {
			formatText = tview.Escape // Dynamic colors are enabled; user values must remain literal.
			secretType = "Auth code"
			code := p.authCode
			if p.authCodeSecretID != account.ID || code == "" {
				code = "Loading…"
			}
			codeLine = "\nCode: " + code
			if code != "Loading…" && code != "Unavailable" {
				codeLine = "\nCode: [" + commandTheme.Success.String() + "]" + code + "[-]"
			}
			if code != "Loading…" && code != "Unavailable" && p.authCodeSecondsLeft > 0 {
				codeLine += fmt.Sprintf("  (next in %ds)", p.authCodeSecondsLeft)
			}
		}
		p.modalDetails.SetText("Service: " + formatText(displayText(serviceName)) + "\nSecret: " + formatText(displayText(account.Name)) + "\nType: " + secretType + "\nFolder: " + formatText(displayText(p.directoryPath(account.DirectoryID))) + "\nSecret ID: " + formatText(account.ID) + "\nService ID: " + formatText(serviceID) + codeLine + "\n\n" + formatText(secretValueDisplay(account.Type, password)) + "\n\nSecret created: " + formatText(account.CreatedAt.Local().Format(time.DateTime)))
	}
	p.updateActions()
}

func (p *servicesPage) updateActions() {
	_, _, err := p.selectedSecret()
	p.create.SetDisabled(p.busy || p.loadError)
	_, directoriesAvailable := p.ui.credentials.(DirectoryCredentialService)
	p.createFolder.SetDisabled(p.busy || p.loadError || !directoriesAvailable)
	p.copy.SetDisabled(p.busy || p.loadError || err != nil)
	p.serviceDetails.SetDisabled(p.busy || p.loadError || p.selectedService < 0)
	p.renameServiceButton.SetDisabled(p.busy || p.loadError || p.selectedService < 0)
	p.moveService.SetDisabled(p.busy || p.loadError || p.selectedService < 0 || !directoriesAvailable)
	p.deleteServiceButton.SetDisabled(p.busy || p.loadError || p.selectedService < 0)
	p.addNote.SetDisabled(p.busy || p.loadError || p.noteService() == nil)
	p.addServiceNote.SetDisabled(p.busy || p.loadError || p.noteService() == nil || p.selectedService < 0)
	p.addSecret.SetDisabled(p.busy || p.loadError || (p.selectedService < 0 && len(p.allItems) == 0))
	p.accountDetails.SetDisabled(p.busy || p.loadError || err != nil)
	p.renameSecretButton.SetDisabled(p.busy || p.loadError || err != nil)
	p.editSecretButton.SetDisabled(p.busy || p.loadError || err != nil)
	p.moveSecret.SetDisabled(p.busy || p.loadError || err != nil || !directoriesAvailable)
	p.deleteSecretButton.SetDisabled(p.busy || p.loadError || err != nil)
	for index, button := range p.folderButtons {
		button.SetDisabled(p.busy || p.loadError || !directoriesAvailable || (index > 0 && (!p.selectedDirectory || p.directoryID == "")))
	}
	for _, button := range p.detailButtons {
		button.SetDisabled(p.busy || err != nil)
	}
	p.folderActions.refreshSelection()
	p.serviceActions.refreshSelection()
	p.actionBar.refreshSelection()
}

func (p *servicesPage) maskPasswords() {
	p.revealEpoch++
	if p.revealTimer != nil {
		p.revealTimer.Stop()
		p.revealTimer = nil
	}
	if p.password != "" {
		p.header.SetText("Secret value hidden")
	}
	p.password = ""
	p.refreshDetails()
}

func (p *servicesPage) showPassword() {
	if p.busy || p.small || p.loadError {
		return
	}
	account, _, err := p.selectedSecret()
	if err != nil {
		return
	}
	// Short layouts expose passwords only in the bounded details dialog.
	if p.modalDetails == nil {
		p.showDetails(true)
		return
	}
	p.maskPasswords()
	epoch, serviceID, accountID := p.revealEpoch, p.serviceID, p.accountID
	encrypted := account.EncryptedPassword
	p.busy = true
	p.updateActions()
	p.header.SetText("Revealing secret value…")
	var password string
	p.ui.beginTask(func(ctx context.Context) error {
		var err error
		password, err = p.ui.credentials.RevealPassword(ctx, encrypted)
		return err
	}, func(err error) {
		p.busy = false
		p.updateActions()
		if err != nil {
			p.ui.showError(err)
			return
		}
		p.completeReveal(epoch, serviceID, accountID, password)
		password = ""
	})
}

func (p *servicesPage) completeReveal(epoch uint64, serviceID, accountID, password string) {
	if epoch != p.revealEpoch || p.serviceID != serviceID || p.accountID != accountID || p.ui.currentRoute != routeServices || p.modalDetails == nil {
		return
	}
	p.password = password
	p.header.SetText("Secret value revealed · hides automatically after 15s")
	p.refreshDetails()
	p.revealTimer = time.AfterFunc(passwordRevealDuration, func() {
		if !p.ui.running.Load() {
			return
		}
		p.ui.tview.QueueUpdateDraw(func() {
			if epoch == p.revealEpoch {
				p.maskPasswords()
			}
		})
	})
}

func (p *servicesPage) copyPassword() {
	if p.busy || p.small || p.loadError {
		return
	}
	account, _, err := p.selectedSecret()
	if err != nil {
		return
	}
	if account.Type == domain.SecretAuthCode {
		p.copyAuthCode()
		return
	}
	encrypted := account.EncryptedPassword
	p.busy = true
	p.header.SetText("Copying secret value…")
	p.updateActions()
	p.ui.beginTask(func(ctx context.Context) error {
		password, err := p.ui.credentials.RevealPassword(ctx, encrypted)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return p.ui.clipboard.Copy(password)
	}, func(err error) {
		p.busy = false
		p.updateActions()
		if err != nil {
			p.ui.showError(err)
			return
		}
		message := "Secret value copied"
		if clipboard, ok := p.ui.clipboard.(interface{ TTL() time.Duration }); ok {
			message += fmt.Sprintf(" · clears in %s if unchanged", clipboard.TTL())
		}
		p.header.SetText(message)
	})
}

func (p *servicesPage) clear() {
	p.stopAuthCode()
	p.maskPasswords()
	p.allItems, p.items = nil, nil
	p.standaloneNotes = nil
	p.directories, p.treeRows = nil, nil
	p.serviceID, p.accountID = "", ""
	p.accountSelections = make(map[string]string)
	p.rendering = true
	p.search.SetText("")
	p.rendering = false
	p.renderServices()
}

func (p *servicesPage) createSecret() {
	if p.busy || p.loadError || (p.selectedService < 0 && !p.selectedDirectory) {
		return
	}
	p.ui.createDirectoryID = p.directoryID
	if p.selectedService >= 0 {
		p.ui.accountServiceID = p.serviceID
	} else {
		p.ui.accountServiceID = ""
	}
	p.ui.navigate(routeCreateSecret)
}

func (p *servicesPage) deleteService() {
	if p.busy || p.loadError || p.selectedService < 0 {
		return
	}
	item := p.items[p.selectedService]
	next := ""
	index := p.selectedService
	if index+1 < len(p.items) {
		next = p.items[index+1].ID
	} else if index > 0 {
		next = p.items[index-1].ID
	}
	count := 0
	if full := p.fullService(); full != nil {
		count = len(full.Secrets)
	}
	p.ui.confirm("Delete service", fmt.Sprintf("Delete %s (ID: %s) and all %d secrets? This cannot be undone.", displayText(item.Name), item.ID, count), func() {
		if p.closeDetails != nil {
			p.closeDetails()
		}
		p.mutate(func(ctx context.Context) error { return p.ui.credentials.DeleteService(ctx, item.ID) }, func() {
			if p.serviceID == item.ID {
				p.serviceID = next
			}
		})
	})
}

func (p *servicesPage) deleteSecret() {
	if p.busy {
		return
	}
	account, row, err := p.selectedSecret()
	if err != nil {
		return
	}
	serviceID, accountID := account.ServiceID, account.ID
	if serviceID == "" {
		serviceID = p.serviceID
	}
	name := p.serviceName(serviceID) + " / " + account.Name
	title := "Delete secret"
	isNote := account.Type == domain.SecretNote
	if isNote {
		title, name = "Delete note", account.Name
	}
	accounts := p.visibleSecrets()
	next := ""
	if row+1 < len(accounts) {
		next = accounts[row+1].ID
	} else if row > 0 {
		next = accounts[row-1].ID
	}
	p.ui.confirm(title, "Delete "+displayText(name)+" (ID: "+accountID+")? This cannot be undone.", func() {
		if p.closeDetails != nil {
			p.closeDetails()
		}
		p.mutate(func(ctx context.Context) error {
			if isNote && p.noteService() != nil {
				return p.noteService().DeleteNote(ctx, accountID)
			}
			return p.ui.credentials.DeleteSecret(ctx, serviceID, accountID)
		}, func() {
			if p.accountSelections[serviceID] == accountID {
				p.accountSelections[serviceID] = next
			}
		})
	})
}

func (p *servicesPage) mutate(work func(context.Context) error, after func()) {
	p.busy = true
	p.updateActions()
	p.header.SetText("Saving changes…")
	p.ui.beginProtectedTask(work, func(err error) {
		p.busy = false
		p.updateActions()
		if err != nil {
			p.ui.showError(err)
			return
		}
		after()
		p.load()
	})
}

// Creation APIs intentionally stay unchanged. Locate the newly created record
// by diffing IDs from the previous load, constrained by the submitted names.
func (p *servicesPage) resolveCreatedSelection(previous []service.Service) {
	if p.preferredService == "" && p.preferredSecret == "" {
		return
	}
	oldServices, oldSecrets := map[string]bool{}, map[string]bool{}
	for _, item := range previous {
		oldServices[item.ID] = true
		for _, account := range item.Secrets {
			oldSecrets[account.ID] = true
		}
	}
	for _, item := range p.allItems {
		if p.preferredService != "" && (item.Name != p.preferredService || oldServices[item.ID]) {
			continue
		}
		if p.preferredService == "" && item.ID != p.ui.accountServiceID {
			continue
		}
		for _, account := range item.Secrets {
			if account.Name == p.preferredSecret && !oldSecrets[account.ID] {
				p.serviceID, p.accountID = item.ID, account.ID
				p.selectedDirectory = false
				p.directoryID = item.DirectoryID
				if p.preferredService == "" && p.ui.createDirectoryID != item.DirectoryID {
					p.selectedDirectory = true
					p.directoryID = p.ui.createDirectoryID
					p.accountSelections["directory:"+p.directoryID] = account.ID
				} else {
					p.accountSelections[item.ID] = account.ID
				}
				p.expanded[p.directoryID] = true
				p.accountsVisible = true
				p.filterCleared = p.search.GetText() != ""
				p.returnFocus = p.accountsTable
				p.rendering = true
				p.search.SetText("")
				p.rendering = false
			}
		}
	}
	p.preferredService, p.preferredSecret = "", ""
}
