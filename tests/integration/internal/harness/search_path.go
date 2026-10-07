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

// sessionLocation reads where a database's sessions create tables, a schema or a
// database, and counts the tables of a name there.
type sessionLocation struct {
	// current returns where a pooled session creates tables.
	current func(t *testing.T, database *sql.DB) string

	// count counts the tables of a name in a schema or database.
	count func(t *testing.T, database *sql.DB, location, table string) int
}

// schemaLocation reads the schemas of a database of the PostgreSQL family.
var schemaLocation = sessionLocation{current: currentSchema, count: tableCount}

// SearchPathLifecycle runs migrations and seeds that change the session's search path on
// a database of the PostgreSQL family, and checks where the history goes and how long the
// path lasts.
//
// The first migration creates a schema and moves the search path into it, and the second
// creates a table without naming a schema, so it lands there too: a run applies its
// migrations on one session, as the generator assumes when it replays them. The history
// and seed tables stay in the schema the database's sessions start in, a second run
// applies nothing, and a migration applied by a later run starts from that schema again,
// since every run begins on a fresh session.
//
// Takes database (*sql.DB) which is an empty database whose sessions start in a schema of
// their own.
// Takes backend (migrate.Backend) which is the engine's backend.
func SearchPathLifecycle(t *testing.T, database *sql.DB, backend migrate.Backend) {
	t.Helper()
	ctx := t.Context()

	home := currentSchema(t, database)
	tenant := home + "_tenant"
	execOnCleanup(t, database, "DROP SCHEMA IF EXISTS "+tenant+" CASCADE")
	files := fstest.MapFS{
		"migrations/0001_tenant.up.sql": {Data: []byte("CREATE SCHEMA " + tenant + ";\nSET search_path TO " + tenant + ";\n" +
			"CREATE TABLE widgets (id INT PRIMARY KEY);\n")},
		"migrations/0002_gadgets.up.sql": {Data: []byte("CREATE TABLE gadgets (id INT PRIMARY KEY);\n")},
		"seeds/0001_widgets.sql":         {Data: []byte("SET search_path TO " + tenant + ";\nINSERT INTO widgets (id) VALUES (1);\n")},
	}

	migrator, err := migrate.NewMigrator(database, backend, files, migrationsDirectory)
	require.NoError(t, err)
	applied, err := migrator.Up(ctx)
	require.NoError(t, err, "the history is written in the schema the run started in, whatever path a migration sets")
	require.Equal(t, 2, applied)
	require.Equal(t, "1,2", AppliedVersions(t, migrator))
	require.Equal(t, 1, tableCount(t, database, home, "sgl_migrations"))
	require.Zero(t, tableCount(t, database, tenant, "sgl_migrations"))
	require.Equal(t, 1, tableCount(t, database, tenant, "widgets"))
	require.Equal(t, 1, tableCount(t, database, tenant, "gadgets"), "a SET in one migration lasts for the later migrations of the run")
	require.Equal(t, home, currentSchema(t, database), "the run's path does not reach the connection pool")

	again, err := migrator.Up(ctx)
	require.NoError(t, err)
	require.Zero(t, again)

	seedAcrossSessions(t, database, backend, files, home, schemaLocation)

	files["migrations/0003_sprockets.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE sprockets (id INT PRIMARY KEY);\n")}
	later, err := migrate.NewMigrator(database, backend, files, migrationsDirectory)
	require.NoError(t, err)
	appliedLater, err := later.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, appliedLater)
	require.Equal(t, "1,2,3", AppliedVersions(t, later))
	require.Equal(t, 1, tableCount(t, database, home, "sprockets"), "a later run starts from the path its session starts with")
}

// seedAcrossSessions applies a seed that moves its session elsewhere, twice, and checks
// the seed history stays where the run started.
//
// Takes database (*sql.DB) which holds the migrated schema.
// Takes backend (migrate.Backend) which is the engine's backend.
// Takes files (fstest.MapFS) which holds the seed.
// Takes home (string) which is where the database's sessions start.
// Takes location (sessionLocation) which reads where sessions are and counts tables.
func seedAcrossSessions(t *testing.T, database *sql.DB, backend migrate.Backend, files fstest.MapFS, home string, location sessionLocation) {
	t.Helper()
	ctx := t.Context()

	seeder, err := migrate.NewSeeder(database, backend, files, seedsDirectory)
	require.NoError(t, err)
	seeded, err := seeder.Apply(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, seeded)
	require.Equal(t, 1, location.count(t, database, home, "sgl_seeds"))
	reseeded, err := seeder.Apply(ctx)
	require.NoError(t, err)
	require.Zero(t, reseeded)
	statuses, err := seeder.Status(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	require.True(t, statuses[0].Applied)
	require.Equal(t, home, location.current(t, database))
}

// currentSchema returns the schema a pooled session of the database creates tables in.
//
// Takes database (*sql.DB) which is the database.
//
// Returns string which is the schema.
func currentSchema(t *testing.T, database *sql.DB) string {
	t.Helper()

	var schema string
	require.NoError(t, database.QueryRowContext(t.Context(), "SELECT current_schema()").Scan(&schema))
	return schema
}

// tableCount counts the tables of a name in a schema, through information_schema.
//
// Takes database (*sql.DB) which is the database.
// Takes schema (string) which is the schema.
// Takes table (string) which is the table's name.
//
// Returns int which is 1 when the table exists and 0 otherwise.
func tableCount(t *testing.T, database *sql.DB, schema, table string) int {
	t.Helper()

	var count int
	require.NoError(t, database.QueryRowContext(t.Context(),
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2", schema, table).Scan(&count))
	return count
}
