// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"lefalock/internal/domain"
	"strconv"
	"strings"
)

// User-controlled names cannot inject line breaks or terminal control characters
// into fixed-height context labels. Secrets are copied from their original value.
func displayText(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || (r >= 127 && r < 160) {
			return ' '
		}
		return r
	}, value)
}

// Escapes are explicit so revealing a secret never silently changes its value.
func passwordDisplay(value string) string {
	if displayText(value) != value {
		return "Password (quoted escapes): " + strconv.QuoteToGraphic(value)
	}
	return "Password: " + value
}

func secretValueDisplay(secretType domain.SecretType, value string) string {
	if secretType != domain.SecretAuthCode {
		return passwordDisplay(value)
	}
	if displayText(value) != value {
		return "Base32 key (quoted escapes): " + strconv.QuoteToGraphic(value)
	}
	return "Base32 key: " + value
}
