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

// Package exampledb gives the SQLite examples' tests a database, migrated or empty, and
// checks a program's output against the expected output.txt beside it.
//
// A test that calls any of these helpers also fails when a goroutine is still running
// once it and its clean-ups have finished.
//
// Run the tests with -update to rewrite output.txt from the program's current output.
package exampledb

import (
	"database/sql"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	// The pure-Go SQLite driver registers itself with database/sql as "sqlite", the driver
	// Open asks for.
	_ "modernc.org/sqlite"

	"seagull.sh/seagull/internal/leakcheck"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
)

const (
	// OutputFile holds a program's expected output, beside its main.go.
	OutputFile = "output.txt"

	// migrationsDirectory is where every example keeps its migrations.
	migrationsDirectory = "db/migrations"

	// outputPermissions is the mode -update creates a missing output.txt with; an existing
	// one keeps its own.
	outputPermissions = 0o600
)

var (
	// update rewrites output.txt instead of comparing against it.
	update = flag.Bool("update", false, "rewrite output.txt from the program's output")

	// leakChecked holds each test whose goroutines are checked once it ends, so a test that
	// calls several helpers registers the check only once.
	leakChecked sync.Map
)

// Open returns a SQLite database in a temporary file with the example's migrations
// applied. It is closed when the test ends.
//
// Takes t (testing.TB) which is the test.
// Takes migrations (fs.FS) which holds db/migrations, usually the example's embed.FS.
//
// Returns *sql.DB which is the migrated database.
func Open(t testing.TB, migrations fs.FS) *sql.DB {
	t.Helper()
	checkLeaks(t)
	database := OpenEmpty(t)
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), migrations, migrationsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Up(t.Context()); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	return database
}

// OpenEmpty returns a SQLite database in a temporary file with nothing in it, for
// examples that migrate it themselves. It is closed when the test ends.
//
// Takes t (testing.TB) which is the test.
//
// Returns *sql.DB which is the empty database.
func OpenEmpty(t testing.TB) *sql.DB {
	t.Helper()
	checkLeaks(t)
	database, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "example.db"))
	if err != nil {
		t.Fatal(err)
	}
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

// checkLeaks fails the test when a goroutine is still running once the test and its
// clean-ups have finished. The first helper a test calls registers the check, before the
// clean-ups that close what the helpers open, so it runs after them; later calls in the
// same test do nothing.
//
// Takes t (testing.TB) which is the test.
func checkLeaks(t testing.TB) {
	t.Helper()
	if _, registered := leakChecked.LoadOrStore(t, struct{}{}); registered {
		return
	}
	t.Cleanup(func() {
		leakChecked.Delete(t)
		if err := leakcheck.FindLeaks(); err != nil {
			t.Errorf("goroutines still running after the test: %v", err)
		}
	})
}
