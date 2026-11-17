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
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
)

const (
	// killChildEnvironment names the variable that carries the connection string to the
	// child migrator, and whose absence keeps the child test from running on its own.
	killChildEnvironment = "SEAGULL_MIGRATION_KILL_CONNECTION"

	// killIterations is how many runs are killed, each at a different point.
	killIterations = 6

	// killFirstDelay is how long the first run is let go before it is killed.
	killFirstDelay = 150 * time.Millisecond

	// killDelayStep is how much longer each later run is let go, so the kills land in the
	// first migration, in its slow statement and in the second migration's.
	killDelayStep = 700 * time.Millisecond

	// killDeadline bounds one killed run and its recovery.
	killDeadline = 2 * time.Minute

	// killReplayAllowance is how many statements of a migration that runs without a
	// transaction may run twice. That is the one that committed before its checkpoint was
	// written.
	killReplayAllowance = 1
)

// killSlowStatements maps each engine to a statement that runs for about two seconds.
var killSlowStatements = map[string]string{
	enginePostgres:    "SELECT pg_sleep(2)",
	engineTimescaleDB: "SELECT pg_sleep(2)",
	engineCockroachDB: "SELECT pg_sleep(2)",
	engineMySQL:       "DO SLEEP(2)",
	engineMariaDB:     "DO SLEEP(2)",
	engineSQLite:      "WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 12000000) SELECT count(*) FROM c",
	engineDuckDB:      "SELECT count(*) FROM range(300000000) AS t(x) WHERE hash(x) % 7 = 1",
	engineClickHouse:  "SELECT sleepEachRow(0.5) FROM numbers(4) SETTINGS max_block_size = 1",
}

// killTransactionalEffects are the values the migrations that run in a transaction log,
// which a killed run either leaves whole or leaves out, so each appears exactly once.
var killTransactionalEffects = []int64{1, 2, 5}

// killWithoutMigrationTransactions lists the engines whose backends run every migration
// without a transaction, resuming each after its last recorded statement, so any one
// statement of any migration may run twice.
var killWithoutMigrationTransactions = map[string]bool{
	engineMySQL: true, engineMariaDB: true, engineCockroachDB: true, engineClickHouse: true,
}

// killResumedEffects are the values the migration that runs without a transaction logs,
// whose statements a resumed run may replay up to killReplayAllowance of.
var killResumedEffects = []int64{3, 4}

// killHistory returns the migrations a killed run applies. The first runs in a
// transaction and holds a slow statement, the second runs without one and holds another,
// and a last one follows.
//
// Takes engine (string) which names the engine.
//
// Returns fstest.MapFS which holds the migrations.
func killHistory(engine string) fstest.MapFS {
	slow := killSlowStatements[engine]
	return fstest.MapFS{
		"migrations/0001_first.up.sql": {Data: fmt.Appendf(nil,
			"INSERT INTO kill_log (n) VALUES (1);\n%s;\nINSERT INTO kill_log (n) VALUES (2);\n", slow)},
		"migrations/0002_resumable.up.sql": {Data: fmt.Appendf(nil,
			"-- sgl.migration(no_transaction: true)\nINSERT INTO kill_log (n) VALUES (3);\n%s;\nINSERT INTO kill_log (n) VALUES (4);\n", slow)},
		"migrations/0003_last.up.sql": {Data: []byte("INSERT INTO kill_log (n) VALUES (5);\n")},
	}
}

// RunMigrationKillChild applies the kill history to the database the parent names, as the
// child process CheckMigrationKill starts and kills. Run without the parent's variable,
// it skips.
//
// Takes t (*testing.T) which reports a failure to start.
// Takes suite (NativeFuzzSuite) which names the driver and engine.
func RunMigrationKillChild(t *testing.T, suite NativeFuzzSuite) {
	t.Helper()
	connection := os.Getenv(killChildEnvironment)
	if connection == "" {
		t.Skip("runs only as the child process TestMigrationKill starts")
	}
	pool, err := sql.Open(suite.Driver.Name, connection)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	migrator, err := migrate.NewMigrator(pool, executionBackend(suite.Engine), killHistory(suite.Engine), "migrations")
	require.NoError(t, err)
	if _, err := migrator.Up(t.Context()); err != nil {
		t.Logf("the child's run stopped: %v", err)
	}
}

// CheckMigrationKill checks recovery after killing a migrator at several points.
//
// A later run must finish with clean history and every statement applied. Transactional
// statements must apply once. One uncheckpointed statement may apply twice.
//
// Takes t (*testing.T) which reports an effect applied wrongly or a failed recovery.
// Takes suite (NativeFuzzSuite) which opens the database and names its driver.
// Takes childTest (string) which is the name of the suite's test calling
// RunMigrationKillChild.
func CheckMigrationKill(t *testing.T, suite NativeFuzzSuite, childTest string) {
	t.Helper()
	if _, known := killSlowStatements[suite.Engine]; !known {
		t.Skipf("capability exclusion: no slow statement is written for %s", suite.Engine)
	}
	for iteration := range killIterations {
		delay := killFirstDelay + time.Duration(iteration)*killDelayStep
		t.Run(delay.String(), func(t *testing.T) {
			runKilledMigration(t, suite, childTest, delay)
		})
	}
}

// runKilledMigration kills one child run after a delay and recovers from it.
//
// Takes t (*testing.T) which reports the outcome.
// Takes suite (NativeFuzzSuite) which opens the database.
// Takes childTest (string) which names the child test.
// Takes delay (time.Duration) which is how long the child runs before it is killed.
//
// Concurrency: waits for the child in a goroutine and receives its end before returning.
func runKilledMigration(t *testing.T, suite NativeFuzzSuite, childTest string, delay time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(killDeadline), errFuzzInputDeadline)
	defer cancel()
	database, connection := suite.OpenNative(t)
	if suite.Engine == engineDuckDB {

		require.NoError(t, database.Close())
	}
	createKillLog(ctx, t, suite, connection)

	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+childTest+"$", "-test.count=1") //nolint:gosec // re-runs this test binary
	child.Env = append(os.Environ(), killChildEnvironment+"="+connection)
	var output bytes.Buffer
	child.Stdout, child.Stderr = &output, &output
	require.NoError(t, child.Start())
	finished := make(chan error, 1)
	go func() { finished <- child.Wait() }()
	select {
	case err := <-finished:
		if err != nil {
			require.Failf(t, "the child failed before it was killed", "%v\n%s", err, output.String())
		}
		t.Log("the child finished before it was killed")
	case <-time.After(delay):
		require.NoError(t, child.Process.Kill())
		<-finished
	}

	if suite.Engine == engineDuckDB {
		reopened, err := sql.Open(suite.Driver.Name, connection)
		require.NoError(t, err)
		t.Cleanup(func() { _ = reopened.Close() })
		database = reopened
	}
	files := killHistory(suite.Engine)
	recovery, err := migrate.NewMigrator(database, executionBackend(suite.Engine), files, "migrations")
	require.NoError(t, err)
	_, err = recovery.Up(ctx)
	require.NoError(t, err, "the run after the kill did not finish the migrations")
	checkKillEffects(t, countEffects(loggedValues(ctx, t, database, "kill_log")), killWithoutMigrationTransactions[suite.Engine])
	checkCleanHistory(ctx, t, database, files, suite.Engine)
}

// createKillLog creates the log table on a connection of its own, outside the history, so
// a kill cannot leave the table half made, and closes the connection before the child
// starts, as an engine that lets one process hold a file needs.
//
// Takes t (*testing.T) which reports a failure.
// Takes suite (NativeFuzzSuite) which names the driver.
// Takes connection (string) which names the database.
func createKillLog(ctx context.Context, t *testing.T, suite NativeFuzzSuite, connection string) {
	t.Helper()
	pool, err := sql.Open(suite.Driver.Name, connection)
	require.NoError(t, err)
	defer func() { _ = pool.Close() }()
	statement := "CREATE TABLE kill_log (n BIGINT NOT NULL)"
	if suite.Engine == engineClickHouse {
		statement = "CREATE TABLE kill_log (n Int64) ENGINE = MergeTree ORDER BY tuple()"
	}
	_, err = pool.ExecContext(ctx, statement)
	require.NoError(t, err)
}

// countEffects counts how often each value was logged.
//
// Takes values ([]int64) which are the logged values.
//
// Returns map[int64]int which maps each value to its count.
func countEffects(values []int64) map[int64]int {
	counts := make(map[int64]int, len(values))
	for _, value := range values {
		counts[value]++
	}
	return counts
}

// checkKillEffects checks the logged effects against the guarantees for a killed run.
//
// Takes t (*testing.T) which reports a broken guarantee.
// Takes counts (map[int64]int) which maps each logged value to its count.
// Takes resumesEverything (bool) which is true for an engine that runs every migration
// without a transaction, so every statement is held to the allowance for a resumed one.
func checkKillEffects(t *testing.T, counts map[int64]int, resumesEverything bool) {
	t.Helper()
	transactional, resumed := killTransactionalEffects, killResumedEffects
	if resumesEverything {
		transactional, resumed = nil, slices.Concat(killTransactionalEffects, killResumedEffects)
	}
	for _, value := range transactional {
		assert.Equal(t, 1, counts[value], "the effect %d of a migration in a transaction was not applied once after a kill and a recovery", value)
	}
	replays := 0
	for _, value := range resumed {
		assert.Positive(t, counts[value], "the effect %d of the resumed migration was never applied", value)
		replays += max(counts[value]-1, 0)
	}
	assert.LessOrEqual(t, replays, killReplayAllowance, "the resumed migration replayed more statements than a missing checkpoint allows")
}
