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
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_cockroachdb"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

func TestCockroachDBRepairResumesAFixedDirtyMigration(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_harbours.up.sql": {Data: []byte("CREATE TABLE harbours (id INT8 PRIMARY KEY, name STRING NOT NULL);\n" +
			"INSERT INTO lighthouses (id) VALUES (1);\n" +
			"INSERT INTO harbours (id, name) VALUES (1, 'Padstow');\n")},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_cockroachdb.New(), files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(ctx)
	require.ErrorContains(t, err, "statement 2/3")
	require.Equal(t, []harness.HistoryEntry{{Version: 1, Dirty: true, LastStatement: 0}}, harness.ReadHistory(t, database))

	files["migrations/0001_harbours.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE harbours (id INT8 PRIMARY KEY, name STRING NOT NULL);\n" +
		"INSERT INTO harbours (id, name) VALUES (2, 'Fowey');\n" +
		"INSERT INTO harbours (id, name) VALUES (1, 'Padstow');\n")}
	plan, err := migrator.PlanRepair(ctx)
	require.NoError(t, err)
	require.Len(t, plan.Dirty, 1)
	_, err = migrator.Repair(ctx, migrate.RepairOptions{Plan: plan, Completed: map[int64]int{1: 1}})
	require.NoError(t, err)

	applied, err := migrator.Up(ctx)
	require.NoError(t, err, "the repaired migration resumes with its fixed second statement")
	require.Equal(t, 1, applied)
	require.Equal(t, 2, harness.CountOf(t, database, "SELECT count(*) FROM harbours"))
	require.Equal(t, []harness.HistoryEntry{{Version: 1, Dirty: false, LastStatement: 2}}, harness.ReadHistory(t, database))
}

func TestCockroachDBForceAndLockTimeout(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_boats.up.sql":    {Data: []byte("CREATE TABLE boats (id INT8 PRIMARY KEY);\n")},
		"migrations/0002_harbours.up.sql": {Data: []byte("CREATE TABLE harbours (id INT8 PRIMARY KEY);\n")},
	}
	holder, err := migrate.NewExecutor(database, migrate_backend_cockroachdb.New())
	require.NoError(t, err)
	require.NoError(t, holder.AcquireLock(ctx))

	waiting, err := migrate.NewMigrator(database, migrate_backend_cockroachdb.New(), files, "migrations", migrate.WithNonBlockingLock())
	require.NoError(t, err)
	require.ErrorIs(t, waiting.Force(ctx, 1), migrate.ErrLockNotAcquired)
	bounded, err := migrate.NewMigrator(database, migrate_backend_cockroachdb.New(), files, "migrations", migrate.WithLockTimeout(500*time.Millisecond))
	require.NoError(t, err)
	_, err = bounded.Up(ctx)
	require.ErrorIs(t, err, migrate.ErrLockTimeout)
	require.NoError(t, holder.ReleaseLock(ctx))

	require.NoError(t, bounded.Force(ctx, 1))
	require.Zero(t, harness.CountOf(t, database, "SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'boats'"), "force runs no SQL")
	applied, err := bounded.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, applied, "only the migration after the forced one runs")
}
