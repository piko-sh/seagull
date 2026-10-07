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

	"seagull.sh/seagull/internal/dialect/duckdb/lexical"
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
			name:     "dollar-quoted bodies hold terminators",
			input:    "SELECT $$a;b$$; SELECT $tag$c;d$tag$;",
			expected: []string{"SELECT $$a;b$$", "SELECT $tag$c;d$tag$"},
		},
		{
			name:     "escape strings hold escaped quotes and terminators",
			input:    `SELECT E'it\'s; here'; SELECT e'a\\'; SELECT 2;`,
			expected: []string{`SELECT E'it\'s; here'`, `SELECT e'a\\'`, "SELECT 2"},
		},
		{
			name:     "nested block comments",
			input:    "/* a /* b */ c; */ SELECT 1;",
			expected: []string{"SELECT 1"},
		},
		{
			name:     "macro definitions",
			input:    "CREATE OR REPLACE MACRO m(a) AS a + 1; SELECT 2;",
			expected: []string{"CREATE OR REPLACE MACRO m(a) AS a + 1", "SELECT 2"},
		},
		{
			name:     "parameters and casts",
			input:    "SELECT $1::INTEGER, :name; SELECT 2;",
			expected: []string{"SELECT $1::INTEGER, :name", "SELECT 2"},
		},
		{
			name:     "begin as a column opens no block",
			input:    "CREATE TABLE t (begin INTEGER); SELECT 1;",
			expected: []string{"CREATE TABLE t (begin INTEGER)", "SELECT 1"},
		},
		{
			name:     "a macro or function body opens a block, as in the dialect's splitter",
			input:    "CREATE TEMP MACRO m() AS BEGIN SELECT 1; END; CREATE FUNCTION f() AS BEGIN SELECT 2; END; SELECT 3;",
			expected: []string{"CREATE TEMP MACRO m() AS BEGIN SELECT 1; END", "CREATE FUNCTION f() AS BEGIN SELECT 2; END", "SELECT 3"},
		},
		{
			name:     "begin after punctuation or a bare create opens no block",
			input:    "(begin); CREATE OR REPLACE begin; SELECT 1;",
			expected: []string{"(begin)", "CREATE OR REPLACE begin", "SELECT 1"},
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
		{name: "an ordinary statement", input: "CREATE TABLE t (id INTEGER);", expected: false},
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

func TestRulesReadEveryPlaceholderForm(t *testing.T) {
	t.Parallel()

	tokens, err := sqllex.Tokens(lexical.Rules(), "SELECT $1, ?, ?3, :name, $label, $tag$ $quoted $tag$")
	require.NoError(t, err)

	var parameters []string
	for _, token := range tokens {
		if token.Kind == sqllex.KindParameter {
			parameters = append(parameters, token.Raw)
		}
	}
	require.Equal(t, []string{"$1", "?", "?3", ":name", "$label"}, parameters)
}

func TestRulesReadAColonAfterAnOperandAsPunctuation(t *testing.T) {
	t.Parallel()

	tokens, err := sqllex.Tokens(lexical.Rules(), "SELECT list_transform(tags, lambda tag:upper(tag)), {'k':v}, list[a:b] WHERE id=:id")
	require.NoError(t, err)
	var parameters []string
	for _, token := range tokens {
		if token.Kind == sqllex.KindParameter {
			parameters = append(parameters, token.Raw)
		}
	}
	require.Equal(t, []string{":id"}, parameters)
}
