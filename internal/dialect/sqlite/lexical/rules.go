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

// Rules returns SQLite's lexical rules.
//
// Strings are '...' with doubled quotes and X'...' blobs. "...", `...` and [...] are
// quoted identifiers. Comments are -- and non-nesting /* */. Parameters are ?, ?N, :name,
// @name and $name. An unquoted name continues through '$' after its first character, as
// in price$usd. BEGIN opens a procedural block in CREATE [TEMP] TRIGGER, and a statement
// that begins with VACUUM cannot run inside a transaction.
//
// Returns sqllex.Rules which describes SQLite.
func Rules() sqllex.Rules {
	return sqllex.Rules{
		ProceduralBlock:       proceduralBlock,
		RequiresNoTransaction: requiresNoTransaction,
		Terminator:            sqllex.DefaultTerminator,
		Quotes: sqllex.QuoteRules{
			StringPrefixes:    "X",
			SingleQuoteEscape: sqllex.EscapeDoubled,
			DoubleQuote:       sqllex.DoubleQuoteIdentifier,
			DoubleQuoteEscape: sqllex.EscapeDoubled,
			BacktickEscape:    sqllex.EscapeDoubled,
			Backticks:         true,
			Brackets:          true,
			DollarQuoting:     false,
			EscapeStrings:     false,
		},
		Parameters: sqllex.ParameterRules{
			NamedSigils:                  ":@$",
			QuestionMark:                 true,
			NumberedQuestionMark:         true,
			DollarNumbers:                false,
			Braced:                       false,
			BracedLiteralWords:           nil,
			NoNamedParameterAfterOperand: false,
		},
		Comments: sqllex.CommentRules{
			DoubleDashRequiresWhitespace: false,
			HashLineComments:             false,
			NestedBlockComments:          false,
			ExecutableComments:           false,
		},
		DelimiterCommand:             false,
		MetaCommands:                 false,
		TerminatorOutsideParentheses: false,
		DollarInIdentifiers:          true,
	}
}

// proceduralBlock reports whether a statement can hold a BEGIN ... END body: a CREATE
// [TEMP | TEMPORARY] TRIGGER.
//
// Takes leadingWords ([]string) which holds the statement's upper-cased leading words.
//
// Returns bool which is true for a trigger definition.
func proceduralBlock(leadingWords []string) bool {
	if len(leadingWords) == 0 || leadingWords[0] != "CREATE" {
		return false
	}
	for _, word := range leadingWords[1:] {
		switch word {
		case "TEMP", "TEMPORARY":
		case "TRIGGER":
			return true
		default:
			return false
		}
	}
	return false
}

// requiresNoTransaction reports whether a statement cannot run inside a transaction: one
// that begins with VACUUM.
//
// Takes tokens ([]sqllex.Token) which holds the statement's significant tokens.
//
// Returns bool which is true for a VACUUM.
func requiresNoTransaction(tokens []sqllex.Token) bool {
	return len(tokens) > 0 && tokens[0].IsWord("VACUUM")
}
