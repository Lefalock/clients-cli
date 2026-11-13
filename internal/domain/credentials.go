// SPDX-License-Identifier: MPL-2.0

package domain

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("local record not found")

type SecretType string

const (
	SecretPassword SecretType = "password"
	SecretAuthCode SecretType = "auth_code"
	SecretNote     SecretType = "note"
)

func (t SecretType) Valid() bool {
	return t == SecretPassword || t == SecretAuthCode || t == SecretNote
}

// Service encrypts user-controlled values; identifiers and timestamps are metadata.
type Service struct {
	ID             string
	DirectoryID    string
	NameCiphertext string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
	Secrets        []Secret
}

type Secret struct {
	ID                   string
	Type                 SecretType
	ServiceID            string
	DirectoryID          string
	SecretNameCiphertext string
	PasswordCiphertext   string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            *time.Time
}

type Directory struct {
	ID             string
	ParentID       string
	NameCiphertext string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
