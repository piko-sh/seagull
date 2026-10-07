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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
)

type testKind uint8

const (
	kindWord testKind = iota + 1
	kindNumber
	kindSymbol
	kindEnd
)

func word(value string, start int) dialectkit.Token[testKind] {
	return dialectkit.Token[testKind]{Value: value, Raw: value, Start: start, End: start + len(value), Kind: kindWord}
}

func quotedWord(value string, start int) dialectkit.Token[testKind] {
	raw := `"` + value + `"`
	return dialectkit.Token[testKind]{Value: value, Raw: raw, Start: start, End: start + len(raw), Kind: kindWord, Quoted: true}
}

func symbol(value string, start int) dialectkit.Token[testKind] {
	return dialectkit.Token[testKind]{Value: value, Raw: value, Start: start, End: start + len(value), Kind: kindSymbol}
}

func endOfInput(start int) dialectkit.Token[testKind] {
	return dialectkit.Token[testKind]{Start: start, End: start, Kind: kindEnd}
}

func newTestCursor(tokens ...dialectkit.Token[testKind]) dialectkit.Cursor[testKind] {
	return dialectkit.NewCursor(tokens, kindWord, kindEnd)
}

func TestCursorWalksAndPeeks(t *testing.T) {
	t.Parallel()

	cursor := newTestCursor(word("SELECT", 0), word("id", 7), endOfInput(9))

	require.Equal(t, "SELECT", cursor.Current().Value)
	require.Equal(t, "id", cursor.Peek().Value)
	require.Equal(t, kindEnd, cursor.PeekAt(2).Kind)
	require.Equal(t, kindEnd, cursor.PeekAt(10).Kind)
	require.Equal(t, kindEnd, cursor.PeekAt(-1).Kind)
	require.False(t, cursor.AtEnd())

	require.Equal(t, "SELECT", cursor.Advance().Value)
	require.Equal(t, "SELECT", cursor.PeekAt(-1).Value)
	require.Equal(t, "id", cursor.Advance().Value)
	require.True(t, cursor.AtEnd())
	require.Equal(t, kindEnd, cursor.Advance().Kind)
	require.Equal(t, kindEnd, cursor.Advance().Kind)
	require.True(t, cursor.AtEnd())
}

func TestCursorEndTokenSitsAfterTheLastToken(t *testing.T) {
	t.Parallel()

	cursor := newTestCursor(word("id", 4))
	cursor.Advance()

	require.True(t, cursor.AtEnd())
	require.Equal(t, 6, cursor.Current().Start)
	require.Equal(t, kindEnd, cursor.Current().Kind)

	empty := newTestCursor()
	require.True(t, empty.AtEnd())
	require.Equal(t, 0, empty.Current().Start)
}

func TestCursorKeywordMatching(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		token     dialectkit.Token[testKind]
		keywords  []string
		wantMatch bool
	}{
		{name: "matches one of several", token: word("from", 0), keywords: []string{"WHERE", "FROM"}, wantMatch: true},
		{name: "quoted reserved word is a name", token: quotedWord("from", 0), keywords: []string{"FROM"}, wantMatch: false},
		{name: "symbol is not a keyword", token: symbol("=", 0), keywords: []string{"="}, wantMatch: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cursor := newTestCursor(testCase.token, endOfInput(10))
			require.Equal(t, testCase.wantMatch, cursor.IsKeyword(testCase.keywords...))

			matched := cursor.MatchKeyword(testCase.keywords[len(testCase.keywords)-1])
			require.Equal(t, testCase.wantMatch, matched)
			if matched {
				require.Equal(t, 1, cursor.Position)
			} else {
				require.Equal(t, 0, cursor.Position)
			}
		})
	}
}

func TestCursorExpectKeyword(t *testing.T) {
	t.Parallel()

	cursor := newTestCursor(word("select", 0), quotedWord("from", 7), endOfInput(13))

	consumed, err := cursor.ExpectKeyword("SELECT")
	require.NoError(t, err)
	require.Equal(t, "select", consumed.Value)

	_, err = cursor.ExpectKeyword("FROM", "WHERE")
	require.Error(t, err)
	require.ErrorIs(t, err, fault.ErrParse)
	require.Equal(t, `syntax error near "\"from\"": expected one of FROM, WHERE`, err.Error())
	require.Equal(t, 1, cursor.Position)

	parseError, ok := errors.AsType[*dialectkit.ParseError](err)
	require.True(t, ok)
	require.Equal(t, 7, parseError.Position)
	require.Equal(t, `"from"`, parseError.Token)
}

func TestCursorRequireEnd(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		tokens    []dialectkit.Token[testKind]
		position  int
		wantError string
	}{
		{name: "at the end token", tokens: []dialectkit.Token[testKind]{word("id", 0), endOfInput(2)}, position: 1},
		{name: "past the stream", tokens: []dialectkit.Token[testKind]{word("id", 0)}, position: 1},
		{
			name:      "trailing tokens",
			tokens:    []dialectkit.Token[testKind]{word("id", 0), symbol(")", 3), endOfInput(4)},
			position:  1,
			wantError: `syntax error near ")": expected the end of the statement`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cursor := newTestCursor(testCase.tokens...)
			cursor.Position = testCase.position
			err := cursor.RequireEnd()
			if testCase.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, fault.ErrParse)
			require.EqualError(t, err, testCase.wantError)
		})
	}
}

func TestCursorCallsItsInterruptOnEveryRead(t *testing.T) {
	t.Parallel()

	cases := []struct {
		read      func(cursor *dialectkit.Cursor[int])
		name      string
		wantCalls int
	}{
		{name: "current", read: func(cursor *dialectkit.Cursor[int]) { cursor.Current() }, wantCalls: 1},
		{name: "peek", read: func(cursor *dialectkit.Cursor[int]) { cursor.Peek() }, wantCalls: 1},
		{name: "peek past the end", read: func(cursor *dialectkit.Cursor[int]) { cursor.PeekAt(10) }, wantCalls: 1},
		{name: "advance", read: func(cursor *dialectkit.Cursor[int]) { cursor.Advance() }, wantCalls: 1},
		{name: "keyword test", read: func(cursor *dialectkit.Cursor[int]) { cursor.IsKeyword("select", "with") }, wantCalls: 1},
		{name: "end test", read: func(cursor *dialectkit.Cursor[int]) { cursor.AtEnd() }, wantCalls: 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			cursor := dialectkit.NewCursor([]dialectkit.Token[int]{{Kind: 1, Value: "select", Raw: "select"}}, 1, 0)
			cursor.Interrupt = func() error {
				calls++
				return nil
			}

			testCase.read(&cursor)

			assert.Equal(t, testCase.wantCalls, calls)
		})
	}
}

func TestCursorStopsAWalkWithItsInterruptsError(t *testing.T) {
	t.Parallel()

	cursor := dialectkit.NewCursor([]dialectkit.Token[int]{{Kind: 1, Value: "select", Raw: "select"}}, 1, 0)
	cursor.Interrupt = func() error { return dialectkit.ErrWalkBudget }

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		cursor.Current()
	}()

	require.True(t, dialectkit.IsInterrupt(recovered))
	require.ErrorIs(t, dialectkit.RecoveredError(t.Context(), "walk", recovered), dialectkit.ErrWalkBudget)
}
