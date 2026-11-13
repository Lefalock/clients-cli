// SPDX-License-Identifier: MPL-2.0

// Package ciphertext defines the authenticated, versioned field envelope shared
// by cryptography and storage. It never handles keys or plaintext.
package ciphertext

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
)

const Prefix = "vlt2:"

type Field struct{ Vault, Entity, ID, Name string }

func (f Field) Header() ([]byte, error) {
	result := []byte{2}
	for _, s := range []string{f.Vault, f.Entity, f.ID, f.Name} {
		if len(s) == 0 || len(s) > 65535 {
			return nil, errors.New("invalid ciphertext binding")
		}
		result = binary.BigEndian.AppendUint16(result, uint16(len(s)))
		result = append(result, s...)
	}
	return result, nil
}

func Encode(header, payload []byte) string {
	return Prefix + hex.EncodeToString(append(append([]byte{}, header...), payload...))
}

// Parse rejects every unknown/truncated envelope. Callers must never retry an
// invalid versioned value as a legacy ciphertext.
func Parse(encoded string) (Field, []byte, []byte, error) {
	fail := func() (Field, []byte, []byte, error) {
		return Field{}, nil, nil, errors.New("invalid ciphertext envelope")
	}
	if !strings.HasPrefix(encoded, Prefix) {
		return fail()
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(encoded, Prefix))
	if err != nil || len(raw) == 0 || raw[0] != 2 {
		return fail()
	}
	pos := 1
	parts := make([]string, 4)
	for i := range parts {
		if len(raw)-pos < 2 {
			return fail()
		}
		n := int(binary.BigEndian.Uint16(raw[pos:]))
		pos += 2
		if n == 0 || len(raw)-pos < n {
			return fail()
		}
		parts[i] = string(raw[pos : pos+n])
		pos += n
	}
	if len(raw)-pos < 28 {
		return fail()
	}
	return Field{parts[0], parts[1], parts[2], parts[3]}, raw[:pos], raw[pos:], nil
}

func Check(encoded string, expected Field) error {
	field, _, _, err := Parse(encoded)
	if err != nil {
		return err
	}
	if field != expected {
		return errors.New("ciphertext belongs to another vault, record or field")
	}
	return nil
}
