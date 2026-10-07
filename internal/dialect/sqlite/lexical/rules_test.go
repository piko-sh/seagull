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

	"seagull.sh/seagull/internal/dialect/sqlite/lexical"
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
			name: "trigger bodies hold terminators",
			input: "CREATE TEMP TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET c = CASE WHEN new.x THEN 1 END; " +
				"DELETE FROM d; END; SELECT 1;",
			expected: []string{
				"CREATE TEMP TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET c = CASE WHEN new.x THEN 1 END; DELETE FROM d; END",
				"SELECT 1",
			},
		},
		{
			name:     "bracket, backtick and double-quoted identifiers",
			input:    "CREATE TABLE [a;b] (`c;d` INT, \"e;f\" TEXT); SELECT 1;",
			expected: []string{"CREATE TABLE [a;b] (`c;d` INT, \"e;f\" TEXT)", "SELECT 1"},
		},
		{
			name:     "named parameters",
			input:    "SELECT :a, @b, $c, ?1, ?; SELECT 2;",
			expected: []string{"SELECT :a, @b, $c, ?1, ?", "SELECT 2"},
		},
		{
			name:     "begin opens no block outside a trigger",
			input:    "CREATE TABLE t (begin INTEGER); BEGIN; COMMIT;",
			expected: []string{"CREATE TABLE t (begin INTEGER)", "BEGIN", "COMMIT"},
		},
		{
			name:     "block comments do not nest",
			input:    "/* a /* b */ SELECT 1; SELECT 2;",
			expected: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name:     "begin after punctuation or a bare create opens no block",
			input:    "(begin); CREATE TEMPORARY begin; SELECT 1;",
			expected: []string{"(begin)", "CREATE TEMPORARY begin", "SELECT 1"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.expected, splitTexts(t, testCase.input))
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
		{name: "leading vacuum", input: "VACUUM;", expected: true},
		{name: "concurrently is not sqlite syntax", input: "CREATE INDEX CONCURRENTLY i ON t (c);", expected: false},
		{name: "a literal mention", input: "SELECT 'VACUUM';", expected: false},
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

func TestRulesReadANamedParameterWhereverItIsWritten(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{name: "after a name, as SQLite reads it", input: "SELECT a:b FROM t", expected: []string{":b"}},
		{name: "a dollar sign inside a name, as SQLite reads it", input: "SELECT price$usd, :a$b FROM t", expected: []string{":a$b"}},
		{
			name:     "every sigil after brackets, commas and operators",
			input:    "SELECT id FROM t WHERE id=:id AND x IN (@a,$b) AND y = (:c) AND z>:d",
			expected: []string{":id", "@a", "$b", ":c", ":d"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.expected, parameterTexts(t, testCase.input))
		})
	}
}

func parameterTexts(t *testing.T, input string) []string {
	t.Helper()

	tokens, err := sqllex.Tokens(lexical.Rules(), input)
	require.NoError(t, err)
	var parameters []string
	for _, token := range tokens {
		if token.Kind == sqllex.KindParameter {
			parameters = append(parameters, token.Raw)
		}
	}
	return parameters
}

func TestRulesContinueAWordThroughADollarSign(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{name: "a column named with a dollar sign", input: "SELECT price$usd FROM t", expected: []string{"SELECT", "price$usd", "FROM", "t"}},
		{name: "a name ending in a dollar sign", input: "SELECT total$ FROM t", expected: []string{"SELECT", "total$", "FROM", "t"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := sqllex.Tokens(lexical.Rules(), testCase.input)
			require.NoError(t, err)
			var words []string
			for _, token := range tokens {
				if token.Kind == sqllex.KindWord {
					words = append(words, token.Raw)
				}
			}
			require.Equal(t, testCase.expected, words)
		})
	}
}
