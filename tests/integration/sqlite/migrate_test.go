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

//go:build integration

package sqlite_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const legacyHistoryDDL = `CREATE TABLE sgl_migrations (
    version        INTEGER NOT NULL PRIMARY KEY,
    name           TEXT    NOT NULL,
    checksum       TEXT    NOT NULL,
    applied_at     TEXT    NOT NULL DEFAULT (datetime('now')),
    duration_ms    INTEGER NOT NULL,
    down_checksum  TEXT,
    last_statement INTEGER,
    dirty          INTEGER NOT NULL DEFAULT 0
)`

func TestSQLiteMigratorAndSeederLifecycle(t *testing.T) {
	database := openMigrationDatabase(t)
	files := harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"DELETE FROM "+harness.MigrationTable+";",
	)
	harness.MigratorLifecycle(t, database, migrate_backend_sqlite.New(), files, harness.SettleImmediately)
}

func openMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "migrate.db")+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestSQLiteLegacyDirtyMigrationIsUpgradedAndNotResumed(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	content := []byte("CREATE TABLE boats (id INTEGER PRIMARY KEY);\nCREATE TABLE harbours (id INTEGER PRIMARY KEY);\n")
	files := fstest.MapFS{"migrations/0001_boats.up.sql": {Data: content}}
	for _, statement := range []string{
		legacyHistoryDDL,
		"INSERT INTO sgl_migrations (version, name, checksum, duration_ms, last_statement, dirty) VALUES (1, 'boats', '" + harness.ChecksumOf(content) + "', 0, 0, 1)",
		"CREATE TABLE boats (id INTEGER PRIMARY KEY)",
	} {
		_, err := database.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), files, "migrations")
	require.NoError(t, err)

	statuses, err := migrator.Status(ctx)
	require.NoError(t, err, "status must read a history table an earlier release created")
	require.True(t, statuses[0].Dirty)

	_, err = migrator.Up(ctx)

	var mismatch *migrate.ResumeMismatchError
	require.ErrorAs(t, err, &mismatch)
	require.Equal(t, 0, mismatch.LastStatement)
	require.Zero(t, mismatch.RecordedSplitterVersion)
	require.Equal(t, []harness.HistoryEntry{{Version: 1, Dirty: true, LastStatement: 0}}, harness.ReadHistory(t, database), "a refused migration stays dirty")

	for _, statement := range []string{"DROP TABLE boats", "UPDATE sgl_migrations SET last_statement = -1 WHERE version = 1"} {
		_, err = database.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	applied, err := migrator.Up(ctx)

	require.NoError(t, err, "once repaired, the upgraded table resumes the migration from the start")
	require.Equal(t, 1, applied)
	var recordedVersion int
	var statementsHash string
	require.NoError(t, database.QueryRowContext(ctx, "SELECT splitter_version, statements_hash FROM sgl_migrations WHERE version = 1").Scan(&recordedVersion, &statementsHash))
	require.Equal(t, 1, recordedVersion)
	require.Len(t, statementsHash, 64)
	require.Equal(t, []harness.HistoryEntry{{Version: 1, Dirty: false, LastStatement: 1}}, harness.ReadHistory(t, database))
}
