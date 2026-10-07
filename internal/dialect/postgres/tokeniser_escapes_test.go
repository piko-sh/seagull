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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscapeStringsDecodeTheirBackslashEscapes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "control characters", input: `E'\b\f\n\r\t'`, want: "\b\f\n\r\t"},
		{name: "an escaped quote and backslash", input: `E'it\'s \\ done'`, want: `it's \ done`},
		{name: "a doubled quote", input: `E'it''s'`, want: "it's"},
		{name: "octal escapes of one to three digits", input: `E'\101\7\0618'`, want: "A\a18"},
		{name: "an octal escape past a byte keeps its low byte", input: `E'\501'`, want: "A"},
		{name: "hexadecimal escapes of one and two digits", input: `E'\x41\x9z'`, want: "A\tz"},
		{name: "a hexadecimal escape with no digit is the letter", input: `E'\xg'`, want: "xg"},
		{name: "a four-digit Unicode escape", input: `E'caf\u00e9'`, want: "café"},
		{name: "an eight-digit Unicode escape", input: `E'\U0001F600'`, want: "\U0001F600"},
		{name: "a surrogate pair", input: `E'\uD83D\uDE00'`, want: "\U0001F600"},
		{name: "any other escaped character stands for itself", input: `E'\q\%'`, want: "q%"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.input)

			require.NoError(t, err)
			require.Len(t, tokens, 2)
			assert.Equal(t, tokenEscapeString, tokens[0].Kind)
			assert.Equal(t, testCase.want, tokens[0].Value)
			assert.Equal(t, testCase.input, tokens[0].Raw)
		})
	}
}

func TestMalformedUnicodeEscapesInEscapeStringsAreLexicalErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		input string
	}{
		{name: "a short four-digit escape", input: `E'\u00e'`},
		{name: "a short eight-digit escape", input: `E'\U0001F6'`},
		{name: "a lone high surrogate", input: `E'\uD83D'`},
		{name: "a high surrogate before a character", input: `E'\uD83Dx'`},
		{name: "a lone low surrogate", input: `E'\uDE00'`},
		{name: "a code point past the last", input: `E'\U00110000'`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			requireTokeniseError(t, testCase.input, "invalid Unicode escape")
		})
	}
}
