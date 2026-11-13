// SPDX-License-Identifier: MPL-2.0

package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"lefalock/internal/domain"
)

func noteRestoreKey(i int) string { return fmt.Sprintf("note:%d", i) }
func folderPath(id string, parents, names map[string]string) string {
	var pieces []string
	for id != "" {
		pieces = append(pieces, names[id])
		id = parents[id]
	}
	var path strings.Builder
	for i := len(pieces) - 1; i >= 0; i-- {
		fmt.Fprintf(&path, "%d:%s/", len(pieces[i]), pieces[i])
	}
	return path.String()
}
func (b *Backup) noteFolderPaths(preview *RestorePreview) (map[string]string, map[string]string, error) {
	parents, names := map[string]string{}, map[string]string{}
	for _, d := range preview.baselineDirectories {
		name, err := decryptField(b.crypto, b.keyRef, "directory", d.ID, "name", d.NameCiphertext)
		if err != nil {
			return nil, nil, err
		}
		parents[d.ID] = d.ParentID
		names[d.ID] = name
	}
	current := map[string]string{"": ""}
	for id := range names {
		current[id] = folderPath(id, parents, names)
	}
	parents, names = map[string]string{}, map[string]string{}
	for _, d := range preview.document.Directories {
		parents[d.ID] = d.ParentID
		names[d.ID] = d.Name
	}
	incoming := map[string]string{"": ""}
	for id := range names {
		incoming[id] = folderPath(id, parents, names)
	}
	return current, incoming, nil
}
func (b *Backup) matchStandaloneNotes(preview *RestorePreview) (map[int]domain.Secret, error) {
	current, incoming, err := b.noteFolderPaths(preview)
	if err != nil {
		return nil, err
	}
	candidates := map[string][]domain.Secret{}
	for _, n := range preview.baselineNotes {
		title, err := decryptField(b.crypto, b.keyRef, "secret", n.ID, "name", n.SecretNameCiphertext)
		if err != nil {
			return nil, err
		}
		key := current[n.DirectoryID] + "\x00" + title
		candidates[key] = append(candidates[key], n)
	}
	result := map[int]domain.Secret{}
	positions := map[string]int{}
	for i, n := range preview.document.Notes {
		key := incoming[n.DirectoryID] + "\x00" + n.Secret
		position := positions[key]
		positions[key]++
		if position < len(candidates[key]) {
			result[i] = candidates[key][position]
		}
	}
	return result, nil
}
func (b *Backup) summarizeStandaloneNotes(preview *RestorePreview, decisions map[string]RestoreDecision, result *RestoreSummary) error {
	matches, err := b.matchStandaloneNotes(preview)
	if err != nil {
		return err
	}
	current, incoming, err := b.noteFolderPaths(preview)
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for _, n := range preview.baselineNotes {
		title, err := decryptField(b.crypto, b.keyRef, "secret", n.ID, "name", n.SecretNameCiphertext)
		if err != nil {
			return err
		}
		used[current[n.DirectoryID]+"\x00"+title] = true
	}
	for _, n := range preview.document.Notes {
		used[incoming[n.DirectoryID]+"\x00"+n.Secret] = true
	}
	for i, n := range preview.document.Notes {
		if _, found := matches[i]; !found {
			result.SecretsAdded++
			continue
		}
		choice := decisions[noteRestoreKey(i)]
		switch choice.Choice {
		case RestoreKeepCurrent:
			result.SecretsKept++
		case RestoreReplace:
			result.SecretsReplaced++
		case RestoreKeepBoth:
			name := strings.TrimSpace(choice.NewName)
			key := incoming[n.DirectoryID] + "\x00" + name
			if name == "" || used[key] {
				return errors.New("choose a unique new title for the folder note")
			}
			used[key] = true
			result.SecretsAdded++
		default:
			return errors.New("choose how to restore the folder note")
		}
	}
	return nil
}
func (b *Backup) restoreStandaloneNotes(preview *RestorePreview, decisions map[string]RestoreDecision, directoryMap map[string]string) ([]domain.RestoreOperation, error) {
	matches, err := b.matchStandaloneNotes(preview)
	if err != nil {
		return nil, err
	}
	var operations []domain.RestoreOperation
	for i, n := range preview.document.Notes {
		local, found := matches[i]
		choice := decisions[noteRestoreKey(i)]
		if found && choice.Choice == RestoreKeepCurrent {
			continue
		}
		id := local.ID
		if !(found && choice.Choice == RestoreReplace) {
			id, err = domain.NewID()
			if err != nil {
				return nil, err
			}
		}
		body, err := encryptField(b.crypto, b.keyRef, "secret", id, secretValueField(domain.SecretNote), n.Password)
		if err != nil {
			return nil, err
		}
		if found && choice.Choice == RestoreReplace {
			operations = append(operations, domain.RestoreOperation{Secret: &domain.Secret{ID: local.ID, Type: domain.SecretNote, PasswordCiphertext: body, UpdatedAt: time.Now().UTC()}, Replace: true})
			continue
		}
		name := n.Secret
		if found && choice.Choice == RestoreKeepBoth {
			name = strings.TrimSpace(choice.NewName)
		}
		title, err := encryptField(b.crypto, b.keyRef, "secret", id, "name", name)
		if err != nil {
			return nil, err
		}
		created := n.Created
		if created.IsZero() {
			created = time.Now().UTC()
		}
		operations = append(operations, domain.RestoreOperation{Secret: &domain.Secret{ID: id, Type: domain.SecretNote, DirectoryID: directoryMap[n.DirectoryID], SecretNameCiphertext: title, PasswordCiphertext: body, CreatedAt: created, UpdatedAt: time.Now().UTC()}})
	}
	return operations, nil
}

func restoreNoteLocation(document backupDocument, id string) string {
	parents, names := map[string]string{}, map[string]string{}
	for _, d := range document.Directories {
		parents[d.ID], names[d.ID] = d.ParentID, d.Name
	}
	path := []string{}
	for id != "" {
		path = append(path, names[id])
		id = parents[id]
	}
	result := "Root"
	for i := len(path) - 1; i >= 0; i-- {
		result += " / " + path[i]
	}
	return result
}
