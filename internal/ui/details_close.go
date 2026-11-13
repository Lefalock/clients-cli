// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// addModalCloseMark gives every bordered modal the same clickable close mark.
// Draw and mouse handling are attached to the border, so resizing keeps the
// mark in the top-right corner regardless of the modal's contents.
func addModalCloseMark(view *tview.Box, close func()) {
	previousDraw := view.GetDrawFunc()
	view.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		innerX, innerY, innerWidth, innerHeight := x+1, y+1, max(width-2, 0), max(height-2, 0)
		if previousDraw != nil {
			innerX, innerY, innerWidth, innerHeight = previousDraw(screen, x, y, width, height)
		}
		if width >= 4 && height >= 2 {
			style := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(view.GetBackgroundColor()).Attributes(view.GetBorderAttributes() | tcell.AttrBold)
			for offset, mark := range []rune{'[', 'X', ']'} {
				screen.SetContent(x+width-3+offset, y, mark, nil, style)
			}
		}
		return innerX, innerY, innerWidth, innerHeight
	})
	previousMouse := view.GetMouseCapture()
	view.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		if action == tview.MouseLeftClick {
			x, y := event.Position()
			left, top, width, _ := view.GetRect()
			if width >= 4 && y == top && x >= left+width-3 && x < left+width {
				close()
				return tview.MouseConsumed, nil
			}
		}
		if previousMouse != nil {
			return previousMouse(action, event)
		}
		return action, event
	})
}
