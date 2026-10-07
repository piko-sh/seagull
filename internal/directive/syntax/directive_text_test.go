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

package syntax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDirectiveIdentifierCharacters(t *testing.T) {
	t.Parallel()

	for _, character := range []byte("aZ_") {
		require.True(t, IsIdentifierStart(character), string(character))
		require.True(t, IsIdentifierPart(character), string(character))
	}
	require.False(t, IsIdentifierStart('7'))
	require.True(t, IsIdentifierPart('7'))
	require.True(t, IsDigit('0'))
	require.False(t, IsDigit('a'))
	for _, character := range []byte("$.-( ") {
		require.False(t, IsIdentifierPart(character), string(character))
	}
}

func TestSkipString(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		text       string
		wantEnd    int
		wantClosed bool
	}{
		{text: `'plain' rest`, wantEnd: 7, wantClosed: true},
		{text: `'it''s' rest`, wantEnd: 7, wantClosed: true},
		{text: `'it\'s' rest`, wantEnd: 7, wantClosed: true},
		{text: `"a\"b" rest`, wantEnd: 6, wantClosed: true},
		{text: `'open`, wantEnd: 5, wantClosed: false},
		{text: `'ends in a backslash\`, wantEnd: 21, wantClosed: false},
	}

	for _, testCase := range testCases {
		end, closed := SkipString(testCase.text, 0)
		require.Equal(t, testCase.wantEnd, end, testCase.text)
		require.Equal(t, testCase.wantClosed, closed, testCase.text)
	}
}

func TestLineDepth(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		text             string
		wantDepth        int
		wantUnterminated int
		wantQuote        byte
	}{
		{text: "sgl.query(A, [b, c])", wantDepth: 0, wantUnterminated: -1, wantQuote: 0},
		{text: "sgl.query(", wantDepth: 1, wantUnterminated: -1, wantQuote: 0},
		{text: "x: ')(', y: \"]\")", wantDepth: -1, wantUnterminated: -1, wantQuote: 0},
		{text: "sgl.query(name: 'open", wantDepth: 1, wantUnterminated: 16, wantQuote: '\''},
	}

	for _, testCase := range testCases {
		depth, unterminated, quote := LineDepth(testCase.text)
		require.Equal(t, testCase.wantDepth, depth, testCase.text)
		require.Equal(t, testCase.wantUnterminated, unterminated, testCase.text)
		require.Equal(t, testCase.wantQuote, quote, testCase.text)
	}
}
