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

package harness

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
)

const (
	// interruptionSleepSeconds is how long the migration's slow statement runs.
	interruptionSleepSeconds = "1.5"

	// interruptionPoll is how often the check looks for the slow statement.
	interruptionPoll = 20 * time.Millisecond

	// interruptionWait bounds how long the check waits for the slow statement to start.
	interruptionWait = 10 * time.Second

	// interruptionDeadline bounds one interrupted run and its recovery.
	interruptionDeadline = time.Minute

	// interruptionCancel interrupts the run by cancelling its context.
	interruptionCancel = "cancel"

	// interruptionKill interrupts the run by ending its database session.
	interruptionKill = "kill"
)

var (
	// errInterrupted is the cause of the migrator's context when the check cancels it.
	errInterrupted = errors.New("the check interrupted the migration run")

	// interruptionEffects lists the values the three migrations log, each once.
	interruptionEffects = []int64{1, 2, 3, 4}

	// interruptionEngines lists the engines whose sessions the check can end mid-statement.
	interruptionEngines = map[string]interruptionEngine{
		enginePostgres:    postgresInterruption(),
		engineTimescaleDB: postgresInterruption(),
		engineMySQL:       mysqlInterruption(),
		engineMariaDB:     mysqlInterruption(),
	}
)

// interruptionEngine is how one engine sleeps in a statement and ends another session.
type interruptionEngine struct {
	// kill ends the session running the sleep, from another session.
	kill func(ctx context.Context, database *sql.DB) error

	// sleeping counts the sessions running the sleep, from another session.
	sleeping string

	// sleep is a statement that runs for interruptionSleepSeconds.
	sleep string
}

// CheckMigrationInterruption interrupts a migration that runs outside a transaction while
// its slow statement runs, by cancelling the run's context and by ending its database
// session, and checks that a later run finishes the migration with every statement's
// effect applied exactly once and a clean history.
//
// Takes t (*testing.T) which reports an effect applied other than once.
// Takes suite (NativeFuzzSuite) which opens the database and names its driver.
func CheckMigrationInterruption(t *testing.T, suite NativeFuzzSuite) {
	t.Helper()
	engine, supported := interruptionEngines[suite.Engine]
	if !supported {
		t.Skipf("capability exclusion: the check cannot end a %s session mid-statement", suite.Engine)
	}
	for _, mode := range []string{interruptionCancel, interruptionKill} {
		t.Run(mode, func(t *testing.T) {
			runInterruption(t, suite, engine, mode)
		})
	}
}

// runInterruption runs one interrupted migration and its recovery.
//
// Takes t (*testing.T) which reports failures.
// Takes suite (NativeFuzzSuite) which opens the database.
// Takes engine (interruptionEngine) which sleeps and ends sessions.
// Takes mode (string) which is cancel or kill.
//
// Concurrency: runs the migration in a goroutine and waits for it before returning.
func runInterruption(t *testing.T, suite NativeFuzzSuite, engine interruptionEngine, mode string) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(interruptionDeadline), errFuzzInputDeadline)
	defer cancel()
	database, connection := suite.OpenNative(t)
	files := fstest.MapFS{
		"migrations/0001_log.up.sql": {Data: []byte("CREATE TABLE interruption_log (n BIGINT NOT NULL);\nINSERT INTO interruption_log (n) VALUES (1);\n")},
		"migrations/0002_slow.up.sql": {Data: fmt.Appendf(nil,
			"-- sgl.migration(no_transaction: true)\nINSERT INTO interruption_log (n) VALUES (2);\n%s;\nINSERT INTO interruption_log (n) VALUES (3);\n", engine.sleep)},
		"migrations/0003_after.up.sql": {Data: []byte("INSERT INTO interruption_log (n) VALUES (4);\n")},
	}
	backend := executionBackend(suite.Engine)
	pool, err := sql.Open(suite.Driver.Name, connection)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	migrator, err := migrate.NewMigrator(pool, backend, files, "migrations")
	require.NoError(t, err)
	runContext, stop := context.WithCancelCause(ctx)
	done := make(chan error, 1)
	go func() {
		_, upErr := migrator.Up(runContext)
		done <- upErr
	}()
	finished := false
	defer func() {

		stop(errInterrupted)
		if !finished {
			<-done
		}
	}()
	require.Eventually(t, func() bool { return sleepStarted(ctx, database, engine.sleeping) }, interruptionWait, interruptionPoll,
		"the slow statement never started")
	if mode == interruptionCancel {
		stop(errInterrupted)
	} else {
		require.NoError(t, engine.kill(ctx, database), "ending the migrator's session")
	}
	interrupted := <-done
	finished = true
	require.Error(t, interrupted, "the interrupted run reported success")
	t.Logf("interrupted run: %v", interrupted)

	recovery, err := migrate.NewMigrator(database, backend, files, "migrations")
	require.NoError(t, err)
	_, err = recovery.Up(ctx)
	require.NoError(t, err, "the run after the interruption did not finish the migrations")
	assert.Equal(t, interruptionEffects, loggedValues(ctx, t, database, "interruption_log"),
		"after an interruption by %s and a recovery the log does not hold each statement's effect once", mode)
	assert.NoError(t, recovery.Validate(ctx), "the history does not validate after recovery")
}

// sleepStarted reports whether another session is running the slow statement.
//
// Takes database (*sql.DB) which observes the sessions.
// Takes query (string) which counts the sessions running the sleep.
//
// Returns bool which is true once one is, and false while none is or the query fails.
func sleepStarted(ctx context.Context, database *sql.DB, query string) bool {
	var sessions int
	return database.QueryRowContext(ctx, query).Scan(&sessions) == nil && sessions > 0
}

// loggedValues reads the values the migrations logged in a log table, in order.
//
// Takes t (*testing.T) which reports a failed read.
// Takes database (*sql.DB) which holds the log.
// Takes table (string) which names the log table.
//
// Returns []int64 which holds the logged values.
func loggedValues(ctx context.Context, t *testing.T, database *sql.DB, table string) []int64 {
	t.Helper()
	var values []int64
	rows, err := database.QueryContext(ctx, "SELECT n FROM "+table+" ORDER BY n") //nolint:gosec // the check names its own log table
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var value int64
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.NoError(t, rows.Err())
	return values
}

// postgresInterruption sleeps with pg_sleep and ends the sleeping session with
// pg_terminate_backend.
//
// Returns interruptionEngine which describes PostgreSQL.
func postgresInterruption() interruptionEngine {
	return interruptionEngine{
		sleep:    "SELECT pg_sleep(" + interruptionSleepSeconds + ")",
		sleeping: "SELECT count(*) FROM pg_stat_activity WHERE query LIKE 'SELECT pg_sleep(%' AND state = 'active' AND pid <> pg_backend_pid()",
		kill: func(ctx context.Context, database *sql.DB) error {
			_, err := database.ExecContext(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE query LIKE 'SELECT pg_sleep(%' AND pid <> pg_backend_pid()")
			return err
		},
	}
}

// mysqlInterruption sleeps with SLEEP and ends the sleeping session with KILL.
//
// Returns interruptionEngine which describes MySQL and MariaDB.
func mysqlInterruption() interruptionEngine {
	return interruptionEngine{
		sleep:    "DO SLEEP(" + interruptionSleepSeconds + ")",
		sleeping: "SELECT count(*) FROM information_schema.processlist WHERE info LIKE 'DO SLEEP(%'",
		kill: func(ctx context.Context, database *sql.DB) error {
			var id int64
			if err := database.QueryRowContext(ctx, "SELECT id FROM information_schema.processlist WHERE info LIKE 'DO SLEEP(%' LIMIT 1").Scan(&id); err != nil {
				return err
			}
			_, err := database.ExecContext(ctx, fmt.Sprintf("KILL %d", id))
			return err
		},
	}
}
