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

// requiresNoTransaction reports whether a statement cannot run inside a transaction. That
// is one that mentions CONCURRENTLY (CREATE INDEX, DROP INDEX and REINDEX CONCURRENTLY),
// or one of the commands refusedInTransaction and alterRefusedInTransaction name.
//
// Takes tokens ([]sqllex.Token) which holds the statement's significant tokens.
//
// Returns bool which is true for such a statement.
func requiresNoTransaction(tokens []sqllex.Token) bool {
	if len(tokens) == 0 {
		return false
	}
	for index := range tokens {
		if tokens[index].IsWord("CONCURRENTLY") {
			return true
		}
	}
	return refusedInTransaction(tokens) || alterRefusedInTransaction(tokens)
}

// refusedInTransaction reports the commands PostgreSQL refuses inside a transaction
// block: VACUUM, CREATE or DROP of a database or tablespace, REINDEX of a database or the
// system catalogues, CLUSTER of every table and DISCARD ALL.
//
// Takes tokens ([]sqllex.Token) which are the statement's significant tokens.
//
// Returns bool which is true for such a command.
func refusedInTransaction(tokens []sqllex.Token) bool {
	switch {
	case tokens[0].IsWord("VACUUM"):
		return true
	case tokens[0].IsWord("CREATE") || tokens[0].IsWord("DROP"):
		return wordAt(tokens, 1, "DATABASE") || wordAt(tokens, 1, "TABLESPACE")
	case tokens[0].IsWord("REINDEX"):
		return wordAt(tokens, 1, "DATABASE") || wordAt(tokens, 1, "SYSTEM")
	case tokens[0].IsWord("CLUSTER"):
		return clustersEveryTable(tokens)
	case tokens[0].IsWord("DISCARD"):
		return wordAt(tokens, 1, "ALL")
	default:
		return false
	}
}

// alterRefusedInTransaction reports the ALTER commands that cannot run usefully inside a
// transaction.
//
// These are ALTER SYSTEM and ALTER DATABASE ... SET TABLESPACE, which PostgreSQL refuses
// there, and ALTER TYPE ... ADD VALUE, whose new value the same transaction cannot use,
// so the statements after it in the migration could not use it either.
//
// Takes tokens ([]sqllex.Token) which are the statement's significant tokens.
//
// Returns bool which is true for such a command.
func alterRefusedInTransaction(tokens []sqllex.Token) bool {
	if !tokens[0].IsWord("ALTER") {
		return false
	}
	switch {
	case wordAt(tokens, 1, "SYSTEM"):
		return true
	case wordAt(tokens, 1, "DATABASE"):
		return followedBy(tokens, "SET", "TABLESPACE")
	case wordAt(tokens, 1, "TYPE"):
		return followedBy(tokens, "ADD", "VALUE")
	default:
		return false
	}
}

// clustersEveryTable reports whether a CLUSTER statement names no table, written as
// CLUSTER or CLUSTER VERBOSE, so it reclusters every table.
//
// Takes tokens ([]sqllex.Token) which are the statement's significant tokens.
//
// Returns bool which is true for such a statement.
func clustersEveryTable(tokens []sqllex.Token) bool {
	bare := len(tokens) == 1
	verboseOnly := len(tokens) == 2 && tokens[1].IsWord("VERBOSE")
	return bare || verboseOnly
}

// wordAt reports whether the token at an index is a given word.
//
// Takes tokens ([]sqllex.Token) which are the tokens.
// Takes index (int) which is the position.
// Takes word (string) which is the word, matched without regard to case.
//
// Returns bool which is true when the token is the word.
func wordAt(tokens []sqllex.Token, index int, word string) bool {
	return index < len(tokens) && tokens[index].IsWord(word)
}

// followedBy reports whether two words appear next to each other among the tokens.
//
// Takes tokens ([]sqllex.Token) which are the tokens.
// Takes first (string) which is the first word.
// Takes second (string) which is the word right after it.
//
// Returns bool which is true when the pair appears.
func followedBy(tokens []sqllex.Token, first, second string) bool {
	for index := 0; index+1 < len(tokens); index++ {
		if tokens[index].IsWord(first) && tokens[index+1].IsWord(second) {
			return true
		}
	}
	return false
}
