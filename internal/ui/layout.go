// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// All layout and focus changes run on the application's event goroutine.
type responsiveFlex struct {
	*tview.Flex
	layout func(width, height int)
}

func newResponsiveFlex() *responsiveFlex {
	return &responsiveFlex{Flex: tview.NewFlex()}
}

func (p *responsiveFlex) Draw(screen tcell.Screen) {
	_, _, width, height := p.GetInnerRect()
	if p.layout != nil {
		p.layout(width, height)
	}
	p.Flex.Draw(screen)
}

type boundedDialog struct {
	*tview.Flex
	content       tview.Primitive
	width, height int
	prepare       func(int, int)
}

func (p *boundedDialog) Draw(screen tcell.Screen) {
	x, y, w, h := p.GetRect()
	if p.prepare != nil {
		p.prepare(w, h)
	}
	wantedW, wantedH := min(p.width, w), min(p.height, h)
	p.content.SetRect(x+(w-wantedW)/2, y+(h-wantedH)/2, wantedW, wantedH)
	p.content.Draw(screen)
}

func (p *boundedDialog) Focus(delegate func(tview.Primitive)) { delegate(p.content) }
func (p *boundedDialog) HasFocus() bool                       { return p.content.HasFocus() }

func (p *boundedDialog) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return p.content.MouseHandler()
}

func (p *boundedDialog) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return p.content.InputHandler()
}
func (p *boundedDialog) PasteHandler() func(string, func(tview.Primitive)) {
	return p.content.PasteHandler()
}
