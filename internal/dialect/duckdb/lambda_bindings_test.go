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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func describeBinding(binding ast.Expression) string {
	switch typed := binding.(type) {
	case nil:
		return "unbound"
	case *ast.LiteralExpression:
		return "literal " + typed.TypeName
	case *ast.ArraySubscriptExpression:
		return "element of " + describeBinding(typed.Array)
	case *ast.ColumnRefExpression:
		return "column " + typed.ColumnName
	case *ast.CoalesceExpression:
		described := "common type of"
		for _, argument := range typed.Arguments {
			described += " (" + describeBinding(argument) + ")"
		}
		return described
	default:
		return "other"
	}
}

func TestListFunctionsBindTheParametersTheyDecide(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "a transform's second parameter is the element's position",
			sql:  "SELECT list_transform(tags, (tag, position) -> position) AS a FROM users",
			want: []string{"unbound", "literal bigint"},
		},
		{
			name: "a filter's second parameter is the element's position",
			sql:  "SELECT array_filter(tags, (tag, position) -> position > 1) AS a FROM users",
			want: []string{"unbound", "literal bigint"},
		},
		{
			name: "a reduce's accumulator is an element and its third parameter the position",
			sql:  "SELECT list_reduce(tags, (total, tag, position) -> total) AS a FROM users",
			want: []string{"element of column tags", "unbound", "literal bigint"},
		},
		{
			name: "a reduce from a starting value accumulates in the common type",
			sql:  "SELECT reduce(scores, (total, score) -> total + score, 0) AS a FROM users",
			want: []string{"common type of (element of column scores) (literal integer)", "unbound", "literal bigint"},
		},
		{
			name: "a function of a schema of the user's binds nothing",
			sql:  "SELECT main.apply(tags, (tag, position) -> position) AS a FROM users",
			want: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nil, testCase.sql)
			call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
			require.True(t, isCall)
			lambda, isLambda := call.Arguments[1].(*ast.LambdaExpression)
			require.True(t, isLambda)

			var bindings []string
			for _, binding := range lambda.Bindings {
				bindings = append(bindings, describeBinding(binding))
			}
			require.Equal(t, testCase.want, bindings)
		})
	}
}
