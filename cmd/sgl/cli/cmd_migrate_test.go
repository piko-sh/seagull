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

package cli_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/cmd/sgl/internal/clitest"
)

const leakyDriverName = "sgl-test-leaky"

const duckDBStandInDriverName = "sgl-test-duckdb"

const duckDBTableProbe = "SELECT count(*) FROM information_schema.tables WHERE table_catalog = current_database() " +
	"AND table_schema = current_schema() AND lower(table_name) = lower($1)"

var errLeakyDriver = errors.New("leaky driver refused the connection")

var (
	duckDBStandIns       = map[string]*duckDBStandInStore{}
	duckDBStandInsMutex  sync.Mutex
	standInCreatePattern = regexp.MustCompile(`^CREATE TABLE IF NOT EXISTS ([A-Za-z_]+)`)
	standInReadPattern   = regexp.MustCompile(`FROM ([A-Za-z_]+) ORDER BY version`)
)

type leakyDriver struct{}

type leakyConnector struct {
	dsn string
}

type duckDBStandInStore struct {
	tables     map[string]bool
	statements []string
	mu         sync.Mutex
}

type duckDBStandInDriver struct{}

type standInConnection struct {
	store *duckDBStandInStore
}

type standInTx struct{}

type standInRows struct {
	columns []string
	values  [][]driver.Value
}

func (leakyDriver) Open(dsn string) (driver.Conn, error) {
	return nil, fmt.Errorf("%w: dsn %s, password %s", errLeakyDriver, dsn, "hunter2")
}

func (leakyDriver) OpenConnector(dsn string) (driver.Connector, error) {
	return leakyConnector{dsn: dsn}, nil
}

func (connector leakyConnector) Connect(context.Context) (driver.Conn, error) {
	return leakyDriver{}.Open(connector.dsn)
}

func (leakyConnector) Driver() driver.Driver {
	return leakyDriver{}
}

func (s *duckDBStandInStore) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.statements...)
}

func (s *duckDBStandInStore) exec(query string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statements = append(s.statements, query)
	if match := standInCreatePattern.FindStringSubmatch(query); match != nil {
		s.tables[match[1]] = true
	}
}

func (s *duckDBStandInStore) query(query string, arguments []driver.NamedValue) (driver.Rows, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statements = append(s.statements, query)
	if query == duckDBTableProbe {
		count := int64(0)
		if name, ok := arguments[0].Value.(string); ok && s.tables[name] {
			count = 1
		}
		return &standInRows{columns: []string{"count"}, values: [][]driver.Value{{count}}}, nil
	}
	if match := standInReadPattern.FindStringSubmatch(query); match != nil && !s.tables[match[1]] {
		return nil, errors.New("Catalog Error: Table with name " + match[1] + " does not exist!")
	}
	return &standInRows{columns: []string{"version"}, values: nil}, nil
}

func (duckDBStandInDriver) Open(dsn string) (driver.Conn, error) {
	return &standInConnection{store: duckDBStandIn(dsn)}, nil
}

func (*standInConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("the DuckDB stand-in prepares no statements")
}

func (*standInConnection) Close() error { return nil }

func (*standInConnection) Begin() (driver.Tx, error) { return standInTx{}, nil }

func (*standInConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return standInTx{}, nil
}

func (connection *standInConnection) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	connection.store.exec(query)
	return driver.RowsAffected(0), nil
}

func (connection *standInConnection) QueryContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	return connection.store.query(query, arguments)
}

func (standInTx) Commit() error { return nil }

func (standInTx) Rollback() error { return nil }

func (rows *standInRows) Columns() []string { return rows.columns }

func (*standInRows) Close() error { return nil }

func (rows *standInRows) Next(destination []driver.Value) error {
	if len(rows.values) == 0 {
		return io.EOF
	}
	copy(destination, rows.values[0])
	rows.values = rows.values[1:]
	return nil
}

func databaseProject(t *testing.T) (*clitest.Project, string) {
	t.Helper()
	project := sqliteProject(t)
	return project, "file:" + project.Path("app.db")
}

func TestMigrateLifecycle(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	requireCode(t, project.Run("migrate", "new", "Add Books!", "--dsn", dsn), 0)
	assert.True(t, project.Exists("db/migrations/0002_add_books.up.sql"))
	assert.True(t, project.Exists("db/migrations/0002_add_books.down.sql"))
	project.Write("db/migrations/0002_add_books.up.sql", "CREATE TABLE books (id INTEGER PRIMARY KEY, title TEXT NOT NULL);\n")
	project.Write("db/migrations/0002_add_books.down.sql", "DROP TABLE books;\n")

	result := project.Run("migrate", "status", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "1        init       pending  -")

	result = project.Run("migrate", "up", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "up 1 init\nup 2 add_books\n2 applied\n", result.Stdout)

	result = project.Run("migrate", "--dsn", dsn, "status")
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "add_books  applied  20")
	assert.NotContains(t, result.Stdout, "pending")

	requireCode(t, project.Run("migrate", "validate", "--dsn", dsn), 0)

	result = project.Run("migrate", "down", "--dsn", dsn)
	requireCode(t, result, 0)
	require.Equal(t, "down 2 add_books\n1 rolled back\n", result.Stdout)

	result = project.Run("migrate", "to", "2", "--dsn", dsn)
	requireCode(t, result, 0)
	require.Equal(t, "up 2 add_books\n1 applied\n", result.Stdout)

	result = project.Run("migrate", "to", "2", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "already at version 2\n", result.Stdout)

	result = project.Run("migrate", "to", "0", "--dsn", dsn)
	requireCode(t, result, 0)
	require.Equal(t, "down 2 add_books\ndown 1 init\n2 rolled back\n", result.Stdout)

	requireCode(t, project.Run("migrate", "up", "--dsn", dsn), 0)
	result = project.Run("migrate", "down", "2", "--dsn", dsn)
	requireCode(t, result, 0)
	require.Equal(t, "down 2 add_books\ndown 1 init\n2 rolled back\n", result.Stdout)
}

func TestMigrateValidateReportsChangedMigrations(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	requireCode(t, project.Run("migrate", "up", "--dsn", dsn), 0)
	project.Write("db/migrations/0001_init.up.sql", project.Read("db/migrations/0001_init.up.sql")+"-- edited\n")

	result := project.Run("migrate", "validate", "--dsn", dsn)
	requireCode(t, result, 1)
	assert.Contains(t, result.Stderr, "checksum")

	result = project.Run("migrate", "status", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "changed")
}

func TestMigrateFailureIsADatabaseError(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	project.Write("db/migrations/0002_bad.up.sql", "CREATE TABLE authors (id INTEGER);\n")
	result := project.Run("migrate", "up", "--dsn", dsn)
	requireCode(t, result, 4)
	assert.Contains(t, result.Stderr, "already exists")
}

func TestMigrateArgumentErrors(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	tests := [][]string{
		{"migrate"},
		{"migrate", "sideways", "--dsn", dsn},
		{"migrate", "up", "extra", "--dsn", dsn},
		{"migrate", "status", "extra", "--dsn", dsn},
		{"migrate", "down", "1", "2", "--dsn", dsn},
		{"migrate", "down", "zero", "--dsn", dsn},
		{"migrate", "down", "-1", "--dsn", dsn},
		{"migrate", "to", "--dsn", dsn},
		{"migrate", "to", "two", "--dsn", dsn},
		{"migrate", "new"},
		{"migrate", "new", "!!!"},
		{"migrate", "up"},
	}
	for _, arguments := range tests {
		result := project.Run(arguments...)
		assert.Equal(t, 2, result.Code, "%v: %s", arguments, result.Stderr)
	}
}

func TestMigrateNewRefusesToOverwrite(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/migrations/0002_taken.up.sql", "")
	project.Write("db/migrations/0002_taken.down.sql", "")
	project.Write("db/migrations/README.txt", "")
	result := project.Run("migrate", "new", "taken")
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "0003_taken.up.sql")

	empty := clitest.NewProject(t)
	empty.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: sqlite\n    migrations: sql/changes\n")
	requireCode(t, empty.Run("migrate", "new", "first"), 0)
	assert.True(t, empty.Exists("sql/changes/0001_first.up.sql"))
}

func TestMigrateConnectionStringSources(t *testing.T) {
	project, dsn := databaseProject(t)

	t.Setenv("SGL_DATABASE_URL", dsn)
	requireCode(t, project.Run("migrate", "up"), 0)

	t.Setenv("SGL_DATABASE_URL", "")
	t.Setenv("SGL_TEST_MIGRATE_DSN", "file:"+project.Path("other.db"))
	project.Write("sgl.yaml", strings.Replace(project.Read("sgl.yaml"), "    seeds: db/seeds\n",
		"    seeds: db/seeds\n    migrate:\n      dsn: ${env:SGL_TEST_MIGRATE_DSN}\n      namespace: app\n      history_table: app_history\n      lock: none\n", 1))
	result := project.Run("migrate", "status")
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "pending")
	requireCode(t, project.Run("migrate", "up"), 0)
}

func TestMigrateDatabaseErrorsRedactTheConnectionString(t *testing.T) {
	t.Parallel()

	project := clitest.NewProject(t)
	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: postgres\n")
	result := project.Run("migrate", "status", "--dsn", "postgres://admin:hunter2@127.0.0.1:1/app?connect_timeout=2")
	requireCode(t, result, 4)
	assert.NotContains(t, result.Stderr, "hunter2")
	assert.Contains(t, result.Stderr, "xxxxx")
}

func TestDriverErrorsRedactTheConnectionStringAndItsPassword(t *testing.T) {
	t.Parallel()

	project := clitest.NewProject(t)
	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: sqlite\n")
	options := []cli.Option{cli.WithDriver("sqlite", leakyDriverName)}

	for _, dsn := range []string{
		"postgres://admin:hunter2@db.internal/app",
		"admin:hunter2@tcp(db.internal:3306)/app",
		"host=db.internal user=admin password='hunter2' dbname=app",
		"postgres://admin@db.internal/app?password=hunter2",
	} {
		result := project.RunWith(t.Context(), options, "migrate", "status", "--dsn", dsn)
		requireCode(t, result, 4)
		assert.NotContains(t, result.Stderr, "hunter2", dsn)
		assert.Contains(t, result.Stderr, "xxxxx", dsn)
	}
}

func TestMigrateRejectsUnsupportedDatabases(t *testing.T) {
	t.Parallel()

	project := clitest.NewProject(t)
	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: duckdb\n")
	result := project.Run("migrate", "status", "--dsn", "ignored")
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "no DuckDB driver")

	project.Write("sgl.yaml", "databases:\n  - name: one\n    dialect: sqlite\n    go: { out: gen/one }\n  - name: two\n    dialect: sqlite\n    go: { out: gen/two }\n")
	result = project.Run("migrate", "status", "--dsn", "file:x.db")
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "choose one with --database")

	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: sqlite\n    migrate: { lock: advisory }\n")
	result = project.Run("migrate", "status", "--dsn", "file:"+project.Path("x.db"))
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "lock")
}

func init() {
	sql.Register(leakyDriverName, leakyDriver{})
}

func TestMigrateUpRefusesAnUpMigrationHoldingAnotherToolsDownSection(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	project.Write("db/migrations/0002_boats.up.sql", "CREATE TABLE boats (id INTEGER PRIMARY KEY);\n-- +goose Down\nDROP TABLE boats;\n")

	result := project.Run("migrate", "up", "--dsn", dsn)

	requireCode(t, result, 4)
	require.Contains(t, result.Stderr, "0002_boats.up.sql holds a goose down section from line 2 (-- +goose Down)")
	require.Contains(t, result.Stderr, "move the statements from line 2 on into 0002_boats.down.sql")
	status := project.Run("migrate", "status", "--dsn", dsn)
	requireCode(t, status, 0)
	require.Regexp(t, `(?m)^1\s+init\s+pending`, status.Stdout)
	require.Regexp(t, `(?m)^2\s+boats\s+pending`, status.Stdout)
}

func TestMigrateNeedsItsMigrationDirectory(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		migrations string
		subcommand string
		stderr     string
	}{
		{name: "up, the default directory", subcommand: "up",
			stderr: `database "app": the migration directory db/migrations does not exist; create it, or set migrations in sgl.yaml to the directory that holds them`},
		{name: "status, the default directory", subcommand: "status",
			stderr: `database "app": the migration directory db/migrations does not exist; create it, or set migrations in sgl.yaml to the directory that holds them`},
		{name: "up, a named directory", migrations: "    migrations: db/schema\n", subcommand: "up",
			stderr: `database "app": the migration directory db/schema does not exist; create it, or correct migrations in sgl.yaml`},
		{name: "validate, a named directory", migrations: "    migrations: db/schema\n", subcommand: "validate",
			stderr: `database "app": the migration directory db/schema does not exist; create it, or correct migrations in sgl.yaml`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			project, dsn := databaseProject(t)
			require.NoError(t, os.RemoveAll(project.Path("db/migrations")))
			project.Write("sgl.yaml", "version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n"+testCase.migrations+
				"    go:\n      package: store\n      out: internal/store\n")

			result := project.Run("migrate", testCase.subcommand, "--dsn", dsn)

			requireCode(t, result, 2)
			require.Contains(t, result.Stderr, testCase.stderr)
			require.NotContains(t, result.Stderr, "open .")
		})
	}
}

func TestAnUnsetVariableFailsOnlyTheCommandsThatUseIt(t *testing.T) {
	t.Parallel()

	const unset = "SGL_TEST_VARIABLE_THAT_IS_NEVER_SET"
	tests := []struct {
		name      string
		setting   string
		arguments []string
		code      int
		message   string
	}{
		{name: "generate with an unset schema_from", setting: "    schema_from: ${env:" + unset + "}\n", arguments: []string{"generate"}, code: 2,
			message: `database "app": unset environment variable: schema_from refers to ${env:` + unset + `}, which is not set`},
		{name: "migrate with an unset migrate.dsn", setting: "    migrate:\n      dsn: ${env:" + unset + "}\n", arguments: []string{"migrate", "status"}, code: 2,
			message: `database "app": unset environment variable: migrate.dsn refers to ${env:` + unset + `}, which is not set; pass --dsn or set SGL_DATABASE_URL instead`},
		{name: "generate with an unset migrate.dsn it never uses", setting: "    migrate:\n      dsn: ${env:" + unset + "}\n", arguments: []string{"generate"}, code: 0},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			project := sqliteProject(t)
			project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: sqlite\n"+testCase.setting)

			result := project.RunWith(t.Context(), nil, testCase.arguments...)

			requireCode(t, result, testCase.code)
			assert.Contains(t, result.Stderr, testCase.message)
		})
	}
}

func duckDBProject(t *testing.T) (*clitest.Project, []cli.Option, string) {
	t.Helper()
	project := clitest.NewProject(t)
	requireCode(t, project.Run("init", "--dialect", "duckdb"), 0)
	return project, []cli.Option{cli.WithDriver("duckdb", duckDBStandInDriverName)}, "duckdb-standin:" + t.Name()
}

func TestMigrateAndSeedRunTheDuckDBBackend(t *testing.T) {
	t.Parallel()

	project, options, dsn := duckDBProject(t)
	ctx := t.Context()

	result := project.RunWith(ctx, options, "migrate", "status", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "1        init  pending  -")

	result = project.RunWith(ctx, options, "migrate", "up", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "up 1 init\n1 applied\n", result.Stdout)

	result = project.RunWith(ctx, options, "seed", "apply", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "1 applied\n", result.Stdout)

	statements := duckDBStandIn(dsn).recorded()
	assert.Contains(t, statements, duckDBTableProbe, "a missing history table is found with the DuckDB probe")
	assert.Contains(t, statements, project.Read("db/seeds/0001_authors.sql"), "duckdb-go runs a whole seed file in one call")
	assert.Contains(t, statements, "INSERT INTO sgl_migrations (version, name, checksum, applied_at, duration_ms, down_checksum, last_statement, dirty, "+
		"splitter_version, statements_hash) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)")
	assert.Contains(t, statements, "INSERT INTO sgl_seeds (version, name, checksum, duration_ms) VALUES ($1, $2, $3, $4) ON CONFLICT (version) DO NOTHING")
	historyDDL := statementStartingWith(statements, "CREATE TABLE IF NOT EXISTS sgl_migrations")
	assert.Contains(t, historyDDL, "applied_at     TIMESTAMPTZ NOT NULL DEFAULT current_timestamp")
	for _, statement := range statements {
		assert.NotRegexp(t, `pg_advisory|GET_LOCK|FOR UPDATE|_lock\b`, statement, "DuckDB takes no lock")
	}
}

func TestMigrateDuckDBHonoursTheConfiguredNames(t *testing.T) {
	t.Parallel()

	project, options, dsn := duckDBProject(t)
	project.Write("sgl.yaml", strings.Replace(project.Read("sgl.yaml"), "    seeds: db/seeds\n",
		"    seeds: db/seeds\n    migrate:\n      namespace: app\n      history_table: app_history\n      lock: none\n", 1))

	requireCode(t, project.RunWith(t.Context(), options, "migrate", "up", "--dsn", dsn), 0)

	statements := duckDBStandIn(dsn).recorded()
	assert.NotEmpty(t, statementStartingWith(statements, "CREATE TABLE IF NOT EXISTS app_history"))
	for _, statement := range statements {
		assert.NotContains(t, statement, "sgl_", "statement %q used a default name", statement)
	}
}

func TestMigrateDuckDBRejectsALock(t *testing.T) {
	t.Parallel()

	for _, lock := range []string{"advisory", "table"} {
		project, options, dsn := duckDBProject(t)
		project.Write("sgl.yaml", strings.Replace(project.Read("sgl.yaml"), "    seeds: db/seeds\n",
			"    seeds: db/seeds\n    migrate:\n      lock: "+lock+"\n", 1))

		result := project.RunWith(t.Context(), options, "migrate", "status", "--dsn", dsn)
		requireCode(t, result, 2)
		assert.Contains(t, result.Stderr, "lock mode not supported", lock)
		assert.Contains(t, result.Stderr, lock+" on DuckDB", lock)
	}
}

func TestDuckDBCannotBeIntrospected(t *testing.T) {
	t.Parallel()

	project, options, dsn := duckDBProject(t)
	ctx := t.Context()

	result := project.RunWith(ctx, options, "schema", "dump", "--from-db", dsn)
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "dialect duckdb cannot be introspected; build the schema from migrations instead")

	result = project.RunWith(ctx, options, "schema", "diff", "--dsn", dsn)
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "dialect duckdb cannot be introspected")

	result = project.RunWith(ctx, options, "schema", "dump")
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, `"name": "authors"`, "the schema still comes from the migrations")
}

func statementStartingWith(statements []string, prefix string) string {
	for _, statement := range statements {
		if strings.HasPrefix(statement, prefix) {
			return statement
		}
	}
	return ""
}

func duckDBStandIn(dsn string) *duckDBStandInStore {
	duckDBStandInsMutex.Lock()
	defer duckDBStandInsMutex.Unlock()
	store, found := duckDBStandIns[dsn]
	if !found {
		store = &duckDBStandInStore{tables: map[string]bool{}, statements: nil}
		duckDBStandIns[dsn] = store
	}
	return store
}

func init() {
	sql.Register(duckDBStandInDriverName, duckDBStandInDriver{})
}

func TestDuckDBStandInReportsAMissingHistoryTable(t *testing.T) {
	t.Parallel()

	store := duckDBStandIn("duckdb-standin:" + t.Name())
	_, err := store.query("SELECT version FROM sgl_migrations ORDER BY version", nil)
	require.ErrorContains(t, err, "does not exist")
	store.exec("CREATE TABLE IF NOT EXISTS sgl_migrations (version BIGINT)")
	rows, err := store.query("SELECT version FROM sgl_migrations ORDER BY version", nil)
	require.NoError(t, err)
	require.ErrorIs(t, rows.Next(make([]driver.Value, 1)), io.EOF)
}
