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
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestLineIndex(t *testing.T) {
	t.Parallel()

	text := sqllex.ByteOrderMark + "  -- a\r\nSELECT 1; -- b\n\n\t-- c"
	lines := NewLineIndex(text)

	first := strings.Index(text, "--")
	require.Equal(t, 1, lines.Line(first))
	require.Equal(t, 0, lines.LineStart(first))
	require.True(t, lines.BeginsLine(text, first))

	second := strings.Index(text, "-- b")
	require.Equal(t, 2, lines.Line(second))
	require.Equal(t, 11, lines.Column(second))
	require.False(t, lines.BeginsLine(text, second))

	third := strings.Index(text, "-- c")
	require.Equal(t, 4, lines.Line(third))
	require.Equal(t, 2, lines.Column(third))
	require.True(t, lines.BeginsLine(text, third))
	require.Equal(t, 4, lines.Line(len(text)))
}

func TestBeginsLineTreatsWhiteSpaceAndByteOrderMarksAsBlank(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		text string
		want bool
	}{
		{name: "spaces and tabs", text: " \t-- c", want: true},
		{name: "a carriage return, vertical tab and form feed", text: "\r\v\f-- c", want: true},
		{name: "a no-break space and an em space", text: "\u00a0\u2003-- c", want: true},
		{name: "a byte order mark opening the text", text: sqllex.ByteOrderMark + "-- c", want: true},
		{name: "a byte order mark after spaces", text: "  " + sqllex.ByteOrderMark + "-- c", want: true},
		{name: "a byte order mark opening a later line", text: "SELECT 1;\n" + sqllex.ByteOrderMark + "-- c", want: true},
		{name: "SQL before the comment", text: "SELECT 1; -- c", want: false},
		{name: "a zero-width space, which is not white space", text: "\u200b-- c", want: false},
		{name: "invalid UTF-8", text: "\xff-- c", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			offset := strings.Index(testCase.text, "-- c")
			require.Equal(t, testCase.want, NewLineIndex(testCase.text).BeginsLine(testCase.text, offset))
		})
	}
}

func TestBeginsLineAgreesWithTrimmingTheLineAtEveryOffset(t *testing.T) {
	t.Parallel()

	texts := []string{
		"",
		"SELECT 1;",
		"  -- c\n\t/* b */ -- d\nSELECT 1; -- e\n",
		"\n\n   \n-- c",
		"\r\n  \r\n-- c\r\n",
		sqllex.ByteOrderMark + "  -- c\n" + sqllex.ByteOrderMark + "x -- d",
		"\u00a0\u2003/* a */ /* b */ -- c\n\u200b-- d",
		"\xff -- c\n  \xff",
	}
	for _, text := range texts {
		index := NewLineIndex(text)
		for offset := 0; offset <= len(text); offset++ {
			if offset < len(text) && !utf8.RuneStart(text[offset]) {
				continue
			}
			trimmed := strings.TrimLeftFunc(text[index.LineStart(offset):offset], isLineBlank) == ""
			assert.Equal(t, trimmed, index.BeginsLine(text, offset), "text %q at offset %d", text, offset)
		}
	}
}

func TestReadDirectiveCommentNeedsTheCommentAloneOnItsLine(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		text string
		want bool
	}{
		{name: "a comment ending the text", text: "-- sgl(x)", want: true},
		{name: "a comment ending a line", text: "-- sgl(x)\nSELECT 1;", want: true},
		{name: "a comment ending a CRLF line", text: "-- sgl(x)\r\nSELECT 1;", want: true},
		{name: "blank characters after the comment", text: "-- sgl(x)\r \u00a0\nSELECT 1;", want: true},
		{name: "SQL after a lone carriage return", text: "-- sgl(x)\rSELECT 1;\n", want: false},
		{name: "a string opened after a lone carriage return", text: "--sgl(0\r'\n--sgl(0'", want: false},
		{name: "SQL before the comment", text: "SELECT 1; -- sgl(x)", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			lexicon := NeutralLexicon()
			tokens := Tokens(lexicon.Rules, testCase.text)
			lines := NewLineIndex(testCase.text)
			start := strings.Index(testCase.text, "--")
			var found bool
			for _, token := range tokens {
				if token.Start == start {
					_, found = ReadDirectiveComment(testCase.text, lines, token, lexicon.LinePrefix)
				}
			}
			require.Equal(t, testCase.want, found)
		})
	}
}

func TestTokensResumeAfterAnUnterminatedConstruct(t *testing.T) {
	t.Parallel()

	text := "SELECT 'open\n-- next\nSELECT /* never closed\n-- last"
	tokens := Tokens(sqllex.Rules{}, text)

	var rebuilt strings.Builder
	var comments []string
	for _, token := range tokens {
		require.Equal(t, text[token.Start:token.End], token.Raw)
		rebuilt.WriteString(token.Raw)
		if token.Kind == sqllex.KindComment {
			comments = append(comments, token.Raw)
		}
	}
	require.Equal(t, text, rebuilt.String())
	require.Equal(t, []string{"-- next", "-- last"}, comments)
	require.Equal(t, sqllex.KindPunctuation, tokens[2].Kind)
	require.Equal(t, "'open\n", tokens[2].Raw)
}

func TestScanTokensStopsRecoveringAtTheLimit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		openers       int
		wantRecovered bool
		wantTruncated bool
	}{
		{name: "a text with no unterminated construct", openers: 0, wantRecovered: false, wantTruncated: false},
		{name: "one unterminated construct", openers: 1, wantRecovered: true, wantTruncated: false},
		{name: "as many as are recovered from", openers: maxLexicalRecoveries, wantRecovered: true, wantTruncated: false},
		{name: "one more than are recovered from", openers: maxLexicalRecoveries + 1, wantRecovered: true, wantTruncated: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			text := strings.Repeat("SELECT 1; /* open\n", testCase.openers) + "-- sgl.migration(no_transaction: true)\n"
			scan := ScanTokens(sqllex.Rules{}, text)

			var rebuilt strings.Builder
			for _, token := range scan.Tokens {
				require.Equal(t, text[token.Start:token.End], token.Raw)
				rebuilt.WriteString(token.Raw)
			}
			require.Equal(t, text, rebuilt.String())
			require.Equal(t, testCase.wantRecovered, scan.Recovered)
			require.Equal(t, testCase.wantTruncated, scan.Truncated())
			last := scan.Tokens[len(scan.Tokens)-1]
			if !testCase.wantTruncated {
				require.Equal(t, -1, scan.TruncatedAt)
				require.Equal(t, sqllex.KindWhitespace, last.Kind)
				return
			}
			require.Equal(t, maxLexicalRecoveries*len("SELECT 1; /* open\n")+len("SELECT 1; "), scan.TruncatedAt)
			require.Equal(t, sqllex.KindPunctuation, last.Kind)
			require.Equal(t, text[scan.TruncatedAt:], last.Raw)
		})
	}
}

func TestScanTokensReadsManyUnterminatedConstructsInLinearTime(t *testing.T) {
	t.Parallel()

	nested := NeutralLexicon().Rules
	nested.Comments.NestedBlockComments = true
	for name, rules := range map[string]sqllex.Rules{"flat comments": NeutralLexicon().Rules, "nested comments": nested} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			smaller := strings.Repeat("/*\n", 8_000)
			larger := strings.Repeat("/*\n", 4*8_000)

			scaling.Linear(t, func() { ScanTokens(rules, smaller) }, func() { ScanTokens(rules, larger) })
		})
	}
}

func TestReadDirectiveComment(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		text     string
		prefix   string
		wantBody string
		wantOK   bool
	}{
		{name: "one space after the prefix is dropped", text: "--  sgl.query(A, one)", prefix: "--", wantBody: " sgl.query(A, one)", wantOK: true},
		{name: "a tab after the prefix is dropped", text: "--\tsgl.query(A, one)", prefix: "--", wantBody: "sgl.query(A, one)", wantOK: true},
		{name: "no space after the prefix", text: "--sgl.query(A, one)", prefix: "--", wantBody: "sgl.query(A, one)", wantOK: true},
		{name: "an indented comment", text: "\t  -- note", prefix: "--", wantBody: "note", wantOK: true},
		{name: "a comment after SQL on its line", text: "SELECT 1; -- note", prefix: "--", wantBody: "", wantOK: false},
		{name: "a block comment", text: "/* -- note */", prefix: "--", wantBody: "", wantOK: false},
		{name: "a comment with another opener", text: "# note", prefix: "--", wantBody: "", wantOK: false},
		{name: "no prefix", text: "-- note", prefix: "", wantBody: "", wantOK: false},
	}

	rules := sqllex.Rules{Comments: sqllex.CommentRules{HashLineComments: true}}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			lines := NewLineIndex(testCase.text)
			found := false
			for _, token := range Tokens(rules, testCase.text) {
				comment, ok := ReadDirectiveComment(testCase.text, lines, token, testCase.prefix)
				if !ok {
					continue
				}
				found = true
				require.Equal(t, testCase.wantBody, comment.Body)
				require.Equal(t, comment.Body, testCase.text[comment.BodyStart:comment.Token.End])
			}
			require.Equal(t, testCase.wantOK, found)
		})
	}
}

func TestDirectiveName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		body       string
		wantPrefix string
		wantRole   string
		wantRest   string
		wantCall   bool
	}{
		{body: " sgl.query(A, one)", wantPrefix: "sgl", wantRole: "query", wantRest: "(A, one)", wantCall: true},
		{body: "sgl.query (A)", wantPrefix: "sgl", wantRole: "query", wantRest: " (A)", wantCall: true},
		{body: "sgl(A)", wantPrefix: "sgl", wantRole: "", wantRest: "(A)", wantCall: true},
		{body: "sgl. query", wantPrefix: "sgl", wantRole: "", wantRest: ". query", wantCall: false},
		{body: "$1 as sgl.param", wantPrefix: "", wantRole: "", wantRest: "$1 as sgl.param", wantCall: false},
		{body: "sgl keeps this fast", wantPrefix: "sgl", wantRole: "", wantRest: " keeps this fast", wantCall: false},
	}

	for _, testCase := range testCases {
		prefix, role, rest := DirectiveName(testCase.body)
		require.Equal(t, testCase.wantPrefix, prefix, testCase.body)
		require.Equal(t, testCase.wantRole, role, testCase.body)
		require.Equal(t, testCase.wantRest, rest, testCase.body)
		require.Equal(t, testCase.wantCall, OpensCall(rest), testCase.body)
	}
}

func TestLexiconDirectiveComments(t *testing.T) {
	t.Parallel()

	text := "-- sgl.query(A, one)\nSELECT $$\n-- inside a body\n$$, `-- inside a name`;\n-- after"
	comments, lines := NeutralLexicon().DirectiveComments(text)

	require.Len(t, comments, 2)
	require.Equal(t, "sgl.query(A, one)", comments[0].Body)
	require.Equal(t, "after", comments[1].Body)
	require.Equal(t, 5, lines.Line(comments[1].Token.Start))
}
