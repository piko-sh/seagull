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

// Package exampledb gives the MySQL examples' tests a fresh, migrated database and checks
// a program's output against the expected output.txt beside it.
//
// SGL_EXAMPLE_DSN names the server: a go-sql-driver/mysql data source name for a user
// that may create and drop databases, such as root:secret@tcp(127.0.0.1:3306)/. Each test
// creates its own database there and drops it when the test ends. SGL_EXAMPLE_ENGINE says
// which server it is, mysql (the default) or mariadb, and picks the migration backend.
// Without a DSN the tests skip, unless SGL_EXAMPLE_REQUIRE_DSN=1, when they fail.
//
// A test that calls any of these helpers also fails when a goroutine is still running
// once it and its clean-ups have finished.
//
// Run the tests with -update to rewrite output.txt from the program's current output.
package exampledb

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"seagull.sh/seagull/internal/leakcheck"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_mysql"
)

const (
	// OutputFile holds a program's expected output, beside its main.go.
	OutputFile = "output.txt"

	// DSNVariable names the environment variable holding the server's data source name.
	DSNVariable = "SGL_EXAMPLE_DSN"

	// EngineVariable names the environment variable saying which server the DSN reaches.
	EngineVariable = "SGL_EXAMPLE_ENGINE"

	// RequireVariable names the environment variable that turns a missing DSN from a skip
	// into a failure.
	RequireVariable = "SGL_EXAMPLE_REQUIRE_DSN"

	// migrationsDirectory is where every example keeps its migrations.
	migrationsDirectory = "db/migrations"

	// outputPermissions is the mode -update creates a missing output.txt with; an existing
	// one keeps its own.
	outputPermissions = 0o600

	// driverName is the name go-sql-driver/mysql registers its database/sql driver under.
	driverName = "mysql"

	// databasePrefix starts the name of every database a test creates.
	databasePrefix = "sgl_example_"

	// maxLabelLength caps the part of a database name taken from the example's directory,
	// keeping the whole name under MySQL's 64-character limit.
	maxLabelLength = 24
)

var (
	// update rewrites output.txt instead of comparing against it.
	update = flag.Bool("update", false, "rewrite output.txt from the program's output")

	// leakChecked holds each test whose goroutines are checked once it ends, so a test that
	// calls several helpers registers the check only once.
	leakChecked sync.Map

	// sequence tells apart the databases created by one test binary.
	sequence atomic.Int64
)

// Open returns a fresh database with the example's migrations applied. It is closed and
// dropped when the test ends.
//
// Takes t (testing.TB) which is the test.
// Takes migrations (fs.FS) which holds db/migrations, usually the example's embed.FS.
//
// Returns *sql.DB which is the migrated database.
func Open(t testing.TB, migrations fs.FS) *sql.DB {
	t.Helper()
	checkLeaks(t)
	database, dsn := newDatabase(t)
	migrator, err := migrate.NewMigrator(database, Backend(t, dsn), migrations, migrationsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Up(t.Context()); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	return database
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
	database, _ := newDatabase(t)
	return database
}

// Backend returns the migration backend for the server SGL_EXAMPLE_ENGINE names. MariaDB
// speaks MySQL's protocol and takes the same backend.
//
// Takes t (testing.TB) which is the test.
// Takes dsn (string) which is the database's data source name.
//
// Returns migrate.Backend which migrates the database.
func Backend(t testing.TB, dsn string) migrate.Backend {
	t.Helper()
	checkLeaks(t)
	switch engine := os.Getenv(EngineVariable); engine {
	case "", "mysql", "mariadb":
		config, err := mysql.ParseDSN(dsn)
		if err != nil {
			t.Fatalf("parsing the data source name: %v", err)
		}
		return migrate_backend_mysql.New(migrate.WithDriverMultiStatements(config.MultiStatements))
	default:
		t.Fatalf("%s is %q, want mysql or mariadb", EngineVariable, engine)
		return migrate.Backend{}
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

// newDatabase creates a database with a unique name on the server SGL_EXAMPLE_DSN names
// and opens it. The test is skipped when there is no DSN, unless SGL_EXAMPLE_REQUIRE_DSN
// is 1. The database is closed and dropped when the test ends.
//
// Takes t (testing.TB) which is the test.
//
// Returns *sql.DB which is the empty database.
// Returns string which is its data source name, with parseTime and multiStatements set.
func newDatabase(t testing.TB) (*sql.DB, string) {
	t.Helper()
	config := serverConfig(t)
	name := databaseName()

	administration, err := sql.Open(driverName, config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = administration.Close() }()
	if _, err := administration.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("creating database %s: %v", name, err)
	}
	t.Cleanup(func() { dropDatabase(t, config.FormatDSN(), name) })

	config.DBName = name
	dsn := config.FormatDSN()
	database, err := sql.Open(driverName, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database, dsn
}

// serverConfig reads the server's data source name from SGL_EXAMPLE_DSN, skipping the
// test when it is unset, and sets the options the examples rely on: parseTime, so
// DATETIME columns scan into time.Time, and multiStatements, as the integration suite
// uses.
//
// Takes t (testing.TB) which is the test.
//
// Returns *mysql.Config which reaches the server with no database selected.
func serverConfig(t testing.TB) *mysql.Config {
	t.Helper()
	dsn := os.Getenv(DSNVariable)
	if dsn == "" {
		if os.Getenv(RequireVariable) == "1" {
			t.Fatalf("%s is not set and %s=1", DSNVariable, RequireVariable)
		}
		t.Skipf("set %s to a MySQL or MariaDB server to run this example", DSNVariable)
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parsing %s: %v", DSNVariable, err)
	}
	config.ParseTime = true
	config.MultiStatements = true
	config.DBName = ""
	return config
}

// databaseName returns a database name no other test is using, labelled with the
// example's directory so a database left behind is easy to trace.
//
// Returns string which is the name, safe to use unquoted.
func databaseName() string {
	label := "example"
	if directory, err := os.Getwd(); err == nil {
		label = filepath.Base(directory)
	}
	label = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, strings.ToLower(label))
	if len(label) > maxLabelLength {
		label = label[:maxLabelLength]
	}
	unique := strconv.FormatInt(time.Now().UnixNano(), 36) + strconv.FormatInt(sequence.Add(1), 36)
	return fmt.Sprintf("%s%s_%s", databasePrefix, label, unique)
}

// dropDatabase drops a database a test created. It runs after the test's context is
// cancelled, so it uses a context of its own.
//
// Takes t (testing.TB) which is the test.
// Takes serverDSN (string) which reaches the server with no database selected.
// Takes name (string) which is the database to drop.
func dropDatabase(t testing.TB, serverDSN, name string) {
	t.Helper()
	administration, err := sql.Open(driverName, serverDSN)
	if err != nil {
		t.Errorf("dropping database %s: %v", name, err)
		return
	}
	defer func() { _ = administration.Close() }()
	if _, err := administration.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name); err != nil {
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
