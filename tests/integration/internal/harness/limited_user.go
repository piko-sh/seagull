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
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
)

const (
	// limitedUserPassword is the password of the user a limited-user check creates.
	limitedUserPassword = "limited_pw"

	// limitedUserDigestBytes is how many bytes of a digest name the user.
	limitedUserDigestBytes = 10

	// limitedUserRows is how many rows the two seeds and the backfill leave.
	limitedUserRows = 3
)

// limitedUserEngines holds the engines whose users can be denied the right to create.
var limitedUserEngines = map[string]limitedUserEngine{
	enginePostgres:    postgresLimitedUser(),
	engineTimescaleDB: postgresLimitedUser(),
	engineCockroachDB: cockroachLimitedUser(),
	engineMySQL:       mysqlLimitedUser(),
	engineMariaDB:     mysqlLimitedUser(),
	engineClickHouse: {
		current: "SELECT currentDatabase()",
		driver:  "clickhouse",
		create: func(user, schema string) []string {
			return []string{
				"CREATE USER " + user + " IDENTIFIED WITH plaintext_password BY '" + limitedUserPassword + "'",
				"GRANT SELECT, INSERT, ALTER UPDATE, ALTER DELETE ON " + schema + ".* TO " + user,
			}
		},
		drop:     func(user, _ string) []string { return []string{"DROP USER IF EXISTS " + user} },
		table:    "CREATE TABLE limited_items (id Int32) ENGINE = MergeTree ORDER BY id;\n",
		password: true,
	},
}

// limitedUserEngine says how to create, on one engine, a user who may read and write the
// tables of the isolated schema or database but create nothing in it.
type limitedUserEngine struct {
	// current reads the name of the pool's schema or database.
	current string

	// driver is the database/sql driver the suite opens its pools with.
	driver string

	// create returns the statements creating the user with those rights.
	create func(user, schema string) []string

	// drop returns the statements removing the user.
	drop func(user, schema string) []string

	// table is the CREATE TABLE of the check's first migration.
	table string

	// password reports whether the user signs in with a password.
	password bool
}

// postgresLimitedUser describes a PostgreSQL or TimescaleDB role that signs in with a
// password.
//
// Returns limitedUserEngine which creates the role.
func postgresLimitedUser() limitedUserEngine {
	engine := postgresFamilyLimitedUser(func(user string) string {
		return "CREATE ROLE " + user + " LOGIN PASSWORD '" + limitedUserPassword + "'"
	})
	engine.password = true
	return engine
}

// cockroachLimitedUser describes a CockroachDB user. The insecure test server takes no
// password.
//
// Returns limitedUserEngine which creates the user.
func cockroachLimitedUser() limitedUserEngine {
	return postgresFamilyLimitedUser(func(user string) string { return "CREATE USER " + user })
}

// postgresFamilyLimitedUser describes a role with the use of the schema and the rights to
// read and write its tables and sequences, which PostgreSQL, TimescaleDB and CockroachDB
// share.
//
// Takes login (func(string) string) which writes the statement creating the role.
//
// Returns limitedUserEngine which creates the role without a password.
func postgresFamilyLimitedUser(login func(user string) string) limitedUserEngine {
	return limitedUserEngine{
		current: "SELECT current_schema()",
		driver:  "pgx",
		create: func(user, schema string) []string {
			return []string{
				login(user),
				"GRANT USAGE ON SCHEMA " + schema + " TO " + user,
				"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA " + schema + " TO " + user,
				"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA " + schema + " TO " + user,
			}
		},
		drop: func(user, schema string) []string {
			return []string{
				"REVOKE ALL ON ALL TABLES IN SCHEMA " + schema + " FROM " + user,
				"REVOKE ALL ON ALL SEQUENCES IN SCHEMA " + schema + " FROM " + user,
				"REVOKE ALL ON SCHEMA " + schema + " FROM " + user,
				"DROP ROLE IF EXISTS " + user,
			}
		},
		table:    "CREATE TABLE limited_items (id INTEGER);\n",
		password: false,
	}
}

// mysqlLimitedUser describes a MySQL or MariaDB user with the rights to read and write
// the database's tables.
//
// Returns limitedUserEngine which creates the user.
func mysqlLimitedUser() limitedUserEngine {
	return limitedUserEngine{
		current: "SELECT DATABASE()",
		driver:  "mysql",
		create: func(user, schema string) []string {
			return []string{
				"CREATE USER '" + user + "'@'%' IDENTIFIED BY '" + limitedUserPassword + "'",
				"GRANT SELECT, INSERT, UPDATE, DELETE ON " + schema + ".* TO '" + user + "'@'%'",
			}
		},
		drop:     func(user, _ string) []string { return []string{"DROP USER IF EXISTS '" + user + "'@'%'"} },
		table:    "CREATE TABLE limited_items (id INTEGER);\n",
		password: true,
	}
}

// CheckLimitedUserMigrations checks data migrations under a user without CREATE TABLE.
//
// The user can apply data migrations after an administrator creates the history tables. A
// migration that creates a table must fail and remain pending. The runtime must reuse
// existing history and lock tables because some engines reject even conditional CREATE.
//
// Takes t (*testing.T) which reports a refused or permitted step.
// Takes suite (NativeFuzzSuite) which supplies the isolated administrator connection.
func CheckLimitedUserMigrations(t *testing.T, suite NativeFuzzSuite) {
	t.Helper()
	engine, supported := limitedUserEngines[suite.Engine]
	if !supported {
		t.Skipf("capability exclusion: %s has no users to deny the right to create", suite.Engine)
	}
	admin, connection := suite.OpenNative(t)
	ctx := t.Context()
	var schema string
	require.NoError(t, admin.QueryRowContext(ctx, engine.current).Scan(&schema))
	files := fstest.MapFS{
		"migrations/0001_items.up.sql": {Data: []byte(engine.table)},
		"seeds/0001_fill.sql":          {Data: []byte("INSERT INTO limited_items (id) VALUES (1);\n")},
	}
	backend := executionBackend(suite.Engine)
	runMigrations(ctx, t, admin, backend, files, 1)
	applySeeds(ctx, t, admin, backend, files, 1)

	digest := sha256.Sum256([]byte(UniqueName(t)))
	user := "lim_" + hex.EncodeToString(digest[:limitedUserDigestBytes])
	t.Cleanup(func() {
		cleanup, cancel := CleanupContext(t)
		defer cancel()
		for _, statement := range engine.drop(user, schema) {

			if _, err := admin.ExecContext(cleanup, statement); err != nil {
				t.Logf("removing the limited user: %s: %v", statement, err)
			}
		}
	})
	for _, statement := range engine.create(user, schema) {
		_, err := admin.ExecContext(ctx, statement)
		require.NoError(t, err, "creating the limited user: %s", statement)
	}
	limited := openIsolated(t, engine.driver, withCredentials(connection, user, engine.password))

	files["migrations/0002_backfill.up.sql"] = &fstest.MapFile{Data: []byte("INSERT INTO limited_items (id) VALUES (2);\n")}
	runMigrations(ctx, t, limited, backend, files, 1)
	files["seeds/0002_more.sql"] = &fstest.MapFile{Data: []byte("INSERT INTO limited_items (id) VALUES (3);\n")}
	applySeeds(ctx, t, limited, backend, files, 1)
	var rows int
	require.NoError(t, admin.QueryRowContext(ctx, "SELECT count(*) FROM limited_items").Scan(&rows))
	assert.Equal(t, limitedUserRows, rows, "limited_items does not hold the two seeds' and the backfill's rows")

	files["migrations/0003_more.up.sql"] = &fstest.MapFile{Data: []byte(strings.Replace(engine.table, "limited_items", "limited_more", 1))}
	migrator, err := migrate.NewMigrator(limited, backend, files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(ctx)
	require.Error(t, err, "the limited user created a table")
	runMigrations(ctx, t, admin, backend, files, 1)
}

// runMigrations runs Up and checks how many migrations it applied.
//
// Takes database (*sql.DB) which is the pool to run on.
// Takes backend (migrate.Backend) which is the engine's backend.
// Takes files (fstest.MapFS) which holds the migrations.
// Takes want (int) which is how many Up must apply.
func runMigrations(ctx context.Context, t *testing.T, database *sql.DB, backend migrate.Backend, files fstest.MapFS, want int) {
	t.Helper()
	migrator, err := migrate.NewMigrator(database, backend, files, "migrations")
	require.NoError(t, err)
	applied, err := migrator.Up(ctx)
	require.NoError(t, err, "migrating")
	require.Equal(t, want, applied, "Up applied the wrong number of migrations")
}

// applySeeds applies the seeds and checks how many it applied.
//
// Takes database (*sql.DB) which is the pool to run on.
// Takes backend (migrate.Backend) which is the engine's backend.
// Takes files (fstest.MapFS) which holds the seeds.
// Takes want (int) which is how many Apply must apply.
func applySeeds(ctx context.Context, t *testing.T, database *sql.DB, backend migrate.Backend, files fstest.MapFS, want int) {
	t.Helper()
	seeder, err := migrate.NewSeeder(database, backend, files, "seeds")
	require.NoError(t, err)
	applied, err := seeder.Apply(ctx)
	require.NoError(t, err, "seeding")
	require.Equal(t, want, applied, "Apply applied the wrong number of seeds")
}

// withCredentials returns a connection string signing in as another user. It replaces the
// user information of a URL, or what precedes the @ of a go-sql-driver/mysql data source
// name.
//
// Takes connection (string) which is the connection string.
// Takes user (string) which is the user to sign in as.
// Takes password (bool) which adds limitedUserPassword.
//
// Returns string which is the rewritten connection string.
func withCredentials(connection, user string, password bool) string {
	if parsed, err := url.Parse(connection); err == nil && parsed.Scheme != "" && strings.Contains(connection, "://") {
		parsed.User = url.User(user)
		if password {
			parsed.User = url.UserPassword(user, limitedUserPassword)
		}
		return parsed.String()
	}
	credentials := user
	if password {
		credentials += ":" + limitedUserPassword
	}
	if _, address, found := strings.Cut(connection, "@"); found {
		return credentials + "@" + address
	}
	return credentials + "@" + connection
}
