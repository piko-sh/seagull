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
		{name: "subquery with FROM and a parameter", sql: "UPDATE users SET (name, email) = (SELECT name, email FROM users WHERE id = $2) WHERE id = $1", parameters: 2, subqueries: 1},
		{name: "subquery with FROM and LIMIT", sql: "UPDATE users SET (name, email) = (SELECT name, email FROM users LIMIT 1)", parameters: 0, subqueries: 1},
		{name: "one column from a subquery", sql: "UPDATE users SET (name) = (SELECT name FROM users LIMIT 1)", parameters: 0, subqueries: 1},
		{name: "subquery without FROM", sql: "UPDATE users SET (name, email) = (SELECT 'x', 'y') WHERE id = $1", parameters: 1, subqueries: 1},
		{name: "a single column from a subquery", sql: "UPDATE users SET name = (SELECT name FROM orders WHERE orders.id = $2) WHERE id = $1", parameters: 2, subqueries: 1},
		{name: "a list of values", sql: "UPDATE users SET (name, email) = ($2, $3) WHERE id = $1", parameters: 3},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)
			var analysis *ast.RawQueryAnalysis
			scaling.Within(t, time.Second, func(ctx context.Context) {
				var analyseErr error
				analysis, analyseErr = engine.AnalyseQuery(ctx, buildTestCatalogue(), statements[0])
				require.NoError(t, analyseErr)
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

func TestABracketedValueInAValuesRowLeavesLaterParametersTheirColumns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantColumns []string
	}{
		{name: "a list", sql: "INSERT INTO users (id, email, name, active) VALUES ($1, ['a', 'b'][1], $2, $3)",
			wantColumns: []string{"id", "name", "active"}},
		{name: "a struct", sql: "INSERT INTO users (id, email, name, active) VALUES ($1, {'a': 'x', 'b': 'y'}.a, $2, $3)",
			wantColumns: []string{"id", "name", "active"}},
		{name: "a map", sql: "INSERT INTO users (id, email, name, active) VALUES ($1, MAP {'a': 'x', 'b': 'y'}['a'], $2, $3)",
			wantColumns: []string{"id", "name", "active"}},
		{name: "a nested list", sql: "INSERT INTO users (id, email, name, active) VALUES ($1, [['a', 'b'], ['c']][1][2], $2, $3)",
			wantColumns: []string{"id", "name", "active"}},
		{name: "parameters inside the list", sql: "INSERT INTO users (id, email, name, active) VALUES ($1, [$2, 'b'][1], $3, $4)",
			wantColumns: []string{"id", "email", "name", "active"}},
		{name: "a second row", sql: "INSERT INTO users (id, email, name) VALUES ($1, ['a', 'b'][1], $2), ($3, {'a': 'c', 'b': 'd'}.b, $4)",
			wantColumns: []string{"id", "name", "id", "name"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)

			columns := make([]string, 0, len(analysis.ParameterReferences))
			for _, reference := range analysis.ParameterReferences {
				require.NotNil(t, reference.ColumnReference)
				columns = append(columns, reference.ColumnReference.ColumnName)
			}
			require.Equal(t, testCase.wantColumns, columns)
		})
	}
}

func TestABracketedValueInAnAssignmentLeavesLaterParametersTheirColumns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantColumns []string
	}{
		{name: "a row assignment", sql: "UPDATE users SET (email, name, active) = (['a', 'b'][1], $1, $2) WHERE id = $3",
			wantColumns: []string{"name", "active", "id"}},
		{name: "parameters inside a row assignment's struct", sql: "UPDATE users SET (email, name) = ({'a': $1, 'b': 'x'}.a, $2) WHERE id = $3",
			wantColumns: []string{"", "name", "id"}},
		{name: "parameters inside an assignment's list", sql: "UPDATE users SET email = [$1, $2][1], name = $3 WHERE id = $4",
			wantColumns: []string{"email", "email", "name", "id"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)

			columns := make([]string, 0, len(analysis.ParameterReferences))
			for _, reference := range analysis.ParameterReferences {
				column := ""
				if reference.ColumnReference != nil {
					column = reference.ColumnReference.ColumnName
				}
				columns = append(columns, column)
			}
			require.Equal(t, testCase.wantColumns, columns)
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
		{name: "the subquery is the value", sql: "INSERT INTO posts (id, user_id, title) VALUES ($1, (SELECT id FROM users WHERE email = $2), $3)", wantNumber: 2},
		{name: "the subquery is inside the value", sql: "INSERT INTO posts (id, user_id, title) VALUES ($1, (COALESCE((SELECT id FROM users WHERE email = $2), 0)), $3)", wantNumber: 2},
		{name: "the subquery is an argument of a call", sql: "INSERT INTO posts (id, user_id, title) VALUES ($1, COALESCE((SELECT id FROM users WHERE email = $2), 0), $3)", wantNumber: 2},
		{name: "the subquery is a CASE result", sql: "INSERT INTO posts (id, user_id, title) VALUES ($1, CASE WHEN $3 = '' THEN (SELECT id FROM users WHERE email = $2) END, $3)", wantNumber: 2},
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
