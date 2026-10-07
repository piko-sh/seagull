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

package dialectkit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	lexEnd  = 0
	lexWord = 1
)

var (
	errUnreadable = errors.New("unreadable character")
	errStopped    = errors.New("generation stopped")
)

func wordTokeniser(reads *int) dialectkit.StatementTokeniser[int] {
	return func(input string, start, end int) func() (dialectkit.Token[int], error) {
		position := start
		return func() (dialectkit.Token[int], error) {
			*reads++
			for position < end && input[position] == ' ' {
				position++
			}
			if position >= end {
				return dialectkit.Token[int]{Kind: lexEnd, Value: "", Raw: "", Start: end, End: end, Quoted: false}, nil
			}
			wordStart := position
			for position < end && input[position] != ' ' {
				position++
			}
			word := input[wordStart:position]
			if strings.Contains(word, "!") {
				return dialectkit.Token[int]{}, errUnreadable
			}
			return dialectkit.Token[int]{Kind: lexWord, Value: word, Raw: word, Start: wordStart, End: position, Quoted: false}, nil
		}
	}
}

func tokenWords(tokens []dialectkit.Token[int]) []string {
	words := make([]string, 0, len(tokens))
	for _, token := range tokens {
		words = append(words, token.Raw)
	}
	return words
}

func TestLexStatementsCutsAndTokenisesEachStatement(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		sql            string
		maxTokens      int
		wantWords      [][]string
		wantOverBudget []bool
	}{
		{name: "two statements", sql: "select a; select b c", maxTokens: 10,
			wantWords: [][]string{{"select", "a"}, {"select", "b", "c"}}, wantOverBudget: []bool{false, false}},
		{name: "an empty statement is left out", sql: "select a; ; select b", maxTokens: 10,
			wantWords: [][]string{{"select", "a"}, {"select", "b"}}, wantOverBudget: []bool{false, false}},
		{name: "a statement exactly at the budget", sql: "a b c", maxTokens: 3,
			wantWords: [][]string{{"a", "b", "c"}}, wantOverBudget: []bool{false}},
		{name: "a statement over the budget keeps a prefix", sql: "a b c d e; f", maxTokens: 3,
			wantWords: [][]string{{"a", "b", "c"}, {"f"}}, wantOverBudget: []bool{true, false}},
		{name: "a budget below one selects the default", sql: "a b", maxTokens: 0,
			wantWords: [][]string{{"a", "b"}}, wantOverBudget: []bool{false}},
		{name: "no statements", sql: "  ", maxTokens: 10, wantWords: [][]string{}, wantOverBudget: []bool{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reads := 0
			statements, err := dialectkit.LexStatements(t.Context(), sqllex.Rules{}, testCase.sql, wordTokeniser(&reads), lexEnd, testCase.maxTokens)

			require.NoError(t, err)
			gotWords := make([][]string, 0, len(statements))
			gotOverBudget := make([]bool, 0, len(statements))
			for _, statement := range statements {
				gotWords = append(gotWords, tokenWords(statement.Tokens))
				gotOverBudget = append(gotOverBudget, statement.OverBudget)
				assert.Equal(t, len(statement.Tokens), cap(statement.Tokens), "a statement holds no spare capacity")
				for _, token := range statement.Tokens {
					assert.Equal(t, token.Raw, testCase.sql[token.Start:token.End], "token positions are offsets into the script")
				}
			}
			assert.Equal(t, testCase.wantWords, gotWords)
			assert.Equal(t, testCase.wantOverBudget, gotOverBudget)
		})
	}
}

func TestLexStatementsStopsTokenisingAStatementAtTheBudget(t *testing.T) {
	t.Parallel()

	reads := 0
	sql := strings.Repeat("word ", 10_000)

	statements, err := dialectkit.LexStatements(t.Context(), sqllex.Rules{}, sql, wordTokeniser(&reads), lexEnd, 5)

	require.NoError(t, err)
	require.Len(t, statements, 1)
	assert.True(t, statements[0].OverBudget)
	assert.Len(t, statements[0].Tokens, 5)
	assert.Equal(t, 6, reads, "the tokeniser reads one token past the budget and no more")
	assert.Equal(t, 0, statements[0].Span.Start)
	assert.Equal(t, strings.TrimSpace(sql), sql[statements[0].Span.Start:statements[0].Span.End])
}

func TestLexStatementsReportsWhyItStopped(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancelCause(t.Context())
	cancel(errStopped)
	cases := []struct {
		ctx     context.Context
		wantErr error
		name    string
		sql     string
		message string
	}{
		{name: "a tokeniser error", ctx: t.Context(), sql: "select a; select b!", wantErr: errUnreadable,
			message: "tokenising SQL: unreadable character"},
		{name: "a cancelled context", ctx: cancelled, sql: "select a", wantErr: errStopped, message: "generation stopped"},
		{name: "an unterminated string", ctx: t.Context(), sql: "select 'open", wantErr: nil, message: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reads := 0
			statements, err := dialectkit.LexStatements(testCase.ctx, sqllex.Rules{}, testCase.sql, wordTokeniser(&reads), lexEnd, 10)

			require.Error(t, err)
			assert.Nil(t, statements)
			if testCase.wantErr != nil {
				assert.ErrorIs(t, err, testCase.wantErr)
				assert.EqualError(t, err, testCase.message)
				return
			}
			assert.True(t, strings.HasPrefix(err.Error(), "splitting SQL: "), err.Error())
		})
	}
}
