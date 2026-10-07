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

package martian

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
)

func TestSyntaxErrorsMatchTheParseSentinelAndSayWhereTheyAre(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		sql              string
		expectedToken    string
		expectedMessage  string
		expectedPosition int
	}{
		{
			name:             "an unknown character",
			sql:              "SELECT a ? b FROM items",
			expectedToken:    "?",
			expectedPosition: 9,
			expectedMessage:  `syntax error near "?": unexpected character`,
		},
		{
			name:             "the concatenation operator",
			sql:              "SELECT a || b FROM items",
			expectedToken:    "||",
			expectedPosition: 9,
			expectedMessage:  `syntax error near "||": Martian has no || operator; join strings with CONCAT`,
		},
		{
			name:             "an unterminated string",
			sql:              "SELECT 'open FROM items",
			expectedToken:    "'",
			expectedPosition: 7,
			expectedMessage:  `syntax error near "'": unterminated string literal`,
		},
		{
			name:             "a token the grammar does not allow",
			sql:              "DROP TABLE items",
			expectedToken:    "DROP",
			expectedPosition: 0,
			expectedMessage:  `syntax error near "DROP": expected CREATE, SELECT, INSERT, UPDATE or DELETE`,
		},
		{
			name:             "input that ends too soon",
			sql:              "SELECT id",
			expectedToken:    "",
			expectedPosition: 9,
			expectedMessage:  "syntax error at end of input: expected FROM but the input ended",
		},
		{
			name:             "a type modifier out of range",
			sql:              "CREATE TABLE items (id TEXTUAL(99999999999999999999))",
			expectedToken:    "99999999999999999999",
			expectedPosition: 31,
			expectedMessage:  `syntax error near "99999999999999999999": type modifier: strconv.Atoi: parsing "99999999999999999999": value out of range`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := New().ParseStatements(t.Context(), testCase.sql)
			require.ErrorIs(t, err, errSyntax)
			require.ErrorIs(t, err, fault.ErrParse)
			require.EqualError(t, err, testCase.expectedMessage)
			parseError, isParseError := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, isParseError)
			token, position := parseError.Location()
			require.Equal(t, testCase.expectedToken, token)
			require.Equal(t, testCase.expectedPosition, position)
		})
	}
}

func TestAnalysingDDLIsASyntaxErrorAtTheStatementsStart(t *testing.T) {
	t.Parallel()

	_, err := New().AnalyseQuery(t.Context(), nil, parseOne(t, "  CREATE TABLE a (x WHOLE32)"))
	require.ErrorIs(t, err, errSyntax)
	require.ErrorIs(t, err, fault.ErrParse)
	require.EqualError(t, err, `syntax error near "CREATE": CREATE TABLE is not a query`)
	parseError, isParseError := errors.AsType[*dialectkit.ParseError](err)
	require.True(t, isParseError)
	token, position := parseError.Location()
	require.Equal(t, "CREATE", token)
	require.Equal(t, 2, position)
}
