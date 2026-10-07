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

package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
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

			analysis := analyseQuery(t, testCase.sql)

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

func TestParseInsertSourceRefusesAParenthesisThatOpensNoQuery(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a parenthesised value", sql: "INSERT INTO t (a) (1)"},
		{name: "a parenthesised parameter", sql: "INSERT INTO t (a) (?)"},
		{name: "an empty pair of parentheses", sql: "INSERT INTO t (a) ()"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.sql)

			require.Nil(t, analysis)
			requireParseErrorAt(t, err, "(")
			require.ErrorContains(t, err, "expected VALUES, SET or a query giving the rows to insert")
		})
	}
}
