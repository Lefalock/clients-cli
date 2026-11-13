// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type commandShell struct {
	root        *responsiveFlex
	header      *tview.TextView
	footer      *tview.TextView
	navigation  *tview.Flex
	content     *tview.Pages
	items       map[route]*tview.Button
	tooSmall    bool
	onTooSmall  func()
	version     string
	update      string
	updateError string
	active      route
	order       []*tview.Button
}

func newCommandShell(content *tview.Pages, onNavigate func(route)) *commandShell {
	s := &commandShell{
		content: content,
		items:   make(map[route]*tview.Button),
	}
	s.header = tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignLeft)
	s.header.SetBackgroundColor(commandTheme.Surface)
	s.header.SetTextColor(commandTheme.Text)
	s.footer = tview.NewTextView().SetTextAlign(tview.AlignLeft)
	s.footer.SetBackgroundColor(commandTheme.Surface)
	s.footer.SetTextColor(commandTheme.Muted)
	s.footer.SetText("  ↑↓ navigate   Tab switch pane   Enter select   Ctrl+K commands   Esc back")

	s.navigation = tview.NewFlex().SetDirection(tview.FlexColumn)
	s.navigation.SetBackgroundColor(commandTheme.Panel)
	for _, item := range []struct {
		route route
		label string
	}{
		{routeServices, "Vault"},
		{routeBackup, "Backups"}, {routeSettings, "Settings"},
	} {
		item := item
		button := compactButton(item.label, func() { onNavigate(item.route) })
		s.items[item.route] = button
		s.order = append(s.order, button)
		s.navigation.AddItem(button, 0, len(item.label)+4, false)
	}
	s.root = newResponsiveFlex()
	s.root.SetDirection(tview.FlexRow)
	s.root.AddItem(s.header, 1, 0, false).
		AddItem(s.navigation, 1, 0, false).
		AddItem(content, 0, 1, true).
		AddItem(s.footer, 1, 0, false)
	minimum := tview.NewTextView().SetText("Terminal too small. Resize to at least 60 × 18.")
	s.root.layout = func(width, height int) {
		if width < 60 || height < 18 {
			if !s.tooSmall && s.onTooSmall != nil {
				s.onTooSmall()
			}
			s.tooSmall = true
			s.root.Clear().AddItem(minimum, 0, 1, false)
			return
		}
		s.tooSmall = false
		s.root.Clear().AddItem(s.header, 1, 0, false).AddItem(s.navigation, 1, 0, false).AddItem(content, 0, 1, true).AddItem(s.footer, 1, 0, false)
		headerHeight := 1
		if s.update != "" || s.updateError != "" {
			headerHeight = 2
		}
		s.root.ResizeItem(s.header, headerHeight, 0)
	}
	return s
}

func (s *commandShell) setVersion(version string) {
	s.version = version
	s.renderHeader()
}

func (s *commandShell) setUpdateAvailable(version string) {
	s.update = version
	s.updateError = ""
	s.renderHeader()
}

func (s *commandShell) setUpdateError(message string) {
	s.updateError = message
	s.update = ""
	s.renderHeader()
}

func (s *commandShell) clearUpdateStatus() {
	s.update = ""
	s.updateError = ""
	s.renderHeader()
}

func (s *commandShell) renderHeader() {
	product := "[::b]◆ LEFALOCK[::-]"
	if s.version != "" {
		product += " [gray]" + tview.Escape(s.version) + "[-]"
	}
	text := "  " + product
	if s.updateError != "" {
		text += "\n  [red]Update failed: " + tview.Escape(s.updateError) + "[-]"
	} else if s.update != "" {
		text += "\n  [yellow]Update available: " + tview.Escape(s.update) + " · lefalock update[-]"
	}
	s.header.SetText(text).SetWrap(false)
}

func (s *commandShell) setRoute(active route) {
	s.active = active
	for itemRoute, button := range s.items {
		if itemRoute == active || (itemRoute == routeServices && active == routeCreateService) || (itemRoute == routeServices && active == routeCreateSecret) {
			button.SetLabel("▸ " + navigationLabel(itemRoute))
			button.SetBackgroundColor(commandTheme.Panel)
			button.SetLabelColor(commandTheme.Focus)
			continue
		}
		button.SetLabel("  " + navigationLabel(itemRoute))
		button.SetBackgroundColor(commandTheme.Panel)
		button.SetLabelColor(commandTheme.Text)
	}
}

func (s *commandShell) ownsNavigation(value tview.Primitive) bool {
	for _, button := range s.items {
		if button == value {
			return true
		}
	}
	return false
}

func (s *commandShell) focusNavigation(application *tview.Application) {
	target := s.active
	if target == routeCreateService || target == routeCreateSecret {
		target = routeServices
	}
	if button := s.items[target]; button != nil {
		application.SetFocus(button)
	}
}

func navigationLabel(value route) string {
	switch value {
	case routeServices:
		return "Vault"
	case routeBackup:
		return "Backups"
	case routeSettings:
		return "Settings"
	default:
		return ""
	}
}

func compactButton(label string, selected func()) *tview.Button {
	button := tview.NewButton(label).SetSelectedFunc(selected)
	button.SetStyle(tcell.StyleDefault.Background(commandTheme.Panel).Foreground(commandTheme.Text))
	button.SetDisabledStyle(tcell.StyleDefault.Background(commandTheme.Surface).Foreground(commandTheme.Muted))
	button.SetBackgroundColorActivated(commandTheme.Focus)
	button.SetLabelColorActivated(tcell.ColorBlack)
	return button
}

func (s *commandShell) moveNavigation(application *tview.Application, step int) {
	for i, button := range s.order {
		if application.GetFocus() == button {
			application.SetFocus(s.order[(i+step+len(s.order))%len(s.order)])
			return
		}
	}
}
