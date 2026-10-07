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
	"errors"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqllex"
)

func TestExcerptCutsOnRuneBoundaries(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		text     string
		expected string
		maxRunes int
	}{
		{name: "ascii shorter than the limit", text: "SELECT", maxRunes: 10, expected: "SELECT"},
		{name: "ascii longer than the limit", text: "SELECT 1", maxRunes: 6, expected: "SELECT..."},
		{name: "exactly the limit", text: "SELECT", maxRunes: 6, expected: "SELECT"},
		{name: "one rune past the limit", text: "SELECT;", maxRunes: 6, expected: "SELECT..."},
		{name: "multi-byte runes counted once", text: "日本語のテキスト", maxRunes: 3, expected: "日本語..."},
		{name: "multi-byte runes at exactly the limit", text: "ü日本", maxRunes: 3, expected: "ü日本"},
		{name: "four-byte rune kept whole", text: "a😀b", maxRunes: 2, expected: "a😀..."},
		{name: "invalid byte replaced", text: "a\xffb", maxRunes: 10, expected: "a\uFFFDb"},
		{name: "run of invalid bytes replaced once", text: "a\xff\xfeb", maxRunes: 10, expected: "a\uFFFDb"},
		{name: "invalid byte counts as one rune", text: "\xffabc", maxRunes: 2, expected: "\uFFFDa..."},
		{name: "truncated multi-byte rune replaced", text: "ab\xe6\x97", maxRunes: 10, expected: "ab\uFFFD"},
		{name: "empty text", text: "", maxRunes: 4, expected: ""},
		{name: "zero limit", text: "SELECT", maxRunes: 0, expected: "..."},
		{name: "negative limit", text: "SELECT", maxRunes: -3, expected: "..."},
		{name: "zero limit on empty text", text: "", maxRunes: 0, expected: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			excerpt := sqllex.Excerpt(testCase.text, testCase.maxRunes)
			require.Equal(t, testCase.expected, excerpt)
			require.True(t, utf8.ValidString(excerpt))
		})
	}
}

func TestCallbackErrorWrapsSentinelAndErrorValues(t *testing.T) {
	t.Parallel()

	cause := errors.New("predicate failed")

	testCases := []struct {
		value    any
		cause    error
		name     string
		callback string
		message  string
	}{
		{
			name:     "string value",
			callback: "ProceduralBlock",
			value:    "boom",
			message:  "lexical rules callback ProceduralBlock panicked: boom",
		},
		{
			name:     "error value",
			callback: "RequiresNoTransaction",
			value:    cause,
			cause:    cause,
			message:  "lexical rules callback RequiresNoTransaction panicked: predicate failed",
		},
		{
			name:     "integer value",
			callback: "ProceduralBlock",
			value:    42,
			message:  "lexical rules callback ProceduralBlock panicked: 42",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			err := &sqllex.CallbackError{Value: testCase.value, Callback: testCase.callback}
			require.Equal(t, testCase.message, err.Error())
			require.ErrorIs(t, err, sqllex.ErrRulesCallback)
			require.NotErrorIs(t, err, sqllex.ErrMalformed)
			if testCase.cause != nil {
				require.ErrorIs(t, err, testCase.cause)
				require.Equal(t, []error{sqllex.ErrRulesCallback, testCase.cause}, err.Unwrap())
				return
			}
			require.Equal(t, []error{sqllex.ErrRulesCallback}, err.Unwrap())
		})
	}
}
