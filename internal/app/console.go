// SPDX-License-Identifier: MPL-2.0

package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"lefalock/internal/config"
	"lefalock/internal/console"
	"lefalock/internal/ui"
)

func runConsole(args []string, version string) error {
	r, err := console.Parse(args, os.Stdout)
	if err != nil || r.Help {
		return err
	}
	ctx, stop := shutdownContext()
	defer stop()
	streams := console.SystemIO(ctx)
	if r.Command == "password generate" {
		return r.Execute(ctx, console.Dependencies{}, streams)
	}
	conf, err := config.Load(nil, version)
	if err != nil {
		return err
	}
	session, err := prepareVaultSession(conf)
	if err != nil {
		return err
	}
	defer session.Close()
	m := session.recovery
	d := console.Dependencies{Recovery: m, ConfigPath: conf.ConfigPath}
	if strings.HasPrefix(r.Command, "recovery ") {
		return r.Execute(ctx, d, streams)
	}
	status, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if status.Pending {
		return errors.New("vault recovery is pending; use recovery status and recovery commands")
	}
	info, err := openActiveVault(ctx, conf.DataDir, session.keys)
	if err != nil {
		return fmt.Errorf("cannot open vault; use recovery commands if the key or metadata was lost: %w", err)
	}
	if err := session.open(ctx, info); err != nil {
		return err
	}
	if session.warning != nil {
		fmt.Fprintln(streams.Err, "Previous key cleanup is pending")
	}
	d.Credentials = session.credentials
	d.Backup = session.backup
	d.Rotate = session.rotation.Rotate
	d.Clipboard = func(ttl time.Duration) console.Clipboard { return ui.NewSystemClipboard(ttl) }
	return r.Execute(ctx, d, streams)
}
