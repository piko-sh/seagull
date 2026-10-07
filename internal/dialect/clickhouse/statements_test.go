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

package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/clickhouse/lexical"
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
			name:     "a hash comment inside a statement",
			input:    "SELECT 1 # it's a note; still the note\n, 2; SELECT 3;",
			expected: []string{"SELECT 1 # it's a note; still the note\n, 2", "SELECT 3"},
		},
		{
			name:     "a backslash-escaped backtick inside an identifier",
			input:    "CREATE TABLE `a\\`;b` (id Int32) ENGINE = Memory; SELECT 1;",
			expected: []string{"CREATE TABLE `a\\`;b` (id Int32) ENGINE = Memory", "SELECT 1"},
		},
		{
			name:     "a backslash-escaped quote inside a double-quoted identifier",
			input:    `SELECT "a\";b" FROM t; SELECT 1;`,
			expected: []string{`SELECT "a\";b" FROM t`, "SELECT 1"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements, err := dialectkit.LexStatements(t.Context(), lexical.Rules(), testCase.input, statementTokens, tokenEOF, 0)
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

	_, err := New().ParseStatements(t.Context(), "SELECT 'abc")
	require.ErrorIs(t, err, sqllex.ErrUnterminatedString)
	require.ErrorIs(t, err, sqllex.ErrMalformed)
}

func TestTokeniserErrorNamesTheWholeCharacter(t *testing.T) {
	t.Parallel()

	_, err := tokenise("SELECT 1 € 2")
	var parseError *dialectkit.ParseError
	require.ErrorAs(t, err, &parseError)
	require.Equal(t, &dialectkit.ParseError{Token: "€", Message: "unexpected character", Position: 9}, parseError)
}

func TestSplitStatements_MultipleStatements(t *testing.T) {
	t.Parallel()

	statements := splitTokens(t, "CREATE TABLE a (id UInt64) ENGINE = Log; CREATE TABLE b (id UInt64) ENGINE = Log;")
	require.Len(t, statements, 2)
	assert.Equal(t, statementKindCreateTable, classifyStatement(statements[0]))
	assert.Equal(t, statementKindCreateTable, classifyStatement(statements[1]))
}

func TestSplitStatements_TrailingSemicolonDoesNotProduceEmptyStatement(t *testing.T) {
	t.Parallel()

	statements := splitTokens(t, "SELECT 1;")
	assert.Len(t, statements, 1)
}

func TestSplitStatements_EmptyInput(t *testing.T) {
	t.Parallel()

	assert.Empty(t, splitTokens(t, ""))
}

func splitTokens(t *testing.T, sql string) [][]token {
	t.Helper()

	statements, err := dialectkit.LexStatements(t.Context(), lexical.Rules(), sql, statementTokens, tokenEOF, 0)
	require.NoError(t, err)
	result := make([][]token, 0, len(statements))
	for _, statement := range statements {
		result = append(result, statement.Tokens)
	}
	return result
}
