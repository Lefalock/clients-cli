// SPDX-License-Identifier: MPL-2.0

package console

import (
	"errors"
	"os"
	"strings"
)

func openSecretFile(path string) (*os.File, error) {
	if strings.HasPrefix(path, `\\.\`) || strings.HasPrefix(path, `\\?\`) {
		return nil, errors.New("device paths are not supported")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	return os.Open(path)
}
