// SPDX-License-Identifier: MPL-2.0

// Package passwordgen generates passwords using cryptographic randomness.
package passwordgen

import (
	"crypto/rand"
	"errors"
	"io"
	"math/big"
)

const Lower = "abcdefghijklmnopqrstuvwxyz"
const Upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
const Digits = "0123456789"
const Special = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"

type Options struct {
	Length                        int
	Lower, Upper, Digits, Special bool
}

func DefaultOptions() Options { return Options{20, true, true, true, true} }
func (o Options) sets() []string {
	var sets []string
	for i, enabled := range []bool{o.Lower, o.Upper, o.Digits, o.Special} {
		if enabled {
			sets = append(sets, []string{Lower, Upper, Digits, Special}[i])
		}
	}
	return sets
}
func Validate(o Options) error {
	if o.Length < 4 || o.Length > 128 {
		return errors.New("Length must be between 4 and 128")
	}
	if len(o.sets()) == 0 {
		return errors.New("Select at least one character set")
	}
	if o.Length < len(o.sets()) {
		return errors.New("Length must include every selected character set")
	}
	return nil
}
func Generate(o Options) (string, error) { return generate(o, rand.Reader) }
func generate(o Options, reader io.Reader) (string, error) {
	if err := Validate(o); err != nil {
		return "", err
	}
	pick := func(n int) (int, error) {
		v, err := rand.Int(reader, big.NewInt(int64(n)))
		if err != nil {
			return 0, errors.New("Unable to generate password: random source unavailable")
		}
		return int(v.Int64()), nil
	}
	result := make([]byte, o.Length)
	defer clear(result)
	sets := o.sets()
	alphabet := ""
	for i, set := range sets {
		alphabet += set
		index, err := pick(len(set))
		if err != nil {
			return "", err
		}
		result[i] = set[index]
	}
	for i := len(sets); i < len(result); i++ {
		index, err := pick(len(alphabet))
		if err != nil {
			return "", err
		}
		result[i] = alphabet[index]
	}
	for i := len(result) - 1; i > 0; i-- {
		j, err := pick(i + 1)
		if err != nil {
			return "", err
		}
		result[i], result[j] = result[j], result[i]
	}
	return string(result), nil
}
