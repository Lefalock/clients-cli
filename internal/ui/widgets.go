// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func borderedButton(label string, selected func()) *tview.Button {
	button := tview.NewButton(label).SetSelectedFunc(selected)
	button.SetBorder(true).SetBorderColor(tcell.ColorWhite)
	return button
}

func borderedDialog(text string, background tcell.Color, close func()) (tview.Primitive, *tview.TextView, *tview.Button) {
	message := tview.NewTextView().
		SetText(text).
		SetTextAlign(tview.AlignCenter).
		SetWrap(true)
	message.SetBackgroundColor(background)

	button := borderedButton("Close", close)
	button.SetExitFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape && close != nil {
			close()
		}
	})
	button.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if (event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2) && close != nil {
			close()
			return nil
		}
		return event
	})

	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(message, 0, 1, false).AddItem(button, 3, 0, true)
	panel.SetBorder(true).SetBorderColor(commandTheme.Danger)
	addModalCloseMark(panel.Box, close)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyUp, tcell.KeyDown:
			message.InputHandler()(event, func(tview.Primitive) {})
			return nil
		}
		return event
	})

	return centered(72, 12, panel), message, button
}

// Visible brackets make actions recognizable even without terminal colours.
func actionButton(label string, selected func()) *tview.Button {
	normal := tview.Escape("[" + label + "]")
	button := compactButton(normal, selected)
	button.SetFocusFunc(func() { button.SetLabel("> " + normal + " <") })
	button.SetBlurFunc(func() { button.SetLabel(normal) })
	return button
}
