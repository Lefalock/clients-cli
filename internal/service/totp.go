// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

const totpPeriod = 30 * time.Second

// GenerateAuthCode produces the standard six-digit TOTP for an encrypted
// Base32 key. The key is never returned to the UI for code generation.
func (c *Credentials) GenerateAuthCode(ctx context.Context, encrypted string, at time.Time) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if encrypted == "" {
		return "", errors.New("encrypted Auth code key is empty")
	}
	key, err := c.crypto.DecryptReference(c.keyRef, encrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt Auth code key: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return totpCode(key, at)
}

func totpCode(encodedKey string, at time.Time) (string, error) {
	if at.Unix() < 0 {
		return "", errors.New("TOTP time is before the Unix epoch")
	}
	key := strings.ToUpper(strings.Join(strings.Fields(encodedKey), ""))
	key = strings.TrimRight(key, "=")
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(key)
	if err != nil || len(secret) == 0 {
		return "", errors.New("invalid Base32 key")
	}
	defer clearBytes(secret)
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(at.Unix()/int64(totpPeriod/time.Second)))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(counter[:])
	digest := mac.Sum(nil)
	defer clearBytes(digest)
	offset := int(digest[len(digest)-1] & 0x0f)
	truncated := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", truncated%1000000), nil
}
