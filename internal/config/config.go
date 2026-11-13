// SPDX-License-Identifier: MPL-2.0

package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

const (
	releaseAppDirName      = "lefalock"
	developmentAppDirName  = "lefalock-dev"
	releaseKeyringService  = "lefalock"
	developmentKeyringName = "lefalock-dev"
)

var releaseVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

type Config struct {
	DataDir, ConfigPath, KeyringService, Profile string
}
type storedConfig struct {
	LastRestoreDirectory string `json:"last_restore_directory,omitempty"`
}

func LastRestoreDirectory(path string) (string, error) {
	s, e := readStoredConfig(path)
	return s.LastRestoreDirectory, e
}
func RememberRestoreDirectory(path, dir string) error {
	s, e := readStoredConfig(path)
	if e != nil {
		return e
	}
	s.LastRestoreDirectory = dir
	return writeStoredConfig(path, s)
}
func Load(args []string, version string) (Config, error) {
	c, e := resolveProfile(version)
	if e != nil {
		return c, e
	}
	fs := flag.NewFlagSet("lefalock", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fs.SetOutput(os.Stdout)
		}
	}
	fs.Usage = func() { printUsage(fs.Output(), fs) }
	if e := fs.Parse(args); e != nil {
		return c, e
	}
	if fs.NArg() != 0 {
		return c, errors.New("unexpected argument; use lefalock --help")
	}
	return c, nil
}
func resolveProfile(version string) (Config, error) {
	dir, e := os.UserConfigDir()
	if e != nil {
		return Config{}, e
	}
	c := Config{Profile: "development", DataDir: filepath.Join(dir, developmentAppDirName), KeyringService: developmentKeyringName}
	if releaseVersionPattern.MatchString(version) {
		c.Profile = "release"
		c.DataDir = filepath.Join(dir, releaseAppDirName)
		c.KeyringService = releaseKeyringService
	}
	c.ConfigPath = filepath.Join(c.DataDir, "config.json")
	return c, nil
}
func RunCommand(args []string, version string, out io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != "config" {
		return false, nil
	}
	if len(args) == 1 || (len(args) == 2 && (args[1] == "--help" || args[1] == "-h" || args[1] == "help")) {
		fmt.Fprintln(out, "Usage: lefalock config show")
		return true, nil
	}
	if len(args) != 2 || args[1] != "show" {
		return true, errors.New("usage: lefalock config show; this local release has no server settings")
	}
	c, e := Load(nil, version)
	if e != nil {
		return true, e
	}
	_, e = fmt.Fprintf(out, "profile: %s\ndata-dir: %s\ndatabase-dir: %s\nkey-files-dir: %s\nconfig-file: %s\nkeyring-service: %s\n", c.Profile, c.DataDir, filepath.Join(c.DataDir, "vaults"), c.DataDir, c.ConfigPath, c.KeyringService)
	return true, e
}
func printUsage(out io.Writer, fs *flag.FlagSet) {
	fmt.Fprintln(out, "Usage:\n  lefalock [options]\n  lefalock vault --help\n  lefalock config show\n  lefalock licenses\n  lefalock update [check]\n\nOptions:\n  -v, --version    Print the installed version\n\nLicense: MPL 2.0 for Lefalock code; third-party licenses also apply.\nRun lefalock licenses for full texts and corresponding source information.")
	fs.PrintDefaults()
}

func readStoredConfig(path string) (storedConfig, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return storedConfig{}, nil
	}
	if err != nil {
		return storedConfig{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var stored storedConfig
	if err := json.Unmarshal(raw, &stored); err != nil {
		return storedConfig{}, fmt.Errorf("decode config %s: %w", path, err)
	}
	return stored, nil
}

func writeStoredConfig(path string, stored storedConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("protect config directory: %w", err)
	}
	raw, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	raw = append(raw, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}
