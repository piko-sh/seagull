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
	"strings"
)

// runOnCancelInterval is how many tokens RunOnStream reads between checks of the context,
// so a long statement can be cancelled without a check on every token.
const runOnCancelInterval = 4096

// RunOnRules tells RunOnStatement how a dialect writes words and parentheses and which
// words open a statement.
type RunOnRules[K comparable] struct {
	// Starters maps each upper-cased word that opens a statement to the upper-cased words
	// that may follow it there, such as CREATE to any word or DROP to TABLE and VIEW; an
	// empty list accepts any word.
	Starters map[string][]string

	// Word is the dialect's kind for words.
	Word K

	// Open is the dialect's kind for an opening parenthesis.
	Open K

	// Close is the dialect's kind for a closing parenthesis.
	Close K
}

// runOnScan follows a statement's tokens one at a time, in order, looking for a word that
// opens the next statement. It holds only the token before the current one and a
// candidate waiting for the word after it, so its memory does not grow with the
// statement.
type runOnScan[K comparable] struct {
	// rules say how the dialect writes words and statements.
	rules RunOnRules[K]

	// source is the text the tokens were read from.
	source string

	// followers are the words that may follow the candidate; empty accepts any word.
	followers []string

	// previous is the token before the one being read.
	previous Token[K]

	// candidate is a word that opens a line and may open the next statement, waiting for the
	// word after it.
	candidate Token[K]

	// depth is how many parentheses are open after the statement's first token.
	depth int

	// started reports that the statement's first token has been read.
	started bool

	// pending reports that candidate is waiting for the word after it.
	pending bool
}

// newRunOnScan returns a scan positioned before a statement's first token.
//
// Takes source (string) which is the text the tokens are read from.
// Takes rules (RunOnRules[K]) which say how the dialect writes words and statements.
//
// Returns *runOnScan[K] which has read no token.
func newRunOnScan[K comparable](source string, rules RunOnRules[K]) *runOnScan[K] {
	return &runOnScan[K]{
		rules:     rules,
		source:    source,
		followers: nil,
		previous:  Token[K]{},
		candidate: Token[K]{},
		depth:     0,
		started:   false,
		pending:   false,
	}
}

// step reads the statement's next token.
//
// Takes token (Token[K]) which is the token after the last one read.
//
// Returns Token[K] which is the word that opens the next statement.
// Returns bool which is true once the statement is found to run on.
func (scan *runOnScan[K]) step(token Token[K]) (Token[K], bool) {
	if scan.pending {
		scan.pending = false
		next := token.Keyword(scan.rules.Word)
		if next != "" && (len(scan.followers) == 0 || slices.Contains(scan.followers, next)) {
			return scan.candidate, true
		}
	}
	previous := scan.previous
	scan.previous = token
	if !scan.started {
		scan.started = true
		return Token[K]{}, false
	}
	switch token.Kind {
	case scan.rules.Open:
		scan.depth++
		return Token[K]{}, false
	case scan.rules.Close:
		scan.depth--
		return Token[K]{}, false
	default:
	}
	if scan.depth != 0 || !beginsLine(scan.source, previous, token) {
		return Token[K]{}, false
	}
	if followers, starts := scan.rules.Starters[token.Keyword(scan.rules.Word)]; starts {
		scan.candidate, scan.followers, scan.pending = token, followers, true
	}
	return Token[K]{}, false
}

// RunOnStatement finds where a statement runs on into the next one because the semicolon
// between them is missing, as when a migration forgets one and the second statement would
// otherwise be read as part of the first and lost.
//
// It looks for the first word after the statement's first token, outside any parentheses,
// that begins a line of the source and opens a statement as the rules describe, together
// with the word after it. A caller checks only statements that cannot hold another one,
// so the body of a function or trigger is never mistaken for a run-on.
//
// Takes source (string) which is the text the tokens were read from.
// Takes tokens ([]Token[K]) which are the statement's tokens.
// Takes rules (RunOnRules[K]) which say how the dialect writes words and statements.
//
// Returns Token[K] which is the word that opens the next statement.
// Returns bool which is true when the statement runs on.
func RunOnStatement[K comparable](source string, tokens []Token[K], rules RunOnRules[K]) (Token[K], bool) {
	scan := newRunOnScan(source, rules)
	for _, token := range tokens {
		if next, found := scan.step(token); found {
			return next, true
		}
	}
	return Token[K]{}, false
}

// RunOnStream finds where a statement runs on into the next one, as RunOnStatement does,
// reading the statement's tokens one at a time from a reader rather than from a slice.
//
// Its memory stays the same however long the statement is, so a dialect can check the
// whole of a statement that LexStatements kept only the first tokens of because it passed
// the token budget, such as a large INSERT seed, by reading the statement again with its
// StatementTokeniser. For a statement within the budget it finds what RunOnStatement
// finds over the statement's tokens.
//
// Takes source (string) which is the text the tokens are read from.
// Takes next (func() (Token[K], error)) which reads the statement's next token, as the
// reader a StatementTokeniser returns does.
// Takes endKind (K) which is the kind of the token that ends the statement.
// Takes rules (RunOnRules[K]) which say how the dialect writes words and statements.
//
// Returns Token[K] which is the word that opens the next statement.
// Returns bool which is true when the statement runs on.
// Returns error which is the context's cause once it is done, the reader's error, or an
// error wrapping ErrWalkBudget when the reader gives more tokens than source has bytes,
// as one that has stopped advancing does.
func RunOnStream[K comparable](
	ctx context.Context,
	source string,
	next func() (Token[K], error),
	endKind K,
	rules RunOnRules[K],
) (Token[K], bool, error) {
	scan := newRunOnScan(source, rules)
	for count := range len(source) + 1 {
		if count%runOnCancelInterval == 0 {
			if err := context.Cause(ctx); err != nil {
				return Token[K]{}, false, err
			}
		}
		token, err := next()
		if err != nil {
			return Token[K]{}, false, fmt.Errorf("tokenising SQL: %w", err)
		}
		if token.Kind == endKind {
			return Token[K]{}, false, nil
		}
		if found, runsOn := scan.step(token); runsOn {
			return found, true, nil
		}
	}
	return Token[K]{}, false, fmt.Errorf("%w: more tokens than the %d bytes of source", ErrWalkBudget, len(source))
}

// beginsLine reports whether a token is the first on its line: the source between it and
// the token before it holds a line break.
//
// Takes source (string) which is the text the tokens were read from.
// Takes previous (Token[K]) which is the token before.
// Takes current (Token[K]) which is the token.
//
// Returns bool which is true when a line break separates the two.
func beginsLine[K comparable](source string, previous, current Token[K]) bool {
	if previous.End < 0 || current.Start > len(source) || previous.End > current.Start {
		return false
	}
	return strings.Contains(source[previous.End:current.Start], "\n")
}
