// SPDX-License-Identifier: MPL-2.0

package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"lefalock/internal/ciphertext"
)

type Crypto struct {
	keys    *KeyStore
	vaultID string
}

func NewCrypto(keys *KeyStore) (*Crypto, error) {
	if keys == nil {
		return nil, errors.New("key store is required")
	}
	return &Crypto{keys: keys}, nil
}

func (c *Crypto) EncryptReference(reference, plaintext string) (string, error) {
	return c.encrypt(c.keys.LoadReference, reference, plaintext)
}

func (c *Crypto) encrypt(load func(string) ([]byte, error), owner, plaintext string) (string, error) {
	key, err := load(owner)
	if err != nil {
		return "", err
	}
	defer clearPart(key)
	return EncryptWithKey(key, plaintext)
}

func EncryptWithKey(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

func (c *Crypto) DecryptReference(reference, encoded string) (string, error) {
	return c.decrypt(c.keys.LoadReference, reference, encoded)
}

func (c *Crypto) decrypt(load func(string) ([]byte, error), owner, encoded string) (string, error) {
	key, err := load(owner)
	if err != nil {
		return "", err
	}
	defer clearPart(key)
	if c.vaultID != "" && !isEnvelope(encoded) {
		return "", errors.New("legacy ciphertext is forbidden in this vault")
	}
	if c.vaultID != "" && isEnvelope(encoded) {
		field, _, _, err := ciphertext.Parse(encoded)
		if err != nil {
			return "", err
		}
		if field.Vault != c.vaultID {
			return "", errors.New("ciphertext belongs to another vault")
		}
	}

	return DecryptWithKey(key, encoded)
}

func DecryptWithKey(key []byte, encoded string) (string, error) {
	if isEnvelope(encoded) {
		return decryptEnvelope(key, encoded)
	}
	ciphertext, err := hex.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", errors.New("ciphertext is too short")
	}
	nonce, ciphertext := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt value: %w", err)
	}
	return string(plaintext), nil
}
