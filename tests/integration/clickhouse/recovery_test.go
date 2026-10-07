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
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_clickhouse"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

func TestClickHouseForceAndRepairReplaceTheHistoryRow(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_boats.up.sql":    {Data: []byte("CREATE TABLE boats (id Int64) ENGINE = MergeTree ORDER BY id;\n")},
		"migrations/0002_harbours.up.sql": {Data: []byte("CREATE TABLE harbours (id Int64) ENGINE = MergeTree ORDER BY id;\n")},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_clickhouse.New(), files, "migrations")
	require.NoError(t, err)

	require.NoError(t, migrator.Force(ctx, 1))
	var boats int
	require.NoError(t, database.QueryRowContext(ctx, "SELECT count() FROM system.tables WHERE database = currentDatabase() AND name = 'boats'").Scan(&boats))
	require.Zero(t, boats, "force runs no SQL")
	applied, err := migrator.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, applied, "only the migration after the forced one runs")

	reviewed := []byte("CREATE TABLE boats (id Int64) ENGINE = MergeTree ORDER BY id; -- reviewed\n")
	files["migrations/0001_boats.up.sql"] = &fstest.MapFile{Data: reviewed}
	var changed *migrate.ChecksumMismatchError
	require.ErrorAs(t, migrator.Validate(ctx), &changed)
	plan, err := migrator.PlanRepair(ctx)
	require.NoError(t, err)
	require.Len(t, plan.Changed, 1)
	_, err = migrator.Repair(ctx, migrate.RepairOptions{Plan: plan, Completed: nil})
	require.NoError(t, err)
	require.NoError(t, migrator.Validate(ctx), "the replaced row is visible as soon as Repair returns")

	var rows int
	require.NoError(t, database.QueryRowContext(ctx, "SELECT count() FROM sgl_migrations FINAL WHERE version = 1").Scan(&rows))
	require.Equal(t, 1, rows, "readers see only the replacement row, which supersedes the old one until it merges away")
	var checksum string
	require.NoError(t, database.QueryRowContext(ctx, "SELECT checksum FROM sgl_migrations FINAL WHERE version = 1").Scan(&checksum))
	require.Equal(t, harness.ChecksumOf(reviewed), checksum, "the visible row carries the reviewed migration's checksum")
	require.Equal(t, "1,2", harness.AppliedVersions(t, migrator))
}
