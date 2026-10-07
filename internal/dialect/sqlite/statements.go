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

package sqlite

import (
	"context"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/sqlite/lexical"
)

// lexStatements cuts sql into statements under SQLite's lexical rules and tokenises each
// one in place, as dialectkit.LexStatements does.
//
// sqllex decides where every statement begins and ends, so generation and the migration
// runner agree on statement boundaries. The dialect's tokeniser then reads each
// statement's text, keeping token positions as byte offsets into sql. A statement holding
// more than maxTokens tokens keeps only its first maxTokens and is marked over budget.
//
// Takes sql (string) which is the script.
// Takes maxTokens (int) which is the per-statement token budget.
//
// Returns []dialectkit.LexedStatement[tokenKind] which holds the statements in source
// order.
// Returns error which is the context's cause once it is done, or an error when sqllex
// finds an unterminated construct, such as a string, comment or procedural block, or the
// tokeniser rejects a statement.
func lexStatements(ctx context.Context, sql string, maxTokens int) ([]dialectkit.LexedStatement[tokenKind], error) {
	return dialectkit.LexStatements(ctx, lexical.Rules(), sql, statementTokens, tokenEOF, maxTokens)
}
