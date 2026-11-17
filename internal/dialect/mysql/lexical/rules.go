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
	"slices"

	"seagull.sh/seagull/internal/sqllex"
)

// notAtomicOpening is the words that open MariaDB's BEGIN NOT ATOMIC block.
var notAtomicOpening = []string{"BEGIN", "NOT", "ATOMIC"}

// Rules returns MySQL's lexical rules in the server's default SQL mode, which MariaDB
// shares.
//
// Strings are '...' with doubled quotes or backslash escapes, plus X'...' and B'...';
// "..." is a string too, with the same escapes, as the default mode reads it without
// ANSI_QUOTES. `...` is a quoted identifier. Comments are # , -- followed by whitespace,
// and non-nesting /* */. Parameters are ?, ?N and :name, where :name stands where an
// operand is expected: a colon written directly after a name ends a statement label, as
// in l1:LOOP. An identifier may contain '$'. The client-side DELIMITER command changes
// the terminator, and BEGIN opens a procedural block in a CREATE statement that names a
// FUNCTION, PROCEDURE, TRIGGER or EVENT, or in MariaDB's BEGIN NOT ATOMIC.
//
// Returns sqllex.Rules which describes MySQL.
func Rules() sqllex.Rules {
	return rules(sqllex.DoubleQuoteString)
}

// ANSIQuotesRules returns MySQL's lexical rules under the ANSI_QUOTES SQL mode, which
// reads "..." as a quoted identifier, in which only a doubled quote escapes a quote and a
// backslash is itself, as the server reads it. Every other rule is as Rules describes.
//
// Returns sqllex.Rules which describes MySQL under ANSI_QUOTES.
func ANSIQuotesRules() sqllex.Rules {
	return rules(sqllex.DoubleQuoteIdentifier)
}

// rules returns MySQL's lexical rules with the given meaning of "...".
//
// Takes doubleQuote (sqllex.DoubleQuoteMeaning) which says whether "..." is a string or a
// quoted identifier.
//
// Returns sqllex.Rules which describes MySQL.
func rules(doubleQuote sqllex.DoubleQuoteMeaning) sqllex.Rules {
	return sqllex.Rules{
		ProceduralBlock:       proceduralBlock,
		RequiresNoTransaction: nil,
		Terminator:            sqllex.DefaultTerminator,
		Quotes: sqllex.QuoteRules{
			StringPrefixes:    "BX",
			SingleQuoteEscape: sqllex.EscapeDoubledOrBackslash,
			DoubleQuote:       doubleQuote,
			DoubleQuoteEscape: doubleQuoteEscape(doubleQuote),
			BacktickEscape:    sqllex.EscapeDoubled,
			Backticks:         true,
			Brackets:          false,
			DollarQuoting:     false,
			EscapeStrings:     false,
		},
		Parameters: sqllex.ParameterRules{
			NamedSigils:                  ":",
			QuestionMark:                 true,
			NumberedQuestionMark:         true,
			DollarNumbers:                false,
			Braced:                       false,
			BracedLiteralWords:           nil,
			NoNamedParameterAfterOperand: true,
		},
		Comments: sqllex.CommentRules{
			DoubleDashRequiresWhitespace: true,
			HashLineComments:             true,
			NestedBlockComments:          false,
			ExecutableComments:           true,
		},
		DelimiterCommand:             true,
		MetaCommands:                 false,
		TerminatorOutsideParentheses: false,
		DollarInIdentifiers:          true,
	}
}

// proceduralBlock reports whether a statement can hold a BEGIN-END body.
//
// That is one that starts with CREATE and names FUNCTION, PROCEDURE, TRIGGER or EVENT
// before the BEGIN, allowing the DEFINER and SQL SECURITY clauses MySQL accepts in
// between, or MariaDB's BEGIN NOT ATOMIC block, which is a statement of its own.
//
// Takes leadingWords ([]string) which holds the statement's upper-cased leading words.
//
// Returns bool which is true for a procedural statement.
func proceduralBlock(leadingWords []string) bool {
	if opensNotAtomicBlock(leadingWords) {
		return true
	}
	if len(leadingWords) == 0 || leadingWords[0] != "CREATE" {
		return false
	}
	return slices.ContainsFunc(leadingWords[1:], func(word string) bool {
		return word == "FUNCTION" || word == "PROCEDURE" || word == "TRIGGER" || word == "EVENT"
	})
}

// opensNotAtomicBlock reports whether a statement opens with MariaDB's BEGIN NOT ATOMIC.
//
// Takes leadingWords ([]string) which holds the statement's upper-cased leading words.
//
// Returns bool which is true for such a block.
func opensNotAtomicBlock(leadingWords []string) bool {
	return len(leadingWords) >= len(notAtomicOpening) && slices.Equal(leadingWords[:len(notAtomicOpening)], notAtomicOpening)
}

// doubleQuoteEscape returns how a quote is written inside "...": doubled or after a
// backslash in a string, and only doubled in an identifier, in which MySQL reads a
// backslash as itself, so "a\" names a\.
//
// Takes doubleQuote (sqllex.DoubleQuoteMeaning) which says whether "..." is a string or a
// quoted identifier.
//
// Returns sqllex.EscapeMode which is the escape.
func doubleQuoteEscape(doubleQuote sqllex.DoubleQuoteMeaning) sqllex.EscapeMode {
	if doubleQuote == sqllex.DoubleQuoteIdentifier {
		return sqllex.EscapeDoubled
	}
	return sqllex.EscapeDoubledOrBackslash
}
