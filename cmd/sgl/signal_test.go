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

//go:build unix

package main

import (
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	signalChildDirectory  = "SGL_SIGNAL_CHILD_DIRECTORY"
	settledJournalAge     = 200 * time.Millisecond
	migrationStartTimeout = 30 * time.Second
	migrationStartPoll    = 20 * time.Millisecond
	slowMigration         = "CREATE TABLE slow_items (n INTEGER);\n" +
		"INSERT INTO slow_items SELECT count(*) FROM (WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 10000000) SELECT x FROM c);\n"
)

func TestSglStopsAMigrationOnASignal(t *testing.T) {
	if directory := os.Getenv(signalChildDirectory); directory != "" {
		require.NoError(t, os.Chdir(directory))
		os.Args = []string{"sgl", "migrate", "up", "--dsn", filepath.Join(directory, "app.db")}
		main()
		return
	}
	t.Parallel()

	for _, testCase := range []struct {
		signal syscall.Signal
		name   string
		code   int
	}{
		{name: "an interrupt", signal: syscall.SIGINT, code: 130},
		{name: "a termination", signal: syscall.SIGTERM, code: 143},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			for name, content := range map[string]string{
				"sgl.yaml": "version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n    migrations: migrations\n" +
					"    queries: queries\n    go:\n      package: store\n      out: internal/store\n",
				"migrations/0001_slow.up.sql": slowMigration,
				"queries/q.sql":               "-- sgl.query(Count, one)\nSELECT count(*) FROM slow_items;\n",
			} {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(directory, name)), 0o750))
				require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600))
			}

			child := exec.Command(os.Args[0], "-test.run=^TestSglStopsAMigrationOnASignal$")
			child.Env = append(os.Environ(), signalChildDirectory+"="+directory)
			require.NoError(t, child.Start())
			t.Cleanup(func() {
				if err := child.Process.Kill(); !errors.Is(err, os.ErrProcessDone) {
					assert.NoError(t, err, "stopping the child sgl")
				}
			})
			journal := filepath.Join(directory, "app.db-journal")
			require.Eventually(t, func() bool { return journalSettled(journal) }, migrationStartTimeout, migrationStartPoll,
				"the child never reached its slow migration")
			require.NoError(t, child.Process.Signal(testCase.signal))
			err := child.Wait()

			var exitError *exec.ExitError
			require.ErrorAs(t, err, &exitError, "the signalled run must not report success")
			status, ok := exitError.Sys().(syscall.WaitStatus)
			require.True(t, ok)
			assert.False(t, status.Signaled(), "sgl exits with a status of its own rather than being killed by the signal")
			assert.Equal(t, testCase.code, exitError.ExitCode())

			database, err := sql.Open("sqlite", filepath.Join(directory, "app.db"))
			require.NoError(t, err)
			defer func() { _ = database.Close() }()
			var tables int
			require.NoError(t, database.QueryRowContext(t.Context(),
				"SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'slow_items'").Scan(&tables))
			assert.Zero(t, tables, "the interrupted migration's transaction left its table behind")

			rerun := exec.Command(os.Args[0], "-test.run=^TestSglStopsAMigrationOnASignal$")
			rerun.Env = append(os.Environ(), signalChildDirectory+"="+directory)
			output, err := rerun.CombinedOutput()
			require.NoError(t, err, "the run after the signal did not finish the migration: %s", output)
		})
	}
}

func journalSettled(journal string) bool {
	info, err := os.Stat(journal)
	return err == nil && time.Since(info.ModTime()) >= settledJournalAge
}
