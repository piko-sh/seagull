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

package goast

import (
	"go/constant"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStringLiteralKeepsEveryByteOfTheText(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		text string
		raw  bool
	}{
		{name: "plain text", text: "SELECT id\nFROM things\tWHERE note = 'a\"b'", raw: true},
		{name: "empty text", text: "", raw: true},
		{name: "backtick", text: "SELECT `id` FROM things", raw: false},
		{name: "carriage return", text: "SELECT id\r\nFROM things", raw: false},
		{name: "NUL", text: "SELECT 'a\x00b'", raw: false},
		{name: "byte order mark", text: "\uFEFFSELECT 'a\uFEFFb'", raw: false},
		{name: "invalid UTF-8", text: "SELECT '\xff\xfe'", raw: false},
		{name: "valid non-ASCII", text: "SELECT 'café', '日本'", raw: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			literal := StringLiteral(testCase.text)

			require.Equal(t, token.STRING, literal.Kind)
			require.Equal(t, testCase.raw, RawStringAllowed(testCase.text))
			require.Equal(t, testCase.raw, literal.Value[0] == '`')
			value := constant.MakeFromLiteral(literal.Value, token.STRING, 0)
			require.Equal(t, constant.String, value.Kind())
			require.Equal(t, testCase.text, constant.StringVal(value))
		})
	}
}
