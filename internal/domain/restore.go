// SPDX-License-Identifier: MPL-2.0

package domain

// RestoreOperation describes an atomic change selected during restore preview.
// The storage adapter checks the baseline before applying these operations.
type RestoreOperation struct {
	Service *Service
	Secret  *Secret
	Replace bool
}
