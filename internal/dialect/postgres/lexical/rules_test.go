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

	"seagull.sh/seagull/internal/dialect/postgres/lexical"
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
			name:     "quoted identifiers hold terminators and quotes",
			input:    `CREATE TABLE "semi;colon" (id int); CREATE TABLE "o'brien" (id int);`,
			expected: []string{`CREATE TABLE "semi;colon" (id int)`, `CREATE TABLE "o'brien" (id int)`},
		},
		{
			name:     "escape strings honour backslashes",
			input:    `SELECT E'a\';b'; SELECT 'c\'; SELECT 2;`,
			expected: []string{`SELECT E'a\';b'`, `SELECT 'c\'`, "SELECT 2"},
		},
		{
			name:     "dollar-quoted bodies hold terminators",
			input:    "CREATE FUNCTION f() RETURNS int AS $body$ SELECT 1; $body$ LANGUAGE sql; SELECT 2;",
			expected: []string{"CREATE FUNCTION f() RETURNS int AS $body$ SELECT 1; $body$ LANGUAGE sql", "SELECT 2"},
		},
		{
			name:     "nested block comments",
			input:    "/* outer /* inner */ still; */ SELECT 1; SELECT 2;",
			expected: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name:     "begin as a column opens no block",
			input:    "CREATE TABLE t (begin timestamptz); ALTER TABLE shifts RENAME COLUMN begin TO starts_at; SELECT 1;",
			expected: []string{"CREATE TABLE t (begin timestamptz)", "ALTER TABLE shifts RENAME COLUMN begin TO starts_at", "SELECT 1"},
		},
		{
			name: "begin atomic bodies hold terminators",
			input: "CREATE OR REPLACE FUNCTION f() RETURNS int LANGUAGE sql BEGIN ATOMIC SELECT CASE WHEN true THEN 1 END; END; " +
				"SELECT 2;",
			expected: []string{
				"CREATE OR REPLACE FUNCTION f() RETURNS int LANGUAGE sql BEGIN ATOMIC SELECT CASE WHEN true THEN 1 END; END",
				"SELECT 2",
			},
		},
		{
			name:     "parentheses hold terminators",
			input:    "CREATE RULE r AS ON INSERT TO t DO ALSO (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2)); SELECT 1;",
			expected: []string{"CREATE RULE r AS ON INSERT TO t DO ALSO (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2))", "SELECT 1"},
		},
		{
			name:     "a dollar inside an identifier opens no dollar quote",
			input:    "CREATE TABLE prices (amount$gbp$net numeric); SELECT 1;",
			expected: []string{"CREATE TABLE prices (amount$gbp$net numeric)", "SELECT 1"},
		},
		{
			name:     "parameters and casts",
			input:    "SELECT $1::int, :name; SELECT 2;",
			expected: []string{"SELECT $1::int, :name", "SELECT 2"},
		},
		{
			name:     "a hash is not a comment",
			input:    "SELECT data #> '{a}'; SELECT 2;",
			expected: []string{"SELECT data #> '{a}'", "SELECT 2"},
		},
		{
			name:     "a DO statement opens a block",
			input:    "DO BEGIN PERFORM 1; END; SELECT 2;",
			expected: []string{"DO BEGIN PERFORM 1; END", "SELECT 2"},
		},
		{
			name:     "begin after punctuation or a bare create opens no block",
			input:    "(begin); CREATE OR REPLACE begin; SELECT 1 AS begin;",
			expected: []string{"(begin)", "CREATE OR REPLACE begin", "SELECT 1 AS begin"},
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
		{name: "create index concurrently", input: "CREATE INDEX CONCURRENTLY i ON t (c);", expected: true},
		{name: "drop index concurrently", input: "SELECT 1; DROP INDEX concurrently i;", expected: true},
		{name: "leading vacuum", input: "VACUUM ANALYZE t;", expected: true},
		{name: "vacuum later in a statement", input: "SELECT vacuum FROM t;", expected: false},
		{name: "a literal mention", input: "INSERT INTO notes VALUES ('CONCURRENTLY');", expected: false},
		{name: "a quoted identifier", input: `CREATE TABLE "concurrently" (id int);`, expected: false},
		{name: "a dollar-quoted body", input: "DO $$ BEGIN EXECUTE 'VACUUM'; END $$;", expected: false},
		{name: "an identifier containing the word", input: "CREATE TABLE concurrently_built (id int);", expected: false},
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

func TestRulesTokens(t *testing.T) {
	t.Parallel()

	tokens := sqllex.SignificantTokens(lexical.Rules(), `SELECT $1, :name, x::int, B'101', "a""b" FROM t`)
	kinds := make([]sqllex.Kind, 0, len(tokens))
	for _, token := range tokens {
		kinds = append(kinds, token.Kind)
	}
	require.Equal(t, []sqllex.Kind{
		sqllex.KindWord, sqllex.KindParameter, sqllex.KindPunctuation, sqllex.KindParameter, sqllex.KindPunctuation,
		sqllex.KindWord, sqllex.KindPunctuation, sqllex.KindWord, sqllex.KindPunctuation, sqllex.KindString,
		sqllex.KindPunctuation, sqllex.KindQuotedIdentifier, sqllex.KindWord, sqllex.KindWord,
	}, kinds)
}

func TestRulesReadANamedParameterOnlyWhereAnOperandIsExpected(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{name: "an array slice between two names", input: "SELECT tags[lo:hi] FROM t WHERE id=:id", expected: []string{":id"}},
		{name: "an array slice after a closing bracket or quote", input: `SELECT (tags)[lo:hi], "tags"[lo:hi] FROM t`, expected: nil},
		{name: "an open upper bound", input: "SELECT tags[lo:] FROM t", expected: nil},
		{name: "a cast stays a cast", input: "SELECT x::text, :a::int FROM t", expected: []string{":a"}},
		{name: "an omitted lower bound reads as a parameter", input: "SELECT tags[:hi] FROM t", expected: []string{":hi"}},
		{
			name:     "parameters after brackets, commas and operators",
			input:    "SELECT f(:a, :b) FROM t WHERE x IN (:c) AND y = ANY(ARRAY[:d]) AND z>:e AND w = :f",
			expected: []string{":a", ":b", ":c", ":d", ":e", ":f"},
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
