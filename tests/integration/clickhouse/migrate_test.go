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

package clickhouse_test

import (
	"database/sql"
	"testing"
	"testing/fstest"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_clickhouse"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const legacyHistoryDDL = `CREATE TABLE sgl_migrations (
    version        Int64       NOT NULL,
    name           String      NOT NULL,
    checksum       String      NOT NULL,
    applied_at     DateTime64(6) NOT NULL DEFAULT now64(),
    duration_ms    Int64       NOT NULL,
    down_checksum  Nullable(String),
    last_statement Nullable(Int32),
    dirty          UInt8       NOT NULL DEFAULT 0
) ENGINE = ReplacingMergeTree(applied_at) ORDER BY version`

func TestClickHouseMigratorAndSeederLifecycle(t *testing.T) {
	database := openMigrationDatabase(t)
	files := harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id Int64, label String) ENGINE = MergeTree() ORDER BY id;\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour Nullable(String);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"TRUNCATE TABLE "+harness.MigrationTable+";",
	)
	harness.MigratorLifecycle(t, database, migrate_backend_clickhouse.New(), files, settleEventually)
}

func TestClickHouseRollbackIsVisibleImmediately(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id Int64, label String) ENGINE = MergeTree() ORDER BY id;\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour Nullable(String);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"TRUNCATE TABLE "+harness.MigrationTable+";",
	)
	migrator, err := migrate.NewMigrator(database, migrate_backend_clickhouse.New(), files, "migrations")
	require.NoError(t, err)

	applied, err := migrator.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, applied)

	rolledBack, err := migrator.Down(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 1, rolledBack)
	require.Equal(t, "1", harness.AppliedVersions(t, migrator), "a rollback must be visible as soon as Down returns")

	reapplied, err := migrator.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, reapplied)
	require.Equal(t, "1,2", harness.AppliedVersions(t, migrator))
}

func openMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()

	database, _ := harness.IsolatedDatabase(t, "clickhouse", testConnectionString)
	return database
}

func settleEventually(t *testing.T, check func() bool) {
	t.Helper()

	require.Eventually(t, check, 30*time.Second, 250*time.Millisecond)
}

func TestClickHouseLegacyHistoryIsUpgraded(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	first := []byte("CREATE TABLE `gulls;archive` (id Int64) ENGINE = MergeTree ORDER BY id; # it's a note\n")
	files := fstest.MapFS{
		"migrations/0001_gulls.up.sql":    {Data: first},
		"migrations/0002_harbours.up.sql": {Data: []byte("CREATE TABLE harbours (id Int64, name String) ENGINE = MergeTree ORDER BY id;\nINSERT INTO harbours VALUES (1, 'a; b');\n")},
	}
	for _, statement := range []string{
		legacyHistoryDDL,
		"INSERT INTO sgl_migrations (version, name, checksum, duration_ms, dirty) VALUES (1, 'gulls', '" + harness.ChecksumOf(first) + "', 0, 0)",
		"CREATE TABLE `gulls;archive` (id Int64) ENGINE = MergeTree ORDER BY id",
	} {
		_, err := database.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_clickhouse.New(), files, "migrations")
	require.NoError(t, err)

	statuses, err := migrator.Status(ctx)
	require.NoError(t, err, "status must read a history table an earlier release created")
	require.True(t, statuses[0].Applied)
	require.False(t, statuses[1].Applied)

	applied, err := migrator.Up(ctx)

	require.NoError(t, err)
	require.Equal(t, 1, applied)
	var added int
	require.NoError(t, database.QueryRowContext(ctx, "SELECT count() FROM system.columns WHERE database = currentDatabase() "+
		"AND table = 'sgl_migrations' AND name IN ('splitter_version', 'statements_hash')").Scan(&added))
	require.Equal(t, 2, added, "up must add the progress columns under the lock")
	var labelled int
	require.NoError(t, database.QueryRowContext(ctx, "SELECT count() FROM harbours WHERE name = 'a; b'").Scan(&labelled))
	require.Equal(t, 1, labelled)
	require.Equal(t, "1,2", harness.AppliedVersions(t, migrator))
}
