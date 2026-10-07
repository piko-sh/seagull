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

package martian

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func analyseText(t *testing.T, sql string) (*ast.RawQueryAnalysis, ast.ParsedStatement) {
	t.Helper()

	statement := parseOne(t, sql)
	analysis, err := New().AnalyseQuery(t.Context(), nil, statement)
	require.NoError(t, err)
	return analysis, statement
}

func relativeText(sql string, statement ast.ParsedStatement, span ast.Span) string {
	return sql[statement.Location+span.Start : statement.Location+span.End]
}

func TestAnalyseRecordsSelectSpans(t *testing.T) {
	t.Parallel()

	sql := "-- lead\nSELECT i.id, CONCAT(name, 'x') AS label, * FROM items i WHERE id IN (@p1) AND name = @p2 ORDER BY id DESC ;"
	analysis, statement := analyseText(t, sql)
	spans := analysis.Spans

	require.Equal(t, "i.id, CONCAT(name, 'x') AS label, *", relativeText(sql, statement, spans.Projection))
	require.Equal(t, "FROM items i", relativeText(sql, statement, spans.From))
	require.Equal(t, "WHERE id IN (@p1) AND name = @p2", relativeText(sql, statement, spans.Where))
	require.Equal(t, "id IN (@p1) AND name = @p2", relativeText(sql, statement, spans.WhereCondition))
	require.Equal(t, "ORDER BY id DESC", relativeText(sql, statement, spans.OrderBy))
	require.Equal(t, ";", relativeText(sql, statement, spans.Terminator))
	require.Len(t, spans.ProjectionItems, 3)
	require.Equal(t, "CONCAT(name, 'x') AS label", relativeText(sql, statement, spans.ProjectionItems[1].Span))
	require.Equal(t, "AS label", relativeText(sql, statement, spans.ProjectionItems[1].Alias))
	require.True(t, spans.ProjectionItems[2].Star)
	require.Len(t, spans.Placeholders, 2)
	require.Equal(t, 1, spans.Placeholders[0].Number)
	require.Equal(t, "IN", relativeText(sql, statement, spans.Placeholders[0].List.Operator))
	require.Equal(t, "(@p1)", relativeText(sql, statement, spans.Placeholders[0].List.List))
	require.Equal(t, ast.ListSpan{}, spans.Placeholders[1].List)
}

func TestAnalyseRecordsWriteSpans(t *testing.T) {
	t.Parallel()

	insert := "INSERT INTO dbo.items AS i (id, name) VALUES (@p1, CONCAT(@p2, 'x'))"
	analysis, statement := analyseText(t, insert)
	require.Equal(t, ast.TableReference{Schema: "DBO", Name: "ITEMS", Alias: "I"}, analysis.InsertTarget)
	require.Len(t, analysis.Spans.Values, 1)
	require.Equal(t, "(@p1, CONCAT(@p2, 'x'))", relativeText(insert, statement, analysis.Spans.Values[0].Span))
	require.Len(t, analysis.Spans.Values[0].Items, 2)
	require.Equal(t, "CONCAT(@p2, 'x')", relativeText(insert, statement, analysis.Spans.Values[0].Items[1]))
	require.False(t, analysis.Spans.Projection.Known())

	update := "UPDATE items SET name = @p2 WHERE id = @p1"
	analysis, statement = analyseText(t, update)
	require.Equal(t, "WHERE id = @p1", relativeText(update, statement, analysis.Spans.Where))
	require.Len(t, analysis.Spans.Placeholders, 2)
	require.Equal(t, 2, analysis.Spans.Placeholders[0].Number)
}

func describeMartianClause(sql string, expression ast.Expression) string {
	switch typed := expression.(type) {
	case *ast.ColumnRefExpression:
		return "column " + sql[typed.Span.Start:typed.Span.End]
	case *ast.FunctionCallExpression:
		return "call " + sql[typed.Span.Start:typed.Span.End]
	case *ast.ComparisonExpression:
		return describeMartianClause(sql, typed.Left) + " " + typed.Operator + " " + describeMartianClause(sql, typed.Right)
	default:
		return fmt.Sprintf("%T", expression)
	}
}

func TestAnalyseRecordsClauseExpressions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "a SELECT records its WHERE condition and ORDER BY keys",
			sql:      "SELECT id FROM items WHERE i.quantity > 1 ORDER BY UPPER(name), id",
			expected: []string{"WHERE: column i.quantity > *ast.LiteralExpression", "ORDER BY: call UPPER", "ORDER BY: column id"},
		},
		{
			name:     "an UPDATE records its SET targets and values and its WHERE condition",
			sql:      "UPDATE items SET name = @p1, quantity = quantity + 1 WHERE id = @p2",
			expected: []string{"SET target: column name", "SET value: *ast.ParameterExpression", "SET target: column quantity", "SET value: *ast.BinaryOpExpression", "WHERE: column id = *ast.ParameterExpression"},
		},
		{
			name:     "a conversion records a cast of what it converts",
			sql:      "SELECT id FROM items WHERE name = CONVERT(@p1 TO TEXTUAL(80))",
			expected: []string{"WHERE: column name = *ast.CastExpression"},
		},
		{
			name:     "an INSERT records its VALUES row's values",
			sql:      "INSERT INTO items (id) VALUES (@p1)",
			expected: []string{"VALUES row value: *ast.ParameterExpression"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseText(t, testCase.sql)
			var described []string
			for _, clauseExpression := range analysis.ClauseExpressions {
				described = append(described, clauseExpression.Clause.String()+": "+describeMartianClause(testCase.sql, clauseExpression.Expression))
			}
			require.Equal(t, testCase.expected, described)
		})
	}
}

func TestAnalyseRecordsTheTypeAConversionDeclares(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "a converted placeholder compared with a column declares the conversion's type",
			sql:      "SELECT id FROM items WHERE name = CONVERT(@p1 TO TEXTUAL(80))",
			expected: []string{"@p1 comparison .NAME TEXTUAL"},
		},
		{
			name:     "a converted placeholder assigned to a column declares the conversion's type",
			sql:      "INSERT INTO items (id, name) VALUES (@p1, CONVERT(@p2 TO WHOLE32))",
			expected: []string{"@p1 assignment ITEMS.ID -", "@p2 assignment ITEMS.NAME WHOLE32"},
		},
		{
			name:     "a placeholder inside a converted expression declares nothing",
			sql:      "UPDATE items SET name = CONVERT(@p1 + 1 TO TEXTUAL) WHERE id = @p2",
			expected: []string{"@p1 unknown - -", "@p2 comparison .ID -"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseText(t, testCase.sql)
			described := make([]string, 0, len(analysis.ParameterReferences))
			for _, reference := range analysis.ParameterReferences {
				column, declared := "-", "-"
				if reference.ColumnReference != nil {
					column = reference.ColumnReference.TableAlias + "." + reference.ColumnReference.ColumnName
				}
				if reference.CastType != nil {
					declared = reference.CastType.EngineName
				}
				described = append(described, fmt.Sprintf("@p%d %s %s %s", reference.Number, reference.Context, column, declared))
			}
			require.Equal(t, testCase.expected, described)
		})
	}
}
