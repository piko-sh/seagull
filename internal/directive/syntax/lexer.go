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
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/diagbuild"
)

const (
	// importPathJoiners lists the characters that join two runs of identifier characters
	// within one identifier token, as they join the elements of an import path.
	importPathJoiners = "/-~+"
)

// Token is a single lexed unit carrying its lexeme text and source span.
type Token struct {
	// Lexeme is the raw text of the token.
	Lexeme string

	// Span is the source span covering the token in file coordinates.
	Span diagbuild.TextSpan

	// Kind is the classification of the token.
	Kind tokenKind
}

// Lexer turns the joined content of a LogicalLine into a stream of tokens. Positions
// track back to the original physical (line, column) coordinates via the LogicalLine's
// LineSegment source map.
//
// Sigil scanning (such as `$1`, `:name` or `{name:Type}`) is enabled only for the very
// first non-whitespace token, and only for the forms the lexer was given, because a later
// `:` is a keyword argument separator and the dialect decides which forms anchor a
// parameter.
type Lexer struct {
	// peeked buffers tokens that have been scanned ahead but not yet consumed.
	peeked []Token

	// sigils lists the anchor forms the first token may take.
	sigils []SigilForm

	// logical is the joined logical line being tokenised.
	logical LogicalLine

	// position is the current byte offset into the logical content.
	position int

	// scannedAny records whether any non-whitespace token has been scanned yet.
	scannedAny bool
}

const (
	// TokenEOF marks the end of the input.
	TokenEOF tokenKind = iota

	// TokenIdentifier is an identifier token.
	TokenIdentifier

	// tokenNumber is a numeric literal token.
	tokenNumber

	// tokenString is a quoted string literal token.
	tokenString

	// TokenLParen is the opening parenthesis token.
	TokenLParen

	// tokenRParen is the closing parenthesis token.
	tokenRParen

	// tokenLBracket is the opening square bracket token.
	tokenLBracket

	// tokenRBracket is the closing square bracket token.
	tokenRBracket

	// tokenColon is the colon token used as a keyword argument separator.
	tokenColon

	// tokenComma is the comma token separating arguments.
	tokenComma

	// TokenDot is the dot token used in dotted paths.
	TokenDot

	// tokenSigil is a parameter anchor token such as $1, :name, @name or ?2.
	tokenSigil

	// tokenInvalid marks a byte or rune that begins no recognised token.
	tokenInvalid
)

// NewLexer constructs a lexer for the given logical line.
//
// Takes logical (LogicalLine) which is the joined logical line to tokenise.
// Takes sigils (...SigilForm) which are the parameter anchor forms the line's first token
// may take.
//
// Returns *Lexer which is the lexer ready to scan the line.
func NewLexer(logical LogicalLine, sigils ...SigilForm) *Lexer {
	return &Lexer{logical: logical, sigils: sigils, peeked: nil, position: 0, scannedAny: false}
}

// Next returns the next token, consuming it.
//
// Returns Token which is the next token in the stream.
func (l *Lexer) Next() Token {
	if len(l.peeked) > 0 {
		current := l.peeked[0]
		l.peeked = l.peeked[1:]
		return current
	}
	return l.scan()
}

// Peek returns the next token without consuming it.
//
// Returns Token which is the next token in the stream.
func (l *Lexer) Peek() Token {
	if len(l.peeked) == 0 {
		l.peeked = append(l.peeked, l.scan())
	}
	return l.peeked[0]
}

// PeekN returns the (offset+1)-th upcoming token without consuming any tokens.
//
// Argument parsing uses it to disambiguate `ident :` (a keyword argument) from a bare
// value.
//
// Takes offset (int) which is the zero-based lookahead distance into the token stream.
//
// Returns Token which is the token at the requested lookahead position.
func (l *Lexer) PeekN(offset int) Token {
	for len(l.peeked) <= offset {
		l.peeked = append(l.peeked, l.scan())
	}
	return l.peeked[offset]
}

// SpanAt maps a half-open content range to file coordinates via the logical line's
// segment map.
//
// Columns count bytes, including in quoted values and invalid tokens containing non-ASCII
// text. Consumers using another column encoding must convert them.
//
// Takes start (int) which is the inclusive byte offset where the range begins.
// Takes end (int) which is the exclusive byte offset where the range ends.
//
// Returns diagbuild.TextSpan which is the file-coordinate span for the range.
func (l *Lexer) SpanAt(start, end int) diagbuild.TextSpan {
	startLine, startColumn := l.locate(start)
	endLine, endColumn := l.locate(end)
	return diagbuild.TextSpan{
		Line:      startLine,
		Column:    startColumn,
		EndLine:   endLine,
		EndColumn: endColumn,
	}
}

// scan reads one token from the input position.
//
// Returns Token which is the token read at the current position.
func (l *Lexer) scan() Token {
	l.skipWhitespace()
	if l.position >= len(l.logical.Content) {
		return Token{Kind: TokenEOF, Span: l.SpanAt(l.position, l.position), Lexeme: ""}
	}
	start := l.position
	character := l.logical.Content[l.position]
	switch {
	case character == '(':
		l.position++
		return Token{Kind: TokenLParen, Lexeme: "(", Span: l.SpanAt(start, l.position)}
	case character == ')':
		l.position++
		return Token{Kind: tokenRParen, Lexeme: ")", Span: l.SpanAt(start, l.position)}
	case character == '[':
		l.position++
		return Token{Kind: tokenLBracket, Lexeme: "[", Span: l.SpanAt(start, l.position)}
	case character == ']':
		l.position++
		return Token{Kind: tokenRBracket, Lexeme: "]", Span: l.SpanAt(start, l.position)}
	case character == ',':
		l.position++
		return Token{Kind: tokenComma, Lexeme: ",", Span: l.SpanAt(start, l.position)}
	case !l.scannedAny && l.matchesSigil():
		l.scannedAny = true
		return l.scanSigil(start)
	case character == ':':
		l.position++
		return Token{Kind: tokenColon, Lexeme: ":", Span: l.SpanAt(start, l.position)}
	case character == '.':
		l.position++
		return Token{Kind: TokenDot, Lexeme: ".", Span: l.SpanAt(start, l.position)}
	case character == '\'' || character == '"':
		l.scannedAny = true
		return l.scanString(start)
	case IsDigit(character):
		l.scannedAny = true
		return l.scanNumber(start)
	case character == '-' && l.position+1 < len(l.logical.Content) && IsDigit(l.logical.Content[l.position+1]):
		l.scannedAny = true
		l.position++
		return l.scanNumber(start)
	case IsIdentifierStart(character):
		l.scannedAny = true
		return l.scanIdentifier(start)
	default:
		return l.scanInvalid(start, character)
	}
}

// scanInvalid emits a tokenInvalid for a byte that begins no recognised token.
//
// It decodes a full rune so a multibyte UTF-8 codepoint is reported intact rather than
// re-encoded byte-by-byte into mojibake, and a genuinely invalid byte is shown as hex.
//
// Takes start (int) which is the byte offset where the token began.
// Takes character (byte) which is the leading byte (used for the hex fallback).
//
// Returns Token which is the tokenInvalid carrying the decoded rune or hex byte.
func (l *Lexer) scanInvalid(start int, character byte) Token {
	decoded, width := utf8.DecodeRuneInString(l.logical.Content[l.position:])
	lexeme := string(decoded)
	if decoded == utf8.RuneError && width <= 1 {
		lexeme = fmt.Sprintf("0x%02x", character)
	}
	l.position += width
	l.scannedAny = true
	return Token{Kind: tokenInvalid, Lexeme: lexeme, Span: l.SpanAt(start, l.position)}
}

// scanString reads a quoted string literal.
//
// The opening quote may be either a single or a double quote, and doubled-quote escapes
// are honoured.
//
// Takes start (int) which is the byte offset where the token began.
//
// Returns Token which is the string token, or a tokenInvalid when the literal is
// unterminated.
func (l *Lexer) scanString(start int) Token {
	quote := l.logical.Content[l.position]
	l.position++
	var unescaped []byte
	for l.position < len(l.logical.Content) {
		character := l.logical.Content[l.position]
		if character == '\\' && l.position+1 < len(l.logical.Content) {
			unescaped = append(unescaped, l.logical.Content[l.position+1])
			l.position += 2
			continue
		}
		if character == quote {
			if l.position+1 < len(l.logical.Content) && l.logical.Content[l.position+1] == quote {
				unescaped = append(unescaped, quote)
				l.position += 2
				continue
			}
			l.position++
			return Token{Kind: tokenString, Lexeme: string(unescaped), Span: l.SpanAt(start, l.position)}
		}
		unescaped = append(unescaped, character)
		l.position++
	}
	return Token{Kind: tokenInvalid, Lexeme: string(unescaped), Span: l.SpanAt(start, l.position)}
}

// scanSigil reads a parameter anchor using the longest matching form.
//
// The token retains its prefix so the parser can identify the form. An unterminated
// anchor produces an invalid token.
//
// Takes start (int) which is the byte offset where the token began.
//
// Returns Token which is the sigil token, or a tokenInvalid for an unterminated form.
func (l *Lexer) scanSigil(start int) Token {
	form, _ := l.longestSigil()
	l.position += len(form.Prefix)
	if form.Suffix != "" {
		closing := strings.Index(l.logical.Content[l.position:], form.Suffix)
		if closing < 0 {
			l.position = len(l.logical.Content)
			return Token{Kind: tokenInvalid, Lexeme: l.logical.Content[start:l.position], Span: l.SpanAt(start, l.position)}
		}
		l.position += closing + len(form.Suffix)
		return Token{Kind: tokenSigil, Lexeme: l.logical.Content[start:l.position], Span: l.SpanAt(start, l.position)}
	}
	for l.position < len(l.logical.Content) && IsIdentifierPart(l.logical.Content[l.position]) {
		l.position++
	}
	return Token{Kind: tokenSigil, Lexeme: l.logical.Content[start:l.position], Span: l.SpanAt(start, l.position)}
}

// matchesSigil reports whether the text at the cursor opens one of the lexer's anchor
// forms.
//
// Returns bool which is true when a form matches.
func (l *Lexer) matchesSigil() bool {
	_, found := l.longestSigil()
	return found
}

// longestSigil returns the anchor form with the longest prefix that the text at the
// cursor opens with.
//
// Returns SigilForm which is the matching form.
// Returns bool which is false when no form matches.
func (l *Lexer) longestSigil() (SigilForm, bool) {
	var chosen SigilForm
	found := false
	rest := l.logical.Content[l.position:]
	for _, form := range l.sigils {
		if form.Prefix != "" && strings.HasPrefix(rest, form.Prefix) && (!found || len(form.Prefix) > len(chosen.Prefix)) {
			chosen, found = form, true
		}
	}
	return chosen, found
}

// scanNumber reads a run of ASCII digits into a numeric token.
//
// Takes start (int) which is the byte offset where the token began.
//
// Returns Token which is the number token covering the consumed digits.
func (l *Lexer) scanNumber(start int) Token {
	for l.position < len(l.logical.Content) && IsDigit(l.logical.Content[l.position]) {
		l.position++
	}
	return Token{Kind: tokenNumber, Lexeme: l.logical.Content[start:l.position], Span: l.SpanAt(start, l.position)}
}

// scanIdentifier reads a run of identifier characters into an identifier token.
//
// The run also takes in each slash, hyphen, tilde or plus sign that joins two runs of
// identifier characters, as an import path's elements are joined, so a go_type value such
// as github.com/twpayne/go-geom.Point reads without quotes as a dotted chain of
// identifiers.
//
// Takes start (int) which is the byte offset where the token began.
//
// Returns Token which is the identifier token covering the consumed characters.
func (l *Lexer) scanIdentifier(start int) Token {
	content := l.logical.Content
	for l.position < len(content) {
		switch {
		case IsIdentifierPart(content[l.position]):
			l.position++
		case strings.IndexByte(importPathJoiners, content[l.position]) >= 0 && l.position+1 < len(content) && IsIdentifierPart(content[l.position+1]):
			l.position += 2
		default:
			return Token{Kind: TokenIdentifier, Lexeme: content[start:l.position], Span: l.SpanAt(start, l.position)}
		}
	}
	return Token{Kind: TokenIdentifier, Lexeme: content[start:l.position], Span: l.SpanAt(start, l.position)}
}

// skipWhitespace advances the current position past spaces and tabs.
func (l *Lexer) skipWhitespace() {
	for l.position < len(l.logical.Content) {
		character := l.logical.Content[l.position]
		if character == ' ' || character == '\t' {
			l.position++
			continue
		}
		break
	}
}

// locate finds the physical line and column for a position in the logical content.
//
// The segment holding the position is found by binary search over the segments, which are
// in content order, so a directive continued over many lines costs no more per span than
// one on a single line. It falls back to the first segment when the position precedes any
// segment, which is a defensive case that does not arise on valid scanner input.
//
// Takes offset (int) which is the byte offset into the logical content.
//
// Returns PhysicalLine (int) which is the one-based physical line number.
// Returns column (int) which is the one-based byte column on that line.
func (l *Lexer) locate(offset int) (physicalLine, column int) {
	segments := l.logical.Segments
	if len(segments) == 0 {
		return l.logical.StartLine, 1
	}
	following, _ := slices.BinarySearchFunc(segments, offset+1, func(segment LineSegment, target int) int {
		return cmp.Compare(segment.ContentStart, target)
	})
	if following == 0 {
		return segments[0].PhysicalLine, segments[0].ColumnOffset
	}
	segment := segments[following-1]
	return segment.PhysicalLine, segment.ColumnOffset + (offset - segment.ContentStart)
}

// SigilForm is the shape of a parameter anchor the lexer recognises at the start of a
// line: the text that opens it and, for a form that encloses its name, the text that
// closes it.
type SigilForm struct {
	// Prefix opens the anchor, such as "$", ":" or "{".
	Prefix string

	// Suffix closes an enclosing anchor, such as "}", or is empty when the anchor ends with
	// its run of identifier characters.
	Suffix string
}

// LogicalLine is one directive after multi-line continuation has been resolved. It
// records the original physical line range so every token can map back to file
// coordinates.
type LogicalLine struct {
	// Content is the joined directive text with comment prefixes stripped from continuation
	// lines.
	Content string

	// Segments records the (PhysicalLine, ContentStart, ContentEnd, ColumnOffset) mapping
	// for each contributing physical line.
	Segments []LineSegment

	// StartLine is the one-based physical line number of the first line that contributed to
	// this logical directive.
	StartLine int

	// EndLine is the one-based physical line number of the last contributing line.
	EndLine int

	// UnterminatedStringOffset, when non-negative, gives the byte offset in content where an
	// unterminated string literal begins. Reported by parseLogicalLine as Q035 before the
	// lexer/parser run so the real cause is surfaced instead of a downstream "unclosed
	// directive" cascade.
	UnterminatedStringOffset int

	// UnterminatedStringQuote is the opening quote character of the unterminated string when
	// UnterminatedStringOffset >= 0.
	UnterminatedStringQuote byte
}

// LineSegment maps a slice of the logical content back to a physical line and column.
//
// ContentEnd is exclusive.
type LineSegment struct {
	// PhysicalLine is the one-based physical line number this segment came from.
	PhysicalLine int

	// ContentStart is the inclusive start offset of this segment in the logical content.
	ContentStart int

	// ContentEnd is the exclusive end offset of this segment in the logical content.
	ContentEnd int

	// ColumnOffset is the one-based column where the content begins on its physical line.
	ColumnOffset int
}

// tokenKind enumerates the lexer's output tokens.
type tokenKind uint8

// MergeSpan returns the smallest TextSpan that encloses both start and end.
//
// The lexer and parser use it to cover constructs spanning several tokens. A span on line
// zero marks a missing location, such as an absent closing parenthesis, and is ignored
// when combined with a located span.
//
// Takes start (diagbuild.TextSpan) which is the first span to enclose.
// Takes end (diagbuild.TextSpan) which is the second span to enclose.
//
// Returns diagbuild.TextSpan which is the smallest span covering both inputs.
func MergeSpan(start, end diagbuild.TextSpan) diagbuild.TextSpan {
	if end.Line < 1 {
		return start
	}
	if start.Line < 1 {
		return end
	}
	merged := start
	if end.EndLine > merged.EndLine || (end.EndLine == merged.EndLine && end.EndColumn > merged.EndColumn) {
		merged.EndLine = end.EndLine
		merged.EndColumn = end.EndColumn
	}
	if end.Line < merged.Line || (end.Line == merged.Line && end.Column < merged.Column) {
		merged.Line = end.Line
		merged.Column = end.Column
	}
	return merged
}
