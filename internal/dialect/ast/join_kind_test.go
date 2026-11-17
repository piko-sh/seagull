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

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestJoinKindsDescribeTheirEffect(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		kind          ast.JoinKind
		leftNullable  bool
		rightNullable bool
		filters       bool
	}{
		{name: "inner", kind: ast.JoinInner, leftNullable: false, rightNullable: false, filters: true},
		{name: "left", kind: ast.JoinLeft, leftNullable: false, rightNullable: true, filters: false},
		{name: "right", kind: ast.JoinRight, leftNullable: true, rightNullable: false, filters: false},
		{name: "full", kind: ast.JoinFull, leftNullable: true, rightNullable: true, filters: false},
		{name: "cross", kind: ast.JoinCross, leftNullable: false, rightNullable: false, filters: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.leftNullable, testCase.kind.Nullable.Includes(ast.JoinSideLeft))
			require.Equal(t, testCase.rightNullable, testCase.kind.Nullable.Includes(ast.JoinSideRight))
			require.Equal(t, testCase.filters, testCase.kind.Filters)
		})
	}
}

func TestJoinKindStringIsItsKeyword(t *testing.T) {
	t.Parallel()

	require.Equal(t, "LEFT", ast.JoinLeft.String())
	require.Equal(t, "ASOF LEFT", ast.JoinKind{Keyword: "ASOF LEFT", Nullable: ast.JoinSideRight, Filters: false}.String())
}

func TestLeavesRightMissingForJoinsThatKeepUnmatchedLeftRows(t *testing.T) {
	t.Parallel()

	require.True(t, ast.JoinLeft.LeavesRightMissing())
	require.True(t, ast.JoinFull.LeavesRightMissing())
	require.False(t, ast.JoinInner.LeavesRightMissing())
	require.False(t, ast.JoinRight.LeavesRightMissing())
	require.False(t, ast.JoinCross.LeavesRightMissing())
}

func TestEncloseGroupRelabelsOnlyUnderAJoinThatLeavesTheGroupMissing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		outer ast.JoinKind
		want  ast.JoinKind
	}{
		{name: "a left join relabels the group", outer: ast.JoinLeft, want: ast.JoinLeft},
		{name: "a full join relabels the group", outer: ast.JoinFull, want: ast.JoinFull},
		{name: "an inner join keeps the group's joins", outer: ast.JoinInner, want: ast.JoinInner},
		{name: "a right join keeps the group's joins", outer: ast.JoinRight, want: ast.JoinInner},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			joins := []ast.JoinClause{{Kind: ast.JoinInner}}
			derived := []ast.RawDerivedTableReference{{JoinKind: ast.JoinInner}}
			functions := []ast.RawTableValuedFunctionReference{{JoinKind: ast.JoinInner}}

			ast.EncloseGroup(testCase.outer, joins, derived, functions)

			require.Equal(t, testCase.want, joins[0].Kind)
			require.Equal(t, testCase.want, derived[0].JoinKind)
			require.Equal(t, testCase.want, functions[0].JoinKind)
		})
	}
}
