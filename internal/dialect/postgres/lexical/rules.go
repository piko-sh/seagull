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

package lexical

import (
	"seagull.sh/seagull/internal/sqllex"
)

// Rules returns PostgreSQL's lexical rules.
//
// Strings are '...' with doubled quotes, E'...' with backslash escapes, B'...' and X'...'
// bit strings, N'...' national character strings and dollar-quoted $tag$...$tag$ bodies;
// "..." is a quoted identifier. Comments are -- and nesting /* */. Parameters are $1 and
// :name, where :name stands where an operand is expected: a colon written directly after
// an operand separates the bounds of an array slice, as in tags[lo:hi]. An identifier may
// continue with '$'. A terminator inside parentheses does not end the statement, as in
// psql, and BEGIN opens a procedural block in DO and in CREATE FUNCTION, PROCEDURE and
// TRIGGER statements. CONCURRENTLY anywhere, or a leading VACUUM, marks a statement that
// cannot run inside a transaction.
//
// Returns sqllex.Rules which describes PostgreSQL.
func Rules() sqllex.Rules {
	return sqllex.Rules{
		ProceduralBlock:       proceduralBlock,
		RequiresNoTransaction: requiresNoTransaction,
		Terminator:            sqllex.DefaultTerminator,
		Quotes: sqllex.QuoteRules{
			StringPrefixes:    "BXN",
			SingleQuoteEscape: sqllex.EscapeDoubled,
			DoubleQuote:       sqllex.DoubleQuoteIdentifier,
			DoubleQuoteEscape: sqllex.EscapeDoubled,
			BacktickEscape:    sqllex.EscapeDoubled,
			Backticks:         false,
			Brackets:          false,
			DollarQuoting:     true,
			EscapeStrings:     true,
		},
		Parameters: sqllex.ParameterRules{
			NamedSigils:                  ":",
			QuestionMark:                 false,
			NumberedQuestionMark:         false,
			DollarNumbers:                true,
			Braced:                       false,
			BracedLiteralWords:           nil,
			NoNamedParameterAfterOperand: true,
		},
		Comments: sqllex.CommentRules{
			DoubleDashRequiresWhitespace: false,
			HashLineComments:             false,
			NestedBlockComments:          true,
			ExecutableComments:           false,
		},
		DelimiterCommand:             false,
		MetaCommands:                 true,
		TerminatorOutsideParentheses: true,
		DollarInIdentifiers:          true,
	}
}

// proceduralBlock reports whether a statement can hold a BEGIN ... END body: a DO, or a
// CREATE [OR REPLACE] [TEMP | TEMPORARY] [CONSTRAINT] FUNCTION, PROCEDURE or TRIGGER.
//
// Takes leadingWords ([]string) which holds the statement's upper-cased leading words.
//
// Returns bool which is true for a procedural statement.
func proceduralBlock(leadingWords []string) bool {
	if len(leadingWords) == 0 {
		return false
	}
	if leadingWords[0] == "DO" {
		return true
	}
	if leadingWords[0] != "CREATE" {
		return false
	}
	for _, word := range leadingWords[1:] {
		switch word {
		case "OR", "REPLACE", "TEMP", "TEMPORARY", "CONSTRAINT":
		case "FUNCTION", "PROCEDURE", "TRIGGER":
			return true
		default:
			return false
		}
	}
	return false
}

// requiresNoTransaction reports whether a statement cannot run inside a transaction: one
// that mentions CONCURRENTLY (CREATE INDEX, DROP INDEX and REINDEX CONCURRENTLY) or one
// that begins with VACUUM.
//
// Takes tokens ([]sqllex.Token) which holds the statement's significant tokens.
//
// Returns bool which is true for such a statement.
func requiresNoTransaction(tokens []sqllex.Token) bool {
	if len(tokens) > 0 && tokens[0].IsWord("VACUUM") {
		return true
	}
	for index := range tokens {
		if tokens[index].IsWord("CONCURRENTLY") {
			return true
		}
	}
	return false
}
