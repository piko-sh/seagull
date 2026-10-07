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

package lexical_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/clickhouse/lexical"
	"seagull.sh/seagull/internal/sqllex"
)

func splitTexts(t *testing.T, input string) []string {
	t.Helper()
	statements, err := sqllex.Split(lexical.Rules(), input)
	require.NoError(t, err)
	texts := make([]string, 0, len(statements))
	for _, statement := range statements {
		texts = append(texts, statement.Text)
	}
	return texts
}

func TestRulesSplit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "backslash escapes in strings and identifiers",
			input:    "SELECT 'a\\';b', \"c\\\";d\", `e\\`;f`; SELECT 2;",
			expected: []string{"SELECT 'a\\';b', \"c\\\";d\", `e\\`;f`", "SELECT 2"},
		},
		{
			name:     "hash comments",
			input:    "SELECT 1; # it's; a note\nSELECT 2;",
			expected: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name:     "typed placeholders",
			input:    "SELECT {id:UInt64}; SELECT 2;",
			expected: []string{"SELECT {id:UInt64}", "SELECT 2"},
		},
		{
			name:     "heredocs hold terminators and quotes",
			input:    "SELECT $$a;b$$; SELECT $tag$it's; $$ $tag$; SELECT 3;",
			expected: []string{"SELECT $$a;b$$", "SELECT $tag$it's; $$ $tag$", "SELECT 3"},
		},
		{
			name:     "every terminator splits",
			input:    "CREATE FUNCTION f AS (x) -> x BEGIN; END;",
			expected: []string{"CREATE FUNCTION f AS (x) -> x BEGIN", "END"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.expected, splitTexts(t, testCase.input))
		})
	}
}

func TestRulesReadEveryPlaceholderForm(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		input      string
		parameters []string
	}{
		{name: "typed placeholders", input: "SELECT id FROM t WHERE id = {id:UInt64}", parameters: []string{"{id:UInt64}"}},
		{name: "positional placeholders", input: "SELECT id FROM t WHERE id = $1 AND name = $12", parameters: []string{"$1", "$12"}},
		{name: "a dollar not followed by a digit", input: "SELECT 'cost $5', `$x` FROM t", parameters: nil},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			tokens, err := sqllex.Tokens(lexical.Rules(), testCase.input)
			require.NoError(t, err)
			var parameters []string
			for _, token := range tokens {
				if token.Kind == sqllex.KindParameter {
					parameters = append(parameters, token.Raw)
				}
			}
			require.Equal(t, testCase.parameters, parameters)
		})
	}
}

func TestRulesRequiresNoTransaction(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{name: "nothing needs to run outside a transaction", input: "OPTIMIZE TABLE t FINAL; VACUUM;", expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			required, err := sqllex.RequiresNoTransaction(lexical.Rules(), testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, required)
		})
	}
}

func TestRulesReadNoPlaceholderInsideAHeredoc(t *testing.T) {
	t.Parallel()

	tokens, err := sqllex.Tokens(lexical.Rules(), "SELECT $tag$ $1 {x:UInt8} $tag$, {id:UInt64}, $2")
	require.NoError(t, err)
	var parameters []string
	for _, token := range tokens {
		if token.Kind == sqllex.KindParameter {
			parameters = append(parameters, token.Raw)
		}
	}
	require.Equal(t, []string{"{id:UInt64}", "$2"}, parameters)
}
