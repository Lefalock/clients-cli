// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/passwordgen"
	"lefalock/internal/service"
	"lefalock/internal/update"
)

type CredentialService interface {
	UpdateSecretValue(context.Context, string, string, string) error
	List(context.Context) ([]service.Service, error)
	RevealPassword(context.Context, string) (string, error)
	GenerateAuthCode(context.Context, string, time.Time) (string, error)
	CreateService(context.Context, string, string, string) error
	CreateSecret(context.Context, string, string, string) error
	RenameService(context.Context, string, string) error
	RenameSecret(context.Context, string, string, string) error
	DeleteService(context.Context, string) error
	DeleteSecret(context.Context, string, string) error
}

type NoteCredentialService interface {
	ListStandaloneNotes(context.Context) ([]service.Secret, error)
	CreateNote(context.Context, string, string, string, string) error
	UpdateNote(context.Context, string, string) error
	RenameNote(context.Context, string, string) error
	MoveNote(context.Context, string, string, string) error
	DeleteNote(context.Context, string) error
}

type DirectoryCredentialService interface {
	ListDirectories(context.Context) ([]service.Directory, error)
	CreateDirectory(context.Context, string, string) error
	RenameDirectory(context.Context, string, string) error
	MoveDirectory(context.Context, string, string) error
	DeleteDirectory(context.Context, string) error
	MoveService(context.Context, string, string) error
	MoveSecret(context.Context, string, string) error
	CreateServiceInDirectory(context.Context, string, string, string, string) error
	CreateSecretInDirectory(context.Context, string, string, string, string) error
}

type Clipboard interface {
	Copy(string) error
}

type BackupService interface {
	DefaultDirectory() (string, error)
	Create(context.Context, string) (service.BackupResult, error)
	SaveMnemonic(context.Context, string, string, string) (string, error)
	PreviewRestore(context.Context, string, string) (*service.RestorePreview, error)
	RestoreSummaryFor(*service.RestorePreview, map[string]service.RestoreDecision) (service.RestoreSummary, error)
	SuggestRestoreName(*service.RestorePreview, service.RestoreConflict, map[string]service.RestoreDecision) string
	SuggestRestoreServiceName(*service.RestorePreview, service.RestoreConflict, map[string]service.RestoreDecision) string
	ApplyRestore(context.Context, *service.RestorePreview, map[string]service.RestoreDecision) (service.RestoreSummary, error)
}

type UpdateChecker interface {
	CurrentVersion() string
	AutomaticCheck(context.Context) (update.Result, bool, error)
}

type KeyRotationService interface {
	Rotate(context.Context) (service.RotationResult, error)
}

type route string

const (
	routeServices      route = "services"
	routeCreateService route = "create-service"
	routeCreateSecret  route = "create-secret"
	routeBackup        route = "backup"
	routeSettings      route = "settings"
	routeError         route = "error"
)

type App struct {
	clearNote        func()
	generatePassword func(passwordgen.Options) (string, error)
	tview            *tview.Application
	pages            *tview.Pages
	credentials      CredentialService
	clipboard        Clipboard
	backup           BackupService
	rotation         KeyRotationService
	rotationButton   *tview.Button
	configPath       string
	updates          UpdateChecker
	errorText        *tview.TextView
	errorClose       *tview.Button
	errorRestore     tview.Primitive
	rootCtx          context.Context
	cancelRoot       context.CancelFunc
	currentRoute     route
	focusOrder       map[route][]tview.Primitive

	running              atomic.Bool
	generation           atomic.Uint64
	taskMu               sync.Mutex
	taskCancel           context.CancelFunc
	services             *servicesPage
	accountServiceID     string
	createDirectoryID    string
	navigationLocked     atomic.Bool
	navigationLockID     atomic.Uint64
	modalDepth           atomic.Int32
	modalFocus           tview.Primitive
	restoreUndoPreview   *service.RestorePreview
	restoreUndoDecisions map[string]service.RestoreDecision
	shell                *commandShell
	contentFocus         tview.Primitive
}

func New(credentials CredentialService, backup BackupService, updates UpdateChecker, clipboard Clipboard, configPath string) (*App, error) {
	if credentials == nil || backup == nil || updates == nil || clipboard == nil {
		return nil, errors.New("UI dependencies are required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	u := &App{
		tview: tview.NewApplication(), pages: tview.NewPages(),
		credentials: credentials, backup: backup, configPath: configPath, updates: updates,
		clipboard: clipboard,
		rootCtx:   ctx, cancelRoot: cancel, currentRoute: routeServices,
		focusOrder: make(map[route][]tview.Primitive),
	}
	u.pages.SetBackgroundColor(tcell.ColorBlack)
	u.buildPages()
	u.shell = newCommandShell(u.pages, u.navigate)
	u.shell.onTooSmall = u.services.maskPasswords
	u.shell.setVersion(updates.CurrentVersion())
	u.shell.setRoute(routeServices)
	u.pages.SwitchToPage(string(routeServices))
	u.focusFirst(routeServices)
	u.installModalMouseTrap()
	return u, nil
}

func (u *App) SetKeyRotation(rotation KeyRotationService) {
	u.rotation = rotation
	if u.rotationButton != nil {
		u.rotationButton.SetDisabled(rotation == nil)
	}
}

func (u *App) Run() error {
	return u.RunContext(context.Background())
}

// RunContext stops the terminal UI on cancellation. Watching starts at the first
// draw, after tview has initialized its screen, so early cancellation cannot be
// lost by calling Stop before Run has started.
func (u *App) RunContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	watchCtx, stopWatching := context.WithCancel(ctx)
	var watchers sync.WaitGroup
	var startWatching sync.Once
	defer func() { stopWatching(); watchers.Wait() }()
	u.running.Store(true)
	defer func() {
		u.running.Store(false)
		u.cancelRoot()
		u.cancelTask()
		if u.clearNote != nil {
			u.clearNote()
			u.clearNote = nil
		}
	}()
	u.navigate(routeServices)

	u.tview.SetInputCapture(u.captureKeyboard)
	u.tview.SetBeforeDrawFunc(func(tcell.Screen) bool {
		startWatching.Do(func() {
			watchers.Add(1)
			go func() {
				defer watchers.Done()
				<-watchCtx.Done()
				if ctx.Err() != nil {
					u.tview.Stop()
				}
			}()
		})
		u.updateFooter()
		return false
	})
	u.watchForUpdates()

	return u.tview.SetRoot(u.shell.root, true).EnableMouse(true).Run()
}

func (u *App) watchForUpdates() {
	go func() {
		ticker := time.NewTicker(update.AutomaticCheckInterval)
		defer ticker.Stop()
		for {
			u.checkForUpdates()
			select {
			case <-u.rootCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (u *App) checkForUpdates() {
	result, checked, err := u.updates.AutomaticCheck(u.rootCtx)
	if err != nil || !checked || !u.running.Load() || u.rootCtx.Err() != nil {
		return
	}
	u.tview.QueueUpdateDraw(func() {
		if u.shell == nil {
			return
		}
		if result.InstallError != "" {
			u.shell.setUpdateError(result.InstallError)
		} else if result.Available {
			u.shell.setUpdateAvailable(result.Latest)
		} else {
			u.shell.clearUpdateStatus()
		}
	})
}

func (u *App) buildPages() {
	u.services = newServicesPage(u)
	u.pages.AddPage(string(routeServices), u.services.primitive(), true, false)
	u.pages.AddPage(string(routeBackup), u.backupPage(), true, false)
	u.pages.AddPage(string(routeSettings), u.settingsPage(), true, false)

	dialog, message, closeButton := borderedDialog("Unexpected error", tcell.ColorRed, func() {
		u.pages.HidePage(string(routeError))
		u.popModal()
		if u.errorRestore != nil {
			u.tview.SetFocus(u.errorRestore)
		} else {
			u.focusFirst(u.currentRoute)
		}

	})
	u.errorText = message
	u.errorClose = closeButton
	u.pages.AddPage(string(routeError), dialog, true, false)
}

func (u *App) navigate(next route) {
	if u.navigationLocked.Load() || u.modalDepth.Load() > 0 {
		return
	}
	if next == routeCreateService || next == routeCreateSecret {
		if u.currentRoute != routeServices {
			u.navigate(routeServices)
			u.tview.SetFocus(u.services.servicesTable)
		}
		u.openCreateModal(next)
		return
	}
	if u.currentRoute == routeServices && next != routeServices {
		focus := u.tview.GetFocus()
		if u.shell.ownsNavigation(focus) {
			focus = u.contentFocus
		}
		u.services.returnFocus = focus
	}
	u.cancelTask()
	u.generation.Add(1)
	if u.services != nil {
		u.services.maskPasswords()
		u.services.busy = false

	}
	u.pages.SwitchToPage(string(next))
	u.contentFocus = nil
	u.currentRoute = next
	u.shell.setRoute(next)
	if next == routeServices {
		u.services.load()
		return
	}
	u.focusFirst(next)
}

func (u *App) showError(err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	u.errorRestore = u.tview.GetFocus()
	u.errorText.SetText(err.Error())
	u.pushModal()
	u.pages.ShowPage(string(routeError)).SendToFront(string(routeError))
	u.tview.SetFocus(u.errorClose)
}

func (u *App) setFocusOrder(page route, controls ...tview.Primitive) {
	u.focusOrder[page] = controls
}

func (u *App) focusFirst(page route) {
	if page == routeServices && u.services != nil {
		u.tview.SetFocus(u.services.servicesTable)
		return
	}
	if controls := u.focusOrder[page]; len(controls) > 0 {
		u.focusRelative(controls, len(controls)-1, 1)
	}
}

func (u *App) captureKeyboard(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape && !u.navigationLocked.Load() && u.services != nil && u.services.closeDetails != nil {
		name, _ := u.pages.GetFrontPage()
		if name == "secret-details" || name == "service-details" || u.modalDepth.Load() == 1 {
			u.services.closeDetails()
			return nil
		}
	}
	if u.shell.tooSmall {
		if event.Key() == tcell.KeyCtrlC {
			return event
		}
		return nil
	}
	if event.Key() == tcell.KeyF6 && u.modalDepth.Load() == 0 && !u.navigationLocked.Load() {
		if u.shell.ownsNavigation(u.tview.GetFocus()) {
			if u.contentFocus != nil {
				u.tview.SetFocus(u.contentFocus)
			} else {
				u.focusFirst(u.currentRoute)
			}
		} else {
			u.contentFocus = u.tview.GetFocus()
			u.shell.focusNavigation(u.tview)
		}
		return nil
	}
	if u.modalDepth.Load() == 0 && u.shell.ownsNavigation(u.tview.GetFocus()) {
		switch event.Key() {
		case tcell.KeyLeft, tcell.KeyBacktab:
			u.shell.moveNavigation(u.tview, -1)
			return nil
		case tcell.KeyRight, tcell.KeyTab:
			u.shell.moveNavigation(u.tview, 1)
			return nil
		case tcell.KeyEscape:
			if u.contentFocus != nil {
				u.tview.SetFocus(u.contentFocus)
			} else {
				u.focusFirst(u.currentRoute)
			}
			return nil
		}
	}
	if event.Key() == tcell.KeyCtrlK {
		u.openCommandPalette()
		return nil
	}
	focus := u.tview.GetFocus()
	if u.modalDepth.Load() > 0 {
		_, front := u.pages.GetFrontPage()
		if front != nil && !front.HasFocus() {
			front.Focus(func(primitive tview.Primitive) { u.tview.SetFocus(primitive) })
		}
		return event
	}
	if u.currentRoute == routeServices && !u.shell.ownsNavigation(focus) {
		return u.services.captureKeyboard(event)
	}
	controls := u.focusOrder[u.currentRoute]
	index := focusIndex(controls, focus)
	if index < 0 {
		return event
	}
	switch event.Key() {
	case tcell.KeyEnter:
		if _, editing := focus.(*tview.InputField); editing && isFormRoute(u.currentRoute) {
			u.focusRelative(controls, index, 1)
			return nil
		}
	case tcell.KeyTab, tcell.KeyBacktab:
		step := 1
		if event.Key() == tcell.KeyBacktab {
			step = -1
		}
		u.focusRelative(controls, index, step)
		return nil
	case tcell.KeyUp:
		if _, text := focus.(*tview.TextView); text {
			return event
		}
		u.focusRelative(controls, index, -1)
		return nil
	case tcell.KeyDown:
		if _, text := focus.(*tview.TextView); text {
			return event
		}
		u.focusRelative(controls, index, 1)
		return nil
	case tcell.KeyLeft:
		if isButton(focus) {
			u.focusRelative(controls, index, -1)
			return nil
		}
	case tcell.KeyRight:
		if isButton(focus) {
			u.focusRelative(controls, index, 1)
			return nil
		}
	case tcell.KeyEscape:
		if isFormRoute(u.currentRoute) {
			return event
		}
		u.goBack()
		return nil
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if _, editing := focus.(*tview.InputField); !editing {
			u.goBack()
			return nil
		}
	}
	return event
}

func (u *App) focusRelative(controls []tview.Primitive, index, step int) {
	if len(controls) == 0 {
		return
	}
	for offset := 1; offset <= len(controls); offset++ {
		next := (index + step*offset + len(controls)*2) % len(controls)
		if button, ok := controls[next].(*tview.Button); ok && button.IsDisabled() {
			continue
		}
		u.tview.SetFocus(controls[next])
		return
	}
}

func (u *App) goBack() {
	switch u.currentRoute {
	case routeServices:
		return
	case routeBackup, routeSettings:
		u.navigate(routeServices)
	case routeCreateService, routeCreateSecret:
		u.navigate(routeServices)
	}
}

func focusIndex(controls []tview.Primitive, focus tview.Primitive) int {
	for index, control := range controls {
		_, valueField := control.(*secretValueInput)
		if control == focus || ((valueField || isNoteTextArea(control)) && control.HasFocus()) {
			return index
		}
	}
	return -1
}

func isButton(value tview.Primitive) bool {
	switch value.(type) {
	case *tview.Button:
		return true
	default:
		return false
	}
}

func isFormRoute(value route) bool {
	switch value {
	case routeCreateService, routeCreateSecret:
		return true
	default:
		return false
	}
}

func (u *App) beginTask(work func(context.Context) error, complete func(error)) {
	u.beginTaskWithNavigationLock(false, work, complete)
}

func (u *App) beginProtectedTask(work func(context.Context) error, complete func(error)) {
	u.beginTaskWithNavigationLock(true, work, complete)
}

func (u *App) pushModal() {
	if u.services != nil {
		u.services.maskPasswords()
	}
	u.modalDepth.Add(1)
}

func (u *App) popModal() {
	if u.modalDepth.Load() > 0 {
		u.modalDepth.Add(-1)
	}
}

func (u *App) beginTaskWithNavigationLock(lockNavigation bool, work func(context.Context) error, complete func(error)) {
	u.cancelTask()
	lockID := uint64(0)
	if lockNavigation {
		lockID = u.navigationLockID.Add(1)
		u.navigationLocked.Store(true)
	}
	unlockNavigation := func() {
		if lockNavigation && u.navigationLockID.Load() == lockID {
			u.navigationLocked.Store(false)
		}
	}
	ctx, cancel := context.WithCancel(u.rootCtx)
	u.taskMu.Lock()
	u.taskCancel = cancel
	u.taskMu.Unlock()
	generation := u.generation.Load()
	go func() {
		err := work(ctx)
		if ctx.Err() != nil || !u.running.Load() {
			unlockNavigation()
			return
		}
		u.tview.QueueUpdateDraw(func() {
			if generation != u.generation.Load() || ctx.Err() != nil {
				unlockNavigation()
				return
			}
			unlockNavigation()
			complete(err)
		})
	}()
}

func (u *App) cancelTask() {
	u.taskMu.Lock()
	cancel := u.taskCancel
	u.taskCancel = nil
	u.taskMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func centered(width, height int, primitive tview.Primitive) tview.Primitive {
	return &boundedDialog{Flex: tview.NewFlex(), content: primitive, width: width, height: height}
}

func (u *App) updateFooter() {
	if u.navigationLocked.Load() {
		u.shell.footer.SetText("Working... Please wait for the operation to finish.")
	} else if u.shell.navigation.HasFocus() {
		u.shell.footer.SetText("←→ Sections · Enter Open · F6 Return · Ctrl+K Commands")
	} else if u.modalDepth.Load() > 0 {
		name, _ := u.pages.GetFrontPage()
		hint := "Tab Actions · Enter Select · PgUp/PgDn Scroll"
		if name == "edit-secret-value" {
			hint = "Tab Next · Enter Save · Esc Cancel"
		}
		if name == string(routeCreateService) || name == string(routeCreateSecret) {
			hint = "Tab Next · Enter Next/submit"
		}
		if name == "backup-created" {
			hint = "Tab Actions · PgUp/PgDn Words"
		}
		if name == "directory-browser" {
			hint = "Enter Open · Tab Actions"
		}
		if name == "backup-file-browser" {
			hint = "Enter Open backup file"
		}
		if name == "confirmation" {
			hint = "Tab Choose · Enter Confirm"
		}
		u.shell.footer.SetText(hint)
	} else if u.currentRoute == routeCreateService || u.currentRoute == routeCreateSecret {
		u.shell.footer.SetText("Tab Next · Enter Next/submit · Esc Cancel")
	} else if u.currentRoute != routeServices {
		u.shell.footer.SetText("Tab Next · Enter Select · Esc Back · F6 · Ctrl+K Menu")
	} else {
		u.services.updateFooter()
	}
}
