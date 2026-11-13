// SPDX-License-Identifier: MPL-2.0

package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lefalock/internal/config"
	"lefalock/internal/domain"
	"lefalock/internal/passwordgen"
	"lefalock/internal/recovery"
	"lefalock/internal/service"
)

type Clipboard interface {
	Copy(string) error
	Close() error
}

type Dependencies struct {
	Credentials *service.Credentials
	Backup      *service.Backup
	Recovery    *recovery.Manager
	Rotate      func(context.Context) (service.RotationResult, error)
	Clipboard   func(time.Duration) Clipboard
	ConfigPath  string
}

// Metadata DTOs deliberately have no encrypted or plaintext secret fields.
type Secret struct {
	ID        string            `json:"id"`
	Type      domain.SecretType `json:"type"`
	ServiceID string            `json:"service_id"`
	FolderID  string            `json:"folder_id"`
	Name      string            `json:"name"`
	CreatedAt string            `json:"created_at"`
}
type Service struct {
	ID        string   `json:"id"`
	FolderID  string   `json:"folder_id"`
	Name      string   `json:"name"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	Secrets   []Secret `json:"secrets"`
}
type Folder struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
}

func metadata(s service.Secret) Secret {
	return Secret{s.ID, s.Type, s.ServiceID, s.DirectoryID, s.Name, s.CreatedAt.Local().Format(time.DateTime)}
}
func encode(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func (r Request) Execute(ctx context.Context, d Dependencies, streams IO) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.Help {
		return nil
	}
	if r.Command == "password generate" {
		value, err := passwordgen.Generate(r.Generator)
		if err != nil {
			return err
		}
		return r.emitSecret(streams, value)
	}
	if strings.HasPrefix(r.Command, "recovery ") {
		return r.recover(ctx, d, streams)
	}
	if strings.HasPrefix(r.Command, "backup ") {
		return r.backup(ctx, d, streams)
	}
	c := d.Credentials
	if c == nil {
		return errors.New("vault is not open")
	}
	success := func(err error) error {
		if err != nil {
			return err
		}
		return encode(streams.Out, map[string]bool{"ok": true})
	}
	switch r.Command {
	case "vault init":
		return success(nil)
	case "vault rotate-key":
		result, err := d.Rotate(ctx)
		if err != nil {
			return err
		}
		warning := ""
		if result.Warning != nil {
			warning = result.Warning.Error()
		}
		return encode(streams.Out, struct {
			Committed bool   `json:"committed"`
			Warning   string `json:"warning,omitempty"`
		}{result.Committed, warning})
	case "vault list":
		folders, err := r.folders(ctx, c)
		if err != nil {
			return err
		}
		services, err := r.services(ctx, c)
		if err != nil {
			return err
		}
		notes, err := c.ListStandaloneNotes(ctx)
		if err != nil {
			return err
		}
		visible := []Secret{}
		for _, n := range notes {
			if r.matches(n.Name) {
				visible = append(visible, metadata(n))
			}
		}
		return encode(streams.Out, struct {
			Folders  []Folder  `json:"folders"`
			Services []Service `json:"services"`
			Notes    []Secret  `json:"notes"`
		}{folders, services, visible})
	case "folder list":
		items, err := r.folders(ctx, c)
		if err != nil {
			return err
		}
		return encode(streams.Out, items)
	case "folder create":
		return success(c.CreateDirectory(ctx, r.Parent, r.Name))
	case "folder rename":
		return success(c.RenameDirectory(ctx, r.ID, r.Name))
	case "folder move":
		return success(c.MoveDirectory(ctx, r.ID, r.Parent))
	case "folder delete":
		return success(c.DeleteDirectory(ctx, r.ID))
	case "service list":
		items, err := r.services(ctx, c)
		if err != nil {
			return err
		}
		return encode(streams.Out, items)
	case "service create":
		value, err := r.readValue(streams, false, r.Type == "password")
		if err != nil {
			return err
		}
		return success(c.CreateServiceWithType(ctx, r.Folder, r.Name, r.SecretName, value, domain.SecretType(r.Type)))
	case "service rename":
		return success(c.RenameService(ctx, r.ID, r.Name))
	case "service move":
		return success(c.MoveService(ctx, r.ID, r.Folder))
	case "service delete":
		return success(c.DeleteService(ctx, r.ID))
	case "secret list", "note list":
		items, err := r.secrets(ctx, c)
		if err != nil {
			return err
		}
		return encode(streams.Out, items)
	case "secret create":
		value, err := r.readValue(streams, false, r.Type == "password")
		if err != nil {
			return err
		}
		return success(c.CreateSecretWithType(ctx, r.Service, r.Folder, r.Name, value, domain.SecretType(r.Type)))
	case "note create":
		value, err := r.readValue(streams, true, false)
		if err != nil {
			return err
		}
		return success(c.CreateNote(ctx, r.Service, r.Folder, r.Name, value))
	}
	// Every secret/note mutation resolves its ID and type before reading input.
	s, err := findSecret(ctx, c, r.ID)
	if err != nil {
		return err
	}
	if strings.HasPrefix(r.Command, "note ") && s.Type != domain.SecretNote {
		return errors.New("ID is not a note")
	}
	if strings.HasPrefix(r.Command, "secret ") && s.Type == domain.SecretNote {
		return errors.New("use note commands for this ID")
	}
	switch r.Command {
	case "secret set":
		if r.Generate && s.Type != domain.SecretPassword {
			return errors.New("generation is only available for passwords")
		}
		value, err := r.readValue(streams, false, s.Type == domain.SecretPassword)
		if err != nil {
			return err
		}
		return success(c.UpdateSecretValue(ctx, s.ServiceID, s.ID, value))
	case "note set":
		value, err := r.readValue(streams, true, false)
		if err != nil {
			return err
		}
		return success(c.UpdateNote(ctx, s.ID, value))
	case "secret rename":
		return success(c.RenameSecret(ctx, s.ServiceID, s.ID, r.Name))
	case "note rename":
		return success(c.RenameNote(ctx, s.ID, r.Name))
	case "secret move":
		return success(c.MoveSecret(ctx, s.ID, r.Folder))
	case "note move":
		return success(c.MoveNote(ctx, s.ID, r.Service, r.Folder))
	case "secret delete":
		return success(c.DeleteSecret(ctx, s.ServiceID, s.ID))
	case "note delete":
		return success(c.DeleteNote(ctx, s.ID))
	case "secret show", "note show", "secret code", "secret copy", "note copy":
		var value string
		if r.Command == "secret code" || (r.Command == "secret copy" && s.Type == domain.SecretAuthCode) {
			if s.Type != domain.SecretAuthCode {
				return errors.New("ID is not an auth code")
			}
			value, err = c.GenerateAuthCode(ctx, s.EncryptedPassword, time.Now())
		} else {
			value, err = c.RevealSecretValue(ctx, s.EncryptedPassword)
		}
		if err != nil {
			return err
		}
		if strings.HasSuffix(r.Command, " copy") {
			return r.copy(ctx, d, streams, value)
		}
		return r.emitSecret(streams, value)
	}
	return errors.New("unsupported vault command")
}

func (r Request) readValue(streams IO, multiline, confirm bool) (string, error) {
	if r.Generate {
		return passwordgen.Generate(r.Generator)
	}
	return r.Input.Read(streams, multiline, confirm)
}

func (r Request) matches(name string) bool {
	return strings.Contains(strings.ToLower(name), strings.ToLower(r.Query))
}

func (r Request) folders(ctx context.Context, c *service.Credentials) ([]Folder, error) {
	items, err := c.ListDirectories(ctx)
	if err != nil {
		return nil, err
	}
	result := []Folder{}
	for _, f := range items {
		if r.matches(f.Name) {
			result = append(result, Folder{f.ID, f.ParentID, f.Name})
		}
	}
	return result, nil
}

func (r Request) services(ctx context.Context, c *service.Credentials) ([]Service, error) {
	items, err := c.List(ctx)
	if err != nil {
		return nil, err
	}
	result := []Service{}
	for _, s := range items {
		if r.set["folder"] && s.DirectoryID != r.Folder {
			continue
		}
		item := Service{s.ID, s.DirectoryID, s.Name, s.CreatedAt.Local().Format(time.DateTime), s.UpdatedAt.Local().Format(time.DateTime), []Secret{}}
		for _, secret := range s.Secrets {
			if r.matches(s.Name) || r.matches(secret.Name) {
				item.Secrets = append(item.Secrets, metadata(secret))
			}
		}
		if r.matches(s.Name) || len(item.Secrets) > 0 {
			result = append(result, item)
		}
	}
	return result, nil
}

func (r Request) secrets(ctx context.Context, c *service.Credentials) ([]Secret, error) {
	services, err := c.List(ctx)
	if err != nil {
		return nil, err
	}
	all := []service.Secret{}
	for _, s := range services {
		all = append(all, s.Secrets...)
	}
	notes, err := c.ListStandaloneNotes(ctx)
	if err != nil {
		return nil, err
	}
	all = append(all, notes...)
	result := []Secret{}
	for _, s := range all {
		if (r.Command == "note list") != (s.Type == domain.SecretNote) {
			continue
		}
		if !r.matches(s.Name) || (r.set["service"] && s.ServiceID != r.Service) || (r.set["folder"] && s.DirectoryID != r.Folder) {
			continue
		}
		result = append(result, metadata(s))
	}
	return result, nil
}

func findSecret(ctx context.Context, c *service.Credentials, id string) (service.Secret, error) {
	item, err := c.Secret(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return service.Secret{}, fmt.Errorf("secret ID not found: %w", err)
	}
	return item, err
}

func (r Request) copy(ctx context.Context, d Dependencies, streams IO, value string) error {
	if d.Clipboard == nil {
		return errors.New("clipboard is unavailable")
	}
	c := d.Clipboard(r.TTL)
	if err := c.Copy(value); err != nil {
		_ = c.Close()
		return errors.New("cannot copy secret to system clipboard")
	}
	fmt.Fprintln(streams.Err, "Copied; waiting for conditional clipboard cleanup. Ctrl+C clears early.")
	timer := time.NewTimer(r.TTL)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	return c.Close()
}

func (r Request) backup(ctx context.Context, d Dependencies, streams IO) error {
	b := d.Backup
	if b == nil {
		return errors.New("backup is unavailable")
	}
	if r.Command == "backup create" {
		// Reserve the phrase file before creating the backup, so a bad output
		// destination cannot leave a backup with an unrecoverable phrase.
		f, err := privateFile(r.PhraseOutput)
		if err != nil {
			return err
		}
		keep := false
		defer func() {
			f.Close()
			if !keep {
				_ = os.Remove(r.PhraseOutput)
			}
		}()
		directory := r.Directory
		if directory == "" {
			directory, err = b.DefaultDirectory()
			if err != nil {
				return err
			}
		}
		result, err := b.Create(ctx, directory)
		if err != nil {
			return err
		}
		if err = writePrivate(f, result.Mnemonic+"\n"); err != nil {
			_ = os.Remove(result.Path)
			return err
		}
		if err = f.Close(); err != nil {
			_ = os.Remove(result.Path)
			return errors.New("cannot close recovery phrase file")
		}
		keep = true
		return encode(streams.Out, map[string]string{"backup": result.Path, "phrase_file": r.PhraseOutput})
	}
	phrase, err := r.Input.Read(streams, false, false)
	if err != nil {
		return err
	}
	preview, err := b.PreviewRestore(ctx, r.File, phrase)
	if err != nil {
		return err
	}
	decisions, err := readDecisions(r.Decisions, preview)
	if err != nil {
		return err
	}
	if r.Command == "backup preview" {
		// Conflicts contain metadata only. The decrypted backup document is private.
		var summary *service.RestoreSummary
		if len(preview.Conflicts) == 0 || r.Decisions != "" {
			s, err := b.RestoreSummaryFor(preview, decisions)
			if err != nil {
				return err
			}
			summary = &s
		}
		return encode(streams.Out, struct {
			Conflicts []service.RestoreConflict `json:"conflicts"`
			Initial   service.RestoreSummary    `json:"initial"`
			Summary   *service.RestoreSummary   `json:"summary,omitempty"`
		}{preview.Conflicts, preview.Initial, summary})
	}
	summary, err := b.ApplyRestore(ctx, preview, decisions)
	if err != nil {
		return err
	}
	if d.ConfigPath != "" {
		if err := config.RememberRestoreDirectory(d.ConfigPath, filepath.Dir(r.File)); err != nil {
			fmt.Fprintln(streams.Err, "Restore succeeded; could not remember backup directory")
		}
	}
	return encode(streams.Out, summary)
}

func readDecisions(path string, preview *service.RestorePreview) (map[string]service.RestoreDecision, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open restore decisions file")
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, maxSecretBytes+1))
	decoder.DisallowUnknownFields()
	var decisions map[string]service.RestoreDecision
	if err := decoder.Decode(&decisions); err != nil {
		return nil, errors.New("invalid restore decisions JSON")
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		return nil, errors.New("trailing data in restore decisions JSON")
	}
	known := map[string]bool{}
	for _, c := range preview.Conflicts {
		known[c.Key] = true
	}
	for key := range decisions {
		if !known[key] {
			return nil, errors.New("restore decisions contain an unknown conflict key")
		}
	}
	return decisions, nil
}

func (r Request) recover(ctx context.Context, d Dependencies, streams IO) error {
	m := d.Recovery
	if m == nil {
		return errors.New("recovery is unavailable")
	}
	var err error
	switch r.Command {
	case "recovery status":
		status, err := m.Status(ctx)
		if err != nil {
			return err
		}
		return encode(streams.Out, status)
	case "recovery begin":
		err = m.Begin(ctx)
	case "recovery empty":
		if err = m.BeginEmpty(ctx); err == nil {
			err = m.ContinueEmpty(ctx)
		}
	case "recovery continue-empty":
		err = m.ContinueEmpty(ctx)
	case "recovery cancel":
		err = m.Cancel(ctx)
	case "recovery activate":
		err = m.Activate(ctx)
	case "recovery inspect-pointer", "recovery repair-pointer":
		preview, e := m.InspectPointerRepair(ctx)
		if e != nil {
			return e
		}
		if r.Command == "recovery inspect-pointer" {
			return encode(streams.Out, preview)
		}
		err = m.RepairPointer(ctx, preview)
	case "recovery preview", "recovery restore":
		phrase, e := r.Input.Read(streams, false, false)
		if e != nil {
			return e
		}
		preview, e := m.Preview(ctx, r.File, phrase)
		if e != nil {
			return e
		}
		if r.Command == "recovery preview" {
			summary, e := m.Summary(preview)
			if e != nil {
				return e
			}
			return encode(streams.Out, summary)
		}
		err = m.Apply(ctx, preview)
	}
	if err != nil {
		return err
	}
	return encode(streams.Out, map[string]bool{"ok": true})
}
