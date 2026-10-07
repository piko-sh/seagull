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
	postgres_lexical "seagull.sh/seagull/internal/dialect/postgres/lexical"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// continuousOptionLength is the token count of the reloption timescaledb.continuous.
	continuousOptionLength = 3
)

// Rules returns TimescaleDB's lexical rules: PostgreSQL's, with two more statements that
// cannot run inside a transaction. A CREATE MATERIALIZED VIEW with the
// timescaledb.continuous option materialises its data on creation unless it ends WITH NO
// DATA, and CALL refresh_continuous_aggregate commits as it goes; TimescaleDB rejects
// both inside a transaction block.
//
// Returns sqllex.Rules which describes TimescaleDB.
func Rules() sqllex.Rules {
	rules := postgres_lexical.Rules()
	base := rules.RequiresNoTransaction
	rules.RequiresNoTransaction = func(tokens []sqllex.Token) bool {
		return base(tokens) || createsContinuousAggregate(tokens) || refreshesContinuousAggregate(tokens)
	}
	return rules
}

// createsContinuousAggregate reports whether a statement creates a continuous aggregate
// with its data: CREATE [OR REPLACE] MATERIALIZED VIEW naming timescaledb.continuous, not
// switched off with = false or = off, and not ending WITH NO DATA.
//
// Takes tokens ([]sqllex.Token) which holds the statement's significant tokens.
//
// Returns bool which is true for such a statement.
func createsContinuousAggregate(tokens []sqllex.Token) bool {
	if !startsWithWords(tokens, "CREATE", "MATERIALIZED", "VIEW") &&
		!startsWithWords(tokens, "CREATE", "OR", "REPLACE", "MATERIALIZED", "VIEW") {
		return false
	}
	if endsWithWords(tokens, "WITH", "NO", "DATA") {
		return false
	}
	for index := 0; index+continuousOptionLength <= len(tokens); index++ {
		if tokens[index].IsWord("timescaledb") && tokens[index+1].Raw == "." && tokens[index+2].IsWord("continuous") {
			return !switchedOff(tokens[index+continuousOptionLength:])
		}
	}
	return false
}

// refreshesContinuousAggregate reports whether a statement is CALL
// refresh_continuous_aggregate, optionally schema-qualified.
//
// Takes tokens ([]sqllex.Token) which holds the statement's significant tokens.
//
// Returns bool which is true for such a statement.
func refreshesContinuousAggregate(tokens []sqllex.Token) bool {
	if len(tokens) == 0 || !tokens[0].IsWord("CALL") {
		return false
	}
	for index := 1; index < len(tokens) && tokens[index].Raw != "("; index++ {
		if tokens[index].IsWord("refresh_continuous_aggregate") {
			return true
		}
	}
	return false
}

// switchedOff reports whether an option's value, which follows its name, turns it off: =
// false, = off, = 'false' or = 'off'.
//
// Takes tokens ([]sqllex.Token) which holds the tokens after the option name.
//
// Returns bool which is true when the option is set to false.
func switchedOff(tokens []sqllex.Token) bool {
	if len(tokens) < 2 || tokens[0].Raw != "=" {
		return false
	}
	value := tokens[1]
	switch value.Kind {
	case sqllex.KindWord:
		return value.IsWord("false") || value.IsWord("off")
	case sqllex.KindString:
		return value.Raw == "'false'" || value.Raw == "'off'"
	default:
		return false
	}
}

// startsWithWords reports whether the statement begins with the given words.
//
// Takes tokens ([]sqllex.Token) which holds the statement's significant tokens.
// Takes words (...string) which are the words to match, in order.
//
// Returns bool which is true when every word matches.
func startsWithWords(tokens []sqllex.Token, words ...string) bool {
	if len(tokens) < len(words) {
		return false
	}
	for index, word := range words {
		if !tokens[index].IsWord(word) {
			return false
		}
	}
	return true
}

// endsWithWords reports whether the statement ends with the given words.
//
// Takes tokens ([]sqllex.Token) which holds the statement's significant tokens.
// Takes words (...string) which are the words to match, in order.
//
// Returns bool which is true when every word matches.
func endsWithWords(tokens []sqllex.Token, words ...string) bool {
	if len(tokens) < len(words) {
		return false
	}
	return startsWithWords(tokens[len(tokens)-len(words):], words...)
}
