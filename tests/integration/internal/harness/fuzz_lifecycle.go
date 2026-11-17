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
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/migrate"
)

// errUnknownLifecycleOperation reports a step the harness has no operation for.
var errUnknownLifecycleOperation = errors.New("unknown migration lifecycle operation")

// lifecycleRun is one history's run against the database.
type lifecycleRun struct {
	// database is the isolated pool.
	database *sql.DB

	// backend is the engine's migration backend.
	backend migrate.Backend

	// model is the history.
	model *fuzzcase.LifecycleCase

	// state is the model's expectation after the steps run so far.
	state fuzzcase.LifecycleState
}

// apply runs one step, checks its result and the database's state.
//
// Takes index (int) which is the step's position, for messages.
// Takes step (fuzzcase.LifecycleStep) which is the step.
func (run *lifecycleRun) apply(ctx context.Context, t *testing.T, index int, step fuzzcase.LifecycleStep) {
	t.Helper()
	before := run.state.Clone()
	want := run.state.Advance(run.model, step)
	files := fstest.MapFS{}
	for name, content := range run.model.Files(run.state.Revealed) {
		files[name] = &fstest.MapFile{Data: content}
	}
	count, err := run.execute(ctx, t, step, files)
	t.Logf("step %d %s %d: count %d, error %v", index, step.Operation, step.Target, count, err)
	require.Equal(t, want.Fails, err != nil, "step %d %s %d failed with %v. The model expects failure %t (state before %+v)",
		index, step.Operation, step.Target, err, want.Fails, before)
	require.Equal(t, want.Count, count, "step %d %s %d gave the wrong count (state before %+v)", index, step.Operation, step.Target, before)
	run.check(ctx, t, index, files)
}

// execute runs a step through the services, or creates the gate table for a fix.
//
// Takes step (fuzzcase.LifecycleStep) which is the step.
// Takes files (fstest.MapFS) which holds the visible files.
//
// Returns int which is how many migrations or seeds the step applied or rolled back.
// Returns error which is the step's error.
func (run *lifecycleRun) execute(ctx context.Context, t *testing.T, step fuzzcase.LifecycleStep, files fstest.MapFS) (int, error) {
	t.Helper()
	migrator, err := migrate.NewMigrator(run.database, run.backend, files, "migrations")
	require.NoError(t, err)
	switch step.Operation {
	case "up":
		return migrator.Up(ctx)
	case "up-to":
		return migrator.UpTo(ctx, step.Target)
	case "down":
		return migrator.Down(ctx, int(step.Target))
	case "down-to":
		return migrator.DownTo(ctx, step.Target)
	case "validate":
		return 0, migrator.Validate(ctx)
	case "seed":
		seeder, seederErr := migrate.NewSeeder(run.database, run.backend, files, "seeds")
		require.NoError(t, seederErr)
		return seeder.Apply(ctx)
	case "fix":
		_, gateErr := run.database.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS "+fuzzcase.LifecycleGate+" "+fuzzcase.LifecycleTableDeclaration(run.model.Engine))
		require.NoError(t, gateErr, "creating the gate table")
		return 0, nil
	case "reveal":

		return 0, nil
	default:
		require.Failf(t, "unknown step", "%v: %q", errUnknownLifecycleOperation, step.Operation)
		return 0, nil
	}
}

// check compares every migration's status and table with the model.
//
// Takes index (int) which is the step's position, for messages.
// Takes files (fstest.MapFS) which holds the visible files.
func (run *lifecycleRun) check(ctx context.Context, t *testing.T, index int, files fstest.MapFS) {
	t.Helper()
	migrator, err := migrate.NewMigrator(run.database, run.backend, files, "migrations")
	require.NoError(t, err)
	statuses, err := migrator.Status(ctx)
	require.NoError(t, err, "after step %d, Status failed", index)
	byVersion := map[int64]migrate.MigrationStatus{}
	for _, status := range statuses {
		byVersion[status.Version] = status
	}
	for position, migration := range run.model.Migrations {
		if !migration.Hidden || run.state.Revealed {
			status, listed := byVersion[migration.Version]
			require.True(t, listed, "after step %d, Status does not list version %d", index, migration.Version)
			checkLifecycleStatus(t, index, migration, status, run.state.Migrations[position])
		}
		run.checkTable(ctx, t, index, migration, run.state.Migrations[position])
	}
}

// checkTable compares a migration's table with the model.
//
// Takes index (int) which is the step's position, for messages.
// Takes migration (fuzzcase.LifecycleMigration) which is the migration.
// Takes want (fuzzcase.LifecycleMigrationState) which is the model's state.
func (run *lifecycleRun) checkTable(ctx context.Context, t *testing.T, index int, migration fuzzcase.LifecycleMigration, want fuzzcase.LifecycleMigrationState) {
	t.Helper()
	rows, exists, err := run.tableRows(ctx, fuzzcase.LifecycleTable(migration))
	require.NoError(t, err, "after step %d, reading the table of version %d", index, migration.Version)
	require.Equal(t, want.HasTable, exists, "after step %d, the table of version %d exists wrongly", index, migration.Version)
	if exists {
		wantRows := slices.Sorted(slices.Values(want.Rows))
		require.True(t, slices.Equal(wantRows, rows), "after step %d, the table of version %d holds %v, want %v", index, migration.Version, rows, wantRows)
	}
}

// tableRows reads a table's ids in ascending order.
//
// Takes table (string) which is the table's name.
//
// Returns []int64 which holds the ids.
// Returns bool which is false when the query fails, as it does for a missing table.
// Returns error when an existing table cannot be read.
func (run *lifecycleRun) tableRows(ctx context.Context, table string) ([]int64, bool, error) {
	rows, err := run.database.QueryContext(ctx, "SELECT id FROM "+table+" ORDER BY id") //nolint:gosec // a table name the model writes
	if err != nil {
		return nil, false, nil
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, true, errors.Join(fmt.Errorf("scanning %s: %w", table, scanErr), rows.Close())
		}
		ids = append(ids, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, true, fmt.Errorf("reading %s: %w", table, err)
	}
	return ids, true, nil
}

// FuzzMigrationLifecycle compares migration and seed results with an independent model.
// It checks errors, counts, history status, and table rows after each step, including
// malformed files, out-of-order versions, and recoverable failures.
//
// Takes f (*testing.F) which receives the seeds.
// Takes suite (NativeFuzzSuite) which supplies the isolated connections.
func FuzzMigrationLifecycle(f *testing.F, suite NativeFuzzSuite) {
	f.Helper()
	require.NotNil(f, suite.OpenNative, "incomplete migration lifecycle suite")
	for _, seed := range fuzzcase.LifecycleSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		model, ok := fuzzcase.DecodeLifecycle(suite.Engine, data)
		if !ok {
			t.Skip("unsupported migration lifecycle envelope")
		}
		runLifecycle(t, suite, &model)
	})
}

// runLifecycle runs one decoded version 20 history on a fresh database and checks every
// step.
//
// Takes t (*testing.T) which reports differences from the model.
// Takes suite (NativeFuzzSuite) which supplies the isolated connection.
// Takes model (*fuzzcase.LifecycleCase) which holds the history and its expectations.
//
// Panics in the watchdog callback if the history ignores cancellation.
func runLifecycle(t *testing.T, suite NativeFuzzSuite, model *fuzzcase.LifecycleCase) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(executionInputDeadline), errFuzzInputDeadline)
	defer cancel()
	watchdog := time.AfterFunc(scaled(executionWatchdogDeadline), func() { panic("migration lifecycle ignored cancellation: " + t.Name()) })
	defer watchdog.Stop()
	database, _ := suite.OpenNative(t)
	run := lifecycleRun{database: database, backend: executionBackend(suite.Engine), model: model, state: model.InitialState()}
	for index, step := range model.Steps {
		run.apply(ctx, t, index, step)
	}
}

// checkLifecycleStatus compares a migration's status with the model.
//
// Takes index (int) which is the step's position, for messages.
// Takes migration (fuzzcase.LifecycleMigration) which is the migration.
// Takes status (migrate.MigrationStatus) which is what Status reports.
// Takes want (fuzzcase.LifecycleMigrationState) which is the model's state.
func checkLifecycleStatus(t *testing.T, index int, migration fuzzcase.LifecycleMigration, status migrate.MigrationStatus, want fuzzcase.LifecycleMigrationState) {
	t.Helper()

	require.Equal(t, want.Applied || want.Dirty, status.Applied, "after step %d, version %d is applied wrongly", index, migration.Version)
	require.Equal(t, want.Dirty, status.Dirty, "after step %d, version %d is dirty wrongly", index, migration.Version)
	if want.Dirty && !want.RollingBack {
		require.NotNil(t, status.LastStatement, "after step %d, dirty version %d records no last statement", index, migration.Version)
		require.Equal(t, want.Completed-1, *status.LastStatement, "after step %d, dirty version %d records the wrong last statement", index, migration.Version)
	}
}
