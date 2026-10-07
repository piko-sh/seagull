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

package migrate

import (
	"context"
)

// ExecutorPort handles a Migrator's database operations, history and locking.
//
// The Executor that NewExecutor returns is the database/sql implementation. Additional
// methods may be introduced in a minor release under docs/reference/compatibility.md.
//
// Treat a missing history table as empty. The Migrator creates it only while holding the
// lock. A lock must remain held until ReleaseLock, even after its acquisition context is
// cancelled.
//
// For resumable non-transactional migrations, record the last completed statement and the
// digest of the completed prefix. On resume, update the existing dirty history row and
// skip that prefix only if it still matches. Return ResumeMismatchError without running
// statements when it differs, and clear Dirty only after success. Executors without
// progress tracking, such as the append-only ClickHouse backend's, record only completed
// migrations.
type ExecutorPort interface {
	// EnsureMigrationTable creates the migration history table if it does not exist. The
	// Migrator calls it under the lock, before it reads the history to apply migrations.
	//
	// Returns error when the table cannot be created.
	EnsureMigrationTable(ctx context.Context) error

	// AcquireLock acquires the executor's migration lock to prevent concurrent runs.
	//
	// Returns error when the lock cannot be acquired.
	AcquireLock(ctx context.Context) error

	// TryAcquireLock attempts to acquire the migration lock without blocking.
	// Returns ErrLockNotAcquired immediately if the executor's lock is already held.
	//
	// Returns error when the lock cannot be acquired or is already held.
	TryAcquireLock(ctx context.Context) error

	// ReleaseLock releases the migration lock acquired by AcquireLock. The Migrator joins a
	// failure into the result of the run that held the lock.
	//
	// Returns error when the lock cannot be released.
	ReleaseLock(ctx context.Context) error

	// AppliedVersions returns all migration versions that have been applied, ordered by
	// version number ascending. It returns no rows, not an error, when the history table
	// does not exist yet.
	//
	// Returns []AppliedMigration which lists all applied migrations in version order.
	// Returns error when the history cannot be read.
	AppliedVersions(ctx context.Context) ([]AppliedMigration, error)

	// ExecuteMigration runs a single migration's SQL content and updates the history table
	// accordingly, honouring the resume contract described on ExecutorPort.
	//
	// Takes migration (MigrationRecord) which holds the version, name, SQL content, and
	// checksum, and for a retried dirty migration the recorded progress.
	// Takes direction (MigrationDirection) which indicates whether this is an up or down
	// migration.
	// Takes useTransaction (bool) which is false when the file carries an explicit
	// no_transaction directive. When it is true, the executor still runs the migration
	// outside a transaction if its engine cannot run DDL in one, if a statement cannot run
	// inside one by the RequiresNoTransaction rule of the engine's lexical rules, or if the
	// migration is a dirty retry.
	//
	// Returns error when the migration fails to execute, or a *ResumeMismatchError when a
	// dirty migration cannot be resumed safely.
	ExecuteMigration(
		ctx context.Context,
		migration MigrationRecord,
		direction MigrationDirection,
		useTransaction bool,
	) error
}

// HistoryEditorPort is implemented by an ExecutorPort that can rewrite the migration
// history without running a migration, which Migrator.Force, Migrator.PlanRepair and
// Migrator.Repair need; the Executor from NewExecutor is one.
//
// It is optional, so an ExecutorPort without it still runs migrations. The Migrator calls
// RecordApplied and RecordProgress only while it holds the lock, after
// EnsureMigrationTable.
type HistoryEditorPort interface {
	// SplitStatements divides a migration's content into the statements ExecuteMigration
	// runs, in order, so a person can say which of a dirty migration's statements ran.
	//
	// Takes content ([]byte) which is the migration's SQL.
	//
	// Returns []string which holds each statement's text.
	// Returns error when the content cannot be split.
	SplitStatements(content []byte) ([]string, error)

	// RecordApplied records a migration as applied and clean without running it. It inserts
	// the migration's history row, stamped with the current time, or replaces the name,
	// checksums, dirty flag and statement progress of an existing row, which keeps when it
	// was applied.
	//
	// Takes migration (MigrationRecord) which holds the version, name, checksums and content
	// to record.
	//
	// Returns error when the history cannot be written.
	RecordApplied(ctx context.Context, migration MigrationRecord) error

	// RecordProgress records a completed statement count without executing the migration.
	//
	// Zero removes the history row. Otherwise the migration remains dirty, with the
	// completed prefix recorded so the next Up resumes after it.
	//
	// Takes migration (MigrationRecord) which holds the version, name, checksums and
	// content.
	// Takes completed (int) which is how many leading statements ran; it must be at least
	// zero and less than the number of statements.
	//
	// Returns error when completed is out of range, the executor cannot record progress, or
	// the history cannot be written.
	RecordProgress(ctx context.Context, migration MigrationRecord, completed int) error
}

// SeedLockTrierPort is implemented by a SeedExecutorPort that can take its lock without
// waiting, which a Seeder built with WithNonBlockingLock needs; the SeedExecutor from
// NewSeedExecutor is one. It is optional, so a SeedExecutorPort without it still runs
// seeds.
type SeedLockTrierPort interface {
	// TryAcquireSeedLock takes the seed lock only when it is free.
	//
	// Returns error which wraps ErrLockNotAcquired when another process holds the lock, or
	// the failure otherwise.
	TryAcquireSeedLock(ctx context.Context) error
}

// SeedExecutorPort handles a Seeder's database operations and seed locking.
//
// The SeedExecutor that NewSeedExecutor returns is the database/sql implementation. Its
// lock is separate from the migration lock. Additional methods may be introduced in a
// minor release under the compatibility policy.
type SeedExecutorPort interface {
	// EnsureSeedTable creates the seed history table (sgl_seeds by default) if it does not
	// exist. The Seeder calls it under the seed lock; Status never calls it.
	//
	// Returns error when the table cannot be created.
	EnsureSeedTable(ctx context.Context) error

	// AcquireSeedLock acquires the executor's lock for seed runs.
	//
	// The lock must use a key distinct from the migration lock so the two surfaces serialise
	// independently. Implementations that require connection pinning hold a dedicated
	// connection until ReleaseSeedLock is called.
	//
	// Returns error when the lock cannot be acquired.
	AcquireSeedLock(ctx context.Context) error

	// ReleaseSeedLock releases the seed lock acquired by AcquireSeedLock. Safe to call when
	// no lock is held.
	//
	// Returns error when the lock cannot be released.
	ReleaseSeedLock(ctx context.Context) error

	// AppliedSeeds returns all seeds that have been applied, ordered by version ascending.
	// It returns no rows, not an error, when the seed history table does not exist yet.
	//
	// Returns []AppliedSeed which lists all applied seeds.
	// Returns error when the history cannot be read.
	AppliedSeeds(ctx context.Context) ([]AppliedSeed, error)

	// ExecuteSeed runs a single seed's SQL content and records it in the history table,
	// using a transaction when the executor supports transactional seeds.
	//
	// Takes seed (SeedRecord) which holds the version, name, SQL content, and checksum.
	//
	// Returns error when the seed fails to execute.
	ExecuteSeed(ctx context.Context, seed SeedRecord) error

	// ClearSeedHistory removes all records from the seed history table, allowing seeds to be
	// re-applied.
	//
	// Returns error when the history cannot be cleared.
	ClearSeedHistory(ctx context.Context) error
}
