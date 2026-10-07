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

package facade_test

import (
	"log/slog"
	"regexp"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
	"seagull.sh/seagull/sdk/emit/emit_driver_pgx"
)

var modelField = regexp.MustCompile(`(?m)^\s+(\w+)\s+(\S+)\s+` + "`json")

var driverTypesProject = fstest.MapFS{
	"db/migrations/0001_readings.up.sql": &fstest.MapFile{Data: []byte(
		"CREATE TABLE readings (id int PRIMARY KEY, at timetz NOT NULL, maybe_at timetz, flags bit(3) NOT NULL, " +
			"mask varbit, price money NOT NULL, refund money, span interval NOT NULL);\n")},
	"db/queries/readings.sql": &fstest.MapFile{Data: []byte(
		"-- sgl.query(GetReading, one)\nSELECT id, at, maybe_at, flags, mask, price, refund, span FROM readings WHERE id = $1;\n\n" +
			"-- sgl.query(AddReading, exec)\nINSERT INTO readings (id, at, maybe_at, flags, mask, price, refund, span) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);\n")},
}

var sqliteDeclaredTypesProject = fstest.MapFS{
	"db/migrations/0001_things.up.sql": &fstest.MapFile{Data: []byte(
		"CREATE TABLE things (id INTEGER PRIMARY KEY, external_id UUID NOT NULL, other_id uuid, starts_at TIME, " +
			"zoned TIMESTAMPTZ, span INTERVAL, raw BYTEA, price MONEY, label TEXT);\n")},
	"db/queries/things.sql": &fstest.MapFile{Data: []byte(
		"-- sgl.query(GetThing, one)\nSELECT id, external_id, other_id, starts_at, zoned, span, raw, price, label FROM things WHERE external_id = ?;\n\n" +
			"-- sgl.query(ThingText, one)\nSELECT lower(external_id) AS lowered, coalesce(starts_at, label) AS either, " +
			"CAST(label AS UUID) AS converted FROM things WHERE id = ?;\n")},
}

func generatedSQLiteFields(t *testing.T, overrides ...seagull.TypeOverride) (map[string]string, *seagull.Result) {
	t.Helper()

	generator, err := seagull.New(dialect_engine_sqlite.New(),
		seagull.WithLogger(slog.New(slog.DiscardHandler)),
		seagull.WithTypeOverrides(overrides...),
	)
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), seagull.Input{
		Package: "store", Migrations: sqliteDeclaredTypesProject, MigrationDir: "db/migrations", QueryDir: "db/queries",
	})
	require.NoError(t, err)
	require.NoError(t, result.Err())

	fields := map[string]string{}
	for _, match := range modelField.FindAllStringSubmatch(generatedSource(t, result), -1) {
		fields[match[1]] = match[2]
	}
	return fields, result
}

func generatedModelFields(t *testing.T, emitter emit.EmitterPort) (map[string]string, string) {
	t.Helper()

	chosen := seagull.DefaultPreset()
	chosen.Types.Interval = seagull.Slot{NotNull: seagull.GoType{Name: "string"}}
	generator, err := seagull.New(dialect_engine_postgres.New(),
		seagull.WithEmitter(emitter),
		seagull.WithPreset(chosen),
		seagull.WithLogger(slog.New(slog.DiscardHandler)),
	)
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), seagull.Input{
		Package: "store", Migrations: driverTypesProject, MigrationDir: "db/migrations", QueryDir: "db/queries",
	})
	require.NoError(t, err)
	require.NoError(t, result.Err())

	fields := map[string]string{}
	for _, file := range result.Files {
		if file.Name != "models.go" {
			continue
		}
		for _, match := range modelField.FindAllStringSubmatch(string(file.Content), -1) {
			fields[match[1]] = match[2]
		}
	}
	return fields, generatedSource(t, result)
}

func TestPostgresDriversReadTheSameGoTypes(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"ID": "int32", "At": "string", "MaybeAt": "*string", "Flags": "string", "Mask": "*string",
		"Price": "string", "Refund": "*string", "Span": "string",
	}
	sqlFields, _ := generatedModelFields(t, emit_driver_databasesql.New())
	pgxFields, pgxSource := generatedModelFields(t, emit_driver_pgx.New())
	require.Equal(t, want, sqlFields)
	require.Equal(t, want, pgxFields)
	for _, column := range []string{"flags", "mask", "span"} {
		require.Contains(t, pgxSource, `CAST(`+column+` AS text) AS "`+column+`"`)
	}
	require.NotContains(t, pgxSource, "CAST(at AS text)")
	require.NotContains(t, pgxSource, "CAST(price AS text)")
}

func TestSQLiteColumnsReadAsTheDriversReturnTheirDeclaredTypes(t *testing.T) {
	t.Parallel()

	fields, result := generatedSQLiteFields(t)
	require.Empty(t, result.Diagnostics)
	require.Equal(t, map[string]string{
		"ID": "int64", "ExternalID": "uuid.UUID", "OtherID": "*uuid.UUID", "StartsAt": "*string", "Zoned": "*string",
		"Span": "*string", "Raw": "[]byte", "Price": "*apd.Decimal", "Label": "*string",
		"Lowered": "string", "Either": "*string", "Converted": "*apd.Decimal",
	}, fields)
}

func TestASQLiteTypeOverrideOfUUIDAppliesToUUIDColumns(t *testing.T) {
	t.Parallel()

	fields, result := generatedSQLiteFields(t, seagull.TypeOverride{SQLTypeName: "uuid", GoName: "string"})
	require.Empty(t, result.Diagnostics)
	require.Equal(t, "string", fields["ExternalID"])
	require.Equal(t, "*string", fields["OtherID"])
	require.Equal(t, "*apd.Decimal", fields["Converted"])
}
