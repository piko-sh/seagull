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
	"slices"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// statementsWithoutAnalysis lists statement prefixes accepted without structural
// analysis.
//
// They cover commands such as transaction control and maintenance. Other unsupported
// statement prefixes are rejected.
var statementsWithoutAnalysis = map[string]struct{}{
	"ABORT": {}, "ALTER": {}, "ANALYZE": {}, "ANALYSE": {}, "BEGIN": {}, "CHECKPOINT": {}, "CLOSE": {},
	"CLUSTER": {}, "COMMENT": {}, "COMMIT": {}, "COPY": {}, keywordCREATE: {}, "DEALLOCATE": {}, "DECLARE": {},
	"DISCARD": {}, "DO": {}, keywordDROP: {}, "END": {}, "EXECUTE": {}, "EXPLAIN": {}, keywordFETCH: {},
	"GRANT": {}, "IMPORT": {}, "LISTEN": {}, "LOAD": {}, "LOCK": {}, "MOVE": {}, "NOTIFY": {}, "PREPARE": {},
	"REASSIGN": {}, "REFRESH": {}, "REINDEX": {}, "RELEASE": {}, "RESET": {}, "REVOKE": {}, "ROLLBACK": {},
	"SAVEPOINT": {}, "SECURITY": {}, keywordSET: {}, "SHOW": {}, "START": {}, "TRUNCATE": {}, "UNLISTEN": {},
	"VACUUM": {},
}

// unboundParameterHint names, for a statement the server takes no parameter in, the
// built-in function that does the same work from a value and the hint to give.
type unboundParameterHint struct {
	// function is the built-in function the hint names.
	function string

	// hint says what to write instead.
	hint string
}

var (
	// unboundParameterHints holds, keyed by the word a statement opens with, the way to do
	// what a statement the server takes no parameter in does from a value.
	unboundParameterHints = map[string]unboundParameterHint{
		keywordSET: {function: "set_config", hint: "the server takes no parameter in SET either, so set it with " +
			"SELECT set_config('setting', $1, true), true for SET LOCAL and false for the session"},
		"NOTIFY": {function: "pg_notify", hint: "the server takes no parameter in NOTIFY either, so send the payload with " +
			"SELECT pg_notify('channel', $1)"},
	}
)

// unanalysedStatementError judges a statement the analyser has no routine for.
//
// A statement whose kind the dialect or one of its extensions recognised, or that opens
// with a word of statementsWithoutAnalysis, has nothing to analyse; any other is rejected
// with a syntax error at its first token that names what is wrong. A parameter in a
// statement with nothing to analyse is refused at the parameter, as the generated code
// could not bind it, with a hint where a built-in function the server has does the same
// work from a value.
//
// Takes parsed (*parsedStatement) which is the classified statement.
//
// Returns error which is the dialectkit.ParseError, or nil when the statement has nothing
// to analyse.
func (engine *Dialect) unanalysedStatementError(parsed *parsedStatement) error {
	if len(parsed.tokens) == 0 {
		return nil
	}
	first := parsed.tokens[0]
	if parsed.kind == statementKindUnknown {
		switch {
		case first.Kind == tokenLeftParen:
			return dialectkit.NewParseError(first, "expected a query inside the parentheses")
		case first.IsKeyword(tokenIdentifier, keywordWITH):
			return dialectkit.NewParseError(first, "expected SELECT, INSERT, UPDATE, DELETE or VALUES after the WITH list")
		}
		if _, runs := statementsWithoutAnalysis[first.Keyword(tokenIdentifier)]; !runs {
			return dialectkit.NewParseError(first, "not a statement Seagull can analyse")
		}
	}
	index := slices.IndexFunc(parsed.tokens, func(candidate token) bool {
		return candidate.Kind == tokenDollarParam || candidate.Kind == tokenNamedParam
	})
	if index < 0 {
		return nil
	}
	verb := first.Keyword(tokenIdentifier)
	return dialectkit.UnboundParameterError(parsed.tokens[index], verb, engine.unboundParameterHint(verb))
}

// unboundParameterHint returns the hint for a parameter in a statement opening with verb,
// or nothing when there is none or the server lacks the function it names.
//
// Takes verb (string) which is the word the statement opens with, upper-cased.
//
// Returns string which is the hint, or empty.
func (engine *Dialect) unboundParameterHint(verb string) string {
	found, known := unboundParameterHints[verb]
	if !known || slices.Contains(engine.settings.WithheldFunctions, found.function) {
		return ""
	}
	return found.hint
}
