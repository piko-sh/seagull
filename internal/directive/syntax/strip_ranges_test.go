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

package syntax_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	clickhouselexical "seagull.sh/seagull/internal/dialect/clickhouse/lexical"
	duckdblexical "seagull.sh/seagull/internal/dialect/duckdb/lexical"
	mysqllexical "seagull.sh/seagull/internal/dialect/mysql/lexical"
	postgreslexical "seagull.sh/seagull/internal/dialect/postgres/lexical"
	sqlitelexical "seagull.sh/seagull/internal/dialect/sqlite/lexical"
	timescaledblexical "seagull.sh/seagull/internal/dialect/timescaledb/lexical"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqllex"
)

func TestStripRanges(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		kept     string
		expected []syntax.Range
	}{
		{
			name:     "a text without directives is kept whole, trimmed",
			sql:      "  SELECT 1\n",
			kept:     "SELECT 1",
			expected: []syntax.Range{{Start: 2, End: 10}},
		},
		{
			name:     "a header directive is left out with its newline",
			sql:      "-- sgl.query(name: A, command: one)\nSELECT 1;",
			kept:     "SELECT 1;",
			expected: []syntax.Range{{Start: 36, End: 45}},
		},
		{
			name:     "a binding line inside the statement is left out and the lines around it join",
			sql:      "SELECT id\nFROM t\n-- $1 as sgl.param(id)\nWHERE id = $1",
			kept:     "SELECT id\nFROM t\nWHERE id = $1",
			expected: []syntax.Range{{Start: 0, End: 17}, {Start: 40, End: 53}},
		},
		{
			name:     "a directive whose parentheses stay open takes its continuation lines with it",
			sql:      "-- sgl.query(name: A,\n--   command: one)\nSELECT 1",
			kept:     "SELECT 1",
			expected: []syntax.Range{{Start: 41, End: 49}},
		},
		{
			name:     "ordinary comments are kept",
			sql:      "-- sgl.query(name: A, command: one)\n-- the newest first\nSELECT 1",
			kept:     "-- the newest first\nSELECT 1",
			expected: []syntax.Range{{Start: 36, End: 64}},
		},
		{
			name:     "a carriage return stays with the line it ends",
			sql:      "-- sgl.query(name: A, command: one)\r\nSELECT 1\r\nFROM t",
			kept:     "SELECT 1\r\nFROM t",
			expected: []syntax.Range{{Start: 37, End: 53}},
		},
		{
			name:     "a trailing directive leaves no newline behind",
			sql:      "SELECT 1\n-- sgl.query(name: A, command: one)\n",
			kept:     "SELECT 1",
			expected: []syntax.Range{{Start: 0, End: 8}},
		},
		{
			name:     "a text of directives alone keeps nothing",
			sql:      "-- sgl.query(name: A, command: one)\n",
			kept:     "",
			expected: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ranges := syntax.StripRanges(testCase.sql, syntax.NeutralLexicon(), namespace.Default())

			require.Equal(t, testCase.expected, ranges)
			require.Equal(t, testCase.kept, syntax.KeptText(testCase.sql, ranges))
			require.Equal(t, testCase.kept, syntax.StripDirectives(testCase.sql, syntax.NeutralLexicon(), namespace.Default()))
		})
	}
}

func TestStripRangesKeepWhatTheLineStripperKeepsInEveryQueryFile(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")
	files := 0
	for _, directory := range []string{filepath.Join("internal", "dialect"), filepath.Join("tests", "integration"), filepath.Join("internal", "pipeline", "testdata")} {
		require.NoError(t, filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || filepath.Ext(path) != ".sql" || filepath.Base(filepath.Dir(path)) != "queries" {
				return err
			}
			content, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			lexicon := lexiconFor(path)
			for _, text := range queryTexts(content, lexicon) {
				requireRangesMatchLineStripper(t, path, text, lexicon)
			}
			files++
			return nil
		}))
	}
	require.Greater(t, files, 100)
}

func TestStripRangesTrimsWhitespaceAcrossRemovedLines(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		sql  string
		want string
	}{
		{"unicode", "\u2003\n-- sgl.query(A, one)\n\tSELECT '\u00a0';\u00a0\n-- sgl.query(B, one)\n\u2002\r\n", "SELECT '\u00a0';"},
		{"empty", "\u2003\n-- sgl.query(A, one)\n\u00a0\n-- sgl.query(B, one)\n\u2002", ""},
		{"interior", "\u2003SELECT\u00a0\n-- sgl.query(A, one)\n\u2002'kept'\u2003", "SELECT\u00a0\n\u2002'kept'"},
		{"invalid UTF-8", " \xff\n-- sgl.query(A, one)\n\xfe\u2003", "\xff\n\xfe"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ranges := syntax.StripRanges(test.sql, syntax.NeutralLexicon(), namespace.Default())
			require.Equal(t, test.want, syntax.KeptText(test.sql, ranges))
			if test.want == "" {
				require.Nil(t, ranges)
			}
		})
	}
}

func requireRangesMatchLineStripper(t *testing.T, path string, text string, lexicon syntax.Lexicon) {
	t.Helper()

	prefixes := namespace.Default()
	ranges := syntax.StripRanges(text, lexicon, prefixes)
	previousEnd := -1
	for _, kept := range ranges {
		require.Greater(t, kept.End, kept.Start, "%s: an empty range", path)
		require.Greater(t, kept.Start, previousEnd, "%s: ranges out of order or touching", path)
		require.LessOrEqual(t, kept.End, len(text), "%s: a range past the end", path)
		previousEnd = kept.End
	}
	require.Equal(t, lineStripper(text, lexicon, prefixes), syntax.KeptText(text, ranges), "%s", path)
}

func queryTexts(content []byte, lexicon syntax.Lexicon) []string {
	blocks, _ := sqlfile.SplitQueryFile(content, sqlfile.QuerySyntax{
		Prefixes:   namespace.Default(),
		Directives: directive.Roles(),
		Lexicon:    lexicon,
	})
	texts := make([]string, 0, 1+len(blocks))
	texts = append(texts, string(content))
	for _, block := range blocks {
		texts = append(texts, block.SQL)
	}
	return texts
}

func lexiconFor(path string) syntax.Lexicon {
	rules := map[string]func() sqllex.Rules{
		"postgres":    postgreslexical.Rules,
		"cockroachdb": postgreslexical.Rules,
		"timescaledb": timescaledblexical.Rules,
		"mysql":       mysqllexical.Rules,
		"mariadb":     mysqllexical.Rules,
		"sqlite":      sqlitelexical.Rules,
		"sqlite_cgo":  sqlitelexical.Rules,
		"duckdb":      duckdblexical.Rules,
		"clickhouse":  clickhouselexical.Rules,
	}
	for segment := range strings.SplitSeq(filepath.ToSlash(path), "/") {
		if rulesOf, known := rules[segment]; known {
			return syntax.Lexicon{LinePrefix: "--", Rules: rulesOf()}
		}
	}
	return syntax.NeutralLexicon()
}

func lineStripper(sql string, lexicon syntax.Lexicon, prefixes namespace.Set) string {
	comments, lines := lexicon.DirectiveComments(sql)
	stripped := make(map[int]struct{})
	for index := 0; index < len(comments); index++ {
		if !syntax.IsDirectiveText(comments[index].Body, prefixes) {
			continue
		}
		line := lines.Line(comments[index].Token.Start)
		stripped[line] = struct{}{}
		depth, unterminated, _ := syntax.LineDepth(comments[index].Body)
		for depth > 0 && unterminated < 0 && index+1 < len(comments) && lines.Line(comments[index+1].Token.Start) == line+1 {
			index++
			line++
			stripped[line] = struct{}{}
			var change int
			change, unterminated, _ = syntax.LineDepth(comments[index].Body)
			depth += change
		}
	}
	if len(stripped) == 0 {
		return strings.TrimSpace(sql)
	}
	var builder strings.Builder
	lineNumber := 0
	for line := range strings.SplitSeq(sql, "\n") {
		lineNumber++
		if _, isDirective := stripped[lineNumber]; isDirective {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(line)
	}
	return strings.TrimSpace(builder.String())
}
