// SPDX-License-Identifier: MPL-2.0

package service

import "lefalock/internal/domain"

type fieldCrypto interface {
	EncryptField(string, string, string, string, string) (string, error)
	DecryptField(string, string, string, string, string) (string, error)
	ValidateField(string, string, string, string) error
}

func encryptField(c valueCrypto, ref, entity, id, field, value string) (string, error) {
	if scoped, ok := c.(fieldCrypto); ok {
		return scoped.EncryptField(ref, entity, id, field, value)
	}
	return c.EncryptReference(ref, value)
}
func decryptField(c valueCrypto, ref, entity, id, field, value string) (string, error) {
	if scoped, ok := c.(fieldCrypto); ok {
		return scoped.DecryptField(ref, entity, id, field, value)
	}
	return c.DecryptReference(ref, value)
}
func validateField(c valueCrypto, entity, id, field, value string) error {
	if scoped, ok := c.(fieldCrypto); ok {
		return scoped.ValidateField(entity, id, field, value)
	}
	return nil
}

func secretValueField(t domain.SecretType) string {
	if t == "" {
		t = domain.SecretPassword
	}
	return "value:" + string(t)
}
