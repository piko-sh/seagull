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
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/emit/pgx"
)

const enumSchema = `CREATE TYPE mood AS ENUM ('calm', 'stormy');
CREATE TABLE items (
    id BIGINT PRIMARY KEY,
    mood mood,
    moods mood[] NOT NULL
);
`

func generatePgxFrom(t *testing.T, schema, queries string) map[string]string {
	t.Helper()
	generator, err := app.New(postgres.New(), app.WithEmitter(pgx.New()))
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), app.Input{
		Package:      "store",
		Migrations:   fstest.MapFS{"migrations/0001_init.up.sql": {Data: []byte(schema)}, "queries/items.sql": {Data: []byte(queries)}},
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)
	require.Empty(t, result.Diagnostics)
	generated := make(map[string]string, len(result.Files))
	for _, file := range result.Files {
		generated[file.Name] = string(file.Content)
	}
	return generated
}

func methodSource(t *testing.T, source, method string) string {
	t.Helper()
	start := strings.Index(source, ") "+method+"(")
	require.GreaterOrEqualf(t, start, 0, "method %s not found in:\n%s", method, source)
	end := strings.Index(source[start:], "\n}\n")
	require.GreaterOrEqual(t, end, 0)
	return source[start : start+end]
}

func TestPgxBindsASliceOfAnEnumAsGoStrings(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		queries string
		method  string
		wants   []string
	}{
		{
			name:    "a slice parameter in IN",
			queries: "-- sgl.query(ItemsWithMoods, many)\n-- $1 as sgl.param(moods, kind: slice)\nSELECT id FROM items WHERE mood IN ($1);\n",
			method:  "ItemsWithMoods",
			wants:   []string{"queries.db.Query(ctx, itemsWithMoodsSQL, sglConvertStrings[string](params.Moods))"},
		},
		{
			name:    "a slice parameter in NOT IN, sent empty for nil",
			queries: "-- sgl.query(ItemsWithoutMoods, many)\n-- $1 as sgl.param(moods, kind: slice)\nSELECT id FROM items WHERE mood NOT IN ($1);\n",
			method:  "ItemsWithoutMoods",
			wants: []string{
				"if params.Moods == nil {\n\t\tparams.Moods = []Mood{}\n\t}",
				"queries.db.Query(ctx, itemsWithoutMoodsSQL, sglConvertStrings[string](params.Moods))",
			},
		},
		{
			name:    "an array parameter, whose elements may be NULL",
			queries: "-- sgl.query(SetMoods, exec)\nUPDATE items SET moods = $1 WHERE id = $2;\n",
			method:  "SetMoods",
			wants:   []string{"queries.db.Exec(ctx, setMoodsSQL, sglConvertStringPointers[string](params.Moods), params.ID)"},
		},
		{
			name:    "an array parameter declaring its elements non-null",
			queries: "-- sgl.query(SetMoods, exec)\n-- $1 as sgl.param(moods, non_null_elements: true)\nUPDATE items SET moods = $1 WHERE id = $2;\n",
			method:  "SetMoods",
			wants:   []string{"queries.db.Exec(ctx, setMoodsSQL, sglConvertStrings[string](params.Moods), params.ID)"},
		},
		{
			name:    "an array parameter of a batch",
			queries: "-- sgl.query(InsertItems, batch)\nINSERT INTO items (id, moods) VALUES ($1, $2);\n",
			method:  "InsertItems",
			wants:   []string{"batch.Queue(insertItemsSQL, item.ID, sglConvertStringPointers[string](item.Moods))"},
		},
		{
			name:    "an enum parameter, which pgx binds as it is",
			queries: "-- sgl.query(SetMood, exec)\nUPDATE items SET mood = $1 WHERE id = $2;\n",
			method:  "SetMood",
			wants:   []string{"queries.db.Exec(ctx, setMoodSQL, params.Mood, params.ID)"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := generatePgxFrom(t, enumSchema, testCase.queries)
			method := methodSource(t, files["items.sql.go"], testCase.method)
			for _, want := range testCase.wants {
				require.Contains(t, method, want)
			}
		})
	}
}

func TestPgxScansAnArrayOfAnEnumThroughGoStrings(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name      string
		directive string
		converts  string
		helper    string
	}{
		{
			name:      "elements that may be NULL",
			directive: "",
			converts:  "row.Moods = sglConvertStringPointers[Mood](raw.Moods)",
			helper:    "func sglConvertStringPointers[",
		},
		{
			name:      "elements declared non-null",
			directive: "-- sgl.column(moods, non_null_elements: true)\n",
			converts:  "row.Moods = sglConvertStrings[Mood](raw.Moods)",
			helper:    "func sglConvertStrings[To, From ~string](values []From) []To {",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := generatePgxFrom(t, enumSchema, "-- sgl.query(GetItem, one)\n"+testCase.directive+"SELECT id, mood, moods FROM items WHERE id = $1;\n")
			method := methodSource(t, files["items.sql.go"], "GetItem")

			require.Contains(t, method, ".Scan(&row.ID, &row.Mood, &raw.Moods)")
			require.Contains(t, method, testCase.converts)
			require.Contains(t, files["convert_strings.go"], testCase.helper)
		})
	}
}

func TestPgxWritesNoConversionHelperWithoutAnArrayOfAnEnum(t *testing.T) {
	t.Parallel()

	files := generatePgxFrom(t, enumSchema, "-- sgl.query(GetMood, one)\nSELECT mood FROM items WHERE id = $1;\n")

	require.NotContains(t, files, "convert_strings.go")
	require.NotContains(t, files["items.sql.go"], "raw")
}
