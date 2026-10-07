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

package mysql_test

import (
	"database/sql"
	"strings"
	"testing"
	"testing/fstest"

	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_mysql"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const legacyHistoryDDL = `CREATE TABLE sgl_migrations (
    version        BIGINT       NOT NULL PRIMARY KEY,
    name           VARCHAR(255) NOT NULL,
    checksum       VARCHAR(64)  NOT NULL,
    applied_at     TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    duration_ms    BIGINT       NOT NULL,
    down_checksum  VARCHAR(64),
    last_statement INTEGER,
    dirty          BOOLEAN      NOT NULL DEFAULT FALSE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

func TestMySQLMigratorAndSeederLifecycle(t *testing.T) {
	database := openMigrationDatabase(t)
	files := harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id INT PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"DELETE FROM "+harness.MigrationTable+";",
	)
	harness.MigratorLifecycle(t, database, migrate_backend_mysql.New(), files, harness.SettleImmediately)
}

func TestMySQLMigrationsThatUseAnotherDatabase(t *testing.T) {
	for name, backend := range map[string]migrate.Backend{
		"advisory lock": migrate_backend_mysql.New(),
		"no lock":       migrate_backend_mysql.New(migrate.WithLock(migrate.LockNone)),
	} {
		t.Run(name, func(t *testing.T) {
			database := openMigrationDatabase(t)
			harness.DatabaseSwitchLifecycle(t, database, backend)
		})
	}
}

func TestMySQLConcurrentFirstBoot(t *testing.T) {
	database := openMigrationDatabase(t)
	harness.ConcurrentFirstBoot(t, database, migrate_backend_mysql.New(), harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id INT PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"DELETE FROM "+harness.MigrationTable+";",
	))
}

func TestMySQLLockKeysAreScopedToTheSchema(t *testing.T) {
	ctx := t.Context()
	first := openMigrationDatabase(t)
	second := openMigrationDatabase(t)

	tests := []struct {
		name        string
		options     []migrate.BackendOption
		wantBlocked bool
	}{
		{name: "scoped by default", options: nil, wantBlocked: false},
		{name: "long scoped names are hashed", options: []migrate.BackendOption{migrate.WithNamespace(strings.Repeat("n", 37))}, wantBlocked: false},
		{name: "server-wide on request", options: []migrate.BackendOption{migrate_backend_mysql.WithServerWideLockKey()}, wantBlocked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			holder, err := migrate.NewExecutor(first, migrate_backend_mysql.New(tt.options...))
			require.NoError(t, err)
			other, err := migrate.NewExecutor(second, migrate_backend_mysql.New(tt.options...))
			require.NoError(t, err)

			require.NoError(t, holder.AcquireLock(ctx))
			otherErr := other.TryAcquireLock(ctx)
			if tt.wantBlocked {
				require.ErrorIs(t, otherErr, migrate.ErrLockNotAcquired)
			} else {
				require.NoError(t, otherErr, "a lock on one schema must not block another schema")
				require.NoError(t, other.ReleaseLock(ctx))
			}
			require.NoError(t, holder.ReleaseLock(ctx))
		})
	}
}

func openMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database, _ := harness.IsolatedDatabase(t, "mysql", testConnectionString)
	return database
}

func TestMySQLFailedMigrationStaysDirtyAndResumes(t *testing.T) {
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_boats.up.sql": {Data: []byte("CREATE TABLE boats (id INT PRIMARY KEY, name VARCHAR(64) NOT NULL);\n" +
			"INSERT INTO boats (id, name) VALUES (1, 'Gull'), (2, 'Gull');\n")},
		"migrations/0002_harbours.up.sql": {Data: []byte("CREATE TABLE harbours (id INT PRIMARY KEY, name VARCHAR(64) NOT NULL);\n" +
			"CREATE UNIQUE INDEX boats_name_key ON boats (name);\n" +
			"INSERT INTO harbours (id, name) VALUES (1, 'Padstow');\n")},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_mysql.New(), files, "migrations")
	require.NoError(t, err)

	applied, err := migrator.Up(t.Context())

	var executionError *migrate.MigrationExecutionError
	require.ErrorAs(t, err, &executionError)
	assert.Equal(t, int64(2), executionError.Version)
	assert.ErrorContains(t, err, "statement 2/3")
	require.Equal(t, 1, applied)
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT count(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'harbours'"), "statement 1 committed implicitly")
	require.Zero(t, harness.CountOf(t, database, "SELECT count(*) FROM harbours"), "statement 3 ran after statement 2 failed")
	require.Equal(t, []harness.HistoryEntry{
		{Version: 1, Dirty: false, LastStatement: 1},
		{Version: 2, Dirty: true, LastStatement: 0},
	}, harness.ReadHistory(t, database))
	statuses, err := migrator.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, statuses, 2)
	assert.True(t, statuses[1].Dirty)

	_, err = database.ExecContext(t.Context(), "UPDATE boats SET name = 'Tern' WHERE id = 2")
	require.NoError(t, err)
	resumed, err := migrator.Up(t.Context())

	require.NoError(t, err, "the rerun must resume after statement 1 rather than create harbours again")
	require.Equal(t, 1, resumed)
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT count(*) FROM harbours WHERE name = 'Padstow'"))
	require.Equal(t, []harness.HistoryEntry{
		{Version: 1, Dirty: false, LastStatement: 1},
		{Version: 2, Dirty: false, LastStatement: 2},
	}, harness.ReadHistory(t, database))
}

func TestMySQLRunsDelimiterMigrations(t *testing.T) {
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_double.up.sql": {Data: []byte("DELIMITER //\n" +
			"CREATE FUNCTION double_it(x INT) RETURNS INT DETERMINISTIC\n" +
			"BEGIN\n" +
			"    RETURN x * 2;\n" +
			"END //\n" +
			"DELIMITER ;\n" +
			"CREATE TABLE `notes;archive` (id INT PRIMARY KEY, note VARCHAR(64) NOT NULL); # it's a note\n" +
			"INSERT INTO `notes;archive` (id, note) VALUES (1, \"a; b\");\n")},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_mysql.New(), files, "migrations")
	require.NoError(t, err)

	applied, err := migrator.Up(t.Context())

	require.NoError(t, err)
	require.Equal(t, 1, applied)
	require.Equal(t, 42, harness.CountOf(t, database, "SELECT double_it(21)"))
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT count(*) FROM `notes;archive` WHERE note = 'a; b'"))
}

func TestMySQLLegacyDirtyMigrationIsUpgradedAndNotResumed(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	content := []byte("CREATE TABLE boats (id INT PRIMARY KEY);\nCREATE TABLE harbours (id INT PRIMARY KEY);\n")
	files := fstest.MapFS{"migrations/0001_boats.up.sql": {Data: content}}
	for _, statement := range []string{
		legacyHistoryDDL,
		"INSERT INTO sgl_migrations (version, name, checksum, duration_ms, last_statement, dirty) VALUES (1, 'boats', '" + harness.ChecksumOf(content) + "', 0, 0, TRUE)",
		"CREATE TABLE boats (id INT PRIMARY KEY)",
	} {
		_, err := database.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_mysql.New(), files, "migrations")
	require.NoError(t, err)

	statuses, err := migrator.Status(ctx)
	require.NoError(t, err, "status must read a history table an earlier release created")
	require.True(t, statuses[0].Dirty)

	_, err = migrator.Up(ctx)

	var mismatch *migrate.ResumeMismatchError
	require.ErrorAs(t, err, &mismatch)
	assert.Equal(t, 0, mismatch.LastStatement)
	assert.Zero(t, mismatch.RecordedSplitterVersion)
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
	assert.Equal(t, 1, recordedVersion)
	assert.Len(t, statementsHash, 64)
}
