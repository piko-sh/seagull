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
	require.Equal(t, 2, recordedVersion, "this release splits statements by splitter version 2")
	require.Len(t, statementsHash, 64)
	require.Equal(t, []harness.HistoryEntry{{Version: 1, Dirty: false, LastStatement: 1}}, harness.ReadHistory(t, database))
}

func TestSQLiteRollbackThatStopsPartWayLeavesTheMigrationDirty(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_boats.up.sql":   {Data: []byte("CREATE TABLE boats (id INTEGER PRIMARY KEY);\nCREATE TABLE harbours (id INTEGER PRIMARY KEY);\n")},
		"migrations/0001_boats.down.sql": {Data: []byte("-- sgl.migration(no_transaction: true)\nDROP TABLE harbours;\nDROP TABLE no_such_table;\nDROP TABLE boats;\n")},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(ctx)
	require.NoError(t, err)

	_, err = migrator.Down(ctx, 1)
	require.Error(t, err, "the second statement of the down migration fails")

	history := harness.ReadHistory(t, database)
	require.Len(t, history, 1, "the half-rolled-back migration is still recorded")
	require.True(t, history[0].Dirty, "a rollback that stopped part-way is recorded dirty rather than applied")

	var dirty *migrate.DirtyMigrationError
	require.ErrorAs(t, migrator.Validate(ctx), &dirty, "validate reports the half-rolled-back migration")
	_, err = migrator.Up(ctx)
	require.ErrorAs(t, err, &dirty, "up never resumes the up migration over a half-dropped schema")
	_, err = migrator.Down(ctx, 1)
	require.Error(t, err, "a second rollback refuses the dirty migration")
}

func TestSQLiteMigrationThatControlsItsTransactionIsRefusedBeforeItRuns(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_boats.up.sql": {Data: []byte("CREATE TABLE boats (id INTEGER PRIMARY KEY);\nCOMMIT;\nCREATE TABLE harbours (id INTEGER PRIMARY KEY);\n")},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), files, "migrations")
	require.NoError(t, err)

	_, err = migrator.Up(ctx)

	require.ErrorIs(t, err, migrate.ErrTransactionControl)
	var tables int
	require.NoError(t, database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name IN ('boats', 'harbours')").Scan(&tables))
	require.Zero(t, tables, "nothing of the refused migration ran")
	require.Empty(t, harness.ReadHistory(t, database))
}

func TestSQLiteSeedWithAByteOrderMarkApplies(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_boats.up.sql": {Data: []byte("CREATE TABLE boats (id INTEGER PRIMARY KEY);\n")},
		"seeds/0001_boats.sql":         {Data: []byte("\xEF\xBB\xBFINSERT INTO boats (id) VALUES (1);\r\nINSERT INTO boats (id) VALUES (2);\r\n")},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(ctx)
	require.NoError(t, err)
	seeder, err := migrate.NewSeeder(database, migrate_backend_sqlite.New(), files, "seeds")
	require.NoError(t, err)

	applied, err := seeder.Apply(ctx)

	require.NoError(t, err, "a seed file an editor saved with a byte order mark and CRLF line endings applies")
	require.Equal(t, 1, applied)
	var boats int
	require.NoError(t, database.QueryRowContext(ctx, "SELECT count(*) FROM boats").Scan(&boats))
	require.Equal(t, 2, boats)
}
