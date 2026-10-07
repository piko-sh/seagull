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

package mysql

import (
	"context"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqllex"
)

// lexStatements cuts sql into statements under rules and tokenises each one in place.
//
// sqllex decides where every statement begins and ends, so generation and the migration
// runner agree on statement boundaries. The dialect's tokeniser then reads each
// statement's text, keeping token positions as byte offsets into sql. A statement holding
// more than maxTokens tokens keeps only its first maxTokens and is marked over budget.
//
// Takes rules (sqllex.Rules) which are the engine's lexical rules.
// Takes sql (string) which is the script.
// Takes maxTokens (int) which is the per-statement token budget.
//
// Returns []dialectkit.LexedStatement[tokenKind] which holds the statements in source
// order.
// Returns error which is the context's cause once it is done, or an error when sqllex
// finds an unterminated construct, such as a string, comment or procedural block, or the
// tokeniser rejects a statement.
func lexStatements(ctx context.Context, rules sqllex.Rules, sql string, maxTokens int) ([]dialectkit.LexedStatement[tokenKind], error) {
	return dialectkit.LexStatements(ctx, rules, sql, statementTokeniser(rules.Quotes.DoubleQuote), tokenEOF, maxTokens)
}

// statementTokeniser returns the reader of one statement's tokens under the SQL mode's
// meaning of "...".
//
// Takes doubleQuote (sqllex.DoubleQuoteMeaning) which says whether "..." is a string or a
// quoted identifier.
//
// Returns dialectkit.StatementTokeniser[tokenKind] which starts a reader at a statement.
func statementTokeniser(doubleQuote sqllex.DoubleQuoteMeaning) dialectkit.StatementTokeniser[tokenKind] {
	return func(input string, start, end int) func() (token, error) {
		lexer := &tokeniser{input: input[:end], position: start, nameEnd: -1, insideExecutableComment: false, doubleQuote: doubleQuote}
		return lexer.read
	}
}
