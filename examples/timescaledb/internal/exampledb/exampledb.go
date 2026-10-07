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

// Package exampledb gives the TimescaleDB examples' tests a database of their own and
// checks a program's output against the expected output.txt beside it.
//
// The tests need a PostgreSQL server with the TimescaleDB extension available, named by a
// connection string in SGL_EXAMPLE_DSN, such as
// postgresql://postgres:password@localhost:5432/postgres?sslmode=disable. Each test
// creates a database with a unique name on that server and drops it when the test ends;
// the examples' first migration creates the extension in it. Without SGL_EXAMPLE_DSN the
// tests skip, unless SGL_EXAMPLE_REQUIRE_DSN=1, when they fail.
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
	"flag"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"seagull.sh/seagull/internal/leakcheck"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_postgres"
)

const (
	// OutputFile holds a program's expected output, beside its main.go.
	OutputFile = "output.txt"

	// DSNVariable names the environment variable that holds the server's connection string.
	DSNVariable = "SGL_EXAMPLE_DSN"

	// RequireVariable names the environment variable that, set to 1, makes a test fail
	// rather than skip when DSNVariable is empty.
	RequireVariable = "SGL_EXAMPLE_REQUIRE_DSN"

	// migrationsDirectory is where every example keeps its migrations.
	migrationsDirectory = "db/migrations"

	// outputPermissions is the mode -update creates a missing output.txt with; an existing
	// one keeps its own.
	outputPermissions = 0o600

	// databasePrefix starts the name of every database a test creates.
	databasePrefix = "sgl_example_"
)

var (
	// update rewrites output.txt instead of comparing against it.
	update = flag.Bool("update", false, "rewrite output.txt from the program's output")

	// leakChecked holds each test whose goroutines are checked once it ends, so a test that
	// calls several helpers registers the check only once.
	leakChecked sync.Map
)

// Open returns a new database on the server with the example's migrations applied by the
// PostgreSQL backend, which TimescaleDB uses. It is dropped when the test ends.
//
// Takes t (testing.TB) which is the test.
// Takes migrations (fs.FS) which holds db/migrations, usually the example's embed.FS.
//
// Returns *sql.DB which is the migrated database.
func Open(t testing.TB, migrations fs.FS) *sql.DB {
	t.Helper()
	checkLeaks(t)
	database := OpenEmpty(t)
	migrator, err := migrate.NewMigrator(database, migrate_backend_postgres.New(), migrations, migrationsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Up(t.Context()); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	return database
}

// OpenEmpty returns a new, empty database on the server, for an example that applies its
// migrations itself. It is dropped when the test ends.
//
// Takes t (testing.TB) which is the test.
//
// Returns *sql.DB which is the empty database.
func OpenEmpty(t testing.TB) *sql.DB {
	t.Helper()
	checkLeaks(t)
	server := serverConfig(t)
	name := databasePrefix + strings.ToLower(rand.Text())

	administration := stdlib.OpenDB(*server)
	defer func() { _ = administration.Close() }()
	if _, err := administration.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("creating database %s: %v", name, err)
	}
	t.Cleanup(func() { dropDatabase(t, server, name) })

	config := server.Copy()
	config.Database = name
	database := stdlib.OpenDB(*config)
	t.Cleanup(func() { _ = database.Close() })
	return database
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

// serverConfig reads the server's connection string from DSNVariable, skipping the test
// when it is empty and RequireVariable is not 1.
//
// Takes t (testing.TB) which is the test.
//
// Returns *pgx.ConnConfig which is the parsed connection string.
func serverConfig(t testing.TB) *pgx.ConnConfig {
	t.Helper()
	dsn := os.Getenv(DSNVariable)
	if dsn == "" {
		if os.Getenv(RequireVariable) == "1" {
			t.Fatalf("%s is empty and %s=1", DSNVariable, RequireVariable)
		}
		t.Skipf("set %s to a TimescaleDB connection string to run this example", DSNVariable)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parsing %s: %v", DSNVariable, err)
	}
	return config
}

// dropDatabase drops a database the test created, ending the sessions still connected to
// it, such as TimescaleDB's background workers.
//
// Takes t (testing.TB) which is the test.
// Takes server (*pgx.ConnConfig) which connects to the server.
// Takes name (string) which is the database to drop.
func dropDatabase(t testing.TB, server *pgx.ConnConfig, name string) {
	t.Helper()
	administration := stdlib.OpenDB(*server)
	defer func() { _ = administration.Close() }()
	if _, err := administration.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
		t.Errorf("dropping database %s: %v", name, err)
	}
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
