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

package duckdb_test

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_duckdb"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const (
	missingTable = "sgl_it_labels"
	counterTable = "sgl_it_counter"
)

var widgetMigrations = harness.MigrationFiles(
	"CREATE TABLE "+harness.MigrationTable+" (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
	"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
	"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
	"DELETE FROM "+harness.MigrationTable+";",
)

const legacyHistoryDDL = `CREATE TABLE sgl_migrations (
    version        BIGINT      NOT NULL PRIMARY KEY,
    name           VARCHAR     NOT NULL,
    checksum       VARCHAR     NOT NULL,
    applied_at     TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    duration_ms    BIGINT      NOT NULL,
    down_checksum  VARCHAR,
    last_statement INTEGER,
    dirty          BOOLEAN     NOT NULL DEFAULT FALSE
)`

func TestDuckDBMigratorAndSeederLifecycle(t *testing.T) {
	database := openMigrationDatabase(t)

	requireReadingRunsNoDDL(t, database, migrate_backend_duckdb.New())
	harness.MigratorLifecycle(t, database, migrate_backend_duckdb.New(), widgetMigrations, harness.SettleImmediately)

	require.NotContains(t, tableNames(t, database), harness.MigrationTable)
}

func TestDuckDBNamespacedLifecycleKeepsToItsOwnTables(t *testing.T) {
	database := openMigrationDatabase(t)
	backend := migrate_backend_duckdb.New(migrate.WithNamespace("acme"), migrate.WithHistoryTable("acme_history"))

	requireReadingRunsNoDDL(t, database, backend)
	harness.MigratorLifecycle(t, database, backend, widgetMigrations, harness.SettleImmediately)

	require.Equal(t, []string{"acme_history", "acme_seeds"}, tableNames(t, database))
}

func TestDuckDBMigrationRollsBackAsAUnit(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_create_widgets.up.sql": {Data: []byte(
			"CREATE TABLE " + harness.MigrationTable + " (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n" +
				"INSERT INTO " + missingTable + " (label) VALUES ('first');\n",
		)},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_duckdb.New(), files, "migrations")
	require.NoError(t, err)

	applied, err := migrator.Up(ctx)
	var failure *migrate.MigrationExecutionError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, int64(1), failure.Version)
	require.Zero(t, applied)

	require.NotContains(t, tableNames(t, database), harness.MigrationTable, "the table the failed migration created must roll back with it")
	statuses, err := migrator.Status(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	require.False(t, statuses[0].Applied)
	require.False(t, statuses[0].Dirty, "a transactional migration leaves no dirty record behind")
}

func TestDuckDBNoTransactionMigrationResumesWhereItStopped(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_create_widgets.up.sql": {Data: []byte(
			"-- sgl.migration(no_transaction: true)\n" +
				"CREATE TABLE " + harness.MigrationTable + " (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n" +
				"INSERT INTO " + missingTable + " (label) VALUES ('first');\n" +
				"INSERT INTO " + harness.MigrationTable + " (id, label) VALUES (1, 'first');\n",
		)},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_duckdb.New(), files, "migrations")
	require.NoError(t, err)

	_, err = migrator.Up(ctx)
	require.Error(t, err)
	statuses, err := migrator.Status(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	require.True(t, statuses[0].Dirty, "the interrupted migration must be recorded as dirty")
	require.NotNil(t, statuses[0].LastStatement)
	require.Equal(t, 0, *statuses[0].LastStatement, "only the CREATE TABLE completed")

	_, err = database.ExecContext(ctx, "CREATE TABLE "+missingTable+" (label VARCHAR(64) NOT NULL)")
	require.NoError(t, err)

	applied, err := migrator.Up(ctx)
	require.NoError(t, err, "the retry must skip the CREATE TABLE that already ran")
	require.Equal(t, 1, applied)
	statuses, err = migrator.Status(ctx)
	require.NoError(t, err)
	require.True(t, statuses[0].Applied)
	require.False(t, statuses[0].Dirty)
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT COUNT(*) FROM "+harness.MigrationTable))
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT COUNT(*) FROM "+missingTable))
}

func TestDuckDBConcurrentUpOnOneMigrator(t *testing.T) {
	database := openMigrationDatabase(t)
	migrator, err := migrate.NewMigrator(database, migrate_backend_duckdb.New(), widgetMigrations, "migrations")
	require.NoError(t, err)

	const callers = 4
	results := harness.RaceUp(t, slices.Repeat([]harness.Boot{migrator.Up}, callers))

	require.Equal(t, 2, harness.TotalApplied(t, results, ""), "the callers between them must apply each migration once")
	require.Equal(t, "1,2", harness.AppliedVersions(t, migrator))
}

func TestDuckDBRacingMigratorsApplyEachMigrationOnce(t *testing.T) {
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_create_counter.up.sql": {Data: []byte("CREATE TABLE " + counterTable + " (n INTEGER);\n")},
		"migrations/0002_count_once.up.sql":     {Data: []byte("INSERT INTO " + counterTable + " (n) VALUES (1);\n")},
	}

	const replicas = 4
	raced := 0
	for _, result := range harness.RaceUp(t, harness.Replicas(t, database, migrate_backend_duckdb.New(), files, replicas)) {
		raced += result.Applied
	}
	require.LessOrEqual(t, raced, 2, "a losing replica may fail, but none applies a migration another applied")

	settled, err := migrate.NewMigrator(database, migrate_backend_duckdb.New(), files, "migrations")
	require.NoError(t, err)
	_, err = settled.Up(t.Context())
	require.NoError(t, err, "a run after the race must finish whatever the losers left")
	require.Equal(t, "1,2", harness.AppliedVersions(t, settled))
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT COUNT(*) FROM "+counterTable),
		"a losing migrator's transaction must roll back, not apply the migration twice")
}

func openMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "migrate.duckdb"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func requireReadingRunsNoDDL(t *testing.T, database *sql.DB, backend migrate.Backend) {
	t.Helper()

	migrator, err := migrate.NewMigrator(database, backend, widgetMigrations, "migrations")
	require.NoError(t, err)
	fresh, err := migrator.Status(t.Context())
	require.NoError(t, err, "status must read a database that has no history table")
	require.Len(t, fresh, 2)
	require.NoError(t, migrator.Validate(t.Context()))
	require.Empty(t, tableNames(t, database), "status and validate must run no DDL")
}

func tableNames(t *testing.T, database *sql.DB) []string {
	t.Helper()

	rows, err := database.QueryContext(t.Context(),
		"SELECT table_name FROM information_schema.tables WHERE table_catalog = current_database() AND table_schema = current_schema() ORDER BY table_name")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	names := []string{}
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	return names
}

func TestDuckDBLegacyDirtyMigrationIsUpgradedAndNotResumed(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	content := []byte("CREATE TABLE boats (id INTEGER PRIMARY KEY);\nCREATE TABLE harbours (id INTEGER PRIMARY KEY);\n")
	files := fstest.MapFS{"migrations/0001_boats.up.sql": {Data: content}}
	for _, statement := range []string{
		legacyHistoryDDL,
		"INSERT INTO sgl_migrations (version, name, checksum, duration_ms, last_statement, dirty) VALUES (1, 'boats', '" + harness.ChecksumOf(content) + "', 0, 0, TRUE)",
		"CREATE TABLE boats (id INTEGER PRIMARY KEY)",
	} {
		_, err := database.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_duckdb.New(), files, "migrations")
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
