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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialecttest"
	"seagull.sh/seagull/internal/dialect/duckdb/lexical"
	"seagull.sh/seagull/internal/testkit/lexdiff"
)

func requireTokens(t *testing.T, input string, expected []token) {
	t.Helper()
	tokens, err := tokenise(input)
	require.NoError(t, err, "tokenise returned an unexpected error for input %q", input)
	require.NotEmpty(t, tokens, "tokenise returned no tokens")
	require.Equal(t, tokenEOF, tokens[len(tokens)-1].Kind, "last token should be EOF")

	tokens = tokens[:len(tokens)-1]
	require.Len(t, tokens, len(expected), "token count for input %q", input)
	for index := range expected {
		require.Equal(t, expected[index].Kind, tokens[index].Kind, "token %d kind for input %q", index, input)
		require.Equal(t, expected[index].Value, tokens[index].Value, "token %d value for input %q", index, input)
		require.Equal(t, expected[index].Start, tokens[index].Start, "token %d start for input %q", index, input)
		require.Equal(t, expected[index].Quoted, tokens[index].Quoted, "token %d quoted for input %q", index, input)
	}
}

func requireTokeniseError(t *testing.T, input string, expectedSubstring string) {
	t.Helper()
	_, err := tokenise(input)
	require.Error(t, err, "expected tokenise to return an error for input %q", input)
	require.Contains(t, err.Error(), expectedSubstring)
}

func TestTokenise_SingleCharTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected token
	}{
		{
			name:     "left parenthesis",
			input:    "(",
			expected: token{Kind: tokenLeftParen, Value: "(", Start: 0},
		},
		{
			name:     "right parenthesis",
			input:    ")",
			expected: token{Kind: tokenRightParen, Value: ")", Start: 0},
		},
		{
			name:     "left bracket",
			input:    "[",
			expected: token{Kind: tokenLeftBracket, Value: "[", Start: 0},
		},
		{
			name:     "right bracket",
			input:    "]",
			expected: token{Kind: tokenRightBracket, Value: "]", Start: 0},
		},
		{
			name:     "comma",
			input:    ",",
			expected: token{Kind: tokenComma, Value: ",", Start: 0},
		},
		{
			name:     "semicolon",
			input:    ";",
			expected: token{Kind: tokenSemicolon, Value: ";", Start: 0},
		},
		{
			name:     "dot",
			input:    ".",
			expected: token{Kind: tokenDot, Value: ".", Start: 0},
		},
		{
			name:     "star",
			input:    "*",
			expected: token{Kind: tokenStar, Value: "*", Start: 0},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, []token{testCase.expected})
		})
	}
}

func TestTokenise_Identifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:  "simple identifier",
			input: "users",
			expected: []token{
				{Kind: tokenIdentifier, Value: "users", Start: 0},
			},
		},
		{
			name:  "identifier with underscore",
			input: "_column_name",
			expected: []token{
				{Kind: tokenIdentifier, Value: "_column_name", Start: 0},
			},
		},
		{
			name:  "double-quoted identifier",
			input: `"my column"`,
			expected: []token{
				{Kind: tokenIdentifier, Value: "my column", Start: 0, Quoted: true},
			},
		},
		{
			name:  "double-quoted identifier with escaped quote",
			input: `"col""name"`,
			expected: []token{
				{Kind: tokenIdentifier, Value: `col"name`, Start: 0, Quoted: true},
			},
		},
		{
			name:  "schema-qualified identifier",
			input: "main.users",
			expected: []token{
				{Kind: tokenIdentifier, Value: "main", Start: 0},
				{Kind: tokenDot, Value: ".", Start: 4},
				{Kind: tokenIdentifier, Value: "users", Start: 5},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestTokenise_UnicodeIdentifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:  "ascii identifier is unchanged",
			input: "users",
			expected: []token{
				{Kind: tokenIdentifier, Value: "users", Start: 0},
			},
		},
		{
			name:  "accented latin identifier stays whole",
			input: "café",
			expected: []token{
				{Kind: tokenIdentifier, Value: "café", Start: 0},
			},
		},
		{
			name:  "identifier with a non-ascii diaeresis letter stays whole",
			input: "naïve",
			expected: []token{
				{Kind: tokenIdentifier, Value: "naïve", Start: 0},
			},
		},
		{
			name:  "cjk identifier stays whole",
			input: "名前",
			expected: []token{
				{Kind: tokenIdentifier, Value: "名前", Start: 0},
			},
		},
		{
			name:  "mixed ascii unicode and trailing digit stays whole",
			input: "user_café2",
			expected: []token{
				{Kind: tokenIdentifier, Value: "user_café2", Start: 0},
			},
		},
		{
			name:  "qualified unicode identifiers split only on the dot",
			input: "café.名前",
			expected: []token{
				{Kind: tokenIdentifier, Value: "café", Start: 0},

				{Kind: tokenDot, Value: ".", Start: 5},
				{Kind: tokenIdentifier, Value: "名前", Start: 6},
			},
		},
		{
			name:  "named parameter accepts a unicode body",
			input: ":café",
			expected: []token{
				{Kind: tokenNamedParam, Value: ":café", Start: 0},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestTokenise_Numbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:  "integer",
			input: "42",
			expected: []token{
				{Kind: tokenNumber, Value: "42", Start: 0},
			},
		},
		{
			name:  "decimal",
			input: "3.14",
			expected: []token{
				{Kind: tokenNumber, Value: "3.14", Start: 0},
			},
		},
		{
			name:  "number with exponent",
			input: "1e10",
			expected: []token{
				{Kind: tokenNumber, Value: "1e10", Start: 0},
			},
		},
		{
			name:  "decimal with signed exponent",
			input: "2.5E-3",
			expected: []token{
				{Kind: tokenNumber, Value: "2.5E-3", Start: 0},
			},
		},
		{
			name:  "hex prefix reads as zero then an identifier",
			input: "0xFF",
			expected: []token{
				{Kind: tokenNumber, Value: "0", Start: 0},
				{Kind: tokenIdentifier, Value: "xFF", Start: 1},
			},
		},
		{
			name:  "octal prefix reads as zero then an identifier",
			input: "0o77",
			expected: []token{
				{Kind: tokenNumber, Value: "0", Start: 0},
				{Kind: tokenIdentifier, Value: "o77", Start: 1},
			},
		},
		{
			name:  "binary prefix reads as zero then an identifier",
			input: "0b1010",
			expected: []token{
				{Kind: tokenNumber, Value: "0", Start: 0},
				{Kind: tokenIdentifier, Value: "b1010", Start: 1},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestTokenise_Strings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:  "simple string",
			input: "'hello'",
			expected: []token{
				{Kind: tokenString, Value: "hello", Start: 0},
			},
		},
		{
			name:  "string with escaped single quote",
			input: "'it''s'",
			expected: []token{
				{Kind: tokenString, Value: "it's", Start: 0},
			},
		},
		{
			name:  "empty string",
			input: "''",
			expected: []token{
				{Kind: tokenString, Value: "", Start: 0},
			},
		},
		{
			name:  "bit string",
			input: "B'1010'",
			expected: []token{
				{Kind: tokenBitString, Value: "1010", Start: 0},
			},
		},
		{
			name:  "bit string lowercase prefix",
			input: "b'110'",
			expected: []token{
				{Kind: tokenBitString, Value: "110", Start: 0},
			},
		},
		{
			name:  "doubled-dollar quoted string",
			input: "$$body$$",
			expected: []token{
				{Kind: tokenDollarString, Value: "body", Start: 0},
			},
		},
		{
			name:  "tagged dollar quoted string",
			input: "$tag$body$tag$",
			expected: []token{
				{Kind: tokenDollarString, Value: "body", Start: 0},
			},
		},
		{
			name:  "dollar quoted string with embedded single quote",
			input: "$$it's fine$$",
			expected: []token{
				{Kind: tokenDollarString, Value: "it's fine", Start: 0},
			},
		},
		{
			name:  "empty doubled-dollar quoted string",
			input: "$$$$",
			expected: []token{
				{Kind: tokenDollarString, Value: "", Start: 0},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestTokenise_Parameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:  "dollar parameter",
			input: "$1",
			expected: []token{
				{Kind: tokenDollarParam, Value: "$1", Start: 0},
			},
		},
		{
			name:  "dollar parameter with multiple digits",
			input: "$123",
			expected: []token{
				{Kind: tokenDollarParam, Value: "$123", Start: 0},
			},
		},
		{
			name:  "dollar parameter at the maximum",
			input: "$65535",
			expected: []token{
				{Kind: tokenDollarParam, Value: "$65535", Start: 0},
			},
		},
		{
			name:  "named parameter with colon",
			input: ":name",
			expected: []token{
				{Kind: tokenNamedParam, Value: ":name", Start: 0},
			},
		},
		{
			name:  "cast operator",
			input: "::",
			expected: []token{
				{Kind: tokenCast, Value: "::", Start: 0},
			},
		},
		{
			name:  "cast used in expression",
			input: "x::int",
			expected: []token{
				{Kind: tokenIdentifier, Value: "x", Start: 0},
				{Kind: tokenCast, Value: "::", Start: 1},
				{Kind: tokenIdentifier, Value: "int", Start: 3},
			},
		},
		{
			name:  "colon as bare operator",
			input: ": ",
			expected: []token{
				{Kind: tokenOperator, Value: ":", Start: 0},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestTokenise_Operators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:  "less than or equal",
			input: "<=",
			expected: []token{
				{Kind: tokenOperator, Value: "<=", Start: 0},
			},
		},
		{
			name:  "greater than or equal",
			input: ">=",
			expected: []token{
				{Kind: tokenOperator, Value: ">=", Start: 0},
			},
		},
		{
			name:  "not equal angle brackets",
			input: "<>",
			expected: []token{
				{Kind: tokenOperator, Value: "<>", Start: 0},
			},
		},
		{
			name:  "not equal exclamation",
			input: "!=",
			expected: []token{
				{Kind: tokenOperator, Value: "!=", Start: 0},
			},
		},
		{
			name:  "string concatenation",
			input: "||",
			expected: []token{
				{Kind: tokenOperator, Value: "||", Start: 0},
			},
		},
		{
			name:  "left shift",
			input: "<<",
			expected: []token{
				{Kind: tokenOperator, Value: "<<", Start: 0},
			},
		},
		{
			name:  "right shift",
			input: ">>",
			expected: []token{
				{Kind: tokenOperator, Value: ">>", Start: 0},
			},
		},
		{
			name:  "arrow operator",
			input: "->",
			expected: []token{
				{Kind: tokenArrow, Value: "->", Start: 0},
			},
		},
		{
			name:  "double arrow operator",
			input: "->>",
			expected: []token{
				{Kind: tokenDoubleArrow, Value: "->>", Start: 0},
			},
		},
		{
			name:  "contains operator",
			input: "@>",
			expected: []token{
				{Kind: tokenOperator, Value: "@>", Start: 0},
			},
		},
		{
			name:  "contained by operator",
			input: "<@",
			expected: []token{
				{Kind: tokenOperator, Value: "<@", Start: 0},
			},
		},
		{
			name:  "regex match case insensitive",
			input: "~*",
			expected: []token{
				{Kind: tokenOperator, Value: "~*", Start: 0},
			},
		},
		{
			name:  "not regex",
			input: "!~",
			expected: []token{
				{Kind: tokenOperator, Value: "!~", Start: 0},
			},
		},
		{
			name:  "not regex case insensitive",
			input: "!~*",
			expected: []token{
				{Kind: tokenOperator, Value: "!~*", Start: 0},
			},
		},
		{
			name:  "logical and",
			input: "&&",
			expected: []token{
				{Kind: tokenOperator, Value: "&&", Start: 0},
			},
		},
		{
			name:  "plus",
			input: "+",
			expected: []token{
				{Kind: tokenOperator, Value: "+", Start: 0},
			},
		},
		{
			name:  "caret",
			input: "^",
			expected: []token{
				{Kind: tokenOperator, Value: "^", Start: 0},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestTokenise_WhitespaceAndComments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:     "whitespace only produces no tokens",
			input:    "   \t\n  ",
			expected: []token{},
		},
		{
			name:  "line comment is skipped",
			input: "-- this is a comment\nSELECT",
			expected: []token{
				{Kind: tokenIdentifier, Value: "SELECT", Start: 21},
			},
		},
		{
			name:  "block comment is skipped",
			input: "/* block */SELECT",
			expected: []token{
				{Kind: tokenIdentifier, Value: "SELECT", Start: 11},
			},
		},
		{
			name:  "nested block comment is skipped",
			input: "/* outer /* inner */ still comment */SELECT",
			expected: []token{
				{Kind: tokenIdentifier, Value: "SELECT", Start: 37},
			},
		},
		{
			name:  "tokens separated by various whitespace",
			input: "a \t b \n c",
			expected: []token{
				{Kind: tokenIdentifier, Value: "a", Start: 0},
				{Kind: tokenIdentifier, Value: "b", Start: 4},
				{Kind: tokenIdentifier, Value: "c", Start: 8},
			},
		},
		{
			name:  "inline block comment between tokens",
			input: "a /* comment */ b",
			expected: []token{
				{Kind: tokenIdentifier, Value: "a", Start: 0},
				{Kind: tokenIdentifier, Value: "b", Start: 16},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestTokenise_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		input             string
		expectedSubstring string
	}{
		{
			name:              "unterminated string literal",
			input:             "'hello",
			expectedSubstring: "unterminated string literal",
		},
		{
			name:              "unterminated quoted identifier",
			input:             `"column`,
			expectedSubstring: "unterminated quoted identifier",
		},
		{
			name:              "bare dollar without digit or tag",
			input:             "$ ",
			expectedSubstring: "invalid dollar-quoted string",
		},
		{
			name:              "unterminated dollar-quoted string",
			input:             "$$body",
			expectedSubstring: "unterminated dollar-quoted string",
		},
		{
			name:              "unterminated tagged dollar-quoted string",
			input:             "$tag$body$other$",
			expectedSubstring: "unterminated dollar-quoted string",
		},
		{
			name:              "unterminated bit string",
			input:             "B'101",
			expectedSubstring: "unterminated bit string",
		},
		{
			name:              "unterminated block comment",
			input:             "SELECT /* open",
			expectedSubstring: "unterminated block comment",
		},
		{
			name:              "unterminated block comment at end of input",
			input:             "SELECT /*",
			expectedSubstring: "unterminated block comment",
		},
		{
			name:              "positional parameter above the maximum",
			input:             "SELECT $65536",
			expectedSubstring: "exceeds maximum of 65535",
		},
		{
			name:              "positional parameter that would overflow an int",
			input:             "SELECT $999999999999999999999",
			expectedSubstring: "exceeds maximum of 65535",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokeniseError(t, testCase.input, testCase.expectedSubstring)
		})
	}
}

func TestTokenise_CompleteStatements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:  "select with dollar parameter",
			input: "SELECT id FROM t WHERE id = $1",
			expected: []token{
				{Kind: tokenIdentifier, Value: "SELECT", Start: 0},
				{Kind: tokenIdentifier, Value: "id", Start: 7},
				{Kind: tokenIdentifier, Value: "FROM", Start: 10},
				{Kind: tokenIdentifier, Value: "t", Start: 15},
				{Kind: tokenIdentifier, Value: "WHERE", Start: 17},
				{Kind: tokenIdentifier, Value: "id", Start: 23},
				{Kind: tokenOperator, Value: "=", Start: 26},
				{Kind: tokenDollarParam, Value: "$1", Start: 28},
			},
		},
		{
			name:  "select with cast and arrow",
			input: "SELECT data->>'key' FROM t WHERE val::int > $1",
			expected: []token{
				{Kind: tokenIdentifier, Value: "SELECT", Start: 0},
				{Kind: tokenIdentifier, Value: "data", Start: 7},
				{Kind: tokenDoubleArrow, Value: "->>", Start: 11},
				{Kind: tokenString, Value: "key", Start: 14},
				{Kind: tokenIdentifier, Value: "FROM", Start: 20},
				{Kind: tokenIdentifier, Value: "t", Start: 25},
				{Kind: tokenIdentifier, Value: "WHERE", Start: 27},
				{Kind: tokenIdentifier, Value: "val", Start: 33},
				{Kind: tokenCast, Value: "::", Start: 36},
				{Kind: tokenIdentifier, Value: "int", Start: 38},
				{Kind: tokenOperator, Value: ">", Start: 42},
				{Kind: tokenDollarParam, Value: "$1", Start: 44},
			},
		},
		{
			name:  "insert with named parameter",
			input: "INSERT INTO users (name) VALUES (:name)",
			expected: []token{
				{Kind: tokenIdentifier, Value: "INSERT", Start: 0},
				{Kind: tokenIdentifier, Value: "INTO", Start: 7},
				{Kind: tokenIdentifier, Value: "users", Start: 12},
				{Kind: tokenLeftParen, Value: "(", Start: 18},
				{Kind: tokenIdentifier, Value: "name", Start: 19},
				{Kind: tokenRightParen, Value: ")", Start: 23},
				{Kind: tokenIdentifier, Value: "VALUES", Start: 25},
				{Kind: tokenLeftParen, Value: "(", Start: 32},
				{Kind: tokenNamedParam, Value: ":name", Start: 33},
				{Kind: tokenRightParen, Value: ")", Start: 38},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestTokenise_DuckDBLexicalForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:  "leading-dot number",
			input: ".5",
			expected: []token{
				{Kind: tokenNumber, Value: ".5", Start: 0},
			},
		},
		{
			name:  "number with exponent",
			input: "2.5e-3",
			expected: []token{
				{Kind: tokenNumber, Value: "2.5e-3", Start: 0},
			},
		},
		{
			name:  "quoted keyword is a quoted identifier",
			input: `"select"`,
			expected: []token{
				{Kind: tokenIdentifier, Value: "select", Start: 0, Quoted: true},
			},
		},
		{
			name:  "struct literal braces",
			input: "{'a': 1}",
			expected: []token{
				{Kind: tokenLeftBrace, Value: "{", Start: 0},
				{Kind: tokenString, Value: "a", Start: 1},
				{Kind: tokenOperator, Value: ":", Start: 4},
				{Kind: tokenNumber, Value: "1", Start: 6},
				{Kind: tokenRightBrace, Value: "}", Start: 7},
			},
		},
		{
			name:  "named argument assignment",
			input: "a := 1",
			expected: []token{
				{Kind: tokenIdentifier, Value: "a", Start: 0},
				{Kind: tokenOperator, Value: ":=", Start: 2},
				{Kind: tokenNumber, Value: "1", Start: 5},
			},
		},
		{
			name:  "escape string",
			input: `E'it\'s'`,
			expected: []token{
				{Kind: tokenEscapeString, Value: "it's", Start: 0},
			},
		},
		{
			name:  "exponent and integer division",
			input: "a ** 2 // 3",
			expected: []token{
				{Kind: tokenIdentifier, Value: "a", Start: 0},
				{Kind: tokenOperator, Value: "**", Start: 2},
				{Kind: tokenNumber, Value: "2", Start: 5},
				{Kind: tokenOperator, Value: "//", Start: 7},
				{Kind: tokenNumber, Value: "3", Start: 10},
			},
		},
		{
			name:  "pattern and distance operators",
			input: "a ~~ b !~~* c <-> d ^@ e == f",
			expected: []token{
				{Kind: tokenIdentifier, Value: "a", Start: 0},
				{Kind: tokenOperator, Value: "~~", Start: 2},
				{Kind: tokenIdentifier, Value: "b", Start: 5},
				{Kind: tokenOperator, Value: "!~~*", Start: 7},
				{Kind: tokenIdentifier, Value: "c", Start: 12},
				{Kind: tokenOperator, Value: "<->", Start: 14},
				{Kind: tokenIdentifier, Value: "d", Start: 18},
				{Kind: tokenOperator, Value: "^@", Start: 20},
				{Kind: tokenIdentifier, Value: "e", Start: 23},
				{Kind: tokenOperator, Value: "==", Start: 25},
				{Kind: tokenIdentifier, Value: "f", Start: 28},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestTokenise_NumbersWithDigitSeparators(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		input    string
		expected []token
	}{
		{input: "1_000", expected: []token{{Kind: tokenNumber, Value: "1000", Start: 0}}},
		{input: "3_000_000_000", expected: []token{{Kind: tokenNumber, Value: "3000000000", Start: 0}}},
		{input: "1_000.5_5", expected: []token{{Kind: tokenNumber, Value: "1000.55", Start: 0}}},
		{input: ".5_5", expected: []token{{Kind: tokenNumber, Value: ".55", Start: 0}}},
		{input: "1.5e1_0", expected: []token{{Kind: tokenNumber, Value: "1.5e10", Start: 0}}},
		{input: "1__000", expected: []token{{Kind: tokenNumber, Value: "1", Start: 0}, {Kind: tokenIdentifier, Value: "__000", Start: 1}}},
		{input: "1_ x", expected: []token{{Kind: tokenNumber, Value: "1", Start: 0}, {Kind: tokenIdentifier, Value: "_", Start: 1},
			{Kind: tokenIdentifier, Value: "x", Start: 3}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()

			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestTokeniserReadsDollarNamedParameters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		input     string
		wantKind  tokenKind
		wantValue string
	}{
		{name: "a named parameter", input: "$name", wantKind: tokenNamedParam, wantValue: "$name"},
		{name: "a named parameter with digits and underscores", input: "$min_length2", wantKind: tokenNamedParam, wantValue: "$min_length2"},
		{name: "a numbered parameter", input: "$12", wantKind: tokenDollarParam, wantValue: "$12"},
		{name: "a tagged dollar-quoted string", input: "$name$ $x $name$", wantKind: tokenDollarString, wantValue: " $x "},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.input)
			require.NoError(t, err)
			require.Len(t, tokens, 2)
			require.Equal(t, testCase.wantKind, tokens[0].Kind)
			require.Equal(t, testCase.wantValue, tokens[0].Value)
		})
	}
}

func TestTokeniserReadsAColonAfterAnOperandAsPunctuation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		input     string
		wantKinds []tokenKind
	}{
		{name: "after a name", input: "x:x", wantKinds: []tokenKind{tokenIdentifier, tokenOperator, tokenIdentifier, tokenEOF}},
		{name: "after a string", input: "'k':v", wantKinds: []tokenKind{tokenString, tokenOperator, tokenIdentifier, tokenEOF}},
		{name: "after a closing bracket", input: "]:v", wantKinds: []tokenKind{tokenRightBracket, tokenOperator, tokenIdentifier, tokenEOF}},
		{name: "after an operator", input: "=:v", wantKinds: []tokenKind{tokenOperator, tokenNamedParam, tokenEOF}},
		{name: "after a space", input: "x :v", wantKinds: []tokenKind{tokenIdentifier, tokenNamedParam, tokenEOF}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.input)
			require.NoError(t, err)
			kinds := make([]tokenKind, 0, len(tokens))
			for _, token := range tokens {
				kinds = append(kinds, token.Kind)
			}
			require.Equal(t, testCase.wantKinds, kinds)
		})
	}
}

func tokenise(input string) ([]token, error) {
	return tokeniseSpan(input, 0, len(input))
}

func TestTokeniserSpansMatchSharedScanner(t *testing.T) {
	t.Parallel()

	files, err := lexdiff.Corpus("duckdb")
	require.NoError(t, err)
	samples := []string{
		"SELECT 'a''b', $tag$x;y$tag$, $$z$$, B'10', \"q\"\"x\" /* a /* b */ */ -- c\nFROM t;",
		"SELECT $1::INTEGER, :name, [1, 2] FROM t;",
		"SELECT E'it\\'s', e'a\\\\', {'k': 1}, x := 2, a ** b, a // b FROM t;",
	}
	for index, sample := range samples {
		files = append(files, lexdiff.File{Path: "sample " + string(rune('a'+index)), Content: sample})
	}

	checked := 0
	var problems []string
	for _, file := range files {
		tokens, tokeniseErr := tokenise(file.Content)
		if tokeniseErr != nil {
			continue
		}
		checked++
		for _, problem := range dialecttest.CompareTokeniserSpans(lexical.Rules(), file.Content, spanTokens(file.Content, tokens),
			lexicalRules.Comments.SkipWhitespaceAndComments) {
			problems = append(problems, file.Path+": "+problem)
		}
	}
	require.Greater(t, checked, len(samples))
	require.Empty(t, problems)
}

func spanTokens(source string, tokens []token) []dialecttest.Token {
	result := make([]dialecttest.Token, 0, len(tokens))
	for _, current := range tokens {
		if current.Kind == tokenEOF {
			continue
		}
		result = append(result, dialecttest.Token{Position: current.Start, Class: spanClass(source, current)})
	}
	return result
}

func spanClass(source string, candidate token) dialecttest.TokenClass {
	switch candidate.Kind {
	case tokenIdentifier:
		if candidate.Quoted {
			return dialecttest.TokenQuotedIdentifier
		}
		return dialecttest.TokenWord
	case tokenString, tokenDollarString, tokenBitString, tokenEscapeString:
		return dialecttest.TokenString
	default:
		return dialecttest.TokenOther
	}
}
