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

package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
)

type failingReader struct {
	err  error
	data []byte
}

func (r *failingReader) Read(buffer []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	read := copy(buffer, r.data)
	r.data = r.data[read:]
	return read, nil
}

func TestScanLinesMarksALineTooLongToShowAndKeepsReading(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", maxSourceLineBytes+1)
	tests := []struct {
		name string
		text string
		want []sourceText
	}{
		{name: "short lines", text: "one\r\ntwo\nthree", want: []sourceText{{text: "one"}, {text: "two"}, {text: "three"}}},
		{name: "a line too long between short ones", text: "one\n" + long + "\nthree\n", want: []sourceText{{text: "one"}, {text: "", tooLong: true}, {text: "three"}}},
		{name: "a line exactly as long as allowed", text: strings.Repeat("y", maxSourceLineBytes) + "\n", want: []sourceText{{text: strings.Repeat("y", maxSourceLineBytes)}}},
		{name: "nothing", text: "", want: nil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, scanLines(strings.NewReader(testCase.text)))
		})
	}
}

func TestScanLinesKeepsTheLinesReadBeforeAnError(t *testing.T) {
	t.Parallel()

	lines := scanLines(&failingReader{data: []byte("one\ntwo\n"), err: errors.New("disk gone")})

	assert.Equal(t, []sourceText{{text: "one"}, {text: "two"}}, lines)
}

func TestWriteTextShowsALineTooLongToShow(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "long.sql")
	content := "SELECT 1;\n" + strings.Repeat("y", maxSourceLineBytes+10) + "\nSELECT 3;\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	findings := []finding{
		{path: path, display: "long.sql", database: "app", diagnostic: seagull.SourceError{Code: "Q001", Message: "on the long line", Line: 2, Column: 40}},
		{path: path, display: "long.sql", database: "app", diagnostic: seagull.SourceError{Code: "Q001", Message: "on the line after", Line: 3, Column: 8}},
	}
	var rendered bytes.Buffer

	require.NoError(t, writeText(&rendered, findings))

	assert.Contains(t, rendered.String(), " 2 | "+tooLongLine+"\n")
	assert.Contains(t, rendered.String(), " 3 | SELECT 3;\n")
	assert.NotContains(t, rendered.String(), "yyyy")
}

func TestReadLinesReadsOnlyRegularFiles(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "query.sql"), []byte("SELECT 1;\n"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(directory, "folder.sql"), 0o750))

	tests := []struct {
		name string
		path string
		want []sourceText
	}{
		{name: "a regular file", path: filepath.Join(directory, "query.sql"), want: []sourceText{{text: "SELECT 1;"}}},
		{name: "a directory", path: filepath.Join(directory, "folder.sql"), want: nil},
		{name: "a missing file", path: filepath.Join(directory, "missing.sql"), want: nil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, readLines(testCase.path))
		})
	}
}

func TestNewTableAlignsColumns(t *testing.T) {
	t.Parallel()

	var rendered bytes.Buffer
	table := newTable(&rendered)
	_, _ = table.Write([]byte("NAME\tSTATE\nlonger name\tok\n"))
	require.NoError(t, table.Flush())

	assert.Equal(t, "NAME         STATE\nlonger name  ok\n", rendered.String())
}

func TestEscapePropertyEscapesTheWorkflowCommandSeparators(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "a%3Ab%2Cc%25%0A", escapeProperty("a:b,c%\n"))
}

func TestFindingPresentationNamesTheLocationAndTheSuggestion(t *testing.T) {
	t.Parallel()

	item := &finding{display: "file.sql"}
	assert.Equal(t, "file.sql", item.presentation().Location)

	item.diagnostic.Line = 3
	assert.Equal(t, "file.sql:3", item.presentation().Location)

	item.diagnostic.Suggestion = "email"
	assert.Equal(t, `did you mean "email"?`, item.presentation().Help)
}

func TestSourceLineReadsALineOfACachedFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cache map[string][]sourceText
		name  string
		path  string
		want  string
		line  int
	}{
		{name: "a file that does not exist", cache: map[string][]sourceText{}, path: "/does/not/exist.sql", line: 1, want: ""},
		{name: "a line past the end", cache: map[string][]sourceText{"cached.sql": {{text: "one"}}}, path: "cached.sql", line: 2, want: ""},
		{name: "a line of the file", cache: map[string][]sourceText{"cached.sql": {{text: "one"}}}, path: "cached.sql", line: 1, want: "one"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, sourceLine(testCase.cache, testCase.path, testCase.line).text)
		})
	}
}
