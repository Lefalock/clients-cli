// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"lefalock/internal/domain"
)

type noteRepository interface {
	StandaloneNotes(context.Context) ([]domain.Secret, error)
	CreateNote(context.Context, domain.Secret) error
	UpdateNote(context.Context, string, string) error
	RenameNote(context.Context, string, string) error
	MoveNote(context.Context, string, string, string) error
	DeleteNote(context.Context, string) error
}

func (c *Credentials) noteRepository() (noteRepository, error) {
	r, ok := c.repository.(noteRepository)
	if !ok {
		return nil, errors.New("note storage is unavailable")
	}
	return r, nil
}
func (c *Credentials) ListStandaloneNotes(ctx context.Context) ([]Secret, error) {
	r, err := c.noteRepository()
	if err != nil {
		return nil, err
	}
	notes, err := r.StandaloneNotes(ctx)
	if err != nil {
		return nil, err
	}
	var result []Secret
	for _, n := range notes {
		title, err := decryptField(c.crypto, c.keyRef, "secret", n.ID, "name", n.SecretNameCiphertext)
		if err != nil {
			return nil, err
		}
		if err := validateField(c.crypto, "secret", n.ID, secretValueField(n.Type), n.PasswordCiphertext); err != nil {
			return nil, err
		}
		result = append(result, Secret{ID: n.ID, Type: domain.SecretNote, DirectoryID: n.DirectoryID, Name: title, EncryptedPassword: n.PasswordCiphertext, CreatedAt: n.CreatedAt})
	}
	return result, nil
}
func (c *Credentials) CreateNote(ctx context.Context, serviceID, directoryID, title, text string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("note title is required")
	}
	r, err := c.noteRepository()
	if err != nil {
		return err
	}
	id, err := domain.NewID()
	if err != nil {
		return err
	}
	name, err := encryptField(c.crypto, c.keyRef, "secret", id, "name", title)
	if err != nil {
		return err
	}
	body, err := encryptField(c.crypto, c.keyRef, "secret", id, secretValueField(domain.SecretNote), text)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return r.CreateNote(ctx, domain.Secret{ID: id, Type: domain.SecretNote, ServiceID: serviceID, DirectoryID: directoryID, SecretNameCiphertext: name, PasswordCiphertext: body, CreatedAt: now, UpdatedAt: now})
}
func (c *Credentials) UpdateNote(ctx context.Context, id, text string) error {
	r, err := c.noteRepository()
	if err != nil {
		return err
	}
	ciphertext, err := encryptField(c.crypto, c.keyRef, "secret", id, secretValueField(domain.SecretNote), text)
	if err != nil {
		return err
	}
	return r.UpdateNote(ctx, id, ciphertext)
}
func (c *Credentials) RenameNote(ctx context.Context, id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("note title is required")
	}
	r, err := c.noteRepository()
	if err != nil {
		return err
	}
	ciphertext, err := encryptField(c.crypto, c.keyRef, "secret", id, "name", title)
	if err != nil {
		return err
	}
	return r.RenameNote(ctx, id, ciphertext)
}
func (c *Credentials) MoveNote(ctx context.Context, id, serviceID, directoryID string) error {
	r, err := c.noteRepository()
	if err != nil {
		return err
	}
	return r.MoveNote(ctx, id, serviceID, directoryID)
}
func (c *Credentials) DeleteNote(ctx context.Context, id string) error {
	r, err := c.noteRepository()
	if err != nil {
		return err
	}
	return r.DeleteNote(ctx, id)
}
