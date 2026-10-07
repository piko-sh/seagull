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
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestATypeNameFollowedByAStringIsATypedLiteral(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		typeName string
	}{
		{name: "a date", sql: "SELECT DATE '2024-01-01' AS value", typeName: "date"},
		{name: "a timestamp", sql: "SELECT TIMESTAMP '2024-01-01 10:00' AS value", typeName: "timestamp"},
		{name: "a time", sql: "SELECT TIME '10:00' AS value", typeName: "time"},
		{name: "a lower-case type name", sql: "SELECT date '2024-01-01' AS value", typeName: "date"},
		{name: "a timestamp with time zone", sql: "SELECT TIMESTAMP WITH TIME ZONE '2024-01-01' AS value", typeName: "timestamp with time zone"},
		{name: "a time with time zone", sql: "SELECT TIME WITH TIME ZONE '10:00+00' AS value", typeName: "time with time zone"},
		{name: "any other type", sql: "SELECT UUID '00000000-0000-0000-0000-000000000000' AS value", typeName: "uuid"},
		{name: "one compared with a column", sql: "SELECT date_diff('day', created_at, DATE '2024-12-31') AS value FROM users WHERE created_at > DATE '2024-01-01'"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, buildTestCatalogue(), testCase.sql)

			require.Len(t, analysis.OutputColumns, 1)
			if testCase.typeName == "" {
				return
			}
			literal, isLiteral := analysis.OutputColumns[0].Expression.(*ast.LiteralExpression)
			require.Truef(t, isLiteral, "got %T", analysis.OutputColumns[0].Expression)
			require.Equal(t, testCase.typeName, literal.TypeName)
		})
	}
}

func TestAReservedWordBeforeAStringIsNoTypedLiteral(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, buildTestCatalogue(), `SELECT trim(BOTH 'x' FROM name) AS trimmed FROM users`)

	require.Len(t, analysis.OutputColumns, 1)
	call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
	require.Truef(t, isCall, "got %T", analysis.OutputColumns[0].Expression)
	require.Equal(t, "trim", call.FunctionName)
}

func TestACastNamesItsTargetWithItsParts(t *testing.T) {
	t.Parallel()

	engine := New()
	testCases := []struct {
		name     string
		sql      string
		category sqlmodel.SQLTypeCategory
		inner    sqlmodel.SQLTypeCategory
	}{
		{name: "a fixed-size array", sql: "SELECT CAST(id AS INTEGER[3]) AS value FROM users", category: sqlmodel.TypeCategoryArray, inner: sqlmodel.TypeCategoryInteger},
		{name: "a map", sql: "SELECT CAST(name AS MAP(VARCHAR, INTEGER)) AS value FROM users", category: sqlmodel.TypeCategoryMap, inner: sqlmodel.TypeCategoryInteger},
		{name: "a struct by the cast operator", sql: "SELECT name::STRUCT(x DOUBLE) AS value FROM users", category: sqlmodel.TypeCategoryStruct, inner: sqlmodel.TypeCategoryFloat},
		{name: "a list of maps by TRY_CAST", sql: "SELECT TRY_CAST(name AS MAP(VARCHAR, DOUBLE)[]) AS value FROM users", category: sqlmodel.TypeCategoryArray, inner: sqlmodel.TypeCategoryMap},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, buildTestCatalogue(), testCase.sql)
			require.Len(t, analysis.OutputColumns, 1)
			cast := castIn(analysis.OutputColumns[0].Expression)
			require.NotNil(t, cast, "%T", analysis.OutputColumns[0].Expression)
			target := engine.NormaliseTypeName(cast.TypeName)
			require.Equal(t, testCase.category, target.Category, cast.TypeName)
			switch target.Category {
			case sqlmodel.TypeCategoryStruct:
				require.Len(t, target.StructFields, 1)
				require.Equal(t, testCase.inner, target.StructFields[0].SQLType.Category)
			default:
				require.NotNil(t, target.ElementType)
				require.Equal(t, testCase.inner, target.ElementType.Category)
			}
		})
	}
}

func TestATryCastGivesNullForAValueItCannotConvert(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, buildTestCatalogue(), "SELECT TRY_CAST(name AS INTEGER) AS tried, CAST(name AS INTEGER) AS cast FROM users")
	require.Len(t, analysis.OutputColumns, 2)
	tried, isCase := analysis.OutputColumns[0].Expression.(*ast.CaseWhenExpression)
	require.True(t, isCase, "%T", analysis.OutputColumns[0].Expression)
	require.Nil(t, tried.ElseResult)
	require.Len(t, tried.Branches, 1)
	cast, isCast := tried.Branches[0].Result.(*ast.CastExpression)
	require.True(t, isCast)
	require.Equal(t, "integer", cast.TypeName)
	_, isPlainCast := analysis.OutputColumns[1].Expression.(*ast.CastExpression)
	require.True(t, isPlainCast)
}

func TestTryGivesNullForAnExpressionThatFails(t *testing.T) {
	t.Parallel()

	overloads := New().BuiltinFunctions().Functions["try"]
	require.Len(t, overloads, 1)
	require.Equal(t, sqlmodel.FunctionNullableCalledOnNull, overloads[0].NullableBehaviour)
	require.Equal(t, 1, overloads[0].ReturnTypeFromArgument)
}

func castIn(expression ast.Expression) *ast.CastExpression {
	if tried, isCase := expression.(*ast.CaseWhenExpression); isCase && len(tried.Branches) == 1 {
		expression = tried.Branches[0].Result
	}
	cast, isCast := expression.(*ast.CastExpression)
	if !isCast {
		return nil
	}
	return cast
}

func TestAStructLiteralOfAnyWidthIsOneShallowConstructor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		fields int
	}{
		{name: "one field", fields: 1},
		{name: "two thousand fields", fields: 2000},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, wideStructLiteral(testCase.fields))

			structure, isStruct := analysis.OutputColumns[0].Expression.(*ast.StructExpression)
			require.True(t, isStruct)
			require.Len(t, structure.Names, testCase.fields)
			require.Len(t, structure.Values, testCase.fields)
			require.Equal(t, "f0", structure.Names[0])
			for _, value := range structure.Values {
				_, isLiteral := value.(*ast.LiteralExpression)
				require.True(t, isLiteral)
			}
		})
	}
}

func TestParsingAWideStructLiteralCostsLinearTimeInItsFields(t *testing.T) {
	t.Parallel()

	engine := New()
	analyse := func(fields int) func() {
		sql := wideStructLiteral(fields)
		return func() {
			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)
			_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
			require.NoError(t, err)
		}
	}

	scaling.Linear(t, analyse(2000), analyse(8000))
}

func TestAWideStructTypesWithoutADepthDiagnostic(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantFields int
	}{
		{name: "a struct literal", sql: wideStructLiteral(2000), wantFields: 2000},
		{name: "struct_pack", sql: wideStructPack(2000), wantFields: 2000},
		{name: "struct_insert", sql: wideStructInsert(2000), wantFields: 2001},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			generator, err := app.New(New())
			require.NoError(t, err)
			report, err := generator.Vet(t.Context(), app.Input{
				Migrations: fstest.MapFS{
					"migrations/001_boats.up.sql": {Data: []byte("CREATE TABLE boats (id INTEGER NOT NULL);")},
					"queries/wide.sql":            {Data: []byte("-- sgl.query(name: Wide, command: one)\n" + testCase.sql + ";")},
				},
				MigrationDir: "migrations",
				QueryDir:     "queries",
			})
			require.NoError(t, err)
			require.Empty(t, report.Diagnostics)
			require.Len(t, report.Queries, 1)
			require.Len(t, report.Queries[0].OutputColumns, 1)
			column := report.Queries[0].OutputColumns[0]
			require.Equal(t, sqlmodel.TypeCategoryStruct, column.SQLType.Category)
			require.Len(t, column.SQLType.StructFields, testCase.wantFields)
			require.False(t, column.Nullable)
			last := column.SQLType.StructFields[testCase.wantFields-1]
			require.Equal(t, "f1999", last.Name)
			require.Equal(t, sqlmodel.TypeCategoryInteger, last.SQLType.Category)
			require.True(t, last.SQLType.Nullable)
		})
	}
}

func wideStructLiteral(fields int) string {
	entries := make([]string, fields)
	for index := range entries {
		entries[index] = "'f" + strconv.Itoa(index) + "': " + strconv.Itoa(index)
	}
	return "SELECT {" + strings.Join(entries, ", ") + "} AS wide"
}

func wideStructPack(fields int) string {
	return "SELECT struct_pack(" + namedArguments(fields) + ") AS wide"
}

func wideStructInsert(fields int) string {
	return "SELECT struct_insert({'base': 1}, " + namedArguments(fields) + ") AS wide"
}

func namedArguments(count int) string {
	arguments := make([]string, count)
	for index := range arguments {
		arguments[index] = "f" + strconv.Itoa(index) + " := " + strconv.Itoa(index)
	}
	return strings.Join(arguments, ", ")
}
