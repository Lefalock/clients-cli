// SPDX-License-Identifier: MPL-2.0

package localdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"lefalock/internal/ciphertext"
)

// transformFields supplies identity from authoritative columns, never from an
// envelope. Relationships are deliberately excluded so moves preserve ciphertext.
func transformFields(ctx context.Context, tx *sql.Tx, table cipherTable, vaultID string, convert func(ciphertext.Field, string) (string, error)) error {
	entity, idColumn, typeColumn := "", "id", "''"
	switch table.name {
	case "services":
		entity = "service"
	case "directories":
		entity = "directory"
	case "secrets":
		entity = "secret"
		typeColumn = "secret_type"
	default:
		return errors.New("unknown cipher table")
	}
	last := int64(0)
	for {
		rows, err := tx.QueryContext(ctx, "SELECT rowid,"+idColumn+","+typeColumn+","+joinColumns(table.columns, ",")+" FROM "+table.name+" WHERE rowid>? ORDER BY rowid LIMIT 128", last)
		if err != nil {
			return err
		}
		type record struct {
			row     int64
			id, typ string
			values  []string
		}
		batch := []record{}
		for rows.Next() {
			r := record{values: make([]string, len(table.columns))}
			args := []any{&r.row, &r.id, &r.typ}
			for i := range r.values {
				args = append(args, &r.values[i])
			}
			if err := rows.Scan(args...); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, r)
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err = errors.Join(err, closeErr); err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		assignments := make([]string, len(table.columns))
		for i, col := range table.columns {
			assignments[i] = col + "=?"
		}
		for _, r := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			args := []any{}
			for i, value := range r.values {
				if value == "" {
					args = append(args, "")
					continue
				}
				field := "name"
				if entity == "secret" && table.columns[i] == "password_ciphertext" {
					typ := r.typ
					if typ == "" {
						typ = "password"
					}
					field = "value:" + typ
				}
				result, err := convert(ciphertext.Field{Vault: vaultID, Entity: entity, ID: r.id, Name: field}, value)
				if err != nil {
					return fmt.Errorf("record %s field %s: %w", r.id, field, err)
				}
				args = append(args, result)
			}
			args = append(args, r.row)
			if _, err := tx.ExecContext(ctx, "UPDATE "+table.name+" SET "+joinColumns(assignments, ",")+" WHERE rowid=?", args...); err != nil {
				return err
			}
			last = r.row
		}
	}
}
