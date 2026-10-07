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

package timescaledb_test

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
	"seagull.sh/seagull/sdk/dialect/dialect_engine_timescaledb"
	"seagull.sh/seagull/sdk/schema/schema_source_postgres"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const (
	introspectionTimeout = 2 * time.Minute
	migrationSchema      = "public"
	featureSchema        = "introspect_features"
	featureMigration     = `
CREATE TABLE conditions (
	time TIMESTAMPTZ NOT NULL,
	device_id INTEGER NOT NULL,
	temperature DOUBLE PRECISION,
	tags TEXT[]
);
SELECT create_hypertable('conditions', 'time', chunk_time_interval => INTERVAL '1 day');
CREATE MATERIALIZED VIEW conditions_daily
WITH (timescaledb.continuous, timescaledb.materialized_only = false) AS
SELECT time_bucket('1 day', time) AS bucket, device_id, avg(temperature) AS mean_temperature
FROM conditions
GROUP BY bucket, device_id
WITH NO DATA;
`
	featureData = `
INSERT INTO conditions (time, device_id, temperature, tags) VALUES
	('2026-01-01 00:00:00+00', 1, 20.5, '{indoor}'),
	('2026-01-05 00:00:00+00', 2, 21.0, NULL),
	('2026-02-01 00:00:00+00', 1, 19.0, NULL);
ALTER TABLE conditions SET (timescaledb.compress, timescaledb.compress_segmentby = 'device_id');
SELECT compress_chunk(chunk) FROM show_chunks('conditions') chunk;
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
			skipMarkedFixture(t, testCaseDirectory)
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

func TestIntrospectionHidesTimescaleDBInternals(t *testing.T) {
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

	assert.ElementsMatch(t, []string{"conditions"}, slices.Collect(maps.Keys(schema.Tables)))
	assert.ElementsMatch(t, []string{"conditions_daily"}, slices.Collect(maps.Keys(schema.Views)))
	assert.Equal(t, 1, harness.ColumnsByName(schema.Tables["conditions"].Columns)["tags"].ArrayDimensions)

	for schemaName, contents := range database.Schemas {
		assert.False(t, strings.HasPrefix(schemaName, "_timescaledb"), "internal schema %s is introspected", schemaName)
		assert.False(t, strings.HasPrefix(schemaName, "pg_"), "system schema %s is introspected", schemaName)
		assert.NotContains(t, []string{"timescaledb_information", "timescaledb_experimental", "toolkit_experimental"}, schemaName,
			"extension schema %s is introspected", schemaName)
		for tableName := range contents.Tables {
			assert.False(t, isTimescaleDBInternalTable(tableName), "internal table %s.%s is introspected", schemaName, tableName)
		}
	}
}

func isTimescaleDBInternalTable(name string) bool {
	for _, prefix := range []string{"_hyper_", "_compressed_hypertable_", "_materialized_hypertable_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func skipMarkedFixture(t *testing.T, testCaseDirectory string) {
	t.Helper()

	if _, statError := os.Stat(filepath.Join(testCaseDirectory, "testspec.json")); statError != nil {
		return
	}
	if spec := harness.LoadSpec(t, testCaseDirectory); spec.Skip {
		t.Skip(spec.SkipReason)
	}
}

func createFeatureSchema(t *testing.T) {
	t.Helper()

	connection, err := sql.Open("pgx", testConnectionString)
	require.NoError(t, err)
	defer func() { assert.NoError(t, connection.Close()) }()

	ctx := t.Context()
	_, err = connection.ExecContext(ctx, fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE; "+
		"DROP SCHEMA IF EXISTS %[1]s CASCADE; CREATE SCHEMA %[1]s; SET search_path TO %[1]s, public;\n%s",
		featureSchema, featureMigration))
	require.NoError(t, err)
	_, err = connection.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s, public;\n%s", featureSchema, featureData))
	require.NoError(t, err)

	var chunkCount int
	require.NoError(t, connection.QueryRowContext(ctx,
		"SELECT count(*) FROM public.show_chunks('"+featureSchema+".conditions')").Scan(&chunkCount))
	require.Positive(t, chunkCount, "the hypertable has chunks to hide")

	t.Cleanup(func() {
		cleanupConnection, cleanupError := sql.Open("pgx", testConnectionString)
		if cleanupError != nil {
			return
		}
		defer func() { _ = cleanupConnection.Close() }()
		ctx, cancel := harness.CleanupContext(t)
		defer cancel()
		_, _ = cleanupConnection.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+featureSchema+" CASCADE")
	})
}

func replayMigrations(t *testing.T, migrations fs.FS) *dialect.Catalogue {
	t.Helper()

	return harness.ReplayCatalogue(t, dialect_engine_timescaledb.New(), migrations)
}

func introspectDatabase(t *testing.T) *dialect.Catalogue {
	t.Helper()

	ctx, cancel := context.WithTimeoutCause(t.Context(), introspectionTimeout,
		errors.New("catalogue introspection did not finish in time"))
	defer cancel()

	database, err := sql.Open("pgx", testConnectionString)
	require.NoError(t, err)
	defer func() { assert.NoError(t, database.Close()) }()

	catalogue, sourceErrors, err := schema_source_postgres.New(database, dialect_engine_timescaledb.New()).BuildCatalogue(ctx)
	require.NoError(t, err)
	assert.Empty(t, sourceErrors)
	require.NotNil(t, catalogue)
	return catalogue
}
