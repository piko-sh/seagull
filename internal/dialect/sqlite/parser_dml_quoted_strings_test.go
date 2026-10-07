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

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestADoubleQuotedNameNoColumnHasIsAString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sql      string
		strings  int
		warnings []string
	}{
		{
			name:     "the pattern pieces of a LIKE",
			sql:      `SELECT id FROM posts WHERE title LIKE concat("%", ?, "%")`,
			strings:  2,
			warnings: []string{`SQLite reads "%" as the string '%'`, `SQLite reads "%" as the string '%'`},
		},
		{name: "a value compared with a column", sql: `SELECT id FROM posts WHERE title = "draft"`, strings: 1, warnings: []string{`"draft"`}},
		{name: "a selected value", sql: `SELECT "none" AS label FROM posts`, strings: 1, warnings: []string{`"none"`}},
		{name: "an argument of an aliased call", sql: `SELECT upper("none") AS label FROM posts`, strings: 1, warnings: []string{`"none"`}},
		{name: "a column of a table", sql: `SELECT "title" FROM posts`},
		{name: "a column in another letter case", sql: `SELECT "TITLE" FROM posts`},
		{name: "a qualified column", sql: `SELECT p."title" FROM posts p`},
		{name: "a select-list alias", sql: `SELECT name AS "label" FROM users ORDER BY "label"`},
		{name: "a column of a table function", sql: `SELECT "key", "value" FROM json_each(?)`},
		{name: "a column a common table expression names", sql: `WITH t("x y") AS (SELECT 1) SELECT "x y" FROM t`},
		{name: "the rowid", sql: `SELECT id FROM posts WHERE "rowid" > 1`},
		{name: "a function name", sql: `SELECT "upper"(title) FROM posts`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), test.sql)
			require.Equal(t, test.strings, countTextLiterals(analysis))
			require.Len(t, analysis.Warnings, len(test.warnings))
			for index, warning := range test.warnings {
				require.Contains(t, analysis.Warnings[index].Message, warning)
				require.Positive(t, analysis.Warnings[index].Span.End)
			}
		})
	}
}

func countTextLiterals(analysis *ast.RawQueryAnalysis) int {
	expressions := make([]ast.Expression, 0, len(analysis.OutputColumns)+len(analysis.ClauseExpressions))
	for _, column := range analysis.OutputColumns {
		expressions = append(expressions, column.Expression)
	}
	for _, clause := range analysis.ClauseExpressions {
		if clause.Clause != ast.ClauseSelectList {
			expressions = append(expressions, clause.Expression)
		}
	}
	count := 0
	for len(expressions) > 0 {
		expression := expressions[0]
		expressions = expressions[1:]
		switch typed := expression.(type) {
		case *ast.LiteralExpression:
			if typed.TypeName == literalTypeText {
				count++
			}
		case *ast.ComparisonExpression:
			expressions = append(expressions, typed.Left, typed.Right)
		case *ast.FunctionCallExpression:
			expressions = append(expressions, typed.Arguments...)
		}
	}
	return count
}

func TestAViewReadsItsDoubleQuotedNamesAsColumns(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, `CREATE VIEW labels AS SELECT "id", upper("email") AS "label" FROM users`)
	require.NotNil(t, mutation.ViewDefinition)
	require.Len(t, mutation.ViewDefinition.OutputColumns, 2)
	require.Equal(t, "id", mutation.ViewDefinition.OutputColumns[0].ColumnName)
	call, isCall := mutation.ViewDefinition.OutputColumns[1].Expression.(*ast.FunctionCallExpression)
	require.True(t, isCall, "%T", mutation.ViewDefinition.OutputColumns[1].Expression)
	require.Len(t, call.Arguments, 1)
	_, isColumn := call.Arguments[0].(*ast.ColumnRefExpression)
	require.True(t, isColumn, "%T", call.Arguments[0])
	require.Empty(t, mutation.ViewDefinition.Warnings)
}
