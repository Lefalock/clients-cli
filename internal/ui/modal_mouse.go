// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// A visible modal owns all mouse input. Events outside its bounds are ignored;
// events inside it never fall through to the page drawn behind it.
func (u *App) installModalMouseTrap() {
	u.tview.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		if event == nil {
			return nil, tview.MouseConsumed
		}
		if u.modalDepth.Load() == 0 {
			return event, action
		}
		_, front := u.pages.GetFrontPage()
		dialog, ok := front.(*boundedDialog)
		if !ok {
			return nil, tview.MouseConsumed
		}
		x, y := event.Position()
		left, top, width, height := dialog.content.GetRect()
		if x < left || x >= left+width || y < top || y >= top+height {
			return nil, tview.MouseConsumed
		}
		return event, action
	})
	u.pages.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if u.modalDepth.Load() == 0 {
			return action, event
		}
		_, front := u.pages.GetFrontPage()
		if front != nil {
			if handler := front.MouseHandler(); handler != nil {
				handler(action, event, func(primitive tview.Primitive) { u.tview.SetFocus(primitive) })
			}
		}
		return tview.MouseConsumed, nil
	})
}
