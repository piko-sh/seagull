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

package corpus

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeAndDecodeRoundTripEveryByte(t *testing.T) {
	t.Parallel()

	every := make([]byte, 256)
	for index := range every {
		every[index] = byte(index)
	}
	tests := []struct {
		name      string
		arguments []Argument
	}{
		{name: "every byte value", arguments: []Argument{{Kind: KindBytes, Value: every}}},
		{name: "invalid UTF-8", arguments: []Argument{{Kind: KindString, Value: []byte{0xff, 0xfe, 'a'}}}},
		{name: "empty string", arguments: []Argument{{Kind: KindString, Value: []byte{}}}},
		{
			name: "three mixed arguments",
			arguments: []Argument{
				{Kind: KindString, Value: []byte("postgres")},
				{Kind: KindBytes, Value: []byte{11, 2, 0, 1}},
				{Kind: KindString, Value: []byte("SELECT \"a\" FROM t\n")},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			encoded := Encode(testCase.arguments)
			decoded, err := Decode(encoded)
			require.NoError(t, err)
			require.Len(t, decoded, len(testCase.arguments))
			for index, argument := range testCase.arguments {
				require.Equal(t, argument.Kind, decoded[index].Kind)
				require.Equal(t, string(argument.Value), string(decoded[index].Value))
			}
			require.Equal(t, encoded, Encode(decoded))
		})
	}
}

func TestEncodeWritesTheGoCorpusFormat(t *testing.T) {
	t.Parallel()

	encoded := Encode([]Argument{{Kind: KindString, Value: []byte("Z\"\\\x00\x7f")}})

	require.Equal(t, "go test fuzz v1\nstring(\"Z\\x22\\x5c\\x00\\x7f\")\n", string(encoded))
}

func TestDecodeReadsWhatGoWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    []Argument
	}{
		{
			name:    "a plain string",
			content: "go test fuzz v1\nstring(\"abc\")\n",
			want:    []Argument{{Kind: KindString, Value: []byte("abc")}},
		},
		{
			name:    "single escapes",
			content: "go test fuzz v1\nstring(\"\\a\\b\\f\\n\\r\\t\\v\\\\\\\"\")\n",
			want:    []Argument{{Kind: KindString, Value: []byte("\a\b\f\n\r\t\v\\\"")}},
		},
		{
			name:    "hexadecimal, Unicode and octal escapes",
			content: "go test fuzz v1\n[]byte(\"\\x41\\u00e9\\U0001F600\\101\\377\")\n",
			want:    []Argument{{Kind: KindBytes, Value: []byte("A\u00e9\U0001F600A\xff")}},
		},
		{
			name:    "literal non-ASCII text",
			content: "go test fuzz v1\nstring(\"caf\u00e9\")\n",
			want:    []Argument{{Kind: KindString, Value: []byte("caf\u00e9")}},
		},
		{
			name:    "blank lines and CRLF endings",
			content: "go test fuzz v1\r\n\r\nstring(\"a\")\r\n  \r\n",
			want:    []Argument{{Kind: KindString, Value: []byte("a")}},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			decoded, err := Decode([]byte(testCase.content))
			require.NoError(t, err)
			require.Equal(t, testCase.want, decoded)
		})
	}
}

func TestDecodeRefusesUnsupportedFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "another version", content: "go test fuzz v2\nstring(\"a\")\n"},
		{name: "no arguments", content: "go test fuzz v1\n"},
		{name: "four arguments", content: "go test fuzz v1\n" + strings.Repeat("string(\"a\")\n", 4)},
		{name: "an integer argument", content: "go test fuzz v1\nint(1)\n"},
		{name: "a raw string", content: "go test fuzz v1\nstring(`a`)\n"},
		{name: "an unescaped quote", content: "go test fuzz v1\nstring(\"a\"b\")\n"},
		{name: "a control character", content: "go test fuzz v1\nstring(\"a\x01\")\n"},
		{name: "an incomplete escape", content: "go test fuzz v1\nstring(\"a\\\")\n"},
		{name: "a short hexadecimal escape", content: "go test fuzz v1\nstring(\"\\x4\")\n"},
		{name: "a signed hexadecimal escape", content: "go test fuzz v1\nstring(\"\\x+4\")\n"},
		{name: "a surrogate escape", content: "go test fuzz v1\nstring(\"\\ud800\")\n"},
		{name: "a code point past Unicode", content: "go test fuzz v1\nstring(\"\\U00110000\")\n"},
		{name: "an octal escape past a byte", content: "go test fuzz v1\nstring(\"\\400\")\n"},
		{name: "a non-octal digit", content: "go test fuzz v1\nstring(\"\\018\")\n"},
		{name: "an unknown escape", content: "go test fuzz v1\nstring(\"\\q\")\n"},
		{name: "invalid UTF-8", content: "go test fuzz v1\nstring(\"\xff\")\n"},
		{name: "arguments past the input bound", content: "go test fuzz v1\nstring(\"" + strings.Repeat("a", MaxInput+1) + "\")\n"},
		{name: "a file past the envelope bound", content: "go test fuzz v1\n" + strings.Repeat(" ", MaxEnvelope)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := Decode([]byte(testCase.content))
			require.ErrorIs(t, err, errUnsupported)
		})
	}
}

func TestCutsHalvesItsWidthEachRound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		want    [][2]int
		length  int
		minimum int
	}{
		{name: "nothing to cut", length: 0, minimum: 0, want: nil},
		{name: "four items", length: 4, minimum: 0, want: [][2]int{{0, 4}, {0, 2}, {2, 4}, {0, 1}, {1, 2}, {2, 3}, {3, 4}}},
		{name: "three items keeping one", length: 3, minimum: 1, want: [][2]int{{0, 1}, {1, 2}, {2, 3}}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var got [][2]int
			for start, end := range cuts(testCase.length, testCase.minimum) {
				got = append(got, [2]int{start, end})
			}
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestScalarsYieldsSmallerValuesKeepingMaskedBits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		want  []int
		value int
		mask  int
	}{
		{name: "zero has nothing smaller", value: 0, mask: 0, want: nil},
		{name: "a large byte", value: 200, mask: 0, want: []int{0, 1, 2, 72, 100, 127, 128}},
		{name: "the flag bit stays set", value: 200, mask: 128, want: []int{128, 129, 130, 128}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, slices.Collect(scalars(testCase.value, testCase.mask)))
		})
	}
}

func TestCandidatesProposeReductions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		function  string
		arguments []Argument
		wantFirst []string
		wantNone  bool
	}{
		{
			name:      "raw text drops whole lines first",
			function:  "FuzzSplit",
			arguments: []Argument{{Kind: KindString, Value: []byte("a;\nb;\n")}},
			wantFirst: []string{"", "b;\n", "a;\n", ""},
		},
		{
			name:      "an engine name is never reduced",
			function:  "FuzzEmit",
			arguments: []Argument{{Kind: KindString, Value: []byte("pgx")}},
			wantNone:  true,
		},
		{
			name:      "a command model drops whole operations",
			function:  "FuzzDMLModel",
			arguments: []Argument{{Kind: KindBytes, Value: []byte{11, 1, 1, 2, 3, 4, 5, 6}}},
			wantFirst: []string{"\x0b\x00\x04\x05\x06", "\x0b\x00\x01\x02\x03"},
		},
		{
			name:      "an unknown model keeps its version byte",
			function:  "FuzzStructuredQuery",
			arguments: []Argument{{Kind: KindBytes, Value: []byte{200, 7, 8}}},
			wantFirst: []string{"\xc8", "\xc8\x08", "\xc8\x07"},
		},
		{
			name:      "a bundle is normalised first",
			function:  "FuzzGenerateFiles",
			arguments: []Argument{{Kind: KindBytes, Value: []byte(`{"version": 1, "files": [{"sql": "SELECT 1;", "name": "queries/a.sql"}]}`)}},
			wantFirst: []string{`{"files":[{"name":"queries/a.sql","sql":"SELECT 1;"}],"version":1}`},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var got []string
			for candidate := range Candidates(testCase.arguments, testCase.function) {
				require.Len(t, candidate, len(testCase.arguments))
				got = append(got, string(candidate[0].Value))
				if len(got) == len(testCase.wantFirst) {
					break
				}
			}
			if testCase.wantNone {
				require.Empty(t, got)
				return
			}
			require.Equal(t, testCase.wantFirst, got)
		})
	}
}

func TestModelCandidatesKeepProtectedBytes(t *testing.T) {
	t.Parallel()

	model := []byte{8, 200, 200, 200, 200, 9, 9}
	for candidate := range modelCandidates(model) {
		require.GreaterOrEqual(t, len(candidate), 5, "the header is never cut")
		require.Equal(t, model[:3], candidate[:3], "the version and mode bytes stay fixed")
		require.Equal(t, byte(128), candidate[3]&128, "the flag bit stays set")
	}
}

func TestBundleCandidatesKeepEveryDirectory(t *testing.T) {
	t.Parallel()

	data := []byte(`{"version":1,"customFunctions":[{"name":"f"}],"files":[` +
		`{"name":"migrations/1.sql","sql":"CREATE TABLE t (a int);"},` +
		`{"name":"queries/a.sql","sql":"SELECT 1;"},{"name":"queries/b.sql","sql":"SELECT 2;"}]}`)
	count := 0
	for candidate := range bundleCandidates(data) {
		decoded, err := decodeBundle(candidate)
		require.NoError(t, err)
		require.Equal(t, map[string]bool{"migrations": true, "queries": true}, fileDirectories(decoded.files))
		count++
	}
	require.Positive(t, count)
}

func TestValidateRefusesInputsTheReducerCannotHandle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		function  string
		arguments []Argument
		wantError bool
	}{
		{name: "a valid bundle", function: "FuzzGenerateFiles", arguments: []Argument{{Kind: KindBytes, Value: []byte(`{"version":1,"files":[]}`)}}},
		{name: "a bundle that is not JSON", function: "FuzzGenerateFiles", arguments: []Argument{{Kind: KindBytes, Value: []byte("{")}}, wantError: true},
		{name: "a bundle of another version", function: "FuzzGenerateFiles", arguments: []Argument{{Kind: KindBytes, Value: []byte(`{"version":1.0,"files":[]}`)}}, wantError: true},
		{name: "trailing data after a bundle", function: "FuzzGenerateFiles", arguments: []Argument{{Kind: KindBytes, Value: []byte(`{"version":1,"files":[]} x`)}}, wantError: true},
		{
			name:      "a bundle path leaving its directory",
			function:  "FuzzGenerateFiles",
			arguments: []Argument{{Kind: KindBytes, Value: []byte(`{"version":1,"files":[{"name":"queries/../a.sql","sql":""}]}`)}},
			wantError: true,
		},
		{
			name:      "a repeated bundle path",
			function:  "FuzzGenerateFiles",
			arguments: []Argument{{Kind: KindBytes, Value: []byte(`{"version":1,"files":[{"name":"queries/a.sql","sql":""},{"name":"queries/a.sql","sql":""}]}`)}},
			wantError: true,
		},
		{name: "an empty model", function: "FuzzStructuredQuery", arguments: []Argument{{Kind: KindBytes, Value: []byte{}}}, wantError: true},
		{name: "an empty string", function: "FuzzSplit", arguments: []Argument{{Kind: KindString, Value: []byte{}}}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := Validate(testCase.arguments, testCase.function)
			if testCase.wantError {
				require.ErrorIs(t, err, errUnsupported)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestComplexityOrdersByLengthThenBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "shorter is simpler", left: "zz", right: "aaa", want: -1},
		{name: "equal length compares bytes", left: "ab", right: "aa", want: 1},
		{name: "identical", left: "ab", right: "ab", want: 0},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			left := []Argument{{Kind: KindString, Value: []byte(testCase.left)}}
			right := []Argument{{Kind: KindString, Value: []byte(testCase.right)}}
			order := Complexity(left, right)
			switch {
			case testCase.want < 0:
				require.Negative(t, order)
			case testCase.want > 0:
				require.Positive(t, order)
			default:
				require.Zero(t, order)
			}
		})
	}
}
