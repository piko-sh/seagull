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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/testkit/scaling"
)

var testSigilForms = []SigilForm{
	{Prefix: "$", Suffix: ""},
	{Prefix: "?", Suffix: ""},
	{Prefix: ":", Suffix: ""},
	{Prefix: "@", Suffix: ""},
	{Prefix: "{", Suffix: "}"},
}

func lexerFromContent(content string) *Lexer {
	return NewLexer(LogicalLine{
		Content: content,
		Segments: []LineSegment{{
			PhysicalLine: 1,
			ContentStart: 0,
			ContentEnd:   len(content),
			ColumnOffset: 1,
		}},
		StartLine: 1,
		EndLine:   1,
	}, testSigilForms...)
}

func drainTokens(t *testing.T, lexer *Lexer) []Token {
	t.Helper()
	var tokens []Token
	for {
		current := lexer.Next()
		if current.Kind == TokenEOF {
			return tokens
		}
		tokens = append(tokens, current)
		require.LessOrEqual(t, len(tokens), 1024, "lexer ran past expected token budget")
	}
}

func TestDirectiveLexerScansPrefixedCallTokens(t *testing.T) {
	lexer := lexerFromContent("acme.embed(orders, from: o)")

	tokens := drainTokens(t, lexer)

	require.Len(t, tokens, 10)
	assert.Equal(t, TokenIdentifier, tokens[0].Kind)
	assert.Equal(t, "acme", tokens[0].Lexeme)
	assert.Equal(t, TokenDot, tokens[1].Kind)
	assert.Equal(t, TokenIdentifier, tokens[2].Kind)
	assert.Equal(t, "embed", tokens[2].Lexeme)
	assert.Equal(t, TokenLParen, tokens[3].Kind)
	assert.Equal(t, TokenIdentifier, tokens[4].Kind)
	assert.Equal(t, "orders", tokens[4].Lexeme)
	assert.Equal(t, tokenComma, tokens[5].Kind)
	assert.Equal(t, TokenIdentifier, tokens[6].Kind)
	assert.Equal(t, "from", tokens[6].Lexeme)

	assert.Equal(t, tokenColon, tokens[7].Kind)
	assert.Equal(t, TokenIdentifier, tokens[8].Kind)
	assert.Equal(t, "o", tokens[8].Lexeme)
	assert.Equal(t, tokenRParen, tokens[9].Kind)
}

func TestDirectiveLexerScansDollarSigilAnchor(t *testing.T) {
	lexer := lexerFromContent("$1 as acme.param(status)")

	tokens := drainTokens(t, lexer)

	require.NotEmpty(t, tokens)
	first := tokens[0]
	assert.Equal(t, tokenSigil, first.Kind, "first token should be a sigil")
	assert.Equal(t, "$1", first.Lexeme)
}

func TestDirectiveLexerScansNamedColonSigil(t *testing.T) {
	lexer := lexerFromContent(":email as acme.param()")

	tokens := drainTokens(t, lexer)

	require.NotEmpty(t, tokens)
	assert.Equal(t, tokenSigil, tokens[0].Kind)
	assert.Equal(t, ":email", tokens[0].Lexeme)
}

func TestDirectiveLexerScansBracedSigil(t *testing.T) {
	lexer := lexerFromContent("{count:UInt32} as acme.param()")

	tokens := drainTokens(t, lexer)

	require.NotEmpty(t, tokens)
	assert.Equal(t, tokenSigil, tokens[0].Kind)
	assert.Equal(t, "{count:UInt32}", tokens[0].Lexeme)
}

func TestDirectiveLexerUnterminatedBracedSigilEmitsInvalid(t *testing.T) {
	lexer := lexerFromContent("{count:UInt32")

	tokens := drainTokens(t, lexer)

	require.NotEmpty(t, tokens)
	assert.Equal(t, tokenInvalid, tokens[0].Kind, "unterminated brace sigil should emit tokenInvalid")
	assert.Equal(t, "{count:UInt32", tokens[0].Lexeme)
}

func TestDirectiveLexerUnterminatedBracedSigilSpanCoversInput(t *testing.T) {
	lexer := lexerFromContent("{count:UInt32")

	tokens := drainTokens(t, lexer)

	require.NotEmpty(t, tokens)
	invalidToken := tokens[0]
	assert.Equal(t, tokenInvalid, invalidToken.Kind)
	assert.Equal(t, 1, invalidToken.Span.Line)
	assert.Equal(t, 1, invalidToken.Span.Column, "unterminated sigil span should anchor at column 1 (the opener)")
	assert.Equal(t, 1, invalidToken.Span.EndLine)
	assert.Equal(t, 14, invalidToken.Span.EndColumn, "unterminated sigil span should extend to one past the consumed content")
}

func TestDirectiveLexerHonoursDoubledQuoteEscape(t *testing.T) {
	lexer := lexerFromContent(`"he said ""hello"""`)

	tokens := drainTokens(t, lexer)

	require.Len(t, tokens, 1)
	assert.Equal(t, tokenString, tokens[0].Kind)
	assert.Equal(t, `he said "hello"`, tokens[0].Lexeme, "doubled-quote should decode to a literal quote")
}

func TestDirectiveLexerHonoursBackslashEscape(t *testing.T) {
	lexer := lexerFromContent(`"line\nbreak"`)

	tokens := drainTokens(t, lexer)

	require.Len(t, tokens, 1)
	assert.Equal(t, tokenString, tokens[0].Kind)
	assert.Equal(t, `linenbreak`, tokens[0].Lexeme, "backslash-n should decode to the literal n character")
}

func TestDirectiveLexerUnterminatedStringEmitsInvalid(t *testing.T) {
	lexer := lexerFromContent(`"never closed`)

	tokens := drainTokens(t, lexer)

	require.Len(t, tokens, 1)
	assert.Equal(t, tokenInvalid, tokens[0].Kind, "unterminated string should emit tokenInvalid")
}

func TestDirectiveLexerRejectsUtf8LeadBytesInsideIdentifier(t *testing.T) {

	lexer := lexerFromContent("acme\xc3\xa9")

	tokens := drainTokens(t, lexer)

	require.GreaterOrEqual(t, len(tokens), 2)
	assert.Equal(t, TokenIdentifier, tokens[0].Kind)
	assert.Equal(t, "acme", tokens[0].Lexeme)
	assert.Equal(t, tokenInvalid, tokens[1].Kind, "UTF-8 lead byte should be rejected as tokenInvalid")
}

func TestDirectiveLexerEOFAtEndOfInput(t *testing.T) {
	lexer := lexerFromContent("")

	first := lexer.Peek()
	assert.Equal(t, TokenEOF, first.Kind)

	consumed := lexer.Next()
	assert.Equal(t, TokenEOF, consumed.Kind)
}

func TestDirectiveLexerSpanReportsByteColumns(t *testing.T) {
	lexer := lexerFromContent("acme.embed(orders)")

	prefixToken := lexer.Next()
	dotToken := lexer.Next()
	embedToken := lexer.Next()

	assert.Equal(t, 1, prefixToken.Span.Line)
	assert.Equal(t, 1, prefixToken.Span.Column)
	assert.Equal(t, 5, prefixToken.Span.EndColumn, "acme spans columns 1..5 (exclusive end)")
	assert.Equal(t, 5, dotToken.Span.Column, "dot should land at column 5 (one-based, after 'acme')")
	assert.Equal(t, 6, embedToken.Span.Column, "ident after dot should start at column 6")
}

func TestDirectiveLexerScansNumericLiteral(t *testing.T) {
	lexer := lexerFromContent("acme(max: -123)")

	var sawNumber bool
	for current := lexer.Next(); current.Kind != TokenEOF; current = lexer.Next() {
		if current.Kind == tokenNumber {
			sawNumber = true
			assert.Equal(t, "-123", current.Lexeme)
			break
		}
	}
	assert.True(t, sawNumber, "expected a negative integer literal to be lexed as tokenNumber")
}

func TestDirectiveLexerPeekDoesNotConsume(t *testing.T) {
	lexer := lexerFromContent("acme(name: Foo)")

	first := lexer.Peek()
	again := lexer.Peek()
	consumed := lexer.Next()

	assert.Equal(t, first.Kind, again.Kind)
	assert.Equal(t, first.Lexeme, again.Lexeme)
	assert.Equal(t, first.Kind, consumed.Kind)
}

func TestDirectiveLexerPeekNReturnsLookahead(t *testing.T) {
	lexer := lexerFromContent("acme(name: Foo)")

	zero := lexer.PeekN(0)
	one := lexer.PeekN(1)
	two := lexer.PeekN(2)

	assert.Equal(t, TokenIdentifier, zero.Kind)
	assert.Equal(t, "acme", zero.Lexeme)
	assert.Equal(t, TokenLParen, one.Kind)
	assert.Equal(t, TokenIdentifier, two.Kind)
	assert.Equal(t, "name", two.Lexeme)
}

func TestDirectiveLexerScansOnlyTheGivenSigilForms(t *testing.T) {
	testCases := []struct {
		name         string
		content      string
		expectedKind tokenKind
		forms        []SigilForm
	}{
		{name: "a multi-character prefix opens its anchor", content: "@p2 as acme.param(x)", forms: []SigilForm{{Prefix: "@p", Suffix: ""}}, expectedKind: tokenSigil},
		{name: "text the prefix does not open is no anchor", content: "@x as acme.param(x)", forms: []SigilForm{{Prefix: "@p", Suffix: ""}}, expectedKind: tokenInvalid},
		{name: "a form the dialect lacks is no anchor", content: "$1 as acme.param(x)", forms: []SigilForm{{Prefix: "?", Suffix: ""}}, expectedKind: tokenInvalid},
		{name: "an enclosing form reads through its suffix", content: "<<id>> as acme.param(x)", forms: []SigilForm{{Prefix: "<<", Suffix: ">>"}}, expectedKind: tokenSigil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			lexer := NewLexer(LogicalLine{
				Content:   testCase.content,
				Segments:  []LineSegment{{PhysicalLine: 1, ContentStart: 0, ContentEnd: len(testCase.content), ColumnOffset: 1}},
				StartLine: 1,
				EndLine:   1,
			}, testCase.forms...)

			first := lexer.Next()

			require.Equal(t, testCase.expectedKind, first.Kind, "lexeme %q", first.Lexeme)
		})
	}
}

func continuedLogicalLine(parts int) LogicalLine {
	var content strings.Builder
	segments := make([]LineSegment, 0, parts)
	for index := range parts {
		if index > 0 {
			content.WriteByte(' ')
		}
		start := content.Len()
		fmt.Fprintf(&content, "column_%d,", index)
		segments = append(segments, LineSegment{PhysicalLine: 10 + index, ContentStart: start, ContentEnd: content.Len(), ColumnOffset: 4})
	}
	return LogicalLine{Content: content.String(), Segments: segments, StartLine: 10, EndLine: 9 + parts, UnterminatedStringOffset: -1}
}

func TestDirectiveLexerLocatesTokensOnContinuationLines(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		logical LogicalLine
		start   int
		end     int
		want    diagbuild.TextSpan
	}{
		{name: "on the first line", logical: continuedLogicalLine(5), start: 0, end: 8, want: diagbuild.TextSpan{Line: 10, Column: 4, EndLine: 10, EndColumn: 12}},
		{name: "on a later line", logical: continuedLogicalLine(5), start: 30, end: 38, want: diagbuild.TextSpan{Line: 13, Column: 4, EndLine: 13, EndColumn: 12}},
		{name: "across lines", logical: continuedLogicalLine(5), start: 9, end: 19, want: diagbuild.TextSpan{Line: 10, Column: 13, EndLine: 11, EndColumn: 13}},
		{name: "at the end of the text", logical: continuedLogicalLine(5), start: 49, end: 49, want: diagbuild.TextSpan{Line: 14, Column: 13, EndLine: 14, EndColumn: 13}},
		{
			name:    "before the first segment",
			logical: LogicalLine{Content: "  x", Segments: []LineSegment{{PhysicalLine: 3, ContentStart: 2, ContentEnd: 3, ColumnOffset: 7}}, StartLine: 3, EndLine: 3},
			start:   0,
			end:     1,
			want:    diagbuild.TextSpan{Line: 3, Column: 7, EndLine: 3, EndColumn: 7},
		},
		{name: "without segments", logical: LogicalLine{Content: "x", StartLine: 6, EndLine: 6}, start: 0, end: 1, want: diagbuild.TextSpan{Line: 6, Column: 1, EndLine: 6, EndColumn: 1}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, NewLexer(testCase.logical).SpanAt(testCase.start, testCase.end))
		})
	}
}

func TestDirectiveLexerReadsAContinuedDirectiveInLinearTime(t *testing.T) {
	t.Parallel()

	smaller := continuedLogicalLine(4_000)
	larger := continuedLogicalLine(4 * 4_000)
	readAll := func(logical LogicalLine) func() {
		return func() {
			lexer := NewLexer(logical)
			tokens := 0
			for token := lexer.Next(); token.Kind != TokenEOF; token = lexer.Next() {
				tokens++
			}
			require.Equal(t, 2*len(logical.Segments), tokens)
		}
	}

	scaling.Linear(t, readAll(smaller), readAll(larger))
}

func TestDirectiveLexerReadsAnImportPathAsIdentifiers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		content string
		want    []string
		kinds   []tokenKind
	}{
		{
			name:    "an import path with a hyphen",
			content: "github.com/twpayne/go-geom.Point",
			want:    []string{"github", ".", "com/twpayne/go-geom", ".", "Point"},
			kinds:   []tokenKind{TokenIdentifier, TokenDot, TokenIdentifier, TokenDot, TokenIdentifier},
		},
		{
			name:    "a tilde and a plus sign between identifier characters",
			content: "a~b+c",
			want:    []string{"a~b+c"},
			kinds:   []tokenKind{TokenIdentifier},
		},
		{
			name:    "a joiner that ends the word",
			content: "name- x",
			want:    []string{"name", "-", "x"},
			kinds:   []tokenKind{TokenIdentifier, tokenInvalid, TokenIdentifier},
		},
		{
			name:    "a joiner that opens the text",
			content: "/path",
			want:    []string{"/", "path"},
			kinds:   []tokenKind{tokenInvalid, TokenIdentifier},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var lexemes []string
			var kinds []tokenKind
			for _, token := range drainTokens(t, NewLexer(LogicalLine{Content: testCase.content, Segments: []LineSegment{{PhysicalLine: 1, ContentEnd: len(testCase.content), ColumnOffset: 1}}})) {
				lexemes = append(lexemes, token.Lexeme)
				kinds = append(kinds, token.Kind)
			}
			assert.Equal(t, testCase.want, lexemes)
			assert.Equal(t, testCase.kinds, kinds)
		})
	}
}

func TestMergeSpanLeavesOutASpanThatLocatesNothing(t *testing.T) {
	t.Parallel()

	opening := diagbuild.TextSpan{Line: 2, Column: 14, EndLine: 2, EndColumn: 15}
	closing := diagbuild.TextSpan{Line: 3, Column: 4, EndLine: 3, EndColumn: 5}
	var unlocated diagbuild.TextSpan

	testCases := []struct {
		name  string
		start diagbuild.TextSpan
		end   diagbuild.TextSpan
		want  diagbuild.TextSpan
	}{
		{
			name:  "two located spans give the span enclosing both",
			start: opening,
			end:   closing,
			want:  diagbuild.TextSpan{Line: 2, Column: 14, EndLine: 3, EndColumn: 5},
		},
		{
			name:  "an unlocated end gives the start",
			start: opening,
			end:   unlocated,
			want:  opening,
		},
		{
			name:  "an unlocated start gives the end",
			start: unlocated,
			end:   closing,
			want:  closing,
		},
		{
			name:  "two unlocated spans give an unlocated span",
			start: unlocated,
			end:   unlocated,
			want:  unlocated,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, MergeSpan(testCase.start, testCase.end))
		})
	}
}
