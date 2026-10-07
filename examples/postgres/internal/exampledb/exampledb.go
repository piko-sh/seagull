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

// Package exampledb gives each PostgreSQL example's test a fresh database of its own,
// migrated with the example's migrations, and checks a program's output against the
// expected output.txt beside it.
//
// The tests need a server, which three environment variables describe:
//
//   - SGL_EXAMPLE_DSN is a connection string for a role that may create databases, for
//     example postgres://postgres:sgl@127.0.0.1:5432/postgres?sslmode=disable. Each test
//     creates a database with a unique name on that server and drops it when it ends.
//   - SGL_EXAMPLE_ENGINE names the server: postgres (the default), cockroachdb or
//     timescaledb. It picks the migration backend (migrate_backend_postgres.New, or
//     migrate_backend_cockroachdb.New for cockroachdb), so the same examples can run
//     against the engines whose cells in concepts.yaml are equivalent to PostgreSQL's.
//   - SGL_EXAMPLE_REQUIRE_DSN=1 fails a test that has no DSN instead of skipping it.
//
// A test that calls any of these helpers also fails when a goroutine is still running
// once it and its clean-ups have finished.
//
// Run the tests with -update to rewrite output.txt from the program's current output.
package exampledb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"seagull.sh/seagull/internal/leakcheck"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_cockroachdb"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_postgres"
)

const (
	// OutputFile holds a program's expected output, beside its main.go.
	OutputFile = "output.txt"

	// EnginePostgres is the default engine: PostgreSQL itself.
	EnginePostgres = "postgres"

	// EngineCockroachDB runs the examples against CockroachDB.
	EngineCockroachDB = "cockroachdb"

	// EngineTimescaleDB runs the examples against TimescaleDB.
	EngineTimescaleDB = "timescaledb"

	// DSNVariable names the server the tests create their databases on.
	DSNVariable = "SGL_EXAMPLE_DSN"

	// EngineVariable names the engine behind DSNVariable.
	EngineVariable = "SGL_EXAMPLE_ENGINE"

	// RequireVariable names the environment variable that, set to 1, fails a test with no
	// DSN instead of skipping it.
	RequireVariable = "SGL_EXAMPLE_REQUIRE_DSN"

	// migrationsDirectory is where every example keeps its migrations.
	migrationsDirectory = "db/migrations"

	// outputPermissions is the mode -update creates a missing output.txt with; an existing
	// one keeps its own.
	outputPermissions = 0o600

	// driverName is the name pgx registers its database/sql driver under.
	driverName = "pgx"

	// databasePrefix starts the name of every database the tests create.
	databasePrefix = "sgl_example_"

	// suffixBytes is how many random bytes end a database's name, as twice as many hex
	// digits.
	suffixBytes = 8

	// dropTimeout bounds dropping a test's database once the test has ended.
	dropTimeout = 30 * time.Second
)

var (
	// update rewrites output.txt instead of comparing against it.
	update = flag.Bool("update", false, "rewrite output.txt from the program's output")

	// leakChecked holds each test whose goroutines are checked once it ends, so a test that
	// calls several helpers registers the check only once.
	leakChecked sync.Map
)

// Open returns a fresh database with the example's migrations applied, through pgx's
// database/sql driver. It is closed and dropped when the test ends.
//
// Takes t (testing.TB) which is the test.
// Takes migrations (fs.FS) which holds db/migrations, usually the example's embed.FS.
//
// Returns *sql.DB which is the migrated database.
func Open(t testing.TB, migrations fs.FS) *sql.DB {
	t.Helper()
	checkLeaks(t)
	database := OpenEmpty(t)
	Migrate(t, database, migrations)
	return database
}

// OpenPool returns a pgxpool pool on a fresh database with the example's migrations
// applied. The migrations run on the pool through stdlib.OpenDBFromPool, as a pgx program
// would run them. The pool is closed and the database dropped when the test ends.
//
// Takes t (testing.TB) which is the test.
// Takes migrations (fs.FS) which holds db/migrations, usually the example's embed.FS.
//
// Returns *pgxpool.Pool which is the pool on the migrated database.
func OpenPool(t testing.TB, migrations fs.FS) *pgxpool.Pool {
	t.Helper()
	checkLeaks(t)
	pool, err := pgxpool.New(t.Context(), NewDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	database := stdlib.OpenDBFromPool(pool)
	defer func() { _ = database.Close() }()
	Migrate(t, database, migrations)
	return pool
}

// OpenEmpty returns a fresh database with no migrations applied, for examples that
// migrate it themselves. It is closed and dropped when the test ends.
//
// Takes t (testing.TB) which is the test.
//
// Returns *sql.DB which is the empty database.
func OpenEmpty(t testing.TB) *sql.DB {
	t.Helper()
	checkLeaks(t)
	database, err := sql.Open(driverName, NewDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

// NewDatabase creates an empty database with a unique name on the server SGL_EXAMPLE_DSN
// names, and returns a connection string for it. The test is skipped when there is no
// DSN, unless SGL_EXAMPLE_REQUIRE_DSN is 1. The database is dropped when the test ends,
// after the cleanups registered later have run, so close any connection you open on it
// with t.Cleanup.
//
// Takes t (testing.TB) which is the test.
//
// Returns string which is the connection string for the new database.
func NewDatabase(t testing.TB) string {
	t.Helper()
	checkLeaks(t)
	serverDSN := os.Getenv(DSNVariable)
	if serverDSN == "" {
		if os.Getenv(RequireVariable) == "1" {
			t.Fatalf("%s is empty and %s=1", DSNVariable, RequireVariable)
		}
		t.Skipf("set %s to a PostgreSQL server to run this example", DSNVariable)
	}
	engine := Engine(t)
	name := uniqueName(t)
	dsn, err := databaseDSN(serverDSN, name)
	if err != nil {
		t.Fatal(err)
	}
	if err := execOnServer(t.Context(), serverDSN, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("creating database %s: %v", name, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), dropTimeout)
		defer cancel()
		if err := execOnServer(ctx, serverDSN, dropStatement(engine, name)); err != nil {
			t.Errorf("dropping database %s: %v", name, err)
		}
	})
	return dsn
}

// Migrate applies the migrations in db/migrations to database with the backend for
// SGL_EXAMPLE_ENGINE.
//
// Takes t (testing.TB) which is the test.
// Takes database (*sql.DB) which is the database to migrate.
// Takes migrations (fs.FS) which holds db/migrations.
func Migrate(t testing.TB, database *sql.DB, migrations fs.FS) {
	t.Helper()
	checkLeaks(t)
	migrator, err := migrate.NewMigrator(database, Backend(t), migrations, migrationsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Up(t.Context()); err != nil {
		t.Fatalf("migrating: %v", err)
	}
}

// Backend returns the migration backend for SGL_EXAMPLE_ENGINE:
// migrate_backend_cockroachdb.New for cockroachdb, and migrate_backend_postgres.New for
// postgres and timescaledb.
//
// Takes t (testing.TB) which is the test.
// Takes options (...migrate.BackendOption) which are passed to the backend.
//
// Returns migrate.Backend which is the backend.
func Backend(t testing.TB, options ...migrate.BackendOption) migrate.Backend {
	t.Helper()
	checkLeaks(t)
	if Engine(t) == EngineCockroachDB {
		return migrate_backend_cockroachdb.New(options...)
	}
	return migrate_backend_postgres.New(options...)
}

// Engine returns the engine named by SGL_EXAMPLE_ENGINE, or postgres when it is empty.
// The test fails when it names an engine this module cannot run.
//
// Takes t (testing.TB) which is the test.
//
// Returns string which is EnginePostgres, EngineCockroachDB or EngineTimescaleDB.
func Engine(t testing.TB) string {
	t.Helper()
	checkLeaks(t)
	engine := os.Getenv(EngineVariable)
	switch engine {
	case "":
		return EnginePostgres
	case EnginePostgres, EngineCockroachDB, EngineTimescaleDB:
		return engine
	default:
		t.Fatalf("%s=%q, want %s, %s or %s", EngineVariable, engine, EnginePostgres, EngineCockroachDB, EngineTimescaleDB)
		return ""
	}
}

// MatchOutput compares a program's output with output.txt in the working directory, or
// rewrites output.txt when the tests run with -update.
//
// Takes t (testing.TB) which is the test.
// Takes got (string) which is the program's output.
func MatchOutput(t testing.TB, got string) {
	t.Helper()
	checkLeaks(t)
	if *update {
		if err := os.WriteFile(OutputFile, []byte(got), outputPermissions); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(OutputFile)
	if err != nil {
		t.Fatalf("reading %s (run the tests with -update to create it): %v", OutputFile, err)
	}
	if got != string(want) {
		t.Fatalf("output differs from %s (run with -update if the change is intended)\n--- got\n%s--- want\n%s", OutputFile, got, want)
	}
}

// uniqueName returns a database name no other test will use.
//
// Takes t (testing.TB) which is the test.
//
// Returns string which is the name, safe to use unquoted.
func uniqueName(t testing.TB) string {
	t.Helper()
	suffix := make([]byte, suffixBytes)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	return databasePrefix + hex.EncodeToString(suffix)
}

// databaseDSN returns serverDSN pointed at the database called name. It accepts both URL
// and keyword/value connection strings.
//
// Takes serverDSN (string) which is the server's connection string.
// Takes name (string) which is the database.
//
// Returns string which is the database's connection string.
// Returns error when serverDSN is not a valid URL.
func databaseDSN(serverDSN, name string) (string, error) {
	if !strings.HasPrefix(serverDSN, "postgres://") && !strings.HasPrefix(serverDSN, "postgresql://") {
		return serverDSN + " dbname=" + name, nil
	}
	parsed, err := url.Parse(serverDSN)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", DSNVariable, err)
	}
	parsed.Path = "/" + name
	return parsed.String(), nil
}

// dropStatement returns the statement that drops a test's database on engine, closing any
// connection still open on it where the engine allows.
//
// Takes engine (string) which is the engine.
// Takes name (string) which is the database.
//
// Returns string which is the DROP DATABASE statement.
func dropStatement(engine, name string) string {
	if engine == EngineCockroachDB {
		return "DROP DATABASE IF EXISTS " + name + " CASCADE"
	}
	return "DROP DATABASE IF EXISTS " + name + " WITH (FORCE)"
}

// execOnServer runs one statement on its own connection to the server.
//
// Takes ctx (context.Context) which bounds the statement.
// Takes serverDSN (string) which is the server's connection string.
// Takes statement (string) which is the statement to run.
//
// Returns error when the connection or the statement fails.
func execOnServer(ctx context.Context, serverDSN, statement string) error {
	server, err := sql.Open(driverName, serverDSN)
	if err != nil {
		return err
	}
	defer func() { _ = server.Close() }()
	_, err = server.ExecContext(ctx, statement)
	return err
}

// checkLeaks fails the test when a goroutine is still running once the test and its
// clean-ups have finished. The first helper a test calls registers the check, before the
// clean-ups that close what the helpers open, so it runs after them; later calls in the
// same test do nothing.
//
// The database driver keeps a goroutine parked on a network read for a connection it has
// closed, so goroutines waiting on the network are not counted.
//
// Takes t (testing.TB) which is the test.
func checkLeaks(t testing.TB) {
	t.Helper()
	if _, registered := leakChecked.LoadOrStore(t, struct{}{}); registered {
		return
	}
	t.Cleanup(func() {
		leakChecked.Delete(t)
		if err := leakcheck.FindLeaks(leakcheck.IgnoreNetworkPollers()); err != nil {
			t.Errorf("goroutines still running after the test: %v", err)
		}
	})
}
