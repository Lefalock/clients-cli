// SPDX-License-Identifier: MPL-2.0

package localdb

import (
	"context"
	"fmt"
)

const schemaVersion = 1
const applicationID = 0x4c464c4b // LFLK distinguishes the released format from development databases.

// initialSchema creates the complete local format in one transaction. Future
// published versions can add migrations after this baseline.
const initialSchema = `
CREATE TABLE vault_metadata (
 singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
 vault_id TEXT NOT NULL, key_ref TEXT NOT NULL,
 crypto_version INTEGER NOT NULL DEFAULT 2 CHECK (crypto_version = 2),
 created_at TEXT NOT NULL,
 previous_key_ref TEXT NOT NULL DEFAULT '', prepared_key_ref TEXT NOT NULL DEFAULT ''
);
CREATE TABLE directories (
 id TEXT PRIMARY KEY, parent_id TEXT NULL, name_ciphertext TEXT NOT NULL,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX directories_parent_idx ON directories(parent_id);
CREATE TABLE services (
 id TEXT PRIMARY KEY, name_ciphertext TEXT NOT NULL,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL, deleted_at TEXT NULL,
 directory_id TEXT NULL
);
CREATE INDEX services_directory_idx ON services(directory_id);
CREATE TABLE secrets (
 id TEXT PRIMARY KEY, service_id TEXT NULL REFERENCES services(id),
 secret_name_ciphertext TEXT NOT NULL, password_ciphertext TEXT NOT NULL,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL, deleted_at TEXT NULL,
 directory_id TEXT NULL,
 secret_type TEXT NOT NULL DEFAULT 'password' CHECK (secret_type IN ('password', 'auth_code', 'note')),
 CHECK (secret_type = 'note' OR service_id IS NOT NULL),
 CHECK (secret_type != 'note' OR service_id IS NULL OR directory_id IS NULL)
);
CREATE INDEX secrets_service_idx ON secrets(service_id);
CREATE INDEX secrets_directory_idx ON secrets(directory_id);
CREATE TABLE recovery_state (
 singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
 imported INTEGER NOT NULL DEFAULT 0
);
`

func (s *Store) schemaIdentity(ctx context.Context) (version, appID int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT user_version, application_id FROM pragma_user_version, pragma_application_id`).Scan(&version, &appID)
	return
}

func (s *Store) validateSchema(ctx context.Context) error {
	version, appID, err := s.schemaIdentity(ctx)
	if err != nil {
		return fmt.Errorf("read database format: %w", err)
	}
	if version != schemaVersion || appID != applicationID {
		return fmt.Errorf("unsupported local database format (application %d, schema %d); development databases are not converted", appID, version)
	}
	return nil
}
