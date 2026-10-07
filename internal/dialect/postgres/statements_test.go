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

package postgres

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/lexical"
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
			name:     "parenthesised rule actions stay one statement",
			input:    "CREATE RULE r AS ON INSERT TO t DO ALSO (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2)); SELECT 1;",
			expected: []string{"CREATE RULE r AS ON INSERT TO t DO ALSO (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2))", "SELECT 1"},
		},
		{
			name:     "END CASE closes the CASE without reopening it",
			input:    "CREATE PROCEDURE p() LANGUAGE plpgsql BEGIN CASE WHEN true THEN SELECT 1; END CASE; END; SELECT 2;",
			expected: []string{"CREATE PROCEDURE p() LANGUAGE plpgsql BEGIN CASE WHEN true THEN SELECT 1; END CASE; END", "SELECT 2"},
		},
		{
			name:     "a dollar inside an identifier",
			input:    "CREATE TABLE prices (amount$gbp$net numeric); SELECT 1;",
			expected: []string{"CREATE TABLE prices (amount$gbp$net numeric)", "SELECT 1"},
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

	_, err := New().ParseStatements(t.Context(), "DO BEGIN SELECT 1;")
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

func TestWithLexicalRulesReplacesTheSplitRules(t *testing.T) {
	t.Parallel()

	const script = "CREATE RULE r AS ON INSERT TO t DO ALSO (INSERT INTO a VALUES (1); INSERT INTO b VALUES (2));"
	rules := lexical.Rules()
	rules.TerminatorOutsideParentheses = false

	statements, err := New(WithLexicalRules(rules)).ParseStatements(t.Context(), script)
	require.NoError(t, err)
	require.Len(t, statements, 2)

	statements, err = New().ParseStatements(t.Context(), script)
	require.NoError(t, err)
	require.Len(t, statements, 1)
}

func TestLexStatementsTokenisesNoFurtherThanTheTokenBudget(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantTokens     []int
		wantOverBudget []bool
	}{
		{
			name:           "a statement over the budget keeps only the budget's tokens",
			sql:            "SELECT 1, 2, 3, 4, 5, 6, 7, 8, 9, 10; SELECT 1",
			wantTokens:     []int{10, 2},
			wantOverBudget: []bool{true, false},
		},
		{
			name:           "a statement of exactly the budget is whole",
			sql:            "SELECT 1, 2, 3, 4, 5",
			wantTokens:     []int{10},
			wantOverBudget: []bool{false},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements, err := New(WithMaxTokensPerStatement(10)).lexStatements(t.Context(), testCase.sql)

			require.NoError(t, err)
			tokens := make([]int, 0, len(statements))
			overBudget := make([]bool, 0, len(statements))
			for _, statement := range statements {
				tokens = append(tokens, len(statement.Tokens))
				overBudget = append(overBudget, statement.OverBudget)
			}
			require.Equal(t, testCase.wantTokens, tokens)
			require.Equal(t, testCase.wantOverBudget, overBudget)
		})
	}
}

func TestSplitStatements_BeginEndBlockHandling(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		sql     string
		want    int
		contain []string
	}{
		{
			name: "do_block_with_inner_semicolons",
			sql: `CREATE TABLE accounts (id UUID PRIMARY KEY);
DO $$
BEGIN
    INSERT INTO accounts VALUES (gen_random_uuid());
    INSERT INTO accounts VALUES (gen_random_uuid());
END;
$$;
SELECT 1;`,
			want:    3,
			contain: []string{"CREATE TABLE", "DO", "SELECT 1"},
		},
		{
			name: "bare_begin_is_transaction_marker",
			sql:  `BEGIN; SELECT 1; COMMIT;`,
			want: 3,
		},
		{

			name:    "begin_as_column_alias_does_not_merge",
			sql:     `SELECT 1 AS begin; SELECT 2;`,
			want:    2,
			contain: []string{"begin", "SELECT 2"},
		},
		{
			name:    "begin_as_column_name_does_not_merge",
			sql:     `SELECT begin FROM events; CREATE TABLE x (id INT);`,
			want:    2,
			contain: []string{"begin", "CREATE TABLE"},
		},
	}

	for _, testCase := range cases {
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
					builder.WriteString(current.Raw)
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
