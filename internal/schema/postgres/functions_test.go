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
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	replay "seagull.sh/seagull/internal/catalogue"
	pgdialect "seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	functionParameterColumns = []string{
		"nspname", "oid", "proname", "prokind", "proisstrict", "proretset", "provariadic",
		"pronargdefaults", "return_type", "mode", "name", "type_name",
	}
)

func parameter(mode string, name string, typeName string) functionParameter {
	return functionParameter{mode: mode, name: name, typeName: typeName}
}

func typeNames(arguments []sqlmodel.FunctionArgument) []string {
	names := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		names = append(names, argument.Type.EngineName)
	}
	return names
}

func argumentNames(arguments []sqlmodel.FunctionArgument) []string {
	names := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		names = append(names, argument.Name)
	}
	return names
}

func optionalFlags(arguments []sqlmodel.FunctionArgument) []bool {
	flags := make([]bool, 0, len(arguments))
	for _, argument := range arguments {
		flags = append(flags, argument.IsOptional)
	}
	return flags
}

func TestBuildFunctionSignature(t *testing.T) {
	testCases := []struct {
		name              string
		wantReturnType    string
		wantArgumentNames []string
		wantArgumentTypes []string
		wantOptional      []bool
		function          introspectedFunction
		wantMinArguments  int
		wantNullable      sqlmodel.FunctionNullableBehaviour
		wantStrict        bool
		wantVariadic      bool
		wantAggregate     bool
		wantReturnsSet    bool
	}{
		{
			name: "trailing default makes the last argument optional",
			function: introspectedFunction{
				name: "add", procKind: "f", returnType: "integer", defaultCount: 1, isStrict: true,
				parameters: []functionParameter{
					parameter("i", "a", "integer"),
					parameter("i", "b", "integer"),
				},
			},
			wantReturnType:    "integer",
			wantArgumentNames: []string{"a", "b"},
			wantArgumentTypes: []string{"integer", "integer"},
			wantOptional:      []bool{false, true},
			wantMinArguments:  1,
			wantNullable:      sqlmodel.FunctionNullableReturnsNullOnNull,
			wantStrict:        true,
		},
		{
			name: "OUT parameters describe the result rather than the call",
			function: introspectedFunction{
				name: "split_name", procKind: "f", returnType: "record",
				parameters: []functionParameter{
					parameter("i", "full_name", "text"),
					parameter("o", "first_name", "text"),
					parameter("o", "last_name", "text"),
				},
			},
			wantReturnType:    "record",
			wantArgumentNames: []string{"full_name"},
			wantArgumentTypes: []string{"text"},
			wantOptional:      []bool{false},
			wantMinArguments:  1,
			wantNullable:      sqlmodel.FunctionNullableCalledOnNull,
		},
		{
			name: "RETURNS TABLE columns are not arguments",
			function: introspectedFunction{
				name: "users_since", procKind: "f", returnType: "record", returnsSet: true,
				parameters: []functionParameter{
					parameter("i", "since", "timestamp with time zone"),
					parameter("t", "id", "integer"),
					parameter("t", "email", "character varying"),
				},
			},
			wantReturnType:    "record",
			wantArgumentNames: []string{"since"},
			wantArgumentTypes: []string{"timestamp with time zone"},
			wantOptional:      []bool{false},
			wantMinArguments:  1,
			wantNullable:      sqlmodel.FunctionNullableCalledOnNull,
			wantReturnsSet:    true,
		},
		{
			name: "INOUT parameters are arguments",
			function: introspectedFunction{
				name: "swap", procKind: "f", returnType: "record",
				parameters: []functionParameter{
					parameter("b", "a", "integer"),
					parameter("b", "b", "integer"),
				},
			},
			wantReturnType:    "record",
			wantArgumentNames: []string{"a", "b"},
			wantArgumentTypes: []string{"integer", "integer"},
			wantOptional:      []bool{false, false},
			wantMinArguments:  2,
			wantNullable:      sqlmodel.FunctionNullableCalledOnNull,
		},
		{
			name: "VARIADIC parameter without a default needs one value",
			function: introspectedFunction{
				name: "total", procKind: "f", returnType: "numeric", isVariadic: true,
				parameters: []functionParameter{parameter("v", "amounts", "numeric[]")},
			},
			wantReturnType:    "numeric",
			wantArgumentNames: []string{"amounts"},
			wantArgumentTypes: []string{"numeric[]"},
			wantOptional:      []bool{false},
			wantMinArguments:  1,
			wantNullable:      sqlmodel.FunctionNullableCalledOnNull,
			wantVariadic:      true,
		},
		{
			name: "VARIADIC parameter with a default accepts no values",
			function: introspectedFunction{
				name: "join_parts", procKind: "f", returnType: "text", isVariadic: true, defaultCount: 1,
				parameters: []functionParameter{
					parameter("i", "separator", "text"),
					parameter("v", "parts", "text[]"),
				},
			},
			wantReturnType:    "text",
			wantArgumentNames: []string{"separator", "parts"},
			wantArgumentTypes: []string{"text", "text[]"},
			wantOptional:      []bool{false, true},
			wantMinArguments:  1,
			wantNullable:      sqlmodel.FunctionNullableCalledOnNull,
			wantVariadic:      true,
		},
		{
			name: "quoted names containing commas and quotes stay whole",
			function: introspectedFunction{
				name: "weird, name", procKind: "f", returnType: "text", defaultCount: 2,
				parameters: []functionParameter{
					parameter("i", "quoted, arg", "text"),
					parameter("i", "x\"y", "numeric"),
				},
			},
			wantReturnType:    "text",
			wantArgumentNames: []string{"quoted, arg", "x\"y"},
			wantArgumentTypes: []string{"text", "numeric"},
			wantOptional:      []bool{true, true},
			wantMinArguments:  0,
			wantNullable:      sqlmodel.FunctionNullableCalledOnNull,
		},
		{
			name: "aggregate with an unnamed argument",
			function: introspectedFunction{
				name: "my_sum", procKind: "a", returnType: "numeric",
				parameters: []functionParameter{parameter("i", "", "numeric")},
			},
			wantReturnType:    "numeric",
			wantArgumentNames: []string{""},
			wantArgumentTypes: []string{"numeric"},
			wantOptional:      []bool{false},
			wantMinArguments:  1,
			wantNullable:      sqlmodel.FunctionNullableCalledOnNull,
			wantAggregate:     true,
		},
		{
			name: "window function counts as an aggregate",
			function: introspectedFunction{
				name: "my_rank", procKind: "w", returnType: "bigint",
			},
			wantReturnType:    "bigint",
			wantArgumentNames: []string{},
			wantArgumentTypes: []string{},
			wantOptional:      []bool{},
			wantNullable:      sqlmodel.FunctionNullableCalledOnNull,
			wantAggregate:     true,
		},
		{
			name: "set-returning function without parameters",
			function: introspectedFunction{
				name: "noargs", procKind: "f", returnType: "integer", returnsSet: true,
			},
			wantReturnType:    "integer",
			wantArgumentNames: []string{},
			wantArgumentTypes: []string{},
			wantOptional:      []bool{},
			wantNullable:      sqlmodel.FunctionNullableCalledOnNull,
			wantReturnsSet:    true,
		},
		{
			name: "more defaults than inputs never makes the minimum negative",
			function: introspectedFunction{
				name: "odd", procKind: "f", returnType: "void", defaultCount: 3,
				parameters: []functionParameter{parameter("i", "only", "text")},
			},
			wantReturnType:    "void",
			wantArgumentNames: []string{"only"},
			wantArgumentTypes: []string{"text"},
			wantOptional:      []bool{true},
			wantMinArguments:  0,
			wantNullable:      sqlmodel.FunctionNullableCalledOnNull,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			signature := buildFunctionSignature(testCase.function, "app", modifierTypeNormaliser{})

			assert.Equal(t, testCase.function.name, signature.Name)
			assert.Equal(t, "app", signature.Schema)
			assert.Equal(t, testCase.wantReturnType, signature.ReturnType.EngineName)
			assert.Equal(t, testCase.wantArgumentNames, argumentNames(signature.Arguments))
			assert.Equal(t, testCase.wantArgumentTypes, typeNames(signature.Arguments))
			assert.Equal(t, testCase.wantOptional, optionalFlags(signature.Arguments))
			assert.Equal(t, testCase.wantMinArguments, signature.MinArguments)
			assert.Equal(t, testCase.wantNullable, signature.NullableBehaviour)
			assert.Equal(t, testCase.wantStrict, signature.IsStrict)
			assert.Equal(t, testCase.wantVariadic, signature.IsVariadic)
			assert.Equal(t, testCase.wantAggregate, signature.IsAggregate)
			assert.Equal(t, testCase.wantReturnsSet, signature.ReturnsSet)
		})
	}
}

func TestIsInputParameterMode(t *testing.T) {
	testCases := []struct {
		mode string
		want bool
	}{
		{mode: "i", want: true},
		{mode: "b", want: true},
		{mode: "v", want: true},
		{mode: "o", want: false},
		{mode: "t", want: false},
		{mode: "", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.mode, func(t *testing.T) {
			assert.Equal(t, testCase.want, isInputParameterMode(testCase.mode))
		})
	}
}

func parameterRow(functionID int64, name string, mode string, parameterName string, typeName string) functionParameterRow {
	row := functionParameterRow{}
	row.functionID = functionID
	row.functionName = name
	row.procKind = "f"
	row.returnType = "integer"
	if typeName != "" {
		row.parameterMode = sql.NullString{String: mode, Valid: true}
		row.parameterName = sql.NullString{String: parameterName, Valid: true}
		row.parameterType = sql.NullString{String: typeName, Valid: true}
	}
	return row
}

func TestGroupFunctionParameterRows(t *testing.T) {
	testCases := []struct {
		name           string
		rows           []functionParameterRow
		wantNames      []string
		wantParameters [][]functionParameter
	}{
		{
			name:           "no rows",
			rows:           nil,
			wantNames:      nil,
			wantParameters: nil,
		},
		{
			name: "overloads with one name stay separate",
			rows: []functionParameterRow{
				parameterRow(1, "area", "i", "side", "integer"),
				parameterRow(2, "area", "i", "width", "numeric"),
				parameterRow(2, "area", "i", "height", "numeric"),
			},
			wantNames: []string{"area", "area"},
			wantParameters: [][]functionParameter{
				{parameter("i", "side", "integer")},
				{parameter("i", "width", "numeric"), parameter("i", "height", "numeric")},
			},
		},
		{
			name: "function without parameters has an empty list",
			rows: []functionParameterRow{
				parameterRow(7, "now_utc", "", "", ""),
				parameterRow(8, "split_name", "i", "full_name", "text"),
				parameterRow(8, "split_name", "o", "first_name", "text"),
			},
			wantNames: []string{"now_utc", "split_name"},
			wantParameters: [][]functionParameter{
				nil,
				{parameter("i", "full_name", "text"), parameter("o", "first_name", "text")},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			functions := groupFunctionParameterRows(testCase.rows)

			var names []string
			var parameters [][]functionParameter
			for _, function := range functions {
				names = append(names, function.name)
				parameters = append(parameters, function.parameters)
			}
			assert.Equal(t, testCase.wantNames, names)
			assert.Equal(t, testCase.wantParameters, parameters)
		})
	}
}

func TestIntrospectFunctionsReadsStructuredParameters(t *testing.T) {
	database := openFakeDatabase(t, fakeQueryHandler{
		marker: "FROM pg_proc p",
		respond: fixedResult(functionParameterColumns,
			[]driver.Value{"app", int64(10), "add", "f", true, false, false, int64(1), "integer", "i", "a", "integer"},
			[]driver.Value{"app", int64(10), "add", "f", true, false, false, int64(1), "integer", "i", "b", "integer"},
			[]driver.Value{"app", int64(11), "noargs", "f", false, true, false, int64(0), "integer", nil, nil, nil},
			[]driver.Value{"app", int64(12), "total", "f", false, false, true, int64(0), "numeric", "v", "amounts", "numeric[]"},
		),
	})
	provider := New(database, modifierTypeNormaliser{})
	schema := newTestSchema("app")

	require.NoError(t, provider.introspectFunctions(t.Context(), catalogueOf(schema)))

	require.Len(t, schema.Functions["add"], 1)
	add := schema.Functions["add"][0]
	assert.Equal(t, []string{"a", "b"}, argumentNames(add.Arguments))
	assert.Equal(t, 1, add.MinArguments)
	assert.True(t, add.IsStrict)

	require.Len(t, schema.Functions["noargs"], 1)
	assert.Empty(t, schema.Functions["noargs"][0].Arguments)
	assert.True(t, schema.Functions["noargs"][0].ReturnsSet)

	require.Len(t, schema.Functions["total"], 1)
	assert.True(t, schema.Functions["total"][0].IsVariadic)
	assert.Equal(t, 1, schema.Functions["total"][0].MinArguments)
}

func TestIntrospectFunctionsFilesEachFunctionUnderTheKeyOfItsStoredName(t *testing.T) {
	tests := []struct {
		name     string
		function string
		wantKey  string
	}{
		{name: "a lower-case name", function: "add", wantKey: "add"},
		{name: "a quoted mixed-case name", function: "AddTax", wantKey: "AddTax"},
		{name: "a quoted upper-case name", function: "TOTAL", wantKey: "TOTAL"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			database := openFakeDatabase(t, fakeQueryHandler{
				marker: "FROM pg_proc p",
				respond: fixedResult(functionParameterColumns,
					[]driver.Value{"app", int64(10), testCase.function, "f", false, false, false, int64(0), "integer", "i", "a", "integer"},
					[]driver.Value{"elsewhere", int64(11), testCase.function, "f", false, false, false, int64(0), "integer", nil, nil, nil},
				),
			})
			schema := newTestSchema("app")

			require.NoError(t, New(database, modifierTypeNormaliser{}).introspectFunctions(t.Context(), catalogueOf(schema)))

			assert.Equal(t, []string{testCase.wantKey}, mapKeys(schema.Functions))
			require.Len(t, schema.Functions[testCase.wantKey], 1, "a function in a schema the catalogue does not cover is dropped")
			assert.Equal(t, testCase.function, schema.Functions[testCase.wantKey][0].Name)
			found := 0
			for range catalogueOf(schema).FunctionsOnPath("app", testCase.function) {
				found++
			}
			assert.Equal(t, 1, found, "the function is found by the name it is called by")
		})
	}
}

func TestIntrospectFunctionsKeepsAQuotedFunctionApartFromTheUnquotedOneOfTheSameLetters(t *testing.T) {
	database := openFakeDatabase(t, fakeQueryHandler{
		marker: "FROM pg_proc p",
		respond: fixedResult(functionParameterColumns,
			[]driver.Value{"app", int64(10), "AddTax", "f", false, false, false, int64(0), "integer", "i", "a", "integer"},
			[]driver.Value{"app", int64(11), "addtax", "f", false, false, false, int64(0), "text", "i", "a", "text"},
		),
	})
	schema := newTestSchema("app")
	catalogue := catalogueOf(schema)
	catalogue.SearchPath = []string{"app"}
	catalogue.Identifiers = sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false}

	require.NoError(t, New(database, modifierTypeNormaliser{}).introspectFunctions(t.Context(), catalogue))

	testCases := []struct {
		name       string
		called     string
		wantReturn string
	}{
		{name: "the unquoted call finds the unquoted function", called: "addtax", wantReturn: "text"},
		{name: "the quoted call finds the quoted function", called: "AddTax", wantReturn: "integer"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var returns []string
			for _, overloads := range catalogue.FunctionsOnPath("", testCase.called) {
				for _, overload := range overloads {
					returns = append(returns, overload.ReturnType.EngineName)
				}
			}
			assert.Equal(t, []string{testCase.wantReturn}, returns)
		})
	}
}

func TestIntrospectedFunctionsResolveAsReplayedOnesDo(t *testing.T) {
	engine := pgdialect.New()
	database := openFakeDatabase(t, fakeQueryHandler{
		marker: "FROM pg_proc p",
		respond: fixedResult(functionParameterColumns,
			[]driver.Value{"public", int64(10), "AddTax", "f", false, false, false, int64(0), "integer", "i", "amount", "integer"},
			[]driver.Value{"public", int64(11), "addtax", "f", false, false, false, int64(0), "text", "i", "amount", "bigint"},
		),
	})
	introspected := catalogueOf(newTestSchema("public"))
	require.NoError(t, New(database, engine).introspectFunctions(t.Context(), introspected))

	migrations := fstest.MapFS{"migrations/001_functions.up.sql": {Data: []byte(`CREATE FUNCTION "AddTax"(amount integer) RETURNS integer
LANGUAGE sql AS $$ SELECT amount $$;
CREATE FUNCTION addtax(amount bigint) RETURNS text
LANGUAGE sql AS $$ SELECT amount::text $$;
`)}}
	provider := replay.NewMigrationCatalogueProvider(engine, sqlfile.NewFSReader(migrations, 0, 0), "migrations", namespace.Default(), replay.Seed{})
	replayed, diagnostics, err := provider.BuildCatalogue(t.Context())
	require.NoError(t, err)
	require.Empty(t, diagnostics)

	dialectInfo := engine.Info()
	replay.Adopt(introspected, &dialectInfo)
	replay.Adopt(replayed, &dialectInfo)
	require.Equal(t, mapKeys(replayed.Schemas["public"].Functions), mapKeys(introspected.Schemas["public"].Functions))

	testCases := []struct {
		name         string
		called       string
		wantArgument string
	}{
		{name: "the quoted function", called: "AddTax", wantArgument: "int4"},
		{name: "the unquoted function", called: "addtax", wantArgument: "int8"},
		{name: "a name in another case reaches neither", called: "ADDTAX", wantArgument: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			for _, built := range []*sqlmodel.Catalogue{introspected, replayed} {
				argument, _ := resolve.NewFunctionResolver(engine.BuiltinFunctions(), built, engine).ArgumentType(testCase.called, 0)
				assert.Equal(t, testCase.wantArgument, argument.EngineName)
				var names []string
				for _, overloads := range built.FunctionsOnPath("", testCase.called) {
					for _, overload := range overloads {
						names = append(names, overload.Name)
					}
				}
				if testCase.wantArgument == "" {
					assert.Empty(t, names)
				} else {
					assert.Equal(t, []string{testCase.called}, names)
				}
			}
		})
	}
}

func TestIntrospectFunctionsReportsQueryFailures(t *testing.T) {
	queryFailure := errors.New("permission denied for pg_proc")
	closeFailure := errors.New("connection reset while closing")
	iterationFailure := errors.New("connection reset while reading")

	testCases := []struct {
		wantError error
		name      string
		result    fakeQueryResult
	}{
		{
			name:      "query fails",
			result:    fakeQueryResult{err: queryFailure},
			wantError: queryFailure,
		},
		{
			name:      "iteration fails",
			result:    fakeQueryResult{columns: functionParameterColumns, nextError: iterationFailure},
			wantError: iterationFailure,
		},
		{
			name:      "closing the rows fails",
			result:    fakeQueryResult{columns: functionParameterColumns, closeError: closeFailure},
			wantError: closeFailure,
		},
		{
			name: "row cannot be scanned",
			result: fakeQueryResult{
				columns: functionParameterColumns,
				rows:    [][]driver.Value{{"app", "not a number", "f", "f", true, false, false, int64(0), "integer", nil, nil, nil}},
			},
			wantError: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			database := openFakeDatabase(t, fakeQueryHandler{
				marker:  "FROM pg_proc p",
				respond: func([]driver.NamedValue) fakeQueryResult { return testCase.result },
			})
			provider := New(database, modifierTypeNormaliser{})

			err := provider.introspectFunctions(t.Context(), catalogueOf(newTestSchema("app")))
			require.Error(t, err)
			if testCase.wantError != nil {
				assert.ErrorIs(t, err, testCase.wantError)
			}
		})
	}
}
