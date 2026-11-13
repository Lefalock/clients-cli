// SPDX-License-Identifier: MPL-2.0

package ui

import (
	"lefalock/internal/service"
	"strings"
)

// filterVault copies account slices so filtering never mutates loaded data.
// Password ciphertext is carried through, never inspected or decrypted.
func filterVault(items []service.Service, query string) []service.Service {
	query = strings.ToLower(strings.TrimSpace(query))
	result := make([]service.Service, 0, len(items))
	for _, item := range items {
		matchesService := strings.Contains(strings.ToLower(item.Name), query)
		accounts := make([]service.Secret, 0, len(item.Secrets))
		for _, account := range item.Secrets {
			if matchesService || strings.Contains(strings.ToLower(account.Name), query) {
				accounts = append(accounts, account)
			}
		}
		if matchesService || len(accounts) > 0 {
			item.Secrets = accounts
			result = append(result, item)
		}
	}
	return result
}
