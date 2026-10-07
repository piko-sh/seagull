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

package harness_test

import (
	"database/sql"
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate/migrate_backend_postgres"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const historyQuery = "SELECT version, dirty, COALESCE(last_statement, -1) FROM sgl_migrations ORDER BY version"

func TestMigrationFilesHoldTheLifecycleAndItsSeed(t *testing.T) {
	t.Parallel()

	files := harness.MigrationFiles("CREATE", "ADD", "DROP COLUMN", "DELETE")

	require.Equal(t, "CREATE", string(files["migrations/0001_create_widgets.up.sql"].Data))
	require.Equal(t, "DROP TABLE sgl_it_widgets;\n", string(files["migrations/0001_create_widgets.down.sql"].Data))
	require.Equal(t, "ADD", string(files["migrations/0002_add_colour.up.sql"].Data))
	require.Equal(t, "DROP COLUMN", string(files["migrations/0002_add_colour.down.sql"].Data))
	require.Contains(t, string(files["seeds/001_widgets.sql"].Data), "DELETE\nINSERT INTO sgl_it_widgets (id, label) VALUES (1, 'first');")
}

func TestChecksumOfIsTheHexadecimalSHA256(t *testing.T) {
	t.Parallel()

	require.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", harness.ChecksumOf(nil))
}

func TestWithParameterAppendsToTheQuery(t *testing.T) {
	t.Parallel()

	require.Equal(t, "postgres://host/db?search_path=s", harness.WithParameter("postgres://host/db", "search_path", "s"))
	require.Equal(t, "postgres://host/db?sslmode=disable&search_path=s", harness.WithParameter("postgres://host/db?sslmode=disable", "search_path", "s"))
}

func TestSettleImmediatelyChecksOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "a change already visible"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			checks := 0
			harness.SettleImmediately(t, func() bool {
				checks++
				return true
			})
			require.Equal(t, 1, checks)
		})
	}
}

func TestCountOfReadsTheOneCountAQueryReturns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		count func(t *testing.T, database *sql.DB) int
		name  string
		query string
		want  int
	}{
		{name: "any count query", query: "SELECT count(*) FROM boats", want: 3, count: func(t *testing.T, database *sql.DB) int {
			t.Helper()
			return harness.CountOf(t, database, "SELECT count(*) FROM boats")
		}},
		{name: "the lifecycle's widgets", query: "SELECT COUNT(*) FROM " + harness.MigrationTable, want: 2, count: harness.CountWidgets},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, name := newFakeServer(t)
			server.answer(test.query, []driver.Value{int64(test.want)})
			database := openFakeDatabase(t, name)
			require.Equal(t, test.want, test.count(t, database))
		})
	}
}

func TestReadHistoryReturnsTheHistoryRowsInVersionOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rows [][]driver.Value
		want []harness.HistoryEntry
	}{
		{name: "no history", rows: nil, want: nil},
		{
			name: "a clean and a dirty migration",
			rows: [][]driver.Value{{int64(1), false, int64(-1)}, {int64(2), true, int64(3)}},
			want: []harness.HistoryEntry{{Version: 1, LastStatement: -1, Dirty: false}, {Version: 2, LastStatement: 3, Dirty: true}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, name := newFakeServer(t)
			server.answer(historyQuery, test.rows...)
			require.Equal(t, test.want, harness.ReadHistory(t, openFakeDatabase(t, name)))
		})
	}
}

func TestReplicasBuildsAMigratorPerReplicaBeforeAnyRuns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		count int
	}{
		{name: "one replica", count: 1},
		{name: "several replicas", count: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server, name := newFakeServer(t)
			files := harness.MigrationFiles("CREATE TABLE sgl_it_widgets (id INT);", "ADD", "DROP COLUMN", "DELETE")
			boots := harness.Replicas(t, openFakeDatabase(t, name), migrate_backend_postgres.New(), files, test.count)
			require.Len(t, boots, test.count)
			require.Empty(t, server.recorded(), "building the replicas runs nothing")
		})
	}
}

func openFakeDatabase(t *testing.T, name string) *sql.DB {
	t.Helper()

	database, err := sql.Open(fakeDriverName, "fake://"+name)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	return database
}
