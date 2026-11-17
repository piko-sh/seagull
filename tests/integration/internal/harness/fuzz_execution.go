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
	"strconv"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_clickhouse"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_cockroachdb"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_duckdb"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_mysql"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_postgres"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_timescaledb"
)

const (
	// executionInputDeadline bounds each complete migration workflow.
	executionInputDeadline = 40 * time.Second

	// executionWatchdogDeadline stops a worker whose workflow ignores cancellation.
	executionWatchdogDeadline = 2 * executionInputDeadline

	// executionChangedMigration is the migration the changed and repair steps edit.
	executionChangedMigration = "migrations/0001_step.up.sql"
)

var (
	// errUnknownExecutionOperation reports a step the harness has no operation for.
	errUnknownExecutionOperation = errors.New("unknown required migration operation")

	// errExecutionCancelled is the cause of the context the cancel step runs under.
	errExecutionCancelled = errors.New("the cancel step cancelled the lifecycle call")

	// executionOperations maps each modelled step to the public operation it runs.
	executionOperations = map[string]executionOperation{
		"up": func(ctx context.Context, state *migrationExecution, _ fuzzcase.ExecutionStep) (int, error) {
			return state.migrator.Up(ctx)
		},
		"up-to": func(ctx context.Context, state *migrationExecution, step fuzzcase.ExecutionStep) (int, error) {
			return state.migrator.UpTo(ctx, int64(step.Target))
		},
		"down": func(ctx context.Context, state *migrationExecution, step fuzzcase.ExecutionStep) (int, error) {
			return state.migrator.Down(ctx, step.Target)
		},
		"down-to": func(ctx context.Context, state *migrationExecution, step fuzzcase.ExecutionStep) (int, error) {
			return state.migrator.DownTo(ctx, int64(step.Target))
		},
		"seed": func(ctx context.Context, state *migrationExecution, _ fuzzcase.ExecutionStep) (int, error) {
			return state.seeder.Apply(ctx)
		},
		"reseed": func(ctx context.Context, state *migrationExecution, _ fuzzcase.ExecutionStep) (int, error) {
			return state.seeder.Reseed(ctx)
		},
		"validate": func(ctx context.Context, state *migrationExecution, _ fuzzcase.ExecutionStep) (int, error) {
			return 0, state.migrator.Validate(ctx)
		},
		"status": func(context.Context, *migrationExecution, fuzzcase.ExecutionStep) (int, error) { return 0, nil },
		"restart": func(_ context.Context, state *migrationExecution, _ fuzzcase.ExecutionStep) (int, error) {
			return 0, state.restart()
		},
		"changed": func(ctx context.Context, state *migrationExecution, _ fuzzcase.ExecutionStep) (int, error) {
			return 0, state.rejectChanged(ctx)
		},
		"repair": func(ctx context.Context, state *migrationExecution, _ fuzzcase.ExecutionStep) (int, error) {
			return 0, state.repairChanged(ctx)
		},
		"cancel": func(ctx context.Context, state *migrationExecution, _ fuzzcase.ExecutionStep) (int, error) {
			return 0, state.cancelled(ctx)
		},
	}
)

// executionOperation runs one modelled step and returns the count the service reports.
type executionOperation func(ctx context.Context, state *migrationExecution, step fuzzcase.ExecutionStep) (int, error)

// migrationExecution owns one native workflow and its independent expected state.
type migrationExecution struct {
	// database owns the current isolated native connection pool.
	database *sql.DB

	// migrator runs the actual public migration service.
	migrator *migrate.Migrator

	// seeder runs the actual public seed service.
	seeder *migrate.Seeder

	// files contains the current independently generated SQL bundle.
	files fstest.MapFS

	// connection allows a fresh connection without exposing credentials.
	connection string

	// suite identifies the native driver and schema adapters.
	suite NativeFuzzSuite

	// backend retains the engine migration and locking contracts.
	backend migrate.Backend

	// model supplies independently generated operations and values.
	model fuzzcase.ExecutionCase

	// expected predicts persisted history and data after each operation.
	expected fuzzcase.ExecutionState
}

// construct opens fresh service objects over the current connection and file bundle.
//
// Returns error when either public constructor rejects the valid model.
func (state *migrationExecution) construct() error {
	state.database.SetMaxOpenConns(2)
	var err error
	state.migrator, err = migrate.NewMigrator(state.database, state.backend, state.files, "migrations")
	if err != nil {
		return err
	}
	state.seeder, err = migrate.NewSeeder(state.database, state.backend, state.files, "seeds")
	return err
}

// apply invokes one public lifecycle operation without using its results as expectations.
//
// Takes step (fuzzcase.ExecutionStep) which supplies bounded operation arguments.
//
// Returns int which reports the service's actual affected count.
// Returns error when an operation violates its required outcome.
func (state *migrationExecution) apply(ctx context.Context, step fuzzcase.ExecutionStep) (int, error) {
	operation, known := executionOperations[step.Operation]
	if !known {
		return 0, fmt.Errorf("%w: %q", errUnknownExecutionOperation, step.Operation)
	}
	return operation(ctx, state, step)
}

// restart verifies persisted state through a fresh connection and fresh service objects.
//
// Returns error when closing, reopening or constructing a service fails.
func (state *migrationExecution) restart() error {
	if err := state.database.Close(); err != nil {
		return err
	}
	database, err := sql.Open(state.suite.Driver.Name, state.connection)
	if err != nil {
		return err
	}
	state.database = database
	return state.construct()
}

// rejectChanged checks that a changed completed migration is refused, then restores it.
//
// Returns error when changed SQL validates or runs silently.
func (state *migrationExecution) rejectChanged(ctx context.Context) error {
	original := state.changeFirstMigration()
	defer func() { state.files[executionChangedMigration] = original }()
	return state.requireChangeRefused(ctx)
}

// repairChanged checks that a changed completed migration is refused and then repaired by
// rebaselining exactly that migration.
//
// Returns error when changed SQL runs silently or repair changes the wrong history.
func (state *migrationExecution) repairChanged(ctx context.Context) error {
	state.changeFirstMigration()
	if err := state.requireChangeRefused(ctx); err != nil {
		return err
	}
	plan, err := state.migrator.PlanRepair(ctx)
	if err != nil {
		return fmt.Errorf("planning the repair: %w", err)
	}
	if len(plan.Changed) != 1 || len(plan.Dirty) != 0 {
		return errors.New("repair plan omitted the exact changed migration")
	}
	result, err := state.migrator.Repair(ctx, migrate.RepairOptions{Plan: plan, Completed: nil})
	if err != nil {
		return fmt.Errorf("repairing the history: %w", err)
	}
	if !slices.Equal(result.Rebaselined, []int64{1}) || len(result.Resolved) != 0 {
		return errors.New("repair changed unexpected migration history")
	}
	return state.migrator.Validate(ctx)
}

// changeFirstMigration edits the first migration's comment, which changes its checksum.
//
// Returns *fstest.MapFile which is the migration before the edit.
func (state *migrationExecution) changeFirstMigration() *fstest.MapFile {
	original := state.files[executionChangedMigration]
	state.files[executionChangedMigration] = &fstest.MapFile{Data: append([]byte("-- The reviewed migration comment changed.\n"), original.Data...)}
	return original
}

// requireChangeRefused checks that validation and Up both refuse the changed migration.
//
// Returns error when either accepts it.
func (state *migrationExecution) requireChangeRefused(ctx context.Context) error {
	var mismatch *migrate.ChecksumMismatchError
	if err := state.migrator.Validate(ctx); !errors.As(err, &mismatch) {
		return fmt.Errorf("changed checksum was not rejected: %w", err)
	}
	count, err := state.migrator.Up(ctx)
	if count != 0 || !errors.As(err, &mismatch) {
		return errors.New("changed migration was allowed to execute")
	}
	return nil
}

// cancelled requires cancelled migration and seed calls to preserve persisted state.
//
// Returns error when cancellation is lost or execution claims progress.
func (state *migrationExecution) cancelled(ctx context.Context) error {
	ctx, cancel := context.WithCancelCause(ctx)
	cancel(errExecutionCancelled)
	for _, run := range []func(context.Context) (int, error){state.migrator.Up, state.seeder.Apply} {
		count, err := run(ctx)
		if count != 0 || !errors.Is(err, context.Canceled) {
			return errors.New("cancelled lifecycle call lost cancellation or claimed progress")
		}
	}
	return nil
}

// check compares complete history, live table membership and every persisted data row.
//
// Takes t (*testing.T) which reports native discrepancies with the independent model.
func (state *migrationExecution) check(ctx context.Context, t *testing.T) {
	t.Helper()
	state.checkHistory(ctx, t)
	relations := observeFuzzMigration(ctx, t, state.database, state.suite.FuzzSuite)
	require.Len(t, relations, state.expected.Level, "migration history disagrees with live table membership")
	for version := 1; version <= state.expected.Level; version++ {
		name := "fuzz_execution_" + strconv.Itoa(version)
		relation, found := relations[name]
		require.True(t, found && !relation.view && len(relation.columns) == 2 && relation.columns[0].name == "id" && relation.columns[1].name == "value",
			"migration schema differs for %s", name)
		require.False(t, relation.columns[0].nullable || relation.columns[1].nullable, "migration lost a NOT NULL constraint")
		rows, err := executionRows(ctx, state.database, name)
		require.NoError(t, err)
		require.Equal(t, fuzzcase.ExecutionRows(state.model, state.expected, version), rows, "migration data changed in %s", name)
	}
}

// checkHistory compares every migration and seed status with the independent model.
//
// Takes t (*testing.T) which reports omitted or corrupted history.
func (state *migrationExecution) checkHistory(ctx context.Context, t *testing.T) {
	t.Helper()
	statuses, err := state.migrator.Status(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, fuzzcase.ExecutionMigrations, "migration status lost catalogue entries")
	for index, status := range statuses {
		applied := index < state.expected.Level
		require.True(t, executionStatusMatches(status, int64(index+1), applied), "incorrect persisted migration status %+v", status)
	}
	seeds, err := state.seeder.Status(ctx)
	require.NoError(t, err)
	require.Len(t, seeds, 1, "incorrect persisted seed history %+v", seeds)
	seed := seeds[0]
	require.True(t, seed.Applied == state.expected.SeedApplied && seed.ChecksumMatch && (!seed.Applied || !seed.AppliedAt.IsZero()),
		"incorrect persisted seed history %+v", seeds)
}

// FuzzMigrationExecution exercises actual migration and seeding services on native
// engines.
//
// Takes f (*testing.F) which owns the bounded workflow seed catalogue.
// Takes suite (NativeFuzzSuite) which supplies isolated native state and driver identity.
func FuzzMigrationExecution(f *testing.F, suite NativeFuzzSuite) {
	f.Helper()
	if suite.OpenNative == nil || suite.Driver.Name == "" || len(fuzzcase.ExecutionCapabilities(suite.Engine)) == 0 {
		require.Fail(f, "incomplete migration execution suite")
	}
	for _, seed := range fuzzcase.ExecutionSeeds() {
		f.Add(seed)
	}
	var metadata sync.Once
	f.Fuzz(func(t *testing.T, data []byte) {
		model, ok := fuzzcase.DecodeExecution(suite.Engine, data)
		if !ok {
			t.Skip("unsupported migration execution envelope")
		}
		runMigrationExecution(t, suite, model, &metadata)
	})
}

// runMigrationExecution checks every public operation against independent persisted
// state.
//
// Takes t (*testing.T) which owns isolated native resources.
// Takes suite (NativeFuzzSuite) which selects the real driver and migration backend.
// Takes model (fuzzcase.ExecutionCase) which supplies independently expected operations.
// Takes metadata (*sync.Once) which records the native version once per worker.
//
// Panics in the watchdog callback if a workflow ignores cancellation.
func runMigrationExecution(t *testing.T, suite NativeFuzzSuite, model fuzzcase.ExecutionCase, metadata *sync.Once) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(executionInputDeadline), errFuzzInputDeadline)
	defer cancel()
	watchdog := time.AfterFunc(scaled(executionWatchdogDeadline), func() { panic("migration execution ignored cancellation: " + t.Name()) })
	defer watchdog.Stop()
	database, connection := suite.OpenNative(t)
	state := migrationExecution{database: database, connection: connection, suite: suite, model: model,
		files: fstest.MapFS{}, backend: executionBackend(suite.Engine), migrator: nil, seeder: nil, expected: fuzzcase.ExecutionState{}}
	defer func() { _ = state.database.Close() }()
	for name, content := range model.Files() {
		state.files[name] = &fstest.MapFile{Data: content}
	}
	metadata.Do(func() {
		writeFuzzEngineMetadata(ctx, t, database, suite.Engine)
		writeFuzzCapabilities(t, "execution", suite.Engine, fuzzcase.ExecutionCapabilities(suite.Engine))
	})
	if model.Recovery {
		state.recovery(ctx, t)
	}
	require.NoError(t, state.construct())
	state.check(ctx, t)
	for index, step := range model.Steps {
		count, err := state.apply(ctx, step)
		require.NoError(t, err, "migration execution %v at step %d (%s)", model.Data, index, step.Operation)
		require.Equal(t, state.expected.Advance(step), count, "%s affected the wrong number of migrations or seeds", step.Operation)
		state.check(ctx, t)
	}
}

// executionBackend selects the actual native migration backend.
//
// Takes engine (string) which was validated by the independent model.
//
// Takes options (...migrate.BackendOption) which isolates recovery history when needed.
//
// Returns migrate.Backend which retains the engine's default locking guarantees.
func executionBackend(engine string, options ...migrate.BackendOption) migrate.Backend {
	switch engine {
	case enginePostgres:
		return migrate_backend_postgres.New(options...)
	case engineTimescaleDB:
		return migrate_backend_timescaledb.New(options...)
	case engineCockroachDB:
		return migrate_backend_cockroachdb.New(options...)
	case engineMySQL, engineMariaDB:
		return migrate_backend_mysql.New(options...)
	case engineClickHouse:
		return migrate_backend_clickhouse.New(options...)
	case engineDuckDB:
		return migrate_backend_duckdb.New(options...)
	default:
		return migrate_backend_sqlite.New(options...)
	}
}

// executionRows observes exact ordered integer rows directly through the native driver.
//
// Takes database (*sql.DB) which owns the current isolated workflow state.
// Takes table (string) which is constructed from the fixed migration catalogue.
//
// Returns [][2]int64 which contains independently scanned persisted data.
// Returns error when a row cannot be read exactly.
func executionRows(ctx context.Context, database *sql.DB, table string) ([][2]int64, error) {
	// #nosec G202 -- The table comes from the fixed independent migration catalogue.
	rows, err := database.QueryContext(ctx, "SELECT id, value FROM "+table+" ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var values [][2]int64
	for rows.Next() {
		var row [2]int64
		if err := rows.Scan(&row[0], &row[1]); err != nil {
			return nil, err
		}
		values = append(values, row)
	}
	return values, rows.Err()
}

// executionStatusMatches reports whether one migration's status is what the model
// predicts.
//
// Takes status (migrate.MigrationStatus) which is the persisted status.
// Takes version (int64) which is the version the position holds.
// Takes applied (bool) which is whether the model has applied it.
//
// Returns bool which is true when the status is clean and matches.
func executionStatusMatches(status migrate.MigrationStatus, version int64, applied bool) bool {
	if status.Version != version || status.Applied != applied || status.Dirty || !status.HasDownMigration {
		return false
	}
	return !applied || (status.ChecksumMatch && !status.AppliedAt.IsZero())
}
