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
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
)

const (
	// concurrentMigrators is how many migrators race to apply the same history.
	concurrentMigrators = 4

	// concurrentMigrations is how many migrations the history holds.
	concurrentMigrations = 6

	// concurrencyDeadline bounds one race.
	concurrencyDeadline = 2 * time.Minute
)

// CheckConcurrentMigrators runs several migrators, each on a connection pool of its own
// as separate processes would hold, against one database at once, and checks that every
// migration was applied exactly once and recorded once, clean.
//
// Half the migrations run without a transaction, where only the engine's migration lock
// keeps two runners from both applying one. Each migration appends its version to a log
// table, so a migration applied twice leaves two rows. A runner may fail as it loses the
// race, but the history it leaves must be whole.
//
// Takes t (*testing.T) which reports a migration applied other than once.
// Takes suite (NativeFuzzSuite) which opens the database and names its driver.
func CheckConcurrentMigrators(t *testing.T, suite NativeFuzzSuite) {
	t.Helper()
	require.True(t, suite.OpenNative != nil && suite.Driver.Name != "", "the suite opens no native database")
	if suite.Engine == engineClickHouse {
		t.Skip("capability exclusion: ClickHouse has neither session locks nor a unique key a lease row could rely on, " +
			"so its backend takes no migration lock and concurrent runners must be coordinated outside the migrator")
	}
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(concurrencyDeadline), errFuzzInputDeadline)
	defer cancel()
	database, connection := suite.OpenNative(t)
	files := concurrentHistory()

	errs := raceMigrators(ctx, suite, connection, files)
	succeeded := 0
	for runner, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		t.Logf("runner %d stopped: %v", runner, err)
	}
	require.NotZero(t, succeeded, "no runner applied the history")
	counts := appliedCounts(ctx, t, database)
	for version := int64(1); version <= concurrentMigrations; version++ {
		assert.Equal(t, 1, counts[version], "migration %d did not run once across %d concurrent runners", version, concurrentMigrators)
	}
	checkCleanHistory(ctx, t, database, files, suite.Engine)
}

// raceMigrators runs the migrators at once, each on a pool of its own.
//
// Takes suite (NativeFuzzSuite) which names the driver and engine.
// Takes connection (string) which is the shared database's connection string.
// Takes files (fstest.MapFS) which holds the migrations.
//
// Returns []error which holds each runner's outcome.
func raceMigrators(ctx context.Context, suite NativeFuzzSuite, connection string, files fstest.MapFS) []error {
	backend := executionBackend(suite.Engine)
	var group sync.WaitGroup
	errs := make([]error, concurrentMigrators)
	for runner := range concurrentMigrators {
		group.Go(func() {
			pool, err := sql.Open(suite.Driver.Name, concurrentConnection(suite.Driver.Name, connection))
			if err != nil {
				errs[runner] = err
				return
			}
			defer func() { _ = pool.Close() }()
			migrator, err := migrate.NewMigrator(pool, backend, files, "migrations")
			if err != nil {
				errs[runner] = err
				return
			}
			_, errs[runner] = migrator.Up(ctx)
		})
	}
	group.Wait()
	return errs
}

// appliedCounts counts how often each migration logged its version.
//
// Takes t (*testing.T) which reports a failed read.
// Takes database (*sql.DB) which holds the log.
//
// Returns map[int64]int which maps each version to its count.
func appliedCounts(ctx context.Context, t *testing.T, database *sql.DB) map[int64]int {
	t.Helper()
	counts := make(map[int64]int)
	rows, err := database.QueryContext(ctx, "SELECT version FROM applied_log")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var version int64
		require.NoError(t, rows.Scan(&version))
		counts[version]++
	}
	require.NoError(t, rows.Err())
	return counts
}

// checkCleanHistory fails unless every migration is recorded applied and clean and the
// history validates.
//
// Takes t (*testing.T) which reports the history.
// Takes database (*sql.DB) which holds the history.
// Takes files (fstest.MapFS) which holds the migrations.
// Takes engine (string) which selects the backend.
func checkCleanHistory(ctx context.Context, t *testing.T, database *sql.DB, files fstest.MapFS, engine string) {
	t.Helper()
	migrator, err := migrate.NewMigrator(database, executionBackend(engine), files, "migrations")
	require.NoError(t, err)
	statuses, err := migrator.Status(ctx)
	require.NoError(t, err)
	for _, status := range statuses {
		assert.True(t, status.Applied && !status.Dirty, "migration %d is applied=%v dirty=%v after the race", status.Version, status.Applied, status.Dirty)
	}
	assert.NoError(t, migrator.Validate(ctx), "the history does not validate after the race")
}

// concurrentHistory writes the migrations the runners race to apply. The first creates
// the log table, and each appends its version, every other one outside a transaction.
//
// Returns fstest.MapFS which holds the migration files.
func concurrentHistory() fstest.MapFS {
	const table = "CREATE TABLE applied_log (version BIGINT NOT NULL)"
	files := fstest.MapFS{}
	for version := 1; version <= concurrentMigrations; version++ {
		var body strings.Builder
		if version%2 == 0 {
			body.WriteString("-- sgl.migration(no_transaction: true)\n")
		}
		if version == 1 {
			body.WriteString(table + ";\n")
		}
		fmt.Fprintf(&body, "INSERT INTO applied_log (version) VALUES (%d);\n", version)
		files[fmt.Sprintf("migrations/%04d_step.up.sql", version)] = &fstest.MapFile{Data: []byte(body.String())}
	}
	return files
}

// concurrentConnection adds the busy timeout an embedded engine needs for several pools
// to share one file.
//
// Takes driver (string) which names the database/sql driver.
// Takes connection (string) which is the isolated connection string.
//
// Returns string which is the connection string a runner opens.
func concurrentConnection(driver, connection string) string {
	switch driver {
	case driverModernSQLite:
		return "file:" + connection + "?_pragma=busy_timeout%3d20000"
	case driverMattnSQLite:
		return "file:" + connection + "?_busy_timeout=20000"
	default:
		return connection
	}
}
