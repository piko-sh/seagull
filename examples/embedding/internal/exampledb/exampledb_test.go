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

package exampledb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// recordingTest stands in for a test, recording the clean-ups and failures a helper
// registers instead of acting on them.
type recordingTest struct {
	testing.TB

	// cleanups holds the clean-ups registered, in order.
	cleanups []func()

	// failures holds each failure reported.
	failures []string
}

// Helper does nothing; the recording test reports no lines.
func (*recordingTest) Helper() {}

// Cleanup records a clean-up instead of registering it.
//
// Takes cleanup (func()) which is recorded.
func (recorder *recordingTest) Cleanup(cleanup func()) {
	recorder.cleanups = append(recorder.cleanups, cleanup)
}

// Errorf records a failure.
//
// Takes format (string) which is the message's format.
// Takes arguments (...any) which are its values.
func (recorder *recordingTest) Errorf(format string, arguments ...any) {
	recorder.failures = append(recorder.failures, fmt.Sprintf(format, arguments...))
}

// Fatalf records a failure; unlike a test's, it does not stop the caller.
//
// Takes format (string) which is the message's format.
// Takes arguments (...any) which are its values.
func (recorder *recordingTest) Fatalf(format string, arguments ...any) {
	recorder.failures = append(recorder.failures, fmt.Sprintf(format, arguments...))
}

func TestCheckLeaksRegistersOneCheckThatFailsATestLeavingAGoroutineRunning(t *testing.T) {
	testCases := []struct {
		name         string
		calls        int
		leak         bool
		wantFailures int
	}{
		{name: "a test that leaves nothing running passes", calls: 1, leak: false, wantFailures: 0},
		{name: "a test that leaves a goroutine running fails", calls: 1, leak: true, wantFailures: 1},
		{name: "several helper calls register one check", calls: 3, leak: true, wantFailures: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := &recordingTest{TB: t}
			for range testCase.calls {
				checkLeaks(recorder)
			}
			if len(recorder.cleanups) != 1 {
				t.Fatalf("%d calls registered %d checks, want 1", testCase.calls, len(recorder.cleanups))
			}

			release := make(chan struct{})
			finished := make(chan struct{})
			if testCase.leak {
				go func() {
					defer close(finished)
					<-release
				}()
			} else {
				close(finished)
			}
			recorder.cleanups[0]()
			close(release)
			<-finished

			if len(recorder.failures) != testCase.wantFailures {
				t.Fatalf("the check reported %q, want %d failures", recorder.failures, testCase.wantFailures)
			}
			if _, registered := leakChecked.Load(recorder); registered {
				t.Fatal("the check is still registered after it ran")
			}
		})
	}
}

func TestOpenAndEmptyGiveTheTestADatabaseOfItsOwn(t *testing.T) {
	migrations := fstest.MapFS{
		"db/migrations/0001_boats.up.sql": {Data: []byte("CREATE TABLE boats (id INTEGER PRIMARY KEY, name TEXT NOT NULL);")},
	}
	testCases := []struct {
		open       func(t *testing.T) int
		name       string
		wantTables int
	}{
		{name: "migrated", wantTables: 1, open: func(t *testing.T) int {
			t.Helper()
			return countTables(t, Open(t, migrations))
		}},
		{name: "empty", wantTables: 0, open: func(t *testing.T) int {
			t.Helper()
			return countTables(t, Empty(t))
		}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if tables := testCase.open(t); tables != testCase.wantTables {
				t.Fatalf("the database holds %d boats tables, want %d", tables, testCase.wantTables)
			}
		})
	}
}

func TestMatchOutputComparesWithOutputTxtOrRewritesIt(t *testing.T) {
	testCases := []struct {
		name         string
		output       string
		wantFile     string
		update       bool
		wantFailures int
	}{
		{name: "the same output", output: "Pip\n", wantFile: "Pip\n", update: false, wantFailures: 0},
		{name: "different output", output: "Gull\n", wantFile: "Pip\n", update: false, wantFailures: 1},
		{name: "rewritten with -update", output: "Gull\n", wantFile: "Gull\n", update: true, wantFailures: 0},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, OutputFile), []byte("Pip\n"), outputPermissions); err != nil {
				t.Fatal(err)
			}
			t.Chdir(directory)
			previous := *update
			*update = testCase.update
			t.Cleanup(func() { *update = previous })

			recorder := &recordingTest{TB: t}
			MatchOutput(recorder, testCase.output)
			for _, cleanup := range recorder.cleanups {
				cleanup()
			}

			if len(recorder.failures) != testCase.wantFailures {
				t.Fatalf("MatchOutput reported %q, want %d failures", recorder.failures, testCase.wantFailures)
			}
			written, err := os.ReadFile(filepath.Join(directory, OutputFile))
			if err != nil {
				t.Fatal(err)
			}
			if string(written) != testCase.wantFile {
				t.Fatalf("output.txt holds %q, want %q", written, testCase.wantFile)
			}
		})
	}
}

// countTables counts the boats tables in a SQLite database.
//
// Takes database (*sql.DB) which is the database.
//
// Returns int which is the count.
func countTables(t *testing.T, database *sql.DB) int {
	t.Helper()
	var count int
	query := "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'boats'"
	if err := database.QueryRowContext(t.Context(), query).Scan(&count); err != nil {
		t.Fatalf("counting tables: %v", err)
	}
	return count
}
