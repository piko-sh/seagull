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

package pgx_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/pgx"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

const enumShapesSchema = `CREATE TYPE mood AS ENUM ('calm', 'stormy');
CREATE TABLE items (
    id BIGINT PRIMARY KEY,
    mood mood,
    moods mood[],
    grid mood[][]
);
`

const extensionSchema = `CREATE EXTENSION IF NOT EXISTS ltree;
CREATE TABLE organisations (id BIGINT PRIMARY KEY, path LTREE NOT NULL, paths LTREE[], label TEXT);
`

const compositeSchema = `CREATE TYPE pair AS (a INT, b TEXT);
CREATE TABLE solutions (id BIGINT PRIMARY KEY, bar pair, pairs pair[]);
`

func checkPgx(t *testing.T, preset app.Preset, queries string) (map[string]string, []diag.SourceError) {
	t.Helper()
	return checkPgxSchema(t, preset, enumShapesSchema, queries)
}

func checkPgxSchema(t *testing.T, preset app.Preset, schema, queries string) (map[string]string, []diag.SourceError) {
	t.Helper()
	generator, err := app.New(postgres.New(), app.WithEmitter(pgx.New()), app.WithPreset(preset))
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), app.Input{
		Package:      "store",
		Migrations:   fstest.MapFS{"migrations/0001_init.up.sql": {Data: []byte(schema)}, "queries/items.sql": {Data: []byte(queries)}},
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)
	generated := make(map[string]string, len(result.Files))
	for _, file := range result.Files {
		generated[file.Name] = string(file.Content)
	}
	return generated, result.Diagnostics
}

func stringEnums() app.Preset {
	preset := app.DefaultPreset()
	text := sqlmodel.GoType{Composite: nil, Package: "", Name: "string"}
	preset.Types.Enum = sqlmodel.TypeSlot{NotNull: text, Nullable: sqlmodel.GoType{Composite: nil, Package: "", Name: "*string"}}
	return preset
}

func TestCheckRefusesACopyIntoAnArrayOfAnEnum(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		preset app.Preset
		insert string
		column string
	}{
		{name: "an enum array as the generated enum type", preset: app.DefaultPreset(), insert: "INSERT INTO items (id, moods) VALUES ($1, $2);", column: "moods"},
		{name: "an enum array as Go strings", preset: stringEnums(), insert: "INSERT INTO items (id, moods) VALUES ($1, $2);", column: "moods"},
		{name: "a multidimensional enum array", preset: app.DefaultPreset(), insert: "INSERT INTO items (grid, id) VALUES ($1, $2);", column: "grid"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, diagnostics := checkPgx(t, testCase.preset, "-- sgl.query(CopyItems, copyfrom)\n"+testCase.insert+"\n")

			require.Empty(t, files)
			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeEmitterUnsupported, diagnostics[0].Code)
			require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			require.Equal(t, "items.sql", diagnostics[0].Filename)
			require.Equal(t, "command: copyfrom cannot send column "+testCase.column+", an array of an enum: pgx's CopyFrom sends every "+
				"value in binary, which pgx writes for an enum array only on a connection that has loaded the enum's array type "+
				"(LoadType and RegisterType for the enum and its array type), and the generated code cannot rely on that; "+
				"use command: batch, which binds the array as text the server parses, or insert the column with another query",
				diagnostics[0].Message)
		})
	}
}

func TestCheckRefusesACopyWhoseStatementWritesAClauseAfterItsValues(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		insert string
	}{
		{name: "an upsert that updates", insert: "INSERT INTO items (id, mood) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET mood = EXCLUDED.mood;"},
		{name: "an upsert that does nothing", insert: "INSERT INTO items (id, mood) VALUES ($1, $2) ON CONFLICT DO NOTHING;"},
		{name: "a returning clause", insert: "INSERT INTO items (id, mood) VALUES ($1, $2) RETURNING id;"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, diagnostics := checkPgx(t, app.DefaultPreset(), "-- sgl.query(CopyItems, copyfrom)\n"+testCase.insert+"\n")

			require.Empty(t, files)
			var refusals []diag.SourceError
			for _, diagnostic := range diagnostics {
				if diagnostic.Severity == diag.SeverityError {
					refusals = append(refusals, diagnostic)
				}
			}
			require.Len(t, refusals, 1)
			require.Equal(t, diag.CodeBatchShape, refusals[0].Code)
			require.Equal(t, "command: copyfrom sends the rows through the copy protocol, which applies nothing the statement "+
				"writes after its VALUES row, such as ON CONFLICT or RETURNING, so the clause would be dropped; use "+
				"command: batch, which runs the statement for each row, or leave the clause out", refusals[0].Message)
		})
	}
}

func TestCheckLetsACopyEndWithACommentAndTheTerminator(t *testing.T) {
	t.Parallel()

	files, diagnostics := checkPgx(t, app.DefaultPreset(), "-- sgl.query(CopyItems, copyfrom)\nINSERT INTO items (id, mood) VALUES ($1, $2) /* rows */ ;\n")

	require.Empty(t, diagnostics)
	require.Contains(t, files["items.sql.go"], "CopyFrom(")
}

func TestCheckRefusesACopyIntoATypePgxCannotEncode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		insert string
		column string
	}{
		{name: "an extension's type", insert: "INSERT INTO organisations (id, path) VALUES ($1, $2);", column: "path"},
		{name: "an array of an extension's type", insert: "INSERT INTO organisations (paths, id) VALUES ($1, $2);", column: "paths"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, diagnostics := checkPgxSchema(t, app.DefaultPreset(), extensionSchema, "-- sgl.query(CopyOrganisations, copyfrom)\n"+testCase.insert+"\n")

			require.Empty(t, files)
			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeEmitterUnsupported, diagnostics[0].Code)
			require.Equal(t, "command: copyfrom cannot send column "+testCase.column+", whose type pgx has no binary encoding for "+
				"unless the connection registers it (an extension's type such as ltree, or a composite type): pgx's CopyFrom "+
				"sends every value in binary, so the value would reach the server as text it rejects; use command: batch, "+
				"which binds the value as text the server parses, or insert the column with another query", diagnostics[0].Message)
		})
	}
}

func TestCheckLeavesACopyToTheCallerWhereTheValueMayEncode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		queries   string
		overrides []app.TypeOverride
	}{
		{
			name: "a table the analysis does not know",
			queries: "-- sgl.query(CreateScratch, exec)\nCREATE TEMPORARY TABLE scratch (id bigint, label text) ON COMMIT DROP;\n" +
				"-- sgl.query(CopyScratch, copyfrom)\nINSERT INTO scratch (id, label) VALUES ($1, $2);\n",
		},
		{
			name:      "an extension's type whose Go type an override chose",
			queries:   "-- sgl.query(CopyOrganisations, copyfrom)\nINSERT INTO organisations (id, path) VALUES ($1, $2);\n",
			overrides: []app.TypeOverride{{Column: "organisations.path", GoPackage: "example.com/paths", GoName: "Path"}},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			generator, err := app.New(postgres.New(), app.WithEmitter(pgx.New()), app.WithTypeOverrides(testCase.overrides...))
			require.NoError(t, err)
			result, err := generator.Generate(t.Context(), app.Input{
				Package:      "store",
				Migrations:   fstest.MapFS{"migrations/0001_init.up.sql": {Data: []byte(extensionSchema)}, "queries/items.sql": {Data: []byte(testCase.queries)}},
				MigrationDir: "migrations",
				QueryDir:     "queries",
			})
			require.NoError(t, err)

			for _, diagnostic := range result.Diagnostics {
				require.NotEqual(t, diag.CodeEmitterUnsupported, diagnostic.Code, diagnostic.Message)
			}
		})
	}
}

func TestCheckLetsACopySendAKnownTypeBesideAnExtensionsType(t *testing.T) {
	t.Parallel()

	files, diagnostics := checkPgxSchema(t, app.DefaultPreset(), extensionSchema,
		"-- sgl.query(CopyLabels, copyfrom)\nINSERT INTO organisations (id, label) VALUES ($1, $2);\n")

	require.Empty(t, diagnostics)
	require.Contains(t, files["items.sql.go"], "CopyFrom(")
}

func TestCheckLetsACopySendAnEnumValue(t *testing.T) {
	t.Parallel()

	files, diagnostics := checkPgx(t, app.DefaultPreset(), "-- sgl.query(CopyItems, copyfrom)\nINSERT INTO items (id, mood) VALUES ($1, $2);\n")

	require.Empty(t, diagnostics)
	require.Contains(t, files["items.sql.go"], "args[0] = params[i].ID\n\t\targs[1] = params[i].Mood\n\t\treturn args, nil")
}

func TestCheckRefusesAMultidimensionalEnumArrayPgxCannotReadOrBind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		queries string
		want    string
	}{
		{
			name:    "a column read",
			queries: "-- sgl.query(GetGrid, one)\nSELECT grid FROM items WHERE id = $1;\n",
			want:    "column grid holds the generated enum type Mood nested in a slice or a struct, which pgx cannot read",
		},
		{
			name:    "a parameter bound",
			queries: "-- sgl.query(SetGrid, exec)\nUPDATE items SET grid = $1 WHERE id = $2;\n",
			want:    "parameter grid holds the generated enum type Mood nested in a slice or a struct, which pgx cannot bind",
		},
		{
			name:    "a parameter queued in a batch",
			queries: "-- sgl.query(InsertGrids, batch)\nINSERT INTO items (id, grid) VALUES ($1, $2);\n",
			want:    "parameter grid holds the generated enum type Mood nested in a slice or a struct, which pgx cannot bind",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, diagnostics := checkPgx(t, app.DefaultPreset(), testCase.queries)

			require.Empty(t, files)
			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeEmitterUnsupported, diagnostics[0].Code)
			require.Contains(t, diagnostics[0].Message, testCase.want)
		})
	}
}

func TestCheckRefusesAnArrayParameterOfACompositeType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		command   string
		query     string
		parameter string
		typeName  string
	}{
		{name: "an array of a composite type", command: "many", query: "SELECT id FROM solutions WHERE bar = ANY($1::pair[]);",
			parameter: "bar", typeName: "pair"},
		{name: "an array of a table's row type", command: "one", query: "SELECT count(*) AS total FROM unnest($1::solutions[]);",
			parameter: "p1", typeName: "solutions"},
		{name: "an array column's value", command: "exec", query: "UPDATE solutions SET pairs = $1 WHERE id = $2;",
			parameter: "pairs", typeName: "pair"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, diagnostics := checkPgxSchema(t, app.DefaultPreset(), compositeSchema,
				"-- sgl.query(Pairs, "+testCase.command+")\n"+testCase.query+"\n")

			require.Empty(t, files)
			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeEmitterUnsupported, diagnostics[0].Code)
			require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			require.Equal(t, "parameter "+testCase.parameter+" is an array of the composite type "+testCase.typeName+", which pgx "+
				"encodes only on a connection that has loaded and registered the type (LoadType and RegisterType), and the "+
				"generated code cannot rely on that; pass each field as an array of its own and build the rows with "+
				"unnest(...), or override the parameter's Go type with a type whose value pgx can encode", diagnostics[0].Message)
		})
	}
}

func TestCheckAcceptsAParameterOfACompositeTypeThatIsNoArray(t *testing.T) {
	t.Parallel()

	files, diagnostics := checkPgxSchema(t, app.DefaultPreset(), compositeSchema,
		"-- sgl.query(Pair, many)\nSELECT id FROM solutions WHERE bar = $1::pair;\n")

	require.NotEmpty(t, files)
	require.Empty(t, diagnostics)
}

func TestCheckRefusesAValueWhoseTypeNestsDeeperThanGeneratedCodeFollows(t *testing.T) {
	t.Parallel()

	nested := func(levels int) sqlmodel.SQLType {
		sqlType := sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger}
		for range levels {
			sqlType = sqlmodel.SQLType{EngineName: "int8[]", Category: sqlmodel.TypeCategoryArray, ElementType: new(sqlType)}
		}
		return sqlType
	}

	testCases := []struct {
		name    string
		sqlType sqlmodel.SQLType
		want    []emit.Problem
	}{
		{name: "a type as deep as generated code follows", sqlType: nested(sqlmodel.MaxTypeDepth - 1)},
		{name: "a type deeper than generated code follows", sqlType: nested(sqlmodel.MaxTypeDepth), want: []emit.Problem{{
			Code: diag.CodeEmitterUnsupported,
			Message: "parameter grid has a type nesting 64 or more levels of elements, keys, fields or members, deeper than " +
				"generated code follows; flatten the type",
			Severity: diag.SeverityError,
		}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{
				Name: "CountGrid", Command: sqlmodel.QueryCommandExec, Filename: "items.sql",
				Parameters: []sqlmodel.QueryParameter{{Number: 1, Name: "grid", SQLType: testCase.sqlType}},
			}
			problems := pgx.New().Check(&emit.Request{
				Types: &gotypes.MappingTable{}, Package: "store", Dialect: dialect.Wrap(postgres.New()).Info(), Queries: []*sqlmodel.AnalysedQuery{query},
			})

			for index := range testCase.want {
				testCase.want[index].Query = query
			}
			require.Equal(t, testCase.want, problems)
		})
	}
}
