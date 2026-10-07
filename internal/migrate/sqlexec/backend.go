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
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/migrate"
)

const (
	// LockAdvisory holds a session-level advisory lock: pg_advisory_lock on PostgreSQL and
	// GET_LOCK on MySQL. It is the default for Postgres and MySQL.
	LockAdvisory LockMode = iota + 1

	// LockTable holds a row lock on a single-row lock table with SELECT ...
	//
	// FOR UPDATE, for PostgreSQL behind a transaction-mode pooler such as PgBouncer, where
	// advisory locks do not survive. It is the default for PostgresPgBouncer.
	LockTable

	// LockNone takes no lock, so concurrent runs must be coordinated outside the migrator.
	// It is the only mode for SQLite, ClickHouse and DuckDB.
	LockNone
)

var (
	// ErrUnsupportedLock is returned when WithLock selects a lock mode the backend's
	// database cannot provide, such as LockAdvisory on SQLite.
	ErrUnsupportedLock = fmt.Errorf("lock mode not supported by this backend: %w", migrate.ErrInvalidConfig)

	// errBackendNotBuilt is returned for a Backend that no constructor produced.
	errBackendNotBuilt = fmt.Errorf("the Backend was not built by a constructor such as sqlexec.Postgres: %w", migrate.ErrInvalidConfig)

	// errNilDatabase is returned when an executor is created without a database.
	errNilDatabase = fmt.Errorf("a *sql.DB is required: %w", migrate.ErrInvalidConfig)
)

// LockMode selects how a Backend stops two processes migrating at once.
type LockMode uint8

// String returns the lock mode's name.
//
// Returns string which is advisory, table, none, or the number of an unknown mode.
func (mode LockMode) String() string {
	switch mode {
	case LockAdvisory:
		return "advisory"
	case LockTable:
		return "table"
	case LockNone:
		return "none"
	default:
		return fmt.Sprintf("LockMode(%d)", uint8(mode))
	}
}

// BackendOption configures a Backend.
type BackendOption func(*backendOptions)

// Backend is the run-time migration SQL for one database: the history and seed table DDL,
// the locks, placeholder syntax and statement splitting, with the persisted names they
// use. Take one from a migrate_backend_* provider, such as migrate_backend_postgres.New,
// or build one with CustomBackend; the zero value is not usable.
//
// A Backend is an immutable value and its locks keep no per-acquisition state, so one
// Backend can be shared by any number of executors, concurrently.
type Backend struct {
	// err records why the backend's spec or options could not be applied; NewExecutor,
	// NewSeedExecutor, NewMigrator and NewSeeder return it.
	err error

	// engine holds the database's SQL; nil for a Backend no constructor built.
	engine *engine

	// migrationLock serialises migration runs.
	migrationLock lockStrategy

	// seedLock serialises seed runs, keyed apart from migrationLock.
	seedLock lockStrategy

	// names holds the persisted names, with the lock keys as Names reports them.
	names Names

	// lockMode is the lock mode the backend uses.
	lockMode LockMode
}

// Names reports every persisted object name the backend uses.
//
// Returns Names which holds the history and seed tables, the lock tables and their
// constraints, and the advisory lock keys.
func (d Backend) Names() Names {
	if d.engine == nil && d.names == (Names{}) {
		return namesFor(DefaultNamespace)
	}
	return d.names
}

// Split divides a migration or seed file into the statements the executors run, in order,
// under the backend's lexical rules (or the Split of its BackendSpec). A dirty
// migration's last completed statement indexes this list.
//
// Takes content (string) which is the file's SQL.
//
// Returns []string which holds each statement's text, without its terminator.
// Returns error which wraps ErrMalformedStatement when the text cannot be split,
// sqllex.ErrRulesCallback when a lexical rules callback panicked, or
// migrate.ErrInvalidConfig for a Backend no constructor built. A panic in a BackendSpec's
// Split or in a lexical rules callback is returned as an error, with its stack logged
// once through the default logger.
func (d Backend) Split(content string) ([]string, error) {
	if d.engine == nil {
		return nil, errBackendNotBuilt
	}
	return d.engine.split(context.Background(), content)
}

// historyTable returns the migration history table.
//
// Returns string which is the history table name.
func (d Backend) historyTable() string {
	return d.Names().History
}

// seedTable returns the seed history table.
//
// Returns string which is the seed table name.
func (d Backend) seedTable() string {
	return d.Names().Seeds
}

// placeholder renders the bind-parameter marker for a 1-based index.
//
// Takes index (int) which is the parameter index.
//
// Returns string which is the marker.
func (d Backend) placeholder(index int) string {
	return d.engine.placeholder(index)
}

// usable reports why the backend cannot drive an executor over database.
//
// Takes database (*sql.DB) which is the executor's database.
//
// Returns error which is the backend's recorded error, or wraps migrate.ErrInvalidConfig
// for a backend no constructor built or a nil database.
func (d Backend) usable(database *sql.DB) error {
	if d.err != nil {
		return d.err
	}
	if d.engine == nil {
		return errBackendNotBuilt
	}
	if database == nil {
		return errNilDatabase
	}
	return nil
}

// backendOptions collects the options passed to a Backend constructor.
type backendOptions struct {
	// namespace, when set, replaces DefaultNamespace.
	namespace *string

	// historyTable, when set, names the migration history table.
	historyTable *string

	// multiStatements, when set, replaces the engine's view of whether the driver runs
	// several statements in one call.
	multiStatements *bool

	// lock, when non-zero, replaces the backend's default lock mode.
	lock LockMode

	// serverWideLockKey keeps MySQL lock keys server-wide instead of scoping them to the
	// current schema.
	serverWideLockKey bool

	// ansiQuotes reads "..." as a quoted identifier when splitting MySQL files.
	ansiQuotes bool
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
	return func(options *backendOptions) {
		options.namespace = &namespace
	}
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
	return func(options *backendOptions) {
		options.historyTable = &name
	}
}

// WithLock selects how runs are serialised: LockAdvisory, LockTable or LockNone.
//
// Takes mode (LockMode) which is the lock mode.
//
// Returns BackendOption which sets the lock mode; a mode the database cannot provide
// makes the executor constructors return ErrUnsupportedLock.
func WithLock(mode LockMode) BackendOption {
	return func(options *backendOptions) {
		options.lock = mode
	}
}

// WithServerWideLockKey enables MySQL's legacy server-wide migration lock key.
//
// Use it while older processes still share the server so all migrators take the same
// lock. Other engines ignore it.
//
// Returns BackendOption which keeps the lock keys server-wide.
func WithServerWideLockKey() BackendOption {
	return func(options *backendOptions) {
		options.serverWideLockKey = true
	}
}

// WithANSIQuotes makes MySQL and MariaDB migration splitting treat double quotes as
// identifier delimiters, matching servers with ANSI_QUOTES enabled. Other engines ignore
// it.
//
// Takes enabled (bool) which is true when the server's sql_mode holds ANSI_QUOTES.
//
// Returns BackendOption which sets how "..." is read.
func WithANSIQuotes(enabled bool) BackendOption {
	return func(options *backendOptions) {
		options.ansiQuotes = enabled
	}
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
	return func(options *backendOptions) {
		options.multiStatements = &enabled
	}
}

// Postgres returns the backend for PostgreSQL: $N placeholders, transactional DDL and, by
// default, advisory locks.
//
// CockroachDB has no advisory locks; use CockroachDB for it. For TimescaleDB use
// TimescaleDB, which also recognises the continuous-aggregate statements that cannot run
// inside a transaction.
//
// Takes options (...BackendOption) which set the namespace, history table and lock mode
// (advisory, table or none).
//
// Returns Backend which is ready for NewMigrator, NewSeeder or NewExecutor.
func Postgres(options ...BackendOption) Backend {
	return buildBackend(postgresEngine(), options)
}

// TimescaleDB returns the backend for TimescaleDB: PostgreSQL's, reading migrations with
// TimescaleDB's lexical rules, so that a migration which creates a continuous aggregate
// with data or calls refresh_continuous_aggregate runs outside a transaction, as
// TimescaleDB requires.
//
// Takes options (...BackendOption) which set the namespace, history table and lock mode
// (advisory, table or none).
//
// Returns Backend which is ready for NewMigrator, NewSeeder or NewExecutor.
func TimescaleDB(options ...BackendOption) Backend {
	return buildBackend(timescaleDBEngine(), options)
}

// PostgresPgBouncer returns the backend for PostgreSQL behind PgBouncer in transaction
// mode, where session-level advisory locks are unavailable: Postgres with a table lock by
// default.
//
// The table lock's transaction stays open, idle, for the whole run. It turns off the
// server's idle_in_transaction_session_timeout for itself, and before each migration or
// seed the executor confirms the transaction is still open, returning ErrLockLost when it
// is not. PgBouncer's own idle_transaction_timeout cannot be turned off from a session,
// so it must be zero or longer than the longest migration run.
//
// Takes options (...BackendOption) which set the namespace, history table and lock mode.
//
// Returns Backend which is ready for NewMigrator, NewSeeder or NewExecutor.
func PostgresPgBouncer(options ...BackendOption) Backend {
	return buildBackend(postgresPgBouncerEngine(), options)
}

// CockroachDB returns the backend for CockroachDB: Postgres with a table lock by default,
// since CockroachDB has no advisory lock functions.
//
// The lock transaction turns on enable_durable_locking_for_serializable, so a range split
// or move during a schema change cannot drop the lock; that needs CockroachDB v23.2 or
// later, and an older server fails the lock with ErrUnsupportedLock. The transaction
// refers to the lock table by its descriptor ID, so it holds no lease on the database or
// schema that a migration's CREATE FUNCTION or CREATE SCHEMA would wait for. It also
// turns off idle_in_transaction_session_timeout for itself, as it sits idle for the whole
// run, and before each migration or seed the executor confirms it is still open,
// returning ErrLockLost when it is not.
//
// Migrations do not run in transactions, because CockroachDB does not make schema changes
// atomic with the rest of a transaction: each statement runs on its own, and a migration
// that fails part-way stays dirty and resumes at the statement that failed.
//
// Takes options (...BackendOption) which set the namespace, history table and lock mode
// (table or none).
//
// Returns Backend which is ready for NewMigrator, NewSeeder or NewExecutor.
func CockroachDB(options ...BackendOption) Backend {
	return buildBackend(cockroachDBEngine(), options)
}

// MySQL returns the backend for MySQL and MariaDB: ? placeholders, statements split under
// MySQL's lexical rules, DELIMITER lines included, and, by default, GET_LOCK advisory
// locks keyed within the current schema.
//
// MySQL commits implicitly around every DDL statement, so migrations do not run in
// transactions: each statement runs on its own and the history row records the progress,
// so a migration that fails part-way stays dirty and resumes after its last completed
// statement. A seed and its history row commit together in a transaction. Seed files are
// split, as go-sql-driver/mysql runs one statement per call; WithDriverMultiStatements
// runs them whole when the DSN sets multiStatements=true. A run works on one session and
// qualifies the history and seed tables with the database current when it began, so a USE
// in a migration cannot move them.
//
// Takes options (...BackendOption) which set the namespace, history table, lock mode
// (advisory or none), lock key scope, multi-statement driver and, with WithANSIQuotes,
// whether the server reads "..." as an identifier.
//
// Returns Backend which is ready for NewMigrator, NewSeeder or NewExecutor.
func MySQL(options ...BackendOption) Backend {
	return buildBackend(mysqlEngine(), options)
}

// MySQLWithDSN returns the MySQL backend with WithDriverMultiStatements set from the
// multiStatements parameter of a go-sql-driver/mysql data source name.
//
// Takes dsn (string) which is the MySQL data source name.
// Takes options (...BackendOption) which set the namespace, history table, lock mode and
// lock key scope; a WithDriverMultiStatements among them overrides the DSN.
//
// Returns Backend which is ready for NewMigrator, NewSeeder or NewExecutor.
//
// Deprecated: the DSN carries credentials and is read only for that one flag. Use MySQL
// with WithDriverMultiStatements instead.
func MySQLWithDSN(dsn string, options ...BackendOption) Backend {
	return MySQL(append([]BackendOption{WithDriverMultiStatements(dsnEnablesMultiStatements(dsn))}, options...)...)
}

// SQLite returns the backend for SQLite: ? placeholders, transactional DDL and no lock,
// since SQLite serialises writers itself.
//
// Takes options (...BackendOption) which set the namespace and history table; only
// LockNone is supported.
//
// Returns Backend which is ready for NewMigrator, NewSeeder or NewExecutor.
func SQLite(options ...BackendOption) Backend {
	return buildBackend(sqliteEngine(), options)
}

// ClickHouse returns the backend for ClickHouse: ? placeholders, no transactions, an
// append-only ReplacingMergeTree history read with FINAL, and no lock, so concurrent runs
// must be coordinated outside the migrator.
//
// Takes options (...BackendOption) which set the namespace and history table; only
// LockNone is supported.
//
// Returns Backend which is ready for NewMigrator, NewSeeder or NewExecutor.
func ClickHouse(options ...BackendOption) Backend {
	return buildBackend(clickHouseEngine(), options)
}

// DuckDB returns a backend with transactional migrations and whole-file seed execution.
//
// DuckDB's file access excludes other writing processes. Within a process, each executor
// serialises its own lock holders.
//
// Takes options (...BackendOption) which set the namespace and history table; only
// LockNone is supported.
//
// Returns Backend which is ready for NewMigrator, NewSeeder or NewExecutor.
func DuckDB(options ...BackendOption) Backend {
	return Custom(duckDBSpec, options...)
}

// NewMigrator creates a migrate.Migrator over a database/sql database, the usual way to
// run migrations from a binary.
//
// Takes database (*sql.DB) which is the database to migrate.
// Takes backend (Backend) which is the database's backend, such as
// migrate_backend_postgres.New().
// Takes files (fs.FS) which holds the migration files, such as an embed.FS.
// Takes directory (string) which is the slash-separated directory of the migration files
// within files.
// Takes options (...migrate.Option) which configure locking, hooks, prefixes, logging and
// limits.
//
// Returns *migrate.Migrator which is ready to run.
// Returns error when the backend's options were invalid, the database is nil, or the
// migrator cannot be created.
func NewMigrator(database *sql.DB, backend Backend, files fs.FS, directory string, options ...migrate.Option) (*migrate.Migrator, error) {
	executor, err := NewExecutor(database, backend)
	if err != nil {
		return nil, err
	}
	return migrate.New(executor, files, directory, options...)
}

// NewSeeder creates a migrate.Seeder over a database/sql database.
//
// Takes database (*sql.DB) which is the database to seed.
// Takes backend (Backend) which is the database's backend, such as
// migrate_backend_postgres.New().
// Takes files (fs.FS) which holds the seed files.
// Takes directory (string) which is the slash-separated directory of the seed files
// within files.
// Takes options (...migrate.Option) which configure logging and limits.
//
// Returns *migrate.Seeder which is ready to run.
// Returns error when the backend's options were invalid, the database is nil, or the
// seeder cannot be created.
func NewSeeder(database *sql.DB, backend Backend, files fs.FS, directory string, options ...migrate.Option) (*migrate.Seeder, error) {
	executor, err := NewSeedExecutor(database, backend)
	if err != nil {
		return nil, err
	}
	return migrate.NewSeeder(executor, files, directory, options...)
}

// dsnEnablesMultiStatements reports whether a go-sql-driver/mysql data source name sets
// multiStatements to true. Its parameters follow the first '?' after the last '/', as the
// driver reads them, and their values are booleans in any form strconv.ParseBool accepts.
//
// Takes dsn (string) which is the data source name.
//
// Returns bool which is true when the DSN enables multiple statements per call.
func dsnEnablesMultiStatements(dsn string) bool {
	_, database, hasSlash := strings.CutLast(dsn, "/")
	if !hasSlash {
		return false
	}
	_, parameters, found := strings.Cut(database, "?")
	if !found {
		return false
	}
	for parameter := range strings.SplitSeq(parameters, "&") {
		key, value, _ := strings.Cut(parameter, "=")
		if key != "multiStatements" {
			continue
		}
		unescaped, unescapeError := url.QueryUnescape(value)
		if unescapeError != nil {
			return false
		}
		enabled, parseError := strconv.ParseBool(unescaped)
		return parseError == nil && enabled
	}
	return false
}

// buildBackend applies options to an engine: the names first, then the lock mode, so the
// result does not depend on the order of the options. Every problem is recorded in the
// backend for the executor constructors to return.
//
// Takes base (*engine) which is the database's SQL.
// Takes options ([]BackendOption) which configure the backend.
//
// Returns Backend which is the configured backend.
func buildBackend(base *engine, options []BackendOption) Backend {
	var chosen backendOptions
	for _, option := range options {
		option(&chosen)
	}

	if chosen.multiStatements != nil && *chosen.multiStatements != base.multiStatementSeeds {
		adjusted := *base
		adjusted.multiStatementSeeds = *chosen.multiStatements
		base = &adjusted
	}
	if chosen.ansiQuotes && base.ansiQuotesRules != nil {
		adjusted := *base
		adjusted.rules = *base.ansiQuotesRules
		adjusted.split = lexicalSplitter(adjusted.rules)
		base = &adjusted
	}

	names, namesError := resolveNames(chosen)
	backend := Backend{
		err:           nil,
		engine:        base,
		migrationLock: nil,
		seedLock:      nil,
		names:         names,
		lockMode:      cmp.Or(chosen.lock, base.defaultLock),
	}

	factory, supported := base.locks[backend.lockMode]
	if !supported {
		backend.err = errors.Join(namesError, fmt.Errorf("%w: %s on %s", ErrUnsupportedLock, backend.lockMode, base.name))
		return backend
	}
	locks := factory(names, chosen.serverWideLockKey)
	backend.migrationLock, backend.seedLock = locks.migration, locks.seed
	backend.names.LockKey = describeLockKey(locks.migration, names.LockKey)
	backend.names.SeedLockKey = describeLockKey(locks.seed, names.SeedLockKey)
	backend.err = namesError
	return backend
}

// describeLockKey returns how Names reports a lock's key.
//
// Takes lock (lockStrategy) which is the lock.
// Takes key (string) which is the key it was built with.
//
// Returns string which is the lock's own description of a scoped key, or key.
func describeLockKey(lock lockStrategy, key string) string {
	if polling, ok := lock.(*pollingAdvisoryLock); ok {
		return polling.description
	}
	return key
}

// resolveNames derives the persisted names from the chosen namespace and history table,
// and checks them.
//
// Takes chosen (backendOptions) which holds the options.
//
// Returns Names which holds the names; an invalid namespace or history table is left at
// its default.
// Returns error which wraps ErrInvalidIdentifier for an unsafe name or two tables that
// share a name.
func resolveNames(chosen backendOptions) (Names, error) {
	names := namesFor(DefaultNamespace)
	if chosen.namespace != nil {
		derived, err := namespaceNames(*chosen.namespace)
		if err != nil {
			return names, err
		}
		names = derived
	}
	if chosen.historyTable != nil && *chosen.historyTable != "" {
		if err := validateIdentifier(*chosen.historyTable); err != nil {
			return names, fmt.Errorf("history table: %w", err)
		}
		names.History = *chosen.historyTable
		names.LockKey = *chosen.historyTable
	}
	return names, checkDistinct(names)
}

// namespaceNames derives and checks the names of a namespace.
//
// Takes namespace (string) which is the namespace prefix.
//
// Returns Names which holds the derived names.
// Returns error which wraps ErrInvalidIdentifier when the namespace or a derived name is
// not a safe identifier.
func namespaceNames(namespace string) (Names, error) {
	if err := validateIdentifier(namespace); err != nil {
		return Names{}, fmt.Errorf("namespace: %w", err)
	}
	derived := namesFor(namespace)
	for _, name := range []string{derived.History, derived.Seeds, derived.MigrationLockConstraint, derived.SeedLockConstraint} {
		if err := validateIdentifier(name); err != nil {
			return Names{}, fmt.Errorf("namespace %q derives an unsafe name: %w", namespace, err)
		}
	}
	return derived, nil
}

// checkDistinct rejects names that give two persisted tables the same name, which would
// merge their rows. Names are compared without regard to case, as most engines fold or
// ignore the case of unquoted identifiers.
//
// Takes names (Names) which holds the names.
//
// Returns error which wraps ErrInvalidIdentifier naming the clash, or nil.
func checkDistinct(names Names) error {
	tables := []struct {
		role string
		name string
	}{
		{role: "history table", name: names.History},
		{role: "seed table", name: names.Seeds},
		{role: "migration lock table", name: names.MigrationLock},
		{role: "seed lock table", name: names.SeedLock},
	}
	seen := make(map[string]string, len(tables))
	for _, table := range tables {
		folded := strings.ToLower(table.name)
		if previous, clash := seen[folded]; clash {
			return fmt.Errorf("%w: the %s and the %s are both %q", ErrInvalidIdentifier, previous, table.role, table.name)
		}
		seen[folded] = table.role
	}
	return nil
}
