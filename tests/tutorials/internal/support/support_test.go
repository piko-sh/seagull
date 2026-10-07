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

package support

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

var errCheckpoint = errors.New("the checkpoint failed")

const tasksMigration = "CREATE TABLE tasks (id INTEGER PRIMARY KEY, title TEXT NOT NULL);"

func TestOpenAppliesTheMigrationsOrReportsWhyItCannot(t *testing.T) {
	testCases := []struct {
		files     fstest.MapFS
		name      string
		wantError bool
	}{
		{
			name:  "the migrations are applied",
			files: fstest.MapFS{"db/migrations/0001_tasks.up.sql": {Data: []byte(tasksMigration)}},
		},
		{
			name:      "a migration that does not run is reported",
			files:     fstest.MapFS{"db/migrations/0001_tasks.up.sql": {Data: []byte("CREATE TABLE (")}},
			wantError: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			database, err := Open(t.Context(), testCase.files, filepath.Join(t.TempDir(), "tasks.db"))
			if testCase.wantError {
				if err == nil || database != nil {
					t.Fatalf("Open = %v, %v; want no database and an error", database, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer func() { _ = database.Close() }()
			if count := tableCount(t, database); count != 1 {
				t.Fatalf("the migrated database holds %d tasks tables, want 1", count)
			}
		})
	}
}

func TestMainRunsTheCheckpointAgainstAMigratedDatabase(t *testing.T) {
	migrated := fstest.MapFS{"db/migrations/0001_tasks.up.sql": {Data: []byte(tasksMigration)}}
	testCases := []struct {
		files     fstest.MapFS
		checkErr  error
		name      string
		wantError string
		wantRun   bool
	}{
		{name: "a checkpoint that succeeds", files: migrated, wantRun: true},
		{name: "a checkpoint that fails", files: migrated, checkErr: errCheckpoint, wantRun: true, wantError: "tutorial: the checkpoint failed"},
		{
			name:      "migrations that do not run",
			files:     fstest.MapFS{"db/migrations/0001_tasks.up.sql": {Data: []byte("CREATE TABLE (")}},
			wantError: "migration",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ran := false
			err := Main(testCase.files, func(ctx context.Context, database *sql.DB, _ io.Writer) error {
				ran = true
				var count int
				if scanErr := database.QueryRowContext(ctx, "SELECT count(*) FROM tasks").Scan(&count); scanErr != nil {
					return scanErr
				}
				return testCase.checkErr
			})
			if ran != testCase.wantRun {
				t.Fatalf("the checkpoint ran: %t, want %t", ran, testCase.wantRun)
			}
			switch {
			case testCase.wantError == "" && err != nil:
				t.Fatalf("Main: %v", err)
			case testCase.wantError != "" && (err == nil || !strings.Contains(err.Error(), testCase.wantError)):
				t.Fatalf("Main = %v, want an error containing %q", err, testCase.wantError)
			case testCase.checkErr != nil && !errors.Is(err, testCase.checkErr):
				t.Fatalf("Main = %v, want it to wrap %v", err, testCase.checkErr)
			}
		})
	}
}

func tableCount(t *testing.T, database *sql.DB) int {
	t.Helper()
	var count int
	query := "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'tasks'"
	if err := database.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
		t.Fatalf("counting tables: %v", err)
	}
	return count
}
