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

package dialecttest

import (
	"fmt"
	"slices"

	"seagull.sh/seagull/internal/sqllex"
)

const (
	// spanExcerptBytes is how much source text a difference quotes.
	spanExcerptBytes = 24
)

// TokenClass is the lexical class of a token a dialect's own tokeniser reads, as far as
// CompareTokeniserSpans needs it.
type TokenClass uint8

const (
	// TokenOther is any token whose span the comparison does not check, such as an operator,
	// a number or a parameter.
	TokenOther TokenClass = iota

	// TokenWord is an unquoted identifier or keyword.
	TokenWord

	// TokenQuotedIdentifier is a delimited identifier.
	TokenQuotedIdentifier

	// TokenString is a string literal of any form.
	TokenString
)

// Token is one token from a dialect's own tokeniser: where it starts and its class.
type Token struct {
	// Position is the byte offset of the token's first byte.
	Position int

	// Class is the token's lexical class.
	Class TokenClass
}

// NoiseSkipper advances past whitespace and comments the way a dialect's tokeniser does.
//
// Takes source (string) which is the text.
// Takes position (int) which is the offset to start from.
//
// Returns int which is the offset of the next token.
// Returns error when a comment is unterminated.
type NoiseSkipper func(source string, position int) (int, error)

// spanComparison holds what one CompareTokeniserSpans call compares.
type spanComparison struct {
	// lexed indexes sqllex's significant tokens by their start offset.
	lexed map[int]sqllex.Token

	// dialectClasses indexes the dialect's tokens by their start offset.
	dialectClasses map[int]TokenClass

	// skipNoise skips noise the dialect's way.
	skipNoise NoiseSkipper

	// source is the text compared.
	source string

	// dialectPositions holds the dialect's token positions in order, ending with the end of
	// input.
	dialectPositions []int

	// problems collects each difference found.
	problems []string
}

// newSpanComparison indexes both token streams.
//
// Takes source (string) which is the text.
// Takes tokens ([]Token) which holds the dialect's tokens.
// Takes lexedTokens ([]sqllex.Token) which holds sqllex's tokens.
// Takes skipNoise (NoiseSkipper) which skips noise the dialect's way.
//
// Returns *spanComparison which is ready to compare.
func newSpanComparison(source string, tokens []Token, lexedTokens []sqllex.Token, skipNoise NoiseSkipper) *spanComparison {
	comparison := &spanComparison{
		lexed:            make(map[int]sqllex.Token, len(lexedTokens)),
		dialectClasses:   make(map[int]TokenClass, len(tokens)),
		skipNoise:        skipNoise,
		source:           source,
		dialectPositions: make([]int, 0, len(tokens)+1),
		problems:         nil,
	}
	for _, token := range lexedTokens {
		if !token.Kind.IsNoise() {
			comparison.lexed[token.Start] = token
		}
	}
	for _, token := range tokens {
		comparison.dialectClasses[token.Position] = token.Class
		comparison.dialectPositions = append(comparison.dialectPositions, token.Position)
	}
	comparison.dialectPositions = append(comparison.dialectPositions, len(source))
	return comparison
}

// checkDialectToken checks one dialect token against sqllex.
//
// Takes token (Token) which is the dialect's token.
// Takes next (int) which is the position of the dialect's next token, or the end of
// input.
func (comparison *spanComparison) checkDialectToken(token Token, next int) {
	expected, checked := lexedKindOf(token.Class)
	if !checked {
		return
	}
	lexed, found := comparison.lexed[token.Position]
	if !found || lexed.Kind != expected {
		comparison.report(token.Position, "the dialect reads a %s here but sqllex reads %s", expected, describeLexed(lexed, found))
		return
	}
	reached, err := comparison.skipNoise(comparison.source, lexed.End)
	if err != nil || reached != next {
		comparison.report(token.Position, "sqllex ends the %s at %d but the dialect's next token starts at %d", expected, lexed.End, next)
	}
}

// checkLexedToken checks one sqllex string, quoted identifier or comment against the
// dialect.
//
// Takes token (sqllex.Token) which is sqllex's token.
func (comparison *spanComparison) checkLexedToken(token sqllex.Token) {
	switch token.Kind {
	case sqllex.KindString, sqllex.KindQuotedIdentifier:
		class, found := comparison.dialectClasses[token.Start]
		if expected, _ := lexedKindOf(class); !found || expected != token.Kind {
			comparison.report(token.Start, "sqllex reads a %s here but the dialect does not", token.Kind)
		}
	case sqllex.KindComment:
		index, _ := slices.BinarySearch(comparison.dialectPositions, token.Start)
		if index < len(comparison.dialectPositions)-1 && comparison.dialectPositions[index] < token.End {
			comparison.report(token.Start, "sqllex reads a comment here but the dialect reads a token at %d", comparison.dialectPositions[index])
		}
	default:
	}
}

// report records a difference at offset.
//
// Takes offset (int) which is where the difference is.
// Takes format (string) which is the message format.
// Takes arguments (...any) which are the format's arguments.
func (comparison *spanComparison) report(offset int, format string, arguments ...any) {
	end := min(offset+spanExcerptBytes, len(comparison.source))
	comparison.problems = append(comparison.problems,
		fmt.Sprintf("byte %d (%q): ", offset, comparison.source[offset:end])+fmt.Sprintf(format, arguments...))
}

// CompareTokeniserSpans checks that sqllex, under rules, reads the words, strings, quoted
// identifiers and comments of source where the dialect's tokeniser does.
//
// Each word, string or quoted identifier the dialect reads must start a sqllex token of
// the same class, and from that token's end the dialect's noise skipper must reach the
// dialect's next token exactly, so both agree where the token ends. Each string or quoted
// identifier sqllex reads must start a dialect token of the same class, and no dialect
// token may start inside a sqllex comment.
//
// Takes rules (sqllex.Rules) which are the engine's lexical rules.
// Takes source (string) which is the text the dialect tokenised without error.
// Takes tokens ([]Token) which holds the dialect's tokens in order, without its
// end-of-input token.
// Takes skipNoise (NoiseSkipper) which skips noise the dialect's way.
//
// Returns []string which describes each difference; empty when the spans agree.
func CompareTokeniserSpans(rules sqllex.Rules, source string, tokens []Token, skipNoise NoiseSkipper) []string {
	lexedTokens, err := sqllex.Tokens(rules, source)
	if err != nil {
		return []string{fmt.Sprintf("sqllex cannot scan text the dialect tokenises: %v", err)}
	}
	comparison := newSpanComparison(source, tokens, lexedTokens, skipNoise)
	for index, token := range tokens {
		comparison.checkDialectToken(token, comparison.dialectPositions[index+1])
	}
	for _, token := range lexedTokens {
		comparison.checkLexedToken(token)
	}
	return comparison.problems
}

// lexedKindOf returns the sqllex kind a dialect class corresponds to.
//
// Takes class (TokenClass) which is the dialect token's class.
//
// Returns sqllex.Kind which is the corresponding kind.
// Returns bool which is false for TokenOther, whose spans are not checked.
func lexedKindOf(class TokenClass) (sqllex.Kind, bool) {
	switch class {
	case TokenWord:
		return sqllex.KindWord, true
	case TokenQuotedIdentifier:
		return sqllex.KindQuotedIdentifier, true
	case TokenString:
		return sqllex.KindString, true
	default:
		return sqllex.KindPunctuation, false
	}
}

// describeLexed names what sqllex read at a position.
//
// Takes token (sqllex.Token) which is the token found.
// Takes found (bool) which is false when no token starts there.
//
// Returns string which describes the token.
func describeLexed(token sqllex.Token, found bool) string {
	if !found {
		return "no token starting there"
	}
	return fmt.Sprintf("a %s %q", token.Kind, token.Raw)
}
