// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"
	"fmt"
	"time"

	"lefalock/internal/service"
)

// stopAuthCode invalidates in-flight calculations as well as stopping future
// ticks. The UI goroutine owns these fields.
func (p *servicesPage) stopAuthCode() {
	if p.authCodeCancel != nil {
		p.authCodeCancel()
		p.authCodeCancel = nil
	}
	p.authCodeEpoch++
	p.authCode = ""
	p.authCodeSecretID = ""
	p.authCodeSecondsLeft = 0
}

func (p *servicesPage) startAuthCode(secret service.Secret) {
	p.stopAuthCode()
	p.authCodeSecretID = secret.ID
	p.authCode = "Loading…"
	p.refreshDetails()
	if !p.ui.running.Load() {
		return
	}
	ctx, cancel := context.WithCancel(p.ui.rootCtx)
	p.authCodeCancel = cancel
	epoch, secretID := p.authCodeEpoch, secret.ID
	encrypted := secret.EncryptedPassword
	clock := p.authCodeNow
	if clock == nil {
		clock = time.Now
	}
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		lastStep := int64(-1)
		lastSecondsLeft := -1
		code := ""
		refresh := func(now time.Time) {
			step := now.Unix() / 30
			secondsLeft := 30 - int(now.Unix()%30)
			if step != lastStep {
				lastStep = step
				var err error
				code, err = p.ui.credentials.GenerateAuthCode(ctx, encrypted, now)
				if ctx.Err() != nil || !p.ui.running.Load() {
					return
				}
				if err != nil {
					code = "Unavailable"
				}
				lastSecondsLeft = -1
			}
			if secondsLeft == lastSecondsLeft {
				return
			}
			lastSecondsLeft = secondsLeft
			displayCode := code
			p.ui.tview.QueueUpdateDraw(func() {
				if ctx.Err() != nil || epoch != p.authCodeEpoch || p.modalDetails == nil ||
					p.accountID != secretID || p.authCodeSecretID != secretID ||
					!p.ui.pages.HasPage("secret-details") || clock().Unix()/30 != step {
					return
				}
				p.authCode = displayCode
				p.authCodeSecondsLeft = 30 - int(clock().Unix()%30)
				p.refreshDetails()
			})
		}
		refresh(clock())
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh(clock())
			}
		}
	}()
}

func (p *servicesPage) copyAuthCode() {
	if p.busy || p.small || p.loadError {
		return
	}
	secret, _, err := p.selectedSecret()
	if err != nil {
		return
	}
	encrypted := secret.EncryptedPassword
	p.busy = true
	p.header.SetText("Copying Auth code…")
	p.updateActions()
	p.ui.beginTask(func(ctx context.Context) error {
		code, err := p.ui.credentials.GenerateAuthCode(ctx, encrypted, time.Now())
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return p.ui.clipboard.Copy(code)
	}, func(err error) {
		p.busy = false
		p.updateActions()
		if err != nil {
			p.ui.showError(err)
			return
		}
		message := "Auth code copied"
		if clipboard, ok := p.ui.clipboard.(interface{ TTL() time.Duration }); ok {
			message += fmt.Sprintf(" · clears in %s if unchanged", clipboard.TTL())
		}
		p.header.SetText(message)
	})
}
