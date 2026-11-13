// SPDX-License-Identifier: MPL-2.0

//go:build !linux && !freebsd && !netbsd && !openbsd && !solaris && !dragonfly && !darwin

package ui

import "github.com/atotto/clipboard"

func systemClipboardAccess() (func() (string, error), func(string) error) {
	return clipboard.ReadAll, clipboard.WriteAll
}
