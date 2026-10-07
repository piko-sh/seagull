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

package directive

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqllex"
)

func TestParserReportsAMisspeltDirectiveAfterTheSQL(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(Both, many)\nSELECT id FROM items;\n\n-- sgl.qeury(Purge, exec)\nDELETE FROM items WHERE id = $1;")

	require.Equal(t, "Both", block.Name.Value)
	unknown := diagnosticsWithCode(diagnostics, diag.CodeUnknownDirective)
	require.Len(t, unknown, 1)
	require.Equal(t, diag.SeverityError, unknown[0].Severity)
	require.Equal(t, 4, unknown[0].Line)
	require.Equal(t, 4, unknown[0].Column)
	require.Equal(t, 13, unknown[0].EndColumn)
	require.Contains(t, unknown[0].Message, `"sgl.qeury"`)
	require.Equal(t, "sgl.query", unknown[0].Suggestion)
	require.Len(t, diagnostics, 1)
}

func TestParserWarnsThatAKnownDirectiveAfterTheSQLIsIgnored(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetItem, one)\nSELECT id\n-- sgl.column(id, nullable: true)\nFROM items\n  -- $1 as sgl.param(item_id)\nWHERE id = $1;")

	require.Empty(t, block.ColumnOverrides)
	require.Empty(t, block.Parameters)
	ignored := diagnosticsWithCode(diagnostics, diag.CodeDirectiveWrongContext)
	require.Len(t, ignored, 2)
	require.Equal(t, diag.SeverityWarning, ignored[0].Severity)
	require.Equal(t, 3, ignored[0].Line)
	require.Contains(t, ignored[0].Message, "sgl.column after the query's SQL is ignored")
	require.Equal(t, 5, ignored[1].Line)
	require.Equal(t, 6, ignored[1].Column)
	require.Contains(t, ignored[1].Message, "parameter binding after the query's SQL is ignored")
	require.Len(t, diagnostics, 2)
}

func TestParserReadsTheHeaderUnderTheBlockLexicalRules(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		wantName      string
		rules         sqllex.Rules
		wantUnknown   int
		wantNoMissing bool
	}{
		{
			name:          "a directive inside a string after the SQL is not reported",
			sql:           "-- sgl.query(GetItem, one)\nSELECT '\n-- sgl.qeury(x)\n' AS text;",
			wantName:      "GetItem",
			wantNoMissing: true,
		},
		{
			name:          "a directive inside a block comment after the SQL is not reported",
			sql:           "-- sgl.query(GetItem, one)\nSELECT 1 /*\n-- sgl.qeury(x)\n*/;",
			wantName:      "GetItem",
			wantNoMissing: true,
		},
		{
			name:          "a byte order mark before the header is skipped",
			sql:           sqllex.ByteOrderMark + "-- sgl.query(GetItem, one)\nSELECT 1;",
			wantName:      "GetItem",
			wantNoMissing: true,
		},
		{
			name:          "a blank line inside the header does not end it",
			sql:           "-- sgl.query(GetItem, one)\n\n-- Reads one item.\n\n-- sgl.column(id, nullable: true)\nSELECT id FROM items;",
			wantName:      "GetItem",
			wantNoMissing: true,
		},
		{
			name:          "a comment opener the dialect does not read as a comment opens no header",
			sql:           "--sgl.query(GetItem, one)\nSELECT 1;",
			rules:         sqllex.Rules{Comments: sqllex.CommentRules{DoubleDashRequiresWhitespace: true}},
			wantName:      "",
			wantNoMissing: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			block, diagnostics := newTestParser().Parse(sqlfile.QueryBlock{SQL: testCase.sql, Lexical: testCase.rules, StartLine: 1}, "test.sql")

			if testCase.wantName != "" {
				require.NotNil(t, block.Name)
				require.Equal(t, testCase.wantName, block.Name.Value)
			} else {
				require.Nil(t, block.Name)
			}
			require.Len(t, diagnosticsWithCode(diagnostics, diag.CodeUnknownDirective), testCase.wantUnknown)
			require.Equal(t, testCase.wantNoMissing, len(diagnosticsWithCode(diagnostics, diag.CodeMissingDirective)) == 0)
		})
	}
}

func TestParserReadsADirectiveAfterABlankHeaderLine(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParser(),
		"-- sgl.query(GetItem, one)\n\n-- sgl.column(id, nullable: true)\nSELECT id FROM items;")

	require.Empty(t, diagnostics)
	require.Len(t, block.ColumnOverrides, 1)
	require.Equal(t, 3, block.ColumnOverrides[0].NameSpan.Line)
}
