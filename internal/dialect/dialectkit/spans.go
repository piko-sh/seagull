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

package dialectkit

import (
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// inKeyword is the keyword of an IN list.
	inKeyword = "IN"

	// notKeyword is the keyword that negates an IN list.
	notKeyword = "NOT"

	// notOffset is how many tokens before a sole IN-list placeholder a NOT negating the list
	// sits: NOT, IN and the opening parenthesis come between.
	notOffset = 3
)

// PlaceholderTokens tells PlaceholderSpans how a dialect classifies the tokens it needs.
type PlaceholderTokens[K comparable] struct {
	// IsParameter reports whether a token is a placeholder.
	IsParameter func(Token[K]) bool

	// Word is the kind of bare words, which keywords such as IN are.
	Word K

	// LeftParenthesis is the kind of "(".
	LeftParenthesis K

	// RightParenthesis is the kind of ")".
	RightParenthesis K
}

// placeholderNumbering numbers anonymous, named and numbered placeholders in source
// order.
//
// Unnumbered placeholders take the next number; repeated names reuse their first number.
type placeholderNumbering struct {
	// names holds the names already given a number.
	names map[string]struct{}

	// anonymous counts the anonymous placeholders seen so far.
	anonymous int

	// largest is the largest number given so far.
	largest int
}

// describe fills a span with what a placeholder's reading says and numbers it.
//
// Takes span (*ast.PlaceholderSpan) which receives the name, number and type.
// Takes reading (dialect.PlaceholderReading) which is what the placeholder says.
func (numbering *placeholderNumbering) describe(span *ast.PlaceholderSpan, reading dialect.PlaceholderReading) {
	switch reading.Sigil.Kind {
	case dialect.PlaceholderAnonymous:
		numbering.anonymous++
		span.Number = numbering.anonymous
	case dialect.PlaceholderNumbered:
		span.Number = numbering.number(reading.Number)
	case dialect.PlaceholderNamed:
		span.Name = reading.Name
		if _, seen := numbering.names[reading.Name]; !seen {
			numbering.names[reading.Name] = struct{}{}
			numbering.largest++
		}
		if reading.Type != "" {
			start := span.Span.Start + reading.TypeOffset
			span.Type = ast.Span{Start: start, End: start + len(reading.Type)}
		}
	}
}

// number returns the number of a numbered placeholder: its own, or the next one when it
// was written without one.
//
// Takes written (int) which is the number the placeholder writes, or zero.
//
// Returns int which is the placeholder's number.
func (numbering *placeholderNumbering) number(written int) int {
	if written == 0 {
		numbering.largest++
		return numbering.largest
	}
	numbering.largest = max(numbering.largest, written)
	return written
}

// TokenSpan returns the span from the first byte of one token to the last byte of
// another, as offsets from a statement's first byte.
//
// Takes first (Token[K]) which is the token the span starts at.
// Takes last (Token[K]) which is the token the span ends with.
// Takes statementStart (int) which is the offset of the statement's first byte in the
// text the tokens were read from.
//
// Returns ast.Span which covers both tokens and everything between them.
func TokenSpan[K comparable](first, last Token[K], statementStart int) ast.Span {
	return ast.Span{Start: first.Start - statementStart, End: last.End - statementStart}
}

// TokenRange returns the offsets in the source text that the tokens from first up to end
// cover.
//
// Takes tokens ([]Token[K]) which are the statement's tokens.
// Takes first (int) which is the index of the first token.
// Takes end (int) which is one past the index of the last token; past the last token it
// stops at the last token.
//
// Returns start (int) which is the offset of the first token's first byte.
// Returns stop (int) which is the offset one past the last token's last byte.
// Returns ok (bool) which is false for an empty range.
func TokenRange[K comparable](tokens []Token[K], first, end int) (start, stop int, ok bool) {
	end = min(end, len(tokens))
	if first < 0 || first >= end {
		return 0, 0, false
	}
	return tokens[first].Start, tokens[end-1].End, true
}

// TokenRangeSpan returns the span from the first byte of the token at first to the last
// byte of the token before end, as offsets into the text the tokens were read from.
//
// Takes tokens ([]Token[K]) which are the statement's tokens.
// Takes first (int) which is the index of the first token.
// Takes end (int) which is one past the index of the last token.
//
// Returns ast.Span which covers the tokens, or is zero for an empty range.
func TokenRangeSpan[K comparable](tokens []Token[K], first, end int) ast.Span {
	start, stop, ok := TokenRange(tokens, first, end)
	if !ok {
		return ast.Span{Start: 0, End: 0}
	}
	return ast.Span{Start: start, End: stop}
}

// RecordTokenRange records the tokens from first up to end as a clause of the top-level
// statement, unless the statement's clause of that kind is recorded already.
//
// Takes recorder (*SpanRecorder) which records the statement's spans; nil records
// nothing.
// Takes field (SpanField) which names the clause.
// Takes tokens ([]Token[K]) which are the statement's tokens.
// Takes first (int) which is the index of the clause's first token.
// Takes end (int) which is one past the index of its last token.
func RecordTokenRange[K comparable](recorder *SpanRecorder, field SpanField, tokens []Token[K], first, end int) {
	if start, stop, ok := TokenRange(tokens, first, end); ok {
		recorder.Record(field, start, stop)
	}
}

// ExtendTokenRange widens a clause of the top-level statement to cover the tokens from
// first up to end as well, for a clause written in several parts, such as LIMIT and
// OFFSET.
//
// Takes recorder (*SpanRecorder) which records the statement's spans; nil records
// nothing.
// Takes field (SpanField) which names the clause.
// Takes tokens ([]Token[K]) which are the statement's tokens.
// Takes first (int) which is the index of the part's first token.
// Takes end (int) which is one past the index of its last token.
func ExtendTokenRange[K comparable](recorder *SpanRecorder, field SpanField, tokens []Token[K], first, end int) {
	if start, stop, ok := TokenRange(tokens, first, end); ok {
		recorder.Extend(field, start, stop)
	}
}

// TokenItemSpan returns the span of one select-list or RETURNING item.
//
// Takes recorder (*SpanRecorder) which records the statement's spans; nil gives spans
// that are zero.
// Takes tokens ([]Token[K]) which are the statement's tokens.
// Takes first (int) which is the index of the item's first token.
// Takes expressionEnd (int) which is one past the index of its expression's last token.
// Takes end (int) which is one past the index of the item's last token, its alias
// included.
// Takes star (bool) which is true for a star item.
//
// Returns ast.ItemSpan which locates the item, or is zero for an empty item.
func TokenItemSpan[K comparable](recorder *SpanRecorder, tokens []Token[K], first, expressionEnd, end int, star bool) ast.ItemSpan {
	start, stop, ok := TokenRange(tokens, first, end)
	if !ok {
		return ast.ItemSpan{Span: ast.Span{Start: 0, End: 0}, Expression: ast.Span{Start: 0, End: 0}, Alias: ast.Span{Start: 0, End: 0}, Star: false}
	}
	_, expressionStop, _ := TokenRange(tokens, first, expressionEnd)
	aliasStart := stop
	if expressionEnd < end && expressionEnd >= 0 && expressionEnd < len(tokens) {
		aliasStart = tokens[expressionEnd].Start
	}
	return recorder.Item(start, expressionStop, aliasStart, stop, star)
}

// RecordOrderByPlaceAt records, for the top-level statement, that its ORDER BY clause is
// written, or would be, right after the token before position.
//
// Takes recorder (*SpanRecorder) which records the statement's spans; nil records
// nothing.
// Takes tokens ([]Token[K]) which are the statement's tokens.
// Takes position (int) which is the index of the token the place comes before, such as a
// cursor's position.
func RecordOrderByPlaceAt[K comparable](recorder *SpanRecorder, tokens []Token[K], position int) {
	if position > 0 && position <= len(tokens) {
		recorder.RecordOrderByPlace(tokens[position-1].End)
	}
}

// SourceBetween returns the source text spanning the tokens from first up to but
// excluding end, sliced from the script exactly as written. Without the script, or when
// the tokens do not lie within it, it joins the tokens' raw text with single spaces.
//
// Takes source (string) which is the script the tokens were read from, or empty.
// Takes tokens ([]Token[K]) which are the tokens; a parser whose stream ends in an
// end-of-input token passes the tokens before it.
// Takes first (int) which is the index of the first token.
// Takes end (int) which is one past the index of the last token.
//
// Returns string which is the text, or empty for an empty or invalid range.
func SourceBetween[K comparable](source string, tokens []Token[K], first, end int) string {
	end = min(end, len(tokens))
	if first < 0 || first >= end {
		return ""
	}
	firstToken, lastToken := tokens[first], tokens[end-1]
	if source != "" && firstToken.Start >= 0 && firstToken.Start <= lastToken.End && lastToken.End <= len(source) {
		return source[firstToken.Start:lastToken.End]
	}
	parts := make([]string, 0, end-first)
	for index := first; index < end; index++ {
		parts = append(parts, tokens[index].Raw)
	}
	return strings.Join(parts, " ")
}

// PlaceholderSpans locates a statement's placeholders using the dialect's forms.
//
// Offsets are relative to the statement's first byte. A placeholder used alone in an IN
// list also records the list and operator spans.
//
// Takes tokens ([]Token[K]) which are the statement's significant tokens in order, every
// nested query's included.
// Takes kinds (PlaceholderTokens[K]) which classifies the tokens.
// Takes placeholders (dialect.Placeholders) which describes the dialect's forms.
// Takes statementStart (int) which is the offset of the statement's first byte in the
// text the tokens were read from.
//
// Returns []ast.PlaceholderSpan which holds one span per placeholder, in source order.
func PlaceholderSpans[K comparable](
	tokens []Token[K],
	kinds PlaceholderTokens[K],
	placeholders dialect.Placeholders,
	statementStart int,
) []ast.PlaceholderSpan {
	var spans []ast.PlaceholderSpan
	numbering := placeholderNumbering{anonymous: 0, largest: 0, names: make(map[string]struct{})}
	for index, token := range tokens {
		if !kinds.IsParameter(token) {
			continue
		}
		span := ast.PlaceholderSpan{
			Name:   "",
			List:   inListSpan(tokens, index, kinds, statementStart),
			Span:   TokenSpan(token, token, statementStart),
			Type:   ast.Span{Start: 0, End: 0},
			Number: 0,
		}
		if reading, ok := placeholders.Read(token.Raw); ok {
			numbering.describe(&span, reading)
		}
		spans = append(spans, span)
	}
	return spans
}

// NumberPlaceholders gives each placeholder span the number its form implies, as
// PlaceholderSpans does, from what each placeholder's text reads as. A span whose text
// reads under no form is left as it is.
//
// Takes texts ([]string) which holds each span's text.
// Takes placeholders (dialect.Placeholders) which describes the dialect's forms.
//
// Returns []int which holds the number each span should record.
func NumberPlaceholders(texts []string, placeholders dialect.Placeholders) []int {
	numbering := placeholderNumbering{anonymous: 0, largest: 0, names: make(map[string]struct{})}
	numbers := make([]int, len(texts))
	for index, text := range texts {
		reading, ok := placeholders.Read(text)
		if !ok {
			continue
		}
		var span ast.PlaceholderSpan
		numbering.describe(&span, reading)
		numbers[index] = span.Number
	}
	return numbers
}

// TerminatorSpan locates the terminator that ends a statement: the text after the
// statement's last token, past any whitespace and comments, when it opens with the rules'
// terminator.
//
// Takes rules (sqllex.Rules) which are the dialect's lexical rules.
// Takes source (string) which is the text the statement was read from.
// Takes statementStart (int) which is the offset of the statement's first byte in source.
// Takes statementEnd (int) which is the offset one past the statement's last byte.
//
// Returns ast.Span which covers the terminator as offsets from the statement's first
// byte, or is zero when no terminator follows.
func TerminatorSpan(rules sqllex.Rules, source string, statementStart, statementEnd int) ast.Span {
	if statementEnd < 0 || statementEnd > len(source) {
		return ast.Span{Start: 0, End: 0}
	}
	terminator := rules.Terminator
	if terminator == "" {
		terminator = sqllex.DefaultTerminator
	}
	scanner := sqllex.NewScanner(rules, source[statementEnd:])
	offset := scanner.SkipNoise()
	if scanner.Err() != nil || !strings.HasPrefix(source[statementEnd+offset:], terminator) {
		return ast.Span{Start: 0, End: 0}
	}
	start := statementEnd + offset - statementStart
	return ast.Span{Start: start, End: start + len(terminator)}
}

// inListSpan locates the IN list whose sole element is the token at index, as in x IN
// ($1) or x NOT IN ($1).
//
// Takes tokens ([]Token[K]) which are the statement's significant tokens.
// Takes index (int) which is the placeholder's index.
// Takes kinds (PlaceholderTokens[K]) which classifies the tokens.
// Takes statementStart (int) which is the offset of the statement's first byte.
//
// Returns ast.ListSpan which locates the list, or is zero when the placeholder is not the
// sole element of one.
func inListSpan[K comparable](tokens []Token[K], index int, kinds PlaceholderTokens[K], statementStart int) ast.ListSpan {
	none := ast.ListSpan{Operator: ast.Span{Start: 0, End: 0}, List: ast.Span{Start: 0, End: 0}, Negated: false}
	if index < 2 || index+1 >= len(tokens) {
		return none
	}
	open, closing, operator := tokens[index-1], tokens[index+1], tokens[index-2]
	if open.Kind != kinds.LeftParenthesis || closing.Kind != kinds.RightParenthesis || !operator.IsKeyword(kinds.Word, inKeyword) {
		return none
	}
	first := operator
	negated := index >= notOffset && tokens[index-notOffset].IsKeyword(kinds.Word, notKeyword)
	if negated {
		first = tokens[index-notOffset]
	}
	return ast.ListSpan{
		Operator: TokenSpan(first, operator, statementStart),
		List:     TokenSpan(open, closing, statementStart),
		Negated:  negated,
	}
}
