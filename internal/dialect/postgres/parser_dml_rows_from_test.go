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

package postgres

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestRowsFromRecordsEveryFunctionCall(t *testing.T) {
	t.Parallel()

	ordinality := &ast.ScopedColumn{Name: "ordinality", SQLType: New().NormaliseTypeName("bigint"), Nullable: false}
	names := func(values ...string) []ast.TVFColumnDefinition {
		definitions := make([]ast.TVFColumnDefinition, len(values))
		for index, value := range values {
			definitions[index] = ast.TVFColumnDefinition{Name: value, TypeName: ""}
		}
		return definitions
	}
	calls := func(values ...string) []ast.TableFunctionCall {
		functions := make([]ast.TableFunctionCall, len(values))
		for index, value := range values {
			functions[index] = ast.TableFunctionCall{FunctionName: value, ColumnDefinitions: nil}
		}
		return functions
	}
	record := []ast.TVFColumnDefinition{{Name: "a", TypeName: "int"}, {Name: "b", TypeName: "text"}}
	testCases := []struct {
		name string
		sql  string
		want ast.RawTableValuedFunctionReference
	}{
		{
			name: "two functions with an alias list",
			sql:  "SELECT n, s FROM ROWS FROM (generate_series(1, 3), unnest(ARRAY['a', 'b'])) AS x(n, s)",
			want: ast.RawTableValuedFunctionReference{
				FunctionName: "generate_series", Alias: "x", ColumnDefinitions: names("n", "s"),
				Functions: calls("generate_series", "unnest"), JoinKind: ast.JoinInner,
			},
		},
		{
			name: "two functions with ordinality and no alias",
			sql:  "SELECT ordinality FROM ROWS FROM (generate_series(1, 3), unnest(ARRAY['a'])) WITH ORDINALITY",
			want: ast.RawTableValuedFunctionReference{
				FunctionName: "generate_series", Alias: "generate_series", Ordinality: ordinality,
				Functions: calls("generate_series", "unnest"), JoinKind: ast.JoinInner,
			},
		},
		{
			name: "lateral functions of a preceding table",
			sql:  "SELECT p, q FROM items, LATERAL ROWS FROM (unnest(items.tags), unnest(items.scores)) WITH ORDINALITY x(p, q, o)",
			want: ast.RawTableValuedFunctionReference{
				FunctionName: "unnest", Alias: "x", ColumnDefinitions: names("p", "q", "o"), Ordinality: ordinality,
				Functions: calls("unnest", "unnest"), JoinKind: ast.JoinInner,
			},
		},
		{
			name: "a function with its own column definition list",
			sql:  "SELECT a FROM ROWS FROM (json_to_record('{}') AS (a int, b text), generate_series(1, 2)) AS r",
			want: ast.RawTableValuedFunctionReference{
				FunctionName: "json_to_record", Alias: "r",
				Functions: []ast.TableFunctionCall{
					{FunctionName: "json_to_record", ColumnDefinitions: record},
					{FunctionName: "generate_series", ColumnDefinitions: nil},
				},
				JoinKind: ast.JoinInner,
			},
		},
		{
			name: "an unnest of several arrays inside the list",
			sql:  "SELECT * FROM ROWS FROM (unnest(ARRAY[1], ARRAY['a']), generate_series(1, 2)) AS z",
			want: ast.RawTableValuedFunctionReference{
				FunctionName: "unnest", Alias: "z",
				Functions: calls("unnest", "unnest", "generate_series"), JoinKind: ast.JoinInner,
			},
		},
		{
			name: "a schema-qualified first function",
			sql:  "SELECT * FROM ROWS FROM (pg_catalog.generate_series(1, 2), generate_series(1, 3))",
			want: ast.RawTableValuedFunctionReference{
				FunctionName: "pg_catalog.generate_series", Alias: "generate_series",
				Functions: calls("pg_catalog.generate_series", "generate_series"), JoinKind: ast.JoinInner,
			},
		},
		{
			name: "a joined list of functions",
			sql:  "SELECT * FROM items LEFT JOIN ROWS FROM (generate_series(1, 2), generate_series(1, 3)) AS g(a, b) ON true",
			want: ast.RawTableValuedFunctionReference{
				FunctionName: "generate_series", Alias: "g", ColumnDefinitions: names("a", "b"),
				Functions: calls("generate_series", "generate_series"), JoinKind: ast.JoinLeft,
			},
		},
		{
			name: "one function reads as the function alone",
			sql:  "SELECT g FROM ROWS FROM (generate_series(1, 3)) AS g",
			want: ast.RawTableValuedFunctionReference{
				FunctionName: "generate_series", Alias: "g", ColumnDefinitions: names("g"), JoinKind: ast.JoinInner,
			},
		},
		{
			name: "one function with ordinality reads as the function alone",
			sql:  "SELECT g, ordinality FROM ROWS FROM (generate_series(1, 3)) WITH ORDINALITY AS g",
			want: ast.RawTableValuedFunctionReference{
				FunctionName: "generate_series", Alias: "g", ColumnDefinitions: names("g"), Ordinality: ordinality,
				JoinKind: ast.JoinInner,
			},
		},
		{
			name: "one function's column definition list is renamed by the alias list",
			sql:  "SELECT x, b FROM ROWS FROM (json_to_record('{}') AS (a int, b text)) AS r(x)",
			want: ast.RawTableValuedFunctionReference{
				FunctionName: "json_to_record", Alias: "r",
				ColumnDefinitions: []ast.TVFColumnDefinition{{Name: "x", TypeName: "int"}, {Name: "b", TypeName: "text"}},
				JoinKind:          ast.JoinInner,
			},
		},
		{
			name: "an unnest of several arrays reads as one call per array",
			sql:  "SELECT x, y FROM unnest(ARRAY[1], ARRAY['a']) AS u(x, y)",
			want: ast.RawTableValuedFunctionReference{
				FunctionName: "unnest", Alias: "u", ColumnDefinitions: names("x", "y"),
				Functions: calls("unnest", "unnest"), JoinKind: ast.JoinInner,
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)
			require.Len(t, analysis.RawTableValuedFunctions, 1)
			require.Equal(t, testCase.want, withoutArguments(analysis.RawTableValuedFunctions[0]))
		})
	}
}

func withoutArguments(reference ast.RawTableValuedFunctionReference) ast.RawTableValuedFunctionReference {
	reference.Arguments = nil
	reference.Functions = slices.Clone(reference.Functions)
	for index := range reference.Functions {
		reference.Functions[index].Arguments = nil
	}
	return reference
}

func TestTableFunctionCallsRecordTheirArguments(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		arguments []int
	}{
		{name: "one call", sql: "SELECT * FROM generate_series(1, 10, 2) AS g", arguments: []int{3}},
		{name: "several calls", sql: "SELECT * FROM ROWS FROM (generate_series(1, 3), unnest(ARRAY['a'])) AS x", arguments: []int{2, 1}},
		{name: "an unnest of several arrays, one array per call", sql: "SELECT * FROM unnest(ARRAY[1], ARRAY['a']) AS u", arguments: []int{1, 1}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)
			require.Len(t, analysis.RawTableValuedFunctions, 1)
			reference := analysis.RawTableValuedFunctions[0]
			var counts []int
			if len(reference.Functions) == 0 {
				counts = append(counts, len(reference.Arguments))
			}
			for _, call := range reference.Functions {
				counts = append(counts, len(call.Arguments))
			}
			require.Equal(t, testCase.arguments, counts)
		})
	}
}

func TestTableNamedRowsStillReadsAsATable(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, nil, "SELECT * FROM rows AS r, rows")
	require.Empty(t, analysis.RawTableValuedFunctions)
	require.Len(t, analysis.FromTables, 2)
	require.Equal(t, "rows", analysis.FromTables[0].Name)
	require.Equal(t, "rows", analysis.FromTables[1].Name)
}
