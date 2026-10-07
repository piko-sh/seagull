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

// Rules returns DuckDB's lexical rules.
//
// Strings are '...' with doubled quotes, B'...' bit strings, E'...' strings with
// backslash escapes and dollar-quoted $tag$...$tag$ bodies; "..." is a quoted identifier.
// Comments are -- and nesting /* */. Parameters are $1, ?, ?2, :name and $name, a "$"
// followed by a name and then by another "$" opening a dollar-quoted string instead, and
// a ":" written directly after an operand separating, as in lambda x:x or {'k':v}, rather
// than starting a parameter. BEGIN opens a procedural block in CREATE [OR REPLACE] [TEMP]
// MACRO and FUNCTION statements. The rules mark a leading VACUUM for execution outside a
// transaction.
//
// Returns sqllex.Rules which describes DuckDB.
func Rules() sqllex.Rules {
	return sqllex.Rules{
		ProceduralBlock:       proceduralBlock,
		RequiresNoTransaction: requiresNoTransaction,
		Terminator:            sqllex.DefaultTerminator,
		Quotes: sqllex.QuoteRules{
			StringPrefixes:    "B",
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
			NamedSigils:                  ":$",
			NoNamedParameterAfterOperand: true,
			QuestionMark:                 true,
			NumberedQuestionMark:         true,
			DollarNumbers:                true,
			Braced:                       false,
			BracedLiteralWords:           nil,
		},
		Comments: sqllex.CommentRules{
			DoubleDashRequiresWhitespace: false,
			HashLineComments:             false,
			NestedBlockComments:          true,
			ExecutableComments:           false,
		},
		DelimiterCommand:             false,
		MetaCommands:                 false,
		TerminatorOutsideParentheses: false,
		DollarInIdentifiers:          false,
	}
}

// proceduralBlock reports whether a statement can hold a BEGIN ... END body: a CREATE [OR
// REPLACE] [TEMP | TEMPORARY] MACRO or FUNCTION.
//
// Takes leadingWords ([]string) which holds the statement's upper-cased leading words.
//
// Returns bool which is true for a macro or function definition.
func proceduralBlock(leadingWords []string) bool {
	if len(leadingWords) == 0 || leadingWords[0] != "CREATE" {
		return false
	}
	for _, word := range leadingWords[1:] {
		switch word {
		case "OR", "REPLACE", "TEMP", "TEMPORARY":
		case "MACRO", "FUNCTION":
			return true
		default:
			return false
		}
	}
	return false
}

// requiresNoTransaction applies the dialect's transaction policy: a statement beginning
// with VACUUM is marked for execution outside a transaction.
//
// Takes tokens ([]sqllex.Token) which holds the statement's significant tokens.
//
// Returns bool which is true for a VACUUM.
func requiresNoTransaction(tokens []sqllex.Token) bool {
	return len(tokens) > 0 && tokens[0].IsWord("VACUUM")
}
