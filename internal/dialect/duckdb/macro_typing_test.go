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

package duckdb

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func macroCatalogue(t *testing.T, migration string) *sqlmodel.Catalogue {
	t.Helper()

	schema := &sqlmodel.Schema{Name: "main", Functions: map[string][]*sqlmodel.Function{}}
	engine := New()
	statements, err := engine.ParseStatements(t.Context(), migration)
	require.NoError(t, err)
	for _, statement := range statements {
		mutation, applyErr := engine.ApplyDDL(t.Context(), statement)
		require.NoError(t, applyErr)
		function := mutation.Function.Function
		if function.MinArguments == 0 {
			function.MinArguments = len(function.Arguments)
		}
		if function.DataAccess == sqlmodel.DataAccessUnknown {
			function.DataAccess = sqlmodel.DataAccessReadOnly
		}
		schema.Functions[function.Name] = append(schema.Functions[function.Name], function)
	}
	return &sqlmodel.Catalogue{DefaultSchema: "main", Schemas: map[string]*sqlmodel.Schema{"main": schema}}
}

func TestResolveFunctionCallTypesAMacroFromItsArguments(t *testing.T) {
	t.Parallel()

	catalogue := macroCatalogue(t, `
		CREATE MACRO add_one(x) AS x + 1;
		CREATE MACRO plus(a, b) AS a + b;
		CREATE MACRO greet(n) AS 'hi ' || n;
		CREATE MACRO shout(s) AS upper(s);
		CREATE MACRO chained(x) AS add_one(add_one(x));
		CREATE MACRO fallback(x) AS coalesce(x, 0);
		CREATE MACRO larger(a, b) AS a > b;
		CREATE MACRO unrelated(x) AS other_table.value;
		CREATE MACRO recursive(x) AS recursive(x);`)
	integer := sqlmodel.NewIntegerType("int4", 32, false)
	bigint := sqlmodel.NewIntegerType("int8", 64, false)
	decimal := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric")
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")

	testCases := []struct {
		name      string
		macro     string
		arguments []sqlmodel.SQLType
		want      sqlmodel.SQLTypeCategory
		strict    bool
		untyped   bool
	}{
		{name: "an integer argument keeps the arithmetic integer", macro: "add_one", arguments: []sqlmodel.SQLType{integer},
			want: sqlmodel.TypeCategoryInteger, strict: true},
		{name: "a decimal argument makes it decimal", macro: "add_one", arguments: []sqlmodel.SQLType{decimal},
			want: sqlmodel.TypeCategoryDecimal, strict: true},
		{name: "the wider operand wins", macro: "plus", arguments: []sqlmodel.SQLType{integer, decimal},
			want: sqlmodel.TypeCategoryDecimal, strict: true},
		{name: "concatenation is text", macro: "greet", arguments: []sqlmodel.SQLType{text}, want: sqlmodel.TypeCategoryText,
			strict: true},
		{name: "a built-in call is typed by its overload", macro: "shout", arguments: []sqlmodel.SQLType{text},
			want: sqlmodel.TypeCategoryText, strict: true},
		{name: "a macro calling a macro", macro: "chained", arguments: []sqlmodel.SQLType{bigint}, want: sqlmodel.TypeCategoryInteger,
			strict: true},
		{name: "coalesce is never strict", macro: "fallback", arguments: []sqlmodel.SQLType{integer},
			want: sqlmodel.TypeCategoryInteger, strict: false},
		{name: "a comparison is boolean", macro: "larger", arguments: []sqlmodel.SQLType{integer, integer},
			want: sqlmodel.TypeCategoryBoolean, strict: true},
		{name: "a name that is no parameter stays untyped", macro: "unrelated", arguments: []sqlmodel.SQLType{integer},
			untyped: true},
		{name: "a macro calling itself stays untyped", macro: "recursive", arguments: []sqlmodel.SQLType{integer},
			untyped: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := New().ResolveFunctionCall(t.Context(), catalogue, testCase.macro, "", testCase.arguments)

			require.NoError(t, err)
			if testCase.untyped {
				require.Nil(t, resolution)
				return
			}
			require.NotNil(t, resolution)
			require.Equal(t, testCase.want, resolution.ReturnType.Category)
			wantNullable := sqlmodel.FunctionNullableCalledOnNull
			if testCase.strict {
				wantNullable = sqlmodel.FunctionNullableReturnsNullOnNull
			}
			require.Equal(t, wantNullable, resolution.NullableBehaviour)
			require.Equal(t, sqlmodel.DataAccessReadOnly, resolution.DataAccess)
		})
	}
}

func TestResolveFunctionCallFindsAMacroByItsNameInAnyCase(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE MACRO AddOne(x) AS x + 1;")
	require.NoError(t, err)
	require.Len(t, statements, 1)
	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	macro := mutation.Function.Function
	macro.MinArguments = len(macro.Arguments)
	macro.DataAccess = sqlmodel.DataAccessReadOnly
	identifiers := engine.Info().Identifiers
	catalogue := &sqlmodel.Catalogue{
		DefaultSchema: "main",
		Identifiers:   identifiers,
		Schemas: map[string]*sqlmodel.Schema{"main": {Name: "main", Functions: map[string][]*sqlmodel.Function{
			identifiers.Key(macro.Name): {macro},
		}}},
	}

	testCases := []struct {
		name   string
		called string
	}{
		{name: "the name as created", called: "AddOne"},
		{name: "the name in lower case", called: "addone"},
		{name: "the name in upper case", called: "ADDONE"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := engine.ResolveFunctionCall(t.Context(), catalogue, testCase.called, "",
				[]sqlmodel.SQLType{sqlmodel.NewIntegerType("int4", 32, false)})

			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, sqlmodel.TypeCategoryInteger, resolution.ReturnType.Category)
		})
	}
}

func TestResolveFunctionCallFindsAMacroAlongTheSearchPath(t *testing.T) {
	t.Parallel()

	addOne := macroCatalogue(t, "CREATE MACRO add_one(x) AS x + 1;").Schemas["main"].Functions["add_one"]
	plus := macroCatalogue(t, "CREATE MACRO add_one(a, b) AS a + b;").Schemas["main"].Functions["add_one"]
	identifiers := New().Info().Identifiers
	catalogueOf := func(searchPath []string, schemas map[string][]*sqlmodel.Function) *sqlmodel.Catalogue {
		catalogue := &sqlmodel.Catalogue{
			DefaultSchema: "main",
			SearchPath:    searchPath,
			Identifiers:   identifiers,
			Schemas:       map[string]*sqlmodel.Schema{"main": {Name: "main", Functions: map[string][]*sqlmodel.Function{}}},
		}
		for name, functions := range schemas {
			catalogue.Schemas[name] = &sqlmodel.Schema{Name: name, Functions: map[string][]*sqlmodel.Function{"add_one": functions}}
		}
		return catalogue
	}

	testCases := []struct {
		catalogue *sqlmodel.Catalogue
		name      string
		schema    string
		wantTyped bool
	}{
		{
			name:      "an unqualified call reaches a schema later on the search path",
			catalogue: catalogueOf([]string{"main", "extra"}, map[string][]*sqlmodel.Function{"extra": addOne}),
			wantTyped: true,
		},
		{
			name:      "an unqualified call does not reach a schema off the search path",
			catalogue: catalogueOf([]string{"main"}, map[string][]*sqlmodel.Function{"extra": addOne}),
			wantTyped: false,
		},
		{
			name:      "an earlier schema's macro of another arity is passed over",
			catalogue: catalogueOf([]string{"first", "extra"}, map[string][]*sqlmodel.Function{"first": plus, "extra": addOne}),
			wantTyped: true,
		},
		{
			name:      "a qualified call finds its schema in any case",
			catalogue: catalogueOf(nil, map[string][]*sqlmodel.Function{"extra": addOne}),
			schema:    "EXTRA",
			wantTyped: true,
		},
		{
			name:      "a qualified call looks in its schema alone",
			catalogue: catalogueOf([]string{"extra"}, map[string][]*sqlmodel.Function{"extra": addOne}),
			schema:    "main",
			wantTyped: false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := New().ResolveFunctionCall(t.Context(), testCase.catalogue, "add_one", testCase.schema,
				[]sqlmodel.SQLType{sqlmodel.NewIntegerType("int4", 32, false)})

			require.NoError(t, err)
			if !testCase.wantTyped {
				require.Nil(t, resolution)
				return
			}
			require.NotNil(t, resolution)
			require.Equal(t, sqlmodel.TypeCategoryInteger, resolution.ReturnType.Category)
		})
	}
}

func TestResolveFunctionCallTypesAMacroThatBuildsAStruct(t *testing.T) {
	t.Parallel()

	catalogue := macroCatalogue(t, `
		CREATE MACRO literal(x) AS {'value': x, 'label': 'fixed'};
		CREATE MACRO packed(x) AS struct_pack(value := x);
		CREATE MACRO extended(x) AS struct_insert({'value': x}, label := 'fixed');`)
	integer := sqlmodel.NewIntegerType("int4", 32, false)

	testCases := []struct {
		name       string
		macro      string
		wantFields []string
	}{
		{name: "a struct literal", macro: "literal", wantFields: []string{"value", "label"}},
		{name: "struct_pack", macro: "packed", wantFields: []string{"value"}},
		{name: "struct_insert", macro: "extended", wantFields: []string{"value", "label"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := New().ResolveFunctionCall(t.Context(), catalogue, testCase.macro, "", []sqlmodel.SQLType{integer})

			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, sqlmodel.TypeCategoryStruct, resolution.ReturnType.Category)
			require.Equal(t, sqlmodel.FunctionNullableCalledOnNull, resolution.NullableBehaviour)
			names := make([]string, 0, len(resolution.ReturnType.StructFields))
			for _, field := range resolution.ReturnType.StructFields {
				require.True(t, field.SQLType.Nullable)
				names = append(names, field.Name)
			}
			require.Equal(t, testCase.wantFields, names)
			require.Equal(t, sqlmodel.TypeCategoryInteger, resolution.ReturnType.StructFields[0].SQLType.Category)
		})
	}
}

func TestResolveFunctionCallLeavesAMacroOfOtherArityAlone(t *testing.T) {
	t.Parallel()

	catalogue := macroCatalogue(t, "CREATE MACRO add_one(x) AS x + 1;")

	resolution, err := New().ResolveFunctionCall(t.Context(), catalogue, "add_one", "", nil)

	require.NoError(t, err)
	require.Nil(t, resolution)
}

func repeatedCalls(callee string, count int) string {
	calls := make([]string, count)
	for index := range calls {
		calls[index] = callee + "(x)"
	}
	return strings.Join(calls, " + ")
}

func fanOutMacros(levels, callsPerBody int) string {
	var migration strings.Builder
	for level := 1; level < levels; level++ {
		fmt.Fprintf(&migration, "CREATE MACRO m%d(x) AS %s;\n", level, repeatedCalls(fmt.Sprintf("m%d", level+1), callsPerBody))
	}
	fmt.Fprintf(&migration, "CREATE MACRO m%d(x) AS x + 1;\n", levels)
	return migration.String()
}

func TestResolveFunctionCallTypesMacrosThatCallOthersManyTimesQuickly(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", 32, false)
	testCases := []struct {
		name      string
		migration string
		macro     string
		want      sqlmodel.SQLTypeCategory
		untyped   bool
	}{
		{name: "a macro calling itself eight times", migration: "CREATE MACRO m(x) AS " + repeatedCalls("m", 8) + ";",
			macro: "m", untyped: true},
		{name: "macros calling each other in a cycle", migration: "CREATE MACRO a(x) AS " + repeatedCalls("b", 8) +
			";\nCREATE MACRO b(x) AS " + repeatedCalls("a", 8) + ";", macro: "a", untyped: true},
		{name: "eight levels of macros each calling the next eight times", migration: fanOutMacros(8, 8), macro: "m1",
			want: sqlmodel.TypeCategoryInteger},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := macroCatalogue(t, testCase.migration)
			engine := New()

			scaling.Within(t, 100*time.Millisecond, func(ctx context.Context) {
				resolution, err := engine.ResolveFunctionCall(ctx, catalogue, testCase.macro, "", []sqlmodel.SQLType{integer})

				require.NoError(t, err)
				if testCase.untyped {
					require.Nil(t, resolution)
					return
				}
				require.NotNil(t, resolution)
				require.Equal(t, testCase.want, resolution.ReturnType.Category)
			})
		})
	}
}

func TestResolveFunctionCallLeavesAMacroUntypedOnceItsCallBudgetIsSpent(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", 32, false)
	testCases := []struct {
		name    string
		callees int
		untyped bool
	}{
		{name: "a body calling fewer macros than the budget", callees: 10, untyped: false},
		{name: "a body calling more macros than the budget", callees: maxMacroTypingCalls + 1, untyped: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var migration strings.Builder
			calls := make([]string, testCase.callees)
			for index := range calls {
				fmt.Fprintf(&migration, "CREATE MACRO callee%d(x) AS x + 1;\n", index)
				calls[index] = fmt.Sprintf("callee%d(x)", index)
			}
			fmt.Fprintf(&migration, "CREATE MACRO caller(x) AS %s;\n", strings.Join(calls, " + "))
			catalogue := macroCatalogue(t, migration.String())

			resolution, err := New().ResolveFunctionCall(t.Context(), catalogue, "caller", "", []sqlmodel.SQLType{integer})

			require.NoError(t, err)
			if testCase.untyped {
				require.Nil(t, resolution)
				return
			}
			require.NotNil(t, resolution)
			require.Equal(t, sqlmodel.TypeCategoryInteger, resolution.ReturnType.Category)
		})
	}
}
