// SPDX-License-Identifier: MPL-2.0

//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package durable

import "os"

func SyncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
