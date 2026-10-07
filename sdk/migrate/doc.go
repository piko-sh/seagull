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

// Package migrate applies versioned SQL migrations and seed files to a database at run
// time, for a binary that migrates its own database or a tool that drives migrations.
//
// A [Migrator] reads numbered up and down files from an [io/fs.FS], checks their
// checksums against the recorded history and applies or reverts them in version order. A
// [Seeder] applies seed files once each, and can clear their history and apply them
// again. The package never links the analyser, a dialect or the code generator, so a
// service that only migrates stays small; the lexical rule types a custom backend needs,
// such as [Rules] and [Token], are named here for that reason.
//
// # Two ways in
//
// Most programs reach their database through database/sql. Take the [Backend] for it from
// a migrate_backend_* provider and pass it, with the *sql.DB, to [NewMigrator] or
// [NewSeeder]. The backend carries the database's history and seed table DDL, its locks,
// its placeholders and how its files split into statements. [CustomBackend] builds one
// from a [BackendSpec] for an engine no provider covers, and [NewExecutor] builds the
// executor behind NewMigrator when an [ExecutorOption] such as [WithProgressBatchSize] is
// needed.
//
// A store that database/sql cannot reach, or one with history tables of its own,
// implements [ExecutorPort] or [SeedExecutorPort] and passes it to [NewMigratorFor] or
// [NewSeederFor]. [HistoryEditorPort] and [SeedLockTrierPort] are optional additions that
// recovery and non-blocking seed locks need.
//
// # Hooks
//
// [WithBeforeRun] and [WithAfterRun] register hooks around a whole run, and
// [WithBeforeMigration] and [WithAfterMigration] hooks around each migration, in either
// direction. A hook that returns an error stops the run, and one that panics stops it
// with a [HookPanicError]. [WithLogger] sets the logger each call logs through, and
// [WithObserver] the observer, the ObserverPort of sdk/observe, that receives a span for
// each migration and seed operation.
//
// # History and recovery
//
// Applied migrations and seeds are recorded, with their checksums, in history tables,
// sgl_migrations and sgl_seeds by default. [WithNamespace] changes the prefix of every
// persisted name and [WithHistoryTable] names the migration history table alone. The
// Migrator's Validate and Status compare the history with the files, and a file edited
// after it ran fails with a [ChecksumMismatchError].
//
// A migration that cannot run in a transaction has its progress recorded statement by
// statement, on every backend but ClickHouse, so one that fails part-way stays dirty and
// the next Up resumes it after its last completed statement. The Migrator's Force records
// a migration as applied without running it, and PlanRepair and Repair record an edited
// file's checksum or how many of a dirty migration's statements ran.
//
// # Locks
//
// A run takes the executor's lock before it reads the history, so two processes never
// migrate at once, and seeds take a lock of their own. [WithLock] picks [LockAdvisory],
// [LockTable] or [LockNone] where the backend offers more than one. [WithNonBlockingLock]
// fails at once with [ErrLockNotAcquired] instead of waiting, and [WithLockTimeout] gives
// up with [ErrLockTimeout].
//
// # Errors
//
// Sentinels such as [ErrDirtyRollback] and [ErrLockLost] match with errors.Is, and typed
// errors such as [ChecksumMismatchError] and [DirtyMigrationError] with errors.As. A
// configuration that cannot be used, a file over its size limit, a panic recovered from a
// hook and malformed SQL match the root seagull package's ErrInvalidConfig,
// ErrFileTooLarge, ErrPanic and ErrMalformedSQL, so a program that uses both matches one
// value.
//
// The guides in docs/how-to cover each topic in depth: startup-migrations.md,
// seeds-namespaces-hooks.md, configure-locks.md, recover-migration.md,
// migration-observability.md and migration-backend.md. The full contract is in
// docs/reference/migration-runtime.md.
//
// # Providers
//
//   - migrate_backend_postgres: PostgreSQL, with NewPgBouncer for PgBouncer in
//     transaction mode
//   - migrate_backend_timescaledb: TimescaleDB
//   - migrate_backend_cockroachdb: CockroachDB
//   - migrate_backend_mysql: MySQL and MariaDB
//   - migrate_backend_sqlite: SQLite
//   - migrate_backend_clickhouse: ClickHouse
//   - migrate_backend_duckdb: DuckDB
//
// # Usage
//
//	//go:embed migrations
//	var files embed.FS
//
//	backend := migrate_backend_postgres.New()
//	migrator, err := migrate.NewMigrator(database, backend, files, "migrations")
//	if err != nil {
//	    return err
//	}
//	applied, err := migrator.Up(ctx)
package migrate
