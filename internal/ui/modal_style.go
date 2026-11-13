// SPDX-License-Identifier: MPL-2.0

package ui

import "github.com/rivo/tview"

// styleModalFrame gives detail dialogs and forms the same black surface,
// border, title and close mark.
func styleModalFrame(view *tview.Box, title string, close func()) {
	view.SetBorder(true).SetTitle(" " + title + " ").SetBorderColor(commandTheme.Border).SetBackgroundColor(commandTheme.Background)
	addModalCloseMark(view, close)
}

func styleModalInput(field *tview.InputField) {
	field.SetBackgroundColor(commandTheme.Background)
	field.SetFieldBackgroundColor(commandTheme.Background)
	field.SetPlaceholderStyle(field.GetPlaceholderStyle().Background(commandTheme.Background))
}
