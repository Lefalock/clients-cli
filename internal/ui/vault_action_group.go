// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// vaultActionGroup keeps keyboard focus on the button block while its selection moves.
type vaultActionGroup struct {
	*tview.Flex
	buttons  []*tview.Button
	slots    [8]*tview.Button
	labels   []string
	selected int
}

func newVaultActionGroup(buttons ...*tview.Button) *vaultActionGroup {
	g := &vaultActionGroup{Flex: tview.NewFlex().SetDirection(tview.FlexRow), buttons: buttons}
	for _, button := range buttons {
		g.labels = append(g.labels, button.GetLabel())
		button.SetDisabledStyle(tcell.StyleDefault.Background(commandTheme.Background).Foreground(commandTheme.Muted))
	}
	g.SetBackgroundColor(commandTheme.Background)
	return g
}

func (g *vaultActionGroup) Focus(delegate func(tview.Primitive)) {
	g.Flex.Box.Focus(delegate)
	g.refreshSelection()
}

func (g *vaultActionGroup) Blur() {
	g.Flex.Box.Blur()
	g.refreshSelection()
}

func (g *vaultActionGroup) Draw(screen tcell.Screen) {
	g.refreshSelection()
	g.Flex.Draw(screen)
}

func (g *vaultActionGroup) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return func(event *tcell.EventKey, setFocus func(tview.Primitive)) {
		if g.Flex.Box.HasFocus() {
			if event.Key() == tcell.KeyEnter && g.available() {
				g.buttons[g.selected].InputHandler()(event, setFocus)
			}
			return
		}
		g.Flex.InputHandler()(event, setFocus)
	}
}

func (g *vaultActionGroup) available() bool {
	for _, button := range g.buttons {
		if !button.IsDisabled() {
			return true
		}
	}
	return false
}

func (g *vaultActionGroup) selectButton(button *tview.Button) {
	for i, candidate := range g.buttons {
		if candidate == button {
			g.selected = i
			return
		}
	}
}

func (g *vaultActionGroup) rows() int {
	return 4
}

func (g *vaultActionGroup) selectedRow() int {
	return g.selectedSlot() % g.rows()
}

func (g *vaultActionGroup) selectedSlot() int {
	if g.selected >= 0 && g.selected < len(g.buttons) {
		for slot, button := range g.slots {
			if button != nil && button == g.buttons[g.selected] {
				return slot
			}
		}
	}
	return 0
}

func (g *vaultActionGroup) indexAt(slot int) (int, bool) {
	if slot < 0 || slot >= len(g.slots) || g.slots[slot] == nil || g.slots[slot].IsDisabled() {
		return -1, false
	}
	for index, button := range g.buttons {
		if button == g.slots[slot] {
			return index, true
		}
	}
	return -1, false
}

func (g *vaultActionGroup) nearestEnabled(column, row int) (int, bool) {
	rows := g.rows()
	best, distance := -1, rows+1
	for candidateRow := 0; candidateRow < rows; candidateRow++ {
		index, ok := g.indexAt(column*rows + candidateRow)
		if !ok {
			continue
		}
		delta := candidateRow - row
		if delta < 0 {
			delta = -delta
		}
		if delta < distance {
			best, distance = index, delta
		}
	}
	return best, best >= 0
}

func (g *vaultActionGroup) selectEntry(row, preferredColumn int) {
	for _, column := range []int{preferredColumn, 1 - preferredColumn} {
		if index, ok := g.nearestEnabled(column, row); ok {
			g.selected = index
			g.refreshSelection()
			return
		}
	}
}

func (g *vaultActionGroup) moveVertical(step int) {
	rows := g.rows()
	slot := g.selectedSlot()
	column, row := slot/rows, slot%rows
	for offset := 1; offset <= rows; offset++ {
		next := (row + step*offset + rows*2) % rows
		if index, ok := g.indexAt(column*rows + next); ok {
			g.selected = index
			g.refreshSelection()
			return
		}
	}
}

func (g *vaultActionGroup) moveHorizontal(step int) bool {
	rows := g.rows()
	column := g.selectedSlot() / rows
	if nextColumn := column + step; nextColumn >= 0 && nextColumn <= 1 {
		if index, ok := g.nearestEnabled(nextColumn, g.selectedRow()); ok {
			g.selected = index
			g.refreshSelection()
			return true
		}
	}
	return false
}

func (g *vaultActionGroup) refreshSelection() {
	if g.Flex.Box.HasFocus() && (g.selected < 0 || g.selected >= len(g.buttons) || g.buttons[g.selected].IsDisabled()) {
		for i, button := range g.buttons {
			if !button.IsDisabled() {
				g.selected = i
				break
			}
		}
	}
	for i, button := range g.buttons {
		style := tcell.StyleDefault.Background(commandTheme.Background).Foreground(commandTheme.Text)
		label := g.labels[i]
		if g.Flex.Box.HasFocus() && i == g.selected && !button.IsDisabled() {
			style = tcell.StyleDefault.Background(commandTheme.Focus).Foreground(tcell.ColorBlack).Bold(true)
			_, _, width, _ := g.GetRect()
			if width/2 >= len(label)+4 {
				label = "> " + label + " <"
			}
		}
		button.SetStyle(style)
		button.SetLabel(label)
	}
}
