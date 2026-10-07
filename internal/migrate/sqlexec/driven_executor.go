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
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/migrate"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// insertPlaceholderVersion is the 1-based bind-parameter index for the version column.
	insertPlaceholderVersion = 1

	// insertPlaceholderName is the 1-based bind-parameter index for the name column.
	insertPlaceholderName = 2

	// insertPlaceholderChecksum is the 1-based bind-parameter index for the checksum column.
	insertPlaceholderChecksum = 3

	// insertPlaceholderAppliedAt is the 1-based bind-parameter index for the applied_at
	// column.
	insertPlaceholderAppliedAt = 4

	// insertPlaceholderDurationMs is the 1-based bind-parameter index for the duration_ms
	// column.
	insertPlaceholderDurationMs = 5

	// insertPlaceholderDownChecksum is the 1-based bind-parameter index for the
	// down_checksum column.
	insertPlaceholderDownChecksum = 6

	// insertPlaceholderLastStatement is the 1-based bind-parameter index for the
	// last_statement column.
	insertPlaceholderLastStatement = 7

	// insertPlaceholderDirty is the 1-based bind-parameter index for the dirty column.
	insertPlaceholderDirty = 8

	// insertPlaceholderSplitterVersion is the 1-based bind-parameter index for the
	// splitter_version column.
	insertPlaceholderSplitterVersion = 9

	// insertPlaceholderStatementsHash is the 1-based bind-parameter index for the
	// statements_hash column.
	insertPlaceholderStatementsHash = 10

	// progressPlaceholderLastStatement is the 1-based bind-parameter index for the
	// last_statement column in the progress UPDATE statement.
	progressPlaceholderLastStatement = 1

	// progressPlaceholderSplitterVersion is the 1-based bind-parameter index for the
	// splitter_version column in the progress UPDATE statement.
	progressPlaceholderSplitterVersion = 2

	// progressPlaceholderStatementsHash is the 1-based bind-parameter index for the
	// statements_hash column in the progress UPDATE statement.
	progressPlaceholderStatementsHash = 3

	// progressPlaceholderVersion is the 1-based bind-parameter index for the version column
	// in the progress UPDATE statement.
	progressPlaceholderVersion = 4

	// clearDirtyPlaceholderDirty is the 1-based bind-parameter index for the dirty column in
	// the clearDirty UPDATE statement.
	clearDirtyPlaceholderDirty = 1

	// clearDirtyPlaceholderDurationMs is the 1-based bind-parameter index for the
	// duration_ms column in the clearDirty UPDATE statement.
	clearDirtyPlaceholderDurationMs = 2

	// clearDirtyPlaceholderVersion is the 1-based bind-parameter index for the version
	// column in the clearDirty UPDATE statement.
	clearDirtyPlaceholderVersion = 3

	// defaultProgressUpdateBatchSize is the number of successful statements an executor
	// applies between writes to the last_statement column: one, to minimise the completed
	// work a crash or failed checkpoint can leave unrecorded.
	defaultProgressUpdateBatchSize = 1

	// checkpointFlushTimeout bounds the write of a resume checkpoint after a statement
	// failed or the run was cancelled, so a database that stopped answering cannot hold the
	// run up until the connection times out.
	checkpointFlushTimeout = 5 * time.Second
)

// Executor implements migrate.ExecutorPort over database/sql for every Backend.
//
// An Executor is safe for concurrent use. Its state is guarded by a mutex, and a second
// AcquireLock in the same process waits until ReleaseLock (TryAcquireLock fails with
// migrate.ErrLockNotAcquired instead), serialising runs on that executor. Coordination
// across processes depends on the backend's lock mode. When a run pins a session, other
// calls share it and may observe the run's uncommitted work: drive one Executor from one
// Migrator, which runs its calls one at a time.
type Executor struct {
	// database holds the underlying database connection pool.
	database *sql.DB

	// clock supplies migration durations and timestamps for history rows created without
	// execution; never nil after NewExecutor, which defaults it to observe.SystemClock().
	clock observe.Clock

	// slot admits one lock holder at a time within the process.
	slot holderSlot

	// held is the lock the executor holds, or nil; guarded by mutex.
	held *heldLock

	// backend holds the dialect-specific SQL and locking behaviour.
	backend Backend

	// progressUpdateBatchSize is the number of successful statements that may be applied
	// between writes to the last_statement column, set via WithProgressBatchSize. Zero or
	// below means use defaultProgressUpdateBatchSize.
	progressUpdateBatchSize int

	// mutex guards held.
	mutex sync.Mutex
}

var (
	_ migrate.ExecutorPort = (*Executor)(nil)

	_ migrate.HistoryEditorPort = (*Executor)(nil)
)

// NewExecutor creates the migrate.ExecutorPort for a database/sql database, for use with
// migrate.New when executor settings such as the clock or progress batch size are needed.
//
// The history table is validated as a safe identifier first, which is what makes
// interpolating it into the executor's SQL safe.
//
// Takes database (*sql.DB) which is the database connection.
// Takes backend (Backend) which provides the database's SQL and locking.
// Takes options (...ExecutorOption) which set the clock and progress batch size.
//
// Returns *Executor which is ready to execute migrations.
// Returns error which is the backend's recorded option error, or wraps
// migrate.ErrInvalidConfig for a backend no constructor built, a nil database or an
// option given a value it cannot use.
func NewExecutor(database *sql.DB, backend Backend, options ...ExecutorOption) (*Executor, error) {
	if err := backend.usable(database); err != nil {
		return nil, err
	}
	if err := newExecutorSettings(options).invalid(); err != nil {
		return nil, err
	}
	return newExecutor(database, backend, options...), nil
}

// newExecutor assembles an Executor without validating the backend.
//
// Takes database (*sql.DB) which is the database connection.
// Takes backend (Backend) which provides the database's SQL and locking.
// Takes options (...ExecutorOption) which set the clock and progress batch size.
//
// Returns *Executor which is ready to execute migrations.
func newExecutor(database *sql.DB, backend Backend, options ...ExecutorOption) *Executor {
	settings := newExecutorSettings(options)
	return &Executor{
		database:                database,
		backend:                 backend,
		progressUpdateBatchSize: settings.progressBatchSize,
		clock:                   settings.clock,
		slot:                    newHolderSlot(),
		held:                    nil,
		mutex:                   sync.Mutex{},
	}
}

// LexicalRules returns the lexical rules of the executor's engine, so a Migrator reads a
// migration's directive comments as the engine writes SQL.
//
// Returns sqllex.Rules which are the engine's rules.
func (executor *Executor) LexicalRules() sqllex.Rules {
	return executor.backend.engine.rules
}

// EnsureMigrationTable creates the migration history table if it does not exist, and adds
// the columns that record statement progress (splitter_version and statements_hash) to a
// table an earlier release created without them.
//
// The Migrator calls it while holding the configured lock. Backends without a database
// lock require external coordination of concurrent runs.
//
// Returns error when the table cannot be created or upgraded.
func (executor *Executor) EnsureMigrationTable(ctx context.Context) error {
	runner := executor.queryExecutor()
	createSQL := executor.backend.engine.createHistoryTable(executor.historyTable())
	if _, createError := runner.ExecContext(ctx, createSQL); createError != nil {
		return fmt.Errorf("creating migration table: %w", createError)
	}
	if upgradeError := executor.upgradeHistoryTable(ctx, runner); upgradeError != nil {
		return fmt.Errorf("upgrading migration table: %w", upgradeError)
	}
	return nil
}

// AcquireLock takes the backend's migration lock, waiting while another goroutine in the
// process or another process holds it.
//
// Locks that are session-scoped pin a dedicated connection, which every call runs on
// until ReleaseLock; a table lock also pins a second connection for the work, since its
// own session holds the lock's transaction. On PostgreSQL, TimescaleDB and CockroachDB a
// run always works on one pinned session, a connection of its own under LockNone, and it
// reads that session's current schema before anything runs: the history table is
// qualified by it for the rest of the run, so a migration's SET search_path cannot move
// it.
//
// Returns error when the lock cannot be acquired or ctx ends first.
func (executor *Executor) AcquireLock(ctx context.Context) error {
	return executor.acquire(ctx, true)
}

// TryAcquireLock takes the migration lock only when it is free.
//
// Returns error which wraps migrate.ErrLockNotAcquired when another goroutine or process
// holds the lock, or the failure otherwise.
func (executor *Executor) TryAcquireLock(ctx context.Context) error {
	return executor.acquire(ctx, false)
}

// ReleaseLock releases the migration lock and its pinned connections, doing nothing when
// no lock is held. On backends whose sessions keep a current schema, the run's session is
// discarded instead of returned to the pool.
//
// Returns error when the lock cannot be released or the connection the run worked on
// apart from the lock's session cannot be closed.
//
// Concurrency: detaches the held lock under the executor mutex before releasing it.
func (executor *Executor) ReleaseLock(ctx context.Context) error {
	executor.mutex.Lock()
	held := executor.held
	executor.held = nil
	executor.mutex.Unlock()
	if held == nil {
		return nil
	}
	defer executor.slot.free()

	handle, workCloseError := finishRun(executor.backend.engine, held)
	return joinWorkCloseError("migration", executor.backend.migrationLock.Release(ctx, handle), workCloseError)
}

// ExecuteMigration runs a single migration's SQL content.
//
// For up migrations it records the migration in the history; for down migrations it
// deletes the record. The content is split into statements with the backend's lexical
// rules and run one at a time, so that per-statement progress can be recorded.
//
// The migration and its history row commit together in one transaction when
// useTransaction is true, the engine's DDL is transactional, no statement is one the
// engine's rules mark as unable to run inside a transaction, and the migration is not a
// retry of a dirty one. Otherwise each statement runs on its own: an up migration is
// recorded as dirty first, its progress is recorded after every statement (or every
// WithProgressBatchSize statements), and the record is marked clean once every statement
// has run. On an append-only backend, such as ClickHouse, up migrations instead record
// only a completed row after all statements succeed, with no dirty progress or resume.
//
// A record whose SkipUpTo is not -1 resumes after that statement only when it carries the
// current splitter version and the digest of the statements before it, as recorded by the
// interrupted run; otherwise nothing runs and a *migrate.ResumeMismatchError is returned.
//
// Takes migration (migrate.MigrationRecord) which holds the migration SQL and metadata.
// Takes direction (migrate.MigrationDirection) which specifies whether this is an up or
// down migration.
// Takes useTransaction (bool) which is false when the migration asks to run without a
// transaction.
//
// Under a table lock the lock's transaction is first confirmed to be still open, so a run
// whose lock the server ended stops with ErrLockLost before the migration runs.
//
// Returns error when the lock was lost, the migration cannot be split or resumed, or its
// SQL or history update fails.
func (executor *Executor) ExecuteMigration(
	ctx context.Context,
	migration migrate.MigrationRecord,
	direction migrate.MigrationDirection,
	useTransaction bool,
) error {
	if lockError := confirmLockHeld(ctx, executor.lockTransaction()); lockError != nil {
		return fmt.Errorf("before migration %d: %w", migration.Version, lockError)
	}
	start := executor.clock.Now()

	statements, splitError := executor.backend.engine.split(ctx, string(migration.Content))
	if splitError != nil {
		return fmt.Errorf("splitting migration %d statements: %w", migration.Version, splitError)
	}
	digest, resumeError := resumeDigest(migration, statements)
	if resumeError != nil {
		return resumeError
	}
	run := migrationRun{start: start, digest: digest, statements: statements, record: migration}

	inTransaction, checkError := executor.runsInTransaction(ctx, run, useTransaction)
	if checkError != nil {
		return fmt.Errorf("checking whether migration %d can run in a transaction: %w", migration.Version, checkError)
	}
	if inTransaction {
		return executor.executeInTransaction(ctx, run, direction)
	}
	return executor.executeWithoutTransaction(ctx, run, direction)
}

// runsInTransaction reports whether a migration runs inside one transaction with its
// history row.
//
// Takes run (migrationRun) which is the migration and its statements.
// Takes useTransaction (bool) which is false when the migration asks to run without one.
//
// Returns bool which is true when every condition for a transaction holds.
// Returns error when the engine's lexical rules panicked while reading a statement.
func (executor *Executor) runsInTransaction(ctx context.Context, run migrationRun, useTransaction bool) (bool, error) {
	engine := executor.backend.engine
	if !useTransaction || !engine.transactionalDDL || run.record.Dirty {
		return false, nil
	}
	requiresNone, err := engine.requiresNoTransaction(ctx, run.statements)
	if err != nil {
		return false, err
	}
	return !requiresNone, nil
}

// acquire takes the in-process slot and then the database lock, and records what they and
// the run pinned.
//
// Takes wait (bool) which reports whether to wait for a lock held elsewhere.
//
// Returns error when the lock cannot be acquired or the run's session prepared; the slot
// is freed again then, and a lock already taken is released.
//
// Concurrency: the slot serialises acquisitions; the mutex protects the held handle.
func (executor *Executor) acquire(ctx context.Context, wait bool) error {
	if slotError := executor.slot.claim(ctx, wait); slotError != nil {
		return slotError
	}
	take := executor.backend.migrationLock.TryAcquire
	if wait {
		take = executor.backend.migrationLock.Acquire
	}
	handle, lockError := take(ctx, executor.database)
	if lockError != nil {
		executor.slot.free()
		return lockError
	}
	held, pinError := executor.pinRun(ctx, handle)
	if pinError != nil {
		executor.slot.free()
		return pinError
	}
	executor.mutex.Lock()
	executor.held = held
	executor.mutex.Unlock()
	return nil
}

// pinRun pins what the run works on once the lock is held, releasing the lock when it
// cannot.
//
// A lock held by an open transaction cannot share its session with migrations: a
// migration's BEGIN would nest inside the lock transaction, which PostgreSQL ignores
// (committing the lock early) and CockroachDB rejects. Such a run, and on an engine whose
// sessions keep a current schema a run under a lock that pins no session, works on a
// connection of its own; see prepareRun.
//
// Takes handle (lockHandle) which is what the lock pinned.
//
// Returns *heldLock which records the lock, any work connection and the run's schema.
// Returns error when the run's session cannot be prepared; the lock is released then.
func (executor *Executor) pinRun(ctx context.Context, handle lockHandle) (*heldLock, error) {
	held, pinError := prepareRun(ctx, executor.database, executor.backend.engine, handle, handle.transaction != nil, "migration")
	if pinError == nil {
		return held, nil
	}
	releaseError := executor.backend.migrationLock.Release(ctx, handle)
	return nil, joinLockReleaseErrors("releasing lock after preparing the migration session failed", releaseError, pinError)
}

// queryExecutor returns the connection migrations run on: the work connection under a
// table lock, the pinned connection under a session lock, or the connection pool when no
// lock is held.
//
// Returns queryRunner which is a pinned connection or the database pool.
//
// Concurrency: reads the held connections under the executor mutex.
func (executor *Executor) queryExecutor() queryRunner {
	executor.mutex.Lock()
	defer executor.mutex.Unlock()
	if executor.held == nil {
		return executor.database
	}
	if executor.held.workConnection != nil {
		return executor.held.workConnection
	}
	if executor.held.handle.connection != nil {
		return executor.held.handle.connection
	}
	return executor.database
}

// lockTransaction returns the open transaction that holds the run's lock, or nil when no
// lock is held or the lock is held without a transaction.
//
// Returns *sql.Tx which is the lock transaction, or nil.
//
// Concurrency: reads the held lock under the executor mutex.
func (executor *Executor) lockTransaction() *sql.Tx {
	executor.mutex.Lock()
	defer executor.mutex.Unlock()
	return executor.held.lockTransaction()
}

// historyTable returns the history table as the executor's SQL names it: qualified by the
// schema the run's session started in while a run that read one holds the lock, and as
// the backend names it otherwise.
//
// Returns string which is the table name to interpolate.
//
// Concurrency: reads the held run under the executor mutex.
func (executor *Executor) historyTable() string {
	executor.mutex.Lock()
	defer executor.mutex.Unlock()
	return qualifiedTable(executor.backend.engine, executor.held, executor.backend.historyTable())
}

// appliedVersionsSQL renders the SELECT AppliedVersions reads the history with.
//
// Takes table (string) which is the history table as the run names it.
// Takes withProgress (bool) which adds the splitter_version and statements_hash columns.
//
// Returns string which is the SELECT statement for the applied-migrations query.
func (executor *Executor) appliedVersionsSQL(table string, withProgress bool) string {
	return renderAppliedVersionsSQL(table, executor.backend.engine.historyReadModifier, withProgress)
}

// progressBatchSize returns the effective progress-update batch size, falling back to
// defaultProgressUpdateBatchSize when the executor was constructed without an explicit
// override.
//
// Returns int which is the number of successful statements between progress writes.
func (executor *Executor) progressBatchSize() int {
	if executor.progressUpdateBatchSize <= 0 {
		return defaultProgressUpdateBatchSize
	}
	return executor.progressUpdateBatchSize
}

// executeInTransaction runs the migration SQL and history update within a single database
// transaction. Statements are executed individually for better error messages, but the
// transaction ensures atomicity so no dirty state tracking is needed.
//
// Takes run (migrationRun) which is the migration and its statements.
// Takes direction (migrate.MigrationDirection) which specifies whether this is an up or
// down migration.
//
// Returns error when the transaction, migration SQL, or history update fails, joined with
// the failure to roll the transaction back when that fails too.
func (executor *Executor) executeInTransaction(
	ctx context.Context,
	run migrationRun,
	direction migrate.MigrationDirection,
) (err error) {
	transaction, beginError := executor.queryExecutor().BeginTx(ctx, nil)
	if beginError != nil {
		return fmt.Errorf("beginning transaction: %w", beginError)
	}
	defer rollbackUnlessDone(transaction, "migration transaction", &err)

	if executionError := executor.execStatements(ctx, transaction, run); executionError != nil {
		return fmt.Errorf("executing SQL: %w", executionError)
	}

	durationMs := executor.clock.Now().Sub(run.start).Milliseconds()

	if historyError := executor.updateHistory(
		ctx, transaction, run.record, direction, run.start, durationMs,
	); historyError != nil {
		return historyError
	}

	if commitError := transaction.Commit(); commitError != nil {
		return fmt.Errorf("committing transaction: %w", commitError)
	}

	return nil
}

// executeWithoutTransaction runs the migration SQL outside a transaction with
// per-statement dirty state tracking, delegating to direction-specific helpers.
//
// Takes run (migrationRun) which is the migration and its statements.
// Takes direction (migrate.MigrationDirection) which specifies whether this is an up or
// down migration.
//
// Returns error when the migration SQL or history update fails.
func (executor *Executor) executeWithoutTransaction(
	ctx context.Context,
	run migrationRun,
	direction migrate.MigrationDirection,
) error {
	if direction == migrate.MigrationDirectionUp {
		return executor.executeWithoutTransactionUp(ctx, run)
	}

	return executor.executeWithoutTransactionDown(ctx, run)
}

// executeWithoutTransactionUp handles non-transactional up migrations with per-statement
// dirty state tracking, or delegates to executeAppendOnlyUp for append-only backends. On
// full success the record is finalised with dirty = FALSE.
//
// The migration is recorded as dirty before its first statement runs. After each
// successful statement (or each WithProgressBatchSize statements) the index of the last
// completed statement is written with the splitter version and the digest of the
// statements completed so far, which a retry checks before it skips them. When a
// statement fails, or the context ends, the last completed index is written if it is not
// already, so a later retry resumes from the correct point.
//
// Takes run (migrationRun) which is the migration and its statements.
//
// Returns error when the migration SQL or history update fails.
func (executor *Executor) executeWithoutTransactionUp(ctx context.Context, run migrationRun) error {
	if executor.backend.engine.appendOnlyHistory {
		return executor.executeAppendOnlyUp(ctx, run)
	}

	if run.record.SkipUpTo < 0 {
		if preRecordError := executor.preRecordDirtyMigration(ctx, run); preRecordError != nil {
			return preRecordError
		}
	}

	batchSize := executor.progressBatchSize()
	lastFlushedIndex := run.record.SkipUpTo

	for index := max(run.record.SkipUpTo+1, 0); index < len(run.statements); index++ {
		if cancelError := contextStopped(ctx); cancelError != nil {
			return executor.handleCancellationCheckpoint(ctx, run, index, lastFlushedIndex, cancelError)
		}
		if runError := executor.runMigrationStatement(ctx, run, index, lastFlushedIndex); runError != nil {
			return runError
		}
		run.digest.add(run.statements[index])
		if shouldFlushProgress(index, lastFlushedIndex, batchSize, len(run.statements)) &&
			executor.flushProgressCheckpoint(ctx, run.record.Version, index, run.digest) {
			lastFlushedIndex = index
		}
	}

	return executor.clearDirty(ctx, run.record.Version, run.start)
}

// handleCancellationCheckpoint flushes the resume checkpoint when the context is
// cancelled part-way through a non-transactional up migration, then returns the
// cancellation error.
//
// The loop guard fires before the statement at currentIndex runs, so the last completed
// statement is currentIndex-1. With batched progress writes, statements between
// lastFlushedIndex and currentIndex-1 may have no checkpoint; without flushing, a resume
// would re-execute those possibly non-idempotent statements. The flush runs on a context
// detached from the cancelled ctx, which would reject the UPDATE, and bounded by
// checkpointFlushTimeout, so a database that stopped answering cannot hold the run up.
// Any flush error is joined onto the cancellation error rather than dropped silently.
//
// Takes run (migrationRun) which is the migration, its statements and their digest so
// far.
// Takes currentIndex (int) which is the 0-based index of the statement about to run.
// Takes lastFlushedIndex (int) which is the index of the most recent flushed checkpoint.
// Takes cancelError (error) which is the context cancellation cause to report.
//
// Returns error which is the cancellation error, joined with any checkpoint-flush error.
func (executor *Executor) handleCancellationCheckpoint(
	ctx context.Context,
	run migrationRun,
	currentIndex int,
	lastFlushedIndex int,
	cancelError error,
) error {
	cancellationError := fmt.Errorf(
		"migration %d cancelled before statement %d: %w", run.record.Version, currentIndex+1, cancelError,
	)

	lastCompletedIndex := currentIndex - 1
	if lastCompletedIndex <= lastFlushedIndex {
		return cancellationError
	}

	flushContext, cancel := checkpointContext(ctx)
	defer cancel()
	if progressError := executor.updateStatementProgress(flushContext, run.record.Version, lastCompletedIndex, run.digest); progressError != nil {
		return errors.Join(cancellationError, fmt.Errorf("flushing resume checkpoint: %w", progressError))
	}
	return cancellationError
}

// runMigrationStatement executes one statement of a non-transactional up migration.
//
// On failure, it saves the previous statement's checkpoint unless already saved, so a
// retry resumes at the correct point. A checkpoint error is joined with the statement
// error. Saving uses a context detached from ctx and bounded by checkpointFlushTimeout,
// allowing it to finish after cancellation.
//
// Takes run (migrationRun) which is the migration, its statements and the digest of the
// statements before index.
// Takes index (int) which is the statement to execute.
// Takes lastFlushedIndex (int) which is the index of the most recent flushed checkpoint.
//
// Returns error when the statement fails, wrapping the resume-checkpoint flush error too.
func (executor *Executor) runMigrationStatement(
	ctx context.Context,
	run migrationRun,
	index int,
	lastFlushedIndex int,
) error {
	_, executionError := executor.queryExecutor().ExecContext(ctx, run.statements[index])
	if executionError == nil {
		return nil
	}
	statementError := fmt.Errorf(
		"executing SQL: statement %d/%d of migration %d: %w",
		index+1, len(run.statements), run.record.Version, executionError,
	)
	if index-1 <= lastFlushedIndex {
		return statementError
	}
	flushContext, cancel := checkpointContext(ctx)
	defer cancel()
	if progressError := executor.updateStatementProgress(flushContext, run.record.Version, index-1, run.digest); progressError != nil {
		return errors.Join(statementError, fmt.Errorf("flushing resume checkpoint: %w", progressError))
	}
	return statementError
}

// executeAppendOnlyUp handles non-transactional up migrations for backends with
// append-only history, such as ClickHouse.
//
// It executes every statement and then records a single completed history row (dirty =
// false). It skips dirty pre-recording, per-statement progress and resume machinery. A
// migration that fails part-way leaves no history row and is re-run from the start on the
// next invocation. Statements must be safe to repeat, or their partial effects must be
// resolved before retrying.
//
// Takes run (migrationRun) which is the migration and its statements.
//
// Returns error when the migration SQL or history insert fails.
func (executor *Executor) executeAppendOnlyUp(ctx context.Context, run migrationRun) error {
	if executionError := executor.execStatements(ctx, executor.queryExecutor(), run); executionError != nil {
		return fmt.Errorf("executing SQL: %w", executionError)
	}

	durationMs := executor.clock.Now().Sub(run.start).Milliseconds()
	return executor.recordCompletedMigration(ctx, run.record, run.start, durationMs)
}

// executeWithoutTransactionDown handles non-transactional down migrations. Down
// migrations do not use dirty state tracking since they delete the history record on
// success.
//
// Takes run (migrationRun) which is the down migration and its statements.
//
// Returns error when the migration SQL or history update fails, joined with the failure
// to roll the history transaction back when that fails too.
func (executor *Executor) executeWithoutTransactionDown(ctx context.Context, run migrationRun) (err error) {
	if executionError := executor.execStatements(ctx, executor.queryExecutor(), run); executionError != nil {
		return fmt.Errorf("executing SQL: %w", executionError)
	}

	durationMs := executor.clock.Now().Sub(run.start).Milliseconds()

	if !executor.backend.engine.transactionalDDL {
		return executor.updateHistory(
			ctx, executor.queryExecutor(), run.record, migrate.MigrationDirectionDown, run.start, durationMs,
		)
	}

	transaction, beginError := executor.queryExecutor().BeginTx(ctx, nil)
	if beginError != nil {
		return fmt.Errorf("beginning history transaction: %w", beginError)
	}
	defer rollbackUnlessDone(transaction, "down-migration history transaction", &err)

	if historyError := executor.updateHistory(
		ctx, transaction, run.record, migrate.MigrationDirectionDown, run.start, durationMs,
	); historyError != nil {
		return historyError
	}

	if commitError := transaction.Commit(); commitError != nil {
		return fmt.Errorf("committing history: %w", commitError)
	}

	return nil
}

// preRecordDirtyMigration inserts a migration history record with dirty = TRUE and
// last_statement = -1 before any SQL statements are executed, with the splitter version
// and the digest of no statements. This ensures the migration is recorded as in-progress
// even if the process crashes during execution.
//
// Takes run (migrationRun) which is the migration about to run.
//
// Returns error when the INSERT statement fails.
func (executor *Executor) preRecordDirtyMigration(ctx context.Context, run migrationRun) error {
	exists, existsError := executor.historyRecordExists(ctx, run.record.Version)
	if existsError != nil {
		return fmt.Errorf("checking existing dirty record for migration %d: %w", run.record.Version, existsError)
	}
	if exists {
		return nil
	}

	_, insertError := executor.queryExecutor().ExecContext(ctx, executor.historyInsertSQL(),
		run.record.Version, run.record.Name, run.record.Checksum, run.start.UTC(), int64(0),
		nullableDownChecksum(run.record.DownChecksum), -1, true, splitterVersion, run.digest.sum(),
	)
	if insertError != nil {
		return fmt.Errorf("pre-recording dirty migration %d: %w", run.record.Version, insertError)
	}

	return nil
}

// historyInsertSQL builds the ten-column INSERT statement for the migration history table
// using the dialect's placeholder syntax. Shared by the dirty pre-record path, the
// transactional history update, and the append-only completed-record path so the column
// order and placeholder layout stay in lockstep across all three.
//
// Returns string which is the complete INSERT statement.
func (executor *Executor) historyInsertSQL() string {
	placeholder := executor.backend.placeholder
	return fmt.Sprintf(
		"INSERT INTO %s (version, name, checksum, applied_at, duration_ms, down_checksum, last_statement, dirty, %s, %s) "+
			"VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s)",
		executor.historyTable(),
		splitterVersionColumn,
		statementsHashColumn,
		placeholder(insertPlaceholderVersion),
		placeholder(insertPlaceholderName),
		placeholder(insertPlaceholderChecksum),
		placeholder(insertPlaceholderAppliedAt),
		placeholder(insertPlaceholderDurationMs),
		placeholder(insertPlaceholderDownChecksum),
		placeholder(insertPlaceholderLastStatement),
		placeholder(insertPlaceholderDirty),
		placeholder(insertPlaceholderSplitterVersion),
		placeholder(insertPlaceholderStatementsHash),
	)
}

// recordCompletedMigration inserts a finalised (dirty = false) migration history row for
// the append-only execution path. Unlike preRecordDirtyMigration it records the real
// duration and a clean dirty flag, and unlike clearDirty it does not rely on an in-place
// UPDATE, so it is safe on ClickHouse MergeTree.
//
// Takes migration (migrate.MigrationRecord) which holds the migration metadata.
// Takes appliedAt (time.Time) which is the timestamp to record.
// Takes durationMs (int64) which is the execution duration in milliseconds.
//
// Returns error when the INSERT statement fails.
func (executor *Executor) recordCompletedMigration(
	ctx context.Context,
	migration migrate.MigrationRecord,
	appliedAt time.Time,
	durationMs int64,
) error {
	_, insertError := executor.queryExecutor().ExecContext(ctx, executor.historyInsertSQL(),
		migration.Version, migration.Name, migration.Checksum, appliedAt.UTC(), durationMs,
		nullableDownChecksum(migration.DownChecksum), nil, false, nil, nil,
	)
	if insertError != nil {
		return fmt.Errorf("recording completed migration %d: %w", migration.Version, insertError)
	}
	return nil
}

// updateStatementProgress records the most recently completed statement of a migration,
// with the splitter version and the digest of the statements completed so far, providing
// a resumption point if the process crashes.
//
// Takes version (int64) which identifies the migration record.
// Takes lastStatement (int) which is the 0-based index of the last successful statement.
// Takes digest (statementDigest) which holds the digest of statements 0..lastStatement.
//
// Returns error when the progress UPDATE fails. Callers on the success-checkpoint path
// treat the failure as best-effort (a later checkpoint or the failure path writes it
// again), but the failure path must propagate it: a stale last_statement would let a
// crash-and-resume re-execute already-applied, possibly non-idempotent statements.
func (executor *Executor) updateStatementProgress(
	ctx context.Context,
	version int64,
	lastStatement int,
	digest statementDigest,
) error {
	placeholder := executor.backend.placeholder
	updateSQL := fmt.Sprintf(
		"UPDATE %s SET last_statement = %s, %s = %s, %s = %s WHERE version = %s",
		executor.historyTable(),
		placeholder(progressPlaceholderLastStatement),
		splitterVersionColumn,
		placeholder(progressPlaceholderSplitterVersion),
		statementsHashColumn,
		placeholder(progressPlaceholderStatementsHash),
		placeholder(progressPlaceholderVersion),
	)

	if _, executionError := executor.queryExecutor().ExecContext(ctx, updateSQL, lastStatement, splitterVersion, digest.sum(), version); executionError != nil {
		return fmt.Errorf("updating statement progress for migration %d: %w", version, executionError)
	}
	return nil
}

// flushProgressCheckpoint writes a best-effort progress checkpoint after a successful
// statement. A failed checkpoint is logged but not propagated, because a later checkpoint
// or the failure-path flush writes last_statement again.
//
// Takes version (int64) which identifies the migration record.
// Takes lastStatement (int) which is the 0-based index of the last successful statement.
// Takes digest (statementDigest) which holds the digest of statements 0..lastStatement.
//
// Returns bool which is true when the checkpoint was written.
func (executor *Executor) flushProgressCheckpoint(
	ctx context.Context,
	version int64,
	lastStatement int,
	digest statementDigest,
) bool {
	flushError := executor.updateStatementProgress(ctx, version, lastStatement, digest)
	if flushError == nil {
		return true
	}
	logger := logging.LoggerFrom(ctx)
	logger.WarnContext(ctx, "best-effort statement progress update failed",
		logging.ErrAttr(flushError),
		slog.Int64("version", version),
		slog.Int("last_statement", lastStatement),
	)
	return false
}

// historyRecordExists reports whether a history row already exists for the given
// migration version.
//
// Used before pre-recording a dirty migration or rewriting history so an existing row can
// be updated instead of inserted again. The caller's configured lock or external
// coordination protects the read and subsequent write from concurrent runs.
//
// Takes version (int64) which identifies the migration record.
//
// Returns bool which is true when a row for version already exists.
// Returns error when the existence query fails.
func (executor *Executor) historyRecordExists(ctx context.Context, version int64) (bool, error) {
	placeholder := executor.backend.placeholder
	selectSQL := fmt.Sprintf(
		"SELECT count(*) FROM %s WHERE version = %s",
		executor.historyTable(),
		placeholder(1),
	)

	var count int64
	if scanError := executor.queryExecutor().QueryRowContext(ctx, selectSQL, version).Scan(&count); scanError != nil {
		return false, fmt.Errorf("querying history record for migration %d: %w", version, scanError)
	}
	return count > 0, nil
}

// execStatements executes a migration's statements one at a time. Statements up to and
// including the record's SkipUpTo are skipped, allowing retry from where a partial
// application left off.
//
// Takes runner (execContextRunner) which executes the SQL.
// Takes run (migrationRun) which is the migration and its statements.
//
// Returns error when any individual statement fails, including which statement index
// failed.
func (*Executor) execStatements(ctx context.Context, runner execContextRunner, run migrationRun) error {
	for index := max(run.record.SkipUpTo+1, 0); index < len(run.statements); index++ {
		if cancelError := contextStopped(ctx); cancelError != nil {
			return fmt.Errorf("migration %d cancelled before statement %d: %w", run.record.Version, index+1, cancelError)
		}
		if _, executionError := runner.ExecContext(ctx, run.statements[index]); executionError != nil {
			return fmt.Errorf("statement %d/%d of migration %d: %w", index+1, len(run.statements), run.record.Version, executionError)
		}
	}
	return nil
}

// ExecutorOption customises an Executor or SeedExecutor at construction time.
type ExecutorOption func(*executorSettings)

// queryRunner is the common interface satisfied by both *sql.DB and *sql.Conn, allowing
// the executor to route operations through a pinned connection when an advisory lock is
// held.
type queryRunner interface {
	// ExecContext executes a query without returning rows.
	//
	// Takes query (string) which is the SQL statement to run.
	// Takes arguments (...any) which are the bind parameters for the statement.
	//
	// Returns sql.Result which reports the rows affected and any generated identifier.
	// Returns error when the statement cannot be run.
	ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error)

	// QueryContext executes a query that returns rows.
	//
	// Takes query (string) which is the SQL statement to run.
	// Takes arguments (...any) which are the bind parameters for the statement.
	//
	// Returns *sql.Rows which holds the result set, and which the caller must close.
	// Returns error when the query cannot be run.
	QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error)

	// QueryRowContext executes a query expected to return at most one row.
	//
	// Takes query (string) which is the SQL statement to run.
	// Takes arguments (...any) which are the bind parameters for the statement.
	//
	// Returns *sql.Row which carries the single row, or the deferred error raised by Scan.
	QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row

	// BeginTx starts a new transaction with the given options.
	//
	// Takes options (*sql.TxOptions) which sets the isolation level and read-only flag. May
	// be nil to accept the driver defaults.
	//
	// Returns *sql.Tx which is the started transaction.
	// Returns error when the transaction cannot be started.
	BeginTx(ctx context.Context, options *sql.TxOptions) (*sql.Tx, error)
}

// execContextRunner is the minimal interface needed to execute a history-table write. It
// is satisfied by *sql.DB, *sql.Conn, and *sql.Tx, so the history helpers can run either
// inside a transaction (the SQL-standard engines) or directly on the pinned
// connection/pool (the non-transactional engines such as ClickHouse).
type execContextRunner interface {
	// ExecContext executes a query without returning rows.
	//
	// Takes query (string) which is the SQL statement to run.
	// Takes arguments (...any) which are the bind parameters for the statement.
	//
	// Returns sql.Result which reports the rows affected and any generated identifier.
	// Returns error when the statement cannot be run.
	ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error)
}

// executorSettings holds the configuration ExecutorOptions set.
type executorSettings struct {
	// clock supplies durations and timestamps for history rows created without execution.
	clock observe.Clock

	// problems records each option given a value it cannot use; the value is not applied.
	problems []error

	// progressBatchSize is the number of statements applied between progress writes.
	progressBatchSize int
}

// newExecutorSettings applies options over the defaults.
//
// Takes options ([]ExecutorOption) which configure the executor.
//
// Returns executorSettings which holds the configuration.
func newExecutorSettings(options []ExecutorOption) executorSettings {
	settings := executorSettings{clock: observe.SystemClock(), problems: nil, progressBatchSize: defaultProgressUpdateBatchSize}
	for _, option := range options {
		option(&settings)
	}
	return settings
}

// invalid reports the options that were given a value they cannot use.
//
// Returns error which wraps migrate.ErrInvalidConfig naming each such option, or nil.
func (settings executorSettings) invalid() error {
	return errors.Join(settings.problems...)
}

// WithClock overrides the time source used to record migration and seed durations and
// timestamps for migration history rows created without execution.
//
// The default is the wall clock; tests inject a fixed clock to assert recorded durations
// deterministically.
//
// Takes source (observe.Clock) which is the time source.
//
// Returns ExecutorOption which sets the clock; a nil clock makes NewExecutor and
// NewSeedExecutor return an error wrapping migrate.ErrInvalidConfig.
func WithClock(source observe.Clock) ExecutorOption {
	return func(settings *executorSettings) {
		if source == nil {
			settings.problems = append(settings.problems, fmt.Errorf("%w: WithClock needs a clock, not nil", errInvalidExecutorOption))
			return
		}
		settings.clock = source
	}
}

// WithProgressBatchSize sets how often non-transactional migrations save statement
// progress.
//
// The default saves after every statement. Larger batches reduce history writes but can
// replay more completed statements after a crash. Seed executors do not use this option.
//
// Takes size (int) which is the number of statements between progress writes.
//
// Returns ExecutorOption which sets the batch size; a size below one makes NewExecutor
// and NewSeedExecutor return an error wrapping migrate.ErrInvalidConfig.
func WithProgressBatchSize(size int) ExecutorOption {
	return func(settings *executorSettings) {
		if size < 1 {
			settings.problems = append(settings.problems,
				fmt.Errorf("%w: WithProgressBatchSize needs at least one statement per batch, got %d", errInvalidExecutorOption, size))
			return
		}
		settings.progressBatchSize = size
	}
}

// renderAppliedVersionsSQL returns the SELECT statement used by AppliedVersions. The
// historyTable string has already passed validateIdentifier upstream, and a schema
// qualifying it is quoted, so the interpolation is safe.
//
// Takes historyTable (string) which is the history table, qualified or not.
// Takes modifier (string) which follows the table, such as " FINAL" so a
// ReplacingMergeTree history table collapses duplicate version rows at read time.
// Takes withProgress (bool) which adds the splitter_version and statements_hash columns.
//
// Returns string which is the SELECT statement for the applied-migrations query.
func renderAppliedVersionsSQL(historyTable, modifier string, withProgress bool) string {
	columns := "version, name, checksum, applied_at, duration_ms, down_checksum, last_statement, dirty"
	if withProgress {
		columns += ", " + splitterVersionColumn + ", " + statementsHashColumn
	}
	return fmt.Sprintf("SELECT %s FROM %s%s ORDER BY version", columns, historyTable, modifier)
}

// shouldFlushProgress reports whether the executor should write a progress checkpoint
// after completing the statement at currentIndex. We flush either when batchSize
// successful statements have accumulated since lastFlushedIndex or when currentIndex is
// the last statement in the migration.
//
// Takes currentIndex (int) which is the 0-based index of the statement just completed.
// Takes lastFlushedIndex (int) which is the index of the most recent flushed checkpoint.
// Takes batchSize (int) which is the configured number of statements per batch.
// Takes totalStatements (int) which is the migration's statement count.
//
// Returns bool which is true when a progress write should occur.
func shouldFlushProgress(currentIndex, lastFlushedIndex, batchSize, totalStatements int) bool {
	if currentIndex == totalStatements-1 {
		return true
	}
	return currentIndex-lastFlushedIndex >= batchSize
}

// nullableDownChecksum returns the down-migration checksum as a bind value, or nil when
// the migration has no down checksum so the column is stored as NULL.
//
// Takes downChecksum (string) which is the down-migration checksum (may be empty).
//
// Returns any which is the checksum string or nil.
func nullableDownChecksum(downChecksum string) any {
	if downChecksum == "" {
		return nil
	}
	return downChecksum
}

// rollbackUnlessDone rolls a transaction back as a deferred clean-up, joining a failed
// rollback into the function's result. A transaction already committed or rolled back
// reports sql.ErrTxDone, which is not a failure.
//
// Takes transaction (*sql.Tx) which is the transaction being released.
// Takes label (string) which names the transaction in the error.
// Takes err (*error) which is the function's result, joined with the rollback failure.
func rollbackUnlessDone(transaction *sql.Tx, label string, err *error) {
	rollbackError := transaction.Rollback()
	if rollbackError == nil || errors.Is(rollbackError, sql.ErrTxDone) {
		return
	}
	*err = errors.Join(*err, fmt.Errorf("rolling back %s: %w", label, rollbackError))
}

// checkpointContext returns the context a resume checkpoint is written on after a
// statement failed or the run was cancelled: detached from parent's cancellation, which
// has usually happened, and bounded by checkpointFlushTimeout with
// errCheckpointFlushTimeout as its cause.
//
// Returns context.Context which is the checkpoint context.
// Returns context.CancelFunc which the caller must invoke to release the timer.
func checkpointContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(context.WithoutCancel(parent), checkpointFlushTimeout, errCheckpointFlushTimeout)
}

// contextStopped reports why ctx has ended: its error, joined with the cause given to
// context.WithCancelCause or context.WithTimeoutCause when that differs, so the result
// matches both context.Canceled or context.DeadlineExceeded and the caller's cause.
//
// Returns error which is nil while ctx is live.
func contextStopped(ctx context.Context) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, err) {
		return fmt.Errorf("%w: %w", err, cause)
	}
	return err
}
