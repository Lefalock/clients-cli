// SPDX-License-Identifier: MPL-2.0

//go:build !windows

package update

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func replaceExecutable(source, target, _ string) (bool, error) {
	err := installExecutable(source, target, "")
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, os.ErrPermission) {
		return false, err
	}
	if err := installExecutableWithSudo(source, target); err != nil {
		return false, err
	}
	return false, nil
}

func installExecutable(source, target, expectedChecksum string) error {
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		sourceFile.Close()
		return err
	}
	if !sourceInfo.Mode().IsRegular() || sourceInfo.Size() < 0 || sourceInfo.Size() > maxBinarySize {
		sourceFile.Close()
		return errors.New("verified update source is not a valid regular file")
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".lefalock-update-")
	if err != nil {
		sourceFile.Close()
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(sourceFile, maxBinarySize+1))
	closeSourceErr := sourceFile.Close()
	if copyErr != nil {
		temporary.Close()
		return copyErr
	}
	if closeSourceErr != nil {
		temporary.Close()
		return closeSourceErr
	}
	if written != sourceInfo.Size() || written > maxBinarySize {
		temporary.Close()
		return errors.New("verified update source changed while being installed")
	}
	if expectedChecksum != "" && !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expectedChecksum) {
		temporary.Close()
		return errors.New("verified update checksum changed before installation")
	}
	mode := info.Mode().Perm()
	if mode&0o111 == 0 {
		mode |= 0o755
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("open executable directory: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return fmt.Errorf("sync executable directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close executable directory: %w", closeErr)
	}
	return nil
}

func installExecutableWithSudo(source, target string) error {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("replace %s: permission denied and sudo is unavailable", target)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read verified update before privilege elevation: %w", err)
	}
	digest := sha256.Sum256(data)
	command := exec.Command(sudo, "--", target, privilegedInstallCommand, source, hex.EncodeToString(digest[:]))
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("install verified update with sudo: %w", err)
	}
	return nil
}

const privilegedInstallCommand = "__install-verified-update"

func RunPrivilegedInstall(args []string) (bool, error) {
	if len(args) == 0 || args[0] != privilegedInstallCommand {
		return false, nil
	}
	if len(args) != 3 {
		return true, errors.New("invalid privileged update invocation")
	}
	if os.Geteuid() != 0 {
		return true, errors.New("privileged update installer must run as root")
	}
	executable, err := os.Executable()
	if err != nil {
		return true, fmt.Errorf("locate privileged update installer: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	checksum := strings.TrimSpace(args[2])
	decoded, err := hex.DecodeString(checksum)
	if err != nil || len(decoded) != sha256.Size {
		return true, errors.New("invalid verified update checksum")
	}
	if err := installExecutable(args[1], executable, checksum); err != nil {
		return true, fmt.Errorf("install verified update: %w", err)
	}
	return true, nil
}
