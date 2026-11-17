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

package fuzztext

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitLinesSplitsAtEveryBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "empty", text: "", want: nil},
		{name: "a trailing newline adds no line", text: "a\nb\n", want: []string{"a", "b"}},
		{name: "CRLF is one boundary", text: "a\r\nb", want: []string{"a", "b"}},
		{name: "a lone CR", text: "a\rb", want: []string{"a", "b"}},
		{name: "blank lines are kept", text: "a\n\nb", want: []string{"a", "", "b"}},
		{name: "vertical tab, form feed and separators", text: "a\vb\fc\x1cd e", want: []string{"a", "b", "c", "d", "e"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, SplitLines(testCase.text))
		})
	}
}

func TestWhitespaceIncludesTheInformationSeparators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		wantStrip string
		wantLeft  string
		wantSplit []string
	}{
		{name: "ASCII", text: " \ta b\n", wantStrip: "a b", wantLeft: "a b\n", wantSplit: []string{"a", "b"}},
		{name: "separators and NBSP", text: "\x1f a\x1cb　", wantStrip: "a\x1cb", wantLeft: "a\x1cb　", wantSplit: []string{"a", "b"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.wantStrip, Strip(testCase.text))
			require.Equal(t, testCase.wantLeft, TrimLeft(testCase.text))
			require.Equal(t, testCase.wantSplit, Fields(testCase.text))
		})
	}
}

func TestMarshalASCIIEscapesEverythingOutsidePrintableASCII(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  any
		want   string
		indent bool
	}{
		{name: "HTML characters stay literal", value: map[string]string{"a": "<b>&"}, want: `{"a":"<b>&"}`},
		{name: "a two-byte rune", value: "café", want: `"caf\u00e9"`},
		{name: "a rune past the BMP becomes a surrogate pair", value: "\U0001F600", want: `"\ud83d\ude00"`},
		{name: "DEL is escaped", value: "\x7f", want: `"\u007f"`},
		{name: "keys are sorted", value: map[string]int{"b": 1, "a": 2}, want: `{"a":2,"b":1}`},
		{name: "indented", value: map[string][]int{"a": {1}}, indent: true, want: "{\n  \"a\": [\n    1\n  ]\n}"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := MarshalASCII(testCase.value, testCase.indent)
			require.NoError(t, err)
			require.Equal(t, testCase.want, string(encoded))
		})
	}
}
