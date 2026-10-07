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
	"database/sql"
	"io/fs"
	"log/slog"
	"time"

	"seagull.sh/seagull/internal/migrate"
	"seagull.sh/seagull/internal/migrate/sqlexec"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// MigrationDirectionUp marks a migration file that applies its version.
	MigrationDirectionUp = migrate.MigrationDirectionUp

	// MigrationDirectionDown marks a migration file that reverts its version.
	MigrationDirectionDown = migrate.MigrationDirectionDown

	// LockAdvisory holds a session-level advisory lock for the run: pg_advisory_lock on
	// PostgreSQL and GET_LOCK on MySQL. It is the default of migrate_backend_postgres.New,
	// migrate_backend_timescaledb.New and migrate_backend_mysql.New.
	LockAdvisory = sqlexec.LockAdvisory

	// LockTable holds a row lock on a single-row lock table, taken with SELECT ... FOR
	// UPDATE in a transaction that stays open for the run.
	//
	// This is the default for migrate_backend_postgres.NewPgBouncer and
	// migrate_backend_cockroachdb.New. A transaction-mode pooler such as PgBouncer cannot
	// preserve session advisory locks across transactions.
	LockTable = sqlexec.LockTable

	// LockNone takes no lock, so concurrent runs must be coordinated outside the migrator.
	// It is the only mode the SQLite, ClickHouse and DuckDB backends offer.
	LockNone = sqlexec.LockNone

	// DefaultTerminator is the statement terminator used when Rules.Terminator is empty.
	DefaultTerminator = sqllex.DefaultTerminator

	// KindWhitespace is a run of whitespace, or the byte order mark at the start of the
	// input.
	KindWhitespace = sqllex.KindWhitespace

	// KindComment is a line or block comment, including its delimiters. A line comment stops
	// before the line break that ends it.
	KindComment = sqllex.KindComment

	// KindWord is an unquoted identifier or keyword.
	KindWord = sqllex.KindWord

	// KindQuotedIdentifier is a delimited identifier, including its quotes.
	KindQuotedIdentifier = sqllex.KindQuotedIdentifier

	// KindString is a string literal, including its quotes, prefix and dollar tags.
	KindString = sqllex.KindString

	// KindNumber is a numeric literal.
	KindNumber = sqllex.KindNumber

	// KindParameter is a bind-parameter placeholder, including its sigil or braces.
	KindParameter = sqllex.KindParameter

	// KindPunctuation is any other character, such as an operator, a parenthesis or the
	// terminator. Each punctuation token is one character, except that a doubled named
	// parameter sigil, such as ::, is one token.
	KindPunctuation = sqllex.KindPunctuation

	// KindExecutableComment is a block comment the server executes, such as MySQL's /*!40101
	// ... */, including its delimiters.
	//
	// It is significant, not noise.
	KindExecutableComment = sqllex.KindExecutableComment

	// EscapeDoubled escapes the closing quote by doubling it, as standard SQL does. It is
	// the zero value.
	//
	// For example:
	//
	// 	'it''s'
	EscapeDoubled = sqllex.EscapeDoubled

	// EscapeBackslash makes a backslash escape the byte after it, so \' does not close the
	// token. A doubled quote closes the token and opens a new one.
	EscapeBackslash = sqllex.EscapeBackslash

	// EscapeDoubledOrBackslash accepts both forms: a doubled quote and a backslash escape.
	EscapeDoubledOrBackslash = sqllex.EscapeDoubledOrBackslash

	// DoubleQuoteIdentifier reads "..." as a quoted identifier, as standard SQL does. It is
	// the zero value.
	DoubleQuoteIdentifier = sqllex.DoubleQuoteIdentifier

	// DoubleQuoteString reads "..." as a string literal.
	DoubleQuoteString = sqllex.DoubleQuoteString
)

var (
	// ErrLockNotAcquired is returned when a non-blocking lock attempt fails because another
	// process already holds the migration lock.
	ErrLockNotAcquired = migrate.ErrLockNotAcquired

	// ErrLockTimeout is the cause of a lock acquisition that WithLockTimeout cut short: the
	// lock was still held elsewhere when the timeout passed. The acquisition error wraps it.
	ErrLockTimeout = migrate.ErrLockTimeout

	// ErrLockLost is returned when the transaction that holds a table lock has ended while a
	// run still needs the lock, as when the server ended its idle session. Another process
	// may hold the lock by then, so the run stops before its next migration or seed.
	ErrLockLost = sqlexec.ErrLockLost

	// ErrDirtyRollback is returned by the Migrator's Down and DownTo while the history holds
	// a dirty migration, before anything runs.
	//
	// The error also wraps a *DirtyMigrationError naming the migration. Finish it with Up,
	// or record which of its statements ran with PlanRepair and Repair, then roll back.
	ErrDirtyRollback = migrate.ErrDirtyRollback

	// ErrRepairOutOfDate is returned by the Migrator's Repair when the history no longer
	// matches what the caller resolved, such as a dirty migration that another run has
	// resumed since the repair was planned. Plan the repair again.
	ErrRepairOutOfDate = migrate.ErrRepairOutOfDate

	// ErrUnsupportedLock is returned when WithLock selects a lock mode the backend's
	// database cannot provide, such as LockAdvisory on SQLite. It wraps
	// seagull.ErrInvalidConfig.
	ErrUnsupportedLock = sqlexec.ErrUnsupportedLock

	// ErrInvalidIdentifier is wrapped by constructor errors when WithHistoryTable or
	// WithNamespace supplies an unsafe SQL identifier, or two persisted tables would share a
	// name.
	ErrInvalidIdentifier = sqlexec.ErrInvalidIdentifier
)

// Running migrations and seeds, and what a run reports.
type (
	// Migrator applies, rolls back and inspects versioned migrations through an
	// ExecutorPort. Build one with NewMigrator, or with NewMigratorFor over an ExecutorPort
	// of your own.
	//
	// It is safe for concurrent use: its calls run one at a time in the process, and the
	// coordination across processes depends on the ExecutorPort's lock implementation.
	Migrator = migrate.Migrator

	// Seeder applies, inspects and re-applies seed files through a SeedExecutorPort. Build
	// one with NewSeeder, or with NewSeederFor over a SeedExecutorPort of your own.
	//
	// Seeds are forward-only SQL files intended to run after migrations. Applied seeds are
	// recorded in a history table and skipped on subsequent Apply calls.
	//
	// A Seeder is safe for concurrent use: its calls run one at a time in the process, and
	// coordination across processes depends on the SeedExecutorPort's lock implementation.
	Seeder = migrate.Seeder

	// Option configures a Migrator or a Seeder. Options that concern only migrations, such
	// as the hooks, are ignored by a Seeder.
	Option = migrate.Option

	// MigrationStatus combines a migration file with its applied state, as the Migrator's
	// Status reports it.
	MigrationStatus = migrate.MigrationStatus

	// SeedStatus combines a seed file with its applied state, as the Seeder's Status reports
	// it.
	SeedStatus = migrate.SeedStatus

	// RepairPlan describes what the Migrator's Repair would change, as its PlanRepair read
	// it: the applied migrations whose files no longer match their recorded checksums, and
	// the dirty migrations that wait for someone to say which of their statements ran.
	// Repair applies a plan only while the history still matches it.
	RepairPlan = migrate.RepairPlan

	// DirtyMigration is a migration whose interrupted run left it dirty.
	DirtyMigration = migrate.DirtyMigration

	// RepairOptions says which repair to apply: the reviewed RepairPlan and, for each dirty
	// migration to resolve, how many of its statements ran.
	RepairOptions = migrate.RepairOptions

	// RepairResult reports what the Migrator's Repair changed.
	RepairResult = migrate.RepairResult

	// MigrationHookContext is what a hook registered with WithBeforeMigration or
	// WithAfterMigration is told about the migration it runs for: its name, version and
	// direction.
	MigrationHookContext = migrate.MigrationHookContext

	// MigrationRunHookContext is what a hook registered with WithBeforeRun or WithAfterRun
	// is told about the run: the pending versions, how many there are and the direction.
	MigrationRunHookContext = migrate.MigrationRunHookContext
)

// NewMigrator creates a Migrator over a database/sql database, the usual way to run
// migrations from a binary.
//
// Takes database (*sql.DB) which is the database to migrate.
// Takes backend (Backend) which is the database's backend, such as
// migrate_backend_postgres.New().
// Takes files (fs.FS) which holds the migration files, such as an embed.FS.
// Takes directory (string) which is the slash-separated directory of the migration files
// within files.
// Takes options (...Option) which configure locking, hooks, prefixes and logging.
//
// Returns *Migrator which is ready to run.
// Returns error when the backend's options were invalid, the database is nil, or the
// migrator cannot be created.
func NewMigrator(database *sql.DB, backend Backend, files fs.FS, directory string, options ...Option) (*Migrator, error) {
	return sqlexec.NewMigrator(database, backend, files, directory, options...)
}

// NewMigratorFor creates a Migrator over an ExecutorPort, for databases reached through
// something other than database/sql or with history tables of their own. For
// database/sql, use NewMigrator.
//
// Takes executor (ExecutorPort) which runs the migrations against the database.
// Takes files (fs.FS) which holds the migration files, such as an embed.FS.
// Takes directory (string) which is the slash-separated directory of the migration files
// within files.
// Takes options (...Option) which configure locking, hooks, prefixes and logging.
//
// Returns *Migrator which is ready to run.
// Returns error which wraps seagull.ErrInvalidConfig when the executor or files are nil,
// the directory is not a valid path, or an option is invalid.
func NewMigratorFor(executor ExecutorPort, files fs.FS, directory string, options ...Option) (*Migrator, error) {
	return migrate.New(executor, files, directory, options...)
}

// NewSeeder creates a Seeder over a database/sql database, the usual way to apply seed
// files from a binary.
//
// Takes database (*sql.DB) which is the database to seed.
// Takes backend (Backend) which is the database's backend, such as
// migrate_backend_postgres.New().
// Takes files (fs.FS) which holds the seed files.
// Takes directory (string) which is the slash-separated directory of the seed files
// within files.
// Takes options (...Option) which configure locking and logging; migration-only options
// are ignored.
//
// Returns *Seeder which is ready to run.
// Returns error when the backend's options were invalid, the database is nil, or the
// seeder cannot be created.
func NewSeeder(database *sql.DB, backend Backend, files fs.FS, directory string, options ...Option) (*Seeder, error) {
	return sqlexec.NewSeeder(database, backend, files, directory, options...)
}

// NewSeederFor creates a Seeder over a SeedExecutorPort, for databases reached through
// something other than database/sql. For database/sql, use NewSeeder.
//
// Takes executor (SeedExecutorPort) which runs the seeds against the database.
// Takes files (fs.FS) which holds the seed files, such as an embed.FS.
// Takes directory (string) which is the slash-separated directory of the seed files
// within files.
// Takes options (...Option) which configure locking and logging; migration-only options
// are ignored.
//
// Returns *Seeder which is ready to run.
// Returns error which wraps seagull.ErrInvalidConfig when the executor or files are nil,
// the directory is not a valid path, an option is invalid, or WithNonBlockingLock is set
// for an executor that does not implement SeedLockTrierPort.
func NewSeederFor(executor SeedExecutorPort, files fs.FS, directory string, options ...Option) (*Seeder, error) {
	return migrate.NewSeeder(executor, files, directory, options...)
}

// MigrationDirection identifies whether a migration file applies (up) or reverts (down)
// its version.
type MigrationDirection = migrate.MigrationDirection

// The run-time migration SQL of a database engine.
type (
	// Backend is the run-time migration SQL for one database: the history and seed table
	// DDL, the locks, placeholder syntax and statement splitting, with the persisted names
	// they use. Take one from a migrate_backend_* provider, such as
	// migrate_backend_postgres.New, or build one with CustomBackend; the zero value is not
	// usable.
	//
	// A Backend is an immutable value and its locks keep no per-acquisition state, so one
	// Backend can be shared by any number of executors, concurrently.
	Backend = sqlexec.Backend

	// BackendOption configures a Backend as a migrate_backend_* provider's New or
	// CustomBackend builds it. The options every backend accepts are in migrate;
	// migrate_backend_mysql adds the ones only MySQL and MariaDB read.
	BackendOption = sqlexec.BackendOption

	// BackendSpec describes the run-time migration SQL of a database engine that has no
	// built-in Backend, so a third-party engine can drive NewMigrator, NewSeeder and
	// NewExecutor through CustomBackend. docs/how-to/migration-backend.md shows one filled
	// in.
	//
	// Table names passed to the renderers have passed the same safe-identifier check as the
	// built-in backends use, so a renderer can interpolate them directly.
	//
	// CustomBackend calls each renderer once, for the names and bind-parameter markers the
	// backend uses, and the executors reuse what it returned; a renderer that panics there
	// makes the executor constructors return an error wrapping seagull.ErrInvalidConfig.
	// Split and the Rules callbacks run on each file instead, and a panic in one of them
	// fails that call with an error rather than the program.
	BackendSpec = sqlexec.BackendSpec

	// Names lists every persisted object name a Backend uses, as Backend.Names returns it,
	// so contract tests and operators can see exactly which tables, constraints and lock
	// keys a deployment touches.
	Names = sqlexec.Names

	// AdvisoryLockSQL renders a session-scoped named lock that a query takes with a bounded
	// wait, in the manner of MySQL's GET_LOCK, for a BackendSpec.
	//
	// Each lock query returns one row whose first column is 1 or true when the lock is held,
	// 0 or false when it is not, or NULL on error.
	AdvisoryLockSQL = sqlexec.AdvisoryLockSQL

	// TableLockSQL renders a lock held as a row lock on a single-row lock table inside an
	// open transaction, for a BackendSpec.
	//
	// Before each migration or seed the executor runs SELECT 1 in that transaction, to
	// confirm it is still open, and returns ErrLockLost when it is not.
	TableLockSQL = sqlexec.TableLockSQL
)

// LockMode selects how a Backend stops two processes migrating at once: LockAdvisory,
// LockTable or LockNone. Pass it to WithLock.
type LockMode = sqlexec.LockMode

// The ports a Migrator and a Seeder run through, and the database/sql implementation.
type (
	// ExecutorPort is the port a Migrator runs migrations through: the database operations,
	// the history and the lock.
	//
	// Implement it for a store that database/sql cannot reach, or one with history tables of
	// its own, and pass it to NewMigratorFor; the Executor that NewExecutor returns is the
	// database/sql implementation, and docs/how-to/migration-backend.md walks through
	// writing one. Additional methods may be introduced in a minor release under
	// docs/reference/compatibility.md.
	//
	// Treat a missing history table as empty. The Migrator creates it only while holding the
	// lock. A lock must remain held until ReleaseLock, even after its acquisition context is
	// cancelled.
	//
	// For resumable non-transactional migrations, record the last completed statement and
	// the digest of the completed prefix. On resume, update the existing dirty history row
	// and skip that prefix only if it still matches. Return ResumeMismatchError without
	// running statements when it differs, and clear Dirty only after success. Executors
	// without progress tracking, such as the append-only ClickHouse backend's, record only
	// completed migrations.
	ExecutorPort = migrate.ExecutorPort

	// HistoryEditorPort is the optional port an ExecutorPort also implements when it can
	// rewrite the migration history without running a migration, which the Migrator's Force,
	// PlanRepair and Repair need; the Executor from NewExecutor is one.
	//
	// An ExecutorPort without it still runs migrations. The Migrator calls RecordApplied and
	// RecordProgress only while it holds the lock, after EnsureMigrationTable.
	HistoryEditorPort = migrate.HistoryEditorPort

	// SeedExecutorPort is the port a Seeder runs seeds through: the database operations, the
	// seed history and the seed lock. Implement it for a store that database/sql cannot
	// reach and pass it to NewSeederFor; NewSeeder builds the database/sql implementation
	// for itself.
	//
	// Its lock is separate from the migration lock. Additional methods may be introduced in
	// a minor release under the compatibility policy.
	SeedExecutorPort = migrate.SeedExecutorPort

	// SeedLockTrierPort is the optional port a SeedExecutorPort also implements when it can
	// take its lock without waiting, which a Seeder built with WithNonBlockingLock needs;
	// the executor NewSeeder builds is one. A SeedExecutorPort without it still runs seeds.
	SeedLockTrierPort = migrate.SeedLockTrierPort

	// AppliedMigration represents a migration that has been applied to the database, as
	// recorded in the migration history table. It is part of the ExecutorPort contract,
	// which is allowed to gain fields in a minor release.
	AppliedMigration = migrate.AppliedMigration

	// MigrationRecord holds the data needed to execute and record a single migration. It is
	// part of the ExecutorPort contract, which is allowed to gain fields in a minor release,
	// so construct it with field names.
	MigrationRecord = migrate.MigrationRecord

	// AppliedSeed represents a seed that has been applied to the database.
	AppliedSeed = migrate.AppliedSeed

	// SeedRecord holds the data needed to execute and record a single seed. It is part of
	// the SeedExecutorPort contract.
	SeedRecord = migrate.SeedRecord

	// Executor implements ExecutorPort and HistoryEditorPort over database/sql, for every
	// Backend. NewExecutor builds one; NewMigrator builds one for itself.
	//
	// An Executor is safe for concurrent use. Its state is guarded by a mutex, and a second
	// AcquireLock in the same process waits until ReleaseLock (TryAcquireLock fails with
	// ErrLockNotAcquired instead), serialising runs on that executor. Coordination across
	// processes depends on the backend's lock mode. When a run pins a session, other calls
	// share it and may observe the run's uncommitted work: drive one Executor from one
	// Migrator, which runs its calls one at a time.
	Executor = sqlexec.Executor

	// ExecutorOption customises an Executor at construction time, as WithProgressBatchSize
	// does.
	ExecutorOption = sqlexec.ExecutorOption
)

// NewExecutor creates the database/sql ExecutorPort, for use with NewMigratorFor when
// executor settings such as the progress batch size are needed.
//
// The history table is validated as a safe identifier first, which is what makes
// interpolating it into the executor's SQL safe.
//
// Takes database (*sql.DB) which is the database connection.
// Takes backend (Backend) which provides the database's SQL and locking.
// Takes options (...ExecutorOption) which set the progress batch size.
//
// Returns *Executor which is ready to execute migrations.
// Returns error which is the backend's recorded option error, or wraps
// seagull.ErrInvalidConfig for a backend no provider or CustomBackend built, a nil
// database or an option given a value it cannot use.
func NewExecutor(database *sql.DB, backend Backend, options ...ExecutorOption) (*Executor, error) {
	return sqlexec.NewExecutor(database, backend, options...)
}

// The errors a run returns, which a caller matches with errors.As.
type (
	// ChecksumMismatchError is returned when an applied migration's recorded checksum does
	// not match the current file on disk.
	ChecksumMismatchError = migrate.ChecksumMismatchError

	// DownChecksumMismatchError is returned when a down migration file's checksum does not
	// match the checksum recorded when the up migration was applied.
	DownChecksumMismatchError = migrate.DownChecksumMismatchError

	// MigrationExecutionError wraps an error from executing a migration's SQL content,
	// carrying the migration identity and direction.
	MigrationExecutionError = migrate.MigrationExecutionError

	// SeedExecutionError wraps an error from executing a seed's SQL content, carrying the
	// seed identity for diagnostics.
	SeedExecutionError = migrate.SeedExecutionError

	// LockAcquisitionError wraps a failure to acquire the migration lock or, from a Seeder,
	// the seed lock.
	LockAcquisitionError = migrate.LockAcquisitionError

	// MissingMigrationFileError is returned when the database records an applied migration
	// but no corresponding file exists on disk.
	MissingMigrationFileError = migrate.MissingMigrationFileError

	// NoDownMigrationError is returned when a rollback is requested for a specific version
	// that has no .down.sql file.
	NoDownMigrationError = migrate.NoDownMigrationError

	// DirtyMigrationError is returned when a dirty (partially-applied) migration blocks
	// further progress. If the dirty migration matches the next pending version it can be
	// retried automatically; otherwise manual intervention is required.
	DirtyMigrationError = migrate.DirtyMigrationError

	// UnknownMigrationError is returned when a migration is named by a version that no
	// migration file has.
	UnknownMigrationError = migrate.UnknownMigrationError

	// LaterMigrationError is returned by the Migrator's Force when the history records a
	// migration after the one to force, so forcing it would not make it the last applied
	// migration. Roll the later migrations back first, or force the later one instead; a
	// dirty one is resolved with the Migrator's Repair.
	LaterMigrationError = migrate.LaterMigrationError

	// ResumeMismatchError reports that a dirty migration's completed statements no longer
	// match the current file.
	//
	// The migration remains dirty until its history is checked and repaired with the
	// Migrator's PlanRepair and Repair.
	ResumeMismatchError = migrate.ResumeMismatchError

	// DuplicateMigrationVersionError is returned when two migration files share a version
	// and direction, for example two 001_*.up.sql files.
	//
	// Such a collision is common after a branch merge. Left alone, whichever file a
	// version-keyed map saw last would win, so an applied record could be validated against
	// the wrong file (a false checksum mismatch, or a false pass hiding drift) and the wrong
	// down file kept for rollback. The error names both files so the conflict is resolved
	// before any migration runs.
	DuplicateMigrationVersionError = migrate.DuplicateMigrationVersionError

	// DuplicateSeedVersionError is returned when two seed files share a version, for example
	// two 001_*.sql files.
	//
	// Left alone, a version-keyed map would silently drop one of them; the error names both
	// files so the collision is resolved before any seed runs.
	DuplicateSeedVersionError = migrate.DuplicateSeedVersionError

	// HookPanicError reports a panic recovered from a migration hook.
	//
	// The run stops with it as it would with an error the hook returned. Match it with
	// errors.As, or match every recovered panic with errors.Is and seagull.ErrPanic. The
	// stack is logged once, at warning level through the context's logger, where the panic
	// was recovered; the error does not carry it.
	HookPanicError = migrate.HookPanicError
)

// The lexical rules a Backend splits migration and seed files under.
type (
	// Rules describes how an engine writes SQL text, as the Rules field of a BackendSpec.
	//
	// The scanner and splitter read only these rules, so every lexical difference between
	// engines is a value here rather than a branch in the code. A built-in dialect's rules
	// are the Lexical field of its facts, such as
	// dialect_engine_postgres.New().Info().Lexical.
	//
	// The zero value describes standard SQL: '...' strings with doubled quotes, "..." quoted
	// identifiers, -- and non-nesting /* */ comments, no parameter sigils, and ";" as the
	// terminator with no procedural blocks.
	Rules = sqllex.Rules

	// QuoteRules describes the quoted forms an engine accepts.
	QuoteRules = sqllex.QuoteRules

	// CommentRules describes the comment forms an engine accepts. Every engine accepts --
	// line comments and /* */ block comments; these rules refine them.
	CommentRules = sqllex.CommentRules

	// ParameterRules describes the bind-parameter placeholders an engine accepts. A
	// placeholder is scanned as one KindParameter token.
	ParameterRules = sqllex.ParameterRules

	// ProceduralBlockFunc decides whether a statement can hold a procedural body whose BEGIN
	// ... END block keeps statement terminators inside the statement.
	//
	// It is the ProceduralBlock field of Rules.
	//
	// The splitter calls it when it meets the word BEGIN after the statement has already
	// started, passing the statement's leading unquoted words in upper case, up to (and not
	// including) that BEGIN and at most 32 of them. When it returns true, the BEGIN opens a
	// block: terminators stay inside the statement until the matching END, CASE ... END
	// expressions inside the block are balanced, and END IF, END LOOP, END WHILE and END
	// REPEAT close inner constructs without closing the block. When it returns false the
	// BEGIN is an ordinary word, such as a column named begin. The slice is reused between
	// calls, so the function must not keep it. A panic in the function stops the split: the
	// file fails with an error rather than the program, and the stack is logged once.
	//
	// Takes leadingWords ([]string) which holds the upper-cased words before the BEGIN.
	//
	// Returns bool which is true when the BEGIN opens a procedural block.
	ProceduralBlockFunc = sqllex.ProceduralBlockFunc

	// StatementPredicate decides a property of one statement from its significant tokens:
	// its words, quoted identifiers, strings, numbers, parameters, punctuation and
	// executable comments, without whitespace or ordinary comments.
	//
	// As the RequiresNoTransaction field of Rules it picks the statements that cannot run
	// inside a transaction. A panic in the predicate fails the migration with an error
	// rather than the program, and the stack is logged once.
	//
	// Takes tokens ([]Token) which holds the statement's significant tokens in order.
	//
	// Returns bool which is true when the statement has the property.
	StatementPredicate = sqllex.StatementPredicate

	// Token is one lexical token and where it sits in the input, as a StatementPredicate
	// receives it.
	Token = sqllex.Token
)

// TokenKind classifies a token: KindWord, KindString and the other Kind constants.
type TokenKind = sqllex.Kind

// EscapeMode says how a quote character is written inside a quoted token.
type EscapeMode = sqllex.EscapeMode

// DoubleQuoteMeaning says what text inside double quotes is.
type DoubleQuoteMeaning = sqllex.DoubleQuoteMeaning

// CustomBackend returns the backend a BackendSpec describes, for a database engine with
// no migrate_backend_* provider. The spec and the options are validated as the built-in
// backends' are; a problem is recorded in the Backend and returned by the executor
// constructors.
//
// Takes spec (BackendSpec) which describes the engine's migration SQL.
// Takes options (...BackendOption) which set the namespace, history table and lock mode.
//
// Returns Backend which is ready for NewMigrator, NewSeeder or NewExecutor.
func CustomBackend(spec BackendSpec, options ...BackendOption) Backend {
	return sqlexec.Custom(spec, options...)
}

// WithProgressBatchSize sets how often non-transactional migrations save statement
// progress.
//
// The default saves after every statement. Larger batches reduce history writes but can
// replay more completed statements after a crash.
//
// Takes size (int) which is the number of statements between progress writes.
//
// Returns ExecutorOption which sets the batch size; a size below one makes NewExecutor
// return an error wrapping seagull.ErrInvalidConfig.
func WithProgressBatchSize(size int) ExecutorOption {
	return sqlexec.WithProgressBatchSize(size)
}

// WithBeforeMigration registers a hook that runs before each individual migration.
// Returning an error cancels the migration run.
//
// Takes hook (func(ctx context.Context, migration MigrationHookContext) error) which is
// the callback to invoke before each migration executes.
//
// Returns Option which adds the hook to a Migrator; a Seeder ignores it.
func WithBeforeMigration(hook func(ctx context.Context, migration MigrationHookContext) error) Option {
	return migrate.WithBeforeMigration(hook)
}

// WithAfterMigration registers a hook that runs after each individual migration succeeds.
//
// Takes hook (func(ctx context.Context, migration MigrationHookContext) error) which is
// the callback to invoke after each migration executes successfully.
//
// Returns Option which adds the hook to a Migrator; a Seeder ignores it.
func WithAfterMigration(hook func(ctx context.Context, migration MigrationHookContext) error) Option {
	return migrate.WithAfterMigration(hook)
}

// WithBeforeRun registers a hook that runs before the migration run begins, after lock
// acquisition and pending migration computation. Returning an error cancels the run.
//
// Takes hook (func(ctx context.Context, run MigrationRunHookContext) error) which is the
// callback to invoke before the migration run starts.
//
// Returns Option which adds the hook to a Migrator; a Seeder ignores it.
func WithBeforeRun(hook func(ctx context.Context, run MigrationRunHookContext) error) Option {
	return migrate.WithBeforeRun(hook)
}

// WithAfterRun registers a hook that runs after the migration run completes, with the
// number of migrations it applied.
//
// Takes hook (func(ctx context.Context, run MigrationRunHookContext, applied int) error)
// which is the callback to invoke after the migration run completes successfully.
//
// Returns Option which adds the hook to a Migrator; a Seeder ignores it.
func WithAfterRun(hook func(ctx context.Context, run MigrationRunHookContext, applied int) error) Option {
	return migrate.WithAfterRun(hook)
}

// WithNonBlockingLock makes the Migrator fail at once with ErrLockNotAcquired when
// another process holds the migration lock, instead of waiting for it, and a Seeder do
// the same with the seed lock. NewSeederFor returns an error wrapping
// seagull.ErrInvalidConfig when the SeedExecutorPort does not implement
// SeedLockTrierPort.
//
// Returns Option which enables the non-blocking lock.
func WithNonBlockingLock() Option {
	return migrate.WithNonBlockingLock()
}

// WithLockTimeout limits how long migration or seed lock acquisition waits.
//
// It does not limit execution after the lock is acquired. Zero waits until context
// cancellation; an expired timeout returns ErrLockTimeout without making changes.
//
// Takes timeout (time.Duration) which is the longest wait; it must not be negative.
//
// Returns Option which sets the timeout.
func WithLockTimeout(timeout time.Duration) Option {
	return migrate.WithLockTimeout(timeout)
}

// WithDirectivePrefixes sets the directive prefixes recognised when a migration is
// scanned for a `-- <prefix>.migration(no_transaction: true)` directive.
//
// The default is the canonical sgl prefix with its seagull alias. Prefixes are matched
// literally, so each must be a lower-case identifier.
//
// Takes canonical (string) which is the canonical directive prefix.
// Takes aliases (...string) which are further prefixes accepted as synonyms.
//
// Returns Option which sets the recognised prefixes.
func WithDirectivePrefixes(canonical string, aliases ...string) Option {
	return migrate.WithDirectivePrefixes(canonical, aliases...)
}

// WithLogger sets the logger every call logs through. Without it a call logs through the
// logger carried by its context, or slog.Default().
//
// Takes logger (*slog.Logger) which receives the log records; nil keeps the default.
//
// Returns Option which sets the logger.
func WithLogger(logger *slog.Logger) Option {
	return migrate.WithLogger(logger)
}

// WithObserver sets the observer that receives a span for each migration and seed
// operation. Without it a call reports to the observer carried by its context, or to
// none.
//
// Takes observer (observe.ObserverPort) which receives the spans, the ObserverPort of
// sdk/observe; nil keeps the default.
//
// Returns Option which sets the observer.
func WithObserver(observer observe.ObserverPort) Option {
	return migrate.WithObserver(observer)
}

// WithNamespace sets the prefix every persisted name derives from: the <ns>_migrations
// and <ns>_seeds history tables, the <ns>_migration_lock and <ns>_seed_lock lock tables
// and their constraints, and the advisory lock keys, which equal the history table names.
// The default is sgl.
//
// Takes namespace (string) which is the prefix; it must be a SQL identifier of letters,
// digits and underscores, short enough that every derived name is one too.
//
// Returns BackendOption which sets the namespace; an invalid one makes the executor
// constructors return an error wrapping ErrInvalidIdentifier.
func WithNamespace(namespace string) BackendOption {
	return sqlexec.WithNamespace(namespace)
}

// WithHistoryTable names the migration history table, so several migration timelines can
// share a database.
//
// The migration lock key follows the history table; the seed table and the seed lock stay
// namespace-derived. It applies after WithNamespace whatever the order of the options.
//
// Takes name (string) which is the table name; it must be a SQL identifier of letters,
// digits and underscores, and differ from the seed and lock tables. Empty keeps the
// namespace-derived name.
//
// Returns BackendOption which sets the history table; an invalid or clashing name makes
// the executor constructors return an error wrapping ErrInvalidIdentifier.
func WithHistoryTable(name string) BackendOption {
	return sqlexec.WithHistoryTable(name)
}

// WithLock selects how runs are serialised: LockAdvisory, LockTable or LockNone.
//
// Takes mode (LockMode) which is the lock mode.
//
// Returns BackendOption which sets the lock mode; a mode the database cannot provide
// makes the executor constructors return ErrUnsupportedLock.
func WithLock(mode LockMode) BackendOption {
	return sqlexec.WithLock(mode)
}

// WithDriverMultiStatements configures whether seed files can run in one driver call.
//
// Otherwise seeds are split into statements. Set it for MySQL when the DSN enables
// multiStatements. Migrations are always split to track progress.
//
// Takes enabled (bool) which reports whether the driver runs several statements per call.
//
// Returns BackendOption which sets how seed files are run.
func WithDriverMultiStatements(enabled bool) BackendOption {
	return sqlexec.WithDriverMultiStatements(enabled)
}
