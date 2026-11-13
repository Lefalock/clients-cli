// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// CommandContext kills a stuck helper; WaitDelay also bounds waiting for pipe
// handles inherited by child processes of clipboard utilities.
type commandClipboard struct {
	readArgs, writeArgs []string
	timeout             time.Duration
	trimCRLF            bool
}

func (c commandClipboard) command(args []string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.WaitDelay = 200 * time.Millisecond
	return cmd, cancel
}

func (c commandClipboard) read() (string, error) {
	cmd, cancel := c.command(c.readArgs)
	defer cancel()
	data, err := cmd.Output()
	if err != nil {
		return "", err
	}
	value := string(data)
	if c.trimCRLF {
		value = strings.TrimSuffix(value, "\r\n")
	}
	return value, nil
}

func (c commandClipboard) write(value string) error {
	cmd, cancel := c.command(c.writeArgs)
	defer cancel()
	// Pass secrets through stdin, never command arguments.
	cmd.Stdin = strings.NewReader(value)
	return cmd.Run()
}
