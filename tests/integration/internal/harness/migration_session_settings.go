// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package harness

import (
	"database/sql"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
)

const (
	// driverModernSQLite names the pure Go SQLite driver.
	driverModernSQLite = "sqlite"

	// driverMattnSQLite names the cgo SQLite driver.
	driverMattnSQLite = "sqlite3"
)

// foreignKeyParameters maps each SQLite driver to the connection parameter that turns
// foreign keys on for every connection it opens.
var foreignKeyParameters = map[string]string{
	driverModernSQLite: "?_pragma=foreign_keys(1)",
	driverMattnSQLite:  "?_foreign_keys=1",
}

// CheckForeignKeyRebuild checks SQLite table rebuilds with foreign keys enabled.
// Referencing rows must survive, connections must restore enforcement, and a migration
// that changes the setting between other statements must be rejected.
//
// Takes t (*testing.T) which reports lost rows or a setting left changed.
// Takes suite (NativeFuzzSuite) which opens the database and names its driver.
func CheckForeignKeyRebuild(t *testing.T, suite NativeFuzzSuite) {
	t.Helper()
	parameter, known := foreignKeyParameters[suite.Driver.Name]
	if !known {
		t.Skipf("capability exclusion: %s is not a SQLite driver", suite.Driver.Name)
	}
	_, connection := suite.OpenNative(t)
	pool, err := sql.Open(suite.Driver.Name, connection+parameter)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	files := fstest.MapFS{
		"migrations/0001_tables.up.sql": {Data: []byte("CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT);\n" +
			"CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent (id) ON DELETE CASCADE);\n" +
			"INSERT INTO parent VALUES (1, 'a'), (2, 'b');\nINSERT INTO child VALUES (10, 1), (11, 2);\n")},
		"migrations/0002_rebuild.up.sql": {Data: []byte("PRAGMA foreign_keys = OFF;\n" +
			"CREATE TABLE parent_new (id INTEGER PRIMARY KEY, name TEXT, extra TEXT);\n" +
			"INSERT INTO parent_new (id, name) SELECT id, name FROM parent;\nDROP TABLE parent;\n" +
			"ALTER TABLE parent_new RENAME TO parent;\nPRAGMA foreign_keys = ON;\n")},
	}
	migrator, err := migrate.NewMigrator(pool, executionBackend(suite.Engine), files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(t.Context())
	require.NoError(t, err, "the rebuild did not apply")
	var children, enforced int
	require.NoError(t, pool.QueryRowContext(t.Context(), "SELECT count(*) FROM child").Scan(&children))
	assert.Equal(t, 2, children, "the rebuild lost rows referring to the rebuilt table")
	require.NoError(t, pool.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&enforced))
	assert.Equal(t, 1, enforced, "a connection no longer enforces foreign keys after the migration")

	files["migrations/0003_middle.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE later (id INTEGER);\nPRAGMA foreign_keys = OFF;\nDROP TABLE later;\n")}
	migrator, err = migrate.NewMigrator(pool, executionBackend(suite.Engine), files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(t.Context())
	assert.ErrorIs(t, err, migrate.ErrSettingInsideTransaction, "a setting between other statements was not refused")
}
