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

func TestFormat(t *testing.T) {
	t.Parallel()

	prefixes := namespace.Set{Canonical: "sgl", Aliases: []string{"seagull"}, Foreign: []string{"other"}}

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "canonical directive is unchanged",
			source: "-- sgl.query(GetUser, one)\nSELECT 1;\n",
			want:   "-- sgl.query(GetUser, one)\nSELECT 1;\n",
		},
		{
			name:   "alias prefix becomes canonical",
			source: "-- seagull.query(GetUser, one)\nSELECT 1;\n",
			want:   "-- sgl.query(GetUser, one)\nSELECT 1;\n",
		},
		{
			name:   "bare call form",
			source: "-- seagull(GetUser, one)\n",
			want:   "-- sgl(GetUser, one)\n",
		},
		{
			name:   "spacing inside the call is normalised",
			source: "--seagull.query(  GetUser ,one,   dynamic:   runtime )   \n",
			want:   "-- sgl.query(GetUser, one, dynamic: runtime)\n",
		},
		{
			name:   "list arguments",
			source: "-- sgl.sortable( order_by , [ name ,total ] )\n",
			want:   "-- sgl.sortable(order_by, [name, total])\n",
		},
		{
			name:   "parameter directive with alias",
			source: "--   $1   as   seagull.param( status,optional: true )\n",
			want:   "-- $1 as sgl.param(status, optional: true)\n",
		},
		{
			name:   "named parameter without arguments",
			source: "-- :email as seagull.param\n",
			want:   "-- :email as sgl.param\n",
		},
		{
			name:   "quoted values keep their spacing",
			source: "-- sgl.column(settings,  go_type: \"example.com/app  x.Settings\")\n",
			want:   "-- sgl.column(settings, go_type: \"example.com/app  x.Settings\")\n",
		},
		{
			name:   "text after the call is kept",
			source: "-- seagull.query(GetUser,one)      -- positional   \n",
			want:   "-- sgl.query(GetUser, one)      -- positional\n",
		},
		{
			name:   "multi-line directive keeps continuation indentation",
			source: "-- seagull.query(\n--   SearchOrders ,\n--   many,\n--   group_by:   orders.id,\n-- )\nSELECT 1;\n",
			want:   "-- sgl.query(\n--   SearchOrders,\n--   many,\n--   group_by: orders.id,\n-- )\nSELECT 1;\n",
		},
		{
			name:   "indentation and CRLF line endings are kept",
			source: "\t-- seagull.query(GetUser, one)   \r\nSELECT 1;\r\n",
			want:   "\t-- sgl.query(GetUser, one)\r\nSELECT 1;\r\n",
		},
		{
			name:   "prose comments are untouched",
			source: "-- seagull.io  is  great\n--   a   comment\n-- other.query(X, one)\n",
			want:   "-- seagull.io  is  great\n--   a   comment\n-- other.query(X, one)\n",
		},
		{
			name:   "SQL bodies are untouched",
			source: "-- sgl.query(X, one)\nSELECT 'a  ,  b',   seagull.query(1)  FROM t;\n",
			want:   "-- sgl.query(X, one)\nSELECT 'a  ,  b',   seagull.query(1)  FROM t;\n",
		},
		{
			name:   "directive-like lines inside a string literal are untouched",
			source: "SELECT 'line one\n-- seagull.query(  X ,one)\nline three' FROM t;\n",
			want:   "SELECT 'line one\n-- seagull.query(  X ,one)\nline three' FROM t;\n",
		},
		{
			name:   "directive-like lines inside a block comment are untouched",
			source: "/* example:\n-- seagull.query(  X ,one)\n*/\n-- seagull.query(Y, one)\n",
			want:   "/* example:\n-- seagull.query(  X ,one)\n*/\n-- sgl.query(Y, one)\n",
		},
		{
			name:   "directive-like lines inside a dollar-quoted body are untouched",
			source: "CREATE FUNCTION f() RETURNS int AS $$\n-- seagull.query(  X ,one)\nSELECT 1\n$$ LANGUAGE sql;\n",
			want:   "CREATE FUNCTION f() RETURNS int AS $$\n-- seagull.query(  X ,one)\nSELECT 1\n$$ LANGUAGE sql;\n",
		},
		{
			name:   "an interrupted multi-line directive stops at the first non-comment line",
			source: "-- sgl.query(\nSELECT  1;\n--   x ,  y\n",
			want:   "-- sgl.query(\nSELECT  1;\n--   x ,  y\n",
		},
		{
			name:   "empty input",
			source: "",
			want:   "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			formatted := FormatSource(testCase.source, NeutralLexicon(), prefixes)
			assert.Equal(t, testCase.want, formatted)
			assert.Equal(t, formatted, FormatSource(formatted, NeutralLexicon(), prefixes), "formatting must be idempotent")
		})
	}
}

func FuzzFormatIsIdempotent(f *testing.F) {
	f.Add("-- seagull.query(  GetUser ,one )\nSELECT 'x';\n")
	f.Add("-- sgl.query(\n--  A ,\n-- )\n")
	f.Add("-- $1 as seagull.param( a , [ b ,c ] )   -- note\n")
	prefixes := namespace.Set{Canonical: "sgl", Aliases: []string{"seagull"}, Foreign: nil}

	f.Fuzz(func(t *testing.T, source string) {
		formatted := FormatSource(source, NeutralLexicon(), prefixes)
		require.Equal(t, formatted, FormatSource(formatted, NeutralLexicon(), prefixes), "formatting the formatted text changed it")
	})
}

func TestFormatSourceReadsTheFileUnderTheLexicon(t *testing.T) {
	t.Parallel()

	prefixes := namespace.Set{Canonical: "sgl", Aliases: []string{"seagull"}, Foreign: nil}
	backslash := Lexicon{LinePrefix: "--", Rules: sqllex.Rules{Quotes: sqllex.QuoteRules{SingleQuoteEscape: sqllex.EscapeBackslash}}}

	tests := []struct {
		name    string
		source  string
		want    string
		lexicon Lexicon
	}{
		{
			name:    "a backslash-escaped quote in a directive value keeps the value whole",
			source:  "-- seagull.column(note,  go_type: \"a\\\"b  ,  c\" ,nullable: true)\n",
			want:    "-- sgl.column(note, go_type: \"a\\\"b  ,  c\", nullable: true)\n",
			lexicon: NeutralLexicon(),
		},
		{
			name:    "a directive-like line inside a backslash-escaped string is untouched",
			source:  "SELECT 'it\\'s\n-- seagull.query(  X ,one)\n';\n-- seagull.query(Y, one)\n",
			want:    "SELECT 'it\\'s\n-- seagull.query(  X ,one)\n';\n-- sgl.query(Y, one)\n",
			lexicon: backslash,
		},
		{
			name:    "a braced parameter binding is canonicalised",
			source:  "--  {page_size:UInt32}   as   seagull.param( page_size )\n",
			want:    "-- {page_size:UInt32} as sgl.param(page_size)\n",
			lexicon: NeutralLexicon(),
		},
		{
			name:    "a comment the dialect does not read as a comment is untouched",
			source:  "--seagull.query(X, one)\n",
			want:    "--seagull.query(X, one)\n",
			lexicon: Lexicon{LinePrefix: "--", Rules: sqllex.Rules{Comments: sqllex.CommentRules{DoubleDashRequiresWhitespace: true}}},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			formatted := FormatSource(testCase.source, testCase.lexicon, prefixes)
			assert.Equal(t, testCase.want, formatted)
			assert.Equal(t, formatted, FormatSource(formatted, testCase.lexicon, prefixes), "formatting must be idempotent")
		})
	}
}

func TestFormatSourceRecognisesAParameterDirectiveOnlyWhenSpacesAndTabsSeparateItsParts(t *testing.T) {
	t.Parallel()

	prefixes := namespace.Set{Canonical: "sgl", Aliases: []string{"seagull"}, Foreign: nil}

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "a comment whose text opens with a vertical tab is not a directive",
			source: "--\vseagull( as seagull.A",
			want:   "--\vseagull( as seagull.A",
		},
		{
			name:   "a vertical tab inside the anchor is not a directive",
			source: "-- $1\vx as seagull.param(a)\n",
			want:   "-- $1\vx as seagull.param(a)\n",
		},
		{
			name:   "a form feed before the as keyword is not a directive",
			source: "-- $1\f as seagull.param(a)\n",
			want:   "-- $1\f as seagull.param(a)\n",
		},
		{
			name:   "spaces and tabs around the as keyword are normalised",
			source: "--  $1 \t as\tseagull.param( a )\n",
			want:   "-- $1 as sgl.param(a)\n",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			formatted := FormatSource(testCase.source, NeutralLexicon(), prefixes)
			assert.Equal(t, testCase.want, formatted)
			assert.Equal(t, formatted, FormatSource(formatted, NeutralLexicon(), prefixes), "formatting must be idempotent")
		})
	}
}
