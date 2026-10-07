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

package cockroachdb_test

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"testing"
	"testing/fstest"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_cockroachdb"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const (
	concurrentBootIterations = 5
	bootReplicas             = 3
	harboursTableQuery       = "SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'harbours'"
	boatsNameIndexQuery      = "SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'boats_name_key'"
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

func TestCockroachDBMigratorAndSeederLifecycle(t *testing.T) {
	database := openMigrationDatabase(t)
	files := harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"DELETE FROM "+harness.MigrationTable+";",
	)
	harness.MigratorLifecycle(t, database, migrate_backend_cockroachdb.New(), files, harness.SettleImmediately)
}

func TestCockroachDBConcurrentFirstBoot(t *testing.T) {
	harness.ConcurrentFirstBoot(t, openMigrationDatabase(t), migrate_backend_cockroachdb.New(), harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"DELETE FROM "+harness.MigrationTable+";",
	))
}

func TestCockroachDBMigrationsThatSetTheSearchPath(t *testing.T) {
	for name, backend := range map[string]migrate.Backend{
		"table lock": migrate_backend_cockroachdb.New(),
		"no lock":    migrate_backend_cockroachdb.New(migrate.WithLock(migrate.LockNone)),
	} {
		t.Run(name, func(t *testing.T) {
			harness.SearchPathLifecycle(t, openMigrationDatabase(t), backend)
		})
	}
}

func openMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database, _ := harness.IsolatedSchema(t, "pgx", testConnectionString)
	return database
}

func TestCockroachDBConcurrentBootWithABackfillingSchemaChange(t *testing.T) {
	files := fstest.MapFS{
		"migrations/0001_boats.up.sql":     {Data: []byte("CREATE TABLE boats (id INT8 NOT NULL DEFAULT unique_rowid() PRIMARY KEY, name STRING NOT NULL);\n")},
		"migrations/0002_home_port.up.sql": {Data: []byte("ALTER TABLE boats ADD COLUMN home_port STRING NOT NULL DEFAULT 'Padstow';\n")},
	}
	for iteration := range concurrentBootIterations {
		t.Run("iteration "+strconv.Itoa(iteration+1), func(t *testing.T) {
			database := openMigrationDatabase(t)

			total := upConcurrently(t, database, files, "SQLSTATE 42701 means a second replica took the lock mid-migration")

			require.Equal(t, 2, total, "the replicas between them must apply each migration once")
			require.Equal(t, []harness.HistoryEntry{
				{Version: 1, Dirty: false, LastStatement: 0},
				{Version: 2, Dirty: false, LastStatement: 0},
			}, harness.ReadHistory(t, database))
			require.Equal(t, 1, harness.CountOf(t, database,
				"SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'boats' AND column_name = 'home_port'"))
		})
	}
}

func TestCockroachDBTableLockSurvivesBackfillingSchemaChanges(t *testing.T) {
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_boats.up.sql":      {Data: []byte("CREATE TABLE boats (id INT8 PRIMARY KEY, name STRING NOT NULL);\nINSERT INTO boats (id, name) VALUES (1, 'Gull'), (2, 'Tern');\n")},
		"migrations/0002_home_port.up.sql":  {Data: []byte("ALTER TABLE boats ADD COLUMN home_port STRING NOT NULL DEFAULT 'Padstow';\n")},
		"migrations/0003_boat_names.up.sql": {Data: []byte("CREATE INDEX boats_name_idx ON boats (name);\n")},
	}
	backend := migrate_backend_cockroachdb.New()
	contender, err := migrate.NewMigrator(database, backend, files, "migrations", migrate.WithNonBlockingLock())
	require.NoError(t, err)
	contenderProbes := map[int64]error{}
	probeContender := func(ctx context.Context, hook migrate.MigrationHookContext) error {
		_, contenderProbes[hook.Version] = contender.Up(ctx)
		return nil
	}
	rowProbes := map[int64]error{}
	probeRow := func(ctx context.Context, hook migrate.MigrationHookContext) error {
		rowProbes[hook.Version] = tryLockRow(ctx, database, backend.Names().MigrationLock)
		return nil
	}
	migrator, err := migrate.NewMigrator(database, backend, files, "migrations",
		migrate.WithBeforeMigration(probeContender), migrate.WithAfterMigration(probeRow))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeoutCause(t.Context(), harness.MigrationDeadline,
		errors.New("the migrations did not finish within the deadline, so a lock likely blocked a schema change"))
	defer cancel()

	applied, err := migrator.Up(ctx)

	require.NoError(t, err)
	require.Equal(t, 3, applied)
	require.Len(t, rowProbes, 3)
	require.Len(t, contenderProbes, 3)
	for version, probeErr := range rowProbes {
		require.Errorf(t, probeErr, "the lock row was free after migration %d, so a schema change dropped the lock", version)
		assert.Containsf(t, probeErr.Error(), "55P03", "migration %d probe failed for another reason", version)
		require.ErrorIsf(t, contenderProbes[version], migrate.ErrLockNotAcquired, "a non-blocking replica got past the lock before migration %d", version)
	}
	require.NoError(t, tryLockRow(t.Context(), database, backend.Names().MigrationLock), "the lock row stays locked after Up returns")
}

func TestCockroachDBFailedMigrationStaysDirtyAndResumes(t *testing.T) {
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_boats.up.sql": {Data: []byte("CREATE TABLE boats (id INT8 PRIMARY KEY, name STRING NOT NULL);\n" +
			"INSERT INTO boats (id, name) VALUES (1, 'Gull'), (2, 'Gull');\n")},
		"migrations/0002_harbours.up.sql": {Data: []byte("CREATE TABLE harbours (id INT8 PRIMARY KEY, name STRING NOT NULL);\n" +
			"CREATE UNIQUE INDEX boats_name_key ON boats (name);\n" +
			"INSERT INTO harbours (id, name) VALUES (1, 'Padstow');\n")},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_cockroachdb.New(), files, "migrations")
	require.NoError(t, err)

	applied, err := migrator.Up(t.Context())

	var executionError *migrate.MigrationExecutionError
	require.ErrorAs(t, err, &executionError)
	assert.Equal(t, int64(2), executionError.Version)
	assert.ErrorContains(t, err, "statement 2/3")
	assert.ErrorContains(t, err, "23505")
	require.Equal(t, 1, applied)
	require.Equal(t, 1, harness.CountOf(t, database, harboursTableQuery), "statement 1 committed on its own")
	require.Zero(t, harness.CountOf(t, database, "SELECT count(*) FROM harbours"), "statement 3 ran after statement 2 failed")
	require.Zero(t, harness.CountOf(t, database, boatsNameIndexQuery), "the failed unique index exists")
	require.Equal(t, []harness.HistoryEntry{
		{Version: 1, Dirty: false, LastStatement: 1},
		{Version: 2, Dirty: true, LastStatement: 0},
	}, harness.ReadHistory(t, database))
	statuses, err := migrator.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, statuses, 2)
	assert.True(t, statuses[1].Dirty)
	require.NotNil(t, statuses[1].LastStatement)
	assert.Equal(t, 0, *statuses[1].LastStatement)
	require.NoError(t, migrator.Validate(t.Context()))

	_, err = database.ExecContext(t.Context(), "UPDATE boats SET name = 'Tern' WHERE id = 2")
	require.NoError(t, err)
	resumed, err := migrator.Up(t.Context())

	require.NoError(t, err, "the rerun must resume after statement 1 rather than create harbours again")
	require.Equal(t, 1, resumed)
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT count(*) FROM harbours WHERE name = 'Padstow'"))
	require.Equal(t, 1, harness.CountOf(t, database, boatsNameIndexQuery))
	require.Equal(t, []harness.HistoryEntry{
		{Version: 1, Dirty: false, LastStatement: 1},
		{Version: 2, Dirty: false, LastStatement: 2},
	}, harness.ReadHistory(t, database))
}

func TestCockroachDBLockLetsMigrationsChangeTheSchemaAndDatabase(t *testing.T) {
	database := openMigrationDatabase(t)
	var schema string
	require.NoError(t, database.QueryRowContext(t.Context(), "SELECT current_schema()").Scan(&schema))
	harbour := harness.UniqueName(t)
	t.Cleanup(func() {
		ctx, cancel := harness.CleanupContext(t)
		defer cancel()
		administer(ctx, "DROP SCHEMA IF EXISTS "+harbour+" CASCADE", "ALTER DEFAULT PRIVILEGES REVOKE SELECT ON TABLES FROM public")
	})
	files := fstest.MapFS{
		"migrations/0001_one.up.sql":     {Data: []byte("CREATE FUNCTION one() RETURNS INT8 LANGUAGE SQL AS $$ SELECT 1 $$;\n")},
		"migrations/0002_harbour.up.sql": {Data: []byte("CREATE SCHEMA " + harbour + ";\n")},
		"migrations/0003_grants.up.sql": {Data: []byte("GRANT USAGE ON SCHEMA " + schema + " TO public;\n" +
			"ALTER DEFAULT PRIVILEGES IN SCHEMA " + schema + " GRANT SELECT ON TABLES TO public;\n" +
			"ALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO public;\n")},
	}

	total := upConcurrently(t, database, files, "a deadline here means a lock transaction's descriptor lease blocked the schema change")

	require.Equal(t, 3, total)
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT one()"))
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT count(*) FROM information_schema.schemata WHERE schema_name = '"+harbour+"'"))
}

func upConcurrently(t *testing.T, database *sql.DB, files fstest.MapFS, failureHint string) int {
	t.Helper()

	results := harness.RaceUp(t, harness.Replicas(t, database, migrate_backend_cockroachdb.New(), files, bootReplicas))
	return harness.TotalApplied(t, results, failureHint)
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

func administer(ctx context.Context, statements ...string) {
	administration, err := sql.Open("pgx", testConnectionString)
	if err != nil {
		return
	}
	defer func() { _ = administration.Close() }()
	for _, statement := range statements {
		_, _ = administration.ExecContext(ctx, statement)
	}
}

func TestCockroachDBLegacyDirtyMigrationIsUpgradedAndNotResumed(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	content := []byte("CREATE TABLE boats (id INT8 PRIMARY KEY);\nCREATE TABLE harbours (id INT8 PRIMARY KEY);\n")
	files := fstest.MapFS{"migrations/0001_boats.up.sql": {Data: content}}
	for _, statement := range []string{
		legacyHistoryDDL,
		"INSERT INTO sgl_migrations (version, name, checksum, duration_ms, last_statement, dirty) VALUES (1, 'boats', '" + harness.ChecksumOf(content) + "', 0, 0, TRUE)",
		"CREATE TABLE boats (id INT8 PRIMARY KEY)",
	} {
		_, err := database.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_cockroachdb.New(), files, "migrations")
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

func TestCockroachDBSeedsCommitWithTheirRecord(t *testing.T) {
	testCases := []struct {
		name         string
		seed         string
		wantRows     int
		createsTable bool
		wantError    bool
	}{
		{name: "a seed of data", seed: "INSERT INTO gulls (id) VALUES (1);\nINSERT INTO gulls (id) VALUES (2);\n", wantRows: 2},
		{name: "a seed that also changes the schema", seed: "CREATE TABLE gulls (id INT8 PRIMARY KEY);\nINSERT INTO gulls (id) VALUES (1), (2);\n", wantRows: 2, createsTable: true},
		{name: "a failing seed records nothing", seed: "INSERT INTO gulls (id) VALUES (1);\nINSERT INTO gulls (id) VALUES (1);\n", wantError: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := t.Context()
			database := openMigrationDatabase(t)
			if !testCase.createsTable {
				_, err := database.ExecContext(ctx, "CREATE TABLE gulls (id INT8 PRIMARY KEY)")
				require.NoError(t, err)
			}
			seeder, err := migrate.NewSeeder(database, migrate_backend_cockroachdb.New(), fstest.MapFS{"seeds/001_gulls.sql": {Data: []byte(testCase.seed)}}, "seeds")
			require.NoError(t, err)

			applied, err := seeder.Apply(ctx)

			statuses, statusErr := seeder.Status(ctx)
			require.NoError(t, statusErr)
			if testCase.wantError {
				require.Error(t, err)
				require.False(t, statuses[0].Applied, "a seed that failed must not be recorded")
				require.Zero(t, harness.CountOf(t, database, "SELECT count(*) FROM gulls"), "the transaction must roll the seed's data back")
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, applied)
			require.True(t, statuses[0].Applied)
			require.Equal(t, testCase.wantRows, harness.CountOf(t, database, "SELECT count(*) FROM gulls"))
		})
	}
}
