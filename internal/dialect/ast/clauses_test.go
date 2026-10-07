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

package ast_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestClauseIncludesEveryClauseOfAnotherSet(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		set   ast.Clause
		other ast.Clause
		want  bool
	}{
		{name: "a clause in a set holding it", set: ast.ClauseWhere | ast.ClauseOrderBy, other: ast.ClauseOrderBy, want: true},
		{name: "a clause the set lacks", set: ast.ClauseWhere, other: ast.ClauseHaving, want: false},
		{name: "several clauses all held", set: ast.ClauseWhere | ast.ClauseGroupBy | ast.ClauseHaving,
			other: ast.ClauseWhere | ast.ClauseHaving, want: true},
		{name: "several clauses one of them missing", set: ast.ClauseWhere | ast.ClauseGroupBy,
			other: ast.ClauseWhere | ast.ClauseHaving, want: false},
		{name: "the empty set in a full set", set: ast.ClauseWhere | ast.ClauseSelectList, other: 0, want: false},
		{name: "the empty set in itself", set: 0, other: 0, want: false},
		{name: "a clause in the empty set", set: 0, other: ast.ClauseWhere, want: false},
		{name: "a set in itself", set: ast.ClauseSetTarget | ast.ClauseSetValue, other: ast.ClauseSetTarget | ast.ClauseSetValue,
			want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.set.Includes(testCase.other))
		})
	}
}

func TestClauseStringListsTheClausesInAFixedOrder(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		clause ast.Clause
		want   string
	}{
		{name: "the empty set", clause: 0, want: "none"},
		{name: "WHERE", clause: ast.ClauseWhere, want: "WHERE"},
		{name: "a join condition", clause: ast.ClauseJoinCondition, want: "JOIN ON"},
		{name: "row shaping", clause: ast.ClauseRowShaping, want: "row shaping"},
		{name: "a VALUES row value", clause: ast.ClauseInsertValue, want: "VALUES row value"},
		{name: "two clauses written out of order", clause: ast.ClauseOrderBy | ast.ClauseGroupBy, want: "GROUP BY, ORDER BY"},
		{name: "the select list before every other clause", clause: ast.ClauseHaving | ast.ClauseSelectList | ast.ClauseWhere,
			want: "SELECT list, WHERE, HAVING"},
		{name: "both halves of a SET clause", clause: ast.ClauseSetValue | ast.ClauseSetTarget, want: "SET target, SET value"},
		{name: "a bit no clause names", clause: ast.Clause(1 << 15), want: "none"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.clause.String())
		})
	}
}
