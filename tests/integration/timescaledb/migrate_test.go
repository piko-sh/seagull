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

package timescaledb_test

import (
	"database/sql"
	"testing"
	"testing/fstest"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_postgres"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_timescaledb"
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

func TestTimescaleDBMigratorAndSeederLifecycle(t *testing.T) {
	database := openMigrationDatabase(t)
	files := harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"DELETE FROM "+harness.MigrationTable+";",
	)
	harness.MigratorLifecycle(t, database, migrate_backend_postgres.New(), files, harness.SettleImmediately)
}

func TestTimescaleDBConcurrentFirstBoot(t *testing.T) {
	harness.ConcurrentFirstBoot(t, openMigrationDatabase(t), migrate_backend_postgres.New(), harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"DELETE FROM "+harness.MigrationTable+";",
	))
}

func TestTimescaleDBMigrationsThatSetTheSearchPath(t *testing.T) {
	harness.SearchPathLifecycle(t, openMigrationDatabase(t), migrate_backend_timescaledb.New())
}

func openMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database, _ := harness.IsolatedSchema(t, "pgx", testConnectionString)
	return database
}

func TestTimescaleDBLegacyDirtyMigrationIsUpgradedAndNotResumed(t *testing.T) {
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
	migrator, err := migrate.NewMigrator(database, migrate_backend_timescaledb.New(), files, "migrations")
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

func TestTimescaleDBRunsAContinuousAggregateOutsideATransaction(t *testing.T) {
	testCases := []struct {
		name        string
		backend     migrate.Backend
		wantApplied int
		wantErr     string
	}{
		{name: "the TimescaleDB backend recognises the statements", backend: migrate_backend_timescaledb.New(), wantApplied: 3},
		{name: "the PostgreSQL backend runs them in a transaction", backend: migrate_backend_postgres.New(), wantApplied: 1, wantErr: "SQLSTATE 25001"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := t.Context()
			database := openMigrationDatabase(t)
			files := fstest.MapFS{
				"migrations/0001_readings.up.sql": {Data: []byte("CREATE TABLE readings (ts timestamptz NOT NULL, buoy text NOT NULL, temperature double precision NOT NULL);\n" +
					"SELECT public.create_hypertable('readings', 'ts', chunk_time_interval => INTERVAL '1 day');\n" +
					"INSERT INTO readings VALUES ('2026-01-01 00:10:00+00', 'north', 9.5), ('2026-01-01 00:40:00+00', 'north', 10.5);\n")},
				"migrations/0002_hourly.up.sql": {Data: []byte("CREATE MATERIALIZED VIEW hourly_temps\n" +
					"WITH (timescaledb.continuous, timescaledb.materialized_only = true) AS\n" +
					"SELECT public.time_bucket(INTERVAL '1 hour', ts) AS bucket, buoy, avg(temperature) AS avg_temp\n" +
					"FROM readings\n" +
					"GROUP BY bucket, buoy;\n")},
				"migrations/0003_refresh.up.sql": {Data: []byte("INSERT INTO readings VALUES ('2026-01-01 05:15:00+00', 'south', 11.0);\n" +
					"CALL public.refresh_continuous_aggregate('hourly_temps', NULL, NULL);\n")},
			}
			migrator, err := migrate.NewMigrator(database, testCase.backend, files, "migrations")
			require.NoError(t, err)

			applied, err := migrator.Up(ctx)

			require.Equal(t, testCase.wantApplied, applied)
			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				return
			}
			require.NoError(t, err, "a continuous aggregate and its refresh must run outside a transaction without a directive")
			var buckets int
			require.NoError(t, database.QueryRowContext(ctx, "SELECT count(*) FROM hourly_temps").Scan(&buckets))
			require.Equal(t, 2, buckets, "the aggregate holds the bucket materialised on creation and the one the refresh added")
		})
	}
}
