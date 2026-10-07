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

package postgres

import (
	"context"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// lexStatements cuts sql into statements under the dialect's lexical rules and tokenises
// each one in place, no further than the per-statement token budget.
//
// sqllex decides where every statement begins and ends, so generation and the migration
// runner agree on statement boundaries. The dialect's tokeniser then reads each
// statement's text, keeping token positions as byte offsets into sql.
//
// Takes sql (string) which is the script.
//
// Returns []dialectkit.LexedStatement[tokenKind] which holds the statements in source
// order, each over the budget marked so.
// Returns error which is the context's cause once it is done, or an error when sqllex
// finds an unterminated construct, such as a string, comment or procedural block, or the
// tokeniser rejects a statement.
func (engine *Dialect) lexStatements(ctx context.Context, sql string) ([]dialectkit.LexedStatement[tokenKind], error) {
	return dialectkit.LexStatements(ctx, engine.settings.LexicalRules, sql, readStatementTokens, tokenEOF,
		engine.settings.Limits.TokensPerStatement())
}
