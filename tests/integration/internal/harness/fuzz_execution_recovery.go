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

package harness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/migrate"
)

const (
	// recoveryFirstValue fixes an independent recovery observation.
	recoveryFirstValue = 42

	// recoveryGateValue fixes an independent recovery observation.
	recoveryGateValue = 43

	// recoveryLastValue fixes an independent recovery observation.
	recoveryLastValue = 44

	// recoveryLastID fixes an independent recovery observation.
	recoveryLastID = 3

	// recoverySeedFirstID fixes an independent recovery observation.
	recoverySeedFirstID = 7

	// recoverySeedGateID fixes an independent recovery observation.
	recoverySeedGateID = 8

	// recoverySeedLastID fixes an independent recovery observation.
	recoverySeedLastID = 9

	// recoverySeedFirstValue fixes an independent recovery observation.
	recoverySeedFirstValue = 70

	// recoverySeedGateValue fixes an independent recovery observation.
	recoverySeedGateValue = 80

	// recoverySeedLastValue fixes an independent recovery observation.
	recoverySeedLastValue = 90

	// recoveryMigrationFile identifies the independently interrupted migration.
	recoveryMigrationFile = "migrations/0001_recovery.up.sql"

	// recoveryTable identifies the table written before interruption.
	recoveryTable = "fuzz_recovery"

	// recoveryGate identifies the deliberately absent migration dependency.
	recoveryGate = "fuzz_recovery_gate"

	// recoverySeedGate identifies the deliberately absent seed dependency.
	recoverySeedGate = "fuzz_recovery_seed_gate"
)

// recovery checks real statement failures before running the ordinary lifecycle.
//
// Takes t (*testing.T) which reports unsafe resume or corrupted persisted state.
func (state *migrationExecution) recovery(ctx context.Context, t *testing.T) {
	t.Helper()
	state.database.SetMaxOpenConns(2)
	prefix := ""
	if state.model.Unwrapped {
		prefix = "-- sgl.migration(no_transaction: true)\n"
	}
	content := []byte(prefix + recoveryDDL(state.model.Engine, recoveryTable) +
		"; INSERT INTO fuzz_recovery VALUES (1, 42); INSERT INTO fuzz_recovery_gate VALUES (2, 43); INSERT INTO fuzz_recovery VALUES (3, 44);")
	files := fstest.MapFS{recoveryMigrationFile: {Data: content}, "migrations/0001_recovery.down.sql": {Data: []byte("DROP TABLE fuzz_recovery;")}}
	backend := executionBackend(state.model.Engine, migrate.WithNamespace("sgl_fuzz_recovery"))
	migrator, err := migrate.NewMigrator(state.database, backend, files, "migrations")
	require.NoError(t, err)
	count, err := migrator.Up(ctx)
	require.Error(t, err, "migration did not fail at its absent dependency")
	require.Zero(t, count, "migration did not fail at its absent dependency")
	atomic := fuzzcase.ExecutionAtomicDDL(state.model.Engine) && !state.model.Unwrapped
	progress := state.model.Engine != engineClickHouse && !atomic
	plan := state.recoveryFailure(ctx, t, migrator, files, atomic, progress)
	fuzzExec(ctx, t, state.database, recoveryDDL(state.model.Engine, recoveryGate), nil)

	resumes := progress || state.model.Engine == engineClickHouse
	if !atomic && !resumes {
		fuzzExec(ctx, t, state.database, "DROP TABLE fuzz_recovery", nil)
	}
	count, err = migrator.Up(ctx)
	require.NoError(t, err, "migration recovery failed")
	require.Equal(t, 1, count, "migration recovery applied the wrong number of migrations")
	state.recoveryRows(ctx, t, recoveryTable, [][2]int64{{1, recoveryFirstValue}, {recoveryLastID, recoveryLastValue}})
	state.recoveryRows(ctx, t, recoveryGate, [][2]int64{{2, recoveryGateValue}})
	recoveryComplete(ctx, t, migrator, plan, progress)
	state.recoverySeed(ctx, t, backend)
	for _, table := range []string{recoveryTable, recoveryGate, recoverySeedGate} {
		// #nosec G202 -- Every table belongs to this fixed isolated recovery catalogue.
		fuzzExec(ctx, t, state.database, "DROP TABLE "+table, nil)
	}
}

// recoveryFailure checks atomic rollback or exact resumable statement checkpoints.
//
// Takes t (*testing.T) which reports incorrect failure history or data.
// Takes migrator (*migrate.Migrator) which attempted the interrupted migration.
// Takes files (fstest.MapFS) which supplies mutable independent source text.
// Takes atomic (bool) which requires rollback of both schema and data.
// Takes progress (bool) which requires exact nontransactional checkpoints.
//
// Returns *migrate.RepairPlan which can later be checked for stale-plan rejection.
func (state *migrationExecution) recoveryFailure(ctx context.Context, t *testing.T, migrator *migrate.Migrator, files fstest.MapFS, atomic, progress bool) *migrate.RepairPlan {
	t.Helper()
	plan, err := migrator.PlanRepair(ctx)
	require.NoError(t, err)
	if progress {
		recoveryPrefix(ctx, t, migrator, files, plan)
	} else {
		require.Empty(t, plan.Dirty, "atomic or checkpoint-free backend reported dirty progress")
	}
	statuses, err := migrator.Status(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, 1, "failed migration history differs: %+v", statuses)
	require.Equal(t, progress, statuses[0].Applied, "failed migration history differs: %+v", statuses)
	if atomic {
		relations := observeFuzzMigration(ctx, t, state.database, state.suite.FuzzSuite)
		require.NotContains(t, relations, recoveryTable, "atomic migration retained its failed table")
	} else {
		state.recoveryRows(ctx, t, recoveryTable, [][2]int64{{1, recoveryFirstValue}})
	}
	return plan
}

// recoverySeed checks atomic seed rollback and idempotent retry after a real failure.
//
// Takes t (*testing.T) which reports premature history or lost seeded data.
// Takes backend (migrate.Backend) which supplies the isolated recovery namespace.
func (state *migrationExecution) recoverySeed(ctx context.Context, t *testing.T, backend migrate.Backend) {
	t.Helper()
	clearing := "DELETE FROM fuzz_recovery;"
	if state.model.Engine == engineClickHouse {
		clearing = "TRUNCATE TABLE fuzz_recovery;"
	}
	files := fstest.MapFS{"seeds/0001_retry.sql": {Data: []byte(clearing +
		" INSERT INTO fuzz_recovery VALUES (7, 70); INSERT INTO fuzz_recovery_seed_gate VALUES (8, 80); INSERT INTO fuzz_recovery VALUES (9, 90);")}}
	seeder, err := migrate.NewSeeder(state.database, backend, files, "seeds")
	require.NoError(t, err)
	count, err := seeder.Apply(ctx)
	require.Error(t, err, "seed did not fail at its absent dependency")
	require.Zero(t, count, "seed did not fail at its absent dependency")
	statuses, err := seeder.Status(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, 1, "failed seed history differs: %+v", statuses)
	require.False(t, statuses[0].Applied, "failed seed was recorded as complete")
	if state.model.Engine == engineClickHouse {

		state.recoveryRows(ctx, t, recoveryTable, [][2]int64{{recoverySeedFirstID, recoverySeedFirstValue}})
	} else {
		state.recoveryRows(ctx, t, recoveryTable, [][2]int64{{1, recoveryFirstValue}, {recoveryLastID, recoveryLastValue}})
	}
	fuzzExec(ctx, t, state.database, recoveryDDL(state.model.Engine, recoverySeedGate), nil)
	count, err = seeder.Apply(ctx)
	require.NoError(t, err, "seed recovery failed")
	require.Equal(t, 1, count, "seed recovery applied the wrong number of seeds")
	state.recoveryRows(ctx, t, recoveryTable, [][2]int64{{recoverySeedFirstID, recoverySeedFirstValue}, {recoverySeedLastID, recoverySeedLastValue}})
	state.recoveryRows(ctx, t, recoverySeedGate, [][2]int64{{recoverySeedGateID, recoverySeedGateValue}})
	count, err = seeder.Apply(ctx)
	require.NoError(t, err, "applying the recovered seed again")
	require.Zero(t, count, "successfully recovered seed repeated")
	state.recoveryRows(ctx, t, recoveryTable, [][2]int64{{recoverySeedFirstID, recoverySeedFirstValue}, {recoverySeedLastID, recoverySeedLastValue}})
}

// recoveryRows checks exact data after a failure or recovery transition.
//
// Takes t (*testing.T) which reports changed native data.
// Takes table (string) which names a fixed independent recovery table.
// Takes expected ([][2]int64) which supplies independently predicted rows.
func (state *migrationExecution) recoveryRows(ctx context.Context, t *testing.T, table string, expected [][2]int64) {
	t.Helper()
	rows, err := executionRows(ctx, state.database, table)
	require.NoError(t, err)
	require.Equal(t, expected, rows, "recovery data differs in %s", table)
}

// recoveryDDL builds one independently declared native recovery table.
//
// Takes engine (string) which selects native integer declarations.
// Takes table (string) which names a fixed recovery table.
//
// Returns string which creates a table with exact integer data.
func recoveryDDL(engine, table string) string {
	declaration := "(id BIGINT NOT NULL, value BIGINT NOT NULL)"
	if engine == engineClickHouse {
		declaration = "(id Int64, value Int64) ENGINE = MergeTree ORDER BY id"
	}
	return fmt.Sprintf("CREATE TABLE %s %s", table, declaration)
}

// recoveryComplete checks idempotence and stale-plan rejection after successful resume.
//
// Takes t (*testing.T) which reports invalid persisted history transitions.
// Takes migrator (*migrate.Migrator) which completed recovery.
// Takes plan (*migrate.RepairPlan) which predates recovery.
// Takes progress (bool) which identifies a formerly dirty migration.
func recoveryComplete(ctx context.Context, t *testing.T, migrator *migrate.Migrator, plan *migrate.RepairPlan, progress bool) {
	t.Helper()
	require.NoError(t, migrator.Validate(ctx))
	count, err := migrator.Up(ctx)
	require.NoError(t, err, "running Up after recovery")
	require.Zero(t, count, "successful recovered migration repeated")
	if progress {
		_, err := migrator.Repair(ctx, migrate.RepairOptions{Plan: plan, Completed: map[int64]int{1: 2}})
		require.ErrorIs(t, err, migrate.ErrRepairOutOfDate, "stale recovery plan was not rejected")
	}
}

// recoveryPrefix rejects dirty rollback and edits to an already completed SQL prefix.
//
// Takes t (*testing.T) which reports unsafe execution after interruption.
// Takes migrator (*migrate.Migrator) which owns the dirty migration.
// Takes files (fstest.MapFS) which supplies the independent migration source.
// Takes plan (*migrate.RepairPlan) which records the native completed statement.
func recoveryPrefix(ctx context.Context, t *testing.T, migrator *migrate.Migrator, files fstest.MapFS, plan *migrate.RepairPlan) {
	t.Helper()
	require.Len(t, plan.Dirty, 1, "failed migration lost its exact completed prefix")
	require.True(t, plan.Dirty[0].LastStatement == 1 && plan.Dirty[0].ChecksumMatch, "failed migration lost its exact completed prefix %+v", plan.Dirty)
	count, err := migrator.Down(ctx, 1)
	require.ErrorIs(t, err, migrate.ErrDirtyRollback, "dirty migration was allowed to roll back")
	require.Zero(t, count, "dirty migration was allowed to roll back")
	original := files[recoveryMigrationFile]
	files[recoveryMigrationFile] = &fstest.MapFile{Data: []byte(strings.Replace(string(original.Data), "(1, 42)", "(1, 99)", 1))}
	count, err = migrator.Up(ctx)
	var mismatch *migrate.ResumeMismatchError
	var checksum *migrate.ChecksumMismatchError
	if count != 0 || (!errors.As(err, &mismatch) && !errors.As(err, &checksum)) {
		require.Failf(t, "edited completed prefix was not rejected", "count %d, error %v", count, err)
	}
	files[recoveryMigrationFile] = original
}
