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

package mysql

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/dialecttest"
	"seagull.sh/seagull/internal/dialect/mysql/lexical"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/testkit/lexdiff"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func requireTokens(t *testing.T, input string, expected []token) {
	t.Helper()
	requireTokensFrom(t, tokenise, input, expected)
}

func requireTokensFrom(t *testing.T, read func(string) ([]token, error), input string, expected []token) {
	t.Helper()
	tokens, err := read(input)
	require.NoError(t, err)
	if len(tokens) > 0 && tokens[len(tokens)-1].Kind == tokenEOF {
		tokens = tokens[:len(tokens)-1]
	}
	require.Equal(t, len(expected), len(tokens), "token count mismatch for input: %q", input)
	for index := range expected {
		assert.Equal(t, expected[index].Kind, tokens[index].Kind, "token %d kind mismatch for input: %q", index, input)
		assert.Equal(t, expected[index].Value, tokens[index].Value, "token %d value mismatch for input: %q", index, input)
	}
}

func requireTokeniseError(t *testing.T, input string, messageContains string) {
	t.Helper()
	requireTokeniseErrorFrom(t, tokenise, input, messageContains)
}

func requireTokeniseErrorFrom(t *testing.T, read func(string) ([]token, error), input string, messageContains string) {
	t.Helper()
	_, err := read(input)
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
		{
			name:  "left parenthesis",
			input: "(",
			expected: token{
				Kind:  tokenLeftParen,
				Value: "(",
			},
		},
		{
			name:  "right parenthesis",
			input: ")",
			expected: token{
				Kind:  tokenRightParen,
				Value: ")",
			},
		},
		{
			name:  "left bracket",
			input: "[",
			expected: token{
				Kind:  tokenLeftBracket,
				Value: "[",
			},
		},
		{
			name:  "right bracket",
			input: "]",
			expected: token{
				Kind:  tokenRightBracket,
				Value: "]",
			},
		},
		{
			name:  "comma",
			input: ",",
			expected: token{
				Kind:  tokenComma,
				Value: ",",
			},
		},
		{
			name:  "semicolon",
			input: ";",
			expected: token{
				Kind:  tokenSemicolon,
				Value: ";",
			},
		},
		{
			name:  "dot",
			input: ".",
			expected: token{
				Kind:  tokenDot,
				Value: ".",
			},
		},
		{
			name:  "star",
			input: "*",
			expected: token{
				Kind:  tokenStar,
				Value: "*",
			},
		},
		{
			name:  "question mark",
			input: "?",
			expected: token{
				Kind:  tokenQuestionMark,
				Value: "?",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			requireTokens(t, testCase.input, []token{testCase.expected})
		})
	}

	t.Run("all single-character tokens in sequence", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "( ) [ ] , ; . * ?", []token{
			{Kind: tokenLeftParen, Value: "("},
			{Kind: tokenRightParen, Value: ")"},
			{Kind: tokenLeftBracket, Value: "["},
			{Kind: tokenRightBracket, Value: "]"},
			{Kind: tokenComma, Value: ","},
			{Kind: tokenSemicolon, Value: ";"},
			{Kind: tokenDot, Value: "."},
			{Kind: tokenStar, Value: "*"},
			{Kind: tokenQuestionMark, Value: "?"},
		})
	})
}

func TestTokenise_Identifiers(t *testing.T) {
	t.Parallel()

	t.Run("simple identifier", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "users", []token{
			{Kind: tokenIdentifier, Value: "users"},
		})
	})

	t.Run("identifier with digits and underscores", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "user_id_2", []token{
			{Kind: tokenIdentifier, Value: "user_id_2"},
		})
	})

	t.Run("identifier starting with underscore", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "_private", []token{
			{Kind: tokenIdentifier, Value: "_private"},
		})
	})

	t.Run("backtick-quoted identifier", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "`column`", []token{
			{Kind: tokenIdentifier, Value: "column"},
		})
	})

	t.Run("backtick-quoted identifier with spaces", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "`my column`", []token{
			{Kind: tokenIdentifier, Value: "my column"},
		})
	})

	t.Run("backtick-quoted identifier with escaped backtick", func(t *testing.T) {
		t.Parallel()

		requireTokens(t, "`col``name`", []token{
			{Kind: tokenIdentifier, Value: "col`name"},
		})
	})

	t.Run("double-quoted identifier under ANSI_QUOTES", func(t *testing.T) {
		t.Parallel()
		requireTokensFrom(t, tokeniseANSIQuotes, `"table_name"`, []token{
			{Kind: tokenIdentifier, Value: "table_name"},
		})
	})

	t.Run("double-quoted identifier with escaped double quote under ANSI_QUOTES", func(t *testing.T) {
		t.Parallel()
		requireTokensFrom(t, tokeniseANSIQuotes, `"table""name"`, []token{
			{Kind: tokenIdentifier, Value: `table"name`},
		})
	})

	t.Run("double-quoted string in the default mode", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, `"table""name"`, []token{
			{Kind: tokenString, Value: `table"name`},
		})
	})

	t.Run("multiple identifiers separated by dot", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "db.users", []token{
			{Kind: tokenIdentifier, Value: "db"},
			{Kind: tokenDot, Value: "."},
			{Kind: tokenIdentifier, Value: "users"},
		})
	})

	t.Run("mixed bare and backtick identifiers", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "`schema`.table_name", []token{
			{Kind: tokenIdentifier, Value: "schema"},
			{Kind: tokenDot, Value: "."},
			{Kind: tokenIdentifier, Value: "table_name"},
		})
	})
}

func TestTokenise_UnicodeIdentifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "identifier with combining accent letter",
			input: "café",
		},
		{
			name:  "identifier with CJK letters",
			input: "名前",
		},
		{
			name:  "identifier with diaeresis letter",
			input: "naïve",
		},
		{
			name:  "mixed ASCII and multi-byte letter with trailing digit",
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

	t.Run("multi-byte identifier qualified by dot", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "café.名前", []token{
			{Kind: tokenIdentifier, Value: "café"},
			{Kind: tokenDot, Value: "."},
			{Kind: tokenIdentifier, Value: "名前"},
		})
	})

	t.Run("ASCII identifier still tokenises unchanged", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "user_id_2", []token{
			{Kind: tokenIdentifier, Value: "user_id_2"},
		})
	})
}

func TestTokenise_Numbers(t *testing.T) {
	t.Parallel()

	t.Run("integer", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "42", []token{
			{Kind: tokenNumber, Value: "42"},
		})
	})

	t.Run("decimal", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "3.14", []token{
			{Kind: tokenNumber, Value: "3.14"},
		})
	})

	t.Run("decimal starting with zero", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "0.5", []token{
			{Kind: tokenNumber, Value: "0.5"},
		})
	})

	t.Run("exponent notation", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "1e10", []token{
			{Kind: tokenNumber, Value: "1e10"},
		})
	})

	t.Run("exponent with uppercase E", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "2E5", []token{
			{Kind: tokenNumber, Value: "2E5"},
		})
	})

	t.Run("exponent with positive sign", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "1.5e+3", []token{
			{Kind: tokenNumber, Value: "1.5e+3"},
		})
	})

	t.Run("exponent with negative sign", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "6.022e-23", []token{
			{Kind: tokenNumber, Value: "6.022e-23"},
		})
	})

	t.Run("hexadecimal number", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "0xFF", []token{
			{Kind: tokenNumber, Value: "0xFF"},
		})
	})

	t.Run("hexadecimal number lowercase prefix", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "0xDEAD", []token{
			{Kind: tokenNumber, Value: "0xDEAD"},
		})
	})

	t.Run("binary number", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "0b1010", []token{
			{Kind: tokenNumber, Value: "0b1010"},
		})
	})

	t.Run("binary number uppercase prefix", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "0B110", []token{
			{Kind: tokenNumber, Value: "0B110"},
		})
	})
}

func TestTokenise_Strings(t *testing.T) {
	t.Parallel()

	t.Run("simple string", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "'hello'", []token{
			{Kind: tokenString, Value: "hello"},
		})
	})

	t.Run("empty string", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "''", []token{
			{Kind: tokenString, Value: ""},
		})
	})

	t.Run("string with escaped single quote (doubled)", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "'it''s'", []token{
			{Kind: tokenString, Value: "it's"},
		})
	})

	t.Run("string with backslash escape newline", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, `'line\none'`, []token{
			{Kind: tokenString, Value: "line\none"},
		})
	})

	t.Run("string with backslash escape tab", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, `'col\tcol'`, []token{
			{Kind: tokenString, Value: "col\tcol"},
		})
	})

	t.Run("string with backslash escape carriage return", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, `'cr\rhere'`, []token{
			{Kind: tokenString, Value: "cr\rhere"},
		})
	})

	t.Run("string with backslash escape null", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, `'null\0byte'`, []token{
			{Kind: tokenString, Value: "null\x00byte"},
		})
	})

	t.Run("string with backslash escape substitute character", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, `'sub\Z'`, []token{
			{Kind: tokenString, Value: "sub\x1A"},
		})
	})

	t.Run("string with backslash escape backspace", func(t *testing.T) {
		t.Parallel()

		requireTokens(t, `'a\bc'`, []token{
			{Kind: tokenString, Value: "a\x08c"},
		})
	})

	t.Run("string with backslash escaped percent keeps backslash", func(t *testing.T) {
		t.Parallel()

		requireTokens(t, `'50\% off'`, []token{
			{Kind: tokenString, Value: `50\% off`},
		})
	})

	t.Run("string with backslash escaped underscore keeps backslash", func(t *testing.T) {
		t.Parallel()

		requireTokens(t, `'a\_b'`, []token{
			{Kind: tokenString, Value: `a\_b`},
		})
	})

	t.Run("string with backslash escaped backslash", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, `'back\\slash'`, []token{
			{Kind: tokenString, Value: `back\slash`},
		})
	})

	t.Run("string with backslash escaped single quote", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, `'it\'s'`, []token{
			{Kind: tokenString, Value: "it's"},
		})
	})

	t.Run("hex string uppercase prefix", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "X'DEADBEEF'", []token{
			{Kind: tokenHexString, Value: "DEADBEEF"},
		})
	})

	t.Run("hex string lowercase prefix", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "x'ab01'", []token{
			{Kind: tokenHexString, Value: "ab01"},
		})
	})

	t.Run("empty hex string", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "X''", []token{
			{Kind: tokenHexString, Value: ""},
		})
	})

	t.Run("bit string uppercase prefix", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "B'1010'", []token{
			{Kind: tokenBitString, Value: "1010"},
		})
	})

	t.Run("bit string lowercase prefix", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "b'110'", []token{
			{Kind: tokenBitString, Value: "110"},
		})
	})
}

func TestTokenise_Parameters(t *testing.T) {
	t.Parallel()

	t.Run("question mark parameter", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "?", []token{
			{Kind: tokenQuestionMark, Value: "?"},
		})
	})

	t.Run("named parameter with colon prefix", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, ":name", []token{
			{Kind: tokenNamedParameter, Value: ":name"},
		})
	})

	t.Run("user variable", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "@myvar", []token{
			{Kind: tokenUserVariable, Value: "@myvar"},
		})
	})

	t.Run("user variable with underscores and digits", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "@user_id_1", []token{
			{Kind: tokenUserVariable, Value: "@user_id_1"},
		})
	})

	t.Run("system variable with global qualifier", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "@@global.max_connections", []token{
			{Kind: tokenSystemVariable, Value: "@@global.max_connections"},
		})
	})

	t.Run("system variable with session qualifier", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "@@session.wait_timeout", []token{
			{Kind: tokenSystemVariable, Value: "@@session.wait_timeout"},
		})
	})

	t.Run("system variable with local qualifier", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "@@local.sort_buffer_size", []token{
			{Kind: tokenSystemVariable, Value: "@@local.sort_buffer_size"},
		})
	})

	t.Run("system variable without qualifier", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "@@version", []token{
			{Kind: tokenSystemVariable, Value: "@@version"},
		})
	})

	t.Run("colon without following identifier is operator", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, ":", []token{
			{Kind: tokenOperator, Value: ":"},
		})
	})

	t.Run("multiple question mark parameters", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "?, ?", []token{
			{Kind: tokenQuestionMark, Value: "?"},
			{Kind: tokenComma, Value: ","},
			{Kind: tokenQuestionMark, Value: "?"},
		})
	})
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
				{Kind: tokenOperator, Value: "<="},
			},
		},
		{
			name:  "greater than or equal",
			input: ">=",
			expected: []token{
				{Kind: tokenOperator, Value: ">="},
			},
		},
		{
			name:  "not equal (angle brackets)",
			input: "<>",
			expected: []token{
				{Kind: tokenOperator, Value: "<>"},
			},
		},
		{
			name:  "not equal (exclamation)",
			input: "!=",
			expected: []token{
				{Kind: tokenOperator, Value: "!="},
			},
		},
		{
			name:  "null-safe equality",
			input: "<=>",
			expected: []token{
				{Kind: tokenOperator, Value: "<=>"},
			},
		},
		{
			name:  "left shift",
			input: "<<",
			expected: []token{
				{Kind: tokenOperator, Value: "<<"},
			},
		},
		{
			name:  "right shift",
			input: ">>",
			expected: []token{
				{Kind: tokenOperator, Value: ">>"},
			},
		},
		{
			name:  "arrow operator",
			input: "->",
			expected: []token{
				{Kind: tokenArrow, Value: "->"},
			},
		},
		{
			name:  "double arrow operator",
			input: "->>",
			expected: []token{
				{Kind: tokenDoubleArrow, Value: "->>"},
			},
		},
		{
			name:  "logical or (double pipe)",
			input: "||",
			expected: []token{
				{Kind: tokenOperator, Value: "||"},
			},
		},
		{
			name:  "equals",
			input: "=",
			expected: []token{
				{Kind: tokenOperator, Value: "="},
			},
		},
		{
			name:  "less than",
			input: "<",
			expected: []token{
				{Kind: tokenOperator, Value: "<"},
			},
		},
		{
			name:  "greater than",
			input: ">",
			expected: []token{
				{Kind: tokenOperator, Value: ">"},
			},
		},
		{
			name:  "plus",
			input: "+",
			expected: []token{
				{Kind: tokenOperator, Value: "+"},
			},
		},
		{
			name:  "minus",
			input: "-",
			expected: []token{
				{Kind: tokenOperator, Value: "-"},
			},
		},
		{
			name:  "forward slash (division)",
			input: "/",
			expected: []token{
				{Kind: tokenOperator, Value: "/"},
			},
		},
		{
			name:  "percent (modulo)",
			input: "%",
			expected: []token{
				{Kind: tokenOperator, Value: "%"},
			},
		},
		{
			name:  "tilde (bitwise not)",
			input: "~",
			expected: []token{
				{Kind: tokenOperator, Value: "~"},
			},
		},
		{
			name:  "ampersand (bitwise and)",
			input: "&",
			expected: []token{
				{Kind: tokenOperator, Value: "&"},
			},
		},
		{
			name:  "pipe (bitwise or)",
			input: "|",
			expected: []token{
				{Kind: tokenOperator, Value: "|"},
			},
		},
		{
			name:  "exclamation (logical not)",
			input: "!",
			expected: []token{
				{Kind: tokenOperator, Value: "!"},
			},
		},
		{
			name:  "caret (bitwise xor)",
			input: "^",
			expected: []token{
				{Kind: tokenOperator, Value: "^"},
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

	t.Run("spaces between tokens are skipped", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "  a   b  ", []token{
			{Kind: tokenIdentifier, Value: "a"},
			{Kind: tokenIdentifier, Value: "b"},
		})
	})

	t.Run("tabs and newlines are skipped", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "a\t\nb", []token{
			{Kind: tokenIdentifier, Value: "a"},
			{Kind: tokenIdentifier, Value: "b"},
		})
	})

	t.Run("carriage return is skipped", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "a\r\nb", []token{
			{Kind: tokenIdentifier, Value: "a"},
			{Kind: tokenIdentifier, Value: "b"},
		})
	})

	t.Run("line comment with double dash and space", func(t *testing.T) {
		t.Parallel()

		requireTokens(t, "a -- this is a comment\nb", []token{
			{Kind: tokenIdentifier, Value: "a"},
			{Kind: tokenIdentifier, Value: "b"},
		})
	})

	t.Run("line comment with double dash and tab", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "a --\tcomment here\nb", []token{
			{Kind: tokenIdentifier, Value: "a"},
			{Kind: tokenIdentifier, Value: "b"},
		})
	})

	t.Run("double dash without space is not a comment", func(t *testing.T) {
		t.Parallel()

		requireTokens(t, "a --b", []token{
			{Kind: tokenIdentifier, Value: "a"},
			{Kind: tokenOperator, Value: "-"},
			{Kind: tokenOperator, Value: "-"},
			{Kind: tokenIdentifier, Value: "b"},
		})
	})

	t.Run("hash line comment", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "a # comment\nb", []token{
			{Kind: tokenIdentifier, Value: "a"},
			{Kind: tokenIdentifier, Value: "b"},
		})
	})

	t.Run("hash comment at end of input", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "a # trailing comment", []token{
			{Kind: tokenIdentifier, Value: "a"},
		})
	})

	t.Run("block comment", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "a /* block */ b", []token{
			{Kind: tokenIdentifier, Value: "a"},
			{Kind: tokenIdentifier, Value: "b"},
		})
	})

	t.Run("block comment spanning multiple lines", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "a /* line1\nline2 */ b", []token{
			{Kind: tokenIdentifier, Value: "a"},
			{Kind: tokenIdentifier, Value: "b"},
		})
	})

	t.Run("unterminated block comment is an error", func(t *testing.T) {
		t.Parallel()

		requireTokeniseError(t, "a /* never closed", "unterminated block comment")
	})

	t.Run("empty input yields no tokens", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "", []token{})
	})

	t.Run("whitespace-only input yields no tokens", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "   \t\n\r  ", []token{})
	})

	t.Run("consecutive comments of different styles", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "-- comment\n# another\n/* block */\na", []token{

			{Kind: tokenIdentifier, Value: "a"},
		})
	})
}

func TestTokenise_Errors(t *testing.T) {
	t.Parallel()

	t.Run("unterminated single-quoted string", func(t *testing.T) {
		t.Parallel()
		requireTokeniseError(t, "'hello", "unterminated string literal")
	})

	t.Run("unterminated string with escaped quote at end", func(t *testing.T) {
		t.Parallel()
		requireTokeniseError(t, `'hello\'`, "unterminated string literal")
	})

	t.Run("unterminated backtick identifier", func(t *testing.T) {
		t.Parallel()
		requireTokeniseError(t, "`column", "unterminated quoted identifier")
	})

	t.Run("unterminated double-quoted identifier under ANSI_QUOTES", func(t *testing.T) {
		t.Parallel()
		requireTokeniseErrorFrom(t, tokeniseANSIQuotes, `"column`, "unterminated quoted identifier")
	})

	t.Run("unterminated double-quoted string in the default mode", func(t *testing.T) {
		t.Parallel()
		requireTokeniseError(t, `"column`, "unterminated string literal")
	})

	t.Run("unterminated hex string", func(t *testing.T) {
		t.Parallel()
		requireTokeniseError(t, "X'DEAD", "unterminated hex string")
	})

	t.Run("unterminated bit string", func(t *testing.T) {
		t.Parallel()
		requireTokeniseError(t, "B'101", "unterminated bit string")
	})

	t.Run("unexpected character", func(t *testing.T) {
		t.Parallel()
		requireTokeniseError(t, "$", "unexpected character")
	})
}

func TestTokenise_CompleteStatements(t *testing.T) {
	t.Parallel()

	t.Run("simple select with question mark parameter", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "SELECT id FROM users WHERE id = ?", []token{
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenIdentifier, Value: "id"},
			{Kind: tokenIdentifier, Value: "FROM"},
			{Kind: tokenIdentifier, Value: "users"},
			{Kind: tokenIdentifier, Value: "WHERE"},
			{Kind: tokenIdentifier, Value: "id"},
			{Kind: tokenOperator, Value: "="},
			{Kind: tokenQuestionMark, Value: "?"},
		})
	})

	t.Run("insert with multiple question mark parameters", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "INSERT INTO foo VALUES (?, ?)", []token{
			{Kind: tokenIdentifier, Value: "INSERT"},
			{Kind: tokenIdentifier, Value: "INTO"},
			{Kind: tokenIdentifier, Value: "foo"},
			{Kind: tokenIdentifier, Value: "VALUES"},
			{Kind: tokenLeftParen, Value: "("},
			{Kind: tokenQuestionMark, Value: "?"},
			{Kind: tokenComma, Value: ","},
			{Kind: tokenQuestionMark, Value: "?"},
			{Kind: tokenRightParen, Value: ")"},
		})
	})

	t.Run("multi-statement input", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "SELECT 1; SELECT 2", []token{
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenNumber, Value: "1"},
			{Kind: tokenSemicolon, Value: ";"},
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenNumber, Value: "2"},
		})
	})

	t.Run("select star with backtick-quoted table", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "SELECT * FROM `order`", []token{
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenStar, Value: "*"},
			{Kind: tokenIdentifier, Value: "FROM"},
			{Kind: tokenIdentifier, Value: "order"},
		})
	})

	t.Run("select with user variable assignment", func(t *testing.T) {
		t.Parallel()

		requireTokens(t, "SELECT @total := COUNT(*) FROM items", []token{
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenUserVariable, Value: "@total"},
			{Kind: tokenOperator, Value: ":"},
			{Kind: tokenOperator, Value: "="},
			{Kind: tokenIdentifier, Value: "COUNT"},
			{Kind: tokenLeftParen, Value: "("},
			{Kind: tokenStar, Value: "*"},
			{Kind: tokenRightParen, Value: ")"},
			{Kind: tokenIdentifier, Value: "FROM"},
			{Kind: tokenIdentifier, Value: "items"},
		})
	})

	t.Run("JSON extraction with arrow operators", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "SELECT data->'$.name', data->>'$.email' FROM profiles", []token{
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenIdentifier, Value: "data"},
			{Kind: tokenArrow, Value: "->"},
			{Kind: tokenString, Value: "$.name"},
			{Kind: tokenComma, Value: ","},
			{Kind: tokenIdentifier, Value: "data"},
			{Kind: tokenDoubleArrow, Value: "->>"},
			{Kind: tokenString, Value: "$.email"},
			{Kind: tokenIdentifier, Value: "FROM"},
			{Kind: tokenIdentifier, Value: "profiles"},
		})
	})

	t.Run("null-safe equality in where clause", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "SELECT * FROM t WHERE a <=> b", []token{
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenStar, Value: "*"},
			{Kind: tokenIdentifier, Value: "FROM"},
			{Kind: tokenIdentifier, Value: "t"},
			{Kind: tokenIdentifier, Value: "WHERE"},
			{Kind: tokenIdentifier, Value: "a"},
			{Kind: tokenOperator, Value: "<=>"},
			{Kind: tokenIdentifier, Value: "b"},
		})
	})

	t.Run("select with system variable", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "SELECT @@global.max_connections", []token{
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenSystemVariable, Value: "@@global.max_connections"},
		})
	})

	t.Run("insert with hex string value", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "INSERT INTO blobs (data) VALUES (X'FF00')", []token{
			{Kind: tokenIdentifier, Value: "INSERT"},
			{Kind: tokenIdentifier, Value: "INTO"},
			{Kind: tokenIdentifier, Value: "blobs"},
			{Kind: tokenLeftParen, Value: "("},
			{Kind: tokenIdentifier, Value: "data"},
			{Kind: tokenRightParen, Value: ")"},
			{Kind: tokenIdentifier, Value: "VALUES"},
			{Kind: tokenLeftParen, Value: "("},
			{Kind: tokenHexString, Value: "FF00"},
			{Kind: tokenRightParen, Value: ")"},
		})
	})

	t.Run("statement with inline comment", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "SELECT /* hint */ id FROM users", []token{
			{Kind: tokenIdentifier, Value: "SELECT"},
			{Kind: tokenIdentifier, Value: "id"},
			{Kind: tokenIdentifier, Value: "FROM"},
			{Kind: tokenIdentifier, Value: "users"},
		})
	})

	t.Run("create table with various column types", func(t *testing.T) {
		t.Parallel()
		requireTokens(t, "CREATE TABLE `t` (`id` INT, `name` VARCHAR(255))", []token{
			{Kind: tokenIdentifier, Value: "CREATE"},
			{Kind: tokenIdentifier, Value: "TABLE"},
			{Kind: tokenIdentifier, Value: "t"},
			{Kind: tokenLeftParen, Value: "("},
			{Kind: tokenIdentifier, Value: "id"},
			{Kind: tokenIdentifier, Value: "INT"},
			{Kind: tokenComma, Value: ","},
			{Kind: tokenIdentifier, Value: "name"},
			{Kind: tokenIdentifier, Value: "VARCHAR"},
			{Kind: tokenLeftParen, Value: "("},
			{Kind: tokenNumber, Value: "255"},
			{Kind: tokenRightParen, Value: ")"},
			{Kind: tokenRightParen, Value: ")"},
		})
	})
}

func TestTokenise_DollarInsideIdentifiers(t *testing.T) {
	t.Parallel()

	requireTokens(t, "amount$gbp, net$", []token{
		{Kind: tokenIdentifier, Value: "amount$gbp"},
		{Kind: tokenComma, Value: ","},
		{Kind: tokenIdentifier, Value: "net$"},
	})
	requireTokeniseError(t, "$net", "unexpected character")
}

func TestTokenise_BackslashEscapesInsideDoubleQuotes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		read     func(string) ([]token, error)
		input    string
		expected []token
	}{
		{
			name:     "a backslash escapes in strings in the default mode",
			read:     tokenise,
			input:    `"say \"hi\"" "a""b"`,
			expected: []token{{Kind: tokenString, Value: `say "hi"`}, {Kind: tokenString, Value: `a"b`}},
		},
		{
			name:     "a backslash is a character of an identifier under ANSI_QUOTES",
			read:     tokeniseANSIQuotes,
			input:    `"a\" "b""c"`,
			expected: []token{{Kind: tokenIdentifier, Value: `a\`}, {Kind: tokenIdentifier, Value: `b"c`}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requireTokensFrom(t, testCase.read, testCase.input, testCase.expected)
		})
	}
	requireTokeniseErrorFrom(t, tokenise, `"open \"`, `syntax error near "\"": unterminated string literal`)
	requireTokeniseErrorFrom(t, tokeniseANSIQuotes, `"open`, `syntax error near "\"": unterminated quoted identifier`)
}

func TestTokeniserMarksDelimitedIdentifiersAndKeepsTheirSource(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		input  string
		value  string
		raw    string
		quoted bool
	}{
		{name: "bare word", input: "select", value: "select", raw: "select", quoted: false},
		{name: "backtick identifier", input: "`select`", value: "select", raw: "`select`", quoted: true},
		{name: "doubled backtick", input: "`a``b`", value: "a`b", raw: "`a``b`", quoted: true},
		{name: "double-quoted identifier under ANSI_QUOTES", input: `"where"`, value: "where", raw: `"where"`, quoted: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokeniseANSIQuotes(testCase.input)
			require.NoError(t, err)

			require.Equal(t, tokenIdentifier, tokens[0].Kind)
			require.Equal(t, testCase.value, tokens[0].Value)
			require.Equal(t, testCase.raw, tokens[0].Raw)
			require.Equal(t, testCase.quoted, tokens[0].Quoted)
			require.Equal(t, len(testCase.input), tokens[0].End)
			require.Equal(t, testCase.quoted, !tokens[0].IsKeyword(tokenIdentifier, testCase.value))
		})
	}
}

func TestTokeniserReadsLeadingDotNumbers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:     "leading-dot number after a keyword",
			input:    "SELECT .5",
			expected: []token{{Kind: tokenIdentifier, Value: "SELECT"}, {Kind: tokenNumber, Value: ".5"}},
		},
		{
			name:     "leading-dot number after an operator",
			input:    "price*.25e1",
			expected: []token{{Kind: tokenIdentifier, Value: "price"}, {Kind: tokenStar, Value: "*"}, {Kind: tokenNumber, Value: ".25e1"}},
		},
		{
			name:  "dot straight after a name qualifies it",
			input: "t.5col",
			expected: []token{
				{Kind: tokenIdentifier, Value: "t"}, {Kind: tokenDot, Value: "."},
				{Kind: tokenNumber, Value: "5"}, {Kind: tokenIdentifier, Value: "col"},
			},
		},
		{
			name:     "exponent without digits is not consumed",
			input:    "1ex",
			expected: []token{{Kind: tokenNumber, Value: "1"}, {Kind: tokenIdentifier, Value: "ex"}},
		},
		{
			name:     "double ampersand",
			input:    "a && b",
			expected: []token{{Kind: tokenIdentifier, Value: "a"}, {Kind: tokenOperator, Value: "&&"}, {Kind: tokenIdentifier, Value: "b"}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requireTokens(t, testCase.input, testCase.expected)
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
		{name: "a statement label", input: "l1:LOOP", wantKinds: []tokenKind{tokenIdentifier, tokenOperator, tokenIdentifier, tokenEOF}},
		{name: "after a backtick name", input: "`l1`:LOOP", wantKinds: []tokenKind{tokenIdentifier, tokenOperator, tokenIdentifier, tokenEOF}},
		{name: "after an operator", input: "=:id", wantKinds: []tokenKind{tokenOperator, tokenNamedParameter, tokenEOF}},
		{name: "after an opening parenthesis", input: "(:id", wantKinds: []tokenKind{tokenLeftParen, tokenNamedParameter, tokenEOF}},
		{name: "after a space", input: "id :id", wantKinds: []tokenKind{tokenIdentifier, tokenNamedParameter, tokenEOF}},
		{name: "an assignment", input: "@total:=1", wantKinds: []tokenKind{tokenUserVariable, tokenOperator, tokenOperator, tokenNumber, tokenEOF}},
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

func TestTokeniserReadsAnExecutableCommentOpenerInsideAnOrdinaryCommentAsComment(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:  "a MySQL opener in a double dash comment",
			input: "SELECT id -- legacy /*! syntax\nFROM users",
			expected: []token{{Kind: tokenIdentifier, Value: "SELECT"}, {Kind: tokenIdentifier, Value: "id"},
				{Kind: tokenIdentifier, Value: "FROM"}, {Kind: tokenIdentifier, Value: "users"}},
		},
		{
			name:  "a MariaDB opener in a double dash comment",
			input: "SELECT id -- legacy /*M!100000 syntax\nFROM users",
			expected: []token{{Kind: tokenIdentifier, Value: "SELECT"}, {Kind: tokenIdentifier, Value: "id"},
				{Kind: tokenIdentifier, Value: "FROM"}, {Kind: tokenIdentifier, Value: "users"}},
		},
		{
			name:  "a MySQL opener in a hash comment",
			input: "SELECT id # legacy /*!50000 syntax\nFROM users",
			expected: []token{{Kind: tokenIdentifier, Value: "SELECT"}, {Kind: tokenIdentifier, Value: "id"},
				{Kind: tokenIdentifier, Value: "FROM"}, {Kind: tokenIdentifier, Value: "users"}},
		},
		{
			name:  "a MariaDB opener in a hash comment",
			input: "SELECT id # legacy /*M! syntax\nFROM users",
			expected: []token{{Kind: tokenIdentifier, Value: "SELECT"}, {Kind: tokenIdentifier, Value: "id"},
				{Kind: tokenIdentifier, Value: "FROM"}, {Kind: tokenIdentifier, Value: "users"}},
		},
		{
			name:  "an opener in an ordinary block comment",
			input: "SELECT id /* legacy /*! syntax */ FROM users",
			expected: []token{{Kind: tokenIdentifier, Value: "SELECT"}, {Kind: tokenIdentifier, Value: "id"},
				{Kind: tokenIdentifier, Value: "FROM"}, {Kind: tokenIdentifier, Value: "users"}},
		},
		{
			name:  "an executable comment after a line comment is still read",
			input: "SELECT id -- note\n/*!50000 , name */ FROM users",
			expected: []token{{Kind: tokenIdentifier, Value: "SELECT"}, {Kind: tokenIdentifier, Value: "id"},
				{Kind: tokenComma, Value: ","}, {Kind: tokenIdentifier, Value: "name"},
				{Kind: tokenIdentifier, Value: "FROM"}, {Kind: tokenIdentifier, Value: "users"}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requireTokens(t, testCase.input, testCase.expected)
		})
	}
}

func TestParseStatementsTokenisesALargeDumpInLinearTime(t *testing.T) {
	t.Parallel()

	const (
		smallerBytes = 1 << 20
		largerBytes  = 4 * smallerBytes
	)

	dump := func(size int) string {
		const row = "(1,'a text value that pads the row out to the width a dump writes',NULL,0x1F) /* note */,\n"
		var builder strings.Builder
		builder.WriteString("INSERT INTO t VALUES ")
		for builder.Len() < size {
			builder.WriteString(row)
		}
		builder.WriteString("(2,'last',NULL,0x00)")
		return builder.String()
	}
	engine := New(WithMaxTokensPerStatement(dialectkit.MaxTokensPerStatementCeiling))
	parse := func(sql string) func() {
		return func() {
			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)
			require.False(t, statements[0].Raw.(*parsedStatement).overBudget, "the whole dump must be tokenised")
		}
	}
	smaller, larger := dump(smallerBytes), dump(largerBytes)

	scaling.Linear(t, parse(smaller), parse(larger))
	scaling.Within(t, 2*time.Second, func(ctx context.Context) {
		_, err := engine.ParseStatements(ctx, larger)
		require.NoError(t, err)
	})
}

func TestTokeniserReadsASystemVariableQualifierWrittenInAnyCase(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "an upper-case GLOBAL", input: "@@GLOBAL.time_zone", want: "@@GLOBAL.time_zone"},
		{name: "a mixed-case Session", input: "@@Session.sql_mode", want: "@@Session.sql_mode"},
		{name: "an upper-case LOCAL", input: "@@LOCAL.sort_buffer_size", want: "@@LOCAL.sort_buffer_size"},
		{name: "a lower-case global", input: "@@global.max_connections", want: "@@global.max_connections"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requireTokens(t, testCase.input, []token{{Kind: tokenSystemVariable, Value: testCase.want}})
			analysis := analyseQuery(t, "SELECT "+testCase.input+" AS setting")
			require.Len(t, analysis.OutputColumns, 1)
			assert.Equal(t, "setting", analysis.OutputColumns[0].Name)
		})
	}
}

func TestTokeniserLeavesAWordThatIsNoQualifierToTheVariableName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input string
		want  []token
	}{
		{
			name:  "a word that only starts like a qualifier",
			input: "@@globals.x",
			want: []token{{Kind: tokenSystemVariable, Value: "@@globals"}, {Kind: tokenDot, Value: "."},
				{Kind: tokenIdentifier, Value: "x"}},
		},
		{
			name:  "a qualifier with no dot after it is the variable itself",
			input: "@@GLOBAL",
			want:  []token{{Kind: tokenSystemVariable, Value: "@@GLOBAL"}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requireTokens(t, testCase.input, testCase.want)
		})
	}
}

func scriptOfEveryTokenShape(size int) string {
	const chunk = "SELECT `col 1`, \"text \\\" here\", 'it''s', x'1F', b'01', 0x2A, 1.5e3, .5, @user, @@GLOBAL.time_zone, :name, ?2 " +
		"FROM t /* block */ WHERE a >= 1 AND b <=> NULL -- line\n# hash\nOR c ->> '$.d' /*!50000 , e */;\n"
	var builder strings.Builder
	builder.Grow(size + len(chunk))
	for builder.Len() < size {
		builder.WriteString(chunk)
	}
	return builder.String()
}

func TestTokeniserGrowsLinearlyWithItsInput(t *testing.T) {
	t.Parallel()

	const smallerBytes = 1 << 20

	read := func(input string) func() {
		return func() {
			tokens, err := tokenise(input)
			require.NoError(t, err)
			require.NotEmpty(t, tokens)
		}
	}
	smaller, larger := scriptOfEveryTokenShape(smallerBytes), scriptOfEveryTokenShape(4*smallerBytes)

	scaling.Linear(t, read(smaller), read(larger))
	scaling.Within(t, 2*time.Second, func(context.Context) {
		_, err := tokenise(larger)
		require.NoError(t, err)
	})
}

func BenchmarkTokeniseScript(b *testing.B) {
	for _, size := range []int{1 << 16, 1 << 18, 1 << 20} {
		input := scriptOfEveryTokenShape(size)
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			b.SetBytes(int64(len(input)))
			for b.Loop() {
				_, err := tokenise(input)
				require.NoError(b, err)
			}
		})
	}
}

func TestTokeniserSpansMatchSharedScanner(t *testing.T) {
	t.Parallel()

	files, err := lexdiff.Corpus("mysql", "mariadb")
	require.NoError(t, err)
	samples := []string{
		"SELECT 'a\\'b', 'c''d', X'0F', b'10', `q``x`, \"r\"\"s\" # c\nFROM t -- d\n/* e */;",
		"SELECT ?, ?1, :name, @var, @@session.sql_mode, 1--1;",
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
	executable := executableCommentRanges(source)
	result := make([]dialecttest.Token, 0, len(tokens))
	for _, current := range tokens {
		if current.Kind == tokenEOF || insideAny(executable, current.Start) {
			continue
		}
		result = append(result, dialecttest.Token{Position: current.Start, Class: spanClass(source, current)})
	}
	return result
}

func spanClass(source string, current token) dialecttest.TokenClass {
	switch current.Kind {
	case tokenIdentifier:
		if strings.IndexByte("`\"", source[current.Start]) >= 0 {
			return dialecttest.TokenQuotedIdentifier
		}
		return dialecttest.TokenWord
	case tokenString, tokenHexString, tokenBitString:
		return dialecttest.TokenString
	default:
		return dialecttest.TokenOther
	}
}

func executableCommentRanges(source string) [][2]int {
	scanner := sqllex.NewScanner(lexical.Rules(), source)
	var ranges [][2]int
	for {
		next, ok := scanner.Next()
		if !ok {
			return ranges
		}
		if next.Kind == sqllex.KindExecutableComment {
			ranges = append(ranges, [2]int{next.Start, next.End})
		}
	}
}

func insideAny(ranges [][2]int, offset int) bool {
	for _, bounds := range ranges {
		if offset >= bounds[0] && offset < bounds[1] {
			return true
		}
	}
	return false
}
