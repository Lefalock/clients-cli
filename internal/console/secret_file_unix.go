// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package console

import (
	"golang.org/x/sys/unix"
	"os"
)

// O_NONBLOCK prevents FIFO/device opens from bypassing cancellation. The caller
// validates the opened descriptor, so a pathname replacement cannot bypass it.
func openSecretFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "secret-input"), nil
}
