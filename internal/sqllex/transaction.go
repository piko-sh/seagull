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

package sqllex

import (
	"fmt"
)

// RequiresNoTransaction reports whether any statement of a script cannot run inside a
// transaction, by applying the rules' RequiresNoTransaction predicate to each statement's
// significant tokens.
//
// Strings, quoted identifiers and executable comments reach the predicate as whole
// tokens; their contents are not scanned as words. Ordinary comments and whitespace are
// omitted. A panic in the predicate, or in the rules' ProceduralBlock callback while the
// script is split, stops the check and is returned as an error.
//
// Takes rules (Rules) which describes the engine's lexical rules.
// Takes text (string) which is the script.
//
// Returns bool which is true when a statement matches the predicate; false when none does
// or the rules have no predicate.
// Returns error which is an *Error when the script cannot be split, or wraps a
// *CallbackError, and so ErrRulesCallback, when a rules callback panics.
func RequiresNoTransaction(rules Rules, text string) (bool, error) {
	predicate := rules.RequiresNoTransaction
	if predicate == nil {
		return false, nil
	}
	statements, err := Split(rules, text)
	if err != nil {
		return false, err
	}
	for index := range statements {
		statement := &statements[index]
		tokens := SignificantTokens(rules, statement.Text)
		required, callbackErr := callPredicate(requiresNoTransactionCallback, func() bool { return predicate(tokens) })
		if callbackErr != nil {
			return false, fmt.Errorf("checking the statement at line %d, column %d (byte %d): %w",
				statement.Line, statement.Column, statement.Start, callbackErr)
		}
		if required {
			return true, nil
		}
	}
	return false, nil
}

// SignificantTokens returns the tokens of text that are not whitespace or ordinary
// comments, stopping quietly at the first lexical error. Executable comments are kept.
//
// Takes rules (Rules) which describes the engine's lexical rules.
// Takes text (string) which is the SQL text, such as one statement's Text.
//
// Returns []Token which holds the significant tokens read before the end of the text or
// the first lexical error.
func SignificantTokens(rules Rules, text string) []Token {
	scanner := NewScanner(rules, text)
	var tokens []Token
	for {
		token, ok := scanner.NextSignificant()
		if !ok {
			return tokens
		}
		tokens = append(tokens, token)
	}
}
