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
	"context"
	"fmt"
	"slices"

	"seagull.sh/seagull/internal/sqllex"
)

// estimatedBytesPerToken is the average source length of a token LexStatements assumes
// when it sizes the buffer it tokenises a statement into.
const estimatedBytesPerToken = 4

// LexedStatement is one statement of a script: where the engine's lexical rules place it
// and the tokens the dialect's tokeniser read from it.
type LexedStatement[K comparable] struct {
	// Tokens holds the statement's tokens, without the end-of-input token. For a statement
	// over the token budget it holds only the tokens read before the budget was reached,
	// enough to classify the statement.
	Tokens []Token[K]

	// Span is where the statement sits in the script.
	Span sqllex.Statement

	// OverBudget reports a statement that holds more tokens than the budget, which the
	// dialect refuses, as TokenBudgetExceeded reports.
	OverBudget bool
}

// StatementTokeniser starts reading the tokens of one statement.
//
// It returns a reader that gives the statement's tokens one at a time, in order, then a
// token of the end-of-input kind at the end of the statement, or an error when the text
// cannot be tokenised. Token positions are byte offsets into input. LexStatements stops
// calling the reader at the end of input, at an error, or once the statement passes the
// token budget, so tokenising never reads far past the budget.
//
// Takes input (string) which is the whole script.
// Takes start (int) which is the byte offset of the statement's first byte.
// Takes end (int) which is the byte offset one past its last byte.
//
// Returns func() (Token[K], error) which reads the next token.
type StatementTokeniser[K comparable] func(input string, start, end int) func() (Token[K], error)

// LexStatements cuts a script into statements under the engine's lexical rules and
// tokenises each one in place.
//
// sqllex decides where every statement begins and ends, so generation and the migration
// runner agree on statement boundaries. The dialect's tokeniser then reads each
// statement's text, keeping token positions as byte offsets into the script. A statement
// with no tokens is left out. A statement holding more than maxTokens tokens keeps only
// its first maxTokens and is marked OverBudget, so one huge statement costs no more than
// the budget allows.
//
// Takes rules (sqllex.Rules) which are the engine's lexical rules.
// Takes sql (string) which is the script.
// Takes tokenise (StatementTokeniser[K]) which reads one statement's tokens.
// Takes endKind (K) which is the kind of the end-of-input token, which is dropped.
// Takes maxTokens (int) which is the per-statement token budget; a value below 1 selects
// DefaultMaxTokensPerStatement.
//
// Returns []LexedStatement[K] which holds the statements in source order.
// Returns error which is the context's cause once it is done, or an error when sqllex
// finds an unterminated construct, such as a string, comment or procedural block, or the
// tokeniser rejects a statement.
func LexStatements[K comparable](
	ctx context.Context,
	rules sqllex.Rules,
	sql string,
	tokenise StatementTokeniser[K],
	endKind K,
	maxTokens int,
) ([]LexedStatement[K], error) {
	if maxTokens < 1 {
		maxTokens = DefaultMaxTokensPerStatement
	}
	spans, err := sqllex.Split(rules, sql)
	if err != nil {
		return nil, fmt.Errorf("splitting SQL: %w", err)
	}
	statements := make([]LexedStatement[K], 0, len(spans))
	var buffer []Token[K]
	for _, span := range spans {
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		estimate := min((span.End-span.Start)/estimatedBytesPerToken+1, maxTokens)
		buffer = slices.Grow(buffer[:0], estimate)
		var overBudget bool
		buffer, overBudget, err = tokeniseStatement(buffer, tokenise(sql, span.Start, span.End), endKind, maxTokens)
		if err != nil {
			return nil, fmt.Errorf("tokenising SQL: %w", err)
		}
		if len(buffer) == 0 {
			continue
		}
		statements = append(statements, LexedStatement[K]{Tokens: slices.Clone(buffer), Span: span, OverBudget: overBudget})
	}
	return statements, nil
}

// tokeniseStatement reads one statement's tokens into a buffer, stopping at the end of
// input or once the statement holds more tokens than the budget.
//
// Takes buffer ([]Token[K]) which is the empty buffer the tokens are appended to.
// Takes next (func() (Token[K], error)) which reads the next token.
// Takes endKind (K) which is the kind of the end-of-input token.
// Takes maxTokens (int) which is the per-statement token budget.
//
// Returns []Token[K] which holds the tokens read, at most maxTokens of them.
// Returns bool which is true when a token past the budget was found.
// Returns error which is the tokeniser's error.
func tokeniseStatement[K comparable](buffer []Token[K], next func() (Token[K], error), endKind K, maxTokens int) ([]Token[K], bool, error) {
	for {
		token, err := next()
		if err != nil {
			return buffer[:0], false, err
		}
		if token.Kind == endKind {
			return buffer, false, nil
		}
		if len(buffer) >= maxTokens {
			return buffer, true, nil
		}
		buffer = append(buffer, token)
	}
}
