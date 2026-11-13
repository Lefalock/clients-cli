// SPDX-License-Identifier: MPL-2.0

package ui

import "time"

func systemClipboardAccess() (func() (string, error), func(string) error) {
	backend := commandClipboard{readArgs: []string{"pbpaste"}, writeArgs: []string{"pbcopy"}, timeout: 2 * time.Second}
	return backend.read, backend.write
}
