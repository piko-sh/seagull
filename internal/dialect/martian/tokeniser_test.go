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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenise(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []token
	}{
		{
			name:  "unquoted identifiers fold to upper case",
			input: "select Name",
			expected: []token{
				{value: "SELECT", offset: 0, end: 6, kind: tokenIdentifier},
				{value: "NAME", offset: 7, end: 11, kind: tokenIdentifier},
				{value: "", offset: 11, end: 11, kind: tokenEnd},
			},
		},
		{
			name:  "bracket identifiers keep their case and unescape doubled brackets",
			input: "[Weird]]Name]",
			expected: []token{
				{value: "Weird]Name", offset: 0, end: 13, kind: tokenQuotedIdentifier},
				{value: "", offset: 13, end: 13, kind: tokenEnd},
			},
		},
		{
			name:  "placeholders carry their number",
			input: "@p12 @P3",
			expected: []token{
				{value: "12", offset: 0, end: 4, kind: tokenPlaceholder},
				{value: "3", offset: 5, end: 8, kind: tokenPlaceholder},
				{value: "", offset: 8, end: 8, kind: tokenEnd},
			},
		},
		{
			name:  "strings unescape doubled quotes",
			input: "'it''s'",
			expected: []token{
				{value: "it's", offset: 0, end: 7, kind: tokenString},
				{value: "", offset: 7, end: 7, kind: tokenEnd},
			},
		},
		{
			name:  "comments are skipped",
			input: "-- line\n/* block */ 42",
			expected: []token{
				{value: "42", offset: 20, end: 22, kind: tokenNumber},
				{value: "", offset: 22, end: 22, kind: tokenEnd},
			},
		},
		{
			name:  "a byte order mark opening the input is skipped",
			input: "\uFEFF42",
			expected: []token{
				{value: "42", offset: 3, end: 5, kind: tokenNumber},
				{value: "", offset: 5, end: 5, kind: tokenEnd},
			},
		},
		{
			name:  "two-character operators are one symbol",
			input: "<=<>>=",
			expected: []token{
				{value: "<=", offset: 0, end: 2, kind: tokenSymbol},
				{value: "<>", offset: 2, end: 4, kind: tokenSymbol},
				{value: ">=", offset: 4, end: 6, kind: tokenSymbol},
				{value: "", offset: 6, end: 6, kind: tokenEnd},
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, tokens)
		})
	}
}

func TestTokeniseRejects(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		input           string
		messageContains string
	}{
		{name: "the concatenation operator", input: "a || b", messageContains: "no || operator"},
		{name: "an unterminated block comment", input: "/* open", messageContains: "unterminated block comment"},
		{name: "an unterminated string", input: "'open", messageContains: "unterminated string literal"},
		{name: "an unterminated delimited identifier", input: "[open", messageContains: "unterminated delimited identifier"},
		{name: "an empty delimited identifier", input: "[]", messageContains: "empty delimited identifier"},
		{name: "a placeholder numbered from zero", input: "@p0", messageContains: "numbered from @p1"},
		{name: "a named placeholder", input: "@name", messageContains: "written @p and a number"},
		{name: "an unknown character", input: "a ? b", messageContains: "unexpected character"},
		{name: "a byte order mark after the start", input: "a \uFEFF", messageContains: "unexpected character"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := tokenise(testCase.input)
			require.ErrorIs(t, err, errSyntax)
			require.ErrorContains(t, err, testCase.messageContains)
		})
	}
}
