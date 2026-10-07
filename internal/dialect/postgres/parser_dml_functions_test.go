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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestArrayConstructorsReadAsCallsOfTheConstructor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantElements []string
	}{
		{name: "a list of values", sql: "SELECT ARRAY[1, 2] AS a", wantElements: []string{"literal", "literal"}},
		{name: "a list of nested lists", sql: "SELECT ARRAY[[1, 2], [3, 4]] AS a", wantElements: []string{"array", "array"}},
		{name: "a list of constructors", sql: "SELECT ARRAY[ARRAY[1], ARRAY[2]] AS a", wantElements: []string{"array", "array"}},
		{name: "a subquery", sql: "SELECT ARRAY(SELECT id FROM users) AS a", wantElements: []string{"scalar_subquery"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.OutputColumns, 1)
			call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
			require.True(t, isCall)
			require.Equal(t, arrayConstructor, call.FunctionName)
			elements := make([]string, len(call.Arguments))
			for index, argument := range call.Arguments {
				elements[index] = arrayElementKind(argument)
			}
			require.Equal(t, testCase.wantElements, elements)
		})
	}
}

func arrayElementKind(expression ast.Expression) string {
	switch element := expression.(type) {
	case *ast.LiteralExpression:
		return "literal"
	case *ast.ScalarSubqueryExpression:
		return "scalar_subquery"
	case *ast.FunctionCallExpression:
		if element.FunctionName == arrayConstructor {
			return "array"
		}
		return "call"
	default:
		return "other"
	}
}

func TestACallKeepsTheNameItsTokenGives(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		wantFunction  string
		wantSchema    string
		wantEnclosing string
	}{
		{name: "a quoted mixed-case name keeps its case", sql: `SELECT "Foo"($1) AS a`, wantFunction: "Foo", wantEnclosing: "Foo"},
		{name: "an unquoted name folds to lower case", sql: `SELECT FOO($1) AS a`, wantFunction: "foo", wantEnclosing: "foo"},
		{name: "a quoted lower-case name is the unquoted one", sql: `SELECT "foo"($1) AS a`, wantFunction: "foo", wantEnclosing: "foo"},
		{name: "a qualified quoted name keeps its case", sql: `SELECT reports."Foo"($1) AS a`, wantFunction: "Foo", wantSchema: "reports",
			wantEnclosing: "reports.Foo"},
		{name: "a quoted schema keeps its case", sql: `SELECT "Reports".foo($1) AS a`, wantFunction: "foo", wantSchema: "Reports",
			wantEnclosing: "Reports.foo"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.OutputColumns, 1)
			call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
			require.True(t, isCall)
			require.Equal(t, testCase.wantFunction, call.FunctionName)
			require.Equal(t, testCase.wantSchema, call.Schema)
			require.Len(t, analysis.ParameterReferences, 1)
			require.Equal(t, testCase.wantEnclosing, analysis.ParameterReferences[0].EnclosingFunctionName)
		})
	}
}

func TestACastTargetReadsItsSchemaQualifierAndBoundedArrayBrackets(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantTypeName   string
		wantEngineName string
	}{
		{name: "a schema-qualified target", sql: "SELECT CAST($1 AS shapes.pair) AS pair", wantTypeName: "shapes.pair",
			wantEngineName: "shapes.pair"},
		{name: "a bounded array target", sql: "SELECT CAST($1 AS integer[3]) AS numbers", wantTypeName: "integer[]",
			wantEngineName: "integer[]"},
		{name: "a two-dimensional array target", sql: "SELECT CAST($1 AS text[][2]) AS grid", wantTypeName: "text[][]",
			wantEngineName: "text[][]"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)
			require.Len(t, analysis.OutputColumns, 1)
			cast, isCast := analysis.OutputColumns[0].Expression.(*ast.CastExpression)
			require.True(t, isCast, "expression %T", analysis.OutputColumns[0].Expression)
			require.Equal(t, testCase.wantTypeName, cast.TypeName)
			require.Len(t, analysis.ParameterReferences, 1)
			require.NotNil(t, analysis.ParameterReferences[0].CastType)
			require.Equal(t, testCase.wantEngineName, analysis.ParameterReferences[0].CastType.EngineName)
		})
	}
}
