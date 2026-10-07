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

	"seagull.sh/seagull/internal/dialect/mysql/lexical"
	"seagull.sh/seagull/internal/sqllex"
)

func splitTexts(t *testing.T, input string) []string {
	t.Helper()
	return splitWith(t, lexical.Rules(), input)
}

func TestRulesSplit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "double-quoted strings hold terminators and escaped quotes",
			input:    `INSERT INTO t VALUES ("a;b", "say \"hi\"; ok"); SELECT 1;`,
			expected: []string{`INSERT INTO t VALUES ("a;b", "say \"hi\"; ok")`, "SELECT 1"},
		},
		{
			name:     "single-quoted strings honour backslashes",
			input:    `INSERT INTO t VALUES ('it\'s; fine'); SELECT 1;`,
			expected: []string{`INSERT INTO t VALUES ('it\'s; fine')`, "SELECT 1"},
		},
		{
			name:     "backtick identifiers hold terminators",
			input:    "CREATE TABLE `a;b` (id INT); SELECT 1;",
			expected: []string{"CREATE TABLE `a;b` (id INT)", "SELECT 1"},
		},
		{
			name:     "hash comments hide apostrophes and terminators",
			input:    "INSERT INTO t VALUES (1); # don't run this twice; ever\nINSERT INTO t VALUES (2);",
			expected: []string{"INSERT INTO t VALUES (1)", "INSERT INTO t VALUES (2)"},
		},
		{
			name:     "double dash needs whitespace",
			input:    "SELECT 1--1; -- note; here\nSELECT 2;",
			expected: []string{"SELECT 1--1", "SELECT 2"},
		},
		{
			name:  "delimiter lines",
			input: "DELIMITER //\nCREATE FUNCTION count_logs() RETURNS INT READS SQL DATA\nBEGIN\n  DECLARE total INT;\n  RETURN total;\nEND //\nDELIMITER ;\nSELECT 1;",
			expected: []string{
				"CREATE FUNCTION count_logs() RETURNS INT READS SQL DATA\nBEGIN\n  DECLARE total INT;\n  RETURN total;\nEND",
				"SELECT 1",
			},
		},
		{
			name:     "a dollar delimiter right after END",
			input:    "DELIMITER $$\nCREATE PROCEDURE p() BEGIN SELECT 1; END$$\nDELIMITER ;\nSELECT 2;",
			expected: []string{"CREATE PROCEDURE p() BEGIN SELECT 1; END", "SELECT 2"},
		},
		{
			name: "a procedure body without DELIMITER",
			input: "CREATE DEFINER = 'app'@'%' PROCEDURE p() BEGIN IF x THEN SELECT 1; END IF; " +
				"SET y = CASE WHEN z THEN 1 END; END; SELECT 2;",
			expected: []string{
				"CREATE DEFINER = 'app'@'%' PROCEDURE p() BEGIN IF x THEN SELECT 1; END IF; SET y = CASE WHEN z THEN 1 END; END",
				"SELECT 2",
			},
		},
		{
			name:     "begin as a column opens no block",
			input:    "CREATE TABLE t (begin INT); SELECT 1 AS begin;",
			expected: []string{"CREATE TABLE t (begin INT)", "SELECT 1 AS begin"},
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
		{name: "nothing needs to run outside a transaction", input: "CREATE INDEX CONCURRENTLY i ON t (c); VACUUM;", expected: false},
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

func TestRulesReadANamedParameterOnlyWhereAnOperandIsExpected(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{name: "a statement label", input: "BEGIN l1:LOOP LEAVE l1; END LOOP l1; END", expected: nil},
		{name: "an assignment to a user variable", input: "SELECT @total:=1", expected: nil},
		{
			name:     "parameters after brackets, commas and operators",
			input:    "SELECT id FROM t WHERE id=:id AND x IN (:a,:b) AND y = (:c) AND z>:d",
			expected: []string{":id", ":a", ":b", ":c", ":d"},
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

func TestDoubleQuotedTextFollowsTheSQLMode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		rules     sqllex.Rules
		kind      sqllex.Kind
		backslash string
		split     []string
	}{
		{name: "a string in the default mode", rules: lexical.Rules(), kind: sqllex.KindString,
			backslash: `SELECT "a\""; SELECT 2;`, split: []string{`SELECT "a\""`, "SELECT 2"}},
		{name: "an identifier under ANSI_QUOTES, in which a backslash escapes nothing", rules: lexical.ANSIQuotesRules(),
			kind: sqllex.KindQuotedIdentifier, backslash: `SELECT 1 AS "a\"; SELECT 2;`, split: []string{`SELECT 1 AS "a\"`, "SELECT 2"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := sqllex.Tokens(testCase.rules, `SELECT "a;b"`)
			require.NoError(t, err)
			require.Equal(t, testCase.kind, tokens[len(tokens)-1].Kind)
			require.Equal(t, []string{`SELECT "say \"hi\"; ok"`, "SELECT 1"}, splitWith(t, testCase.rules, `SELECT "say \"hi\"; ok"; SELECT 1;`))
			require.Equal(t, testCase.split, splitWith(t, testCase.rules, testCase.backslash))
		})
	}
}

func splitWith(t *testing.T, rules sqllex.Rules, input string) []string {
	t.Helper()
	statements, err := sqllex.Split(rules, input)
	require.NoError(t, err)
	texts := make([]string, 0, len(statements))
	for _, statement := range statements {
		texts = append(texts, statement.Text)
	}
	return texts
}
