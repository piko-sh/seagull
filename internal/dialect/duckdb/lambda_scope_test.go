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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestAParameterComparedWithALambdaParameterNamesNoColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		statement string
		column    string
		table     string
	}{
		{name: "a lambda parameter named like a column", statement: "SELECT id FROM t WHERE len(list_filter(l, x -> x > $1)) > 0"},
		{name: "a column outside any lambda", statement: "SELECT id FROM t WHERE x > $1", column: "x"},
		{name: "a column the lambda does not bind", statement: "SELECT id FROM t WHERE len(list_filter(l, y -> x > $1)) > 0", column: "x"},
		{name: "a parenthesised parameter list", statement: "SELECT id FROM t WHERE len(list_filter(l, (x, i) -> x > $1)) > 0"},
		{name: "the index parameter", statement: "SELECT id FROM t WHERE len(list_filter(l, (y, x) -> x > $1)) > 0"},
		{name: "a keyword lambda", statement: "SELECT id FROM t WHERE len(list_filter(l, lambda x: x > $1)) > 0"},
		{name: "a field of a lambda parameter", statement: "SELECT id FROM t AS x WHERE len(list_filter(s, x -> x.name = $1)) > 0"},
		{name: "a field of a table", statement: "SELECT id FROM t AS x WHERE len(list_filter(s, y -> x.name = $1)) > 0", column: "name", table: "x"},
		{name: "an outer lambda's parameter", statement: "SELECT id FROM t WHERE len(list_transform(ll, x -> list_filter(l, y -> x > $1))) > 0"},
		{name: "an inner lambda's parameter", statement: "SELECT id FROM t WHERE len(list_transform(ll, y -> list_filter(y, x -> x > $1))) > 0"},
		{name: "a list comprehension's variable", statement: "SELECT id FROM t WHERE len([x FOR x IN l IF x > $1]) > 0"},
		{name: "a column after the lambda's call", statement: "SELECT id FROM t WHERE len(list_filter(l, x -> x > 0)) > 0 AND x = $1", column: "x"},
		{name: "a column in the argument after the lambda", statement: "SELECT id FROM t WHERE list_reduce(l, (x, y) -> x + y, x = $1)", column: "x"},
		{name: "a column of a lambda in a HAVING condition", statement: "SELECT id FROM t GROUP BY id HAVING len(list_filter(any_value(l), x -> x > $1)) > 0"},
		{name: "a column after a JSON arrow", statement: "SELECT id FROM t WHERE j -> 'k' = '1' AND j = $1", column: "j"},
		{name: "a lambda parameter in an UPDATE's SET value", statement: "UPDATE t SET l = list_filter(l, x -> x <> $1)"},
		{name: "a parameter in a lambda's body in an UPDATE's SET value", statement: "UPDATE t SET l = list_transform(l, x -> x + $1)"},
		{name: "a parameter assigned in an UPDATE's SET value", statement: "UPDATE t SET l = $1", column: "l", table: "t"},
		{name: "a column in an UPDATE's SET value", statement: "UPDATE t SET l = CASE WHEN x > $1 THEN l END", column: "x", table: "t"},
		{name: "a column in a list comprehension's list", statement: "SELECT id FROM t WHERE len([y FOR x IN list_filter(l, z -> x > $1)]) > 0",
			column: "x"},
		{name: "a column after a list comprehension", statement: "SELECT id FROM t WHERE len([x FOR x IN l]) > 0 AND x = $1", column: "x"},
		{name: "a parameter in a list comprehension's condition", statement: "SELECT id FROM t WHERE len([y FOR x IN l IF y > 0 AND x = $1]) > 0"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.statement)

			require.Len(t, analysis.ParameterReferences, 1)
			reference := analysis.ParameterReferences[0].ColumnReference
			if testCase.column == "" {
				require.Nil(t, reference)
				return
			}
			require.NotNil(t, reference)
			require.Equal(t, testCase.column, reference.ColumnName)
			require.Equal(t, testCase.table, reference.TableAlias)
		})
	}
}

func comparedParameters(format string, count int) string {
	conditions := make([]string, count)
	for index := range conditions {
		conditions[index] = fmt.Sprintf(format, index+1)
	}
	return strings.Join(conditions, " OR ")
}

func TestLambdaScopesCostLinearTimeInTheParameters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		statement func(count int) string
	}{
		{name: "a WHERE clause with no lambda", statement: func(count int) string {
			return "SELECT id FROM t WHERE " + comparedParameters("a = $%d", count)
		}},
		{name: "a WHERE clause with a lambda", statement: func(count int) string {
			return "SELECT id FROM t WHERE len(list_filter(l, x -> x > 0)) > 0 AND (" + comparedParameters("a = $%d", count) + ")"
		}},
		{name: "an UPDATE's CASE", statement: func(count int) string {
			return "UPDATE t SET a = CASE WHEN " + strings.ReplaceAll(comparedParameters("b = $%d", count), " OR ", " THEN 1 WHEN ") +
				" THEN 1 END"
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analyse := func(count int) func() {
				statement := testCase.statement(count)
				return func() {
					analysis, _ := analyseStatement(t, statement)
					require.Len(t, analysis.ParameterReferences, count)
				}
			}

			scaling.Linear(t, analyse(1_000), analyse(4_000))
		})
	}
}
