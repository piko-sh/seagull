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
	"errors"
	"fmt"
	"regexp"
	"strings"

	clickhouse_lexical "seagull.sh/seagull/internal/dialect/clickhouse/lexical"
	duckdb_lexical "seagull.sh/seagull/internal/dialect/duckdb/lexical"
	mysql_lexical "seagull.sh/seagull/internal/dialect/mysql/lexical"
	postgres_lexical "seagull.sh/seagull/internal/dialect/postgres/lexical"
	sqlite_lexical "seagull.sh/seagull/internal/dialect/sqlite/lexical"
	timescaledb_lexical "seagull.sh/seagull/internal/dialect/timescaledb/lexical"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// DefaultNamespace is the prefix every persisted object name derives from unless
	// WithNamespace sets another.
	DefaultNamespace = "sgl"

	// defaultHistoryTableName is the history table of the default namespace.
	defaultHistoryTableName = DefaultNamespace + historyTableSuffix

	// defaultSeedTableName is the seed history table of the default namespace.
	defaultSeedTableName = DefaultNamespace + seedTableSuffix

	// historyTableSuffix follows the namespace in the migration history table name.
	historyTableSuffix = "_migrations"

	// seedTableSuffix follows the namespace in the seed history table name.
	seedTableSuffix = "_seeds"

	// migrationLockTableSuffix follows the namespace in the table-based migration lock table
	// name.
	migrationLockTableSuffix = "_migration_lock"

	// seedLockTableSuffix follows the namespace in the table-based seed lock table name.
	seedLockTableSuffix = "_seed_lock"

	// singleRowConstraintSuffix follows a lock table name in the name of the CHECK
	// constraint that keeps the lock table to a single row.
	singleRowConstraintSuffix = "_single_row"

	// postgresHistoryTableDDL creates the PostgreSQL migration history table.
	postgresHistoryTableDDL = `CREATE TABLE IF NOT EXISTS %s (
    version        BIGINT      NOT NULL PRIMARY KEY,
    name           TEXT        NOT NULL,
    checksum       TEXT        NOT NULL,
    applied_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms    BIGINT      NOT NULL,
    down_checksum  TEXT,
    last_statement INTEGER,
    dirty          BOOLEAN     NOT NULL DEFAULT FALSE
)`

	// postgresSeedTableDDL creates the PostgreSQL seed history table.
	postgresSeedTableDDL = `CREATE TABLE IF NOT EXISTS %s (
    version     BIGINT      NOT NULL PRIMARY KEY,
    name        TEXT        NOT NULL,
    checksum    TEXT        NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms BIGINT      NOT NULL
)`

	// mysqlHistoryTableDDL creates the MySQL migration history table.
	mysqlHistoryTableDDL = `CREATE TABLE IF NOT EXISTS %s (
    version        BIGINT       NOT NULL PRIMARY KEY,
    name           VARCHAR(255) NOT NULL,
    checksum       VARCHAR(64)  NOT NULL,
    applied_at     TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    duration_ms    BIGINT       NOT NULL,
    down_checksum  VARCHAR(64),
    last_statement INTEGER,
    dirty          BOOLEAN      NOT NULL DEFAULT FALSE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

	// mysqlSeedTableDDL creates the MySQL seed history table.
	mysqlSeedTableDDL = `CREATE TABLE IF NOT EXISTS %s (
    version     BIGINT       NOT NULL PRIMARY KEY,
    name        VARCHAR(255) NOT NULL,
    checksum    VARCHAR(64)  NOT NULL,
    applied_at  TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    duration_ms BIGINT       NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

	// clickHouseHistoryTableDDL creates the ClickHouse migration history table.
	clickHouseHistoryTableDDL = `CREATE TABLE IF NOT EXISTS %s (
    version        Int64       NOT NULL,
    name           String      NOT NULL,
    checksum       String      NOT NULL,
    applied_at     DateTime64(6) NOT NULL DEFAULT now64(),
    duration_ms    Int64       NOT NULL,
    down_checksum  Nullable(String),
    last_statement Nullable(Int32),
    dirty          UInt8       NOT NULL DEFAULT 0
) ENGINE = ReplacingMergeTree(applied_at) ORDER BY version`

	// clickHouseSeedTableDDL creates the ClickHouse seed history table.
	clickHouseSeedTableDDL = `CREATE TABLE IF NOT EXISTS %s (
    version     Int64         NOT NULL,
    name        String        NOT NULL,
    checksum    String        NOT NULL,
    applied_at  DateTime64(6) NOT NULL DEFAULT now64(),
    duration_ms Int64         NOT NULL
) ENGINE = ReplacingMergeTree(applied_at) ORDER BY version`

	// sqliteHistoryTableDDL creates the SQLite migration history table.
	sqliteHistoryTableDDL = `CREATE TABLE IF NOT EXISTS %s (
    version        INTEGER NOT NULL PRIMARY KEY,
    name           TEXT    NOT NULL,
    checksum       TEXT    NOT NULL,
    applied_at     TEXT    NOT NULL DEFAULT (datetime('now')),
    duration_ms    INTEGER NOT NULL,
    down_checksum  TEXT,
    last_statement INTEGER,
    dirty          INTEGER NOT NULL DEFAULT 0
)`

	// sqliteSeedTableDDL creates the SQLite seed history table.
	sqliteSeedTableDDL = `CREATE TABLE IF NOT EXISTS %s (
    version     INTEGER NOT NULL PRIMARY KEY,
    name        TEXT    NOT NULL,
    checksum    TEXT    NOT NULL,
    applied_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    duration_ms INTEGER NOT NULL
)`

	// duckDBHistoryTableDDL creates the DuckDB migration history table.
	duckDBHistoryTableDDL = `CREATE TABLE IF NOT EXISTS %s (
    version        BIGINT      NOT NULL PRIMARY KEY,
    name           VARCHAR     NOT NULL,
    checksum       VARCHAR     NOT NULL,
    applied_at     TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    duration_ms    BIGINT      NOT NULL,
    down_checksum  VARCHAR,
    last_statement INTEGER,
    dirty          BOOLEAN     NOT NULL DEFAULT FALSE
)`

	// duckDBSeedTableDDL creates the DuckDB seed history table.
	duckDBSeedTableDDL = `CREATE TABLE IF NOT EXISTS %s (
    version     BIGINT      NOT NULL PRIMARY KEY,
    name        VARCHAR     NOT NULL,
    checksum    VARCHAR     NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT current_timestamp,
    duration_ms BIGINT      NOT NULL
)`

	// duckDBEngineName names DuckDB in error messages.
	duckDBEngineName = "DuckDB"

	// splitterVersionColumn is the history column that records the version of the statement
	// splitter that divided a migration when its progress was recorded.
	splitterVersionColumn = "splitter_version"

	// statementsHashColumn is the history column that records the digest of the statements a
	// migration has completed.
	statementsHashColumn = "statements_hash"

	// requiresNoTransactionCallback names the rules' RequiresNoTransaction predicate in the
	// error a panic in it is returned as.
	requiresNoTransactionCallback = "RequiresNoTransaction"

	// proceduralBlockCallback names the rules' ProceduralBlock callback in the record a
	// panic in it is logged as.
	proceduralBlockCallback = "ProceduralBlock"
)

var (
	// ErrInvalidIdentifier is wrapped by constructor errors when WithHistoryTable or
	// WithNamespace supplies an unsafe SQL identifier, or two persisted tables would share a
	// name. Callers can match it with errors.Is.
	ErrInvalidIdentifier = errors.New("invalid SQL identifier")

	// ErrMalformedStatement is returned when migration SQL contains an unterminated string
	// literal, quoted identifier, dollar-quoted block or block comment, or an unbalanced
	// BEGIN or CASE block. It is the generator's ErrMalformedSQL, so it matches with
	// errors.Is either way, and the error also wraps the sqllex error that says where.
	ErrMalformedStatement = fault.ErrMalformedSQL

	// safeIdentifierRegex matches the conservative subset of SQL identifier syntax the
	// migrator accepts for the namespace, the migration history table name and lock keys.
	//
	// The match requires an ASCII letter or underscore start, then ASCII letters, digits, or
	// underscores, bounded to 63 characters (Postgres NAMEDATALEN minus one byte for the
	// trailing terminator). Quoted identifiers are deliberately rejected so no
	// dialect-specific quoting needs to be performed downstream.
	safeIdentifierRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)
)

// historyColumnAdditions lists the columns the migration history table gained after the
// eight its CREATE TABLE names, in the order they were added.
//
// The CREATE TABLE DDL is frozen, so EnsureMigrationTable adds each listed column a table
// lacks, upgrading a table that an earlier release created as well as a new one.
var historyColumnAdditions = []string{splitterVersionColumn, statementsHashColumn}

// standardHistoryColumnTypes maps each added history column to its standard SQL type.
var standardHistoryColumnTypes = map[string]string{
	splitterVersionColumn: "INTEGER",
	statementsHashColumn:  "VARCHAR(64)",
}

// clickHouseHistoryColumnTypes maps each added history column to its ClickHouse type.
var clickHouseHistoryColumnTypes = map[string]string{
	splitterVersionColumn: "Nullable(Int32)",
	statementsHashColumn:  "Nullable(String)",
}

// duckDBSpec describes DuckDB as a BackendSpec, so its backend is built through Custom
// exactly as a third-party engine's is.
//
// DuckDB runs DDL inside transactions, so each migration commits together with its
// history row. Its placeholders are $N, the form the DuckDB dialect generates, and it
// accepts PostgreSQL's ON CONFLICT (version) DO NOTHING for the idempotent seed insert.
// duckdb-go runs every statement of one Exec, so seed files run whole, and migrations are
// split under DuckDB's lexical rules, as the DuckDB dialect splits them.
//
// DuckDB has no advisory locks and no row locks (it rejects SELECT ... FOR UPDATE), so
// the backend takes no lock. A database file is opened read-write by one process at a
// time, so no other process can migrate it meanwhile; within a process an Executor admits
// one lock holder at a time, and DuckDB's optimistic concurrency control fails a
// conflicting transaction from another executor rather than applying it twice.
var duckDBSpec = BackendSpec{
	Placeholder:         numberedPlaceholder,
	CreateHistoryTable:  renderTable(duckDBHistoryTableDDL),
	CreateSeedTable:     renderTable(duckDBSeedTableDDL),
	InsertSeed:          postgresOnConflictSeedInsert,
	TableExists:         duckDBTableExists,
	DeleteHistory:       nil,
	ClearSeedHistory:    nil,
	Split:               nil,
	AddHistoryColumn:    nil,
	Rules:               duckdb_lexical.Rules(),
	AdvisoryLock:        nil,
	TableLock:           nil,
	Name:                duckDBEngineName,
	HistoryReadModifier: "",
	DefaultLock:         LockNone,
	TransactionalDDL:    true,
	TransactionalSeeds:  true,
	AppendOnlyHistory:   false,
	MultiStatementSeeds: true,
}

// Names lists every persisted object name a Backend uses, so contract tests and operators
// can see exactly which tables, constraints and lock keys a deployment touches.
type Names struct {
	// History is the migration history table.
	History string

	// Seeds is the seed history table.
	Seeds string

	// MigrationLock is the table the table-based migration lock uses.
	MigrationLock string

	// SeedLock is the table the table-based seed lock uses.
	SeedLock string

	// MigrationLockConstraint is the single-row CHECK constraint on MigrationLock.
	MigrationLockConstraint string

	// SeedLockConstraint is the single-row CHECK constraint on SeedLock.
	SeedLockConstraint string

	// LockKey identifies the migration advisory lock. MySQL scopes it to the current schema
	// unless WithServerWideLockKey is enabled.
	LockKey string

	// SeedLockKey is the advisory lock key the seed lock uses: the seed table name, scoped
	// on MySQL in the same way as LockKey.
	SeedLockKey string
}

// engine is the run-time SQL of one database: everything a Backend needs apart from the
// persisted names and the lock mode chosen for it.
//
// Built-in constructors and Custom both produce one, and validateEngine holds them to the
// same rules. An engine is never modified after construction, so Backends share it.
type engine struct {
	// placeholder renders the bind-parameter marker for a 1-based index.
	placeholder func(index int) string

	// createHistoryTable renders the idempotent DDL of the migration history table.
	createHistoryTable func(table string) string

	// createSeedTable renders the idempotent DDL of the seed history table.
	createSeedTable func(table string) string

	// insertSeed renders the idempotent INSERT that records an applied seed.
	insertSeed func(table, version, name, checksum, duration string) string

	// tableExists renders the query that counts the tables named by its one bind parameter
	// in the session's current schema.
	tableExists func(tablePlaceholder string) string

	// deleteHistory renders the statement that removes one history row by version; nil means
	// DELETE FROM <table> WHERE version = <placeholder>.
	deleteHistory func(table, versionPlaceholder string) string

	// clearSeedHistory renders the statement that empties the seed history; nil means DELETE
	// FROM <table>.
	clearSeedHistory func(table string) string

	// split divides a migration or seed file into statements. A Custom spec's Split runs
	// under a guard that returns its panic as an error, logged through ctx's logger.
	split func(ctx context.Context, content string) ([]string, error)

	// addHistoryColumn renders the DDL that adds one of historyColumnAdditions to a history
	// table that lacks it.
	addHistoryColumn func(table, column string) string

	// qualifyTable renders a table name qualified by a schema, quoting the schema as the
	// engine quotes identifiers; nil when currentSchemaSQL is empty.
	qualifyTable func(schema, table string) string

	// ansiQuotesRules are the engine's lexical rules when the server's sql_mode holds
	// ANSI_QUOTES, which WithANSIQuotes selects, or nil for an engine without the mode.
	ansiQuotesRules *sqllex.Rules

	// locks builds the migration and seed locks of each supported lock mode.
	locks map[LockMode]lockFactory

	// name identifies the engine in error messages.
	name string

	// currentSchemaSQL reads the schema used for unqualified table creation. When set, each
	// run pins a session and qualifies its history tables so migration settings cannot move
	// them or reach the connection pool.
	currentSchemaSQL string

	// historyReadModifier follows the history table in the applied-versions SELECT, such as
	// " FINAL" for a ClickHouse ReplacingMergeTree; empty for most engines.
	historyReadModifier string

	// rules are the engine's lexical rules. Unless a Custom spec supplies its own split,
	// they divide files into statements, and their RequiresNoTransaction predicate decides
	// which statements cannot run inside a transaction.
	rules sqllex.Rules

	// defaultLock is the lock mode used when WithLock is not given.
	defaultLock LockMode

	// transactionalDDL reports that DDL runs inside transactions, so a migration and its
	// history row commit together. When false every migration takes the non-transactional
	// path, which records progress unless appendOnlyHistory is set.
	transactionalDDL bool

	// transactionalSeeds reports that a seed's SQL and its history row can commit together
	// in one transaction. It is separate from transactionalDDL because seeds hold data: an
	// engine whose DDL commits implicitly can still make data and its record atomic.
	transactionalSeeds bool

	// appendOnlyHistory selects an append-only history for engines that cannot UPDATE rows
	// in place: an up migration records one completed row on success instead of the dirty
	// pre-record, per-statement progress and clear-dirty updates.
	appendOnlyHistory bool

	// multiStatementSeeds reports that the driver runs a whole seed file in one call;
	// otherwise each seed is split and run statement by statement.
	multiStatementSeeds bool
}

// requiresNoTransaction reports whether any statement cannot run inside a transaction
// under the engine's lexical rules.
//
// The predicate is host code on a Custom backend, so a panic in it stops the check: the
// stack is logged once through ctx's logger, and the panic is returned as a
// *sqllex.CallbackError, as sqllex reports a panic in any rules callback.
//
// Takes statements ([]string) which holds the statements of one file.
//
// Returns bool which is true when the rules' predicate matches a statement.
// Returns error which wraps sqllex.ErrRulesCallback when the predicate panicked.
func (e *engine) requiresNoTransaction(ctx context.Context, statements []string) (bool, error) {
	predicate := e.rules.RequiresNoTransaction
	if predicate == nil {
		return false, nil
	}
	for index, statement := range statements {
		required, err := callRulesPredicate(ctx, predicate, sqllex.SignificantTokens(e.rules, statement))
		if err != nil {
			return false, fmt.Errorf("checking statement %d of %d: %w", index+1, len(statements), err)
		}
		if required {
			return true, nil
		}
	}
	return false, nil
}

// lockFactory builds a lock mode's migration and seed locks for a backend's names.
//
// Takes names (Names) which holds the lock tables, constraints and keys.
// Takes serverWideKey (bool) which keeps a MySQL lock key server-wide rather than scoped
// to the current schema; other engines ignore it.
//
// Returns lockPair which holds the two locks.
type lockFactory func(names Names, serverWideKey bool) lockPair

// lockPair holds a backend's migration and seed locks.
type lockPair struct {
	// migration serialises migration runs.
	migration lockStrategy

	// seed serialises seed runs, keyed apart from the migration lock.
	seed lockStrategy
}

// namesFor derives every persisted object name from a namespace.
//
// Takes namespace (string) which is the namespace prefix.
//
// Returns Names which holds the derived table, constraint and lock-key names.
func namesFor(namespace string) Names {
	migrationLock := namespace + migrationLockTableSuffix
	seedLock := namespace + seedLockTableSuffix
	history := namespace + historyTableSuffix
	seeds := namespace + seedTableSuffix
	return Names{
		History:                 history,
		Seeds:                   seeds,
		MigrationLock:           migrationLock,
		SeedLock:                seedLock,
		MigrationLockConstraint: migrationLock + singleRowConstraintSuffix,
		SeedLockConstraint:      seedLock + singleRowConstraintSuffix,
		LockKey:                 history,
		SeedLockKey:             seeds,
	}
}

// validateIdentifier reports whether name is a safe SQL identifier that the migrator is
// willing to interpolate into DDL or advisory-lock keys.
//
// Takes name (string) which is the candidate identifier.
//
// Returns error which wraps ErrInvalidIdentifier when name fails the safe-identifier
// grammar.
func validateIdentifier(name string) error {
	if !safeIdentifierRegex.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidIdentifier, name)
	}
	return nil
}

// renderTable returns a renderer that interpolates one table name or bind-parameter
// marker into SQL.
//
// Takes format (string) which is the SQL with one %s.
//
// Returns func(string) string which renders the SQL.
func renderTable(format string) func(string) string {
	return func(table string) string {
		return fmt.Sprintf(format, table)
	}
}

// renderProbe returns a renderer that appends a bind-parameter marker to a query.
//
// Takes prefix (string) which is the query up to the marker.
//
// Returns func(string) string which renders the query for a marker.
func renderProbe(prefix string) func(string) string {
	return func(placeholder string) string {
		return prefix + placeholder
	}
}

// lexicalSplitter returns a splitter that divides SQL into statements under an engine's
// lexical rules, exactly as the engine's dialect does when it generates code: quoted
// tokens, comments, dollar-quoted bodies, procedural blocks and DELIMITER lines are all
// read as the rules describe them.
//
// Takes rules (sqllex.Rules) which are the engine's lexical rules.
//
// Returns func(context.Context, string) ([]string, error) which returns each statement's
// text, without its terminator or the whitespace and comments around it, and an error
// wrapping ErrMalformedStatement and the sqllex error when the text cannot be split. A
// panic in one of the rules' callbacks is not malformed SQL: its stack is logged once
// through ctx's logger, and its error wraps sqllex.ErrRulesCallback and not
// ErrMalformedStatement.
func lexicalSplitter(rules sqllex.Rules) func(ctx context.Context, content string) ([]string, error) {
	return func(ctx context.Context, content string) ([]string, error) {
		statements, err := sqllex.Split(withLoggedPanics(ctx, rules), content)
		if errors.Is(err, sqllex.ErrRulesCallback) {
			return nil, err
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrMalformedStatement, err)
		}
		texts := make([]string, len(statements))
		for index := range statements {
			texts[index] = statements[index].Text
		}
		return texts, nil
	}
}

// withLoggedPanics returns rules whose ProceduralBlock callback logs a panic once, with
// its stack, through ctx's logger and then panics again, so sqllex.Split still returns
// the panic as a *sqllex.CallbackError, which carries no stack.
//
// The stack is logged from the deferred function that recovered, so it still holds the
// frames that panicked.
//
// Takes rules (sqllex.Rules) which are the engine's lexical rules.
//
// Returns sqllex.Rules which are the same rules, with the callback wrapped.
func withLoggedPanics(ctx context.Context, rules sqllex.Rules) sqllex.Rules {
	procedural := rules.ProceduralBlock
	if procedural == nil {
		return rules
	}
	rules.ProceduralBlock = func(leadingWords []string) bool {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			logging.LogRecoveredPanic(ctx, "lexical rules callback "+proceduralBlockCallback, recovered)
			panic(recovered)
		}()
		return procedural(leadingWords)
	}
	return rules
}

// addColumnRenderer returns a renderer of the ALTER TABLE that adds a nullable history
// column with the type the map gives it.
//
// Takes columnTypes (map[string]string) which maps each added column to its type.
//
// Returns func(string, string) string which renders the DDL.
func addColumnRenderer(columnTypes map[string]string) func(table, column string) string {
	return func(table, column string) string {
		return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, columnTypes[column])
	}
}

// numberedPlaceholder renders PostgreSQL's $N bind-parameter markers.
//
// Takes index (int) which is the 1-based parameter index.
//
// Returns string which is the marker.
func numberedPlaceholder(index int) string {
	return fmt.Sprintf("$%d", index)
}

// postgresQualifiedTable renders a table qualified by a schema for the PostgreSQL family,
// quoting the schema so that a name such as "Accounts" or one holding a space or a quote
// names the schema it came from. The table is left unquoted, so PostgreSQL folds its
// case.
//
// Takes schema (string) which is the schema, as current_schema() returns it.
// Takes table (string) which is the validated table name.
//
// Returns string which is the qualified name.
func postgresQualifiedTable(schema, table string) string {
	return `"` + strings.ReplaceAll(schema, `"`, `""`) + `".` + table
}

// mysqlQualifiedTable renders a table qualified by a database for MySQL and MariaDB,
// quoting the database in backticks so that a name holding a backtick, a space or a dot
// names the database it came from. The table is left as written.
//
// Takes schema (string) which is the database, as DATABASE() returns it.
// Takes table (string) which is the validated table name.
//
// Returns string which is the qualified name.
func mysqlQualifiedTable(schema, table string) string {
	return "`" + strings.ReplaceAll(schema, "`", "``") + "`." + table
}

// mysqlTableExists renders MySQL's table probe, which takes a table named alone, in the
// session's current database, or qualified by a database as mysqlQualifiedTable writes
// it, since a run names its tables qualified by the database it started in. The marker is
// bound once, as the probe's one parameter.
//
// Takes tablePlaceholder (string) which is the bind-parameter marker for the table name.
//
// Returns string which is the query counting the matching tables.
func mysqlTableExists(tablePlaceholder string) string {
	return "SELECT count(*) FROM information_schema.tables AS candidate, (SELECT " + tablePlaceholder + " AS name) AS probe " +
		"WHERE (candidate.table_schema = DATABASE() AND candidate.table_name = probe.name) " +
		"OR CONCAT('`', REPLACE(candidate.table_schema, '`', '``'), '`.', candidate.table_name) = probe.name"
}

// questionPlaceholder renders the anonymous ? bind-parameter marker.
//
// Returns string which is always "?".
func questionPlaceholder(_ int) string {
	return "?"
}

// postgresOnConflictSeedInsert builds an idempotent INSERT statement using PostgreSQL's
// "ON CONFLICT (version) DO NOTHING" clause.
//
// Takes seedTable (string) which is the seed history table name.
// Takes versionPlaceholder (string) which is the parameter marker for the version column.
// Takes namePlaceholder (string) which is the parameter marker for the name column.
// Takes checksumPlaceholder (string) which is the parameter marker for the checksum
// column.
// Takes durationPlaceholder (string) which is the parameter marker for the duration_ms
// column.
//
// Returns string which is the complete INSERT statement.
func postgresOnConflictSeedInsert(
	seedTable, versionPlaceholder, namePlaceholder, checksumPlaceholder, durationPlaceholder string,
) string {
	return fmt.Sprintf(
		"INSERT INTO %s (version, name, checksum, duration_ms) VALUES (%s, %s, %s, %s) ON CONFLICT (version) DO NOTHING",
		seedTable, versionPlaceholder, namePlaceholder, checksumPlaceholder, durationPlaceholder,
	)
}

// sqliteOnConflictSeedInsert builds an idempotent INSERT statement using SQLite's "ON
// CONFLICT(version) DO NOTHING" clause.
//
// Takes seedTable (string) which is the seed history table name.
// Takes versionPlaceholder (string) which is the parameter marker for the version column.
// Takes namePlaceholder (string) which is the parameter marker for the name column.
// Takes checksumPlaceholder (string) which is the parameter marker for the checksum
// column.
// Takes durationPlaceholder (string) which is the parameter marker for the duration_ms
// column.
//
// Returns string which is the complete INSERT statement.
func sqliteOnConflictSeedInsert(
	seedTable, versionPlaceholder, namePlaceholder, checksumPlaceholder, durationPlaceholder string,
) string {
	return fmt.Sprintf(
		"INSERT INTO %s (version, name, checksum, duration_ms) VALUES (%s, %s, %s, %s) ON CONFLICT(version) DO NOTHING",
		seedTable, versionPlaceholder, namePlaceholder, checksumPlaceholder, durationPlaceholder,
	)
}

// mysqlIgnoreSeedInsert builds an idempotent INSERT using MySQL's INSERT IGNORE.
//
// Takes seedTable (string) which is the seed history table name.
// Takes versionPlaceholder (string) which is the parameter marker for the version column.
// Takes namePlaceholder (string) which is the parameter marker for the name column.
// Takes checksumPlaceholder (string) which is the parameter marker for the checksum
// column.
// Takes durationPlaceholder (string) which is the parameter marker for the duration_ms
// column.
//
// Returns string which is the complete INSERT statement.
func mysqlIgnoreSeedInsert(
	seedTable, versionPlaceholder, namePlaceholder, checksumPlaceholder, durationPlaceholder string,
) string {
	return fmt.Sprintf(
		"INSERT IGNORE INTO %s (version, name, checksum, duration_ms) VALUES (%s, %s, %s, %s)",
		seedTable, versionPlaceholder, namePlaceholder, checksumPlaceholder, durationPlaceholder,
	)
}

// clickHouseNotExistsSeedInsert builds an idempotent INSERT for ClickHouse engines.
//
// ClickHouse MergeTree tables do not enforce primary-key uniqueness and the engine has no
// equivalent of ON CONFLICT / INSERT IGNORE. The statement guards the INSERT by wrapping
// the values in a derived row source, then filtering the candidate row out when its
// version already exists in the seed table. A re-application of an already recorded seed
// inserts zero rows.
//
// Each placeholder appears exactly once in the rendered SQL so the call site can pass the
// same fixed positional argument tuple (version, name, checksum, duration_ms) as every
// other engine; ClickHouse anonymous markers bind in order.
//
// Two migrators racing between the guard's SELECT and the INSERT can still write two rows
// for one version. The seed table is a ReplacingMergeTree(applied_at) ordered by version,
// so such duplicates collapse to the latest row when parts merge.
//
// Takes seedTable (string) which is the seed history table name.
// Takes versionPlaceholder (string) which is the parameter marker for the version column.
// Takes namePlaceholder (string) which is the parameter marker for the name column.
// Takes checksumPlaceholder (string) which is the parameter marker for the checksum
// column.
// Takes durationPlaceholder (string) which is the parameter marker for the duration_ms
// column.
//
// Returns string which is the complete INSERT statement.
func clickHouseNotExistsSeedInsert(
	seedTable, versionPlaceholder, namePlaceholder, checksumPlaceholder, durationPlaceholder string,
) string {
	return fmt.Sprintf(
		"INSERT INTO %s (version, name, checksum, duration_ms) "+
			"SELECT candidate.version, candidate.name, candidate.checksum, candidate.duration_ms "+
			"FROM (SELECT %s AS version, %s AS name, %s AS checksum, %s AS duration_ms) AS candidate "+
			"WHERE candidate.version NOT IN (SELECT version FROM %s)",
		seedTable, versionPlaceholder, namePlaceholder, checksumPlaceholder, durationPlaceholder, seedTable,
	)
}

// clickHouseDeleteHistory builds the ClickHouse rollback statement for removing a single
// migration history row.
//
// The removal is an ALTER TABLE ... DELETE mutation run with mutations_sync = 2, so the
// statement returns only once every replica has applied it and an up or status that
// follows a rollback no longer sees the version as applied. Without the setting the
// mutation would run in the background.
//
// Takes historyTable (string) which is the validated history table identifier.
// Takes versionPlaceholder (string) which is the parameter marker for the version column.
//
// Returns string which is the complete ALTER TABLE ... DELETE statement.
func clickHouseDeleteHistory(historyTable, versionPlaceholder string) string {
	return fmt.Sprintf(
		"ALTER TABLE %s DELETE WHERE version = %s SETTINGS mutations_sync = 2",
		historyTable, versionPlaceholder,
	)
}

// clickHouseClearSeedHistory renders the statement that empties the ClickHouse seed
// history table.
//
// TRUNCATE is synchronous, unlike an ALTER TABLE ... DELETE mutation.
//
// Takes seedTable (string) which is the validated seed table identifier.
//
// Returns string which is the TRUNCATE statement.
func clickHouseClearSeedHistory(seedTable string) string {
	return "TRUNCATE TABLE " + seedTable
}

// postgresEngine returns the engine for PostgreSQL and TimescaleDB: $N placeholders,
// transactional DDL, and advisory, table or no locks, advisory by default.
//
// Returns *engine which describes PostgreSQL.
func postgresEngine() *engine {
	return &engine{
		name:               "PostgreSQL",
		placeholder:        numberedPlaceholder,
		createHistoryTable: renderTable(postgresHistoryTableDDL),
		createSeedTable:    renderTable(postgresSeedTableDDL),
		insertSeed:         postgresOnConflictSeedInsert,
		tableExists:        renderTable("SELECT CASE WHEN to_regclass(%s) IS NULL THEN 0 ELSE 1 END"),
		deleteHistory:      nil,
		clearSeedHistory:   nil,
		split:              lexicalSplitter(postgres_lexical.Rules()),
		addHistoryColumn:   addColumnRenderer(standardHistoryColumnTypes),
		qualifyTable:       postgresQualifiedTable,
		rules:              postgres_lexical.Rules(),
		ansiQuotesRules:    nil,
		locks: map[LockMode]lockFactory{
			LockAdvisory: postgresAdvisoryLocks,
			LockTable:    postgresTableLocks,
			LockNone:     noLocks,
		},
		currentSchemaSQL:    "SELECT current_schema()",
		historyReadModifier: "",
		defaultLock:         LockAdvisory,
		transactionalDDL:    true,
		transactionalSeeds:  true,
		appendOnlyHistory:   false,
		multiStatementSeeds: true,
	}
}

// timescaleDBEngine returns the engine for TimescaleDB: PostgreSQL's, read with
// TimescaleDB's lexical rules, so a migration that creates a continuous aggregate with
// data or calls refresh_continuous_aggregate runs without a transaction.
//
// Returns *engine which describes TimescaleDB.
func timescaleDBEngine() *engine {
	base := postgresEngine()
	base.name = "TimescaleDB"
	base.rules = timescaledb_lexical.Rules()
	base.split = lexicalSplitter(base.rules)
	return base
}

// postgresPgBouncerEngine returns the PostgreSQL engine with a table lock by default, for
// PgBouncer in transaction mode, where session-level advisory locks do not survive.
//
// Returns *engine which describes PostgreSQL behind PgBouncer.
func postgresPgBouncerEngine() *engine {
	base := postgresEngine()
	base.defaultLock = LockTable
	return base
}

// cockroachDBEngine returns the engine for CockroachDB: PostgreSQL's SQL with a durable
// table lock by default, since CockroachDB has no advisory lock functions, and without
// transactional DDL.
//
// CockroachDB does not make a schema change atomic with the rest of its transaction. From
// v25.1 its autocommit_before_ddl setting commits an open transaction before each schema
// change. With the setting off, a schema change that fails validation after COMMIT, such
// as a unique index over duplicate rows or a CHECK or NOT NULL that existing rows break,
// reports SQLSTATE XXA00 once the transaction's other writes, the history row among them,
// have committed. Every migration therefore takes the non-transactional path, which runs
// each statement on its own and records its progress, so a migration that fails part-way
// stays dirty and resumes after its last completed statement.
//
// Seeds still run in a transaction with their history row, since a seed holds data and
// CockroachDB commits data atomically. A seed file runs as one batch inside it. A seed
// that also changes the schema loses that atomicity, as autocommit_before_ddl commits
// what came before the schema change, but its history row is still written after it.
//
// Returns *engine which describes CockroachDB.
func cockroachDBEngine() *engine {
	base := postgresEngine()
	base.name = "CockroachDB"
	base.defaultLock = LockTable
	base.transactionalDDL = false
	base.locks = map[LockMode]lockFactory{
		LockTable: cockroachDBTableLocks,
		LockNone:  noLocks,
	}
	return base
}

// mysqlEngine returns the engine for MySQL and MariaDB: ? placeholders, statements split
// under MySQL's lexical rules (DELIMITER lines included), and GET_LOCK advisory locks by
// default.
//
// MySQL commits implicitly before and after every DDL statement, so a transaction cannot
// make a migration atomic: a failure part-way would leave the earlier statements applied
// and roll back only the history row. Every migration therefore takes the
// non-transactional path, which records its progress statement by statement, so a
// migration that fails part-way stays dirty and resumes after its last completed
// statement. Seeds hold data, which InnoDB commits atomically, so a seed and its history
// row still commit together.
//
// Seeds are split, since go-sql-driver/mysql runs one statement per call unless the DSN
// sets multiStatements; WithDriverMultiStatements runs them whole instead.
//
// A migration's USE moves its session to another database, so a run works on one session
// of its own, reads DATABASE() there before it runs anything and keeps its history and
// seed tables qualified by that database, as it does a PostgreSQL schema.
//
// Returns *engine which describes MySQL.
func mysqlEngine() *engine {
	return &engine{
		name:               "MySQL",
		placeholder:        questionPlaceholder,
		createHistoryTable: renderTable(mysqlHistoryTableDDL),
		createSeedTable:    renderTable(mysqlSeedTableDDL),
		insertSeed:         mysqlIgnoreSeedInsert,
		tableExists:        mysqlTableExists,
		deleteHistory:      nil,
		clearSeedHistory:   nil,
		split:              lexicalSplitter(mysql_lexical.Rules()),
		addHistoryColumn:   addColumnRenderer(standardHistoryColumnTypes),
		qualifyTable:       mysqlQualifiedTable,
		rules:              mysql_lexical.Rules(),
		ansiQuotesRules:    new(mysql_lexical.ANSIQuotesRules()),
		locks: map[LockMode]lockFactory{
			LockAdvisory: mysqlAdvisoryLocks,
			LockNone:     noLocks,
		},
		currentSchemaSQL:    "SELECT DATABASE()",
		historyReadModifier: "",
		defaultLock:         LockAdvisory,
		transactionalDDL:    false,
		transactionalSeeds:  true,
		appendOnlyHistory:   false,
		multiStatementSeeds: false,
	}
}

// clickHouseEngine returns the engine for ClickHouse.
//
// The backend uses no database lock or transactions, and keeps append-only history.
//
// Locking is a no-op because ClickHouse offers no cheap session-scoped advisory lock.
// Separate migrator instances are not serialised by a database lock; deployments that run
// migrations from more than one place must coordinate through a CI lock or a deployment
// gate.
//
// Transactions are disabled, so every migration runs on the non-transactional path.
//
// History is append-only. The history table is a ReplacingMergeTree(applied_at) ordered
// by version, so re-recording a version collapses to the latest row on merge or FINAL. Up
// migrations write one completed row on success; rollbacks remove the row with a
// synchronous ALTER TABLE ... DELETE mutation.
//
// Applied-versions reads use FINAL so duplicate version rows that have not yet merged are
// deduplicated at read time.
//
// The dirty column is a UInt8 holding 0 or 1. The seed history table is also a
// ReplacingMergeTree(applied_at) ordered by version, and the NOT IN guard of its INSERT
// avoids adding a history row for an already recorded seed.
//
// Returns *engine which describes ClickHouse.
func clickHouseEngine() *engine {
	return &engine{
		name:                "ClickHouse",
		placeholder:         questionPlaceholder,
		createHistoryTable:  renderTable(clickHouseHistoryTableDDL),
		createSeedTable:     renderTable(clickHouseSeedTableDDL),
		insertSeed:          clickHouseNotExistsSeedInsert,
		tableExists:         renderProbe("SELECT count() FROM system.tables WHERE database = currentDatabase() AND name = "),
		deleteHistory:       clickHouseDeleteHistory,
		clearSeedHistory:    clickHouseClearSeedHistory,
		split:               lexicalSplitter(clickhouse_lexical.Rules()),
		addHistoryColumn:    addColumnRenderer(clickHouseHistoryColumnTypes),
		qualifyTable:        nil,
		rules:               clickhouse_lexical.Rules(),
		ansiQuotesRules:     nil,
		locks:               map[LockMode]lockFactory{LockNone: noLocks},
		currentSchemaSQL:    "",
		historyReadModifier: " FINAL",
		defaultLock:         LockNone,
		transactionalDDL:    false,
		transactionalSeeds:  false,
		appendOnlyHistory:   true,
		multiStatementSeeds: false,
	}
}

// sqliteEngine returns the engine for SQLite: ? placeholders, transactional DDL and no
// lock, since SQLite serialises writers itself.
//
// Returns *engine which describes SQLite.
func sqliteEngine() *engine {
	return &engine{
		name:                "SQLite",
		placeholder:         questionPlaceholder,
		createHistoryTable:  renderTable(sqliteHistoryTableDDL),
		createSeedTable:     renderTable(sqliteSeedTableDDL),
		insertSeed:          sqliteOnConflictSeedInsert,
		tableExists:         renderProbe("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = "),
		deleteHistory:       nil,
		clearSeedHistory:    nil,
		split:               lexicalSplitter(sqlite_lexical.Rules()),
		addHistoryColumn:    addColumnRenderer(standardHistoryColumnTypes),
		qualifyTable:        nil,
		rules:               sqlite_lexical.Rules(),
		ansiQuotesRules:     nil,
		locks:               map[LockMode]lockFactory{LockNone: noLocks},
		currentSchemaSQL:    "",
		historyReadModifier: "",
		defaultLock:         LockNone,
		transactionalDDL:    true,
		transactionalSeeds:  true,
		appendOnlyHistory:   false,
		multiStatementSeeds: true,
	}
}

// duckDBTableExists renders DuckDB's table probe. information_schema.tables lists the
// tables of every attached database, so the probe keeps to the session's current database
// and schema, and it compares names without regard to case, as DuckDB resolves
// identifiers.
//
// Takes tablePlaceholder (string) which is the bind-parameter marker for the table name.
//
// Returns string which is the query counting the matching tables.
func duckDBTableExists(tablePlaceholder string) string {
	return "SELECT count(*) FROM information_schema.tables WHERE table_catalog = current_database() " +
		"AND table_schema = current_schema() AND lower(table_name) = lower(" + tablePlaceholder + ")"
}

// callRulesPredicate calls a rules' RequiresNoTransaction predicate, turning a panic in
// it into an error. The stack is logged once, from the deferred function that recovered,
// so it still holds the frames that panicked.
//
// Takes predicate (func([]sqllex.Token) bool) which is the rules' predicate.
// Takes tokens ([]sqllex.Token) which are one statement's significant tokens.
//
// Returns bool which is the predicate's answer, or false when it panicked.
// Returns error which is a *sqllex.CallbackError when the predicate panicked.
func callRulesPredicate(ctx context.Context, predicate func([]sqllex.Token) bool, tokens []sqllex.Token) (required bool, err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		logging.LogRecoveredPanic(ctx, "lexical rules callback "+requiresNoTransactionCallback, recovered)
		required, err = false, &sqllex.CallbackError{Value: recovered, Callback: requiresNoTransactionCallback}
	}()
	return predicate(tokens), nil
}
