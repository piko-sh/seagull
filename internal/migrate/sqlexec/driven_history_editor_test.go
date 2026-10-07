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
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
)

var historyEditorStart = time.Date(2026, time.October, 7, 9, 30, 0, 0, time.UTC)

func newEditingExecutor(t *testing.T, store *progressStore, backend Backend) *Executor {
	t.Helper()
	database := store.open()
	t.Cleanup(func() { _ = database.Close() })
	executor, err := NewExecutor(database, backend, WithClock(newSteppingClock(historyEditorStart, time.Second)))
	require.NoError(t, err)
	require.NoError(t, executor.EnsureMigrationTable(t.Context()))
	if backend.engine.appendOnlyHistory {
		store.emulateReplacingMergeTree()
	}
	return executor
}

func boatsRecord() migrate.MigrationRecord {
	content := sixStatementContent()
	return migrate.MigrationRecord{
		Name: "boats", Checksum: checksumOf(content), DownChecksum: "down-sum", StatementsHash: "", Content: content,
		Version: 1, SkipUpTo: -1, SplitterVersion: 0, Dirty: false,
	}
}

func TestSplitStatementsDividesAsMigrationsRun(t *testing.T) {
	t.Parallel()

	executor := newEditingExecutor(t, newProgressStore(), SQLite())
	statements, err := executor.SplitStatements(sixStatementContent())
	require.NoError(t, err)
	assert.Equal(t, []string{"STMT_0 col0", "STMT_1 col1", "STMT_2 col2", "STMT_3 col3", "STMT_4 col4", "STMT_5 col5"}, statements)

	_, err = executor.SplitStatements([]byte("SELECT 'unterminated"))
	require.ErrorIs(t, err, ErrMalformedStatement)
}

func TestRecordAppliedWritesACleanRow(t *testing.T) {
	t.Parallel()

	recordedBefore := historyEditorStart.Add(-time.Hour)
	tests := []struct {
		existing      *progressRow
		backend       Backend
		name          string
		wantAppliedAt time.Time
		wantDuration  int64
	}{
		{name: "a missing row is inserted now", backend: SQLite(), wantAppliedAt: historyEditorStart},
		{
			name: "an existing row keeps when it was applied", backend: SQLite(),
			existing:      &progressRow{appliedAt: recordedBefore, durationMs: 42, name: "old", checksum: "old-sum", dirty: true, lastStatement: new(int64(2)), splitterVersion: new(int64(1)), statementsHash: new("digest")},
			wantAppliedAt: recordedBefore, wantDuration: 42,
		},
		{
			name: "an append-only row is replaced", backend: ClickHouse(),
			existing:      &progressRow{appliedAt: recordedBefore, durationMs: 42, name: "old", checksum: "old-sum"},
			wantAppliedAt: recordedBefore, wantDuration: 42,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := newProgressStore()
			executor := newEditingExecutor(t, store, testCase.backend)
			if testCase.existing != nil {
				store.setRow(1, *testCase.existing)
			}

			require.NoError(t, executor.RecordApplied(t.Context(), boatsRecord()))

			row, exists := store.row(1)
			require.True(t, exists)
			assert.Equal(t, "boats", row.name)
			assert.Equal(t, checksumOf(sixStatementContent()), row.checksum)
			assert.Equal(t, new("down-sum"), row.downChecksum)
			assert.False(t, row.dirty)
			assert.Nil(t, row.lastStatement)
			assert.Nil(t, row.splitterVersion)
			assert.Nil(t, row.statementsHash)
			assert.Equal(t, testCase.wantAppliedAt, row.appliedAt.UTC())
			assert.Equal(t, testCase.wantDuration, row.durationMs)
		})
	}
}

func TestRecordProgressRecordsWhatADirtyMigrationCompleted(t *testing.T) {
	t.Parallel()

	statements, err := SQLite().Split(string(sixStatementContent()))
	require.NoError(t, err)
	tests := []struct {
		existing  *progressRow
		name      string
		completed int
		wantRow   bool
	}{
		{name: "none removes the row", completed: 0, existing: &progressRow{name: "boats", dirty: true, lastStatement: new(int64(3))}},
		{name: "none with no row does nothing", completed: 0},
		{name: "some rewrite the row", completed: 2, wantRow: true, existing: &progressRow{name: "boats", dirty: true, lastStatement: new(int64(3))}},
		{name: "some insert a missing row", completed: 5, wantRow: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := newProgressStore()
			executor := newEditingExecutor(t, store, SQLite())
			if testCase.existing != nil {
				store.setRow(1, *testCase.existing)
			}

			require.NoError(t, executor.RecordProgress(t.Context(), boatsRecord(), testCase.completed))

			row, exists := store.row(1)
			require.Equal(t, testCase.wantRow, exists)
			if !testCase.wantRow {
				return
			}
			assert.True(t, row.dirty)
			assert.Equal(t, checksumOf(sixStatementContent()), row.checksum)
			assert.Equal(t, new(int64(testCase.completed-1)), row.lastStatement)
			assert.Equal(t, new(int64(splitterVersion)), row.splitterVersion)
			assert.Equal(t, new(digestOf(statements[:testCase.completed])), row.statementsHash)
		})
	}
}

func TestRecordProgressRefusesWhatItCannotRecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		backend   Backend
		name      string
		message   string
		completed int
	}{
		{name: "every statement", backend: SQLite(), completed: 6, message: "between 0 and 5 of them, not 6"},
		{name: "a negative count", backend: SQLite(), completed: -1, message: "not -1"},
		{name: "progress on an append-only history", backend: ClickHouse(), completed: 2, message: "ClickHouse records no statement progress"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := newProgressStore()
			executor := newEditingExecutor(t, store, testCase.backend)
			err := executor.RecordProgress(t.Context(), boatsRecord(), testCase.completed)
			require.ErrorIs(t, err, migrate.ErrInvalidConfig)
			assert.ErrorContains(t, err, testCase.message)
			_, exists := store.row(1)
			assert.False(t, exists)
		})
	}

	executor := newEditingExecutor(t, newProgressStore(), SQLite())
	record := boatsRecord()
	record.Content = []byte("SELECT 'unterminated")
	require.ErrorIs(t, executor.RecordProgress(t.Context(), record, 1), ErrMalformedStatement)
}

func TestRepairResumesADirtyMigrationTheSplitterCannotMatch(t *testing.T) {
	t.Parallel()

	store := newProgressStore()
	store.createLegacyTable()
	store.setRow(1, progressRow{name: "boats", checksum: checksumOf(sixStatementContent()), dirty: true, lastStatement: new(int64(0))})
	database := store.open()
	t.Cleanup(func() { _ = database.Close() })
	migrator, err := NewMigrator(database, SQLite(), sixStatementMigration, "migrations")
	require.NoError(t, err)

	_, err = migrator.Up(t.Context())
	require.ErrorAs(t, err, new(*migrate.ResumeMismatchError))

	plan, err := migrator.PlanRepair(t.Context())
	require.NoError(t, err)
	require.Len(t, plan.Dirty, 1)
	assert.Len(t, plan.Dirty[0].Statements, 6)
	assert.Equal(t, 0, plan.Dirty[0].LastStatement)

	_, err = migrator.Repair(t.Context(), migrate.RepairOptions{Plan: plan, Completed: map[int64]int{1: 2}})
	require.NoError(t, err)

	store.resetExecuted()
	count, err := migrator.Up(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, []string{"STMT_2 col2", "STMT_3 col3", "STMT_4 col4", "STMT_5 col5"}, store.userStatements(),
		"the retry resumes after the statements the repair recorded")
	row, _ := store.row(1)
	assert.False(t, row.dirty)
}

func TestForceRecordsAMigrationWithoutRunningIt(t *testing.T) {
	t.Parallel()

	store := newProgressStore()
	database := store.open()
	t.Cleanup(func() { _ = database.Close() })
	migrator, err := NewMigrator(database, SQLite(), sixStatementMigration, "migrations")
	require.NoError(t, err)

	require.NoError(t, migrator.Force(t.Context(), 1))
	assert.Empty(t, store.userStatements())

	count, err := migrator.Up(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	assert.Empty(t, store.userStatements())
}

func TestTryAcquireSeedLockDoesNotWait(t *testing.T) {
	t.Parallel()

	shared := newMemoryDatabase()
	first, second := shared.open(), shared.open()
	t.Cleanup(func() { _ = first.Close(); _ = second.Close() })
	holder, err := NewSeedExecutor(first, Postgres())
	require.NoError(t, err)
	other, err := NewSeedExecutor(second, Postgres())
	require.NoError(t, err)

	require.NoError(t, holder.TryAcquireSeedLock(t.Context()))
	require.ErrorIs(t, holder.TryAcquireSeedLock(t.Context()), migrate.ErrLockNotAcquired, "a second holder in this process")
	require.ErrorIs(t, other.TryAcquireSeedLock(t.Context()), migrate.ErrLockNotAcquired, "a holder in another session")
	require.NoError(t, holder.ReleaseSeedLock(t.Context()))
	require.NoError(t, other.TryAcquireSeedLock(t.Context()))
	require.NoError(t, other.ReleaseSeedLock(t.Context()))
}

func TestTableLockOutlivesTheContextItWasAcquiredWith(t *testing.T) {
	t.Parallel()

	for name, backend := range map[string]Backend{"postgres": PostgresPgBouncer(), "cockroachdb": CockroachDB()} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			shared := newMemoryDatabase()
			first, second := shared.open(), shared.open()
			t.Cleanup(func() { _ = first.Close(); _ = second.Close() })
			holder, err := NewExecutor(first, backend)
			require.NoError(t, err)
			contender, err := NewExecutor(second, backend)
			require.NoError(t, err)

			acquisition, cancel := context.WithCancelCause(t.Context())
			require.NoError(t, holder.AcquireLock(acquisition))
			cancel(migrate.ErrLockTimeout)

			require.Never(t, func() bool {
				shared.mu.Lock()
				defer shared.mu.Unlock()
				return !shared.anyLockHeld()
			}, 50*time.Millisecond, 5*time.Millisecond,
				"ending the acquisition's context must not roll back the lock transaction")
			require.ErrorIs(t, contender.TryAcquireLock(t.Context()), migrate.ErrLockNotAcquired)
			require.NoError(t, holder.ReleaseLock(t.Context()))
			require.NoError(t, contender.TryAcquireLock(t.Context()))
			require.NoError(t, contender.ReleaseLock(t.Context()))
		})
	}
}

func TestAppendOnlyHistoryRewriteInsertsBeforeItRemovesAnything(t *testing.T) {
	t.Parallel()

	recordedBefore := historyEditorStart.Add(-time.Hour)
	insertSQL := newExecutor(nil, ClickHouse()).historyInsertSQL()
	deleteSQL := newExecutor(nil, ClickHouse()).deleteHistorySQL()
	tests := []struct {
		rawAppliedAt  driver.Value
		insertError   error
		wantAppliedAt time.Time
		name          string
		wantName      string
		wantDelete    bool
	}{
		{name: "the new row outranks the old without a delete", wantName: "boats", wantAppliedAt: recordedBefore},
		{name: "a failed insert leaves the old row in place", insertError: errConnectionLost, wantName: "old", wantAppliedAt: recordedBefore},
		{name: "an unreadable applied time removes the old row first", rawAppliedAt: "not a date", wantName: "boats", wantDelete: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := newProgressStore()
			executor := newEditingExecutor(t, store, ClickHouse())
			store.setRow(1, progressRow{appliedAt: recordedBefore, rawAppliedAt: testCase.rawAppliedAt, durationMs: 42, name: "old", checksum: "old-sum"})
			if testCase.insertError != nil {
				store.failOn(insertSQL, testCase.insertError)
			}

			err := executor.RecordApplied(t.Context(), boatsRecord())

			if testCase.insertError != nil {
				require.ErrorIs(t, err, testCase.insertError)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, testCase.wantDelete, store.ran(deleteSQL))
			row, exists := store.row(1)
			require.True(t, exists, "the migration must keep a history row whatever fails, or the next Up runs it again")
			assert.Equal(t, testCase.wantName, row.name)
			assert.Equal(t, testCase.wantAppliedAt, row.appliedAt.UTC())
			assert.Equal(t, int64(42), row.durationMs)
		})
	}
}
