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

package catalogue

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
)

const partitionedMeasurements = `
CREATE TABLE measurements (id serial, taken_on date NOT NULL, reading numeric, PRIMARY KEY (id, taken_on))
	PARTITION BY RANGE (taken_on);
CREATE TABLE measurements_2026 PARTITION OF measurements FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
`

func replayPostgresMigrations(t *testing.T, migrations ...string) (*sqlmodel.Catalogue, []diag.SourceError) {
	t.Helper()

	builder := newCatalogueBuilder(postgres.New(), namespace.Default())
	diagnostics := make([]diag.SourceError, 0, len(migrations))
	for index, migration := range migrations {
		diagnostics = append(diagnostics, applyMigration(t, builder, "migration.sql", migration, index)...)
	}
	return builder.Catalogue(), diagnostics
}

func schemaTableNames(catalogue *sqlmodel.Catalogue, schemaName string) []string {
	schema := catalogue.Schemas[schemaName]
	if schema == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(schema.Tables))
}

func diagnosticMessages(diagnostics []diag.SourceError) []string {
	messages := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		messages = append(messages, diagnostic.Message)
	}
	return messages
}

func columnNamesOf(columns []sqlmodel.Column) []string {
	names := make([]string, 0, len(columns))
	for index := range columns {
		names = append(names, columns[index].Name)
	}
	return names
}

func partitionParent(catalogue *sqlmodel.Catalogue, schemaName, name string) string {
	table := catalogue.LookupTable(schemaName, name)
	if table == nil || table.PartitionOf == nil {
		return ""
	}
	return table.PartitionOf.Schema + "." + table.PartitionOf.Name
}

func TestPartitionReplayRegistersEachPartitionAsATable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		migrations  []string
		wantTables  []string
		wantParents map[string]string
	}{
		{
			name:        "a partition is a table of its parent",
			migrations:  []string{partitionedMeasurements},
			wantTables:  []string{"measurements", "measurements_2026"},
			wantParents: map[string]string{"measurements": "", "measurements_2026": "public.measurements"},
		},
		{
			name: "a renamed partition is still a partition",
			migrations: []string{partitionedMeasurements, `
ALTER TABLE measurements_2026 RENAME TO measurements_y2026;
ALTER TABLE measurements_y2026 ALTER COLUMN reading SET NOT NULL;
`},
			wantTables:  []string{"measurements", "measurements_y2026"},
			wantParents: map[string]string{"measurements_y2026": "public.measurements"},
		},
		{
			name: "a dropped partition frees its name for a table",
			migrations: []string{partitionedMeasurements, `
DROP TABLE IF EXISTS measurements_2026;
CREATE TABLE measurements_2026 (note text);
`},
			wantTables:  []string{"measurements", "measurements_2026"},
			wantParents: map[string]string{"measurements_2026": ""},
		},
		{
			name: "an attached table becomes a partition",
			migrations: []string{partitionedMeasurements, `
CREATE TABLE measurements_2028 (LIKE measurements INCLUDING ALL);
ALTER TABLE measurements ATTACH PARTITION measurements_2028 FOR VALUES FROM ('2028-01-01') TO ('2029-01-01');
`},
			wantTables:  []string{"measurements", "measurements_2026", "measurements_2028"},
			wantParents: map[string]string{"measurements_2028": "public.measurements"},
		},
		{
			name: "dropping the parent drops its partitions at every level",
			migrations: []string{partitionedMeasurements, `
CREATE TABLE measurements_2027 PARTITION OF measurements FOR VALUES FROM ('2027-01-01') TO ('2028-01-01')
	PARTITION BY RANGE (id);
CREATE TABLE measurements_2027_low PARTITION OF measurements_2027 FOR VALUES FROM (0) TO (100);
DROP TABLE measurements;
CREATE TABLE measurements_2026 (note text);
CREATE TABLE measurements_2027_low (note text);
`},
			wantTables:  []string{"measurements_2026", "measurements_2027_low"},
			wantParents: map[string]string{"measurements_2026": "", "measurements_2027_low": ""},
		},
		{
			name: "a partition of a partition stays one when its parent is detached",
			migrations: []string{partitionedMeasurements, `
CREATE TABLE measurements_2027 PARTITION OF measurements FOR VALUES FROM ('2027-01-01') TO ('2028-01-01')
	PARTITION BY RANGE (id);
CREATE TABLE measurements_2027_low PARTITION OF measurements_2027 FOR VALUES FROM (0) TO (100);
ALTER TABLE measurements DETACH PARTITION measurements_2027;
`},
			wantTables: []string{"measurements", "measurements_2026", "measurements_2027", "measurements_2027_low"},
			wantParents: map[string]string{"measurements_2027": "", "measurements_2027_low": "public.measurements_2027",
				"measurements_2026": "public.measurements"},
		},
		{
			name: "a partition names its parent by the parent's name now",
			migrations: []string{partitionedMeasurements, `
ALTER TABLE measurements RENAME TO readings;
`},
			wantTables:  []string{"measurements_2026", "readings"},
			wantParents: map[string]string{"measurements_2026": "public.readings"},
		},
		{
			name: "a partition repeated IF NOT EXISTS is created once",
			migrations: []string{partitionedMeasurements, `
CREATE TABLE IF NOT EXISTS measurements_2026 PARTITION OF measurements DEFAULT;
`},
			wantTables:  []string{"measurements", "measurements_2026"},
			wantParents: map[string]string{"measurements_2026": "public.measurements"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, testCase.migrations...)

			require.Empty(t, diagnosticMessages(diagnostics))
			require.Equal(t, testCase.wantTables, schemaTableNames(catalogue, "public"))
			for name, wantParent := range testCase.wantParents {
				require.Equal(t, wantParent, partitionParent(catalogue, "", name), name)
			}
		})
	}
}

func TestPartitionReplayKeepsAPartitionsColumnsThoseOfItsParent(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t, partitionedMeasurements, `
ALTER TABLE measurements ADD COLUMN note text;
CREATE TABLE measurements_2027 PARTITION OF measurements (note WITH OPTIONS NOT NULL, CONSTRAINT positive CHECK (reading > 0))
	FOR VALUES FROM ('2027-01-01') TO ('2028-01-01') PARTITION BY RANGE (id);
CREATE TABLE measurements_2027_low PARTITION OF measurements_2027 FOR VALUES FROM (0) TO (100);
`, `
ALTER TABLE measurements RENAME COLUMN reading TO value;
ALTER TABLE measurements ALTER COLUMN value TYPE double precision;
ALTER TABLE measurements ALTER COLUMN value SET NOT NULL;
ALTER TABLE measurements ADD COLUMN added integer;
ALTER TABLE measurements DROP COLUMN added;
ALTER TABLE measurements_2026 ALTER COLUMN note SET NOT NULL;
`)

	require.Empty(t, diagnosticMessages(diagnostics))
	parent := catalogue.LookupTable("", "measurements")
	for _, name := range []string{"measurements_2026", "measurements_2027", "measurements_2027_low"} {
		partition := catalogue.LookupTable("", name)
		require.NotNil(t, partition, name)
		require.Equal(t, []string{"id", "taken_on", "value", "note"}, columnNamesOf(partition.Columns), name)
		require.Equal(t, parent.PrimaryKey, partition.PrimaryKey, name)
		require.Equal(t, "float8", partition.Columns[2].SQLType.EngineName, name)
		require.False(t, partition.Columns[2].Nullable, name)
		require.Equal(t, parent.Columns[0].HasDefault, partition.Columns[0].HasDefault, name)
	}
	require.True(t, parent.Columns[3].Nullable, "a partition's own NOT NULL leaves its parent's column alone")
	require.False(t, catalogue.LookupTable("", "measurements_2026").Columns[3].Nullable)
	require.False(t, catalogue.LookupTable("", "measurements_2027").Columns[3].Nullable)
	require.False(t, catalogue.LookupTable("", "measurements_2027_low").Columns[3].Nullable, "a partition takes the columns of its parent")
	require.Len(t, catalogue.LookupTable("", "measurements_2027").Constraints, 1)

	parent.Columns[0].Name = "changed"
	require.Equal(t, "id", catalogue.LookupTable("", "measurements_2026").Columns[0].Name)
}

func TestPartitionReplayGivesAParentsLaterPrimaryKeyToItsPartitions(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t, `
CREATE TABLE readings (id integer, kind integer) PARTITION BY LIST (kind);
CREATE TABLE readings_one PARTITION OF readings FOR VALUES IN (1) PARTITION BY RANGE (id);
CREATE TABLE readings_one_low PARTITION OF readings_one FOR VALUES FROM (0) TO (10);
CREATE TABLE readings_two PARTITION OF readings (PRIMARY KEY (id)) FOR VALUES IN (2);
`, "ALTER TABLE readings ADD PRIMARY KEY (id, kind);")

	require.Empty(t, diagnosticMessages(diagnostics))
	for _, name := range []string{"readings", "readings_one", "readings_one_low", "readings_two"} {
		table := catalogue.LookupTable("", name)
		require.Equal(t, []string{"id", "kind"}, table.PrimaryKey, name)
		require.False(t, table.Columns[0].Nullable, name)
	}
}

func TestPartitionReplayFollowsPartitionsAcrossSchemas(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t, partitionedMeasurements, `
CREATE SCHEMA archive;
ALTER TABLE measurements_2026 SET SCHEMA archive;
ALTER TABLE archive.measurements_2026 ALTER COLUMN reading SET NOT NULL;
CREATE TABLE archive.measurements_2027 PARTITION OF public.measurements FOR VALUES FROM ('2027-01-01') TO ('2028-01-01');
ALTER TABLE measurements DETACH PARTITION archive.measurements_2026;
CREATE SCHEMA staging;
CREATE TABLE staging.parent (id int) PARTITION BY RANGE (id);
CREATE TABLE public.staged PARTITION OF staging.parent FOR VALUES FROM (0) TO (10);
DROP SCHEMA staging CASCADE;
`)

	require.Empty(t, diagnosticMessages(diagnostics))
	require.Equal(t, []string{"measurements"}, schemaTableNames(catalogue, "public"))
	require.Equal(t, []string{"measurements_2026", "measurements_2027"}, schemaTableNames(catalogue, "archive"))
	require.Equal(t, "", partitionParent(catalogue, "archive", "measurements_2026"))
	require.Equal(t, "public.measurements", partitionParent(catalogue, "archive", "measurements_2027"))
	require.False(t, catalogue.LookupTable("archive", "measurements_2026").Columns[2].Nullable)
}

func TestPartitionReplayDetachesAPartitionAsATableWithItsColumns(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t, partitionedMeasurements, `
ALTER TABLE measurements ADD COLUMN note text;
ALTER TABLE measurements_2026 ALTER COLUMN note SET NOT NULL;
`, "ALTER TABLE measurements DETACH PARTITION measurements_2026 CONCURRENTLY;", "ALTER TABLE measurements ADD COLUMN later text;")

	require.Empty(t, diagnosticMessages(diagnostics))
	detached := catalogue.LookupTable("", "measurements_2026")
	require.NotNil(t, detached)
	require.Nil(t, detached.PartitionOf)
	require.Equal(t, []string{"id", "taken_on", "reading", "note"}, columnNamesOf(detached.Columns))
	require.False(t, detached.Columns[3].Nullable)
	require.Equal(t, []string{"id", "taken_on"}, detached.PrimaryKey)
}

func TestPartitionReplayReportsWhatTheDatabaseRefuses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		migration   string
		wantMessage string
		wantCode    string
	}{
		{
			name:        "a partition of a missing table",
			migration:   "CREATE TABLE orphan PARTITION OF missing FOR VALUES FROM (1) TO (2);",
			wantMessage: "table missing not found",
			wantCode:    diag.CodeUnknownObject,
		},
		{
			name:        "a partition under a name already taken",
			migration:   partitionedMeasurements + "CREATE TABLE measurements_2026 PARTITION OF measurements DEFAULT;",
			wantMessage: "measurements_2026 already exists",
			wantCode:    diag.CodeDuplicateObject,
		},
		{
			name:        "a table under a partition's name",
			migration:   partitionedMeasurements + "CREATE TABLE measurements_2026 (note text);",
			wantMessage: "measurements_2026 already exists",
			wantCode:    diag.CodeDuplicateObject,
		},
		{
			name:        "attaching a missing table",
			migration:   partitionedMeasurements + "ALTER TABLE measurements ATTACH PARTITION missing DEFAULT;",
			wantMessage: "table missing not found",
			wantCode:    diag.CodeUnknownObject,
		},
		{
			name:        "detaching a table that is not a partition",
			migration:   partitionedMeasurements + "CREATE TABLE loose (id int); ALTER TABLE measurements DETACH PARTITION loose;",
			wantMessage: "loose is not a partition of measurements",
			wantCode:    diag.CodeUnknownObject,
		},
		{
			name: "detaching a partition from another parent",
			migration: partitionedMeasurements + "CREATE TABLE other (id int) PARTITION BY RANGE (id); " +
				"ALTER TABLE other DETACH PARTITION measurements_2026;",
			wantMessage: "measurements_2026 is not a partition of other",
			wantCode:    diag.CodeUnknownObject,
		},
		{
			name:        "attaching a table to itself",
			migration:   partitionedMeasurements + "ALTER TABLE measurements ATTACH PARTITION measurements DEFAULT;",
			wantMessage: "measurements cannot be attached as a partition of itself",
			wantCode:    diag.CodeParseError,
		},
		{
			name: "attaching a partition a second time",
			migration: partitionedMeasurements + "CREATE TABLE other (id serial, taken_on date NOT NULL, reading numeric) " +
				"PARTITION BY RANGE (taken_on); ALTER TABLE other ATTACH PARTITION measurements_2026 DEFAULT;",
			wantMessage: "measurements_2026 is already a partition of measurements",
			wantCode:    diag.CodeParseError,
		},
		{
			name: "attaching a table under one of its own partitions",
			migration: partitionedMeasurements + "ALTER TABLE measurements_2026 ATTACH PARTITION measurements " +
				"FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');",
			wantMessage: "would make it a partition of itself",
			wantCode:    diag.CodeParseError,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, diagnostics := replayPostgresMigrations(t, testCase.migration)

			require.Len(t, diagnostics, 1)
			require.Contains(t, diagnostics[0].Message, testCase.wantMessage)
			require.Equal(t, testCase.wantCode, diagnostics[0].Code)
		})
	}
}

func TestAPartitionMutationNamingNoParentIsRefused(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		kind ast.MutationKind
	}{
		{name: "an attach", kind: ast.MutationAttachPartition},
		{name: "a detach", kind: ast.MutationDetachPartition},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := newCatalogueBuilder(postgres.New(), namespace.Default())
			require.Empty(t, applyMigration(t, builder, "001.sql", partitionedMeasurements, 0))

			var err error
			require.NotPanics(t, func() {
				err = builder.applyMutation(t.Context(), ast.NewCatalogueMutation(testCase.kind, "", "measurements_2026"))
			})

			require.ErrorIs(t, err, errMalformedMutation)
			require.Equal(t, diag.CodeParseError, mutationErrorCode(err))
		})
	}
}

func TestAPrimaryKeyReachesEveryLevelOfPartitionsOnce(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t, `
CREATE TABLE events (id int, at date NOT NULL) PARTITION BY RANGE (at);
CREATE TABLE events_2026 PARTITION OF events FOR VALUES FROM ('2026-01-01') TO ('2027-01-01') PARTITION BY RANGE (id);
CREATE TABLE events_2026_low PARTITION OF events_2026 FOR VALUES FROM (0) TO (100);
CREATE TABLE loose (id int, at date NOT NULL);
ALTER TABLE events ADD PRIMARY KEY (id, at);
`)

	require.Empty(t, diagnosticMessages(diagnostics))
	testCases := []struct {
		table  string
		wantPK []string
	}{
		{table: "events", wantPK: []string{"id", "at"}},
		{table: "events_2026", wantPK: []string{"id", "at"}},
		{table: "events_2026_low", wantPK: []string{"id", "at"}},
		{table: "loose", wantPK: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.table, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.wantPK, catalogue.LookupTable("", testCase.table).PrimaryKey)
		})
	}
}
