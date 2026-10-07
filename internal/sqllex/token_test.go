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

package sqllex_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqllex"
)

func TestTokenIsWord(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		word     string
		token    sqllex.Token
		expected bool
	}{
		{name: "same case", token: sqllex.Token{Raw: "BEGIN", Kind: sqllex.KindWord}, word: "BEGIN", expected: true},
		{name: "other case", token: sqllex.Token{Raw: "begin", Kind: sqllex.KindWord}, word: "BEGIN", expected: true},
		{name: "different word", token: sqllex.Token{Raw: "begins", Kind: sqllex.KindWord}, word: "BEGIN", expected: false},
		{name: "quoted identifier", token: sqllex.Token{Raw: "begin", Kind: sqllex.KindQuotedIdentifier}, word: "BEGIN", expected: false},
		{name: "non-letter bytes must match", token: sqllex.Token{Raw: "a_1", Kind: sqllex.KindWord}, word: "A_1", expected: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.expected, testCase.token.IsWord(testCase.word))
		})
	}
}

func TestKindNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		value    interface{ String() string }
		expected string
	}{
		{value: sqllex.KindWhitespace, expected: "whitespace"},
		{value: sqllex.KindComment, expected: "comment"},
		{value: sqllex.KindWord, expected: "word"},
		{value: sqllex.KindQuotedIdentifier, expected: "quoted identifier"},
		{value: sqllex.KindString, expected: "string"},
		{value: sqllex.KindNumber, expected: "number"},
		{value: sqllex.KindParameter, expected: "parameter"},
		{value: sqllex.KindPunctuation, expected: "punctuation"},
		{value: sqllex.Kind(200), expected: "unknown"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.expected, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.expected, testCase.value.String())
		})
	}
}
