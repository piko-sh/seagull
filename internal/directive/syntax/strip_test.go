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

package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
)

func TestStripDirectiveComments(t *testing.T) {
	input := "SELECT id, name\n-- sgl.name: GetUser\nFROM users\n-- sgl.command: one\nWHERE id = ?1"
	expected := "SELECT id, name\nFROM users\nWHERE id = ?1"

	result := StripDirectives(input, NeutralLexicon(), namespace.Default())
	assert.Equal(t, expected, result)
}

func TestStripDirectiveCommentsStripsCallHeader(t *testing.T) {
	input := "-- sgl.query(name: ListFailedTasks, command: many)\nSELECT id, name\nFROM tasks\nWHERE status = 'FAILED'"
	expected := "SELECT id, name\nFROM tasks\nWHERE status = 'FAILED'"

	result := StripDirectives(input, NeutralLexicon(), namespace.Default())
	assert.Equal(t, expected, result)
}

func TestStripDirectiveCommentsPreservesRegularComments(t *testing.T) {
	input := "SELECT id\n-- This is a regular comment\nFROM users\n-- Another comment about the query\nWHERE active = 1"
	expected := "SELECT id\n-- This is a regular comment\nFROM users\n-- Another comment about the query\nWHERE active = 1"

	result := StripDirectives(input, NeutralLexicon(), namespace.Default())
	assert.Equal(t, expected, result)
}

func TestStripDirectiveCommentsParameterDirectives(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "question mark parameter directive",
			input:    "SELECT id FROM users\n-- ?1 as sgl.param(user_id)\nWHERE id = ?1",
			expected: "SELECT id FROM users\nWHERE id = ?1",
		},
		{
			name:     "dollar parameter directive",
			input:    "SELECT id FROM users\n-- $1 as sgl.param(page_size)\nLIMIT $1",
			expected: "SELECT id FROM users\nLIMIT $1",
		},
		{
			name:     "colon parameter directive",
			input:    "SELECT id FROM users\n-- :email as sgl.param\nWHERE email = :email",
			expected: "SELECT id FROM users\nWHERE email = :email",
		},
		{
			name:     "at parameter directive",
			input:    "SELECT id FROM users\n-- @name as sgl.param\nWHERE name = @name",
			expected: "SELECT id FROM users\nWHERE name = @name",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := StripDirectives(test.input, NeutralLexicon(), namespace.Default())
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestStripDirectivesReadsTheSQLUnderTheLexicon(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		input   string
		want    string
		lexicon Lexicon
	}{
		{
			name:    "a braced parameter binding is stripped",
			input:   "-- sgl.query(ListFinds, many)\n-- {page_size:UInt32} as sgl.param(page_size, default: 3, max: 5)\nSELECT id FROM finds LIMIT {page_size:UInt32};",
			want:    "SELECT id FROM finds LIMIT {page_size:UInt32};",
			lexicon: NeutralLexicon(),
		},
		{
			name:    "a binding written with an alias prefix is stripped",
			input:   "-- seagull.query(ListFinds, many)\n-- {page_size:UInt32} as seagull.param(page_size)\nSELECT id FROM finds LIMIT {page_size:UInt32};",
			want:    "SELECT id FROM finds LIMIT {page_size:UInt32};",
			lexicon: NeutralLexicon(),
		},
		{
			name:    "the continuation lines of a multi-line directive are stripped",
			input:   "-- sgl.query(\n--   GetUser,\n--   one\n-- )\n-- Reads one user.\nSELECT 1;",
			want:    "-- Reads one user.\nSELECT 1;",
			lexicon: NeutralLexicon(),
		},
		{
			name:    "a directive written without a space after the comment opener is stripped",
			input:   "--sgl.query(GetUser, one)\nSELECT 1;",
			want:    "SELECT 1;",
			lexicon: NeutralLexicon(),
		},
		{
			name:    "a directive-like line inside a string is kept",
			input:   "-- sgl.query(GetText, one)\nSELECT '\n-- sgl.query(Inner, one)\n' AS text;",
			want:    "SELECT '\n-- sgl.query(Inner, one)\n' AS text;",
			lexicon: NeutralLexicon(),
		},
		{
			name:    "a directive-like line inside a block comment is kept",
			input:   "-- sgl.query(GetText, one)\nSELECT 1 /*\n-- sgl.query(Inner, one)\n*/;",
			want:    "SELECT 1 /*\n-- sgl.query(Inner, one)\n*/;",
			lexicon: NeutralLexicon(),
		},
		{
			name:    "a foreign directive is kept",
			input:   "-- sgl.query(GetUser, one)\n-- other.column(name, type: text)\nSELECT 1;",
			want:    "-- other.column(name, type: text)\nSELECT 1;",
			lexicon: NeutralLexicon(),
		},
		{
			name:    "a backslash-escaped quote keeps a string open under rules that allow it",
			input:   "-- sgl.query(GetText, one)\nSELECT 'it\\'s\n-- sgl.query(Inner, one)\n' AS text;",
			want:    "SELECT 'it\\'s\n-- sgl.query(Inner, one)\n' AS text;",
			lexicon: Lexicon{LinePrefix: "--", Rules: sqllex.Rules{Quotes: sqllex.QuoteRules{SingleQuoteEscape: sqllex.EscapeBackslash}}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prefixes := namespace.Set{Canonical: "sgl", Aliases: []string{"seagull"}, Foreign: []string{"other"}}
			require.Equal(t, testCase.want, StripDirectives(testCase.input, testCase.lexicon, prefixes))
		})
	}
}

func TestStripDirectivesStripsADirectiveAloneOnItsLine(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "a carriage return before an unclosed call",
			input: "\r--sgl(0",
			want:  "",
		},
		{
			name:  "a no-break space before a header",
			input: "\u00a0-- sgl.query(GetUser, one)\nSELECT 1;",
			want:  "SELECT 1;",
		},
		{
			name:  "a vertical tab and form feed before a binding",
			input: "SELECT id FROM users\n\v\f-- $1 as sgl.param(id)\nWHERE id = $1",
			want:  "SELECT id FROM users\nWHERE id = $1",
		},
		{
			name:  "a byte order mark opening a later line",
			input: "SELECT 1\n" + sqllex.ByteOrderMark + "-- sgl.query(GetUser, one)",
			want:  "SELECT 1",
		},
		{
			name:  "a zero-width space keeps the comment",
			input: "\u200b-- sgl.query(GetUser, one)",
			want:  "\u200b-- sgl.query(GetUser, one)",
		},
		{
			name:  "SQL after a lone carriage return keeps the comment's line",
			input: "-- sgl.query(GetUser, one)\rSELECT 1;",
			want:  "-- sgl.query(GetUser, one)\rSELECT 1;",
		},
		{
			name:  "a string opened after a lone carriage return keeps the comment's line",
			input: "--sgl(0\r'\n--sgl(0'",
			want:  "--sgl(0\r'\n--sgl(0'",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			stripped := StripDirectives(testCase.input, NeutralLexicon(), namespace.Default())
			require.Equal(t, testCase.want, stripped)
			require.Equal(t, stripped, StripDirectives(stripped, NeutralLexicon(), namespace.Default()), "stripping must be idempotent")
		})
	}
}
