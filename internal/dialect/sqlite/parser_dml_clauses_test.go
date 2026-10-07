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

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestMultiColumnSetFromASubqueryTerminates(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		parameters int
		subqueries int
	}{
		{name: "subquery with FROM and a parameter", sql: "UPDATE users SET (name, email) = (SELECT name, email FROM users WHERE id = ?) WHERE id = ?", parameters: 2, subqueries: 1},
		{name: "subquery with FROM and LIMIT", sql: "UPDATE users SET (name, email) = (SELECT name, email FROM users LIMIT 1)", parameters: 0, subqueries: 1},
		{name: "one column from a subquery", sql: "UPDATE users SET (name) = (SELECT name FROM users LIMIT 1)", parameters: 0, subqueries: 1},
		{name: "subquery without FROM", sql: "UPDATE users SET (name, email) = (SELECT 'x', 'y') WHERE id = ?", parameters: 1, subqueries: 1},
		{name: "a single column from a subquery", sql: "UPDATE users SET name = (SELECT name FROM orders WHERE orders.id = ?) WHERE id = ?", parameters: 2, subqueries: 1},
		{name: "a list of values", sql: "UPDATE users SET (name, email) = (?, ?) WHERE id = ?", parameters: 3},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)
			var analysis *ast.RawQueryAnalysis
			scaling.Within(t, 10*time.Second, func(ctx context.Context) {
				var analyseError error
				analysis, analyseError = engine.AnalyseQuery(ctx, nil, statements[0])
				require.NoError(t, analyseError)
			})

			numbers := map[int]struct{}{}
			for _, reference := range analysis.ParameterReferences {
				numbers[reference.Number] = struct{}{}
			}
			require.Len(t, numbers, testCase.parameters)
			require.Len(t, analysis.PredicateSubqueries, testCase.subqueries)
		})
	}
}

func TestASubqueryInAValuesRowTypesItsOwnParameters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantNumber int
	}{
		{name: "the subquery is the value", sql: "INSERT INTO posts (id, user_id, title) VALUES (?, (SELECT id FROM users WHERE email = ?), ?)", wantNumber: 2},
		{name: "the subquery is inside the value", sql: "INSERT INTO posts (id, user_id, title) VALUES (?, (COALESCE((SELECT id FROM users WHERE email = ?), 0)), ?)", wantNumber: 2},
		{name: "the subquery is an argument of a call", sql: "INSERT INTO posts (id, user_id, title) VALUES (?, COALESCE((SELECT id FROM users WHERE email = ?), 0), ?)", wantNumber: 2},
		{name: "the subquery is a CASE result", sql: "INSERT INTO posts (id, user_id, title) VALUES (?, CASE WHEN ? = '' THEN (SELECT id FROM users WHERE email = ?) END, ?)", wantNumber: 3},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)

			var compared *ast.RawParameterReference
			for index := range analysis.ParameterReferences {
				if analysis.ParameterReferences[index].Number == testCase.wantNumber {
					compared = &analysis.ParameterReferences[index]
				}
			}
			require.NotNil(t, compared)
			require.Equal(t, sqlmodel.ParameterContextComparison, compared.Context)
			require.Equal(t, "email", compared.ColumnReference.ColumnName)
		})
	}
}

func TestARowAssignmentTypesEachValueAsItsColumn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sql     string
		columns []string
	}{
		{name: "a row of parameters", sql: "UPDATE posts SET (title, user_id) = (?, ?) WHERE id = ?", columns: []string{"title", "user_id", "id"}},
		{name: "a row beside a column", sql: "UPDATE posts SET body = ?, (title, user_id) = (?, ?)", columns: []string{"body", "title", "user_id"}},
		{name: "a row holding an expression", sql: "UPDATE posts SET (title, user_id) = (upper(?), ? + 1)", columns: []string{"title", "user_id"}},
		{name: "a row from a subquery", sql: "UPDATE posts SET (title, body) = (SELECT name, email FROM users WHERE id = ?)", columns: []string{"id"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), test.sql)
			columns := make([]string, 0, len(analysis.ParameterReferences))
			for _, reference := range analysis.ParameterReferences {
				require.NotNil(t, reference.ColumnReference, "parameter %d", reference.Number)
				columns = append(columns, reference.ColumnReference.ColumnName)
			}
			require.Equal(t, test.columns, columns)
		})
	}
}

func TestScanningTheConditionOfManyPlaceholdersFinishesWithinItsBudget(t *testing.T) {
	const size = 60_000

	testCases := []struct {
		wantErr error
		query   func(count int) string
		name    string
	}{
		{
			name: "the arguments of one call",
			query: func(count int) string {
				return "SELECT id FROM users WHERE id = max(" + repeatJoined("?", ", ", count) + ")"
			},
		},
		{
			name: "the operand of one CAST",
			query: func(count int) string {
				return "SELECT id FROM users WHERE id = CAST(" + repeatJoined("?", " + ", count) + " AS INTEGER)"
			},
		},
		{
			name:    "groups compared one inside another",
			wantErr: dialectkit.ErrParseDepth,
			query: func(count int) string {
				return "SELECT id FROM users WHERE " + strings.Repeat("(", count) + "id" + strings.Repeat(") = ?", count)
			},
		},
		{
			name:    "AND bounds one inside another",
			wantErr: dialectkit.ErrParseDepth,
			query: func(count int) string {
				return "SELECT id FROM users WHERE " + strings.Repeat("(", count) + "id = 1" + strings.Repeat(" AND ?)", count)
			},
		},
		{
			name: "a pattern matched against a long expression",
			query: func(count int) string {
				return "SELECT id FROM users WHERE (" + repeatJoined("1", " + ", count) + " || name) LIKE " + repeatJoined("?", " || ", count)
			},
		},
		{
			name: "a CAST to a long type name",
			query: func(count int) string {
				return "SELECT id FROM users WHERE id = CAST(" + repeatJoined("?", " + ", count) + " AS " + repeatJoined("INTEGER", " ", count) + ")"
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			requireAnalysedWithinBudget(t, testCase.query(size), testCase.wantErr)
		})
	}
}

func TestScanningAConditionsPlaceholdersFindsTheirSlotsAndTypes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		check func(t *testing.T, references []ast.RawParameterReference)
		name  string
		sql   string
	}{
		{
			name: "the slots of a call's arguments, past a nested call",
			sql:  "SELECT id FROM users WHERE length(substr(?, 1, ?), ?) > 0",
			check: func(t *testing.T, references []ast.RawParameterReference) {
				require.Len(t, references, 3)
				require.Equal(t, "substr", references[0].EnclosingFunctionName)
				require.Equal(t, 0, references[0].ArgumentOrdinal)
				require.Equal(t, "substr", references[1].EnclosingFunctionName)
				require.Equal(t, 2, references[1].ArgumentOrdinal)
				require.Equal(t, "length", references[2].EnclosingFunctionName)
				require.Equal(t, 1, references[2].ArgumentOrdinal)
			},
		},
		{
			name: "a placeholder compared with a call whose result does not depend on its arguments",
			sql:  "SELECT id FROM users WHERE length(name, 1) = ? AND typeof(name) = ?",
			check: func(t *testing.T, references []ast.RawParameterReference) {
				require.Len(t, references, 2)
				require.Nil(t, references[0].CastType)
				require.NotNil(t, references[1].CastType)
				require.Equal(t, sqlmodel.TypeCategoryText, references[1].CastType.Category)
			},
		},
		{
			name: "a placeholder in a CAST takes the CAST's type",
			sql:  "SELECT id FROM users WHERE id = CAST(? + ? AS TEXT)",
			check: func(t *testing.T, references []ast.RawParameterReference) {
				require.Len(t, references, 2)
				for _, reference := range references {
					require.NotNil(t, reference.CastType)
					require.Equal(t, sqlmodel.TypeCategoryText, reference.CastType.Category)
				}
			},
		},
		{
			name: "placeholders in a CAST take the modifiers of its type",
			sql:  "SELECT id FROM users WHERE id = CAST(? + ? AS VARCHAR(10))",
			check: func(t *testing.T, references []ast.RawParameterReference) {
				require.Len(t, references, 2)
				for _, reference := range references {
					require.NotNil(t, reference.CastType)
					require.Equal(t, sqlmodel.TypeCategoryText, reference.CastType.Category)
					require.Equal(t, new(10), reference.CastType.Length)
				}
			},
		},
		{
			name: "placeholders in a CAST to a list of more modifiers than a type takes take none",
			sql:  "SELECT id FROM users WHERE id = CAST(? + ? AS DECIMAL(10, 2, 3))",
			check: func(t *testing.T, references []ast.RawParameterReference) {
				require.Len(t, references, 2)
				for _, reference := range references {
					require.NotNil(t, reference.CastType)
					require.Equal(t, sqlmodel.TypeCategoryDecimal, reference.CastType.Category)
					require.Nil(t, reference.CastType.Precision)
				}
			},
		},
		{
			name: "a placeholder in a CAST whose operand holds a group takes no type",
			sql:  "SELECT id FROM users WHERE id = CAST(? + (1) AS TEXT)",
			check: func(t *testing.T, references []ast.RawParameterReference) {
				require.Len(t, references, 1)
				require.Nil(t, references[0].CastType)
			},
		},
		{
			name: "a BETWEEN bound after a parenthesised low bound",
			sql:  "SELECT id FROM users WHERE id BETWEEN (1 + 1) AND ?",
			check: func(t *testing.T, references []ast.RawParameterReference) {
				require.Len(t, references, 1)
				require.Equal(t, sqlmodel.ParameterContextBetween, references[0].Context)
				require.Equal(t, "id", references[0].ColumnReference.ColumnName)
			},
		},
		{
			name: "every placeholder of a pattern names the column the pattern matches",
			sql:  "SELECT id FROM users WHERE (name || email) LIKE ? || ?",
			check: func(t *testing.T, references []ast.RawParameterReference) {
				require.Len(t, references, 2)
				for _, reference := range references {
					require.Equal(t, sqlmodel.ParameterContextLike, reference.Context)
					require.Equal(t, "name", reference.ColumnReference.ColumnName)
				}
				require.NotSame(t, references[0].ColumnReference, references[1].ColumnReference)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), testCase.sql)

			testCase.check(t, analysis.ParameterReferences)
		})
	}
}
