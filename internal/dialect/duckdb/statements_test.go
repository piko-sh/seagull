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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqllex"
)

func TestLexStatementsSplitsUnderTheLexicalRules(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "END CASE closes the CASE without reopening it",
			input:    "CREATE FUNCTION f() AS BEGIN CASE WHEN 1 THEN SELECT 1; END CASE; END; SELECT 2;",
			expected: []string{"CREATE FUNCTION f() AS BEGIN CASE WHEN 1 THEN SELECT 1; END CASE; END", "SELECT 2"},
		},
		{
			name:     "a column named begin",
			input:    "SELECT 1 AS begin; SELECT 2;",
			expected: []string{"SELECT 1 AS begin", "SELECT 2"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements, err := New().lexStatements(t.Context(), testCase.input)
			require.NoError(t, err)
			texts := make([]string, 0, len(statements))
			for _, statement := range statements {
				texts = append(texts, statement.Span.Text)
				require.Equal(t, statement.Span.Start, statement.Tokens[0].Start)
			}
			require.Equal(t, testCase.expected, texts)

			parsed, err := New().ParseStatements(t.Context(), testCase.input)
			require.NoError(t, err)
			require.Len(t, parsed, len(testCase.expected))
			for index, statement := range parsed {
				require.Equal(t, statements[index].Span.Start, statement.Location)
				require.Equal(t, len(testCase.expected[index]), statement.Length)
			}
		})
	}
}

func TestParseStatementsSkipsALeadingByteOrderMark(t *testing.T) {
	t.Parallel()

	statements, err := New().ParseStatements(t.Context(), sqllex.ByteOrderMark+"SELECT 1;\nSELECT 22")
	require.NoError(t, err)
	require.Len(t, statements, 2)
	require.Equal(t, len(sqllex.ByteOrderMark), statements[0].Location)
	require.Equal(t, len("SELECT 1"), statements[0].Length)
	require.Equal(t, len(sqllex.ByteOrderMark+"SELECT 1;\n"), statements[1].Location)
	require.Equal(t, len("SELECT 22"), statements[1].Length)
}

func TestParseStatementsReportsLexicalErrors(t *testing.T) {
	t.Parallel()

	_, err := New().ParseStatements(t.Context(), "CREATE FUNCTION f() AS BEGIN SELECT 1;")
	require.ErrorIs(t, err, sqllex.ErrUnterminatedBlock)
	require.ErrorIs(t, err, sqllex.ErrMalformed)
}

func TestTokeniserErrorNamesTheWholeCharacter(t *testing.T) {
	t.Parallel()

	_, err := tokenise("SELECT 1 € 2")
	var parseError *dialectkit.ParseError
	require.ErrorAs(t, err, &parseError)
	require.Equal(t, &dialectkit.ParseError{Token: "€", Message: "unexpected character", Position: 9}, parseError)
}

func TestParseStatementsKeepsOnlyTheTokensWithinTheBudget(t *testing.T) {
	t.Parallel()

	statements, err := New(WithMaxTokensPerStatement(5)).ParseStatements(t.Context(), "SELECT a, b, c, d, e FROM t; SELECT 1")

	require.NoError(t, err)
	require.Len(t, statements, 2)
	first, isParsed := statements[0].Raw.(*parsedStatement)
	require.True(t, isParsed)
	require.Len(t, first.tokens, 5)
	require.True(t, first.overBudget)
	require.Equal(t, statementKindSelect, first.kind)
	second, isParsed := statements[1].Raw.(*parsedStatement)
	require.True(t, isParsed)
	require.False(t, second.overBudget)
}

func TestSplitKeepsABeginEndBodyInOneStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		contain []string
		want    int
	}{
		{
			name: "create_prefixed_begin_end_block",
			sql: `CREATE OR REPLACE MACRO m() AS BEGIN
    SELECT 1;
    SELECT 2;
END;
SELECT 3;`,
			want:    2,
			contain: []string{"CREATE OR REPLACE MACRO", "SELECT 3"},
		},
		{
			name: "bare_begin_is_transaction_marker",
			sql:  `BEGIN; SELECT 1; COMMIT;`,
			want: 3,
		},
		{
			name:    "begin_alias_does_not_swallow_next_statement",
			sql:     `SELECT 1 AS begin; SELECT 2;`,
			want:    2,
			contain: []string{"SELECT 1 AS begin", "SELECT 2"},
		},
		{
			name:    "begin_alias_quoted_identifier_does_not_open_block",
			sql:     `SELECT 1 AS "begin"; SELECT 2;`,
			want:    2,
			contain: []string{"SELECT 1 AS", "SELECT 2"},
		},
		{
			name: "macro_body_with_scalar_case_keeps_statement_intact",
			sql: `CREATE MACRO m(x) AS BEGIN
    SELECT CASE x WHEN 1 THEN 2 ELSE 3 END;
    SELECT 4;
END;
SELECT 5;`,
			want:    2,
			contain: []string{"CREATE MACRO", "SELECT 5"},
		},
		{
			name: "macro_body_with_end_if",
			sql: `CREATE MACRO m() AS BEGIN
    IF x = 1 THEN
        SELECT 1;
    END IF;
END;
SELECT 2;`,
			want:    2,
			contain: []string{"CREATE MACRO", "SELECT 2"},
		},
		{
			name: "macro_body_with_end_loop",
			sql: `CREATE MACRO m() AS BEGIN
    LOOP
        SELECT 1;
    END LOOP;
END;`,
			want:    1,
			contain: []string{"CREATE MACRO"},
		},
		{
			name: "macro_body_with_end_case",
			sql: `CREATE MACRO m() AS BEGIN
    CASE x
        WHEN 1 THEN SELECT 1;
        ELSE SELECT 0;
    END CASE;
END;`,
			want:    1,
			contain: []string{"CREATE MACRO"},
		},
		{
			name: "macro_body_with_end_while",
			sql: `CREATE MACRO m() AS BEGIN
    WHILE x < 10 DO
        SELECT 1;
    END WHILE;
END;`,
			want:    1,
			contain: []string{"CREATE MACRO"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			statements := splitTokens(t, testCase.sql)
			require.Len(t, statements, testCase.want)
			for index, want := range testCase.contain {
				if index >= len(statements) {
					break
				}
				var builder strings.Builder
				for _, current := range statements[index] {
					builder.WriteString(current.Value)
					builder.WriteRune(' ')
				}
				require.Contains(t, builder.String(), want)
			}
		})
	}
}

func splitTokens(t *testing.T, sql string) [][]token {
	t.Helper()

	statements, err := New().lexStatements(t.Context(), sql)
	require.NoError(t, err)
	result := make([][]token, 0, len(statements))
	for _, statement := range statements {
		result = append(result, statement.Tokens)
	}
	return result
}
