// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"lefalock/internal/domain"
)

type credentialRepository interface {
	SecretType(context.Context, string, string) (domain.SecretType, error)
	UpdateSecretValue(context.Context, string, string, string) error
	Services(context.Context) ([]domain.Service, error)
	Directories(context.Context) ([]domain.Directory, error)
	CreateDirectory(context.Context, domain.Directory) error
	RenameDirectory(context.Context, string, string) error
	MoveDirectory(context.Context, string, string) error
	DeleteDirectory(context.Context, string) error
	MoveService(context.Context, string, string) error
	MoveSecret(context.Context, string, string) error
	CreateService(context.Context, domain.Service, domain.Secret) error
	CreateSecret(context.Context, domain.Secret) error
	RenameService(context.Context, string, string) error
	RenameSecret(context.Context, string, string, string) error
	DeleteService(context.Context, string) error
	DeleteSecret(context.Context, string, string) error
}

type valueCrypto interface {
	EncryptReference(string, string) (string, error)
	DecryptReference(string, string) (string, error)
}

type Secret struct {
	ID                string
	Type              domain.SecretType
	ServiceID         string
	DirectoryID       string
	Name              string
	EncryptedPassword string
	CreatedAt         time.Time
}

type Service struct {
	ID          string
	DirectoryID string
	Name        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Secrets     []Secret
}

type Directory struct {
	ID       string
	ParentID string
	Name     string
}

type Credentials struct {
	repository credentialRepository
	crypto     valueCrypto
	keyRef     string
}

func NewCredentials(repository credentialRepository, crypto valueCrypto, keyRef string) (*Credentials, error) {
	if repository == nil || crypto == nil || keyRef == "" {
		return nil, errors.New("credential dependencies are required")
	}
	return &Credentials{repository: repository, crypto: crypto, keyRef: keyRef}, nil
}

func (c *Credentials) List(ctx context.Context) ([]Service, error) {
	items, err := c.repository.Services(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Service, 0, len(items))
	for _, item := range items {
		name, err := decryptField(c.crypto, c.keyRef, "service", item.ID, "name", item.NameCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt local service %s: %w", item.ID, err)
		}
		converted := Service{
			ID: item.ID, DirectoryID: item.DirectoryID, Name: name,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
			Secrets:   make([]Secret, 0, len(item.Secrets)),
		}
		for _, account := range item.Secrets {
			accountName, err := decryptField(c.crypto, c.keyRef, "secret", account.ID, "name", account.SecretNameCiphertext)
			if err != nil {
				return nil, fmt.Errorf("decrypt local secret %s: %w", account.ID, err)
			}
			if err := validateField(c.crypto, "secret", account.ID, secretValueField(account.Type), account.PasswordCiphertext); err != nil {
				return nil, err
			}
			converted.Secrets = append(converted.Secrets, Secret{
				ID: account.ID, ServiceID: item.ID, DirectoryID: account.DirectoryID,
				Type: account.Type,
				Name: accountName, EncryptedPassword: account.PasswordCiphertext,
				CreatedAt: account.CreatedAt,
			})
		}
		result = append(result, converted)
	}
	return result, nil
}

func (c *Credentials) ListDirectories(ctx context.Context) ([]Directory, error) {
	items, err := c.repository.Directories(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Directory, 0, len(items))
	for _, item := range items {
		name, err := decryptField(c.crypto, c.keyRef, "directory", item.ID, "name", item.NameCiphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt directory %s: %w", item.ID, err)
		}
		result = append(result, Directory{ID: item.ID, ParentID: item.ParentID, Name: name})
	}
	return result, nil
}

func (c *Credentials) CreateDirectory(ctx context.Context, parentID, name string) error {
	if name == "" {
		return errors.New("directory name is required")
	}
	id, err := domain.NewID()
	if err != nil {
		return err
	}
	ciphertext, err := encryptField(c.crypto, c.keyRef, "directory", id, "name", name)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return c.repository.CreateDirectory(ctx, domain.Directory{ID: id, ParentID: parentID, NameCiphertext: ciphertext, CreatedAt: now, UpdatedAt: now})
}

func (c *Credentials) RenameDirectory(ctx context.Context, id, name string) error {
	if id == "" || name == "" {
		return errors.New("directory id and name are required")
	}
	ciphertext, err := encryptField(c.crypto, c.keyRef, "directory", id, "name", name)
	if err != nil {
		return err
	}
	return c.repository.RenameDirectory(ctx, id, ciphertext)
}

func (c *Credentials) MoveDirectory(ctx context.Context, id, parentID string) error {
	if id == "" {
		return errors.New("directory id is required")
	}
	return c.repository.MoveDirectory(ctx, id, parentID)
}

func (c *Credentials) DeleteDirectory(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("directory id is required")
	}
	return c.repository.DeleteDirectory(ctx, id)
}

func (c *Credentials) MoveService(ctx context.Context, id, directoryID string) error {
	return c.repository.MoveService(ctx, id, directoryID)
}

func (c *Credentials) MoveSecret(ctx context.Context, id, directoryID string) error {
	return c.repository.MoveSecret(ctx, id, directoryID)
}

func (c *Credentials) RevealPassword(ctx context.Context, encrypted string) (string, error) {
	return c.RevealSecretValue(ctx, encrypted)
}

func (c *Credentials) RevealSecretValue(_ context.Context, encrypted string) (string, error) {
	if encrypted == "" {
		return "", errors.New("encrypted secret value is empty")
	}
	return c.crypto.DecryptReference(c.keyRef, encrypted)
}

func (c *Credentials) CreateService(ctx context.Context, name, account, password string) error {
	return c.CreateServiceInDirectory(ctx, "", name, account, password)
}

func (c *Credentials) CreateServiceInDirectory(ctx context.Context, directoryID, name, account, password string) error {
	return c.CreateServiceWithType(ctx, directoryID, name, account, password, domain.SecretPassword)
}

func (c *Credentials) CreateServiceWithType(ctx context.Context, directoryID, name, account, password string, secretType domain.SecretType) error {
	if !secretType.Valid() {
		return errors.New("invalid secret type")
	}
	if secretType == domain.SecretNote {
		account = strings.TrimSpace(account)
	}
	if secretType == domain.SecretAuthCode {
		var err error
		password, err = normalizeAuthKey(password)
		if err != nil {
			return err
		}
	}
	if name == "" || account == "" || (password == "" && secretType != domain.SecretNote) {
		return errors.New("service, secret and password are required")
	}
	serviceID, err := domain.NewID()
	if err != nil {
		return err
	}
	accountID, err := domain.NewID()
	if err != nil {
		return err
	}
	nameCiphertext, err := encryptField(c.crypto, c.keyRef, "service", serviceID, "name", name)
	if err != nil {
		return fmt.Errorf("encrypt service name: %w", err)
	}
	accountCiphertext, err := encryptField(c.crypto, c.keyRef, "secret", accountID, "name", account)
	if err != nil {
		return fmt.Errorf("encrypt secret name: %w", err)
	}
	passwordCiphertext, err := encryptField(c.crypto, c.keyRef, "secret", accountID, secretValueField(secretType), password)
	if err != nil {
		return fmt.Errorf("encrypt password: %w", err)
	}
	now := time.Now().UTC()
	return c.repository.CreateService(ctx,
		domain.Service{ID: serviceID, DirectoryID: directoryID, NameCiphertext: nameCiphertext, CreatedAt: now, UpdatedAt: now},
		domain.Secret{ID: accountID, ServiceID: serviceID, DirectoryID: directoryID, SecretNameCiphertext: accountCiphertext,
			Type:               secretType,
			PasswordCiphertext: passwordCiphertext, CreatedAt: now, UpdatedAt: now},
	)
}

func (c *Credentials) CreateSecret(ctx context.Context, serviceID, account, password string) error {
	return c.CreateSecretInDirectory(ctx, serviceID, "", account, password)
}

func (c *Credentials) CreateSecretInDirectory(ctx context.Context, serviceID, directoryID, account, password string) error {
	return c.CreateSecretWithType(ctx, serviceID, directoryID, account, password, domain.SecretPassword)
}

func (c *Credentials) CreateSecretWithType(ctx context.Context, serviceID, directoryID, account, password string, secretType domain.SecretType) error {
	if !secretType.Valid() {
		return errors.New("invalid secret type")
	}
	if secretType == domain.SecretNote {
		account = strings.TrimSpace(account)
	}
	if secretType == domain.SecretAuthCode {
		var err error
		password, err = normalizeAuthKey(password)
		if err != nil {
			return err
		}
	}
	if serviceID == "" || account == "" || (password == "" && secretType != domain.SecretNote) {
		return errors.New("service, secret and password are required")
	}
	id, err := domain.NewID()
	if err != nil {
		return err
	}
	accountCiphertext, err := encryptField(c.crypto, c.keyRef, "secret", id, "name", account)
	if err != nil {
		return fmt.Errorf("encrypt secret name: %w", err)
	}
	passwordCiphertext, err := encryptField(c.crypto, c.keyRef, "secret", id, secretValueField(secretType), password)
	if err != nil {
		return fmt.Errorf("encrypt password: %w", err)
	}
	now := time.Now().UTC()
	return c.repository.CreateSecret(ctx, domain.Secret{
		ID: id, ServiceID: serviceID, DirectoryID: directoryID, SecretNameCiphertext: accountCiphertext,
		Type:               secretType,
		PasswordCiphertext: passwordCiphertext, CreatedAt: now, UpdatedAt: now,
	})
}

func normalizeAuthKey(value string) (string, error) {
	key := strings.ToUpper(strings.Join(strings.Fields(value), ""))
	key = strings.TrimRight(key, "=")
	if key == "" {
		return "", errors.New("Base32 key is required")
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(key)
	if err != nil || len(decoded) == 0 {
		return "", errors.New("invalid Base32 key")
	}
	return key, nil
}

func (c *Credentials) RenameService(ctx context.Context, id, name string) error {
	name = strings.TrimSpace(name)
	if id == "" || name == "" {
		return errors.New("service id and name are required")
	}
	ciphertext, err := encryptField(c.crypto, c.keyRef, "service", id, "name", name)
	if err != nil {
		return fmt.Errorf("encrypt service name: %w", err)
	}
	return c.repository.RenameService(ctx, id, ciphertext)
}

func (c *Credentials) RenameSecret(ctx context.Context, serviceID, accountID, name string) error {
	name = strings.TrimSpace(name)
	if serviceID == "" || accountID == "" || name == "" {
		return errors.New("service id, secret id and name are required")
	}
	ciphertext, err := encryptField(c.crypto, c.keyRef, "secret", accountID, "name", name)
	if err != nil {
		return fmt.Errorf("encrypt secret name: %w", err)
	}
	return c.repository.RenameSecret(ctx, serviceID, accountID, ciphertext)
}

// UpdateSecretValue replaces a value using the stored type; callers cannot change
// a secret's type or bypass Base32 validation by supplying a different type.
func (c *Credentials) UpdateSecretValue(ctx context.Context, serviceID, secretID, value string) error {
	if serviceID == "" || secretID == "" {
		return errors.New("service id, secret id and value are required")
	}
	secretType, err := c.repository.SecretType(ctx, serviceID, secretID)
	if err != nil {
		return err
	}
	if !secretType.Valid() {
		return errors.New("invalid secret type")
	}
	if value == "" && secretType != domain.SecretNote {
		return errors.New("secret value is required")
	}
	if secretType == domain.SecretAuthCode {
		value, err = normalizeAuthKey(value)
		if err != nil {
			return err
		}
	}
	ciphertext, err := encryptField(c.crypto, c.keyRef, "secret", secretID, secretValueField(secretType), value)
	if err != nil {
		return fmt.Errorf("encrypt secret value: %w", err)
	}
	return c.repository.UpdateSecretValue(ctx, serviceID, secretID, ciphertext)
}

func (c *Credentials) DeleteService(ctx context.Context, serviceID string) error {
	if serviceID == "" {
		return errors.New("service id is required")
	}
	return c.repository.DeleteService(ctx, serviceID)
}

func (c *Credentials) DeleteSecret(ctx context.Context, serviceID, accountID string) error {
	if serviceID == "" || accountID == "" {
		return errors.New("service and secret ids are required")
	}
	return c.repository.DeleteSecret(ctx, serviceID, accountID)
}

// Secret avoids decoding unrelated records when a command addresses one ID.
func (c *Credentials) Secret(ctx context.Context, id string) (Secret, error) {
	repository, ok := c.repository.(interface {
		Secret(context.Context, string) (domain.Secret, error)
	})
	if !ok {
		return Secret{}, errors.New("secret lookup is unavailable")
	}
	item, err := repository.Secret(ctx, id)
	if err != nil {
		return Secret{}, err
	}
	name, err := decryptField(c.crypto, c.keyRef, "secret", item.ID, "name", item.SecretNameCiphertext)
	if err != nil {
		return Secret{}, err
	}
	if err := validateField(c.crypto, "secret", item.ID, secretValueField(item.Type), item.PasswordCiphertext); err != nil {
		return Secret{}, err
	}
	return Secret{ID: item.ID, Type: item.Type, ServiceID: item.ServiceID, DirectoryID: item.DirectoryID, Name: name, EncryptedPassword: item.PasswordCiphertext, CreatedAt: item.CreatedAt}, nil
}
