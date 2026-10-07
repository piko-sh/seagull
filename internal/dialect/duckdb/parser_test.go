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
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
)

func TestCollectParenthesised(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		wantError    error
		name         string
		input        string
		wantValues   []string
		wantPosition int
		wantAnyError bool
	}{
		{name: "empty group", input: "() x", wantValues: nil, wantPosition: 2},
		{name: "nested groups", input: "(a, (b)) x", wantValues: []string{"a", ",", "(", "b", ")"}, wantPosition: 7},
		{name: "not a group", input: "a", wantAnyError: true},
		{name: "unbalanced group", input: "(a, (b)", wantError: fault.ErrParse},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.input)
			require.NoError(t, err)
			parser := newParser(tokens)

			inner, err := parser.collectParenthesised()

			switch {
			case testCase.wantError != nil:
				require.ErrorIs(t, err, testCase.wantError)
				return
			case testCase.wantAnyError:
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			var values []string
			for _, current := range inner {
				values = append(values, current.Value)
			}
			assert.Equal(t, testCase.wantValues, values)
			assert.Equal(t, testCase.wantPosition, parser.Position)
			assert.Equal(t, len(inner), cap(inner), "the view must not expose the tokens after the group")
		})
	}
}

func TestNamedPlaceholdersAreNumberedAfterTheNumberedOnes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		numbers map[string]int
	}{
		{
			name:    "a name written before an explicit $1",
			sql:     "SELECT id FROM users WHERE name = :x AND id = $1",
			numbers: map[string]int{"x": 2, "$1": 1},
		},
		{
			name:    "two names around an explicit $1",
			sql:     "SELECT id FROM users WHERE name = :x AND id = $1 AND name <> :y",
			numbers: map[string]int{"x": 2, "$1": 1, "y": 3},
		},
		{
			name:    "a name before a numbered question mark",
			sql:     "SELECT id FROM users WHERE name = :x AND id = ?2",
			numbers: map[string]int{"x": 3, "$2": 2},
		},
		{
			name:    "a name inside a subquery before the numbered placeholder",
			sql:     "SELECT id FROM users WHERE id IN (SELECT user_id FROM orders WHERE total = :total) AND id = $1",
			numbers: map[string]int{"total": 2, "$1": 1},
		},
		{
			name:    "names alone count from one",
			sql:     "SELECT id FROM users WHERE id = :id AND name = :name",
			numbers: map[string]int{"id": 1, "name": 2},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, buildTestCatalogue(), testCase.sql)
			numbers := map[string]int{}
			for _, reference := range analysis.ParameterReferences {
				key := reference.Name
				if key == "" {
					key = "$" + strconv.Itoa(reference.Number)
				}
				numbers[key] = reference.Number
			}
			require.Equal(t, testCase.numbers, numbers)
		})
	}
}

func TestParserDepthLimitPreventsStackOverflow(t *testing.T) {
	t.Parallel()

	const depth = 100_000
	engine := New()

	t.Run("nested expression parentheses", func(t *testing.T) {
		t.Parallel()
		sql := "SELECT " + strings.Repeat("(", depth) + "1" + strings.Repeat(")", depth) + " FROM t"
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.NotEmpty(t, statements)

		_, _ = engine.AnalyseQuery(t.Context(), nil, statements[0])
	})

	t.Run("nested compound column type", func(t *testing.T) {
		t.Parallel()
		sql := "CREATE TABLE t (c " + strings.Repeat("STRUCT(a ", depth) + "INTEGER" + strings.Repeat(")", depth) + ")"
		statements, err := engine.ParseStatements(t.Context(), sql)
		require.NoError(t, err)
		require.NotEmpty(t, statements)
		_, _ = engine.ApplyDDL(t.Context(), statements[0])
	})
}

func TestParserDepthLimitIsConfigurable(t *testing.T) {
	t.Parallel()

	engine := New(WithMaxParseDepth(8))
	sql := "SELECT " + strings.Repeat("(", 64) + "1" + strings.Repeat(")", 64) + " FROM t"
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.NotEmpty(t, statements)
	_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.ErrorIs(t, err, dialectkit.ErrParseDepth)
}
