// SPDX-License-Identifier: MPL-2.0

//go:build windows

package filelock

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func Acquire(path string, timeout time.Duration) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	var overlapped windows.Overlapped
	handle := windows.Handle(file.Fd())
	deadline := time.Now().Add(timeout)
	for {
		err = windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
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
		_ = windows.UnlockFileEx(handle, 0, 1, 0, &overlapped)
		_ = file.Close()
	}, nil
}
