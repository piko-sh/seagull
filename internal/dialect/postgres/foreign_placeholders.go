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
	"errors"
	"fmt"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// atPlaceholderMessage warns of @name, which another tool's placeholder syntax writes
	// for a parameter.
	atPlaceholderMessage = "@%[1]s is no parameter here: @ before a name is the absolute value operator, so the " +
		"statement reads the column %[1]s or fails rather than taking a parameter; write $1 or :%[1]s"

	// questionPlaceholderMessage warns of ?, which another tool's placeholder syntax writes
	// for a parameter.
	questionPlaceholderMessage = "? is no parameter here: it is the operator that tests whether a jsonb document has " +
		"a key, so the statement fails; write $1 or :name"
)

var (
	// operandOpeningKeywords lists the keywords an operand follows, so a ? written after one
	// stands where a parameter would rather than between the two operands of the jsonb key
	// test.
	operandOpeningKeywords = map[string]struct{}{
		keywordSELECT: {}, keywordWHERE: {}, keywordAND: {}, "OR": {}, keywordNOT: {}, keywordON: {}, keywordSET: {},
		keywordVALUES: {}, keywordLIMIT: {}, keywordOFFSET: {}, keywordFETCH: {}, "FIRST": {}, "NEXT": {},
		"WHEN": {}, "THEN": {}, "ELSE": {}, "CASE": {}, keywordBY: {}, "IN": {}, "IS": {}, "LIKE": {}, "ILIKE": {},
		"BETWEEN": {}, keywordHAVING: {}, keywordRETURNING: {}, "ANY": {}, keywordALL: {}, "SOME": {},
		"DISTINCT": {}, keywordFROM: {}, "TO": {},
	}
)

// foreignPlaceholderWarnings flags @name and operand-position question marks that
// PostgreSQL interprets as operators rather than parameters. Such placeholders generate
// no method parameter and can fail or read a column instead.
//
// Takes tokens ([]token) which is the statement's token stream.
// Takes warnings (*statementWarnings) which receives one warning per such placeholder, in
// order, while it has room.
func foreignPlaceholderWarnings(tokens []token, warnings *statementWarnings) {
	for index := range tokens {
		if isForeignPlaceholder(tokens, index) && warnings.room() {
			warnings.add(foreignPlaceholderWarning(tokens, index))
		}
	}
}

// isForeignPlaceholder reports whether the token at index writes a placeholder in another
// tool's syntax: an @ written right before a name, or a ? where an operand stands.
//
// Takes tokens ([]token) which is the statement's token stream.
// Takes index (int) which is the index of the token.
//
// Returns bool which is true for such a placeholder.
func isForeignPlaceholder(tokens []token, index int) bool {
	current := tokens[index]
	if current.Kind != tokenOperator {
		return false
	}
	switch current.Value {
	case "@":
		return index+1 < len(tokens) && writesName(tokens[index+1], current.End)
	case "?":
		return index == 0 || !endsOperand(tokens[index-1])
	default:
		return false
	}
}

// foreignPlaceholderWarning builds the warning for the placeholder in another tool's
// syntax at index, which isForeignPlaceholder has found.
//
// Takes tokens ([]token) which is the statement's token stream.
// Takes index (int) which is the index of the placeholder's first token.
//
// Returns ast.Warning which says how PostgreSQL reads the placeholder and how to write
// it.
func foreignPlaceholderWarning(tokens []token, index int) ast.Warning {
	current := tokens[index]
	if current.Value == "@" {
		return ast.Warning{Message: fmt.Sprintf(atPlaceholderMessage, tokens[index+1].Value),
			Span: ast.Span{Start: current.Start, End: tokens[index+1].End}}
	}
	return ast.Warning{Message: questionPlaceholderMessage, Span: ast.Span{Start: current.Start, End: current.End}}
}

// writesName reports whether a token is an unquoted name written right after an offset,
// with no space between.
//
// Takes candidate (token) which is the token.
// Takes after (int) which is the offset the name must start at.
//
// Returns bool which is true for such a name.
func writesName(candidate token, after int) bool {
	return candidate.Kind == tokenIdentifier && !candidate.Quoted && candidate.Start == after
}

// endsOperand reports whether a token ends an operand: a name other than a keyword an
// operand follows, a literal, a parameter or a closing bracket.
//
// Takes candidate (token) which is the token.
//
// Returns bool which is true when an operand ends at the token.
func endsOperand(candidate token) bool {
	switch candidate.Kind {
	case tokenIdentifier:
		if candidate.Quoted {
			return true
		}
		_, opensOperand := operandOpeningKeywords[candidate.Keyword(tokenIdentifier)]
		return !opensOperand
	case tokenString, tokenEscapeString, tokenDollarString, tokenBitString, tokenNumber, tokenRightParen,
		tokenRightBracket, tokenDollarParam, tokenNamedParam:
		return true
	default:
		return false
	}
}

// explainForeignPlaceholder replaces a syntax error in a statement that writes a
// placeholder in another tool's syntax with one at the first such placeholder, which is
// what PostgreSQL could not read, saying how to write it.
//
// Takes err (error) which is the statement's error.
// Takes tokens ([]token) which is the statement's token stream.
//
// Returns error which is the error at the placeholder, or err when it is no syntax error
// or the statement writes no foreign placeholder.
func explainForeignPlaceholder(err error, tokens []token) error {
	if _, isParseError := errors.AsType[*dialectkit.ParseError](err); !isParseError {
		return err
	}
	for index, current := range tokens {
		if isForeignPlaceholder(tokens, index) {
			return dialectkit.NewParseError(current, foreignPlaceholderWarning(tokens, index).Message)
		}
	}
	return err
}
