// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"errors"
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"lefalock/internal/passwordgen"
)

type passwordFields struct {
	input, repeat *tview.InputField
	generate      *tview.Button
	view          *tview.Button
	revealed      bool
	canView       func() bool
	options       passwordgen.Options
}

func newPasswordFields(input *tview.InputField) *passwordFields {
	p := &passwordFields{input: input, repeat: formField("Repeat password", true), options: passwordgen.DefaultOptions()}
	styleModalInput(p.repeat)
	p.generate = actionButton("Generate password", nil)
	return p
}
func (p *passwordFields) bind(u *App, allowed func() bool, mask func()) {
	p.canView = func() bool { return allowed() && !u.navigationLocked.Load() }
	p.generate.SetSelectedFunc(func() {
		if !allowed() || u.navigationLocked.Load() {
			return
		}
		u.openPasswordGenerator(p.options, p.generate, func(value string, options passwordgen.Options) {
			p.input.SetText(value)
			p.repeat.SetText(value)
			p.options = options
			p.mask()
			if mask != nil {
				mask()
			}
		})
	})
}
func (p *passwordFields) addViewButton() {
	p.view = actionButton("View", func() {
		if p.view.IsDisabled() || (p.canView != nil && !p.canView()) {
			return
		}
		p.revealed = !p.revealed
		mask := '*'
		if p.revealed {
			mask = 0
		}
		p.input.SetMaskCharacter(mask)
		p.repeat.SetMaskCharacter(mask)
		p.refreshView()
	})
	p.view.SetFocusFunc(p.refreshView)
	p.view.SetBlurFunc(p.refreshView)
}
func (p *passwordFields) refreshView() {
	if p.view == nil {
		return
	}
	label := "View"
	if p.revealed {
		label = "Hide"
	}
	label = tview.Escape("[" + label + "]")
	if p.view.HasFocus() {
		label = "> " + label + " <"
	}
	p.view.SetLabel(label)
}
func (p *passwordFields) mask() {
	p.revealed = false
	p.input.SetMaskCharacter('*')
	p.repeat.SetMaskCharacter('*')
	p.refreshView()
}
func (p *passwordFields) clear() {
	p.input.SetText("")
	p.repeat.SetText("")
	p.mask()
	p.options = passwordgen.DefaultOptions()
}
func (p *passwordFields) disable(b bool) {
	p.input.SetDisabled(b)
	p.repeat.SetDisabled(b)
	p.generate.SetDisabled(b)
	if p.view != nil {
		p.view.SetDisabled(b)
	}
}
func (p *passwordFields) validate(u *App) bool {
	if p.input.GetText() == "" {
		u.tview.SetFocus(p.input)
		u.showError(errors.New("Password is required"))
		return false
	}
	if p.input.GetText() != p.repeat.GetText() {
		u.tview.SetFocus(p.repeat)
		u.showError(errors.New("Passwords do not match"))
		return false
	}
	return true
}

func (u *App) openPasswordGenerator(options passwordgen.Options, restore tview.Primitive, apply func(string, passwordgen.Options)) {
	const pageName = "password-generator"
	if u.navigationLocked.Load() || u.pages.HasPage(pageName) {
		return
	}
	length := formField("Length (4-128)", false).SetText(strconv.Itoa(options.Length))
	styleModalInput(length)
	panel := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(length, 1, 0, true)
	controls := []tview.Primitive{length}
	checks := []*tview.Checkbox{}
	for i, label := range []string{"Lowercase (a-z)", "Uppercase (A-Z)", "Digits (0-9)", "Special characters"} {
		check := tview.NewCheckbox().SetLabel(label + ": ").SetChecked([]bool{options.Lower, options.Upper, options.Digits, options.Special}[i])
		check.SetBackgroundColor(commandTheme.Background)
		check.SetLabelColor(commandTheme.Text).SetFieldBackgroundColor(commandTheme.Panel).SetFieldTextColor(commandTheme.Text)
		checks = append(checks, check)
		controls = append(controls, check)
		panel.AddItem(check, 1, 0, false)
	}
	message := tview.NewTextView().SetWrap(true).SetTextColor(commandTheme.Danger)
	message.SetBackgroundColor(commandTheme.Background)
	close := func() { u.pages.RemovePage(pageName); u.popModal(); u.tview.SetFocus(restore) }
	generate := actionButton("Generate", func() {
		n, err := strconv.Atoi(length.GetText())
		if err != nil {
			message.SetText("Enter a whole number between 4 and 128")
			u.tview.SetFocus(length)
			return
		}
		selected := passwordgen.Options{Length: n, Lower: checks[0].IsChecked(), Upper: checks[1].IsChecked(), Digits: checks[2].IsChecked(), Special: checks[3].IsChecked()}
		if err := passwordgen.Validate(selected); err != nil {
			message.SetText(err.Error())
			return
		}
		generator := u.generatePassword
		if generator == nil {
			generator = passwordgen.Generate
		}
		value, err := generator(selected)
		if err != nil {
			message.SetText("Unable to generate password. Please try again.")
			return
		}
		apply(value, selected)
		close()
	})
	controls = append(controls, generate)
	panel.AddItem(message, 2, 0, false).AddItem(generate, 1, 0, false)
	styleModalFrame(panel.Box, "Generate password", close)
	panel.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEscape:
			close()
			return nil
		case tcell.KeyCtrlS:
			return nil
		case tcell.KeyTab, tcell.KeyBacktab:
			step := 1
			if event.Key() == tcell.KeyBacktab {
				step = -1
			}
			u.focusRelative(controls, focusIndex(controls, u.tview.GetFocus()), step)
			return nil
		case tcell.KeyEnter:
			if u.tview.GetFocus() == length {
				u.tview.SetFocus(checks[0])
				return nil
			}
		}
		return event
	})
	u.pushModal()
	u.pages.AddPage(pageName, centered(54, 10, panel), true, true)
	u.tview.SetFocus(length)
}
