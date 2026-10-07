---
title: Migrate API
description: "sdk/migrate applies migrations and seeds at run time: the migrator owns ordering and lifecycle, executors and SQL backends perform the database operations, and the lexical rules split their files."
section: Reference
subsection: Extensions
order: 1080
---

# Migrate API

The migrate area of the `sdk` kit, `seagull.sh/seagull/sdk/migrate`, applies versioned SQL
migrations and seed files to a database at run time, for a binary that migrates its own
database or a tool that drives migrations. The migrator owns version ordering and
lifecycle; executors perform database operations, and SQL backends describe
engine-specific statements and locking. `migrate.NewMigrator` takes a `*sql.DB` and a
backend and builds the `database/sql` executor itself, while `migrate.NewMigratorFor`
accepts any `ExecutorPort`. The package never links the analyser, a dialect or the code
generator, so a service that only migrates stays small; for that reason it also names the
lexical rule types a custom backend splits its files with.

Persisted names, checksums, lock identity and checkpoint semantics are compatibility contracts. See the [backend implementation guide](../how-to/migration-backend.md) for extension work and the [runtime reference](migration-runtime.md) for execution and recovery behaviour.

## Providers

Each built-in backend is a provider package under `sdk/migrate`, imported as
`seagull.sh/seagull/sdk/migrate/<sub-package>`. Its `New` returns the `migrate.Backend`
to pass to `migrate.NewMigrator` or `migrate.NewSeeder`, and takes the
`migrate.BackendOption` values every backend shares, such as `WithNamespace` and
`WithLock`.

| Sub-package | What it provides | When to use it |
|---|---|---|
| `migrate_backend_postgres` | `New`, with advisory locks; `NewPgBouncer`, with a lock table | PostgreSQL; `NewPgBouncer` behind PgBouncer in transaction mode |
| `migrate_backend_timescaledb` | `New`: PostgreSQL's backend, which also runs continuous-aggregate statements outside a transaction | TimescaleDB |
| `migrate_backend_cockroachdb` | `New`, with a durable lock table and no migration transactions | CockroachDB |
| `migrate_backend_mysql` | `New`, with `GET_LOCK` locks keyed within the current schema; `WithANSIQuotes` and `WithServerWideLockKey` | MySQL and MariaDB |
| `migrate_backend_sqlite` | `New`, with no lock | SQLite |
| `migrate_backend_clickhouse` | `New`, with no lock and no transactions | ClickHouse |
| `migrate_backend_duckdb` | `New`, with transactions and no lock | DuckDB |

An engine no provider covers takes `migrate.CustomBackend(spec, options...)` with a
`migrate.BackendSpec`, and a store `database/sql` cannot reach implements `ExecutorPort`
and `SeedExecutorPort`, as the [executor contract](#executor-contract) describes.

Source: [`sdk/migrate/facade.go`](../../sdk/migrate/facade.go).

## Exported package inventory

These declarations are the current `go doc -short` output of each package. Most are
aliases of the implementation under `internal/`, which a program never imports: run
`go doc` on a name for its doc comment, and see the [executor contract](#executor-contract)
for the methods of the ports.

[Package source](../../sdk/migrate) - [Go package reference](https://pkg.go.dev/seagull.sh/seagull/sdk/migrate)

### Migrator and seeder

`go doc -short` folds a constant or variable group into its first name, so the migration
directions sit in the `MigrationDirectionUp` group, and the sentinels (`ErrLockNotAcquired`,
`ErrLockTimeout`, `ErrLockLost`, `ErrDirtyRollback`, `ErrRepairOutOfDate`,
`ErrUnsupportedLock` and `ErrInvalidIdentifier`) in the `ErrLockNotAcquired` group. A
configuration that cannot be used, a file over its size limit, a panic recovered from a
hook or a callback and malformed SQL return errors matching the root package's
`seagull.ErrInvalidConfig`, `seagull.ErrFileTooLarge`, `seagull.ErrPanic` and
`seagull.ErrMalformedSQL`.

```go
const MigrationDirectionUp = migrate.MigrationDirectionUp ...
var ErrLockNotAcquired = migrate.ErrLockNotAcquired ...
type AppliedMigration = migrate.AppliedMigration
type AppliedSeed = migrate.AppliedSeed
type ChecksumMismatchError = migrate.ChecksumMismatchError
type DirtyMigration = migrate.DirtyMigration
type DirtyMigrationError = migrate.DirtyMigrationError
type DownChecksumMismatchError = migrate.DownChecksumMismatchError
type DuplicateMigrationVersionError = migrate.DuplicateMigrationVersionError
type DuplicateSeedVersionError = migrate.DuplicateSeedVersionError
type ExecutorPort = migrate.ExecutorPort
type HistoryEditorPort = migrate.HistoryEditorPort
type HookPanicError = migrate.HookPanicError
type LaterMigrationError = migrate.LaterMigrationError
type LockAcquisitionError = migrate.LockAcquisitionError
type MigrationDirection = migrate.MigrationDirection
type MigrationExecutionError = migrate.MigrationExecutionError
type MigrationHookContext = migrate.MigrationHookContext
type MigrationRecord = migrate.MigrationRecord
type MigrationRunHookContext = migrate.MigrationRunHookContext
type MigrationStatus = migrate.MigrationStatus
type Migrator = migrate.Migrator
    func NewMigrator(database *sql.DB, backend Backend, files fs.FS, directory string, ...) (*Migrator, error)
    func NewMigratorFor(executor ExecutorPort, files fs.FS, directory string, options ...Option) (*Migrator, error)
type MissingMigrationFileError = migrate.MissingMigrationFileError
type NoDownMigrationError = migrate.NoDownMigrationError
type Option = migrate.Option
    func WithAfterMigration(hook func(ctx context.Context, migration MigrationHookContext) error) Option
    func WithAfterRun(hook func(ctx context.Context, run MigrationRunHookContext, applied int) error) Option
    func WithBeforeMigration(hook func(ctx context.Context, migration MigrationHookContext) error) Option
    func WithBeforeRun(hook func(ctx context.Context, run MigrationRunHookContext) error) Option
    func WithDirectivePrefixes(canonical string, aliases ...string) Option
    func WithLockTimeout(timeout time.Duration) Option
    func WithLogger(logger *slog.Logger) Option
    func WithNonBlockingLock() Option
    func WithObserver(observer observe.ObserverPort) Option
type RepairOptions = migrate.RepairOptions
type RepairPlan = migrate.RepairPlan
type RepairResult = migrate.RepairResult
type ResumeMismatchError = migrate.ResumeMismatchError
type SeedExecutionError = migrate.SeedExecutionError
type SeedExecutorPort = migrate.SeedExecutorPort
type SeedLockTrierPort = migrate.SeedLockTrierPort
type SeedRecord = migrate.SeedRecord
type SeedStatus = migrate.SeedStatus
type Seeder = migrate.Seeder
    func NewSeeder(database *sql.DB, backend Backend, files fs.FS, directory string, ...) (*Seeder, error)
    func NewSeederFor(executor SeedExecutorPort, files fs.FS, directory string, options ...Option) (*Seeder, error)
type UnknownMigrationError = migrate.UnknownMigrationError
```

### Executors and backends

The `database/sql` executor and the backends it runs. The lock modes (`LockAdvisory`,
`LockTable` and `LockNone`) sit in the `LockAdvisory` group.

```go
const LockAdvisory = sqlexec.LockAdvisory ...
type AdvisoryLockSQL = sqlexec.AdvisoryLockSQL
type Backend = sqlexec.Backend
    func CustomBackend(spec BackendSpec, options ...BackendOption) Backend
type BackendOption = sqlexec.BackendOption
    func WithDriverMultiStatements(enabled bool) BackendOption
    func WithHistoryTable(name string) BackendOption
    func WithLock(mode LockMode) BackendOption
    func WithNamespace(namespace string) BackendOption
type BackendSpec = sqlexec.BackendSpec
type Executor = sqlexec.Executor
    func NewExecutor(database *sql.DB, backend Backend, options ...ExecutorOption) (*Executor, error)
type ExecutorOption = sqlexec.ExecutorOption
    func WithProgressBatchSize(size int) ExecutorOption
type LockMode = sqlexec.LockMode
type Names = sqlexec.Names
type TableLockSQL = sqlexec.TableLockSQL
```

### Lexical rules for a backend

Lexical rules describe how an engine writes SQL text: its quoting, comments, placeholders
and statement boundaries, semicolons inside literals and procedural bodies included.
Generation and the migration runtime split files under the same rules, so they agree on
where a statement begins and ends. A `BackendSpec` splits its files under `Rules`, and
`Rules.RequiresNoTransaction` names the statements a backend must run outside a
transaction. Each `Token` a scan reads carries a `TokenKind`.

Every built-in engine's rules are in its facts, `Facts.Lexical`, whose type `migrate.Rules`
names: `dialect_engine_postgres.New().Info().Lexical` gives PostgreSQL's, and the other
engine providers give theirs the same way. Rules also recognise client commands outside
SQL statements: `DelimiterCommand` handles MySQL `DELIMITER` lines, and `MetaCommands`
handles psql backslash commands at statement boundaries, including pg_dump's restrict
lines, which the PostgreSQL-family rules enable. Quoting modes must match the server:
`dialect_engine_mysql.New().Info().Lexical` reads double quotes as strings, and
`dialect_engine_mysql.New(dialect_engine_mysql.WithANSIQuotes(true)).Info().Lexical` as
identifiers, as `dialect_options.ansi_quotes` selects for the CLI.

The `ProceduralBlock` and `RequiresNoTransaction` rules are functions a set of rules
supplies. If one panics, the split stops and returns the panic as an error naming the
callback and the panic value, instead of crashing the caller, before any of the file runs.
The error carries no stack trace. Generation reports such an error as
[Q070](diagnostics.md#q070) rather than as a syntax error, and malformed SQL itself
matches `seagull.ErrMalformedSQL`.

`go doc -short` folds the token kinds (`KindWhitespace` and the rest), the escape modes
and the double-quote meanings into their first names.

```go
const KindWhitespace = sqllex.KindWhitespace ...
const EscapeDoubled = sqllex.EscapeDoubled ...
const DoubleQuoteIdentifier = sqllex.DoubleQuoteIdentifier ...
const DefaultTerminator = sqllex.DefaultTerminator
type CommentRules = sqllex.CommentRules
type DoubleQuoteMeaning = sqllex.DoubleQuoteMeaning
type EscapeMode = sqllex.EscapeMode
type ParameterRules = sqllex.ParameterRules
type ProceduralBlockFunc = sqllex.ProceduralBlockFunc
type QuoteRules = sqllex.QuoteRules
type Rules = sqllex.Rules
type StatementPredicate = sqllex.StatementPredicate
type Token = sqllex.Token
type TokenKind = sqllex.Kind
```

### Backend providers

#### migrate_backend_postgres

```go
func New(options ...migrate.BackendOption) migrate.Backend
func NewPgBouncer(options ...migrate.BackendOption) migrate.Backend
```

#### migrate_backend_timescaledb

```go
func New(options ...migrate.BackendOption) migrate.Backend
```

#### migrate_backend_cockroachdb

```go
func New(options ...migrate.BackendOption) migrate.Backend
```

#### migrate_backend_mysql

```go
func New(options ...migrate.BackendOption) migrate.Backend
func WithANSIQuotes(enabled bool) migrate.BackendOption
func WithServerWideLockKey() migrate.BackendOption
```

#### migrate_backend_sqlite

```go
func New(options ...migrate.BackendOption) migrate.Backend
```

#### migrate_backend_clickhouse

```go
func New(options ...migrate.BackendOption) migrate.Backend
```

#### migrate_backend_duckdb

```go
func New(options ...migrate.BackendOption) migrate.Backend
```

## Executor contract

`migrate.NewMigratorFor` and `migrate.NewSeederFor` drive any `ExecutorPort` and
`SeedExecutorPort`. The executors `migrate.NewMigrator` and `migrate.NewSeeder` build over
a `Backend` are one implementation, and `migrate.CustomBackend` builds a backend from a
description of an engine's SQL.
[Other databases and drivers](migration-runtime.md#other-databases-and-drivers) walks
through both routes. `go doc` shows each port as an alias of its declaration under
`internal/`; the second block of each is that declaration, written with the names
`sdk/migrate` exports.

```go
package migrate // import "seagull.sh/seagull/sdk/migrate"

type ExecutorPort = migrate.ExecutorPort
    ExecutorPort is the port a Migrator runs migrations through: the database
    operations, the history and the lock.

    Implement it for a store that database/sql cannot reach, or one
    with history tables of its own, and pass it to NewMigratorFor;
    the Executor that NewExecutor returns is the database/sql implementation,
    and docs/how-to/migration-backend.md walks through writing one.
    Additional methods may be introduced in a minor release under
    docs/reference/compatibility.md.

    Treat a missing history table as empty. The Migrator creates it only while
    holding the lock. A lock must remain held until ReleaseLock, even after its
    acquisition context is cancelled.

    For resumable non-transactional migrations, record the last completed
    statement and the digest of the completed prefix. On resume, update the
    existing dirty history row and skip that prefix only if it still matches.
    Return ResumeMismatchError without running statements when it differs, and
    clear Dirty only after success. Executors without progress tracking, such as
    the append-only ClickHouse backend's, record only completed migrations.
```

The declaration `ExecutorPort` aliases, written with the names `sdk/migrate` exports:

```go
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
```

An executor that also implements `HistoryEditorPort` supports `Force`, `PlanRepair` and
`Repair`; without it they return an error matching `seagull.ErrInvalidConfig` that says
the executor cannot rewrite the history.

```go
package migrate // import "seagull.sh/seagull/sdk/migrate"

type HistoryEditorPort = migrate.HistoryEditorPort
    HistoryEditorPort is the optional port an ExecutorPort also implements when
    it can rewrite the migration history without running a migration, which the
    Migrator's Force, PlanRepair and Repair need; the Executor from NewExecutor
    is one.

    An ExecutorPort without it still runs migrations. The Migrator calls
    RecordApplied and RecordProgress only while it holds the lock, after
    EnsureMigrationTable.
```

The declaration `HistoryEditorPort` aliases, written with the names `sdk/migrate` exports:

```go
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
```

A seeder drives a `SeedExecutorPort`. One that also implements `SeedLockTrierPort`
supports `WithNonBlockingLock` on a seeder.

```go
package migrate // import "seagull.sh/seagull/sdk/migrate"

type SeedExecutorPort = migrate.SeedExecutorPort
    SeedExecutorPort is the port a Seeder runs seeds through: the database
    operations, the seed history and the seed lock. Implement it for a store
    that database/sql cannot reach and pass it to NewSeederFor; NewSeeder builds
    the database/sql implementation for itself.

    Its lock is separate from the migration lock. Additional methods may be
    introduced in a minor release under the compatibility policy.
```

The declaration `SeedExecutorPort` aliases, written with the names `sdk/migrate` exports:

```go
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
```
