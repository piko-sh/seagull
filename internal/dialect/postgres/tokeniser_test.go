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
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialecttest"
	"seagull.sh/seagull/internal/dialect/postgres/lexical"
	"seagull.sh/seagull/internal/testkit/lexdiff"
)

func requireTokens(t *testing.T, input string, expected []token) {
	t.Helper()
	tokens, err := tokenise(input)
	require.NoError(t, err)

	if len(tokens) > 0 && tokens[len(tokens)-1].Kind == tokenEOF {
		tokens = tokens[:len(tokens)-1]
	}
	require.Equal(t, len(expected), len(tokens), "token count mismatch for input: %q", input)
	for index := range expected {
		assert.Equal(t, expected[index].Kind, tokens[index].Kind, "token %d kind mismatch for input: %q", index, input)
		assert.Equal(t, foldedValue(expected[index].Kind, expected[index].Value, tokens[index].Quoted), tokens[index].Value, "token %d value mismatch for input: %q", index, input)
	}
}

func foldedValue(kind tokenKind, value string, quoted bool) string {
	if kind == tokenIdentifier && !quoted {
		return strings.ToLower(value)
	}
	return value
}

func requireTokeniseError(t *testing.T, input string, messageContains string) {
	t.Helper()
	_, err := tokenise(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), messageContains)
}

func TestTokenise_SingleCharTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected token
	}{
		{name: "left parenthesis", input: "(", expected: token{Kind: tokenLeftParen, Value: "("}},
		{name: "right parenthesis", input: ")", expected: token{Kind: tokenRightParen, Value: ")"}},
		{name: "left bracket", input: "[", expected: token{Kind: tokenLeftBracket, Value: "["}},
		{name: "right bracket", input: "]", expected: token{Kind: tokenRightBracket, Value: "]"}},
		{name: "comma", input: ",", expected: token{Kind: tokenComma, Value: ","}},
		{name: "semicolon", input: ";", expected: token{Kind: tokenSemicolon, Value: ";"}},
		{name: "dot", input: ".", expected: token{Kind: tokenDot, Value: "."}},
		{name: "star", input: "*", expected: token{Kind: tokenStar, Value: "*"}},
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
		expected token
	}{
		{
			name:     "simple keyword",
			input:    "SELECT",
			expected: token{Kind: tokenIdentifier, Value: "SELECT"},
		},
		{
			name:     "underscore prefix",
			input:    "_col",
			expected: token{Kind: tokenIdentifier, Value: "_col"},
		},
		{
			name:     "mixed case",
			input:    "MyTable",
			expected: token{Kind: tokenIdentifier, Value: "MyTable"},
		},
		{
			name:     "with digits",
			input:    "col1",
			expected: token{Kind: tokenIdentifier, Value: "col1"},
		},
		{
			name:     "quoted identifier with spaces",
			input:    `"my column"`,
			expected: token{Kind: tokenIdentifier, Value: "my column"},
		},
		{
			name:     "quoted identifier with escaped quotes",
			input:    `"say ""hello"""`,
			expected: token{Kind: tokenIdentifier, Value: `say "hello"`},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, []token{testCase.expected})
		})
	}
}

func TestTokenise_Numbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected token
	}{
		{
			name:     "integer",
			input:    "42",
			expected: token{Kind: tokenNumber, Value: "42"},
		},
		{
			name:     "decimal",
			input:    "3.14",
			expected: token{Kind: tokenNumber, Value: "3.14"},
		},
		{
			name:     "exponent notation",
			input:    "1e10",
			expected: token{Kind: tokenNumber, Value: "1e10"},
		},
		{
			name:     "decimal with negative exponent",
			input:    "1.5e-3",
			expected: token{Kind: tokenNumber, Value: "1.5e-3"},
		},
		{
			name:     "hexadecimal",
			input:    "0xFF",
			expected: token{Kind: tokenNumber, Value: "0xFF"},
		},
		{
			name:     "octal",
			input:    "0o17",
			expected: token{Kind: tokenNumber, Value: "0o17"},
		},
		{
			name:     "binary",
			input:    "0b1010",
			expected: token{Kind: tokenNumber, Value: "0b1010"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, []token{testCase.expected})
		})
	}
}

func TestTokenise_Strings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		input         string
		expectedKind  tokenKind
		expectedValue string
	}{
		{
			name:          "simple string",
			input:         "'hello'",
			expectedKind:  tokenString,
			expectedValue: "hello",
		},
		{
			name:          "string with escaped single quote",
			input:         "'it''s'",
			expectedKind:  tokenString,
			expectedValue: "it's",
		},
		{
			name:          "escape string with backslash sequence",
			input:         `E'hello\nworld'`,
			expectedKind:  tokenEscapeString,
			expectedValue: "hello\nworld",
		},
		{
			name:          "dollar-quoted string with empty tag",
			input:         "$$body$$",
			expectedKind:  tokenDollarString,
			expectedValue: "body",
		},
		{
			name:          "dollar-quoted string with named tag",
			input:         "$tag$body$tag$",
			expectedKind:  tokenDollarString,
			expectedValue: "body",
		},
		{
			name:          "bit string literal",
			input:         "B'1010'",
			expectedKind:  tokenBitString,
			expectedValue: "1010",
		},
		{
			name:          "lowercase escape string prefix",
			input:         `e'tab\there'`,
			expectedKind:  tokenEscapeString,
			expectedValue: "tab\there",
		},
		{
			name:          "lowercase bit string prefix",
			input:         "b'0011'",
			expectedKind:  tokenBitString,
			expectedValue: "0011",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, []token{
				{Kind: testCase.expectedKind, Value: testCase.expectedValue},
			})
		})
	}
}

func TestTokenise_Parameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected token
	}{
		{
			name:     "dollar parameter",
			input:    "$1",
			expected: token{Kind: tokenDollarParam, Value: "$1"},
		},
		{
			name:     "multi-digit dollar parameter",
			input:    "$123",
			expected: token{Kind: tokenDollarParam, Value: "$123"},
		},
		{
			name:     "named parameter",
			input:    ":name",
			expected: token{Kind: tokenNamedParam, Value: ":name"},
		},
		{
			name:     "cast operator",
			input:    "::",
			expected: token{Kind: tokenCast, Value: "::"},
		},
		{
			name:     "bare colon as operator",
			input:    ": ",
			expected: token{Kind: tokenOperator, Value: ":"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, []token{testCase.expected})
		})
	}
}

func TestTokenise_Operators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected token
	}{

		{name: "less than or equal", input: "<=", expected: token{Kind: tokenOperator, Value: "<="}},
		{name: "greater than or equal", input: ">=", expected: token{Kind: tokenOperator, Value: ">="}},
		{name: "not equal angle brackets", input: "<>", expected: token{Kind: tokenOperator, Value: "<>"}},
		{name: "not equal exclamation", input: "!=", expected: token{Kind: tokenOperator, Value: "!="}},
		{name: "string concatenation", input: "||", expected: token{Kind: tokenOperator, Value: "||"}},
		{name: "left shift", input: "<<", expected: token{Kind: tokenOperator, Value: "<<"}},
		{name: "right shift", input: ">>", expected: token{Kind: tokenOperator, Value: ">>"}},

		{name: "arrow", input: "->", expected: token{Kind: tokenArrow, Value: "->"}},
		{name: "double arrow", input: "->>", expected: token{Kind: tokenDoubleArrow, Value: "->>"}},
		{name: "hash arrow", input: "#>", expected: token{Kind: tokenHashArrow, Value: "#>"}},
		{name: "hash double arrow", input: "#>>", expected: token{Kind: tokenHashDoubleArrow, Value: "#>>"}},

		{name: "equals", input: "=", expected: token{Kind: tokenOperator, Value: "="}},
		{name: "less than", input: "<", expected: token{Kind: tokenOperator, Value: "<"}},
		{name: "greater than", input: ">", expected: token{Kind: tokenOperator, Value: ">"}},
		{name: "plus", input: "+", expected: token{Kind: tokenOperator, Value: "+"}},
		{name: "minus", input: "-", expected: token{Kind: tokenOperator, Value: "-"}},
		{name: "divide", input: "/", expected: token{Kind: tokenOperator, Value: "/"}},
		{name: "modulo", input: "%", expected: token{Kind: tokenOperator, Value: "%"}},
		{name: "tilde", input: "~", expected: token{Kind: tokenOperator, Value: "~"}},
		{name: "ampersand", input: "&", expected: token{Kind: tokenOperator, Value: "&"}},
		{name: "pipe", input: "|", expected: token{Kind: tokenOperator, Value: "|"}},
		{name: "exclamation", input: "!", expected: token{Kind: tokenOperator, Value: "!"}},
		{name: "hash", input: "#", expected: token{Kind: tokenOperator, Value: "#"}},
		{name: "caret", input: "^", expected: token{Kind: tokenOperator, Value: "^"}},

		{name: "logical and", input: "&&", expected: token{Kind: tokenOperator, Value: "&&"}},
		{name: "contains", input: "@>", expected: token{Kind: tokenOperator, Value: "@>"}},
		{name: "contained by", input: "<@", expected: token{Kind: tokenOperator, Value: "<@"}},
		{name: "case-insensitive regex", input: "~*", expected: token{Kind: tokenOperator, Value: "~*"}},
		{name: "not regex", input: "!~", expected: token{Kind: tokenOperator, Value: "!~"}},

		{name: "case-insensitive not regex", input: "!~*", expected: token{Kind: tokenOperator, Value: "!~*"}},

		{name: "jsonb key exists", input: "?", expected: token{Kind: tokenOperator, Value: "?"}},
		{name: "jsonb any key exists", input: "?|", expected: token{Kind: tokenOperator, Value: "?|"}},
		{name: "jsonb all keys exist", input: "?&", expected: token{Kind: tokenOperator, Value: "?&"}},
		{name: "jsonb path exists", input: "@?", expected: token{Kind: tokenOperator, Value: "@?"}},
		{name: "jsonb delete path", input: "#-", expected: token{Kind: tokenOperator, Value: "#-"}},
		{name: "absolute value", input: "@", expected: token{Kind: tokenOperator, Value: "@"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, []token{testCase.expected})
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
			name:     "leading and trailing whitespace is stripped",
			input:    "  SELECT  ",
			expected: []token{{Kind: tokenIdentifier, Value: "SELECT"}},
		},
		{
			name:     "tabs and newlines are treated as whitespace",
			input:    "\t\nSELECT\r\n",
			expected: []token{{Kind: tokenIdentifier, Value: "SELECT"}},
		},
		{
			name:  "line comment is skipped",
			input: "SELECT -- this is a comment\n1",
			expected: []token{
				{Kind: tokenIdentifier, Value: "SELECT"},
				{Kind: tokenNumber, Value: "1"},
			},
		},
		{
			name:  "block comment is skipped",
			input: "SELECT /* comment */ 1",
			expected: []token{
				{Kind: tokenIdentifier, Value: "SELECT"},
				{Kind: tokenNumber, Value: "1"},
			},
		},
		{
			name:     "nested block comment is fully consumed",
			input:    "/* outer /* inner */ */",
			expected: []token{},
		},
		{
			name:     "empty input produces no tokens",
			input:    "",
			expected: []token{},
		},
		{
			name:     "whitespace-only input produces no tokens",
			input:    "   \t\n  ",
			expected: []token{},
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
		name            string
		input           string
		messageContains string
	}{
		{
			name:            "unterminated string literal",
			input:           "'hello",
			messageContains: "unterminated string literal",
		},
		{
			name:            "unterminated escape string",
			input:           "E'hello",
			messageContains: "unterminated escape string",
		},
		{
			name:            "unterminated quoted identifier",
			input:           `"hello`,
			messageContains: "unterminated quoted identifier",
		},
		{
			name:            "unterminated dollar-quoted string",
			input:           "$$hello",
			messageContains: "unterminated dollar-quoted string",
		},
		{
			name:            "unterminated tagged dollar-quoted string",
			input:           "$tag$hello",
			messageContains: "unterminated dollar-quoted string",
		},
		{
			name:            "unterminated bit string",
			input:           "B'1010",
			messageContains: "unterminated bit string",
		},
		{
			name:            "unexpected character",
			input:           "SELECT \x00",
			messageContains: "unexpected character",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokeniseError(t, testCase.input, testCase.messageContains)
		})
	}
}

func TestTokenise_CompleteStatements(t *testing.T) {
	t.Parallel()

	t.Run("SELECT with WHERE clause and dollar parameter", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "SELECT id, name FROM users WHERE id = $1", []token{
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenIdentifier, Value: "id"},
			{Kind: tokenComma, Value: ","},
			{Kind: tokenIdentifier, Value: "name"},
			{Kind: tokenIdentifier, Value: "FROM"},
			{Kind: tokenIdentifier, Value: "users"},
			{Kind: tokenIdentifier, Value: "WHERE"},
			{Kind: tokenIdentifier, Value: "id"},
			{Kind: tokenOperator, Value: "="},
			{Kind: tokenDollarParam, Value: "$1"},
		})
	})

	t.Run("INSERT with multiple parameters", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "INSERT INTO foo (a, b) VALUES ($1, $2)", []token{
			{Kind: tokenIdentifier, Value: "INSERT"},
			{Kind: tokenIdentifier, Value: "INTO"},
			{Kind: tokenIdentifier, Value: "foo"},
			{Kind: tokenLeftParen, Value: "("},
			{Kind: tokenIdentifier, Value: "a"},
			{Kind: tokenComma, Value: ","},
			{Kind: tokenIdentifier, Value: "b"},
			{Kind: tokenRightParen, Value: ")"},
			{Kind: tokenIdentifier, Value: "VALUES"},
			{Kind: tokenLeftParen, Value: "("},
			{Kind: tokenDollarParam, Value: "$1"},
			{Kind: tokenComma, Value: ","},
			{Kind: tokenDollarParam, Value: "$2"},
			{Kind: tokenRightParen, Value: ")"},
		})
	})

	t.Run("multi-statement separated by semicolon", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "SELECT 1; SELECT 2", []token{
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenNumber, Value: "1"},
			{Kind: tokenSemicolon, Value: ";"},
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenNumber, Value: "2"},
		})
	})

	t.Run("SELECT with type cast", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "SELECT col::integer FROM t", []token{
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenIdentifier, Value: "col"},
			{Kind: tokenCast, Value: "::"},
			{Kind: tokenIdentifier, Value: "integer"},
			{Kind: tokenIdentifier, Value: "FROM"},
			{Kind: tokenIdentifier, Value: "t"},
		})
	})

	t.Run("JSONB arrow operator chain", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "data->'key'->>'nested'", []token{
			{Kind: tokenIdentifier, Value: "data"},
			{Kind: tokenArrow, Value: "->"},
			{Kind: tokenString, Value: "key"},
			{Kind: tokenDoubleArrow, Value: "->>"},
			{Kind: tokenString, Value: "nested"},
		})
	})

	t.Run("function call with star and dot-qualified name", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "SELECT s.name, COUNT(*) FROM schema1.users s", []token{
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenIdentifier, Value: "s"},
			{Kind: tokenDot, Value: "."},
			{Kind: tokenIdentifier, Value: "name"},
			{Kind: tokenComma, Value: ","},
			{Kind: tokenIdentifier, Value: "COUNT"},
			{Kind: tokenLeftParen, Value: "("},
			{Kind: tokenStar, Value: "*"},
			{Kind: tokenRightParen, Value: ")"},
			{Kind: tokenIdentifier, Value: "FROM"},
			{Kind: tokenIdentifier, Value: "schema1"},
			{Kind: tokenDot, Value: "."},
			{Kind: tokenIdentifier, Value: "users"},
			{Kind: tokenIdentifier, Value: "s"},
		})
	})

	t.Run("array subscript with bracket notation", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "arr[1]", []token{
			{Kind: tokenIdentifier, Value: "arr"},
			{Kind: tokenLeftBracket, Value: "["},
			{Kind: tokenNumber, Value: "1"},
			{Kind: tokenRightBracket, Value: "]"},
		})
	})
}

func TestTokenise_NonASCIIIdentifierKeepsCodepoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "accented latin identifier",
			input: "café",
		},
		{
			name:  "diaeresis identifier",
			input: "naïve",
		},
		{
			name:  "cjk identifier",
			input: "名前",
		},
		{
			name:  "mixed ascii and multi-byte with trailing digit",
			input: "user_café2",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, []token{
				{Kind: tokenIdentifier, Value: testCase.input},
			})
		})
	}
}

func TestTokenise_DollarInsideIdentifiers(t *testing.T) {
	t.Parallel()

	requireTokens(t, "amount$gbp$net $1 $$body$$", []token{
		{Kind: tokenIdentifier, Value: "amount$gbp$net"},
		{Kind: tokenDollarParam, Value: "$1"},
		{Kind: tokenDollarString, Value: "body"},
	})
}

func TestTokeniseRecordsSpansRawTextAndQuoting(t *testing.T) {
	t.Parallel()

	input := `SELECT "Order", 'it''s', E'a\nb', $q$ x $q$, .5, 1e, x=>1`
	tokens, err := tokenise(input)
	require.NoError(t, err)

	type expected struct {
		kind   tokenKind
		value  string
		raw    string
		quoted bool
	}
	want := []expected{
		{kind: tokenIdentifier, value: "SELECT", raw: "SELECT"},
		{kind: tokenIdentifier, value: "Order", raw: `"Order"`, quoted: true},
		{kind: tokenComma, value: ",", raw: ","},
		{kind: tokenString, value: "it's", raw: "'it''s'"},
		{kind: tokenComma, value: ",", raw: ","},
		{kind: tokenEscapeString, value: "a\nb", raw: `E'a\nb'`},
		{kind: tokenComma, value: ",", raw: ","},
		{kind: tokenDollarString, value: " x ", raw: "$q$ x $q$"},
		{kind: tokenComma, value: ",", raw: ","},
		{kind: tokenNumber, value: ".5", raw: ".5"},
		{kind: tokenComma, value: ",", raw: ","},
		{kind: tokenNumber, value: "1", raw: "1"},
		{kind: tokenIdentifier, value: "e", raw: "e"},
		{kind: tokenComma, value: ",", raw: ","},
		{kind: tokenIdentifier, value: "x", raw: "x"},
		{kind: tokenOperator, value: "=>", raw: "=>"},
		{kind: tokenNumber, value: "1", raw: "1"},
		{kind: tokenEOF, value: "", raw: ""},
	}
	require.Len(t, tokens, len(want))
	for index, current := range tokens {
		assert.Equal(t, want[index].kind, current.Kind, "token %d kind", index)
		assert.Equal(t, foldedValue(want[index].kind, want[index].value, want[index].quoted), current.Value, "token %d value", index)
		assert.Equal(t, want[index].raw, current.Raw, "token %d raw", index)
		assert.Equal(t, want[index].quoted, current.Quoted, "token %d quoted", index)
		assert.Equal(t, current.Raw, input[current.Start:current.End], "token %d span", index)
	}
}

func TestTokeniserReadsAColonAfterAnOperandAsPunctuation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		input     string
		wantKinds []tokenKind
	}{
		{name: "after a name", input: "lo:hi", wantKinds: []tokenKind{tokenIdentifier, tokenOperator, tokenIdentifier, tokenEOF}},
		{name: "after a number", input: "1:hi", wantKinds: []tokenKind{tokenNumber, tokenOperator, tokenIdentifier, tokenEOF}},
		{name: "after a quoted name", input: `"lo":hi`, wantKinds: []tokenKind{tokenIdentifier, tokenOperator, tokenIdentifier, tokenEOF}},
		{name: "after a closing parenthesis", input: "(lo):hi", wantKinds: []tokenKind{
			tokenLeftParen, tokenIdentifier, tokenRightParen, tokenOperator, tokenIdentifier, tokenEOF,
		}},
		{name: "after an opening bracket", input: "[:hi", wantKinds: []tokenKind{tokenLeftBracket, tokenNamedParam, tokenEOF}},
		{name: "after an operator", input: "=:hi", wantKinds: []tokenKind{tokenOperator, tokenNamedParam, tokenEOF}},
		{name: "after a space", input: "lo :hi", wantKinds: []tokenKind{tokenIdentifier, tokenNamedParam, tokenEOF}},
		{name: "a cast after a name", input: "lo::int", wantKinds: []tokenKind{tokenIdentifier, tokenCast, tokenIdentifier, tokenEOF}},
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

func TestTheDistanceOperatorsAreOneTokenEach(t *testing.T) {
	t.Parallel()

	for _, operator := range []string{"<->", "<#>", "<=>", "<+>", "<~>", "<%>", "|=|", "<<->", "<->>", "<<->>", "<<#>>", "<<<->", "<->>>"} {
		t.Run(operator, func(t *testing.T) {
			t.Parallel()

			requireTokens(t, "a "+operator+" $1", []token{
				{Kind: tokenIdentifier, Value: "a"},
				{Kind: tokenOperator, Value: operator},
				{Kind: tokenDollarParam, Value: "$1"},
			})
		})
	}
}

func TestAComparisonBeforeANegativeNumberStaysTwoTokens(t *testing.T) {
	t.Parallel()

	requireTokens(t, "a<-1", []token{
		{Kind: tokenIdentifier, Value: "a"},
		{Kind: tokenOperator, Value: "<"},
		{Kind: tokenOperator, Value: "-"},
		{Kind: tokenNumber, Value: "1"},
	})
}

func TestPrefixedLiteralsAreOneTokenWithTheirValueDecoded(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		input      string
		wantValue  string
		wantKind   tokenKind
		wantQuoted bool
	}{
		{name: "a Unicode escape string", input: `U&'d\0061t'`, wantKind: tokenString, wantValue: "dat"},
		{name: "a lower-case Unicode escape string", input: `u&'d\0061t'`, wantKind: tokenString, wantValue: "dat"},
		{name: "a six-digit Unicode escape", input: `U&'\+01F600'`, wantKind: tokenString, wantValue: "\U0001F600"},
		{name: "a surrogate pair", input: `U&'\D83D\DE00'`, wantKind: tokenString, wantValue: "\U0001F600"},
		{name: "a doubled escape character", input: `U&'a\\b'`, wantKind: tokenString, wantValue: `a\b`},
		{name: "a doubled quote", input: `U&'it''s'`, wantKind: tokenString, wantValue: "it's"},
		{name: "an escape character named by UESCAPE", input: `U&'d!0061t' UESCAPE '!'`, wantKind: tokenString, wantValue: "dat"},
		{name: "UESCAPE after a comment", input: `U&'d!0061t' /* c */ uescape '!'`, wantKind: tokenString, wantValue: "dat"},
		{name: "a Unicode escape identifier", input: `U&"n\0061me"`, wantKind: tokenIdentifier, wantValue: "name", wantQuoted: true},
		{name: "a hexadecimal bit string", input: `X'1F'`, wantKind: tokenBitString, wantValue: "1F"},
		{name: "a lower-case hexadecimal bit string", input: `x'1f'`, wantKind: tokenBitString, wantValue: "1f"},
		{name: "a national character string", input: `N'abc'`, wantKind: tokenString, wantValue: "abc"},
		{name: "a national character string with a doubled quote", input: `n'it''s'`, wantKind: tokenString, wantValue: "it's"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.input)

			require.NoError(t, err)
			require.Len(t, tokens, 2)
			assert.Equal(t, testCase.wantKind, tokens[0].Kind)
			assert.Equal(t, testCase.wantValue, tokens[0].Value)
			assert.Equal(t, testCase.wantQuoted, tokens[0].Quoted)
			assert.Equal(t, testCase.input, tokens[0].Raw)
			assert.Equal(t, 0, tokens[0].Start)
			assert.Equal(t, len(testCase.input), tokens[0].End)
		})
	}
}

func TestPrefixedLiteralsKeepTheTokensThatFollowThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input string
		want  []token
	}{
		{
			name:  "a Unicode escape string without UESCAPE before a word",
			input: `U&'a' uescaped`,
			want:  []token{{Kind: tokenString, Value: "a"}, {Kind: tokenIdentifier, Value: "uescaped"}},
		},
		{
			name:  "a name that is not a prefix",
			input: `u & 'a', name 'b'`,
			want: []token{{Kind: tokenIdentifier, Value: "u"}, {Kind: tokenOperator, Value: "&"}, {Kind: tokenString, Value: "a"},
				{Kind: tokenComma, Value: ","}, {Kind: tokenIdentifier, Value: "name"}, {Kind: tokenString, Value: "b"}},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requireTokens(t, testCase.input, testCase.want)
		})
	}
}

func TestMalformedPrefixedLiteralsAreLexicalErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		input       string
		wantMessage string
	}{
		{name: "an unterminated Unicode escape string", input: `U&'abc`, wantMessage: "unterminated Unicode escape literal"},
		{name: "a short Unicode escape", input: `U&'\00'`, wantMessage: "invalid Unicode escape"},
		{name: "a lone high surrogate", input: `U&'\D83D'`, wantMessage: "invalid Unicode escape"},
		{name: "a lone low surrogate", input: `U&'\DE00x'`, wantMessage: "invalid Unicode escape"},
		{name: "a code point past the last", input: `U&'\+110000'`, wantMessage: "invalid Unicode escape"},
		{name: "UESCAPE naming a hexadecimal digit", input: `U&'a' UESCAPE 'a'`, wantMessage: "invalid Unicode escape character"},
		{name: "UESCAPE naming two characters", input: `U&'a' UESCAPE '!!'`, wantMessage: "invalid Unicode escape character"},
		{name: "UESCAPE with no character", input: `U&'a' UESCAPE x`, wantMessage: "expected the escape character after UESCAPE"},
		{name: "an unterminated national character string", input: `N'abc`, wantMessage: "unterminated string literal"},
		{name: "an unterminated hexadecimal bit string", input: `X'1F`, wantMessage: "unterminated bit string"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requireTokeniseError(t, testCase.input, testCase.wantMessage)
		})
	}
}

func TestPrefixedLiteralsAnalyseAsTheValuesTheyWrite(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantType    string
		wantColumn  string
		wantOutputs []string
	}{
		{name: "a Unicode escape string", sql: `SELECT U&'d\0061t' AS v`, wantType: "text", wantOutputs: []string{"v"}},
		{name: "a hexadecimal bit string", sql: `SELECT X'1F' AS v`, wantType: "bit", wantOutputs: []string{"v"}},
		{name: "a national character string", sql: `SELECT N'abc' AS v`, wantType: "text", wantOutputs: []string{"v"}},
		{name: "a Unicode escape identifier names its column", sql: `SELECT U&"n\0061me" FROM users`, wantColumn: "name", wantOutputs: []string{"name"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.OutputColumns, 1)
			assert.Equal(t, testCase.wantOutputs, outputNames(analysis))
			assert.Equal(t, testCase.wantColumn, analysis.OutputColumns[0].ColumnName)
			if testCase.wantType == "" {
				return
			}
			literal, isLiteral := analysis.OutputColumns[0].Expression.(*ast.LiteralExpression)
			require.True(t, isLiteral)
			assert.Equal(t, testCase.wantType, literal.TypeName)
		})
	}
}

func TestDollarParametersPastTheBindLimitAreRejected(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "the highest parameter one statement binds", input: "$" + strconv.Itoa(New().Info().MaxParameters)},
		{name: "one past it", input: "$" + strconv.Itoa(New().Info().MaxParameters+1), wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.input)

			if testCase.wantErr {
				require.ErrorContains(t, err, "invalid parameter number: exceeds maximum of 65535")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tokenDollarParam, tokens[0].Kind)
		})
	}
}

func TestTokeniserSpansMatchSharedScanner(t *testing.T) {
	t.Parallel()

	files, err := lexdiff.Corpus("postgres", "cockroachdb", "timescaledb")
	require.NoError(t, err)
	samples := []string{
		`SELECT E'a\'b', $tag$x;y$tag$, $$z$$, B'10', "q""x", 'it''s' /* a /* b */ */ -- c`,
		"SELECT $1::int, :name, x -> 'k', y #>> '{a}' FROM t;",
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

func spanClass(source string, current token) dialecttest.TokenClass {
	switch current.Kind {
	case tokenIdentifier:
		if source[current.Start] == '"' {
			return dialecttest.TokenQuotedIdentifier
		}
		return dialecttest.TokenWord
	case tokenString, tokenEscapeString, tokenDollarString, tokenBitString:
		return dialecttest.TokenString
	default:
		return dialecttest.TokenOther
	}
}

func TestSingleCharacterPunctuationIsOneTokenOfItsKind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		input    string
		wantKind tokenKind
	}{
		{input: "(", wantKind: tokenLeftParen},
		{input: ")", wantKind: tokenRightParen},
		{input: "[", wantKind: tokenLeftBracket},
		{input: "]", wantKind: tokenRightBracket},
		{input: ",", wantKind: tokenComma},
		{input: ";", wantKind: tokenSemicolon},
		{input: ".", wantKind: tokenDot},
		{input: "*", wantKind: tokenStar},
		{input: "+", wantKind: tokenOperator},
	}
	for _, testCase := range testCases {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.input)

			require.NoError(t, err)
			require.NotEmpty(t, tokens)
			assert.Equal(t, testCase.wantKind, tokens[0].Kind)
			assert.Equal(t, testCase.input, tokens[0].Raw)
		})
	}
}

func tokenise(input string) ([]token, error) {
	next := readStatementTokens(input, 0, len(input))
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
