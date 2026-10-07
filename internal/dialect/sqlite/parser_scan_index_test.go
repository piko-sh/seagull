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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParenthesisGroupsFindTheGroupOpenAtEachPosition(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantOpens   []int
		wantCommas  []int
		positionsOf string
	}{
		{
			name:        "a call holding a nested call",
			sql:         "f(a, g(b, c), d)",
			positionsOf: "f ( a , g ( b , c ) , d ) end",
			wantOpens:   []int{-1, -1, 1, 1, 1, 1, 5, 5, 5, 5, 1, 1, 1, -1},
			wantCommas:  []int{0, 0, 0, 0, 1, 1, 0, 0, 1, 1, 1, 2, 2, 0},
		},
		{
			name:        "a closing parenthesis that closes nothing",
			sql:         "a ) ( b",
			positionsOf: "a ) ( b end",
			wantOpens:   []int{-1, -1, -1, 2, 2},
			wantCommas:  []int{0, 0, 0, 0, 0},
		},
		{
			name:        "commas outside every group",
			sql:         "a, b",
			positionsOf: "a , b end",
			wantOpens:   []int{-1, -1, -1, -1},
			wantCommas:  []int{0, 0, 0, 0},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens := statementTokensOf(t, testCase.sql)
			groups := newParenthesisGroups(tokens)

			require.Len(t, testCase.wantOpens, len(tokens)+1)
			for position := range len(tokens) + 1 {
				assert.Equal(t, testCase.wantOpens[position], groups.opening(position), "opening at %d", position)
				assert.Equal(t, testCase.wantCommas[position], groups.commaCount(position), "comma count at %d", position)
			}
			assert.Equal(t, -1, groups.opening(-1))
			assert.Equal(t, -1, groups.opening(len(tokens)+1))
			assert.Zero(t, groups.commaCount(len(tokens)+1))
		})
	}
}

func TestCastTypeStopFindsTheNextClosingParenthesisOrAS(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		position int
		want     int
	}{
		{name: "an AS after the placeholder", sql: "CAST(? + ? AS TEXT)", position: 2, want: 5},
		{name: "a closing parenthesis before any AS", sql: "CAST(? + (1) AS TEXT)", position: 2, want: 6},
		{name: "nothing after the last token", sql: "CAST(? AS TEXT)", position: 5, want: -1},
		{name: "a position out of range", sql: "CAST(? AS TEXT)", position: 99, want: -1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			p := newParser(statementTokensOf(t, testCase.sql))

			assert.Equal(t, testCase.want, p.castTypeStop(testCase.position))
		})
	}
}

func TestLikeOperatorColumnGivesEachCallerItsOwnCopy(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		likePosition int
		wantColumn   string
	}{
		{name: "a column before the operator", sql: "name LIKE ?", likePosition: 1, wantColumn: "name"},
		{name: "a column inside an expression before the operator", sql: "(1 + 1 || email) LIKE ?", likePosition: 7, wantColumn: "email"},
		{name: "no column before the operator", sql: "(1 || 2) LIKE ?", likePosition: 5, wantColumn: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			p := newParser(statementTokensOf(t, testCase.sql))

			first := p.likeOperatorColumn(testCase.likePosition)
			second := p.likeOperatorColumn(testCase.likePosition)

			if testCase.wantColumn == "" {
				assert.Nil(t, first)
				assert.Nil(t, second)
				return
			}
			require.NotNil(t, first)
			assert.Equal(t, testCase.wantColumn, first.ColumnName)
			assert.Equal(t, first, second)
			assert.NotSame(t, first, second)
		})
	}
}

func statementTokensOf(t *testing.T, sql string) []token {
	t.Helper()

	tokens, err := tokenise(sql)
	require.NoError(t, err)
	return tokens[:len(tokens)-1]
}
