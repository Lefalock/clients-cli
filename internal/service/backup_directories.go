// SPDX-License-Identifier: MPL-2.0

package service

import (
	"errors"
	"fmt"
)

func validateBackupDirectoryTree(document backupDocument) error {
	if document.Version == 1 {
		return nil
	}
	parents := make(map[string]string, len(document.Directories))
	for _, item := range document.Directories {
		if item.ID == "" || item.Name == "" {
			return errors.New("backup contains an invalid directory")
		}
		if _, duplicate := parents[item.ID]; duplicate {
			return fmt.Errorf("backup contains duplicate directory %s", item.ID)
		}
		parents[item.ID] = item.ParentID
	}
	for id := range parents {
		seen := make(map[string]bool)
		for cursor := id; cursor != ""; cursor = parents[cursor] {
			if seen[cursor] {
				return errors.New("backup directory tree contains a cycle")
			}
			seen[cursor] = true
			if _, ok := parents[cursor]; !ok {
				return fmt.Errorf("backup directory %s has a missing parent", id)
			}
		}
	}
	for _, note := range document.Notes {
		if note.DirectoryID != "" {
			if _, ok := parents[note.DirectoryID]; !ok {
				return fmt.Errorf("backup note %q has a missing directory", note.Secret)
			}
		}
	}
	for _, item := range document.Services {
		if item.DirectoryID != "" {
			if _, ok := parents[item.DirectoryID]; !ok {
				return fmt.Errorf("backup service %q has a missing directory", item.Name)
			}
		}
		for _, account := range item.Secrets {
			if account.DirectoryID != "" {
				if _, ok := parents[account.DirectoryID]; !ok {
					return fmt.Errorf("backup secret %q has a missing directory", account.Secret)
				}
			}
		}
	}
	return nil
}
