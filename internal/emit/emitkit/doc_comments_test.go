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

package emitkit_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit/emitkit"
)

var commentLineCases = []struct {
	name string
	text string
	want []string
}{
	{name: "no text gives no comment", text: "", want: nil},
	{name: "one line", text: "Lists every post.", want: []string{"// Lists every post."}},
	{name: "paragraphs keep their empty line", text: "Lists every post.\n\nIt reads the replica.", want: []string{"// Lists every post.", "//", "// It reads the replica."}},
	{name: "indentation is kept and trailing spaces dropped", text: "Example:  \n\n  SELECT 1;", want: []string{"// Example:", "//", "//   SELECT 1;"}},
	{name: "empty lines at either end are dropped", text: "\n\n  \nBody.\n \n", want: []string{"// Body."}},
	{name: "carriage returns are dropped", text: "First.\r\nSecond.\r", want: []string{"// First.", "// Second."}},
	{name: "a line that would read as a directive does not", text: "go:generate stringer\n+build ignore", want: []string{"// go:generate stringer", "// +build ignore"}},
	{name: "a NUL is replaced", text: "a\x00b", want: []string{"// a\uFFFDb"}},
	{name: "a byte order mark is replaced", text: "\uFEFFLists every post.", want: []string{"// \uFFFDLists every post."}},
	{name: "each byte that is not valid UTF-8 is replaced", text: "a\xff\xfeb", want: []string{"// a\uFFFD\uFFFDb"}},
	{name: "a replacement character is kept", text: "a\uFFFDb", want: []string{"// a\uFFFDb"}},
}

func TestCommentLinesWriteTextAsAGoDocComment(t *testing.T) {
	t.Parallel()

	for _, testCase := range commentLineCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, emitkit.CommentLines(testCase.text))
		})
	}
}

func TestCommentLinesWriteOnlyRunesGoSourceCanHold(t *testing.T) {
	t.Parallel()

	for _, text := range []string{"a\x00b", "\uFEFFdescribed", "bad \xc3\x28 bytes", "x\r\ny\x00\uFEFF\xff"} {
		lines := emitkit.CommentLines(text)
		source := "package p\n\n" + strings.Join(lines, "\n") + "\nvar v int\n"

		_, err := parser.ParseFile(token.NewFileSet(), "p.go", source, parser.ParseComments)

		require.NoErrorf(t, err, "comment lines %q", lines)
	}
}
