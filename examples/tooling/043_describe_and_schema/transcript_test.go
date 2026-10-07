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

// Package example replays transcript.txt, a terminal session that describes queries,
// dumps the schema and diffs it against a database that has drifted.
//
// The transcript is a shell session: a "$ " line is a command, the lines after it are
// what it printed (stdout and stderr together), "# exit N" is its exit status, and other
// "#" lines narrate. sgl runs in-process in a temporary copy of this directory. Run
// `go test . -update` to record it again.
package example

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"seagull.sh/seagull/examples/tooling/internal/transcript"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
)

func TestTranscript(t *testing.T) {
	transcript.Replay(t, driftedDatabase)
}

// driftedDatabase writes drift.db: the migrations applied, then two changes made by
// hand that no migration records.
//
// Takes t (*testing.T) which is the test.
// Takes directory (string) which is the working copy of the example.
func driftedDatabase(t *testing.T, directory string) {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+filepath.Join(directory, "drift.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()

	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), os.DirFS(directory), "db/migrations")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Up(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"ALTER TABLE books ADD COLUMN isbn TEXT",
		"ALTER TABLE books DROP COLUMN subtitle",
	} {
		if _, err := database.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
}
