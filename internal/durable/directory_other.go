// SPDX-License-Identifier: MPL-2.0

//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package durable

func SyncDirectory(string) error { return nil }
