// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"sort"
	"strings"

	"lefalock/internal/domain"
	"lefalock/internal/service"
)

func (p *servicesPage) visibleChildDirectories() []service.Directory {
	if !p.selectedDirectory {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(p.search.GetText()))
	parentMatches := query != "" && strings.Contains(strings.ToLower(p.directoryPath(p.directoryID)), query)
	var children []service.Directory
	for _, folder := range p.directories {
		if folder.ParentID == p.directoryID && (query == "" || parentMatches || strings.Contains(strings.ToLower(folder.Name), query)) {
			children = append(children, folder)
		}
	}
	sort.Slice(children, func(i, j int) bool {
		left, right := strings.ToLower(children[i].Name), strings.ToLower(children[j].Name)
		if left == right {
			return children[i].ID < children[j].ID
		}
		return left < right
	})
	return children
}

func (p *servicesPage) visibleServicesInDirectory() []service.Service {
	if !p.selectedDirectory {
		return nil
	}
	var items []service.Service
	for _, item := range p.items {
		if item.DirectoryID == p.directoryID {
			items = append(items, item)
		}
	}
	return items
}

func (p *servicesPage) directoryName(id string) string {
	if id == "" {
		return "Root"
	}
	for _, item := range p.directories {
		if item.ID == id {
			return item.Name
		}
	}
	return "Missing folder"
}

func (p *servicesPage) serviceName(id string) string {
	for _, item := range p.allItems {
		if item.ID == id {
			return item.Name
		}
	}
	return "Missing service"
}

func (p *servicesPage) visibleSecrets() []service.Secret {
	if p.selectedService >= 0 && p.selectedService < len(p.items) {
		return p.items[p.selectedService].Secrets
	}
	if !p.selectedDirectory {
		return nil
	}
	var accounts []service.Secret
	query := strings.ToLower(strings.TrimSpace(p.search.GetText()))
	folderMatches := query != "" && strings.Contains(strings.ToLower(p.directoryPath(p.directoryID)), query)
	for _, item := range p.allItems {
		for _, account := range item.Secrets {
			// Secrets sharing their service folder are shown inside the service.
			// Only separately placed secrets appear directly in folder contents.
			if account.Type != domain.SecretNote && account.DirectoryID != item.DirectoryID && account.DirectoryID == p.directoryID && (query == "" || folderMatches || strings.Contains(strings.ToLower(account.Name), query) || strings.Contains(strings.ToLower(item.Name), query)) {
				accounts = append(accounts, account)
			}
		}
	}
	for _, note := range p.standaloneNotes {
		if note.DirectoryID == p.directoryID && (query == "" || folderMatches || strings.Contains(strings.ToLower(note.Name), query)) {
			accounts = append(accounts, note)
		}
	}
	return accounts
}

func (p *servicesPage) filterItems(query string) []service.Service {
	result := filterVault(p.allItems, query)
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return result
	}
	included := make(map[string]bool, len(result))
	for _, item := range result {
		included[item.ID] = true
	}
	for _, item := range p.allItems {
		if !included[item.ID] && strings.Contains(strings.ToLower(p.directoryPath(item.DirectoryID)), query) {
			result = append(result, item)
		}
	}
	return result
}

func (p *servicesPage) folderMatchesSearch() bool {
	query := strings.ToLower(strings.TrimSpace(p.search.GetText()))
	if query == "" {
		return false
	}
	for _, item := range p.directories {
		if strings.Contains(strings.ToLower(item.Name), query) {
			return true
		}
	}
	return false
}

func (p *servicesPage) accountSelectionKey() string {
	if p.selectedDirectory {
		return "directory:" + p.directoryID
	}
	return p.serviceID
}

func (p *servicesPage) resolveCreatedDirectorySelection(previous []service.Directory) {
	if p.preferredDirectoryName == "" {
		return
	}
	old := make(map[string]bool, len(previous))
	for _, item := range previous {
		old[item.ID] = true
	}
	for _, item := range p.directories {
		if !old[item.ID] && item.Name == p.preferredDirectoryName && item.ParentID == p.preferredDirectoryParent {
			p.selectedDirectory, p.directoryID = true, item.ID
			p.expanded[item.ParentID] = true
			break
		}
	}
	p.preferredDirectoryName, p.preferredDirectoryParent = "", ""
}

func (p *servicesPage) buildTreeRows() {
	p.treeRows = nil
	searching := p.search != nil && strings.TrimSpace(p.search.GetText()) != ""
	children := make(map[string][]service.Directory)
	for _, item := range p.directories {
		children[item.ParentID] = append(children[item.ParentID], item)
	}
	services := make(map[string][]int)
	for index, item := range p.items {
		services[item.DirectoryID] = append(services[item.DirectoryID], index)
	}
	seen := make(map[string]bool)
	var visit func(string, []bool)
	visit = func(parent string, continuations []bool) {
		remaining := len(children[parent]) + len(services[parent])
		for _, child := range children[parent] {
			if seen[child.ID] {
				continue
			}
			seen[child.ID] = true
			remaining--
			p.treeRows = append(p.treeRows, vaultTreeRow{directory: true, directoryID: child.ID, serviceIndex: -1, prefix: vaultTreePrefix(continuations, remaining == 0)})
			if p.expanded[child.ID] || searching {
				visit(child.ID, append(continuations, remaining > 0))
			}
		}
		for _, index := range services[parent] {
			remaining--
			p.treeRows = append(p.treeRows, vaultTreeRow{serviceIndex: index, prefix: vaultTreePrefix(continuations, remaining == 0)})
		}
	}
	visit("", nil)
}

func vaultTreePrefix(continuations []bool, last bool) string {
	var prefix strings.Builder
	for _, more := range continuations {
		if more {
			prefix.WriteString("│  ")
		} else {
			prefix.WriteString("   ")
		}
	}
	if last {
		prefix.WriteString("└─ ")
	} else {
		prefix.WriteString("├─ ")
	}
	return prefix.String()
}

func (p *servicesPage) directoryPath(id string) string {
	if id == "" {
		return "Root"
	}
	parents := make(map[string]service.Directory)
	for _, item := range p.directories {
		parents[item.ID] = item
	}
	var names []string
	seen := make(map[string]bool)
	for id != "" {
		item, ok := parents[id]
		if !ok || seen[id] {
			return "Missing folder"
		}
		seen[id] = true
		names = append(names, item.Name)
		id = item.ParentID
	}
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
	return "Root / " + strings.Join(names, " / ")
}

func (p *servicesPage) directoryBreadcrumb() string {
	return p.directoryBreadcrumbFor(p.directoryID)
}

func (p *servicesPage) directoryBreadcrumbFor(directoryID string) string {
	if directoryID == "" {
		return "/"
	}
	parents := make(map[string]service.Directory, len(p.directories))
	for _, folder := range p.directories {
		parents[folder.ID] = folder
	}
	var names []string
	seen := make(map[string]bool)
	for id := directoryID; id != ""; {
		if seen[id] {
			return "Missing folder"
		}
		folder, ok := parents[id]
		if !ok {
			return "Missing folder"
		}
		seen[id] = true
		names = append(names, folder.Name)
		id = folder.ParentID
	}
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
	return strings.Join(names, " › ")
}

func (p *servicesPage) parentDirectoryID() string {
	for _, folder := range p.directories {
		if folder.ID == p.directoryID {
			return folder.ParentID
		}
	}
	return ""
}

func (p *servicesPage) contentBreadcrumb() string {
	if p.selectedDirectory {
		return p.directoryBreadcrumb()
	}
	if p.selectedService < 0 || p.selectedService >= len(p.items) {
		return ""
	}
	name := p.items[p.selectedService].Name
	if p.directoryID == "" {
		return name
	}
	return p.directoryBreadcrumb() + " › " + name
}
