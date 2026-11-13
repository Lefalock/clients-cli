// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/recovery"
	"lefalock/internal/service"
)

type RecoveryBackend interface {
	Status(context.Context) (recovery.Status, error)
	Begin(context.Context) error
	BeginEmpty(context.Context) error
	ContinueEmpty(context.Context) error
	Preview(context.Context, string, string) (*service.RestorePreview, error)
	Summary(*service.RestorePreview) (service.RestoreSummary, error)
	Apply(context.Context, *service.RestorePreview) error
	Activate(context.Context) error
	InspectPointerRepair(context.Context) (*recovery.PointerRepairPreview, error)
	RepairPointer(context.Context, *recovery.PointerRepairPreview) error
	Cancel(context.Context) error
}

type startupRecovery struct {
	app         *tview.Application
	pages       *tview.Pages
	backend     RecoveryBackend
	retry       func() error
	recoverable func(error) bool
	cause       error
	directory   string
	ctx         context.Context
	cancel      context.CancelFunc
	busy        bool
	proceed     bool
}

// RunStartupRecovery opens independently of the normal App and its services.
// A false result means the user exited; a true result means startup can continue.
func RunStartupRecovery(cause error, backend RecoveryBackend, retry func() error, recoverable func(error) bool, directory string) (bool, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &startupRecovery{app: tview.NewApplication(), pages: tview.NewPages(), backend: backend, retry: retry, recoverable: recoverable, cause: cause, directory: directory, ctx: ctx, cancel: cancel}
	r.app.SetRoot(r.pages, true).EnableMouse(true)
	r.showStart()
	err := r.app.Run()
	return r.proceed, err
}

func (r *startupRecovery) show(title string, content tview.Primitive, focus tview.Primitive) {
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(content, 0, 1, true)
	panel.SetBorder(true).SetTitle(" " + title + " ")
	r.pages.RemovePage("screen")
	r.pages.AddPage("screen", centered(84, 25, panel), true, true)
	r.app.SetFocus(focus)
}

// Disk and keyring work runs outside the terminal event loop. During a task
// navigation is locked; exit remains available after its outcome is known.
func (r *startupRecovery) task(work func(context.Context) error, done func(error)) {
	if r.busy {
		return
	}
	r.busy = true
	r.show("Vault recovery", tview.NewTextView().SetText("Working... Please wait for the operation to finish."), nil)
	r.app.SetInputCapture(func(*tcell.EventKey) *tcell.EventKey { return nil })
	r.app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		return nil, action
	})
	go func() {
		err := work(r.ctx)
		r.app.QueueUpdateDraw(func() {
			r.busy = false
			r.app.SetInputCapture(nil)
			r.app.SetMouseCapture(nil)
			done(err)
		})
	}()
}

func (r *startupRecovery) showError(err error, back func()) {
	form := tview.NewForm().AddTextView("", tview.Escape(displayText(err.Error())), 70, 10, true, false).
		AddButton("Back", back)
	r.show("Recovery error", form, form)
}

func (r *startupRecovery) showStart() {
	status, statusErr := r.backend.Status(r.ctx)
	ready := status.Ready || (!status.Empty && status.Imported)
	text := "The current vault could not be opened.\n"
	if r.cause != nil {
		text += "\n" + r.cause.Error() + "\n"
	}
	if statusErr != nil {
		text += "\nRecovery status: " + statusErr.Error()
	}
	if status.Empty {
		text += "\nCreate a separate empty vault. No backup will be imported. The old files will be preserved, and the new vault will stay local until explicit sign-in."
	} else if ready {
		text += "\nThe recovered database and its key will be preserved."
	} else if !status.Pending {
		text += "\nCreate an empty vault or restore a backup into a separate vault with a new key. The old database and key files will be preserved. Backup restore requires its recovery phrase."
	} else {
		text += "\nRestore a backup into a separate vault with a new key. The old database and key files will be preserved. You need the backup's recovery phrase."
	}
	if status.Pending {
		text += "\n\nAn unfinished recovery was found."
	}
	if status.Pending && !status.CanDiscard && status.DiscardReason != "" {
		text += "\n\n" + status.DiscardReason + " The database, key and recovery journal will be preserved."
	}
	if ready {
		if !status.Empty {
			text += " The backup was already imported."
		}
		if status.CanRepairPointer {
			text += " Verify the recovered vault to restore its active pointer."
		}
	}
	if status.ActivationBlocked != "" {
		text += "\n\n" + status.ActivationBlocked
	}
	form := tview.NewForm().AddTextView("", tview.Escape(displayText(text)), 72, 13, true, false)
	if status.Pending {
		if ready && statusErr == nil {
			if status.CanRepairPointer {
				form.AddButton("Repair vault pointer", r.inspectPointerRepair)
			} else if status.ActivationBlocked == "" {
				if status.Empty {
					form.AddButton("Finish creation", func() { r.finishEmpty() })
				} else {
					form.AddButton("Finish recovery", func() { r.finish() })
				}
			}
		} else if statusErr == nil {
			if status.Empty {
				form.AddButton("Continue creation", r.finishEmpty)
			} else {
				form.AddButton("Continue recovery", r.chooseFile)
			}
		}
		if status.CanDiscard {
			form.AddButton("Discard attempt", func() {
				r.task(r.backend.Cancel, func(err error) {
					if err != nil {
						r.showError(err, r.showStart)
					} else {
						r.cause = r.retry()
						r.showStart()
					}
				})
			})
		}
	} else if statusErr == nil && r.recoverable(r.cause) {
		form.AddButton("Create empty vault", r.confirmEmpty)
		form.AddButton("Restore backup", func() {
			r.task(r.backend.Begin, func(err error) {
				if err != nil {
					r.showError(err, r.showStart)
				} else {
					r.chooseFile()
				}
			})
		})
	}
	form.AddButton("Retry opening", func() {
		r.task(func(context.Context) error {
			status, err := r.backend.Status(r.ctx)
			if err != nil {
				return err
			}
			if status.Pending {
				return fmt.Errorf("finish or discard the recovery attempt first")
			}
			return r.retry()
		}, func(err error) {
			if err != nil {
				r.cause = err
				r.showStart()
			} else {
				r.proceed = true
				r.app.Stop()
			}
		})
	}).AddButton("Exit", func() { r.cancel(); r.app.Stop() })
	r.show("Vault recovery", form, form)
}

func (r *startupRecovery) confirmEmpty() {
	text := "Create a new empty vault with a new local encryption key.\n\nNo backup will be imported and no old records will be recovered. The old database, remaining key shares and original metadata will be preserved.\n\nThe new vault will stay local until you explicitly sign in. A working system keyring is required."
	form := tview.NewForm().AddTextView("", text, 72, 13, true, false)
	form.AddButton("Create and open", func() {
		r.task(func(ctx context.Context) error {
			if err := r.backend.BeginEmpty(ctx); err != nil {
				return err
			}
			return r.backend.Activate(ctx)
		}, r.creationDone)
	}).AddButton("Back", r.showStart)
	r.show("Create empty vault", form, form)
}

func (r *startupRecovery) finishEmpty() {
	r.task(r.backend.ContinueEmpty, r.creationDone)
}

func (r *startupRecovery) creationDone(err error) {
	if err != nil {
		r.showError(err, r.showStart)
	} else {
		r.proceed = true
		r.app.Stop()
	}
}

func (r *startupRecovery) chooseFile() {
	initial := filepath.Clean(r.directory)
	for {
		if stat, err := os.Stat(initial); err == nil && stat.IsDir() {
			break
		}
		parent := filepath.Dir(initial)
		if parent == initial {
			break
		}
		initial = parent
	}
	panel, focus, err := buildBackupFileBrowser(initial, r.askPhrase, r.showStart, func(err error) { r.showError(err, r.chooseFile) })
	if err != nil {
		r.showError(err, r.showStart)
		return
	}
	r.show("Choose backup", panel, focus)
}

func (r *startupRecovery) askPhrase(path string) {
	form := tview.NewForm()
	form.AddTextView("File", tview.Escape(displayText(path)), 70, 3, true, false)
	form.AddPasswordField("Recovery phrase", "", 58, '*', nil)
	phrase := form.GetFormItem(1).(*tview.InputField)
	form.AddButton("Inspect backup", func() {
		words := phrase.GetText()
		phrase.SetText("")
		var preview *service.RestorePreview
		r.task(func(ctx context.Context) error {
			var err error
			preview, err = r.backend.Preview(ctx, path, words)
			words = ""
			return err
		}, func(err error) {
			if err != nil {
				r.showError(err, func() { r.askPhrase(path) })
			} else {
				r.confirm(preview)
			}
		})
	}).AddButton("Back", func() { phrase.SetText(""); r.showStart() })
	r.show("Unlock backup", form, phrase)
}

func (r *startupRecovery) confirm(preview *service.RestorePreview) {
	summary, err := r.backend.Summary(preview)
	if err != nil {
		r.showError(err, r.showStart)
		return
	}
	text := fmt.Sprintf("Restore into a new vault\n\nFolders: %d\nServices: %d\nSecrets: %d\n\nThe old vault files will remain. The new vault will stay local until you explicitly connect an account.", summary.DirectoriesAdded, summary.ServicesAdded, summary.SecretsAdded)
	form := tview.NewForm().AddTextView("", text, 72, 12, true, false)
	form.AddButton("Restore and open", func() {
		r.task(func(ctx context.Context) error {
			if err := r.backend.Apply(ctx, preview); err != nil {
				return err
			}
			return r.backend.Activate(ctx)
		}, func(err error) {
			if err != nil {
				r.showError(err, r.showStart)
			} else {
				r.proceed = true
				r.app.Stop()
			}
		})
	}).AddButton("Back", r.showStart)
	r.show("Confirm recovery", form, form)
}

func (r *startupRecovery) finish() {
	r.task(r.backend.Activate, func(err error) {
		if err != nil {
			r.showError(err, r.showStart)
		} else {
			r.proceed = true
			r.app.Stop()
		}
	})
}

func (r *startupRecovery) inspectPointerRepair() {
	var preview *recovery.PointerRepairPreview
	r.task(func(ctx context.Context) error {
		var err error
		preview, err = r.backend.InspectPointerRepair(ctx)
		return err
	}, func(err error) {
		if err != nil {
			r.showError(err, r.showStart)
			return
		}
		r.confirmPointerRepair(preview)
	})
}

func (r *startupRecovery) confirmPointerRepair(preview *recovery.PointerRepairPreview) {
	state := "The damaged pointer will be saved in a separate archive."
	if preview.MissingPointer {
		state = "The pointer is missing. Its absence will be recorded in the archive."
	}
	text := fmt.Sprintf("Verified recovered vault: %s\n\nFolders: %d\nServices: %d\nSecrets: %d\n\n%s\n\nConfirm making this recovered vault active. Its existing database and key will be used.", preview.VaultID, preview.Directories, preview.Services, preview.Secrets, state)
	form := tview.NewForm().AddTextView("", tview.Escape(displayText(text)), 72, 13, true, false)
	form.AddButton("Repair and open", func() {
		r.task(func(ctx context.Context) error { return r.backend.RepairPointer(ctx, preview) }, func(err error) {
			if err != nil {
				r.showError(err, r.showStart)
			} else {
				r.proceed = true
				r.app.Stop()
			}
		})
	}).AddButton("Back", r.showStart)
	r.show("Repair vault pointer", form, form)
}
