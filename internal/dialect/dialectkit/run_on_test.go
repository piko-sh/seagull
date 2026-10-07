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

package dialectkit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/scaling"
)

type runOnKind uint8

const (
	runOnWord runOnKind = iota
	runOnOpen
	runOnClose
	runOnOther
	runOnEnd
)

var runOnTestRules = RunOnRules[runOnKind]{
	Starters: map[string][]string{"CREATE": nil, "DROP": {"TABLE"}},
	Word:     runOnWord,
	Open:     runOnOpen,
	Close:    runOnClose,
}

var runOnCases = []struct {
	name      string
	source    string
	wantFound bool
	wantStart int
}{
	{name: "a CREATE on a line of its own", source: "CREATE TABLE a (id int)\nCREATE TABLE b (id int)", wantFound: true, wantStart: 24},
	{name: "a starter needs its follower", source: "ALTER TABLE a\nDROP COLUMN b", wantFound: false},
	{name: "a starter with its follower", source: "ALTER TABLE a ADD b int\nDROP TABLE c", wantFound: true, wantStart: 24},
	{name: "a starter inside parentheses does not count", source: "CREATE SCHEMA s (\nCREATE TABLE a (id int))", wantFound: false},
	{name: "a starter mid-line does not count", source: "GRANT CREATE ON SCHEMA s TO u", wantFound: false},
	{name: "the statement's own first word does not count", source: "CREATE TABLE a (id int)", wantFound: false},
	{name: "a starter that ends the statement does not count", source: "SELECT 1\nCREATE", wantFound: false},
	{name: "a first parenthesis is not counted", source: "(SELECT 1)\nCREATE TABLE a (id int)", wantFound: false},
	{name: "a starter followed by no word", source: "SELECT 1\nDROP (TABLE)", wantFound: false},
	{name: "a starter right after a closing parenthesis", source: "INSERT INTO t VALUES (1)\nCREATE TABLE b (id int)", wantFound: true, wantStart: 25},
}

func runOnTokens(source string) []Token[runOnKind] {
	var tokens []Token[runOnKind]
	for index := 0; index < len(source); {
		character := rune(source[index])
		switch {
		case unicode.IsSpace(character):
			index++
		case character == '(' || character == ')':
			kind := runOnOpen
			if character == ')' {
				kind = runOnClose
			}
			tokens = append(tokens, Token[runOnKind]{Kind: kind, Value: string(character), Raw: string(character), Start: index, End: index + 1, Quoted: false})
			index++
		case unicode.IsLetter(character) || character == '_':
			end := index
			for end < len(source) && (unicode.IsLetter(rune(source[end])) || source[end] == '_') {
				end++
			}
			tokens = append(tokens, Token[runOnKind]{Kind: runOnWord, Value: source[index:end], Raw: source[index:end], Start: index, End: end, Quoted: false})
			index = end
		default:
			tokens = append(tokens, Token[runOnKind]{Kind: runOnOther, Value: string(character), Raw: string(character), Start: index, End: index + 1, Quoted: false})
			index++
		}
	}
	return tokens
}

func TestRunOnStatementFindsTheStatementAMissingSemicolonRunsOnto(t *testing.T) {
	t.Parallel()

	for _, testCase := range runOnCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			token, found := RunOnStatement(testCase.source, runOnTokens(testCase.source), runOnTestRules)
			require.Equal(t, testCase.wantFound, found)
			if testCase.wantFound {
				require.Equal(t, testCase.wantStart, token.Start)
				require.True(t, strings.HasPrefix(testCase.source[token.Start:], token.Value))
			}
		})
	}
}

func TestRunOnStreamFindsWhatRunOnStatementFinds(t *testing.T) {
	t.Parallel()

	for _, testCase := range runOnCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens := runOnTokens(testCase.source)
			wantToken, wantFound := RunOnStatement(testCase.source, tokens, runOnTestRules)

			token, found, err := RunOnStream(t.Context(), testCase.source, runOnReader(tokens), runOnEnd, runOnTestRules)

			require.NoError(t, err)
			require.Equal(t, wantFound, found)
			require.Equal(t, wantToken, token)
		})
	}
}

func TestRunOnStreamFindsARunOnPastTheTokensAStatementKept(t *testing.T) {
	t.Parallel()

	const keptTokens = 8
	source := "INSERT INTO t VALUES " + strings.Repeat("(1), ", 50) + "(1)\nCREATE TABLE u (id int)"
	tokens := runOnTokens(source)
	require.Greater(t, len(tokens), keptTokens)

	_, foundInKept := RunOnStatement(source, tokens[:keptTokens], runOnTestRules)
	token, found, err := RunOnStream(t.Context(), source, runOnReader(tokens), runOnEnd, runOnTestRules)

	require.False(t, foundInKept)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "CREATE", token.Value)
	require.Equal(t, strings.Index(source, "CREATE"), token.Start)
}

func TestRunOnStreamStopsOnAReaderThatCannotFinish(t *testing.T) {
	t.Parallel()

	readerFailure := errors.New("unterminated string")
	cancelled, cancel := context.WithCancelCause(t.Context())
	cancellation := errors.New("stopped by the caller")
	cancel(cancellation)
	testCases := []struct {
		ctx     context.Context
		next    func() (Token[runOnKind], error)
		wantErr error
		name    string
	}{
		{
			name:    "the reader's error is wrapped",
			ctx:     t.Context(),
			next:    func() (Token[runOnKind], error) { return Token[runOnKind]{}, readerFailure },
			wantErr: readerFailure,
		},
		{
			name: "a reader that never reaches the end",
			ctx:  t.Context(),
			next: func() (Token[runOnKind], error) {
				return Token[runOnKind]{Kind: runOnOther, Value: ",", Raw: ",", Start: 0, End: 1, Quoted: false}, nil
			},
			wantErr: ErrWalkBudget,
		},
		{
			name:    "a cancelled context",
			ctx:     cancelled,
			next:    runOnReader(runOnTokens("SELECT 1")),
			wantErr: cancellation,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, found, err := RunOnStream(testCase.ctx, "SELECT 1", testCase.next, runOnEnd, runOnTestRules)

			require.ErrorIs(t, err, testCase.wantErr)
			require.False(t, found)
		})
	}
}

func TestRunOnStreamGrowsLinearlyWithTheStatement(t *testing.T) {
	smaller := strings.Repeat("(1), ", 50_000)
	larger := strings.Repeat("(1), ", 200_000)
	smallerTokens := runOnTokens(smaller)
	largerTokens := runOnTokens(larger)
	read := func(source string, tokens []Token[runOnKind]) func() {
		return func() {
			_, found, err := RunOnStream(t.Context(), source, runOnReader(tokens), runOnEnd, runOnTestRules)
			require.NoError(t, err)
			require.False(t, found)
		}
	}

	scaling.Linear(t, read(smaller, smallerTokens), read(larger, largerTokens))
}

func runOnReader(tokens []Token[runOnKind]) func() (Token[runOnKind], error) {
	index := 0
	return func() (Token[runOnKind], error) {
		if index >= len(tokens) {
			return Token[runOnKind]{Kind: runOnEnd, Value: "", Raw: "", Start: 0, End: 0, Quoted: false}, nil
		}
		index++
		return tokens[index-1], nil
	}
}
