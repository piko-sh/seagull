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

package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

const scanBudget = 500 * time.Millisecond

func TestRowValuesCompareElementByElement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		left       int
		parameters int
	}{
		{name: "a row against a row of parameters", sql: "SELECT id FROM posts WHERE (id, user_id) < (?, ?)", left: 2, parameters: 2},
		{name: "a row against a row of literals", sql: "SELECT id FROM posts WHERE (id, title) = (1, 'a')", left: 2},
		{name: "a row in a list of rows", sql: "SELECT id FROM posts WHERE (id, user_id) IN ((1, 2), (?, ?))", left: 2, parameters: 2},
		{name: "a row against a subquery", sql: "SELECT id FROM posts WHERE (id, user_id) = (SELECT id, user_id FROM posts WHERE id = ?)", left: 2, parameters: 1},
		{name: "a row of three", sql: "SELECT id FROM posts WHERE (user_id, title, id) > (?, ?, ?) ORDER BY user_id, title, id", left: 3, parameters: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), test.sql)
			require.Len(t, analysis.ParameterReferences, test.parameters)
			var condition ast.Expression
			for _, clause := range analysis.ClauseExpressions {
				if clause.Clause == ast.ClauseWhere {
					condition = clause.Expression
				}
			}
			require.NotNil(t, condition)
			row := leftOperand(condition)
			require.NotNil(t, row, "%T", condition)
			require.Len(t, row.Elements, test.left)
		})
	}
}

func leftOperand(condition ast.Expression) *ast.RowValueExpression {
	var operand ast.Expression
	switch typed := condition.(type) {
	case *ast.ComparisonExpression:
		operand = typed.Left
	case *ast.InListExpression:
		operand = typed.Inner
	default:
		return nil
	}
	row, isRow := operand.(*ast.RowValueExpression)
	if !isRow {
		return nil
	}
	return row
}

func TestAParameterACastConvertsTakesTheTypeItsValueKeeps(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		wantCast string
	}{
		{name: "a truth value cast to BOOLEAN stays a truth value", sql: "SELECT id FROM users WHERE CAST(? AS boolean)", wantCast: "boolean"},
		{name: "a truth value cast to BOOL stays a truth value", sql: "SELECT id FROM users WHERE CAST(? AS BOOL) OR id = 1", wantCast: "boolean"},
		{name: "a value cast to JSON is a number", sql: "SELECT CAST(? AS JSON) AS doc FROM users", wantCast: "numeric"},
		{name: "a value cast to STRING is a number", sql: "SELECT CAST(? AS STRING) AS label FROM users", wantCast: "numeric"},
		{name: "a value cast to DATE is a number", sql: "SELECT CAST(? AS DATE) AS day FROM users", wantCast: "numeric"},
		{name: "a value cast to TEXT is text", sql: "SELECT CAST(? AS TEXT) AS label FROM users", wantCast: "text"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), testCase.sql)

			require.Len(t, analysis.ParameterReferences, 1)
			require.NotNil(t, analysis.ParameterReferences[0].CastType)
			require.Equal(t, testCase.wantCast, analysis.ParameterReferences[0].CastType.EngineName)
		})
	}
}

func TestACastTargetAgreesWithAColumnDeclaredWithTheSameType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		spelling     string
		wantTypeName string
	}{
		{name: "a length", spelling: "VARCHAR(10)", wantTypeName: "varchar(10)"},
		{name: "a precision and a negative scale", spelling: "DECIMAL(10, -2)", wantTypeName: "decimal(10,-2)"},
		{name: "a signed precision written with spaces", spelling: "NUMERIC( + 5 )", wantTypeName: "numeric(5)"},
		{name: "a spelling read by its affinity", spelling: "MONEY(12, 4)", wantTypeName: "money(12,4)"},
		{name: "a spelling of two words", spelling: "CHARACTER VARYING(40)", wantTypeName: "character varying(40)"},
		{name: "a spelling written without modifiers", spelling: "DECIMAL", wantTypeName: "decimal"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			declared := applyDDL(t, "CREATE TABLE t (a "+testCase.spelling+")")
			require.Len(t, declared.Columns, 1)
			analysis := analyseQuery(t, newSQLiteCatalogue(), "SELECT CAST(? AS "+testCase.spelling+") AS a FROM users")
			require.Len(t, analysis.OutputColumns, 1)
			cast, isCast := analysis.OutputColumns[0].Expression.(*ast.CastExpression)
			require.True(t, isCast, "got %T", analysis.OutputColumns[0].Expression)
			require.Len(t, analysis.ParameterReferences, 1)
			require.NotNil(t, analysis.ParameterReferences[0].CastType)

			assert.Equal(t, testCase.wantTypeName, cast.TypeName)
			assert.Equal(t, declared.Columns[0].SQLType, New().NormaliseTypeName(cast.TypeName))
			assert.Equal(t, declared.Columns[0].SQLType, *analysis.ParameterReferences[0].CastType)
		})
	}
}

func TestACastTypedByItsAffinityTakesNoModifiers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                string
		spelling            string
		wantTypeName        string
		wantParameterEngine string
	}{
		{name: "a timestamp with a fractional-second precision", spelling: "TIMESTAMP(3)", wantTypeName: "numeric",
			wantParameterEngine: "numeric"},
		{name: "a string with a length", spelling: "STRING(10)", wantTypeName: "numeric", wantParameterEngine: "numeric"},
		{name: "a truth value with a width", spelling: "BOOLEAN(1)", wantTypeName: "numeric",
			wantParameterEngine: "boolean"},
		{name: "a list of more modifiers than a type takes", spelling: "VARCHAR(1, 2, 3)", wantTypeName: "varchar",
			wantParameterEngine: "text"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), "SELECT CAST(? AS "+testCase.spelling+") AS a FROM users")
			require.Len(t, analysis.OutputColumns, 1)
			cast, isCast := analysis.OutputColumns[0].Expression.(*ast.CastExpression)
			require.True(t, isCast, "got %T", analysis.OutputColumns[0].Expression)
			require.Len(t, analysis.ParameterReferences, 1)
			parameterType := analysis.ParameterReferences[0].CastType
			require.NotNil(t, parameterType)
			resolved := New().NormaliseTypeName(cast.TypeName)

			assert.Equal(t, testCase.wantTypeName, cast.TypeName)
			assert.Nil(t, resolved.Precision)
			assert.Nil(t, resolved.Length)
			assert.Equal(t, testCase.wantParameterEngine, parameterType.EngineName)
			assert.Nil(t, parameterType.Precision)
			assert.Nil(t, parameterType.Length)
		})
	}
}

func TestTaggingTheOperandsOfManyPlaceholdersFinishesWithinItsBudget(t *testing.T) {
	const placeholders = 60_000

	testCases := []struct {
		query func(count int) string
		name  string
	}{
		{
			name: "IN lists in a condition",
			query: func(count int) string {
				return "SELECT id FROM users WHERE " + repeatJoined("id IN (?)", " OR ", count)
			},
		},
		{
			name:  "IS comparisons in a condition",
			query: func(count int) string { return "SELECT id FROM users WHERE " + repeatJoined("id IS ?", " OR ", count) },
		},
		{
			name: "BETWEEN bounds in a condition",
			query: func(count int) string {
				return "SELECT id FROM users WHERE " + repeatJoined("id BETWEEN ? AND ?", " OR ", count)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			requireAnalysedWithinBudget(t, testCase.query(placeholders), nil)
		})
	}
}

func TestTaggingAnOperandsPlaceholdersKeepsTheirContexts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantContext []sqlmodel.ParameterContext
		wantColumns []string
	}{
		{
			name:        "an IN list after a comparison",
			sql:         "SELECT id FROM users WHERE name = ? AND id IN (?, ?)",
			wantContext: []sqlmodel.ParameterContext{sqlmodel.ParameterContextComparison, sqlmodel.ParameterContextInList, sqlmodel.ParameterContextInList},
			wantColumns: []string{"name", "id", "id"},
		},
		{
			name:        "BETWEEN bounds after an IS comparison",
			sql:         "SELECT id FROM users WHERE email IS ? AND id BETWEEN ? AND ?",
			wantContext: []sqlmodel.ParameterContext{sqlmodel.ParameterContextComparison, sqlmodel.ParameterContextBetween, sqlmodel.ParameterContextBetween},
			wantColumns: []string{"email", "id", "id"},
		},
		{
			name:        "IS comparisons in the select list",
			sql:         "SELECT id IS ?, name IS ? FROM users",
			wantContext: []sqlmodel.ParameterContext{sqlmodel.ParameterContextComparison, sqlmodel.ParameterContextComparison},
			wantColumns: []string{"id", "name"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), testCase.sql)

			require.Len(t, analysis.ParameterReferences, len(testCase.wantContext))
			for index, reference := range analysis.ParameterReferences {
				require.Equal(t, testCase.wantContext[index], reference.Context, "parameter %d", index)
				require.NotNil(t, reference.ColumnReference, "parameter %d", index)
				require.Equal(t, testCase.wantColumns[index], reference.ColumnReference.ColumnName, "parameter %d", index)
			}
		})
	}
}

func repeatJoined(item, separator string, count int) string {
	return strings.TrimSuffix(strings.Repeat(item+separator, count), separator)
}

func requireAnalysedWithinBudget(t *testing.T, sql string, wantErr error) {
	t.Helper()

	engine := New(WithMaxTokensPerStatement(dialectkit.MaxTokensPerStatementCeiling))
	statement := parseOne(t, engine, sql)

	scaling.Within(t, scanBudget, func(ctx context.Context) {
		_, err := engine.AnalyseQuery(ctx, nil, statement)
		if wantErr == nil {
			require.NoError(t, err)
			return
		}
		require.ErrorIs(t, err, wantErr)
	})
}
