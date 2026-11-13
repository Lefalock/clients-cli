// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/service"
)

func (u *App) settingsPage() tview.Primitive {
	panel := tview.NewFlex().SetDirection(tview.FlexRow)
	panel.SetBorder(true).SetTitle(" Settings ").SetBorderColor(commandTheme.Border)
	panel.AddItem(tview.NewTextView().SetText("Rotate the local vault encryption key. Existing backup files keep their own recovery phrases.").SetWrap(true), 4, 0, false)
	button := actionButton("Rotate encryption key", nil)
	button.SetDisabled(u.rotation == nil)
	u.rotationButton = button
	button.SetSelectedFunc(func() {
		if u.rotation == nil {
			return
		}
		u.confirm("Rotate encryption key", "The client will re-encrypt every local record. Keep this client open until the operation finishes. The client will close after success; then open it again.", func() {
			button.SetDisabled(true)
			button.SetLabel("[Rotating encryption key...]")
			var result service.RotationResult
			u.beginProtectedTask(func(ctx context.Context) error {
				var err error
				result, err = u.rotation.Rotate(ctx)
				return err
			}, func(err error) {
				button.SetLabel("[Rotate encryption key]")
				button.SetDisabled(false)
				if err != nil {
					u.showError(err)
					return
				}
				u.showRotationComplete(result)
			})
		})
	})
	panel.AddItem(button, 1, 0, true)
	back := actionButton("Return to Vault", func() { u.navigate(routeServices) })
	panel.AddItem(back, 1, 0, false)
	u.setFocusOrder(routeSettings, button, back)
	return centered(72, 11, panel)
}

func (u *App) showRotationComplete(result service.RotationResult) {
	message := "Encryption key rotated. Close and reopen the client to continue."
	if result.Warning != nil {
		message += "\n\nCleanup is pending and will be retried on next start: " + result.Warning.Error()
	}
	text := tview.NewTextView().SetText(message).SetWrap(true)
	text.SetBorder(true).SetTitle(" Rotation complete ").SetBorderColor(commandTheme.Focus)
	exit := actionButton("Close client", func() { u.tview.Stop() })
	panel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(text, 0, 1, false).
		AddItem(exit, 1, 0, true)
	panel.SetBorder(true).SetBorderColor(tcell.ColorAqua)
	u.pages.AddPage("rotation-complete", centered(72, 11, panel), true, true)
	u.pushModal()
	u.tview.SetFocus(exit)
}
