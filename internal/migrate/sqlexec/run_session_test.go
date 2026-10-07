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
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
)

const sessionTestSchema = `tenant "one"`

var (
	postgresSession = sessionSchemaBackend{
		schemaSQL: postgresEngine().currentSchemaSQL, switchSession: "SET search_path TO elsewhere", qualifier: `"tenant ""one""".`,
	}
	mysqlSession = sessionSchemaBackend{
		schemaSQL: mysqlEngine().currentSchemaSQL, switchSession: "USE elsewhere", qualifier: "`tenant \"one\"`.",
	}
)

var sessionSchemaBackends = []sessionSchemaBackend{
	sessionBackend(postgresSession, "postgres", Postgres()),
	sessionBackend(postgresSession, "postgres without a lock", Postgres(WithLock(LockNone))),
	sessionBackend(postgresSession, "postgres behind pgbouncer", PostgresPgBouncer()),
	sessionBackend(postgresSession, "timescaledb", TimescaleDB()),
	sessionBackend(postgresSession, "cockroachdb", CockroachDB()),
	sessionBackend(postgresSession, "cockroachdb without a lock", CockroachDB(WithLock(LockNone))),
	sessionBackend(mysqlSession, "mysql", MySQL()),
	sessionBackend(mysqlSession, "mysql without a lock", MySQL(WithLock(LockNone))),
	sessionBackend(mysqlSession, "mysql with a server-wide lock key", MySQL(WithServerWideLockKey())),
}

type sessionStatement struct {
	query   string
	session int64
}

type sessionRecorder struct {
	schema     driver.Value
	closed     map[int64]bool
	statements []sessionStatement
	sessions   int64
	mu         sync.Mutex
}

func newSessionRecorder(schema driver.Value) *sessionRecorder {
	return &sessionRecorder{schema: schema, closed: map[int64]bool{}}
}

type sessionConnector struct {
	recorder *sessionRecorder
}

type sessionDriver struct{}

type sessionConn struct {
	recorder *sessionRecorder
	session  int64
}

type sessionTx struct{}

type sessionRows struct {
	values []driver.Value
}

type sessionSchemaBackend struct {
	backend       Backend
	name          string
	schemaSQL     string
	switchSession string
	qualifier     string
}

func (recorder *sessionRecorder) open() *sql.DB {
	return sql.OpenDB(sessionConnector{recorder: recorder})
}

func (recorder *sessionRecorder) record(session int64, query string) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.statements = append(recorder.statements, sessionStatement{query: query, session: session})
}

func (recorder *sessionRecorder) sessionOf(fragment string) (int64, bool) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for _, statement := range recorder.statements {
		if strings.Contains(statement.query, fragment) {
			return statement.session, true
		}
	}
	return 0, false
}

func (recorder *sessionRecorder) naming(table string) []string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	var named []string
	for _, statement := range recorder.statements {
		if strings.Contains(statement.query, table) && !strings.Contains(statement.query, "lock") &&
			!strings.HasPrefix(statement.query, "SELECT IF(CHAR_LENGTH(") {
			named = append(named, statement.query)
		}
	}
	return named
}

func (recorder *sessionRecorder) isClosed(session int64) bool {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.closed[session]
}

func (recorder *sessionRecorder) asked(query string) bool {
	_, found := recorder.sessionOf(query)
	return found
}

func (connector sessionConnector) Connect(context.Context) (driver.Conn, error) {
	connector.recorder.mu.Lock()
	defer connector.recorder.mu.Unlock()
	connector.recorder.sessions++
	return &sessionConn{recorder: connector.recorder, session: connector.recorder.sessions}, nil
}

func (sessionConnector) Driver() driver.Driver { return sessionDriver{} }

func (sessionDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("sessionDriver opens connections only through its connector")
}

func (*sessionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("sessionConn does not prepare statements")
}

func (conn *sessionConn) Close() error {
	conn.recorder.mu.Lock()
	defer conn.recorder.mu.Unlock()
	conn.recorder.closed[conn.session] = true
	return nil
}

func (*sessionConn) Begin() (driver.Tx, error) { return sessionTx{}, nil }

func (*sessionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return sessionTx{}, nil
}

func (conn *sessionConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	conn.recorder.record(conn.session, query)
	return driver.RowsAffected(0), nil
}

func (conn *sessionConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	conn.recorder.record(conn.session, query)
	switch {
	case query == postgresEngine().currentSchemaSQL || query == mysqlEngine().currentSchemaSQL:
		return &sessionRows{values: []driver.Value{conn.recorder.schema}}, nil
	case strings.HasPrefix(query, "SELECT IF(CHAR_LENGTH("):
		return &sessionRows{values: []driver.Value{"scoped lock"}}, nil
	case strings.Contains(query, "GET_LOCK(") || strings.Contains(query, "RELEASE_LOCK("):
		return &sessionRows{values: []driver.Value{int64(1)}}, nil
	case strings.Contains(query, "::REGCLASS::OID"):
		return &sessionRows{values: []driver.Value{int64(104)}}, nil
	case strings.Contains(query, "count(") || strings.Contains(query, "to_regclass"):
		return &sessionRows{values: []driver.Value{int64(0)}}, nil
	default:
		return &sessionRows{values: nil}, nil
	}
}

func (sessionTx) Commit() error   { return nil }
func (sessionTx) Rollback() error { return nil }

func (*sessionRows) Columns() []string { return []string{"value"} }
func (*sessionRows) Close() error      { return nil }

func (rows *sessionRows) Next(destination []driver.Value) error {
	if len(rows.values) == 0 {
		return io.EOF
	}
	destination[0] = rows.values[0]
	rows.values = rows.values[1:]
	return nil
}

func (backend sessionSchemaBackend) migration() migrate.MigrationRecord {
	return migrate.MigrationRecord{
		Name:     "move_along",
		Checksum: "checksum",
		Content:  []byte(backend.switchSession + ";\nCREATE TABLE widgets (id INT);\n"),
		Version:  1,
		SkipUpTo: -1,
	}
}

func (backend sessionSchemaBackend) seed() migrate.SeedRecord {
	return migrate.SeedRecord{
		Name:     "widgets",
		Checksum: "checksum",
		Content:  []byte(backend.switchSession + ";\nINSERT INTO widgets (id) VALUES (1);\n"),
		Version:  1,
	}
}

func sessionBackend(family sessionSchemaBackend, name string, backend Backend) sessionSchemaBackend {
	family.name, family.backend = name, backend
	return family
}

func TestAMigrationRunKeepsItsHistoryInTheSchemaItStartedIn(t *testing.T) {
	t.Parallel()

	for _, testCase := range sessionSchemaBackends {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			recorder := newSessionRecorder(sessionTestSchema)
			database := recorder.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, testCase.backend)
			require.NoError(t, err)

			require.NoError(t, executor.AcquireLock(t.Context()))
			require.NoError(t, executor.EnsureMigrationTable(t.Context()))
			require.NoError(t, executor.ExecuteMigration(t.Context(), testCase.migration(), migrate.MigrationDirectionUp, true))
			require.NoError(t, executor.ReleaseLock(t.Context()))

			history := recorder.naming("sgl_migrations")
			require.NotEmpty(t, history)
			for _, statement := range history {
				require.Contains(t, statement, testCase.qualifier+"sgl_migrations")
			}
			setSession, ok := recorder.sessionOf(testCase.switchSession)
			require.True(t, ok)
			for _, fragment := range []string{testCase.schemaSQL, "CREATE TABLE widgets", "INSERT INTO " + testCase.qualifier + "sgl_migrations"} {
				session, found := recorder.sessionOf(fragment)
				require.True(t, found, fragment)
				require.Equal(t, setSession, session, "%s runs on the run's one session", fragment)
			}
			require.True(t, recorder.isClosed(setSession), "the run's session is closed, not returned to the pool")
			require.Equal(t, "sgl_migrations", executor.historyTable(), "outside a run the table is named as the backend names it")
		})
	}
}

func TestASeedRunKeepsItsHistoryInTheSchemaItStartedIn(t *testing.T) {
	t.Parallel()

	for _, testCase := range sessionSchemaBackends {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			recorder := newSessionRecorder(sessionTestSchema)
			database := recorder.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewSeedExecutor(database, testCase.backend)
			require.NoError(t, err)

			require.NoError(t, executor.AcquireSeedLock(t.Context()))
			require.NoError(t, executor.EnsureSeedTable(t.Context()))
			_, err = executor.AppliedSeeds(t.Context())
			require.NoError(t, err)
			require.NoError(t, executor.ExecuteSeed(t.Context(), testCase.seed()))
			require.NoError(t, executor.ClearSeedHistory(t.Context()))
			require.NoError(t, executor.ReleaseSeedLock(t.Context()))

			history := recorder.naming("sgl_seeds")
			require.Len(t, history, 4)
			for _, statement := range history {
				require.Contains(t, statement, testCase.qualifier+"sgl_seeds")
			}
			setSession, ok := recorder.sessionOf(testCase.switchSession)
			require.True(t, ok)
			for _, fragment := range []string{testCase.schemaSQL, "INSERT INTO widgets", "INTO " + testCase.qualifier + "sgl_seeds"} {
				session, found := recorder.sessionOf(fragment)
				require.True(t, found, fragment)
				require.Equal(t, setSession, session, "%s runs on the run's one session", fragment)
			}
			require.True(t, recorder.isClosed(setSession), "the run's session is closed, not returned to the pool")
		})
	}
}

func TestASessionWithoutACurrentSchemaLeavesTheTablesUnqualified(t *testing.T) {
	t.Parallel()

	for _, testCase := range []sessionSchemaBackend{
		sessionBackend(postgresSession, "postgres", Postgres()),
		sessionBackend(mysqlSession, "mysql", MySQL()),
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			recorder := newSessionRecorder(nil)
			database := recorder.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, testCase.backend)
			require.NoError(t, err)

			require.NoError(t, executor.AcquireLock(t.Context()))
			require.NoError(t, executor.EnsureMigrationTable(t.Context()))
			require.NoError(t, executor.ReleaseLock(t.Context()))

			require.True(t, recorder.asked(testCase.schemaSQL))
			require.True(t, recorder.asked("CREATE TABLE IF NOT EXISTS sgl_migrations"))
		})
	}
}

func TestReadsOutsideARunNameTheTablesAsTheBackendDoes(t *testing.T) {
	t.Parallel()

	for _, testCase := range []sessionSchemaBackend{
		sessionBackend(postgresSession, "postgres", Postgres()),
		sessionBackend(mysqlSession, "mysql", MySQL()),
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			recorder := newSessionRecorder(sessionTestSchema)
			database := recorder.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, testCase.backend)
			require.NoError(t, err)
			seeds, err := NewSeedExecutor(database, testCase.backend)
			require.NoError(t, err)

			_, err = executor.AppliedVersions(t.Context())
			require.NoError(t, err)
			_, err = seeds.AppliedSeeds(t.Context())
			require.NoError(t, err)

			require.False(t, recorder.asked(testCase.schemaSQL))
			require.True(t, recorder.asked("FROM sgl_migrations ORDER BY version"))
			require.True(t, recorder.asked("FROM sgl_seeds ORDER BY version"))
		})
	}
}

func TestEnginesWithoutASessionSchemaRunAsBefore(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		backend Backend
		name    string
	}{
		{name: "sqlite", backend: SQLite()},
		{name: "clickhouse", backend: ClickHouse()},
		{name: "duckdb", backend: DuckDB()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			recorder := newSessionRecorder(sessionTestSchema)
			database := recorder.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, testCase.backend)
			require.NoError(t, err)
			seeds, err := NewSeedExecutor(database, testCase.backend)
			require.NoError(t, err)

			require.NoError(t, executor.AcquireLock(t.Context()))
			require.NoError(t, executor.EnsureMigrationTable(t.Context()))
			require.NoError(t, executor.ReleaseLock(t.Context()))
			require.NoError(t, seeds.AcquireSeedLock(t.Context()))
			require.NoError(t, seeds.EnsureSeedTable(t.Context()))
			require.NoError(t, seeds.ReleaseSeedLock(t.Context()))

			require.False(t, recorder.asked(postgresEngine().currentSchemaSQL))
			require.False(t, recorder.asked(mysqlEngine().currentSchemaSQL))
			require.True(t, recorder.asked("CREATE TABLE IF NOT EXISTS sgl_migrations"))
			require.True(t, recorder.asked("CREATE TABLE IF NOT EXISTS sgl_seeds"))
			require.Nil(t, executor.held)
		})
	}
}

func TestAFailedSchemaReadReleasesTheLock(t *testing.T) {
	t.Parallel()

	strategy := &pinningLock{}
	executor, _ := executorWithLock(t, strategy)
	broken := *postgresEngine()
	broken.currentSchemaSQL = "SELECT unknown_schema_function()"
	executor.backend.engine = &broken

	err := executor.AcquireLock(t.Context())

	require.ErrorContains(t, err, "preparing the migration session")
	require.Nil(t, executor.held)
	require.Equal(t, int32(1), strategy.releases.Load())
	executor.backend.engine = postgresEngine()
	require.NoError(t, executor.TryAcquireLock(t.Context()), "a failed acquisition must free the in-process slot")
	require.NoError(t, executor.ReleaseLock(t.Context()))
}
