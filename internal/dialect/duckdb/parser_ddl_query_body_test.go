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

package duckdb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
)

func TestApplyDDLFailsAStatementWhoseQueryBodyIsMalformed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a view whose join is not closed", sql: "CREATE VIEW v (a, b) AS SELECT x, y FROM t JOIN u USING (x"},
		{name: "a view whose select list runs together", sql: "CREATE VIEW v AS SELECT a b c d FROM"},
		{name: "a view whose parentheses are not closed", sql: "CREATE VIEW v AS SELECT (((1"},
		{name: "a view with no query", sql: "CREATE VIEW v AS"},
		{name: "a table created from a malformed query", sql: "CREATE TABLE t2 AS SELECT a b c FROM"},
		{name: "a table macro whose query is malformed", sql: "CREATE MACRO m() AS TABLE SELECT a b c d FROM"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := applyDDLError(t, testCase.sql)

			require.ErrorIs(t, err, fault.ErrParse)
			require.ErrorAs(t, err, new(*dialectkit.ParseError))
		})
	}
}

func TestApplyDDLRecordsAQueryBodyItCannotAnalyse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		body     string
		options  []Option
		wantKind ast.MutationKind
	}{
		{name: "a view whose pivot takes its columns from the data", sql: "CREATE VIEW v AS PIVOT t ON species",
			body: "PIVOT t ON species", wantKind: ast.MutationCreateView},
		{name: "a view nesting deeper than the parse depth limit", sql: "CREATE VIEW v AS SELECT ((((((((((1))))))))))",
			body: "SELECT ((((((((((1))))))))))", options: []Option{WithMaxParseDepth(4)}, wantKind: ast.MutationCreateView},
		{name: "a table created from a pivot wider than the limit",
			sql:  "CREATE TABLE wide AS PIVOT t ON a IN (1, 2), b IN (1, 2) USING sum(x) GROUP BY c",
			body: "PIVOT t ON a IN (1, 2), b IN (1, 2) USING sum(x) GROUP BY c", options: []Option{WithMaxPivotColumns(3)},
			wantKind: ast.MutationCreateTable},
		{name: "a table macro whose unpivot lists no columns", sql: "CREATE MACRO m() AS TABLE UNPIVOT t ON COLUMNS(*)",
			body: "UNPIVOT t ON COLUMNS(*)", wantKind: ast.MutationCreateFunction},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(testCase.options...)
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			mutation, err := engine.ApplyDDL(t.Context(), statements[0])

			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Equal(t, testCase.wantKind, mutation.Kind)
			assert.Nil(t, mutation.ViewDefinition)
			assert.Empty(t, mutation.Columns)
			require.Len(t, mutation.UnanalysedParts, 1)
			assert.Contains(t, mutation.UnanalysedParts[0].Message, "was not analysed")
			start := strings.Index(testCase.sql, testCase.body)
			assert.Equal(t, ast.Span{Start: start, End: start + len(testCase.body)}, mutation.UnanalysedParts[0].Span)
		})
	}
}
