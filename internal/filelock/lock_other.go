// SPDX-License-Identifier: MPL-2.0

//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package filelock

import (
	"errors"
	"time"
)

func Acquire(string, time.Duration) (func(), error) {
	return nil, errors.New("file locking is unsupported on this platform")
}
