// SPDX-License-Identifier: MPL-2.0

//go:build linux || freebsd || netbsd || openbsd || solaris || dragonfly

package ui

import (
	"errors"
	"os"
	"os/exec"
	"time"
)

func systemClipboardAccess() (func() (string, error), func(string) error) {
	backend, err := selectCommandClipboard(os.Getenv("WAYLAND_DISPLAY") != "", exec.LookPath)
	if err != nil {
		return func() (string, error) { return "", err }, func(string) error { return err }
	}
	return backend.read, backend.write
}

func selectCommandClipboard(wayland bool, lookPath func(string) (string, error)) (commandClipboard, error) {
	var candidates []commandClipboard
	if wayland {
		candidates = append(candidates, commandClipboard{readArgs: []string{"wl-paste", "--no-newline"}, writeArgs: []string{"wl-copy"}})
	}
	candidates = append(candidates,
		commandClipboard{readArgs: []string{"xclip", "-out", "-selection", "clipboard"}, writeArgs: []string{"xclip", "-in", "-selection", "clipboard"}},
		commandClipboard{readArgs: []string{"xsel", "--output", "--clipboard"}, writeArgs: []string{"xsel", "--input", "--clipboard"}},
		commandClipboard{readArgs: []string{"termux-clipboard-get"}, writeArgs: []string{"termux-clipboard-set"}},
		commandClipboard{readArgs: []string{"powershell.exe", "Get-Clipboard"}, writeArgs: []string{"clip.exe"}, trimCRLF: true},
	)
	for _, candidate := range candidates {
		readPath, readErr := lookPath(candidate.readArgs[0])
		writePath, writeErr := lookPath(candidate.writeArgs[0])
		if readErr == nil && writeErr == nil {
			candidate.readArgs[0], candidate.writeArgs[0] = readPath, writePath
			candidate.timeout = 2 * time.Second
			return candidate, nil
		}
	}
	return commandClipboard{}, errors.New("no clipboard utilities available; install wl-clipboard, xclip or xsel")
}
