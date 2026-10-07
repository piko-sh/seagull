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
	"fmt"
	"sync"
	"time"

	"seagull.sh/seagull/internal/migrate"
	"seagull.sh/seagull/internal/observe"
)

const (
	// seedPlaceholderVersion holds the 1-based index for the version column.
	seedPlaceholderVersion = 1

	// seedPlaceholderName holds the 1-based index for the name column.
	seedPlaceholderName = 2

	// seedPlaceholderChecksum holds the 1-based index for the checksum column.
	seedPlaceholderChecksum = 3

	// seedPlaceholderDurationMs holds the 1-based index for the duration_ms column.
	seedPlaceholderDurationMs = 4
)

// SeedExecutor implements migrate.SeedExecutorPort over database/sql for every Backend.
// It records applied seeds and runs each in a transaction where the engine has them.
//
// A SeedExecutor is safe for concurrent use: its state is guarded by a mutex, and a
// second AcquireSeedLock in the same process waits until ReleaseSeedLock.
type SeedExecutor struct {
	// database holds the underlying database connection pool.
	database *sql.DB

	// clock is the time source used to stamp seed durations; never nil after
	// NewSeedExecutor, which defaults it to observe.SystemClock().
	clock observe.Clock

	// slot admits one seed lock holder at a time within the process.
	slot holderSlot

	// held is what the held seed lock and its run pinned, or nil; guarded by mutex.
	held *heldLock

	// backend holds the dialect-specific SQL and behaviour.
	backend Backend

	// mutex guards held.
	mutex sync.Mutex
}

var (
	_ migrate.SeedExecutorPort = (*SeedExecutor)(nil)

	_ migrate.SeedLockTrierPort = (*SeedExecutor)(nil)
)

// NewSeedExecutor creates the migrate.SeedExecutorPort for a database/sql database, for
// use with migrate.NewSeeder.
//
// Takes database (*sql.DB) which is the database connection.
// Takes backend (Backend) which provides the database's SQL and locking.
// Takes options (...ExecutorOption) which set the clock.
//
// Returns *SeedExecutor which is ready to execute seeds.
// Returns error which is the backend's recorded option error, or wraps
// migrate.ErrInvalidConfig for a backend no constructor built, a nil database or an
// option given a value it cannot use.
func NewSeedExecutor(database *sql.DB, backend Backend, options ...ExecutorOption) (*SeedExecutor, error) {
	if err := backend.usable(database); err != nil {
		return nil, err
	}
	if err := newExecutorSettings(options).invalid(); err != nil {
		return nil, err
	}
	return newSeedExecutor(database, backend, options...), nil
}

// newSeedExecutor assembles a SeedExecutor without validating the backend.
//
// Takes database (*sql.DB) which is the database connection.
// Takes backend (Backend) which provides the database's SQL and locking.
// Takes options (...ExecutorOption) which set the clock.
//
// Returns *SeedExecutor which is ready to execute seeds.
func newSeedExecutor(database *sql.DB, backend Backend, options ...ExecutorOption) *SeedExecutor {
	return &SeedExecutor{
		database: database,
		backend:  backend,
		clock:    newExecutorSettings(options).clock,
		slot:     newHolderSlot(),
		held:     nil,
		mutex:    sync.Mutex{},
	}
}

// EnsureSeedTable creates the seed history table ("<ns>_seeds", default "sgl_seeds") if
// it does not exist.
//
// Returns error when the table cannot be created.
func (e *SeedExecutor) EnsureSeedTable(ctx context.Context) error {
	table := e.seedTable()
	_, err := e.runner().ExecContext(ctx, e.backend.engine.createSeedTable(table))
	if err != nil {
		return fmt.Errorf("creating %s table: %w", table, err)
	}
	return nil
}

// AppliedSeeds returns all seeds that have been applied, ordered by version ascending. A
// database without a seed history table has applied none.
//
// Returns []migrate.AppliedSeed which lists all applied seeds.
// Returns error when the history cannot be read.
func (e *SeedExecutor) AppliedSeeds(ctx context.Context) ([]migrate.AppliedSeed, error) {
	runner := e.runner()
	table := e.seedTable()
	return readUnlessMissing(ctx, runner, e.backend, table, func() ([]migrate.AppliedSeed, error) {
		return e.readAppliedSeeds(ctx, runner, table)
	})
}

// ExecuteSeed runs a single seed's SQL content and records it in the history table.
//
// The INSERT into the seed table is rendered through the dialect's insertSeedSQLFunc,
// which yields an idempotent statement (e.g. "ON CONFLICT (version) DO NOTHING" on
// PostgreSQL/SQLite, "INSERT IGNORE" on MySQL). Where the engine enforces version
// uniqueness, a duplicate history insert is ignored rather than failing with a
// primary-key violation. This does not prevent the seed SQL from running twice; callers
// must hold the seed lock or coordinate runs externally when the backend has no lock.
//
// On engines where a seed and its record cannot commit together, such as ClickHouse, the
// seed SQL and the record insert run without a transaction. A failure can leave applied
// statements without a completed history record.
//
// Under a table lock the lock's transaction is first confirmed to be still open, so a run
// whose lock the server ended stops with ErrLockLost before the seed runs.
//
// Takes seed (migrate.SeedRecord) which holds the seed SQL and metadata.
//
// Returns error when the lock was lost or the seed fails to execute, joined with the
// failure to roll the seed transaction back when that fails too.
func (e *SeedExecutor) ExecuteSeed(ctx context.Context, seed migrate.SeedRecord) (err error) {
	if lockError := confirmLockHeld(ctx, e.lockTransaction()); lockError != nil {
		return fmt.Errorf("before seed %d (%s): %w", seed.Version, seed.Name, lockError)
	}
	start := e.clock.Now()

	runner := e.runner()
	if !e.backend.engine.transactionalSeeds {
		return e.applySeed(ctx, runner, seed, start)
	}

	transaction, beginError := runner.BeginTx(ctx, nil)
	if beginError != nil {
		return fmt.Errorf("beginning seed transaction: %w", beginError)
	}
	defer rollbackUnlessDone(transaction, "seed transaction", &err)

	if applyError := e.applySeed(ctx, transaction, seed, start); applyError != nil {
		return applyError
	}

	if commitError := transaction.Commit(); commitError != nil {
		return fmt.Errorf("committing seed %d (%s): %w", seed.Version, seed.Name, commitError)
	}
	return nil
}

// AcquireSeedLock takes the backend's seed lock, keyed apart from the migration lock so
// seed and migration runs serialise independently, waiting while another goroutine in the
// process or another process holds it.
//
// Returns error when the lock cannot be acquired or ctx ends first.
func (e *SeedExecutor) AcquireSeedLock(ctx context.Context) error {
	if slotError := e.slot.claim(ctx, true); slotError != nil {
		return slotError
	}
	handle, lockError := e.backend.seedLock.Acquire(ctx, e.database)
	if lockError != nil {
		e.slot.free()
		return lockError
	}
	return e.hold(ctx, handle)
}

// TryAcquireSeedLock takes the seed lock only when it is free, for a Seeder built with
// migrate.WithNonBlockingLock. The executor serialises concurrent acquisition attempts.
//
// Returns error which wraps migrate.ErrLockNotAcquired when another goroutine or process
// holds the lock, or the failure otherwise.
func (e *SeedExecutor) TryAcquireSeedLock(ctx context.Context) error {
	if slotError := e.slot.claim(ctx, false); slotError != nil {
		return slotError
	}
	handle, lockError := e.backend.seedLock.TryAcquire(ctx, e.database)
	if lockError != nil {
		e.slot.free()
		return lockError
	}
	return e.hold(ctx, handle)
}

// ReleaseSeedLock releases the seed lock previously acquired by AcquireSeedLock. It does
// nothing when no lock is held.
//
// Returns error when the lock cannot be released or the seed run's own connection cannot
// be closed. The caller names the step, as the Seeder does with "releasing seed lock".
//
// Concurrency: detaches the held lock under the seed executor mutex before releasing it.
func (e *SeedExecutor) ReleaseSeedLock(ctx context.Context) error {
	e.mutex.Lock()
	held := e.held
	e.held = nil
	e.mutex.Unlock()
	if held == nil {
		return nil
	}
	defer e.slot.free()
	handle, workCloseError := finishRun(e.backend.engine, held)
	return joinWorkCloseError("seed", e.backend.seedLock.Release(ctx, handle), workCloseError)
}

// ClearSeedHistory removes all records from the seed history table.
//
// Returns error when the history cannot be cleared.
func (e *SeedExecutor) ClearSeedHistory(ctx context.Context) error {
	_, err := e.runner().ExecContext(ctx, e.clearSeedHistorySQL())
	if err != nil {
		return fmt.Errorf("clearing seed history: %w", err)
	}
	return nil
}

// hold prepares the seed run once the seed lock is taken and records what it holds.
//
// On an engine whose sessions keep a current schema the run works on one session, the
// lock's own or a connection pinned for it, and the seed table is qualified by the schema
// current there when the run began; on other engines seeds run on the connection pool.
//
// Takes handle (lockHandle) which is what the seed lock pinned.
//
// Returns error when the run's session cannot be prepared; the lock is released and the
// slot freed then.
//
// Concurrency: the mutex protects the held state.
func (e *SeedExecutor) hold(ctx context.Context, handle lockHandle) error {
	engine := e.backend.engine
	held, pinError := prepareRun(ctx, e.database, engine, handle, engine.keepsSessionSchema() && handle.transaction != nil, "seed")
	if pinError != nil {
		releaseError := e.backend.seedLock.Release(ctx, handle)
		e.slot.free()
		return joinLockReleaseErrors("releasing seed lock after preparing the seed session failed", releaseError, pinError)
	}
	e.mutex.Lock()
	e.held = held
	e.mutex.Unlock()
	return nil
}

// runner returns the connection a seed run works on: on an engine whose sessions keep a
// current schema, the session the held run pinned; otherwise, and outside a run, the
// connection pool.
//
// Returns queryRunner which is the pinned session or the pool.
//
// Concurrency: reads the held run under the seed executor mutex.
func (e *SeedExecutor) runner() queryRunner {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.held == nil || !e.backend.engine.keepsSessionSchema() {
		return e.database
	}
	if session := e.held.session(); session != nil {
		return session
	}
	return e.database
}

// seedTable returns the seed table as the seed executor's SQL names it: qualified by the
// schema the run's session started in while a run that read one holds the lock, and as
// the backend names it otherwise.
//
// Returns string which is the table name to interpolate.
//
// Concurrency: reads the held run under the seed executor mutex.
func (e *SeedExecutor) seedTable() string {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return qualifiedTable(e.backend.engine, e.held, e.backend.seedTable())
}

// lockTransaction returns the open transaction that holds the seed run's lock, or nil
// when no lock is held or the lock is held without a transaction.
//
// Returns *sql.Tx which is the lock transaction, or nil.
//
// Concurrency: reads the held lock under the seed executor mutex.
func (e *SeedExecutor) lockTransaction() *sql.Tx {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.held.lockTransaction()
}

// readAppliedSeeds reads the seed history table.
//
// Takes runner (queryRunner) which is the connection to read on.
// Takes table (string) which is the seed table as the run names it.
//
// Returns []migrate.AppliedSeed which lists all applied seeds.
// Returns error when the history cannot be read, joined with the failure to close the
// rows when that fails too.
func (*SeedExecutor) readAppliedSeeds(ctx context.Context, runner queryRunner, table string) (seeds []migrate.AppliedSeed, err error) {
	query := fmt.Sprintf(
		"SELECT version, name, checksum, applied_at, duration_ms FROM %s ORDER BY version",
		table,
	)
	rows, queryError := runner.QueryContext(ctx, query)
	if queryError != nil {
		return nil, fmt.Errorf("querying applied seeds: %w", queryError)
	}
	defer closeRows(rows, &err)

	var fallbacks appliedAtFallbacks
	for rows.Next() {
		var seed migrate.AppliedSeed
		var appliedAt any
		if scanError := rows.Scan(&seed.Version, &seed.Name, &seed.Checksum, &appliedAt, &seed.DurationMs); scanError != nil {
			return nil, fmt.Errorf("scanning applied seed: %w", scanError)
		}
		seed.AppliedAt = fallbacks.parse(appliedAt, seed.Version)
		seeds = append(seeds, seed)
	}

	if rowsError := rows.Err(); rowsError != nil {
		return nil, fmt.Errorf("iterating applied seeds: %w", rowsError)
	}
	fallbacks.report(ctx, table)
	return seeds, nil
}

// clearSeedHistorySQL returns the statement that empties the seed history table,
// honouring the engine's own statement when it has one.
//
// Returns string which is the statement to run.
func (e *SeedExecutor) clearSeedHistorySQL() string {
	table := e.seedTable()
	if e.backend.engine.clearSeedHistory != nil {
		return e.backend.engine.clearSeedHistory(table)
	}
	return "DELETE FROM " + table
}

// applySeed runs the seed SQL and records its history row on the given runner, which is
// the transaction on transactional engines or the pool on non-transactional ones.
//
// Takes runner (execContextRunner) which executes the SQL.
// Takes seed (migrate.SeedRecord) which holds the seed SQL and metadata.
// Takes start (time.Time) which marks when execution began, for the recorded duration.
//
// Returns error when the seed SQL or the history insert fails.
func (e *SeedExecutor) applySeed(ctx context.Context, runner execContextRunner, seed migrate.SeedRecord, start time.Time) error {
	if executionError := e.executeSeedSQL(ctx, runner, seed.Content); executionError != nil {
		return executionError
	}

	durationMs := e.clock.Now().Sub(start).Milliseconds()

	insertSQL := e.renderSeedInsertSQL()

	if _, insertError := runner.ExecContext(ctx, insertSQL,
		seed.Version, seed.Name, seed.Checksum, durationMs,
	); insertError != nil {
		return fmt.Errorf("recording seed %d (%s): %w", seed.Version, seed.Name, insertError)
	}

	return nil
}

// renderSeedInsertSQL builds the engine's idempotent INSERT for the seed history table.
//
// Returns string which is the rendered INSERT statement.
func (e *SeedExecutor) renderSeedInsertSQL() string {
	return e.backend.engine.insertSeed(
		e.seedTable(),
		e.backend.placeholder(seedPlaceholderVersion),
		e.backend.placeholder(seedPlaceholderName),
		e.backend.placeholder(seedPlaceholderChecksum),
		e.backend.placeholder(seedPlaceholderDurationMs),
	)
}

// executeSeedSQL executes the seed SQL content against the given runner.
//
// Engines whose driver runs several statements in one call take the whole file; the
// others split it and run each statement separately.
//
// Takes runner (execContextRunner) which executes the SQL.
// Takes content ([]byte) which holds the raw seed SQL.
//
// Returns error when any statement fails to execute.
func (e *SeedExecutor) executeSeedSQL(ctx context.Context, runner execContextRunner, content []byte) error {
	if e.backend.engine.multiStatementSeeds {
		if _, err := runner.ExecContext(ctx, string(content)); err != nil {
			return fmt.Errorf("executing seed SQL: %w", err)
		}
		return nil
	}

	statements, splitError := e.backend.engine.split(ctx, string(content))
	if splitError != nil {
		return fmt.Errorf("splitting seed statements: %w", splitError)
	}

	for index, statement := range statements {
		if cancelError := contextStopped(ctx); cancelError != nil {
			return fmt.Errorf("seed cancelled before statement %d: %w", index+1, cancelError)
		}
		if _, err := runner.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("executing seed statement %d/%d: %w", index+1, len(statements), err)
		}
	}

	return nil
}
