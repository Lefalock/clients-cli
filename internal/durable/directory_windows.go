// SPDX-License-Identifier: MPL-2.0

//go:build windows

package durable

// Windows rename persistence is handled by the filesystem API; syncing a
// directory handle is not supported by os.File.Sync there.
func SyncDirectory(string) error { return nil }
