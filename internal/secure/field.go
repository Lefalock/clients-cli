// SPDX-License-Identifier: MPL-2.0

package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"strings"

	"lefalock/internal/ciphertext"
)

// ForVault creates an adapter that requires ciphertext bound to this vault.
func (c *Crypto) ForVault(id string) *Crypto {
	return &Crypto{keys: c.keys, vaultID: id}
}

func (c *Crypto) EncryptField(ref, entity, id, field, value string) (string, error) {
	if c.vaultID == "" {
		return c.EncryptReference(ref, value)
	} // Unscoped encryption is used for independent payloads and test adapters.
	key, err := c.keys.LoadReference(ref)
	if err != nil {
		return "", err
	}
	defer clearPart(key)
	return EncryptFieldWithKey(key, ciphertext.Field{Vault: c.vaultID, Entity: entity, ID: id, Name: field}, value)
}

func (c *Crypto) DecryptField(ref, entity, id, field, value string) (string, error) {
	if c.vaultID != "" {
		if err := ciphertext.Check(value, ciphertext.Field{Vault: c.vaultID, Entity: entity, ID: id, Name: field}); err != nil {
			return "", err
		}
	}
	return c.DecryptReference(ref, value)
}

func (c *Crypto) ValidateField(entity, id, field, value string) error {
	if c.vaultID == "" {
		return nil
	}
	return ciphertext.Check(value, ciphertext.Field{Vault: c.vaultID, Entity: entity, ID: id, Name: field})
}

func EncryptFieldWithKey(key []byte, field ciphertext.Field, value string) (string, error) {
	header, err := field.Header()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return ciphertext.Encode(header, gcm.Seal(nonce, nonce, []byte(value), header)), nil
}

func decryptEnvelope(key []byte, encoded string) (string, error) {
	_, header, payload, err := ciphertext.Parse(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], header)
	if err != nil {
		return "", errors.New("ciphertext authentication failed")
	}
	defer clearPart(plain)
	return string(plain), nil
}

// ReencryptWithKey retains the authenticated identity during key rotation.
func ReencryptWithKey(oldKey, newKey []byte, value string) (string, error) {
	plain, err := DecryptWithKey(oldKey, value)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(value, ciphertext.Prefix) {
		field, _, _, err := ciphertext.Parse(value)
		if err != nil {
			return "", err
		}
		return EncryptFieldWithKey(newKey, field, plain)
	}
	return EncryptWithKey(newKey, plain)
}

func isEnvelope(encoded string) bool { return strings.Contains(encoded, ":") }
