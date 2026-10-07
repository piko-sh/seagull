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
	"database/sql"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
)

// databaseLocation reads the databases of a MySQL or MariaDB server.
var databaseLocation = sessionLocation{current: currentDatabase, count: databaseTableCount}

// DatabaseSwitchLifecycle runs migrations and seeds that move the session to another
// database with USE on MySQL or MariaDB, and checks where the history goes and how long
// the move lasts.
//
// The first migration creates a database and moves into it, and the second creates a
// table without naming a database, so it lands there too: a run applies its migrations on
// one session. The history and seed tables stay in the database the run started in, a
// second run applies nothing, and a migration applied by a later run starts from that
// database again, since every run begins on a fresh session.
//
// Takes database (*sql.DB) which is an empty database its connection string names.
// Takes backend (migrate.Backend) which is the engine's backend.
func DatabaseSwitchLifecycle(t *testing.T, database *sql.DB, backend migrate.Backend) {
	t.Helper()
	ctx := t.Context()

	home := currentDatabase(t, database)
	other := home + "_other"
	execOnCleanup(t, database, "DROP DATABASE IF EXISTS "+other)
	files := fstest.MapFS{
		"migrations/0001_other.up.sql": {Data: []byte("CREATE DATABASE " + other + ";\nUSE " + other + ";\n" +
			"CREATE TABLE widgets (id INT PRIMARY KEY);\n")},
		"migrations/0002_gadgets.up.sql": {Data: []byte("CREATE TABLE gadgets (id INT PRIMARY KEY);\n")},
		"seeds/0001_widgets.sql":         {Data: []byte("USE " + other + ";\nINSERT INTO widgets (id) VALUES (1);\n")},
	}

	migrator, err := migrate.NewMigrator(database, backend, files, migrationsDirectory)
	require.NoError(t, err)
	applied, err := migrator.Up(ctx)
	require.NoError(t, err, "the history is written in the database the run started in, whatever database a migration uses")
	require.Equal(t, 2, applied)
	require.Equal(t, "1,2", AppliedVersions(t, migrator))
	require.Equal(t, 1, databaseTableCount(t, database, home, "sgl_migrations"))
	require.Zero(t, databaseTableCount(t, database, other, "sgl_migrations"))
	require.Equal(t, 1, databaseTableCount(t, database, other, "widgets"))
	require.Equal(t, 1, databaseTableCount(t, database, other, "gadgets"), "a USE in one migration lasts for the later migrations of the run")
	require.Equal(t, home, currentDatabase(t, database), "the run's database does not reach the connection pool")

	again, err := migrator.Up(ctx)
	require.NoError(t, err)
	require.Zero(t, again, "a second run finds its history and applies nothing")

	seedAcrossSessions(t, database, backend, files, home, databaseLocation)

	files["migrations/0003_sprockets.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE sprockets (id INT PRIMARY KEY);\n")}
	later, err := migrate.NewMigrator(database, backend, files, migrationsDirectory)
	require.NoError(t, err)
	appliedLater, err := later.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, appliedLater)
	require.Equal(t, "1,2,3", AppliedVersions(t, later))
	require.Equal(t, 1, databaseTableCount(t, database, home, "sprockets"), "a later run starts from the database its session starts with")
}

// currentDatabase returns the database a pooled session of a MySQL or MariaDB server
// creates tables in.
//
// Takes database (*sql.DB) which is the database.
//
// Returns string which is the database's name.
func currentDatabase(t *testing.T, database *sql.DB) string {
	t.Helper()

	var name string
	require.NoError(t, database.QueryRowContext(t.Context(), "SELECT DATABASE()").Scan(&name))
	return name
}

// databaseTableCount counts the tables of a name in a MySQL or MariaDB database, through
// information_schema.
//
// Takes database (*sql.DB) which is the database.
// Takes schema (string) which is the database the table is counted in.
// Takes table (string) which is the table's name.
//
// Returns int which is 1 when the table exists and 0 otherwise.
func databaseTableCount(t *testing.T, database *sql.DB, schema, table string) int {
	t.Helper()

	var count int
	require.NoError(t, database.QueryRowContext(t.Context(),
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = ?", schema, table).Scan(&count))
	return count
}
