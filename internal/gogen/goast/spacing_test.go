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
	"go/format"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestSpaceDeclarationsPartsEveryTwoTopLevelDeclarations(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "functions of one kind",
			source: "package p\n\nfunc a() {\n}\nfunc b() {}\n",
			want:   "package p\n\nfunc a() {\n}\n\nfunc b() {}\n",
		},
		{
			name:   "one-line declarations",
			source: "package p\n\nconst a = 1\nconst b = 2\nvar c = 3\n",
			want:   "package p\n\nconst a = 1\n\nconst b = 2\n\nvar c = 3\n",
		},
		{
			name:   "a doc comment goes with its declaration",
			source: "package p\n\ntype A struct{}\n// B is documented.\ntype B struct{}\n",
			want:   "package p\n\ntype A struct{}\n\n// B is documented.\ntype B struct{}\n",
		},
		{
			name:   "a trailing comment stays on its line",
			source: "package p\n\nvar a = 1 // first\nvar b = 2\n",
			want:   "package p\n\nvar a = 1 // first\n\nvar b = 2\n",
		},
		{
			name:   "declarations already parted are kept",
			source: "package p\n\nimport \"fmt\"\n\nfunc a() { fmt.Println() }\n\nfunc b() {}\n",
			want:   "package p\n\nimport \"fmt\"\n\nfunc a() { fmt.Println() }\n\nfunc b() {}\n",
		},
		{
			name:   "a comment inside a declaration is not a part",
			source: "package p\n\nfunc a() {\n\t// inside\n}\nfunc b() {}\n",
			want:   "package p\n\nfunc a() {\n\t// inside\n}\n\nfunc b() {}\n",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spaced, err := SpaceDeclarations([]byte(testCase.source))

			require.NoError(t, err)
			require.Equal(t, testCase.want, string(spaced))
			formatted, err := format.Source(spaced)
			require.NoError(t, err)
			require.Equal(t, string(spaced), string(formatted), "the spaced source stays gofmt-clean")
		})
	}
}

func TestSpaceDeclarationsRefusesSourceItCannotSpace(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   error
	}{
		{name: "source that does not parse", source: "package p\n\nfunc {", want: nil},
		{name: "source longer than the ceiling", source: "package p\n\n// " + strings.Repeat("x", maxFormattedFileBytes), want: errGeneratedFileTooLarge},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spaced, err := SpaceDeclarations([]byte(testCase.source))

			require.Error(t, err)
			require.Nil(t, spaced)
			if testCase.want != nil {
				require.ErrorIs(t, err, testCase.want)
			}
		})
	}
}

func TestSpaceDeclarationsTakesTimeLinearInTheDeclarations(t *testing.T) {
	t.Parallel()

	source := func(declarations int) []byte {
		var builder strings.Builder
		builder.WriteString("package p\n\n")
		for index := range declarations {
			builder.WriteString("// v" + strconv.Itoa(index) + " is documented.\nvar v" + strconv.Itoa(index) + " = 1 // trailing\n")
		}
		return []byte(builder.String())
	}
	smaller, larger := source(2_000), source(8_000)

	scaling.Linear(t,
		func() { _, _ = SpaceDeclarations(smaller) },
		func() { _, _ = SpaceDeclarations(larger) },
	)
}
