// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/service"
)

func (p *servicesPage) fullService() *service.Service {
	for i := range p.allItems {
		if p.allItems[i].ID == p.serviceID {
			return &p.allItems[i]
		}
	}
	return nil
}

func (p *servicesPage) openServiceDetails() {
	if p.busy || p.loadError || p.ui.modalDepth.Load() > 0 {
		return
	}
	item := p.fullService()
	if item == nil {
		return
	}
	restore := p.ui.tview.GetFocus()
	const name = "service-details"
	text := tview.NewTextView().SetWrap(true).SetText(fmt.Sprintf("Service: %s\nFolder: %s\nID: %s\nCreated: %s\nUpdated: %s\nSecrets: %d (all, including search-hidden secrets)", displayText(item.Name), displayText(p.directoryPath(item.DirectoryID)), item.ID, item.CreatedAt.Local().Format(time.DateTime), item.UpdatedAt.Local().Format(time.DateTime), len(item.Secrets)))
	close := func() {
		p.ui.pages.RemovePage(name)
		p.ui.popModal()
		p.closeDetails = nil
		p.ui.tview.SetFocus(restore)
	}
	p.closeDetails = close
	styleModalFrame(text.Box, "Service details", close)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(text, 0, 1, true)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		}
		return event
	})
	p.ui.pushModal()
	p.ui.pages.AddPage(name, centered(72, 12, panel), true, true)
	p.ui.tview.SetFocus(text)
}
