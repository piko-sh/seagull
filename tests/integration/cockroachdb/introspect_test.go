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

package cockroachdb_test

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
	"seagull.sh/seagull/sdk/dialect/dialect_engine_cockroachdb"
	"seagull.sh/seagull/sdk/schema/schema_source_postgres"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const (
	introspectionTimeout = 2 * time.Minute
	migrationSchema      = "public"
	featureSchema        = "introspect_features"
	featureMigration     = `
CREATE TYPE mood AS ENUM ('sad', 'ok', 'happy');
CREATE TABLE readings (
	id INT8 PRIMARY KEY,
	scores INT4[] NOT NULL,
	labels STRING[],
	codes VARCHAR(10)[],
	feeling mood NOT NULL DEFAULT 'ok',
	feelings mood[],
	price DECIMAL(10, 2),
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE VIEW reading_labels AS SELECT id, labels FROM readings;
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

func TestIntrospectionDescribesCockroachDBFeatures(t *testing.T) {
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

	assert.ElementsMatch(t, []string{"readings"}, slices.Collect(maps.Keys(schema.Tables)))
	assert.ElementsMatch(t, []string{"reading_labels"}, slices.Collect(maps.Keys(schema.Views)))

	readings := harness.ColumnsByName(schema.Tables["readings"].Columns)
	assert.Equal(t, 1, readings["scores"].ArrayDimensions)
	assert.Equal(t, dialect.TypeCategoryEnum, readings["feelings"].SQLType.Category)
	assert.Equal(t, featureSchema, readings["feelings"].SQLType.Schema)
	assert.Equal(t, []string{"sad", "ok", "happy"}, readings["feelings"].SQLType.EnumValues)

	for schemaName := range database.Schemas {
		assert.False(t, strings.HasPrefix(schemaName, "pg_"), "system schema %s is introspected", schemaName)
		assert.NotContains(t, []string{"information_schema", "crdb_internal"}, schemaName)
	}
}

func createFeatureSchema(t *testing.T) {
	t.Helper()

	connection, err := sql.Open("pgx", testConnectionString)
	require.NoError(t, err)
	defer func() { assert.NoError(t, connection.Close()) }()

	ctx := t.Context()
	statements := fmt.Sprintf("DROP SCHEMA IF EXISTS %[1]s CASCADE; CREATE SCHEMA %[1]s; SET search_path TO %[1]s;\n%s",
		featureSchema, featureMigration)
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
			"DROP SCHEMA IF EXISTS "+featureSchema+" CASCADE")
	})
}

func replayMigrations(t *testing.T, migrations fs.FS) *dialect.Catalogue {
	t.Helper()

	return harness.ReplayCatalogue(t, dialect_engine_cockroachdb.New(), migrations)
}

func introspectDatabase(t *testing.T) *dialect.Catalogue {
	t.Helper()

	ctx, cancel := context.WithTimeoutCause(t.Context(), introspectionTimeout,
		errors.New("catalogue introspection did not finish in time"))
	defer cancel()

	database, err := sql.Open("pgx", testConnectionString)
	require.NoError(t, err)
	defer func() { assert.NoError(t, database.Close()) }()

	catalogue, sourceErrors, err := schema_source_postgres.New(database, dialect_engine_cockroachdb.New()).BuildCatalogue(ctx)
	require.NoError(t, err)
	assert.Empty(t, sourceErrors)
	require.NotNil(t, catalogue)
	return catalogue
}
