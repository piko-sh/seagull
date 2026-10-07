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

// Package exampledb gives the ClickHouse examples' tests a fresh, migrated database on
// the server SGL_EXAMPLE_DSN names, and checks a program's output against the expected
// output.txt beside it.
//
// Each test gets its own database, created with a unique name and dropped when the test
// ends, so tests never see each other's rows. Without SGL_EXAMPLE_DSN the tests skip,
// unless SGL_EXAMPLE_REQUIRE_DSN=1, which makes a missing DSN a failure.
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
	"io/fs"
	"maps"
	"os"
	"sync"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"seagull.sh/seagull/internal/leakcheck"
	sdkmigrate "seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_clickhouse"
)

const (
	// OutputFile holds a program's expected output, beside its main.go.
	OutputFile = "output.txt"

	// DSNVariable names the environment variable holding the ClickHouse DSN, such as
	// clickhouse://default:password@localhost:9000/default.
	DSNVariable = "SGL_EXAMPLE_DSN"

	// RequireVariable names the environment variable that, set to 1, fails a test that has
	// no DSN instead of skipping it.
	RequireVariable = "SGL_EXAMPLE_REQUIRE_DSN"

	// migrationsDirectory is where every example keeps its migrations.
	migrationsDirectory = "db/migrations"

	// outputPermissions is the mode -update creates a missing output.txt with; an existing
	// one keeps its own.
	outputPermissions = 0o600

	// databasePrefix starts the name of every database a test creates.
	databasePrefix = "sgl_example_"

	// suffixBytes is how many random bytes end a database's name, as twice as many hex
	// digits.
	suffixBytes = 8
)

var (
	// update rewrites output.txt instead of comparing against it.
	update = flag.Bool("update", false, "rewrite output.txt from the program's output")

	// leakChecked holds each test whose goroutines are checked once it ends, so a test that
	// calls several helpers registers the check only once.
	leakChecked sync.Map
)

// Open returns a database/sql handle on a fresh ClickHouse database with the example's
// migrations applied. The handle is closed and the database dropped when the test ends.
//
// Takes t (testing.TB) which is the test.
// Takes migrations (fs.FS) which holds db/migrations, usually the example's embed.FS.
//
// Returns *sql.DB which is the migrated database.
func Open(t testing.TB, migrations fs.FS) *sql.DB {
	t.Helper()
	checkLeaks(t)
	database := OpenEmpty(t)
	migrate(t, database, migrations)
	return database
}

// OpenEmpty returns a database/sql handle on a fresh ClickHouse database with nothing in
// it, for an example that applies its own migrations. The handle is closed and the
// database dropped when the test ends.
//
// Takes t (testing.TB) which is the test.
//
// Returns *sql.DB which is the empty database.
func OpenEmpty(t testing.TB) *sql.DB {
	t.Helper()
	checkLeaks(t)
	database := clickhouse.OpenDB(freshDatabase(t))
	t.Cleanup(func() { _ = database.Close() })
	return database
}

// OpenConn returns a native clickhouse-go connection to a fresh ClickHouse database with
// the example's migrations applied. The migrator runs on database/sql, so the migrations
// are applied through the driver's database/sql adapter before the native connection
// opens. The connection is closed and the database dropped when the test ends.
//
// Takes t (testing.TB) which is the test.
// Takes migrations (fs.FS) which holds db/migrations, usually the example's embed.FS.
//
// Returns driver.Conn which is the native connection.
func OpenConn(t testing.TB, migrations fs.FS) driver.Conn {
	t.Helper()
	checkLeaks(t)
	options := freshDatabase(t)
	database := clickhouse.OpenDB(options)
	t.Cleanup(func() { _ = database.Close() })
	migrate(t, database, migrations)
	connection, err := clickhouse.Open(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return connection
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

// freshDatabase creates a database with a unique name on the server SGL_EXAMPLE_DSN
// names, drops it when the test ends, and returns connection options that use it. The
// options set mutations_sync=2, so an ALTER TABLE UPDATE or DELETE returns only once
// every replica has applied it and the test reads the result straight away.
//
// Takes t (testing.TB) which is the test.
//
// Returns *clickhouse.Options which connects to the new database.
func freshDatabase(t testing.TB) *clickhouse.Options {
	t.Helper()
	dsn := os.Getenv(DSNVariable)
	if dsn == "" {
		if os.Getenv(RequireVariable) == "1" {
			t.Fatalf("%s is empty and %s=1; set it to a ClickHouse DSN", DSNVariable, RequireVariable)
		}
		t.Skipf("set %s to a ClickHouse DSN, such as clickhouse://default:password@localhost:9000/default, to run this example", DSNVariable)
	}
	options, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parsing %s: %v", DSNVariable, err)
	}

	name := databasePrefix + randomSuffix(t)
	server := clickhouse.OpenDB(options)
	defer func() { _ = server.Close() }()
	if _, err := server.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("creating database %s: %v", name, err)
	}
	t.Cleanup(func() { dropDatabase(options, name) })

	fresh := *options
	fresh.Auth.Database = name
	fresh.Settings = clickhouse.Settings{"mutations_sync": 2}
	maps.Copy(fresh.Settings, options.Settings)
	return &fresh
}

// dropDatabase drops a database a test created. The test's context is already cancelled
// when cleanups run, so it uses its own.
//
// Takes options (*clickhouse.Options) which connects to the server.
// Takes name (string) which is the database to drop.
func dropDatabase(options *clickhouse.Options, name string) {
	server := clickhouse.OpenDB(options)
	defer func() { _ = server.Close() }()
	_, _ = server.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name)
}

// migrate applies the migrations in db/migrations with the ClickHouse backend.
//
// Takes t (testing.TB) which is the test.
// Takes database (*sql.DB) which is the database to migrate.
// Takes migrations (fs.FS) which holds db/migrations.
func migrate(t testing.TB, database *sql.DB, migrations fs.FS) {
	t.Helper()
	migrator, err := sdkmigrate.NewMigrator(database, migrate_backend_clickhouse.New(), migrations, migrationsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Up(t.Context()); err != nil {
		t.Fatalf("migrating: %v", err)
	}
}

// randomSuffix returns sixteen random hex digits for a database name.
//
// Takes t (testing.TB) which is the test.
//
// Returns string which is the suffix.
func randomSuffix(t testing.TB) string {
	t.Helper()
	buffer := make([]byte, suffixBytes)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(buffer)
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
