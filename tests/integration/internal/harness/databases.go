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
	"cmp"
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	// casePrefix starts the name of the schema or database a case runs in.
	casePrefix = "test_"

	// upMigrationGlob matches the names of a directory's up migration files.
	upMigrationGlob = "*.up.sql"

	// databaseURLVariable is the variable a runner reads its connection string from.
	databaseURLVariable = "DATABASE_URL="

	// databaseSchemaVariable is the variable a runner reads its schema from.
	databaseSchemaVariable = "DATABASE_SCHEMA="
)

// upMigrationPattern matches the name of a forward migration as the migration runner and
// the generator read one: <version>_<name>.up.sql, capturing the version.
var upMigrationPattern = regexp.MustCompile(`^0*(\d+)_.+\.up\.sql$`)

// Schemas runs each case of a server suite in a schema of its own, as the PostgreSQL
// family's suites do.
type Schemas struct {
	// Driver is the database/sql driver name the suite opens the server with.
	Driver string

	// Connection is the connection string of the suite's server.
	Connection string

	// Setup holds statements run before a case's schema is created, such as creating an
	// extension.
	Setup []string

	// SearchPath holds the schemas that follow a case's own on the search path its
	// migrations run with, such as public for an extension's functions.
	SearchPath []string
}

// Prepare creates a case's schema, applies the case's migrations in it, and returns the
// runner's DATABASE_URL and DATABASE_SCHEMA. The schema is dropped when the case ends.
//
// Takes caseDirectory (string) which is the case's directory.
//
// Returns []string which holds the runner's variables.
func (schemas Schemas) Prepare(ctx context.Context, t *testing.T, caseDirectory string) []string {
	t.Helper()

	schemaName := schemas.create(ctx, t, caseDirectory)
	return []string{databaseURLVariable + schemas.Connection, databaseSchemaVariable + schemaName}
}

// Create creates a case's schema and applies the case's forward migrations in it, each
// file as one statement batch with the schema first on the search path. The schema is
// dropped when the case ends.
//
// Takes caseDirectory (string) which is the case's directory.
//
// Returns string which is the schema's name.
func (schemas Schemas) Create(t *testing.T, caseDirectory string) string {
	t.Helper()

	return schemas.create(t.Context(), t, caseDirectory)
}

// create creates a case's schema under ctx, as Create describes.
//
// Takes caseDirectory (string) which is the case's directory.
//
// Returns string which is the schema's name.
func (schemas Schemas) create(ctx context.Context, t *testing.T, caseDirectory string) string {
	t.Helper()

	schemaName := CaseName(caseDirectory)
	searchPath := strings.Join(append([]string{schemaName}, schemas.SearchPath...), ", ")
	statements := slices.Clone(schemas.Setup)
	statements = append(statements,
		"DROP SCHEMA IF EXISTS "+schemaName+" CASCADE",
		"CREATE SCHEMA "+schemaName,
	)
	connection := openServer(t, schemas.Driver, schemas.Connection)
	defer func() { _ = connection.Close() }()
	for _, statement := range statements {
		_, err := connection.ExecContext(ctx, statement)
		require.NoError(t, err, "running %s", statement)
	}
	for _, file := range CaseMigrations(t, caseDirectory) {
		_, err := connection.ExecContext(ctx, "SET search_path TO "+searchPath+";\n"+file.Content)
		require.NoError(t, err, "applying migration %s", file.Name)
	}
	dropOnCleanup(t, schemas.Driver, schemas.Connection, "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE")
	return schemaName
}

// Databases runs each case of a server suite in a database of its own, as the MySQL
// family's suites do.
type Databases struct {
	// Driver is the database/sql driver name the suite opens the server with.
	Driver string

	// Connection is the connection string of the suite's server, which must allow more than
	// one statement per call so a migration file runs as one batch.
	Connection string
}

// Prepare creates a case's database, applies the case's forward migrations in it, and
// returns the runner's DATABASE_URL, which names that database. The database is dropped
// when the case ends.
//
// Takes caseDirectory (string) which is the case's directory.
//
// Returns []string which holds the runner's variables.
func (databases Databases) Prepare(ctx context.Context, t *testing.T, caseDirectory string) []string {
	t.Helper()

	databaseName := CaseName(caseDirectory)
	connection := openServer(t, databases.Driver, databases.Connection)
	defer func() { _ = connection.Close() }()
	for _, statement := range []string{"DROP DATABASE IF EXISTS " + databaseName, "CREATE DATABASE " + databaseName, "USE " + databaseName} {
		_, err := connection.ExecContext(ctx, statement)
		require.NoError(t, err, "running %s", statement)
	}
	for _, file := range CaseMigrations(t, caseDirectory) {
		_, err := connection.ExecContext(ctx, file.Content)
		require.NoError(t, err, "applying migration %s", file.Name)
	}
	dropOnCleanup(t, databases.Driver, databases.Connection, "DROP DATABASE IF EXISTS "+databaseName)
	server, _, _ := strings.Cut(databases.Connection, "/")
	return []string{databaseURLVariable + server + "/" + databaseName + "?parseTime=true"}
}

// Migration is one forward migration file of a case.
type Migration struct {
	// Name is the file's name.
	Name string

	// Content is the file's SQL.
	Content string
}

// CaseName returns the name of the schema or database a case runs in: test_ and the case
// directory's name, with each hyphen an underscore.
//
// Takes caseDirectory (string) which is the case's directory.
//
// Returns string which is the name.
func CaseName(caseDirectory string) string {
	return casePrefix + strings.ReplaceAll(filepath.Base(caseDirectory), "-", "_")
}

// CaseMigrations reads a case's forward migrations, the files the generator replays, in
// the order the migration runner applies them: by version, then by name. Down migrations
// and any other file are left out.
//
// Takes caseDirectory (string) which is the case's directory.
//
// Returns []Migration which holds the files.
func CaseMigrations(t *testing.T, caseDirectory string) []Migration {
	t.Helper()

	migrations := os.DirFS(filepath.Join(caseDirectory, migrationDirectory))
	names, err := fs.Glob(migrations, upMigrationGlob)
	require.NoError(t, err)
	names = slices.DeleteFunc(names, func(name string) bool { return !IsUpMigration(name) })
	slices.SortStableFunc(names, compareMigrationNames)
	files := make([]Migration, 0, len(names))
	for _, name := range names {
		content, readErr := fs.ReadFile(migrations, name)
		require.NoError(t, readErr, "reading migration %s", name)
		files = append(files, Migration{Name: path.Base(name), Content: string(content)})
	}
	return files
}

// IsUpMigration reports whether a file name is a forward migration as the migration
// runner reads one, <version>_<name>.up.sql, so a down migration is never applied as
// though it were the next step.
//
// Takes name (string) which is the file's name.
//
// Returns bool which is true for a forward migration.
func IsUpMigration(name string) bool {
	return upMigrationPattern.MatchString(name)
}

// compareMigrationNames orders two forward migrations by version, compared as numbers of
// any length, then by name.
//
// Takes left (string) which is one file's name.
// Takes right (string) which is the other's.
//
// Returns int which is negative, zero or positive as left sorts before, with or after
// right.
func compareMigrationNames(left, right string) int {
	leftVersion := migrationVersion(left)
	rightVersion := migrationVersion(right)
	return cmp.Or(
		cmp.Compare(len(leftVersion), len(rightVersion)),
		strings.Compare(leftVersion, rightVersion),
		strings.Compare(left, right),
	)
}

// migrationVersion returns a forward migration's version without its leading zeros, so
// versions of different lengths compare as numbers by length and then digit by digit.
//
// Takes name (string) which is the file's name.
//
// Returns string which is the version's digits, or empty for a name that is not a forward
// migration.
func migrationVersion(name string) string {
	match := upMigrationPattern.FindStringSubmatch(name)
	if match == nil {
		return ""
	}
	return match[1]
}

// openServer opens the suite's server; the caller closes it.
//
// Takes driverName (string) which is the database/sql driver name.
// Takes connectionString (string) which is the server's connection string.
//
// Returns *sql.DB which is the open connection pool.
func openServer(t *testing.T, driverName, connectionString string) *sql.DB {
	t.Helper()

	connection, err := sql.Open(driverName, connectionString)
	require.NoError(t, err)
	return connection
}
