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

// Rules returns ClickHouse's lexical rules.
//
// Strings are '...' with doubled quotes or backslash escapes, and the heredocs $$...$$
// and $tag$...$tag$, read verbatim up to the same tag. "..." and `...` are quoted
// identifiers, which also honour backslash escapes. Comments are --, # and nesting /* */.
// Parameters are typed {name:Type} placeholders, and the numbered $1 form the dialect's
// drivers bind by position, which the generated code writes where it binds that way, such
// as in the predicates a runtime query builder appends. A brace holding literal words, as
// in {true: false}, is no placeholder: ClickHouse reads it as a map literal in the rows
// of INSERT ... VALUES and has no type named false. ClickHouse has no procedural blocks,
// so every terminator outside quotes and comments ends a statement, and no statement
// needs to run outside a transaction.
//
// Returns sqllex.Rules which describes ClickHouse.
func Rules() sqllex.Rules {
	return sqllex.Rules{
		ProceduralBlock:       nil,
		RequiresNoTransaction: nil,
		Terminator:            sqllex.DefaultTerminator,
		Quotes: sqllex.QuoteRules{
			StringPrefixes:    "",
			SingleQuoteEscape: sqllex.EscapeDoubledOrBackslash,
			DoubleQuote:       sqllex.DoubleQuoteIdentifier,
			DoubleQuoteEscape: sqllex.EscapeDoubledOrBackslash,
			BacktickEscape:    sqllex.EscapeDoubledOrBackslash,
			Backticks:         true,
			Brackets:          false,
			DollarQuoting:     true,
			EscapeStrings:     false,
		},
		Parameters: sqllex.ParameterRules{
			NamedSigils:                  "",
			QuestionMark:                 false,
			NumberedQuestionMark:         false,
			DollarNumbers:                true,
			Braced:                       true,
			BracedLiteralWords:           []string{"true", "false", "null", "nan", "inf", "infinity"},
			NoNamedParameterAfterOperand: false,
		},
		Comments: sqllex.CommentRules{
			DoubleDashRequiresWhitespace: false,
			HashLineComments:             true,
			NestedBlockComments:          true,
			ExecutableComments:           false,
		},
		DelimiterCommand:             false,
		MetaCommands:                 false,
		TerminatorOutsideParentheses: false,
		DollarInIdentifiers:          false,
	}
}
