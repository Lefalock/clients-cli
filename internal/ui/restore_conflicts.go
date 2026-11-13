// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"fmt"
	"lefalock/internal/domain"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/service"
)

var restoreChoices = []service.RestoreChoice{
	"", service.RestoreKeepCurrent, service.RestoreReplace, service.RestoreKeepBoth, service.RestoreNewService,
}

type restoreConflictEditor struct {
	app       *App
	preview   *service.RestorePreview
	decisions map[string]service.RestoreDecision
	table     *tview.Table
	dropdown  *tview.DropDown
	name      *tview.InputField
	editor    *tview.Flex
	status    *tview.TextView
	review    *tview.Button
	selected  int
	loading   bool
}

func (u *App) showRestoreConflicts(preview *service.RestorePreview, decisions map[string]service.RestoreDecision) {
	if len(preview.Conflicts) == 0 {
		u.showRestoreSummary(preview, decisions)
		return
	}
	e := &restoreConflictEditor{app: u, preview: preview, decisions: decisions, selected: 0}
	e.table = tview.NewTable().SetSelectable(true, false).SetFixed(1, 0)
	e.table.SetBorder(true).SetTitle(" Secret conflicts ").SetBorderColor(commandTheme.Border)
	for column, title := range []string{"Imported service", "Imported secret", "Current service", "Current secret"} {
		e.table.SetCell(0, column, tview.NewTableCell(title).SetTextColor(tcell.ColorAqua).SetExpansion(1).SetSelectable(false))
	}
	serviceIDs := make(map[string][]string)
	accountIDs := make(map[string][]string)
	for _, conflict := range preview.Conflicts {
		serviceIDs[conflict.Service] = appendUniqueRestoreID(serviceIDs[conflict.Service], conflict.CurrentServiceID)
		key := conflict.CurrentServiceID + "\x00" + conflict.Secret
		accountIDs[key] = appendUniqueRestoreID(accountIDs[key], conflict.CurrentSecretID)
	}
	for index, conflict := range preview.Conflicts {
		currentService := conflict.Service
		if len(serviceIDs[conflict.Service]) > 1 {
			currentService += fmt.Sprintf(" (#%d)", restoreIDPosition(serviceIDs[conflict.Service], conflict.CurrentServiceID))
		}
		currentSecret := conflict.Secret
		accountKey := conflict.CurrentServiceID + "\x00" + conflict.Secret
		if len(accountIDs[accountKey]) > 1 {
			currentSecret += fmt.Sprintf(" (#%d)", restoreIDPosition(accountIDs[accountKey], conflict.CurrentSecretID))
		}
		for column, value := range []string{conflict.Service, restoreTypedName(conflict.Secret, conflict.BackupType), currentService, restoreTypedName(currentSecret, conflict.CurrentType)} {
			e.table.SetCell(index+1, column, tview.NewTableCell(displayText(value)).SetMaxWidth(28).SetExpansion(1))
		}
	}
	e.table.SetSelectionChangedFunc(func(row, _ int) {
		if row < 1 || row > len(preview.Conflicts) {
			return
		}
		e.selected = row - 1
		e.showEditor()
	})
	e.table.SetSelectedFunc(func(int, int) { u.tview.SetFocus(e.dropdown) })
	e.dropdown = tview.NewDropDown().SetLabel("Decision: ").SetFieldWidth(0)
	e.dropdown.SetOptions([]string{"Choose...", "Keep current", "Use backup", "Keep both: rename secret", "Keep both: rename service"}, func(_ string, option int) {
		if e.loading {
			return
		}
		if option < 0 || option >= len(restoreChoices) {
			return
		}
		e.setChoice(e.selected, restoreChoices[option], "")
		e.showEditor()
	})
	e.name = formField("New name", false)
	e.name.SetChangedFunc(func(value string) {
		if e.loading {
			return
		}
		e.updateName(value)
	})
	e.editor = tview.NewFlex()
	e.status = tview.NewTextView()
	e.table.Select(1, 0)
	e.showEditor()
	applyAll := restoreApplyAllCheckbox()
	undo := func() {
		if u.restoreUndoPreview != preview || u.restoreUndoDecisions == nil {
			return
		}
		for key := range decisions {
			delete(decisions, key)
		}
		for key, decision := range u.restoreUndoDecisions {
			decisions[key] = decision
		}
		u.restoreUndoPreview = nil
		u.restoreUndoDecisions = nil
		e.showEditor()
	}
	if u.restoreUndoPreview == preview && u.restoreUndoDecisions != nil {
		applyAll.SetChecked(true)
	}
	applyAll.SetChangedFunc(func(checked bool) {
		if !checked {
			undo()
			return
		}
		option, _ := e.dropdown.GetCurrentOption()
		if option < 1 || option >= len(restoreChoices) {
			applyAll.SetChecked(false)
			e.status.SetText(fmt.Sprintf("%d conflicts remaining. Choose a decision before Apply for all.", restoreUnresolvedConflicts(preview, decisions)))
			return
		}
		u.restoreUndoPreview = preview
		u.restoreUndoDecisions = cloneRestoreDecisions(decisions)
		e.applyAll(restoreChoices[option])
	})
	cancel := func() {
		u.restoreUndoPreview = nil
		u.restoreUndoDecisions = nil
		u.closeRestoreDialog()
	}
	review := actionButton("Review restore", func() {
		if restoreUnresolvedConflicts(preview, decisions) > 0 {
			e.refreshTable()
			return
		}
		if _, err := u.backup.RestoreSummaryFor(preview, decisions); err != nil {
			e.status.SetText("Cannot continue: " + err.Error())
			return
		}
		u.closeRestoreDialog()
		u.showRestoreSummary(preview, decisions)
	})
	e.review = review
	e.refreshTable()
	body := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(e.status, 1, 0, false).
		AddItem(e.table, 0, 1, true).
		AddItem(e.editor, 1, 0, false).
		AddItem(applyAll, 1, 0, false).
		AddItem(review, 1, 0, false)
	controls := func() []tview.Primitive {
		items := []tview.Primitive{e.table, e.dropdown}
		if e.needsName() {
			items = append(items, e.name)
		}
		return append(items, applyAll, review)
	}
	u.openRestoreDialogDynamic("Resolve backup conflicts", body, controls, e.table, cancel)
}

func restoreApplyAllCheckbox() *tview.Checkbox {
	return tview.NewCheckbox().SetLabel("Apply for all ").
		SetUncheckedString(tview.Escape("[ ]")).SetCheckedString(tview.Escape("[x]")).
		SetLabelStyle(tcell.StyleDefault.Foreground(commandTheme.Text)).
		SetUncheckedStyle(tcell.StyleDefault.Foreground(commandTheme.Text).Background(commandTheme.Panel)).
		SetCheckedStyle(tcell.StyleDefault.Foreground(commandTheme.Background).Background(commandTheme.Success)).
		SetActivatedStyle(tcell.StyleDefault.Foreground(commandTheme.Background).Background(commandTheme.Focus))
}

func cloneRestoreDecisions(source map[string]service.RestoreDecision) map[string]service.RestoreDecision {
	copy := make(map[string]service.RestoreDecision, len(source))
	for key, decision := range source {
		copy[key] = decision
	}
	return copy
}

func restoreUnresolvedConflicts(preview *service.RestorePreview, decisions map[string]service.RestoreDecision) int {
	remaining := 0
	for _, conflict := range preview.Conflicts {
		if !restoreConflictResolved(conflict, decisions[conflict.Key]) {
			remaining++
		}
	}
	return remaining
}

func restoreConflictResolved(conflict service.RestoreConflict, decision service.RestoreDecision) bool {
	switch decision.Choice {
	case service.RestoreKeepCurrent:
		return true
	case service.RestoreReplace:
		return conflict.CurrentType == conflict.BackupType
	case service.RestoreKeepBoth:
		return strings.TrimSpace(decision.NewName) != ""
	case service.RestoreNewService:
		if conflict.Standalone {
			return false
		}
		return strings.TrimSpace(decision.NewServiceName) != ""
	default:
		return false
	}
}

func appendUniqueRestoreID(ids []string, id string) []string {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

func restoreIDPosition(ids []string, id string) int {
	for index, existing := range ids {
		if existing == id {
			return index + 1
		}
	}
	return 0
}

func (e *restoreConflictEditor) conflict() service.RestoreConflict {
	return e.preview.Conflicts[e.selected]
}

func (e *restoreConflictEditor) needsName() bool {
	choice := e.decisions[e.conflict().Key].Choice
	return choice == service.RestoreKeepBoth || choice == service.RestoreNewService
}

func (e *restoreConflictEditor) showEditor() {
	if e.dropdown == nil || e.editor == nil || e.name == nil {
		return
	}
	decision := e.decisions[e.conflict().Key]
	e.loading = true
	options := []string{"Choose...", "Keep current", "Use backup", "Keep both: rename secret", "Keep both: rename service"}
	if e.conflict().Standalone {
		options = []string{"Choose...", "Keep current", "Use backup", "Keep both: rename note"}
	}
	e.dropdown.SetOptions(options, func(_ string, option int) {
		if e.loading || option < 0 || option >= len(restoreChoices) {
			return
		}
		e.setChoice(e.selected, restoreChoices[option], "")
		e.showEditor()
	})
	option := 0
	for index, choice := range restoreChoices {
		if choice == decision.Choice {
			option = index
			break
		}
	}
	e.dropdown.SetCurrentOption(option)
	e.editor.Clear()
	e.editor.AddItem(e.dropdown, 0, 1, true)
	if e.needsName() {
		if decision.Choice == service.RestoreNewService {
			e.name.SetLabel("New service: ").SetText(decision.NewServiceName)
		} else {
			label := "New secret: "
			if e.conflict().Standalone {
				label = "New title: "
			}
			e.name.SetLabel(label).SetText(decision.NewName)
		}
		e.editor.AddItem(e.name, 0, 1, false)
	}
	e.loading = false
	e.refreshTable()
}

func (e *restoreConflictEditor) refreshTable() {
	remaining := restoreUnresolvedConflicts(e.preview, e.decisions)
	for index, conflict := range e.preview.Conflicts {
		color := commandTheme.Text
		if restoreConflictResolved(conflict, e.decisions[conflict.Key]) {
			color = tcell.ColorGreen
		}
		for column := 0; column < 4; column++ {
			e.table.GetCell(index+1, column).SetTextColor(color)
		}
	}
	status := fmt.Sprintf("%d conflicts remaining of %d. Select a row, then choose its decision below.", remaining, len(e.preview.Conflicts))
	if selected := e.conflict(); selected.CurrentType != selected.BackupType {
		status = "Different secret types: replacement unavailable; choose Keep current or Keep both."
	}
	if selected := e.conflict(); selected.Standalone && e.decisions[selected.Key].Choice == service.RestoreNewService {
		status = "Folder note: choose Keep current, Use backup or Keep both: rename secret."
	}
	e.status.SetText(status)
	if e.review != nil {
		e.review.SetDisabled(remaining > 0)
	}
}

func (e *restoreConflictEditor) setChoice(index int, choice service.RestoreChoice, name string) {
	conflict := e.preview.Conflicts[index]
	if conflict.Standalone && choice == service.RestoreNewService {
		return
	}
	previous := e.decisions[conflict.Key]
	if previous.Choice == service.RestoreNewService && choice != service.RestoreNewService {
		for _, other := range e.preview.Conflicts {
			if other.ServiceIndex == conflict.ServiceIndex {
				delete(e.decisions, other.Key)
			}
		}
	}
	switch choice {
	case "":
		delete(e.decisions, conflict.Key)
	case service.RestoreNewService:
		if name == "" {
			if previous.Choice == choice {
				name = previous.NewServiceName
			} else {
				name = e.app.backup.SuggestRestoreServiceName(e.preview, conflict, e.decisions)
			}
		}
		for _, other := range e.preview.Conflicts {
			if other.ServiceIndex == conflict.ServiceIndex {
				e.decisions[other.Key] = service.RestoreDecision{Choice: choice, NewServiceName: name}
			}
		}
	case service.RestoreKeepBoth:
		if name == "" {
			if previous.Choice == choice {
				name = previous.NewName
			} else {
				name = e.app.backup.SuggestRestoreName(e.preview, conflict, e.decisions)
			}
		}
		e.decisions[conflict.Key] = service.RestoreDecision{Choice: choice, NewName: name}
	default:
		e.decisions[conflict.Key] = service.RestoreDecision{Choice: choice}
	}
	e.refreshTable()
}

func (e *restoreConflictEditor) updateName(value string) {
	conflict := e.conflict()
	decision := e.decisions[conflict.Key]
	if decision.Choice == service.RestoreNewService {
		for _, other := range e.preview.Conflicts {
			if other.ServiceIndex == conflict.ServiceIndex {
				e.decisions[other.Key] = service.RestoreDecision{Choice: service.RestoreNewService, NewServiceName: strings.TrimSpace(value)}
			}
		}
	} else if decision.Choice == service.RestoreKeepBoth {
		decision.NewName = strings.TrimSpace(value)
		e.decisions[conflict.Key] = decision
	}
	e.refreshTable()
}

func (e *restoreConflictEditor) applyAll(choice service.RestoreChoice) {
	selected := e.selected
	for index := range e.preview.Conflicts {
		if index == selected {
			continue
		}
		e.setChoice(index, choice, "")
	}
	e.showEditor()
}

func restoreTypedName(name string, typ domain.SecretType) string {
	if typ == "" {
		return name
	}
	return name + " (" + string(typ) + ")"
}
