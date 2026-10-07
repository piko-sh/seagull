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

package sqlexec

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/migrate"
)

var testAdvisoryLockSQL = AdvisoryLockSQL{
	Name:    nil,
	Wait:    func(name string) string { return "SELECT take_lock(" + name + ", 5)" },
	Try:     func(name string) string { return "SELECT take_lock(" + name + ", 0)" },
	Release: func(name string) string { return "SELECT drop_lock(" + name + ")" },
}

var errRendererFault = errors.New("renderer fault")

type renderCounts struct {
	calls atomic.Int32
}

func (counts *renderCounts) count(render func(string) string) func(string) string {
	return func(input string) string {
		counts.calls.Add(1)
		return render(input)
	}
}

func testBackendSpec() BackendSpec {
	postgres := postgresEngine()
	return BackendSpec{
		Placeholder:         numberedPlaceholder,
		CreateHistoryTable:  postgres.createHistoryTable,
		CreateSeedTable:     postgres.createSeedTable,
		InsertSeed:          postgres.insertSeed,
		TableExists:         postgres.tableExists,
		DeleteHistory:       nil,
		ClearSeedHistory:    nil,
		Split:               nil,
		AdvisoryLock:        new(testAdvisoryLockSQL),
		TableLock:           new(postgresTableLockSQL),
		Name:                "martian",
		HistoryReadModifier: "",
		DefaultLock:         LockAdvisory,
		TransactionalDDL:    true,
		AppendOnlyHistory:   false,
		MultiStatementSeeds: true,
	}
}

func TestCustomRejectsIncompleteSpecs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		change  func(spec *BackendSpec)
		missing string
	}{
		{name: "placeholder", change: func(spec *BackendSpec) { spec.Placeholder = nil }, missing: "BackendSpec.Placeholder"},
		{name: "history table", change: func(spec *BackendSpec) { spec.CreateHistoryTable = nil }, missing: "BackendSpec.CreateHistoryTable"},
		{name: "seed table", change: func(spec *BackendSpec) { spec.CreateSeedTable = nil }, missing: "BackendSpec.CreateSeedTable"},
		{name: "seed insert", change: func(spec *BackendSpec) { spec.InsertSeed = nil }, missing: "BackendSpec.InsertSeed"},
		{name: "table probe", change: func(spec *BackendSpec) { spec.TableExists = nil }, missing: "BackendSpec.TableExists"},
		{name: "advisory wait", change: func(spec *BackendSpec) { spec.AdvisoryLock.Wait = nil }, missing: "AdvisoryLock.Wait"},
		{name: "table try lock", change: func(spec *BackendSpec) { spec.TableLock.TryLock = nil }, missing: "TableLock.TryLock"},
		{name: "default lock without its SQL", change: func(spec *BackendSpec) { spec.AdvisoryLock = nil }, missing: "default lock mode advisory"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spec := testBackendSpec()
			spec.AdvisoryLock = new(*spec.AdvisoryLock)
			spec.TableLock = new(*spec.TableLock)
			testCase.change(&spec)
			backend := Custom(spec)

			require.ErrorIs(t, backend.err, migrate.ErrInvalidConfig)
			require.ErrorContains(t, backend.err, testCase.missing)
			_, err := NewExecutor(fakeDatabase(t), backend)
			require.ErrorIs(t, err, migrate.ErrInvalidConfig)
		})
	}
}

func TestCustomAppliesOptionsLikeTheBuiltIns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		backend   Backend
		wantNames Names
		wantLocks lockPair
		wantError error
	}{
		{
			name:      "defaults",
			backend:   Custom(testBackendSpec()),
			wantNames: namesFor(DefaultNamespace),
			wantLocks: lockPair{
				migration: newCustomAdvisoryLock(testAdvisoryLockSQL, numberedPlaceholder, "sgl_migrations", "martian"),
				seed:      newCustomAdvisoryLock(testAdvisoryLockSQL, numberedPlaceholder, "sgl_seeds", "martian seed"),
			},
		},
		{
			name:      "namespace and table lock",
			backend:   Custom(testBackendSpec(), WithNamespace("acme"), WithLock(LockTable)),
			wantNames: namesFor("acme"),
			wantLocks: customTableLocks(postgresTableLockSQL)(namesFor("acme"), false),
		},
		{
			name:      "clashing history table",
			backend:   Custom(testBackendSpec(), WithHistoryTable("sgl_seeds")),
			wantError: ErrInvalidIdentifier,
		},
		{
			name:      "unsafe namespace",
			backend:   Custom(testBackendSpec(), WithNamespace("bad name")),
			wantError: ErrInvalidIdentifier,
		},
		{
			name: "lock mode without its SQL",
			backend: func() Backend {
				spec := testBackendSpec()
				spec.TableLock = nil
				return Custom(spec, WithLock(LockTable))
			}(),
			wantError: ErrUnsupportedLock,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.wantError != nil {
				require.ErrorIs(t, testCase.backend.err, testCase.wantError)
				return
			}
			require.NoError(t, testCase.backend.err)
			require.Equal(t, testCase.wantNames, testCase.backend.Names())
			require.Equal(t, testCase.wantLocks, lockPair{migration: testCase.backend.migrationLock, seed: testCase.backend.seedLock})
		})
	}
}

func TestCustomBackendRunsMigrationsAndSeeds(t *testing.T) {
	t.Parallel()

	for name, backend := range map[string]Backend{
		"advisory lock": Custom(testBackendSpec()),
		"table lock":    Custom(testBackendSpec(), WithLock(LockTable)),
		"no lock":       Custom(testBackendSpec(), WithLock(LockNone)),
		"duckdb":        DuckDB(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			memory := newMemoryDatabase()
			database := memory.open()
			t.Cleanup(func() { _ = database.Close() })
			migrator, err := NewMigrator(database, backend, threeMigrations, "migrations")
			require.NoError(t, err)

			applied, err := migrator.Up(t.Context())
			require.NoError(t, err)
			require.Equal(t, 3, applied)
			require.ElementsMatch(t, []int64{1, 2, 3}, memory.appliedVersions())

			seedExecutor, err := NewSeedExecutor(database, backend)
			require.NoError(t, err)
			require.NoError(t, seedExecutor.AcquireSeedLock(t.Context()))
			require.NoError(t, seedExecutor.ReleaseSeedLock(t.Context()))
		})
	}
}

func panicking[T any](T) T { panic(errRendererFault) }

func TestCustomRecordsARendererThatPanicsInsteadOfCrashing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		change  func(spec *BackendSpec)
		name    string
		options []BackendOption
	}{
		{name: "placeholder", change: func(spec *BackendSpec) { spec.Placeholder = func(int) string { panic(errRendererFault) } }},
		{name: "history table", change: func(spec *BackendSpec) { spec.CreateHistoryTable = panicking[string] }},
		{name: "seed table", change: func(spec *BackendSpec) { spec.CreateSeedTable = panicking[string] }},
		{name: "table probe", change: func(spec *BackendSpec) { spec.TableExists = panicking[string] }},
		{name: "seed history clear", change: func(spec *BackendSpec) { spec.ClearSeedHistory = panicking[string] }},
		{name: "seed insert", change: func(spec *BackendSpec) {
			spec.InsertSeed = func(string, string, string, string, string) string { panic(errRendererFault) }
		}},
		{name: "history delete", change: func(spec *BackendSpec) {
			spec.DeleteHistory = func(string, string) string { panic(errRendererFault) }
		}},
		{name: "history column", change: func(spec *BackendSpec) {
			spec.AddHistoryColumn = func(string, string) string { panic(errRendererFault) }
		}},
		{name: "advisory lock name", change: func(spec *BackendSpec) { spec.AdvisoryLock.Name = panicking[string] }},
		{name: "advisory lock wait", change: func(spec *BackendSpec) { spec.AdvisoryLock.Wait = panicking[string] }},
		{
			name:    "table lock create",
			options: []BackendOption{WithLock(LockTable)},
			change: func(spec *BackendSpec) {
				spec.TableLock.Create = func(string, string) string { panic(errRendererFault) }
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spec := testBackendSpec()
			spec.AdvisoryLock = new(*spec.AdvisoryLock)
			spec.TableLock = new(*spec.TableLock)
			testCase.change(&spec)

			var backend Backend
			require.NotPanics(t, func() { backend = Custom(spec, testCase.options...) })

			require.ErrorIs(t, backend.err, migrate.ErrInvalidConfig)
			require.ErrorIs(t, backend.err, fault.ErrPanic)
			require.ErrorIs(t, backend.err, errRendererFault)
			require.ErrorContains(t, backend.err, "rendering the martian backend's SQL: panic: renderer fault")
			_, err := NewExecutor(fakeDatabase(t), backend)
			require.ErrorIs(t, err, fault.ErrPanic)
			_, err = NewSeedExecutor(fakeDatabase(t), backend)
			require.ErrorIs(t, err, fault.ErrPanic)
		})
	}
}

func countingSpec(counts *renderCounts) BackendSpec {
	spec := minimalSpec()
	placeholder := spec.Placeholder
	spec.Placeholder = func(index int) string {
		counts.calls.Add(1)
		return placeholder(index)
	}
	spec.CreateHistoryTable = counts.count(spec.CreateHistoryTable)
	spec.CreateSeedTable = counts.count(spec.CreateSeedTable)
	spec.TableExists = counts.count(spec.TableExists)
	spec.ClearSeedHistory = counts.count(func(table string) string { return "DELETE FROM " + table })
	insertSeed := spec.InsertSeed
	spec.InsertSeed = func(table, version, name, checksum, duration string) string {
		counts.calls.Add(1)
		return insertSeed(table, version, name, checksum, duration)
	}
	spec.DeleteHistory = func(table, version string) string {
		counts.calls.Add(1)
		return fmt.Sprintf("DELETE FROM %s WHERE version = %s", table, version)
	}
	spec.AddHistoryColumn = func(table, column string) string {
		counts.calls.Add(1)
		return addColumnRenderer(standardHistoryColumnTypes)(table, column)
	}
	return spec
}

func TestCustomBackendCallsItsRenderersOnlyWhileBeingBuilt(t *testing.T) {
	t.Parallel()

	var counts renderCounts
	backend := Custom(countingSpec(&counts))
	require.NoError(t, backend.err)
	rendered := counts.calls.Load()
	require.Positive(t, rendered)

	memory := newMemoryDatabase()
	database := memory.open()
	t.Cleanup(func() { _ = database.Close() })
	files := fstest.MapFS{
		"migrations/0001_users.up.sql":   {Data: []byte("CREATE TABLE users (id BIGINT);\n")},
		"migrations/0001_users.down.sql": {Data: []byte("DROP TABLE users;\n")},
	}
	migrator, err := NewMigrator(database, backend, files, "migrations")
	require.NoError(t, err)
	applied, err := migrator.Up(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, applied)
	require.NoError(t, migrator.Force(t.Context(), 1))
	rolledBack, err := migrator.Down(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, rolledBack)

	seeds, err := NewSeedExecutor(database, backend)
	require.NoError(t, err)
	require.NoError(t, seeds.EnsureSeedTable(t.Context()))
	require.NoError(t, seeds.ExecuteSeed(t.Context(), migrate.SeedRecord{Name: "rows", Checksum: "c", Content: []byte("SELECT 1;"), Version: 1}))
	require.NoError(t, seeds.ClearSeedHistory(t.Context()))

	require.Equal(t, rendered, counts.calls.Load(), "a run called a renderer of the spec instead of the SQL rendered by Custom")
	require.Contains(t, memory.executed(), "DELETE FROM sgl_migrations WHERE version = $1")
	require.Contains(t, memory.executed(), "DELETE FROM sgl_seeds")
}

func TestCustomSplitThatPanicsFailsTheCallInsteadOfCrashing(t *testing.T) {
	t.Parallel()

	spec := minimalSpec()
	spec.Split = func(string) ([]string, error) { panic(errRendererFault) }
	spec.MultiStatementSeeds = false
	backend := Custom(spec, WithLock(LockNone))
	require.NoError(t, backend.err)

	var logs bytes.Buffer
	ctx := logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))
	executor, err := NewExecutor(fakeDatabase(t), backend)
	require.NoError(t, err)
	seeds, err := NewSeedExecutor(fakeDatabase(t), backend)
	require.NoError(t, err)
	record := migrate.MigrationRecord{Name: "m", Checksum: "c", Content: []byte("OK1;"), Version: 1, SkipUpTo: -1}

	calls := []struct {
		call func() error
		name string
	}{
		{name: "a migration", call: func() error {
			return executor.ExecuteMigration(ctx, record, migrate.MigrationDirectionUp, true)
		}},
		{name: "a repair", call: func() error { return executor.RecordProgress(ctx, record, 1) }},
		{name: "a seed", call: func() error {
			return seeds.ExecuteSeed(ctx, migrate.SeedRecord{Name: "s", Checksum: "c", Content: []byte("OK2;"), Version: 1})
		}},
		{name: "the history editor", call: func() error {
			_, splitError := executor.SplitStatements([]byte("OK1;"))
			return splitError
		}},
		{name: "the backend", call: func() error {
			_, splitError := backend.Split("OK1;")
			return splitError
		}},
	}
	for _, call := range calls {
		var err error
		require.NotPanics(t, func() { err = call.call() }, call.name)
		require.ErrorIs(t, err, fault.ErrPanic, call.name)
		require.ErrorIs(t, err, errRendererFault, call.name)
		require.ErrorContains(t, err, "martian backend Split: panic: renderer fault", call.name)
		require.NotContains(t, err.Error(), "goroutine", "%s: the stack belongs in the log, not the error", call.name)
	}
	require.Equal(t, 3, strings.Count(logs.String(), `"msg":"recovered a panic"`),
		"each call that carried the run's logger must log the stack once")
	require.Contains(t, logs.String(), `"operation":"martian backend Split"`)
}
