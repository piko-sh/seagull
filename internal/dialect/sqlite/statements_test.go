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
			input:    "CREATE TRIGGER t AFTER INSERT ON a BEGIN SELECT CASE WHEN 1 THEN 2 END CASE; END; SELECT 2;",
			expected: []string{"CREATE TRIGGER t AFTER INSERT ON a BEGIN SELECT CASE WHEN 1 THEN 2 END CASE; END", "SELECT 2"},
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

			statements, err := lexStatements(t.Context(), testCase.input, dialectkit.DefaultMaxTokensPerStatement)
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

	_, err := New().ParseStatements(t.Context(), "CREATE TRIGGER t AFTER INSERT ON a BEGIN DELETE FROM b;")
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

func TestSplitStatements_BeginEndBlockHandling(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		sql     string
		want    int
		contain []string
	}{
		{
			name: "trigger_body_with_inner_semicolon",
			sql: `CREATE TABLE accounts (id TEXT);
CREATE TRIGGER tr_no_update
    BEFORE UPDATE ON accounts
    FOR EACH ROW
BEGIN
    SELECT RAISE(ABORT, 'no');
END;
INSERT INTO accounts VALUES ('a');`,
			want:    3,
			contain: []string{"CREATE TABLE", "CREATE TRIGGER", "INSERT"},
		},
		{
			name: "multi_statement_trigger_body",
			sql: `CREATE TRIGGER tr_log
    AFTER INSERT ON accounts
BEGIN
    INSERT INTO log VALUES (NEW.id);
    INSERT INTO log VALUES (NEW.id);
END;`,
			want:    1,
			contain: []string{"CREATE TRIGGER"},
		},
		{
			name: "bare_begin_is_transaction_marker",
			sql:  `BEGIN; SELECT 1; COMMIT;`,
			want: 3,
		},
		{
			name:    "begin_as_column_alias_does_not_swallow_next_statement",
			sql:     `SELECT 1 AS begin; SELECT 2;`,
			want:    2,
			contain: []string{"SELECT 1 AS begin", "SELECT 2"},
		},
		{
			name:    "begin_as_identifier_in_non_procedural_statement",
			sql:     `SELECT begin FROM t; SELECT 2;`,
			want:    2,
			contain: []string{"SELECT begin FROM t", "SELECT 2"},
		},
		{
			name: "case_insensitive_begin_end",
			sql: `CREATE TRIGGER t
    BEFORE INSERT ON foo
begin
    select raise(abort, 'no');
End;`,
			want: 1,
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
				var buffer strings.Builder
				for _, current := range statements[index] {
					buffer.WriteString(current.Value)
					buffer.WriteRune(' ')
				}
				require.Contains(t, buffer.String(), want)
			}
		})
	}
}

func TestSplitStatements_TriggerBodyWithCaseExpression(t *testing.T) {
	t.Parallel()

	sql := `CREATE TRIGGER trg AFTER INSERT ON x BEGIN UPDATE y SET z = CASE WHEN NEW.a > 0 THEN 1 ELSE 0 END WHERE id = NEW.id; END;
SELECT 1;`

	statements := splitTokens(t, sql)
	require.Len(t, statements, 2,
		"the trigger (with its inner CASE) and the trailing SELECT should be two statements")
}

func tokenise(input string) ([]token, error) {
	next := statementTokens(input, 0, len(input))
	var tokens []token
	for {
		current, err := next()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, current)
		if current.Kind == tokenEOF {
			return tokens, nil
		}
	}
}

func splitTokens(t *testing.T, sql string) [][]token {
	t.Helper()

	statements, err := lexStatements(t.Context(), sql, dialectkit.DefaultMaxTokensPerStatement)
	require.NoError(t, err)
	result := make([][]token, 0, len(statements))
	for _, statement := range statements {
		result = append(result, statement.Tokens)
	}
	return result
}
