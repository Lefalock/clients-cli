// SPDX-License-Identifier: MPL-2.0

package console

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"unicode/utf8"

	"golang.org/x/term"
)

const maxSecretBytes = 1 << 20

type SecretInput struct {
	Stdin bool
	File  string
	FD    int
}

func (s SecretInput) sources() int {
	n := 0
	if s.Stdin {
		n++
	}
	if s.File != "" {
		n++
	}
	if s.FD >= 0 {
		n++
	}
	return n
}

// IO keeps secret input/output separate from metadata. Tests can supply pipes
// without replacing the OS keyring or application logic.
type IO struct {
	Context      context.Context
	In           io.Reader
	Out, Err     io.Writer
	ReadPassword func() ([]byte, error)
}

func SystemIO(ctx context.Context) IO {
	i := IO{Context: ctx, In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		i.ReadPassword = func() ([]byte, error) { return hiddenPassword(ctx) }
	}
	return i
}

func hiddenPassword(ctx context.Context) ([]byte, error) {
	// Switch modes synchronously before starting the reader. Cancellation can
	// then always restore the original mode, including echo, before returning.
	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return nil, err
	}
	defer term.Restore(int(os.Stdin.Fd()), state)
	type result struct {
		raw []byte
		err error
	}
	done := make(chan result)
	go func() {
		raw, err := readHidden(os.Stdin)
		select {
		case done <- result{raw, err}:
		case <-ctx.Done():
			clear(raw)
		}
	}()
	select {
	case r := <-done:
		return r.raw, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s SecretInput) Read(streams IO, multiline, confirm bool) (string, error) {
	var reader io.Reader
	switch {
	case s.Stdin:
		reader = streams.In
	case s.File != "":
		f, err := openSecretFile(s.File)
		if err != nil {
			return "", errors.New("cannot open secret input file")
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return "", errors.New("secret input must be a regular file")
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			return "", errors.New("secret input file must have no group or other permissions (use chmod 600)")
		}
		reader = f
	case s.FD >= 0:
		f := os.NewFile(uintptr(s.FD), "secret-input")
		if f == nil {
			return "", errors.New("invalid secret input descriptor")
		}
		// This is an explicitly inherited descriptor owned by this command.
		defer f.Close()
		reader = f
	default:
		if streams.ReadPassword == nil {
			return "", errors.New("no terminal for hidden input; use --value-stdin/--value-file/--value-fd (or --phrase-*)")
		}
		if multiline {
			return "", errors.New("multiline notes require --value-stdin, --value-file or --value-fd")
		}
		fmt.Fprint(streams.Err, "Secret (hidden): ")
		raw, err := streams.ReadPassword()
		fmt.Fprintln(streams.Err)
		defer clear(raw)
		if err != nil {
			return "", errors.New("unable to read hidden secret")
		}
		if len(raw) > maxSecretBytes {
			return "", errors.New("secret exceeds 1 MiB")
		}
		if confirm {
			fmt.Fprint(streams.Err, "Repeat secret (hidden): ")
			check, err := streams.ReadPassword()
			fmt.Fprintln(streams.Err)
			defer clear(check)
			if err != nil {
				return "", errors.New("unable to read confirmation")
			}
			if !bytes.Equal(raw, check) {
				return "", errors.New("secret confirmation does not match")
			}
		}
		return string(raw), nil
	}
	raw, err := readBounded(streams.Context, reader)
	defer clear(raw)
	if err != nil {
		return "", errors.New("unable to read secret input")
	}
	if len(raw) > maxSecretBytes {
		return "", errors.New("secret exceeds 1 MiB")
	}
	if !multiline {
		if bytes.HasSuffix(raw, []byte("\n")) {
			raw = bytes.TrimSuffix(raw, []byte("\n"))
			raw = bytes.TrimSuffix(raw, []byte("\r"))
		}
	}
	return string(raw), nil
}

func readBounded(ctx context.Context, reader io.Reader) ([]byte, error) {
	if ctx == nil {
		return io.ReadAll(io.LimitReader(reader, maxSecretBytes+1))
	}
	type result struct {
		raw []byte
		err error
	}
	done := make(chan result)
	go func() {
		raw, err := io.ReadAll(io.LimitReader(reader, maxSecretBytes+1))
		select {
		case done <- result{raw, err}:
		case <-ctx.Done():
			clear(raw)
		}
	}()
	select {
	case r := <-done:
		return r.raw, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// New files only: never follow an existing symlink or truncate another file.
func privateFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, errors.New("cannot create secret output file; use a new path in a private directory")
	}
	return f, nil
}

func writePrivate(f *os.File, value string) error {
	if _, err := io.WriteString(f, value); err != nil {
		return errors.New("cannot write secret output file")
	}
	if err := f.Sync(); err != nil {
		return errors.New("cannot sync secret output file")
	}
	return nil
}

func (r Request) emitSecret(streams IO, value string) error {
	if r.Output != "" {
		f, err := privateFile(r.Output)
		if err != nil {
			return err
		}
		err = writePrivate(f, value)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			_ = os.Remove(r.Output)
			return errors.New("cannot save secret output")
		}
		return nil
	}
	if !r.Reveal {
		return errors.New("secret output requires --reveal or --output")
	}
	_, err := io.WriteString(streams.Out, value)
	return err
}

// Read raw input ourselves: x/term.Terminal silently truncates long lines.
func readHidden(reader io.Reader) ([]byte, error) {
	raw := make([]byte, 0, 128)
	var one [1]byte
	var invalid error
	for {
		if _, err := io.ReadFull(reader, one[:]); err != nil {
			clear(raw)
			return nil, err
		}
		switch one[0] {
		case '\r', '\n':
			if invalid != nil {
				clear(raw)
				return nil, invalid
			}
			return raw, nil
		case 3, 4:
			clear(raw)
			return nil, errors.New("input cancelled")
		case 8, 127:
			if len(raw) > 0 {
				_, n := utf8.DecodeLastRune(raw)
				clear(raw[len(raw)-n:])
				raw = raw[:len(raw)-n]
			}
		default:
			if one[0] < 32 {
				clear(raw)
				invalid = errors.New("unsupported control character")
				continue
			}
			if len(raw) >= maxSecretBytes {
				clear(raw)
				invalid = errors.New("secret exceeds 1 MiB")
				continue
			}
			raw = append(raw, one[0])
		}
	}
}
