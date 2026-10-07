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

//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/schema/schema_source_postgres"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const (
	introspectionTimeout = 2 * time.Minute
	migrationSchema      = "public"
	featureSchema        = "introspect_features"
	featureMigration     = `
CREATE TYPE mood AS ENUM ('sad', 'ok', 'happy');
CREATE TYPE address AS (street text, postcode varchar(10), tags text[]);
CREATE DOMAIN email AS varchar(100) NOT NULL;
CREATE TABLE readings (
	id bigint PRIMARY KEY,
	scores int4[] NOT NULL,
	labels text[],
	grid int[][],
	codes varchar(10)[],
	feeling mood NOT NULL DEFAULT 'ok',
	feelings mood[],
	home address,
	price numeric(10, 2),
	created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE contacts (id int NOT NULL, address email, history email[]);
CREATE TABLE measurements (id int NOT NULL, taken_on date NOT NULL) PARTITION BY RANGE (taken_on);
CREATE TABLE measurements_2026 PARTITION OF measurements FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
CREATE EXTENSION IF NOT EXISTS file_fdw;
CREATE SERVER IF NOT EXISTS introspect_files FOREIGN DATA WRAPPER file_fdw;
CREATE FOREIGN TABLE remote_readings (id int NOT NULL, note text) SERVER introspect_files OPTIONS (filename '/dev/null');
CREATE EXTENSION pg_buffercache;
CREATE MATERIALIZED VIEW reading_totals AS SELECT id, scores FROM readings;
`
)

var introspectionAllowlist = harness.Allowlist{}

func TestIntrospectionMatchesMigrations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	entries, err := os.ReadDir("testdata")
	require.NoError(t, err)

	checked := make(map[string]bool)
	used := make(map[string]bool)
	for _, entry := range entries {
		testCaseDirectory := filepath.Join("testdata", entry.Name())
		if _, statError := os.Stat(filepath.Join(testCaseDirectory, "migrations")); statError != nil {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			checked[entry.Name()] = true
			schemaName := caseSchemas().Create(t, testCaseDirectory)
			migrations := replayMigrations(t, os.DirFS(testCaseDirectory))
			database := introspectDatabase(t)

			differences := harness.CompareCatalogues(
				harness.CatalogueSchema{Catalogue: migrations, Name: migrationSchema},
				harness.CatalogueSchema{Catalogue: database, Name: schemaName},
			)
			introspectionAllowlist.Check(t, entry.Name(), differences, used)
		})
	}

	introspectionAllowlist.CheckUsed(t, checked, used)
}

func TestIntrospectionDescribesPostgresFeatures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	createFeatureSchema(t)
	migrations := replayMigrations(t, fstest.MapFS{
		"migrations/001_schema.up.sql": &fstest.MapFile{Data: []byte(featureMigration)},
	})
	database := introspectDatabase(t)

	differences := harness.CompareCatalogues(
		harness.CatalogueSchema{Catalogue: migrations, Name: migrationSchema},
		harness.CatalogueSchema{Catalogue: database, Name: featureSchema},
	)
	used := make(map[string]bool)
	introspectionAllowlist.Check(t, "features", differences, used)
	introspectionAllowlist.CheckUsed(t, map[string]bool{"features": true}, used)

	schema := database.Schemas[featureSchema]
	require.NotNil(t, schema)

	assert.ElementsMatch(t, []string{"contacts", "measurements", "measurements_2026", "readings", "remote_readings"},
		slices.Collect(maps.Keys(schema.Tables)), "the partitioned parent, the partition and the foreign table are tables")
	assert.Equal(t, &dialect.TableName{Schema: featureSchema, Name: "measurements"}, schema.Tables["measurements_2026"].PartitionOf)
	assert.Nil(t, schema.Tables["measurements"].PartitionOf)
	assert.ElementsMatch(t, []string{"reading_totals"}, slices.Collect(maps.Keys(schema.Views)),
		"the materialised view is a view; the extension's views are not")

	contacts := harness.ColumnsByName(schema.Tables["contacts"].Columns)
	assert.Equal(t, "varchar", contacts["address"].SQLType.EngineName, "a domain resolves to the type it is declared over")
	require.NotNil(t, contacts["address"].SQLType.Length)
	assert.Equal(t, 100, *contacts["address"].SQLType.Length)
	assert.False(t, contacts["address"].Nullable, "a NOT NULL domain makes the column non-nullable")
	assert.Equal(t, 1, contacts["history"].ArrayDimensions)
	assert.True(t, contacts["history"].Nullable, "an array of a NOT NULL domain may itself be NULL")

	remote := harness.ColumnsByName(schema.Tables["remote_readings"].Columns)
	assert.False(t, remote["id"].Nullable)
	assert.Equal(t, "text", remote["note"].SQLType.EngineName)

	readings := harness.ColumnsByName(schema.Tables["readings"].Columns)
	assert.Equal(t, dialect.TypeCategoryEnum, readings["feelings"].SQLType.Category)
	assert.Equal(t, featureSchema, readings["feelings"].SQLType.Schema)
	assert.Equal(t, []string{"sad", "ok", "happy"}, readings["feelings"].SQLType.EnumValues)

	for schemaName := range database.Schemas {
		assert.False(t, strings.HasPrefix(schemaName, "pg_"), "system schema %s is introspected", schemaName)
		assert.NotEqual(t, "information_schema", schemaName)
	}
}

func createFeatureSchema(t *testing.T) {
	t.Helper()

	connection, err := sql.Open("pgx", testConnectionString)
	require.NoError(t, err)
	defer func() { assert.NoError(t, connection.Close()) }()

	ctx := t.Context()
	statements := fmt.Sprintf("DROP EXTENSION IF EXISTS pg_buffercache CASCADE; DROP SCHEMA IF EXISTS %[1]s CASCADE; "+
		"CREATE SCHEMA %[1]s; SET search_path TO %[1]s;\n%s", featureSchema, featureMigration)
	_, err = connection.ExecContext(ctx, statements)
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupConnection, cleanupError := sql.Open("pgx", testConnectionString)
		if cleanupError != nil {
			return
		}
		defer func() { _ = cleanupConnection.Close() }()
		ctx, cancel := harness.CleanupContext(t)
		defer cancel()
		_, _ = cleanupConnection.ExecContext(ctx,
			"DROP SCHEMA IF EXISTS "+featureSchema+" CASCADE; DROP SERVER IF EXISTS introspect_files CASCADE")
	})
}

func replayMigrations(t *testing.T, migrations fs.FS) *dialect.Catalogue {
	t.Helper()

	return harness.ReplayCatalogue(t, dialect_engine_postgres.New(), migrations)
}

func introspectDatabase(t *testing.T) *dialect.Catalogue {
	t.Helper()

	ctx, cancel := context.WithTimeoutCause(t.Context(), introspectionTimeout,
		errors.New("catalogue introspection did not finish in time"))
	defer cancel()

	database, err := sql.Open("pgx", testConnectionString)
	require.NoError(t, err)
	defer func() { assert.NoError(t, database.Close()) }()

	catalogue, sourceErrors, err := schema_source_postgres.New(database, dialect_engine_postgres.New()).BuildCatalogue(ctx)
	require.NoError(t, err)
	assert.Empty(t, sourceErrors)
	require.NotNil(t, catalogue)
	return catalogue
}
