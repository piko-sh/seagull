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

package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"testing/fstest"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_postgres"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const legacyHistoryDDL = `CREATE TABLE sgl_migrations (
    version        BIGINT      NOT NULL PRIMARY KEY,
    name           TEXT        NOT NULL,
    checksum       TEXT        NOT NULL,
    applied_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms    BIGINT      NOT NULL,
    down_checksum  TEXT,
    last_statement INTEGER,
    dirty          BOOLEAN     NOT NULL DEFAULT FALSE
)`

func TestPostgresMigratorAndSeederLifecycle(t *testing.T) {
	database := openMigrationDatabase(t)
	files := harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"DELETE FROM "+harness.MigrationTable+";",
	)
	harness.MigratorLifecycle(t, database, migrate_backend_postgres.New(), files, harness.SettleImmediately)
}

func TestPostgresMigratorAndSeederLifecycleWithTableLock(t *testing.T) {
	database := openMigrationDatabase(t)
	files := harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"DELETE FROM "+harness.MigrationTable+";",
	)
	harness.MigratorLifecycle(t, database, migrate_backend_postgres.NewPgBouncer(), files, harness.SettleImmediately)
}

func TestPostgresTableLockHeldAcrossMigrations(t *testing.T) {
	database := openMigrationDatabase(t)
	files := harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"DELETE FROM "+harness.MigrationTable+";",
	)
	backend := migrate_backend_postgres.NewPgBouncer()
	probed := map[int64]error{}
	probe := func(ctx context.Context, hook migrate.MigrationHookContext) error {
		probed[hook.Version] = tryLockRow(ctx, database, backend.Names().MigrationLock)
		return nil
	}
	migrator, err := migrate.NewMigrator(database, backend, files, "migrations", migrate.WithBeforeMigration(probe))
	require.NoError(t, err)

	applied, err := migrator.Up(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, applied)
	require.Len(t, probed, 2)
	for version, probeErr := range probed {
		require.Errorf(t, probeErr, "the lock row was free before migration %d, so the lock was released early", version)
		assert.Containsf(t, probeErr.Error(), "55P03", "migration %d probe failed for another reason", version)
	}
	require.NoError(t, tryLockRow(t.Context(), database, backend.Names().MigrationLock), "the lock row stays locked after Up returns")
}

func tryLockRow(ctx context.Context, database *sql.DB, lockTable string) error {
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	_, err = transaction.ExecContext(ctx, "SELECT lock_id FROM "+lockTable+" FOR UPDATE NOWAIT")
	return err
}

func TestPostgresMigrationsThatSetTheSearchPath(t *testing.T) {
	for name, backend := range map[string]migrate.Backend{
		"advisory lock": migrate_backend_postgres.New(),
		"table lock":    migrate_backend_postgres.NewPgBouncer(),
		"no lock":       migrate_backend_postgres.New(migrate.WithLock(migrate.LockNone)),
	} {
		t.Run(name, func(t *testing.T) {
			harness.SearchPathLifecycle(t, openMigrationDatabase(t), backend)
		})
	}
}

func TestPostgresConcurrentFirstBoot(t *testing.T) {
	for name, backend := range map[string]migrate.Backend{
		"advisory lock": migrate_backend_postgres.New(),
		"table lock":    migrate_backend_postgres.NewPgBouncer(),
	} {
		t.Run(name, func(t *testing.T) {
			harness.ConcurrentFirstBoot(t, openMigrationDatabase(t), backend, harness.MigrationFiles(
				"CREATE TABLE "+harness.MigrationTable+" (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
				"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
				"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
				"DELETE FROM "+harness.MigrationTable+";",
			))
		})
	}
}

func openMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database, _ := harness.IsolatedSchema(t, "pgx", testConnectionString)
	return database
}

func TestPostgresLegacyDirtyMigrationIsUpgradedAndNotResumed(t *testing.T) {
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
	migrator, err := migrate.NewMigrator(database, migrate_backend_postgres.New(), files, "migrations")
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

func TestPostgresMigrationSplitsLikeTheDialect(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_semicolons.up.sql": {Data: []byte(`CREATE TABLE "semi;colon" (id int, begin timestamptz);
/* outer /* inner; */ still; */
INSERT INTO "semi;colon" (id) VALUES (1);
ALTER TABLE "semi;colon" RENAME COLUMN begin TO starts_at;
CREATE TABLE notes (body text);
INSERT INTO notes (body) VALUES (E'it\'s; fine');
CREATE FUNCTION one() RETURNS int LANGUAGE sql AS $body$ SELECT 1; $body$;
CREATE INDEX CONCURRENTLY semi_idx ON "semi;colon" (id);
`)},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_postgres.New(), files, "migrations")
	require.NoError(t, err)

	applied, err := migrator.Up(ctx)

	require.NoError(t, err, "the index built concurrently must run outside a transaction without a directive")
	require.Equal(t, 1, applied)
	require.Equal(t, 1, harness.CountOf(t, database, `SELECT count(*) FROM "semi;colon" WHERE starts_at IS NULL`))
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT count(*) FROM notes WHERE body = 'it''s; fine'"))
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT one()"))
	require.Equal(t, []harness.HistoryEntry{{Version: 1, Dirty: false, LastStatement: 6}}, harness.ReadHistory(t, database))
}
