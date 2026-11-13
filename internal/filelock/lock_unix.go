// SPDX-License-Identifier: MPL-2.0

//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package filelock

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func Acquire(path string, timeout time.Duration) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			file.Close()
			return nil, fmt.Errorf("acquire lock: %w", err)
		}
		if time.Now().After(deadline) {
			file.Close()
			return nil, errors.New("timed out waiting for lock")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return func() {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		_ = file.Close()
	}, nil
}
