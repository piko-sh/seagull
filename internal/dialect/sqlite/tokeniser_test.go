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

	"seagull.sh/seagull/internal/dialect/dialecttest"
	"seagull.sh/seagull/internal/dialect/sqlite/lexical"
	"seagull.sh/seagull/internal/testkit/lexdiff"
)

func requireTokens(t *testing.T, input string, expected []token) {
	t.Helper()
	tokens, err := tokenise(input)
	require.NoError(t, err, "tokenise returned an unexpected error for input %q", input)
	require.NotEmpty(t, tokens, "tokenise returned no tokens")
	require.Equal(t, tokenEOF, tokens[len(tokens)-1].Kind, "last token should be EOF")

	tokens = tokens[:len(tokens)-1]
	for index := range tokens {
		require.Equal(t, input[tokens[index].Start:tokens[index].End], tokens[index].Raw,
			"token %d's source text should be its span of the input", index)
		tokens[index].Raw = ""
		tokens[index].End = 0
	}
	require.Equal(t, expected, tokens)
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
			name:  "bracket identifier",
			input: "[my column]",
			expected: []token{
				{Kind: tokenIdentifier, Value: "my column", Start: 0, Quoted: true},
			},
		},
		{
			name:  "multiple identifiers separated by dot",
			input: "schema.table",
			expected: []token{
				{Kind: tokenIdentifier, Value: "schema", Start: 0},
				{Kind: tokenDot, Value: ".", Start: 6},
				{Kind: tokenIdentifier, Value: "table", Start: 7},
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
			name:  "accented latin identifier",
			input: "café",
			expected: []token{
				{Kind: tokenIdentifier, Value: "café", Start: 0},
			},
		},
		{
			name:  "diaeresis identifier",
			input: "naïve",
			expected: []token{
				{Kind: tokenIdentifier, Value: "naïve", Start: 0},
			},
		},
		{
			name:  "cjk identifier",
			input: "名前",
			expected: []token{
				{Kind: tokenIdentifier, Value: "名前", Start: 0},
			},
		},
		{
			name:  "mixed ascii underscore unicode and digit",
			input: "user_café2",
			expected: []token{
				{Kind: tokenIdentifier, Value: "user_café2", Start: 0},
			},
		},
		{
			name:  "ascii identifier remains unchanged",
			input: "plain_column1",
			expected: []token{
				{Kind: tokenIdentifier, Value: "plain_column1", Start: 0},
			},
		},
		{
			name:  "unicode identifiers separated by dot",
			input: "café.名前",
			expected: []token{
				{Kind: tokenIdentifier, Value: "café", Start: 0},
				{Kind: tokenDot, Value: ".", Start: 5},
				{Kind: tokenIdentifier, Value: "名前", Start: 6},
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
			name:  "hex number lowercase",
			input: "0xff",
			expected: []token{
				{Kind: tokenNumber, Value: "0xff", Start: 0},
			},
		},
		{
			name:  "hex number uppercase",
			input: "0XAB",
			expected: []token{
				{Kind: tokenNumber, Value: "0XAB", Start: 0},
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
			name:  "decimal with exponent and sign",
			input: "2.5E-3",
			expected: []token{
				{Kind: tokenNumber, Value: "2.5E-3", Start: 0},
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
			name:  "blob literal uppercase",
			input: "X'DEADBEEF'",
			expected: []token{
				{Kind: tokenBlobLiteral, Value: "DEADBEEF", Start: 0},
			},
		},
		{
			name:  "blob literal lowercase prefix",
			input: "x'AB'",
			expected: []token{
				{Kind: tokenBlobLiteral, Value: "AB", Start: 0},
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
			name:  "plain question mark parameter",
			input: "?",
			expected: []token{
				{Kind: tokenQuestionMark, Value: "?", Start: 0},
			},
		},
		{
			name:  "numbered question mark parameter",
			input: "?1",
			expected: []token{
				{Kind: tokenNumberedParameter, Value: "?1", Start: 0},
			},
		},
		{
			name:  "numbered question mark with multiple digits",
			input: "?123",
			expected: []token{
				{Kind: tokenNumberedParameter, Value: "?123", Start: 0},
			},
		},
		{
			name:  "colon named parameter",
			input: ":name",
			expected: []token{
				{Kind: tokenNamedParameter, Value: ":name", Start: 0},
			},
		},
		{
			name:  "at-sign named parameter",
			input: "@user_id",
			expected: []token{
				{Kind: tokenNamedParameter, Value: "@user_id", Start: 0},
			},
		},
		{
			name:  "dollar named parameter",
			input: "$count",
			expected: []token{
				{Kind: tokenNamedParameter, Value: "$count", Start: 0},
			},
		},
		{
			name:  "multiple parameters in expression",
			input: "? AND ?1",
			expected: []token{
				{Kind: tokenQuestionMark, Value: "?", Start: 0},
				{Kind: tokenIdentifier, Value: "AND", Start: 2},
				{Kind: tokenNumberedParameter, Value: "?1", Start: 6},
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
			name:  "double equals",
			input: "==",
			expected: []token{
				{Kind: tokenOperator, Value: "==", Start: 0},
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
			name:  "single equals",
			input: "=",
			expected: []token{
				{Kind: tokenOperator, Value: "=", Start: 0},
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
			name:  "plus",
			input: "+",
			expected: []token{
				{Kind: tokenOperator, Value: "+", Start: 0},
			},
		},
		{
			name:  "minus as operator",
			input: "a - b",
			expected: []token{
				{Kind: tokenIdentifier, Value: "a", Start: 0},
				{Kind: tokenOperator, Value: "-", Start: 2},
				{Kind: tokenIdentifier, Value: "b", Start: 4},
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
			name:              "unterminated bracket identifier",
			input:             "[column",
			expectedSubstring: "unterminated bracket identifier",
		},
		{
			name:              "unterminated quoted identifier",
			input:             `"column`,
			expectedSubstring: "unterminated quoted identifier",
		},
		{
			name:              "unterminated blob literal",
			input:             "X'DEAD",
			expectedSubstring: "unterminated blob literal",
		},
		{
			name:              "a colon cast, which SQLite does not have",
			input:             "SELECT id::text FROM t",
			expectedSubstring: `syntax error near ":": unexpected character`,
		},
		{
			name:              "an at sign with no name",
			input:             "SELECT @ FROM t",
			expectedSubstring: `syntax error near "@": unexpected character`,
		},
		{
			name:              "a dollar sign with no name",
			input:             "SELECT $ FROM t",
			expectedSubstring: `syntax error near "$": unexpected character`,
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
			name:  "select with numbered parameter",
			input: "SELECT * FROM t WHERE id = ?1",
			expected: []token{
				{Kind: tokenIdentifier, Value: "SELECT", Start: 0},
				{Kind: tokenStar, Value: "*", Start: 7},
				{Kind: tokenIdentifier, Value: "FROM", Start: 9},
				{Kind: tokenIdentifier, Value: "t", Start: 14},
				{Kind: tokenIdentifier, Value: "WHERE", Start: 16},
				{Kind: tokenIdentifier, Value: "id", Start: 22},
				{Kind: tokenOperator, Value: "=", Start: 25},
				{Kind: tokenNumberedParameter, Value: "?1", Start: 27},
			},
		},
		{
			name:  "insert with plain question marks",
			input: "INSERT INTO users (name, age) VALUES (?, ?)",
			expected: []token{
				{Kind: tokenIdentifier, Value: "INSERT", Start: 0},
				{Kind: tokenIdentifier, Value: "INTO", Start: 7},
				{Kind: tokenIdentifier, Value: "users", Start: 12},
				{Kind: tokenLeftParen, Value: "(", Start: 18},
				{Kind: tokenIdentifier, Value: "name", Start: 19},
				{Kind: tokenComma, Value: ",", Start: 23},
				{Kind: tokenIdentifier, Value: "age", Start: 25},
				{Kind: tokenRightParen, Value: ")", Start: 28},
				{Kind: tokenIdentifier, Value: "VALUES", Start: 30},
				{Kind: tokenLeftParen, Value: "(", Start: 37},
				{Kind: tokenQuestionMark, Value: "?", Start: 38},
				{Kind: tokenComma, Value: ",", Start: 39},
				{Kind: tokenQuestionMark, Value: "?", Start: 41},
				{Kind: tokenRightParen, Value: ")", Start: 42},
			},
		},
		{
			name:  "select with named parameter and arrow",
			input: "SELECT data->>'key' FROM t WHERE id = :id",
			expected: []token{
				{Kind: tokenIdentifier, Value: "SELECT", Start: 0},
				{Kind: tokenIdentifier, Value: "data", Start: 7},
				{Kind: tokenDoubleArrow, Value: "->>", Start: 11},
				{Kind: tokenString, Value: "key", Start: 14},
				{Kind: tokenIdentifier, Value: "FROM", Start: 20},
				{Kind: tokenIdentifier, Value: "t", Start: 25},
				{Kind: tokenIdentifier, Value: "WHERE", Start: 27},
				{Kind: tokenIdentifier, Value: "id", Start: 33},
				{Kind: tokenOperator, Value: "=", Start: 36},
				{Kind: tokenNamedParameter, Value: ":id", Start: 38},
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

func TestTokenise_LeadingDotNumbers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:     "leading-dot fraction",
			input:    ".5",
			expected: []token{{Kind: tokenNumber, Value: ".5", Start: 0}},
		},
		{
			name:     "leading-dot fraction with an exponent",
			input:    ".5e3",
			expected: []token{{Kind: tokenNumber, Value: ".5e3", Start: 0}},
		},
		{
			name:  "qualified column keeps its dot",
			input: "t.a",
			expected: []token{
				{Kind: tokenIdentifier, Value: "t", Start: 0},
				{Kind: tokenDot, Value: ".", Start: 1},
				{Kind: tokenIdentifier, Value: "a", Start: 2},
			},
		},
		{
			name:  "multiplied by a leading-dot fraction",
			input: "price*.5",
			expected: []token{
				{Kind: tokenIdentifier, Value: "price", Start: 0},
				{Kind: tokenStar, Value: "*", Start: 5},
				{Kind: tokenNumber, Value: ".5", Start: 6},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestTokenise_DelimitedIdentifiersAreQuoted(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input string
		value string
	}{
		{name: "double quotes", input: `"select"`, value: "select"},
		{name: "backticks", input: "`select`", value: "select"},
		{name: "brackets", input: "[select]", value: "select"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requireTokens(t, testCase.input, []token{{Kind: tokenIdentifier, Value: testCase.value, Start: 0, Quoted: true}})

			tokens, err := tokenise(testCase.input)
			require.NoError(t, err)
			require.False(t, tokens[0].IsKeyword(tokenIdentifier, "SELECT"), "a delimited identifier is never a keyword")
			require.Empty(t, tokens[0].Keyword(tokenIdentifier))
		})
	}
}

func TestTokenise_NamesContinueThroughADollarSign(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:     "an identifier with a dollar sign inside",
			input:    "price$usd",
			expected: []token{{Kind: tokenIdentifier, Value: "price$usd", Start: 0}},
		},
		{
			name:     "an identifier ending in a dollar sign",
			input:    "total$",
			expected: []token{{Kind: tokenIdentifier, Value: "total$", Start: 0}},
		},
		{
			name:     "a named parameter with a dollar sign inside",
			input:    ":a$b",
			expected: []token{{Kind: tokenNamedParameter, Value: ":a$b", Start: 0}},
		},
		{
			name:     "a dollar parameter with a dollar sign inside",
			input:    "$a$b",
			expected: []token{{Kind: tokenNamedParameter, Value: "$a$b", Start: 0}},
		},
		{
			name:  "a dollar sign after a space starts a parameter",
			input: "price $usd",
			expected: []token{
				{Kind: tokenIdentifier, Value: "price", Start: 0},
				{Kind: tokenNamedParameter, Value: "$usd", Start: 6},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestTokenise_ASigilFollowedByADollarSignNamesNoParameter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input string
	}{
		{name: "a colon", input: ":$a"},
		{name: "a dollar sign", input: "$$a"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokeniseError(t, testCase.input, "unexpected character")
		})
	}
}

func TestTokeniserSpansMatchSharedScanner(t *testing.T) {
	t.Parallel()

	files, err := lexdiff.Corpus("sqlite", "sqlite_cgo")
	require.NoError(t, err)
	samples := []string{
		"SELECT 'a''b', X'0F', `q``x`, \"r\"\"s\", [t u] /* c */ -- d\nFROM t;",
		"SELECT ?, ?1, :a, @b, $c FROM t;",
	}
	for index, sample := range samples {
		files = append(files, lexdiff.File{Path: "sample " + string(rune('a'+index)), Content: sample})
	}

	checked := 0
	var problems []string
	for _, file := range files {
		tokens, tokeniseError := tokenise(file.Content)
		if tokeniseError != nil {
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

func spanClass(source string, current token) dialecttest.TokenClass {
	switch current.Kind {
	case tokenIdentifier:
		if strings.IndexByte("`\"[", source[current.Start]) >= 0 {
			return dialecttest.TokenQuotedIdentifier
		}
		return dialecttest.TokenWord
	case tokenString, tokenBlobLiteral:
		return dialecttest.TokenString
	default:
		return dialecttest.TokenOther
	}
}
