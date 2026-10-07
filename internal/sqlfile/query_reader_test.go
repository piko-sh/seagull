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

package sqlfile

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/testkit/fakefs"
	"seagull.sh/seagull/internal/testkit/scaling"
)

var directiveRoles = []string{"query", "param", "sortable", "embed", "column", "migration"}

var standardSyntax = QuerySyntax{Prefixes: namespace.Default(), Directives: directiveRoles, Lexicon: syntax.Lexicon{LinePrefix: "--", Rules: sqllex.Rules{}}}

type wantBlock struct {
	sql       string
	startLine int
}

func backslashSyntax() QuerySyntax {
	querySyntax := standardSyntax
	querySyntax.Lexicon.Rules.Quotes.SingleQuoteEscape = sqllex.EscapeBackslash
	querySyntax.Lexicon.Rules.Comments.HashLineComments = true
	querySyntax.Lexicon.Rules.Comments.DoubleDashRequiresWhitespace = true
	return querySyntax
}

func blocksOf(blocks []QueryBlock) []wantBlock {
	if len(blocks) == 0 {
		return nil
	}
	found := make([]wantBlock, 0, len(blocks))
	for _, block := range blocks {
		found = append(found, wantBlock{sql: block.SQL, startLine: block.StartLine})
	}
	return found
}

func TestSplitQueryFile(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		input  string
		want   []wantBlock
		syntax QuerySyntax
	}{
		{
			name:   "a single query without a header is one block",
			input:  "SELECT 1;",
			syntax: standardSyntax,
			want:   []wantBlock{{sql: "SELECT 1;", startLine: 1}},
		},
		{
			name:   "each query header opens a block",
			input:  "-- sgl.query(GetUser, one)\nSELECT * FROM users;\n-- sgl.query(ListUsers, many)\nSELECT * FROM users WHERE active;",
			syntax: standardSyntax,
			want: []wantBlock{
				{sql: "-- sgl.query(GetUser, one)\nSELECT * FROM users;", startLine: 1},
				{sql: "-- sgl.query(ListUsers, many)\nSELECT * FROM users WHERE active;", startLine: 3},
			},
		},
		{
			name:   "blank lines between queries are dropped",
			input:  "-- sgl.query(First, one)\nSELECT 1;\n\n\n-- sgl.query(Second, one)\nSELECT 2;",
			syntax: standardSyntax,
			want: []wantBlock{
				{sql: "-- sgl.query(First, one)\nSELECT 1;", startLine: 1},
				{sql: "-- sgl.query(Second, one)\nSELECT 2;", startLine: 5},
			},
		},
		{
			name:   "a leading byte order mark is stripped",
			input:  sqllex.ByteOrderMark + "-- sgl.query(First, one)\nSELECT 1;",
			syntax: standardSyntax,
			want:   []wantBlock{{sql: "-- sgl.query(First, one)\nSELECT 1;", startLine: 1}},
		},
		{
			name:   "a comment directly above a header belongs to the query it heads",
			input:  "-- sgl.query(First, one)\nSELECT 1;\n\n-- Second reads the other row.\n-- sgl.query(Second, one)\nSELECT 2;",
			syntax: standardSyntax,
			want: []wantBlock{
				{sql: "-- sgl.query(First, one)\nSELECT 1;", startLine: 1},
				{sql: "-- Second reads the other row.\n-- sgl.query(Second, one)\nSELECT 2;", startLine: 4},
			},
		},
		{
			name:   "a block comment directly above a header belongs to the query it heads",
			input:  "-- sgl.query(First, one)\nSELECT 1;\n/*\n * Second.\n */\n-- sgl.query(Second, one)\nSELECT 2;",
			syntax: standardSyntax,
			want: []wantBlock{
				{sql: "-- sgl.query(First, one)\nSELECT 1;", startLine: 1},
				{sql: "/*\n * Second.\n */\n-- sgl.query(Second, one)\nSELECT 2;", startLine: 3},
			},
		},
		{
			name:   "a comment after a blank line stays with the query above",
			input:  "-- sgl.query(First, one)\nSELECT 1;\n-- trailing note\n\n-- sgl.query(Second, one)\nSELECT 2;",
			syntax: standardSyntax,
			want: []wantBlock{
				{sql: "-- sgl.query(First, one)\nSELECT 1;\n-- trailing note", startLine: 1},
				{sql: "-- sgl.query(Second, one)\nSELECT 2;", startLine: 5},
			},
		},
		{
			name:   "a comment on the same line as SQL stays with the query above",
			input:  "-- sgl.query(First, one)\nSELECT 1; -- one\n-- sgl.query(Second, one)\nSELECT 2;",
			syntax: standardSyntax,
			want: []wantBlock{
				{sql: "-- sgl.query(First, one)\nSELECT 1; -- one", startLine: 1},
				{sql: "-- sgl.query(Second, one)\nSELECT 2;", startLine: 3},
			},
		},
		{
			name:   "the directives of a header without SQL stay with it",
			input:  "-- sgl.query(First, many)\n-- $1 as sgl.param(id)\n-- sgl.query(Second, one)\nSELECT 2;",
			syntax: standardSyntax,
			want: []wantBlock{
				{sql: "-- sgl.query(First, many)\n-- $1 as sgl.param(id)", startLine: 1},
				{sql: "-- sgl.query(Second, one)\nSELECT 2;", startLine: 3},
			},
		},
		{
			name:   "a comment-only stretch before the first header is not a block",
			input:  "-- Licence header.\n-- More licence.\n\n-- sgl.query(First, one)\nSELECT 1;",
			syntax: standardSyntax,
			want:   []wantBlock{{sql: "-- sgl.query(First, one)\nSELECT 1;", startLine: 4}},
		},
		{
			name:   "a file holding only comments has no blocks",
			input:  "-- nothing here yet\n/* or here */\n",
			syntax: standardSyntax,
			want:   nil,
		},
		{
			name:   "a directive before the first header without SQL is still a block",
			input:  "-- $1 as sgl.param(id)\n",
			syntax: standardSyntax,
			want:   []wantBlock{{sql: "-- $1 as sgl.param(id)", startLine: 1}},
		},
		{
			name:   "a block's start line skips the blank lines before it",
			input:  "\n\n-- sgl.query(First, one)\nSELECT 1;",
			syntax: standardSyntax,
			want:   []wantBlock{{sql: "-- sgl.query(First, one)\nSELECT 1;", startLine: 3}},
		},
		{
			name:   "a header written without a space after the comment opener opens a block",
			input:  "-- sgl.query(First, one)\nSELECT 1;\n--sgl.query(Second, one)\nSELECT 2;",
			syntax: standardSyntax,
			want: []wantBlock{
				{sql: "-- sgl.query(First, one)\nSELECT 1;", startLine: 1},
				{sql: "--sgl.query(Second, one)\nSELECT 2;", startLine: 3},
			},
		},
		{
			name:   "a misspelt header after SQL opens a block of its own",
			input:  "-- sgl.query(Both, many)\nSELECT id FROM items;\n\n-- sgl.qeury(Purge, exec)\nDELETE FROM items WHERE id = ?;",
			syntax: standardSyntax,
			want: []wantBlock{
				{sql: "-- sgl.query(Both, many)\nSELECT id FROM items;", startLine: 1},
				{sql: "-- sgl.qeury(Purge, exec)\nDELETE FROM items WHERE id = ?;", startLine: 4},
			},
		},
		{
			name:   "a known directive after SQL does not open a block",
			input:  "-- sgl.query(First, one)\nSELECT 1\n-- sgl.column(id, nullable: true)\nFROM items;",
			syntax: standardSyntax,
			want:   []wantBlock{{sql: "-- sgl.query(First, one)\nSELECT 1\n-- sgl.column(id, nullable: true)\nFROM items;", startLine: 1}},
		},
		{
			name:   "a misspelt directive in the header does not open a block",
			input:  "-- sgl.query(First, one)\n-- sgl.colum(id, nullable: true)\nSELECT 1;",
			syntax: standardSyntax,
			want:   []wantBlock{{sql: "-- sgl.query(First, one)\n-- sgl.colum(id, nullable: true)\nSELECT 1;", startLine: 1}},
		},
		{
			name:   "a header inside a string does not open a block",
			input:  "-- sgl.query(First, one)\nSELECT '\n-- sgl.query(Second, one)\n';",
			syntax: standardSyntax,
			want:   []wantBlock{{sql: "-- sgl.query(First, one)\nSELECT '\n-- sgl.query(Second, one)\n';", startLine: 1}},
		},
		{
			name:   "a header inside a block comment does not open a block",
			input:  "-- sgl.query(First, one)\nSELECT 1 /*\n-- sgl.query(Second, one)\n*/;",
			syntax: standardSyntax,
			want:   []wantBlock{{sql: "-- sgl.query(First, one)\nSELECT 1 /*\n-- sgl.query(Second, one)\n*/;", startLine: 1}},
		},
		{
			name:   "a backslash-escaped quote keeps a string open under rules that allow it",
			input:  "-- sgl.query(First, one)\nSELECT 'it\\'s\n-- sgl.query(Second, one)\n';",
			syntax: backslashSyntax(),
			want:   []wantBlock{{sql: "-- sgl.query(First, one)\nSELECT 'it\\'s\n-- sgl.query(Second, one)\n';", startLine: 1}},
		},
		{
			name:   "a header is read under the dialect's rules for the comment opener",
			input:  "-- sgl.query(First, one)\nSELECT 1;\n--sgl.query(Second, one)\nSELECT 2;",
			syntax: backslashSyntax(),
			want:   []wantBlock{{sql: "-- sgl.query(First, one)\nSELECT 1;\n--sgl.query(Second, one)\nSELECT 2;", startLine: 1}},
		},
		{
			name:   "reading resumes after an unterminated string so later headers are found",
			input:  "-- sgl.query(First, one)\nSELECT 'open;\n-- sgl.query(Second, one)\nSELECT 2;",
			syntax: standardSyntax,
			want: []wantBlock{
				{sql: "-- sgl.query(First, one)\nSELECT 'open;", startLine: 1},
				{sql: "-- sgl.query(Second, one)\nSELECT 2;", startLine: 3},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			blocks, _ := SplitQueryFile([]byte(testCase.input), testCase.syntax)
			require.Equal(t, testCase.want, blocksOf(blocks))
			for _, block := range blocks {
				require.Equal(t, testCase.syntax.Lexicon.Rules.Quotes, block.Lexical.Quotes)
				require.Equal(t, testCase.syntax.Lexicon.Rules.Comments, block.Lexical.Comments)
			}
		})
	}
}

func TestSplitQueryFileTakesLinearTimeOverALineOfBlanksAndComments(t *testing.T) {
	t.Parallel()

	file := func(size int) []byte {
		return []byte(strings.Repeat(" ", size) + strings.Repeat("/* c */ ", size/4) +
			"\n-- sgl.query(One, one)\nSELECT 1;\n")
	}
	smaller, larger := file(20_000), file(80_000)
	scaling.Linear(t,
		func() { SplitQueryFile(smaller, standardSyntax) },
		func() { SplitQueryFile(larger, standardSyntax) },
	)
}

func TestSplitQueryFileSplitsOnAliasAndForeignOpeners(t *testing.T) {
	t.Parallel()

	content := "-- acme.query(First, one)\nSELECT 1;\n-- acmedb.query(Second, one)\nSELECT 2;\n-- other.query(Third, one)\nSELECT 3;\n-- unrelated.query(Fourth, one)\nSELECT 4;"
	querySyntax := standardSyntax
	querySyntax.Prefixes = namespace.Set{Canonical: "acme", Aliases: []string{"acmedb"}, Foreign: []string{"other"}}

	blocks, _ := SplitQueryFile([]byte(content), querySyntax)

	require.Len(t, blocks, 3)
	require.Equal(t, []int{1, 3, 5}, []int{blocks[0].StartLine, blocks[1].StartLine, blocks[2].StartLine})
	require.Contains(t, blocks[2].SQL, "unrelated.query")
}

func TestListQueryFiles(t *testing.T) {
	t.Parallel()

	reader := &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{
			"queries": {
				&fakefs.DirEntry{EntryName: "z_last.sql", Directory: false},
				&fakefs.DirEntry{EntryName: "notes.md", Directory: false},
				&fakefs.DirEntry{EntryName: "nested.sql", Directory: true},
				&fakefs.DirEntry{EntryName: "a_first.sql", Directory: false},
				&fakefs.DirEntry{EntryName: "001_reports.down.sql", Directory: false},
			},
		},
	}

	names, err := ListQueryFiles(t.Context(), reader, "queries")
	require.NoError(t, err)
	require.Equal(t, []string{"001_reports.down.sql", "a_first.sql", "z_last.sql"}, names)

	failing := &fakefs.FileReader{ReadDirErrors: map[string]error{"queries": errors.New("permission denied")}}
	_, err = ListQueryFiles(t.Context(), failing, "queries")
	require.ErrorContains(t, err, "reading query directory")
}

func TestSplitTokensMatchFreshScan(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		text       string
		wantBlocks int
	}{
		{
			name:       "blocks padded by a byte order mark, carriage returns and trailing tabs",
			text:       "\ufeff  -- sgl.query(One, one)\r\n SELECT 'a -- b'; \n\n-- sgl.query(Two, one)\nSELECT 2; \t",
			wantBlocks: 2,
		},
		{
			name:       "a block left unterminated before a good one",
			text:       "-- sgl.query(Broken, one)\nSELECT 'broken\n-- sgl.query(Good, one)\nSELECT 2;",
			wantBlocks: 2,
		},
		{
			name:       "a block ending in a comment and trailing spaces",
			text:       "  -- sgl.query(One, one)\nSELECT 1; -- trailing comment   ",
			wantBlocks: 1,
		},
		{
			name:       "blocks with no whitespace around them",
			text:       "-- sgl.query(One, one)\nSELECT 1;\n-- sgl.query(Two, one)\nSELECT 2;",
			wantBlocks: 2,
		},
		{
			name:       "blocks separated by runs of blank lines",
			text:       "\n\n\n-- sgl.query(One, one)\nSELECT 1;\n\n\n\n-- sgl.query(Two, one)\n  SELECT 2;   \n\n",
			wantBlocks: 2,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			blocks, _ := SplitQueryFile([]byte(testCase.text), standardSyntax)
			require.Len(t, blocks, testCase.wantBlocks)
			for _, block := range blocks {
				require.Equal(t, syntax.Tokens(block.Lexical, block.SQL), block.Tokens())
				if block.StrictTokens() != nil {
					tokens, err := sqllex.Tokens(block.Lexical, block.SQL)
					require.NoError(t, err)
					require.Equal(t, tokens, block.StrictTokens())
				}
			}
		})
	}
}

func TestListQueryFilesLeavesOutFilesNoReaderCanLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode fs.FileMode
	}{
		{name: "a named pipe", mode: fs.ModeNamedPipe},
		{name: "a socket", mode: fs.ModeSocket},
		{name: "a device", mode: fs.ModeDevice},
		{name: "a character device", mode: fs.ModeDevice | fs.ModeCharDevice},
		{name: "an irregular file", mode: fs.ModeIrregular},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := fstest.MapFS{
				"queries/items.sql":   &fstest.MapFile{Data: []byte("SELECT 1;")},
				"queries/special.sql": &fstest.MapFile{Mode: testCase.mode},
			}
			names, err := ListQueryFiles(t.Context(), NewFSReader(files, 0, 0), "queries")
			require.NoError(t, err)
			require.Equal(t, []string{"items.sql"}, names)
		})
	}
}

func TestSplitQueryFileReportsWhereItStoppedReadingTheFile(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		content  string
		wantLine int
	}{
		{
			name:     "a file read whole",
			content:  "-- sgl.query(One, one)\nSELECT 'open\n-- sgl.query(Two, one)\nSELECT 2;\n",
			wantLine: 0,
		},
		{
			name:     "a file holding more unterminated strings than are read",
			content:  "-- sgl.query(One, one)\n" + strings.Repeat("SELECT 1; /* open\n", 65) + "-- sgl.query(Two, one)\nSELECT 2;\n",
			wantLine: 66,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			blocks, truncatedLine := SplitQueryFile([]byte(testCase.content), standardSyntax)

			assert.Equal(t, testCase.wantLine, truncatedLine)
			assert.NotEmpty(t, blocks)
		})
	}
}
