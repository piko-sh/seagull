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

package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArgumentScanIndexAgreesWithAWalkFromTheEnclosingParenthesis(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "nested calls", sql: "f(a, g(b, c), (d, e), h())"},
		{name: "commas outside any group", sql: "a, b, f(c, d), e"},
		{name: "a group closed before it opens", sql: ") a, ( b, c ) , d ( e"},
		{name: "groups left open", sql: "f(a, (b, c, g(d"},
		{name: "deep nesting", sql: "((((a, b), c), d), e)"},
		{name: "a closing parenthesis too many", sql: "f(a)), (b, c)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.sql)
			require.NoError(t, err)
			tokens = tokens[:len(tokens)-1]
			p := newParser(tokens)

			for position := range tokens {
				if open := p.findEnclosingParen(position); open >= 0 {
					assert.Equal(t, walkedArgumentOrdinal(tokens, open, position), p.argumentOrdinalAt(position),
						"ordinal of token %d", position)
				}
				if tokens[position].Kind == tokenRightParen {
					assert.Equal(t, walkedMatchingLeftParen(tokens, position), p.findMatchingLeftParen(position),
						"match of token %d", position)
				}
			}
		})
	}
}

func TestArgumentScanIndexAnswersOutOfRangePositions(t *testing.T) {
	t.Parallel()

	tokens, err := tokenise("f(a, b)")
	require.NoError(t, err)

	testCases := []struct {
		name     string
		position int
	}{
		{name: "before the first token", position: -1},
		{name: "past the last token", position: 99},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			p := newParser(tokens[:len(tokens)-1])

			assert.Equal(t, 0, p.argumentOrdinalAt(testCase.position))
			assert.Equal(t, -1, p.findMatchingLeftParen(testCase.position))
		})
	}
}

func walkedArgumentOrdinal(tokens []token, openParen, position int) int {
	ordinal, depth := 0, 0
	for index := openParen + 1; index < position; index++ {
		switch tokens[index].Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		case tokenComma:
			if depth == 0 {
				ordinal++
			}
		default:
		}
	}
	return ordinal
}

func walkedMatchingLeftParen(tokens []token, rightParen int) int {
	depth := 0
	for index := rightParen; index >= 0; index-- {
		switch tokens[index].Kind {
		case tokenRightParen:
			depth++
		case tokenLeftParen:
			depth--
			if depth == 0 {
				return index
			}
		default:
		}
	}
	return -1
}
