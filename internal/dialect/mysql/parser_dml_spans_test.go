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

package mysql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
)

var errWalkStopped = errors.New("walk stopped by the test")

type renderedSpans struct {
	Clauses         map[string]string
	ProjectionItems []string
	ReturningItems  []string
	Values          []string
	Placeholders    []string
}

func analyseStatement(t *testing.T, sql string) (*ast.RawQueryAnalysis, ast.ParsedStatement) {
	t.Helper()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.NoError(t, err)
	return analysis, statements[0]
}

func spanTextOf(sql string, statement ast.ParsedStatement, span ast.Span) string {
	return sql[statement.Location+span.Start : statement.Location+span.End]
}

func renderItems(sql string, statement ast.ParsedStatement, items []ast.ItemSpan) []string {
	if len(items) == 0 {
		return nil
	}
	rendered := make([]string, 0, len(items))
	for _, item := range items {
		rendered = append(rendered, fmt.Sprintf("%s|%s|%s|%t", spanTextOf(sql, statement, item.Span),
			spanTextOf(sql, statement, item.Expression), spanTextOf(sql, statement, item.Alias), item.Star))
	}
	return rendered
}

func renderSpans(sql string, statement ast.ParsedStatement, spans ast.StatementSpans) renderedSpans {
	rendered := renderedSpans{Clauses: map[string]string{}}
	for name, span := range map[string]ast.Span{
		"Quantifier": spans.Quantifier, "Projection": spans.Projection, "From": spans.From, "Where": spans.Where,
		"WhereCondition": spans.WhereCondition, "GroupBy": spans.GroupBy, "Having": spans.Having, "OrderBy": spans.OrderBy,
		"Pagination": spans.Pagination, "Locking": spans.Locking, "Trailing": spans.Trailing, "Returning": spans.Returning,
		"Terminator": spans.Terminator,
	} {
		if span.Known() {
			rendered.Clauses[name] = spanTextOf(sql, statement, span)
		}
	}
	rendered.ProjectionItems = renderItems(sql, statement, spans.ProjectionItems)
	rendered.ReturningItems = renderItems(sql, statement, spans.ReturningItems)
	for _, row := range spans.Values {
		var items []string
		for _, item := range row.Items {
			items = append(items, spanTextOf(sql, statement, item))
		}
		rendered.Values = append(rendered.Values, spanTextOf(sql, statement, row.Span)+"="+strings.Join(items, ";"))
	}
	for _, placeholder := range spans.Placeholders {
		list := ""
		if placeholder.List.List.Known() {
			list = spanTextOf(sql, statement, placeholder.List.Operator) + spanTextOf(sql, statement, placeholder.List.List)
		}
		rendered.Placeholders = append(rendered.Placeholders, fmt.Sprintf("%s|%s|%d|%s",
			spanTextOf(sql, statement, placeholder.Span), placeholder.Name, placeholder.Number, list))
	}
	return rendered
}

func TestStatementSpans(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected renderedSpans
	}{
		{
			name: "a SELECT with every clause",
			sql: "-- leading comment\nSELECT DISTINCT SQL_NO_CACHE i.id, i.name AS label, count(*) total, i.* FROM items i " +
				"JOIN items j ON j.id = i.id WHERE i.id > ? AND i.name NOT IN (?) GROUP BY i.id, i.name WITH ROLLUP " +
				"HAVING count(*) > ? ORDER BY i.id DESC LIMIT ?, ? FOR UPDATE SKIP LOCKED ;",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Quantifier":     "DISTINCT SQL_NO_CACHE",
					"Projection":     "i.id, i.name AS label, count(*) total, i.*",
					"From":           "FROM items i JOIN items j ON j.id = i.id",
					"Where":          "WHERE i.id > ? AND i.name NOT IN (?)",
					"WhereCondition": "i.id > ? AND i.name NOT IN (?)",
					"GroupBy":        "GROUP BY i.id, i.name WITH ROLLUP",
					"Having":         "HAVING count(*) > ?",
					"OrderBy":        "ORDER BY i.id DESC",
					"Pagination":     "LIMIT ?, ?",
					"Locking":        "FOR UPDATE SKIP LOCKED",
					"Terminator":     ";",
				},
				ProjectionItems: []string{"i.id|i.id||false", "i.name AS label|i.name|AS label|false",
					"count(*) total|count(*)|total|false", "i.*|i.*||true"},
				Placeholders: []string{"?||1|", "?||2|NOT IN(?)", "?||3|", "?||4|", "?||5|"},
			},
		},
		{
			name: "server modifiers without a quantifier and a shared lock",
			sql:  "SELECT SQL_CALC_FOUND_ROWS id ident FROM items WHERE id = :id ORDER BY id LIMIT 10 OFFSET ? LOCK IN SHARE MODE",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Projection":     "id ident",
					"From":           "FROM items",
					"Where":          "WHERE id = :id",
					"WhereCondition": "id = :id",
					"OrderBy":        "ORDER BY id",
					"Pagination":     "LIMIT 10 OFFSET ?",
					"Locking":        "LOCK IN SHARE MODE",
				},
				ProjectionItems: []string{"id ident|id|ident|false"},
				Placeholders:    []string{":id|id|0|", "?||1|"},
			},
		},
		{
			name: "a compound query keeps only the clauses after its branches",
			sql:  "SELECT id FROM items WHERE id = ? UNION ALL SELECT id FROM items WHERE name = ? ORDER BY id LIMIT ?",
			expected: renderedSpans{
				Clauses:      map[string]string{"OrderBy": "ORDER BY id", "Pagination": "LIMIT ?"},
				Placeholders: []string{"?||1|", "?||2|", "?||3|"},
			},
		},
		{
			name: "a CTE body records nothing of its own",
			sql:  "WITH recent AS (SELECT id FROM items WHERE id > ? ORDER BY id LIMIT 5) SELECT r.* FROM recent r WHERE r.id IN (?)",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Projection":     "r.*",
					"From":           "FROM recent r",
					"Where":          "WHERE r.id IN (?)",
					"WhereCondition": "r.id IN (?)",
				},
				ProjectionItems: []string{"r.*|r.*||true"},
				Placeholders:    []string{"?||1|", "?||2|IN(?)"},
			},
		},
		{
			name: "an INSERT records its rows and its terminator",
			sql:  "INSERT INTO shop.items (id, name) VALUES (?, ?), (?, lower(?)) ON DUPLICATE KEY UPDATE name = VALUES(name);",
			expected: renderedSpans{
				Clauses:      map[string]string{"Terminator": ";"},
				Values:       []string{"(?, ?)=?;?", "(?, lower(?))=?;lower(?)"},
				Placeholders: []string{"?||1|", "?||2|", "?||3|", "?||4|"},
			},
		},
		{
			name: "a REPLACE records its RETURNING clause",
			sql:  "REPLACE INTO items (id, name) VALUES (?, ?) RETURNING id, name AS label",
			expected: renderedSpans{
				Clauses:        map[string]string{"Returning": "RETURNING id, name AS label"},
				ReturningItems: []string{"id|id||false", "name AS label|name|AS label|false"},
				Values:         []string{"(?, ?)=?;?"},
				Placeholders:   []string{"?||1|", "?||2|"},
			},
		},
		{
			name: "an INSERT ... SELECT records none of its SELECT's clauses",
			sql:  "INSERT INTO items (id, name) SELECT id, name FROM items WHERE id = ?",
			expected: renderedSpans{
				Clauses:      map[string]string{},
				Placeholders: []string{"?||1|"},
			},
		},
		{
			name: "a multi-table UPDATE records its WHERE clause alone",
			sql:  "UPDATE items i JOIN tags t ON t.item_id = i.id SET i.name = ? WHERE i.id = ? ORDER BY i.id LIMIT 1",
			expected: renderedSpans{
				Clauses:      map[string]string{"Where": "WHERE i.id = ?", "WhereCondition": "i.id = ?"},
				Placeholders: []string{"?||1|", "?||2|"},
			},
		},
		{
			name: "a DELETE locates the placeholders inside its subquery",
			sql:  "DELETE FROM items WHERE id IN (SELECT id FROM items WHERE name = ?) RETURNING id",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Where":          "WHERE id IN (SELECT id FROM items WHERE name = ?)",
					"WhereCondition": "id IN (SELECT id FROM items WHERE name = ?)",
					"Returning":      "RETURNING id",
				},
				ReturningItems: []string{"id|id||false"},
				Placeholders:   []string{"?||1|"},
			},
		},
		{
			name: "a multi-table DELETE records its WHERE clause",
			sql:  "DELETE i FROM items i JOIN tags t ON t.item_id = i.id WHERE t.tag = ?",
			expected: renderedSpans{
				Clauses:      map[string]string{"Where": "WHERE t.tag = ?", "WhereCondition": "t.tag = ?"},
				Placeholders: []string{"?||1|"},
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, statement := analyseStatement(t, testCase.sql)
			require.Equal(t, testCase.expected, renderSpans(testCase.sql, statement, analysis.Spans))
		})
	}
}

func TestStatementSpansRecordTheInsertTarget(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "INSERT INTO billing.invoices (id) VALUES (?)")

	require.Equal(t, ast.TableReference{Schema: "billing", Name: "invoices", Alias: ""}, analysis.InsertTarget)
	require.Equal(t, "invoices", analysis.InsertTable)
}

func describeClauseExpression(sql string, expression ast.Expression) string {
	switch typed := expression.(type) {
	case *ast.ColumnRefExpression:
		return "column " + sql[typed.Span.Start:typed.Span.End]
	case *ast.FunctionCallExpression:
		arguments := make([]string, 0, len(typed.Arguments))
		for _, argument := range typed.Arguments {
			arguments = append(arguments, describeClauseExpression(sql, argument))
		}
		return "call " + sql[typed.Span.Start:typed.Span.End] + "(" + strings.Join(arguments, ", ") + ")"
	case *ast.ComparisonExpression:
		return describeClauseExpression(sql, typed.Left) + " " + typed.Operator + " " + describeClauseExpression(sql, typed.Right)
	case *ast.LogicalOpExpression:
		operands := make([]string, 0, len(typed.Operands))
		for _, operand := range typed.Operands {
			operands = append(operands, describeClauseExpression(sql, operand))
		}
		return strings.Join(operands, " "+typed.Operator+" ")
	case *ast.InListExpression:
		return describeClauseExpression(sql, typed.Inner) + " in list"
	default:
		return fmt.Sprintf("%T", expression)
	}
}

func clauseExpressionsOf(sql string, analysis *ast.RawQueryAnalysis) []string {
	if len(analysis.ClauseExpressions) == 0 {
		return nil
	}
	described := make([]string, 0, len(analysis.ClauseExpressions))
	for _, clauseExpression := range analysis.ClauseExpressions {
		described = append(described, clauseExpression.Clause.String()+": "+describeClauseExpression(sql, clauseExpression.Expression))
	}
	return described
}

func TestClauseExpressions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
		nested   []string
	}{
		{
			name: "a SELECT reports its join, WHERE, GROUP BY, HAVING and ORDER BY expressions",
			sql: "SELECT i.id AS item_key FROM items i JOIN tags t ON t.item_id = i.id WHERE lower(i.name) = ? " +
				"GROUP BY i.id, i.kind WITH ROLLUP HAVING count(*) > 1 ORDER BY item_key DESC, i.id",
			expected: []string{
				"JOIN ON: column t.item_id = column i.id",
				"WHERE: call lower(column i.name) = *ast.ParameterExpression",
				"GROUP BY: column i.id",
				"GROUP BY: column i.kind",
				"HAVING: call count() > *ast.LiteralExpression",
				"ORDER BY: column item_key",
				"ORDER BY: column i.id",
			},
		},
		{
			name: "a multi-table UPDATE reports its qualified SET targets, values, WHERE and ORDER BY",
			sql:  "UPDATE items i JOIN tags t ON t.item_id = i.id SET i.name = upper(t.label), (kind, rank) = (?, rank + 1) WHERE shop.is_live(i.id) ORDER BY i.id",
			expected: []string{
				"JOIN ON: column t.item_id = column i.id",
				"SET target: column i.name",
				"SET value: call upper(column t.label)",
				"SET target: column kind",
				"SET target: column rank",
				"SET value: *ast.ParameterExpression",
				"SET value: *ast.BinaryOpExpression",
				"WHERE: call shop.is_live(column i.id)",
				"ORDER BY: column i.id",
			},
		},
		{
			name:     "ON DUPLICATE KEY UPDATE naming the inserted row reports only the VALUES row's values",
			sql:      "INSERT INTO items (id, name) VALUES (?, ?) AS new ON DUPLICATE KEY UPDATE name = new.name, qty = VALUES(qty) + 1",
			expected: []string{"VALUES row value: *ast.ParameterExpression", "VALUES row value: *ast.ParameterExpression"},
		},
		{
			name:     "an INSERT ... SET reports nothing",
			sql:      "INSERT INTO items SET id = ?, name = lower(?)",
			expected: nil,
		},
		{
			name:     "a VALUES statement reports nothing",
			sql:      "VALUES ROW(1, 2) ORDER BY column_0",
			expected: nil,
		},
		{
			name:     "a compound query reports its ORDER BY on the statement and each branch's WHERE on the branch",
			sql:      "SELECT id FROM items WHERE id = 1 UNION SELECT id FROM tags WHERE tag_id = 2 ORDER BY id",
			expected: []string{"WHERE: column id = *ast.LiteralExpression", "ORDER BY: column id"},
			nested:   []string{"WHERE: column tag_id = *ast.LiteralExpression"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)
			require.Equal(t, testCase.expected, clauseExpressionsOf(testCase.sql, analysis))
			if testCase.nested != nil {
				require.Len(t, analysis.CompoundBranches, 1)
				require.Equal(t, testCase.nested, clauseExpressionsOf(testCase.sql, analysis.CompoundBranches[0].Query))
			}
		})
	}
}

func TestClauseExpressionsOfNestedQueriesStayOnThem(t *testing.T) {
	t.Parallel()

	sql := "WITH recent AS (SELECT a.id FROM items a, items c JOIN tags b ON b.id = c.id WHERE a.id > ?) " +
		"SELECT r.id FROM recent r WHERE EXISTS (SELECT 1 FROM tags x WHERE x.item_id = r.id)"
	analysis, _ := analyseStatement(t, sql)

	require.Len(t, analysis.CTEDefinitions, 1)
	definition := analysis.CTEDefinitions[0]
	require.Equal(t, []string{"a", "c", "b"}, definition.FromOrder)
	require.NotNil(t, definition.Query)
	require.Equal(t, []string{"JOIN ON: column b.id = column c.id", "WHERE: column a.id > *ast.ParameterExpression"},
		clauseExpressionsOf(sql, definition.Query))
	require.Equal(t, []string{"WHERE: *ast.ExistsExpression"}, clauseExpressionsOf(sql, analysis))
	require.Len(t, analysis.PredicateSubqueries, 1)
	require.Equal(t, []string{"WHERE: column x.item_id = column r.id"}, clauseExpressionsOf(sql, analysis.PredicateSubqueries[0]))
}

func TestImplicitColumnAliases(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "SELECT id ident, count(*) total, lower(name) label FROM items")

	require.Len(t, analysis.OutputColumns, 3)
	require.Equal(t, "ident", analysis.OutputColumns[0].Name)
	require.Equal(t, "total", analysis.OutputColumns[1].Name)
	require.Equal(t, "label", analysis.OutputColumns[2].Name)
}

func TestOrderByPlace(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		sql    string
		before string
	}{
		{name: "a SELECT without ORDER BY", sql: "SELECT id FROM items WHERE id = ?", before: "SELECT id FROM items WHERE id = ?"},
		{name: "a SELECT with ORDER BY", sql: "SELECT id FROM items WHERE id = ? ORDER BY id", before: "SELECT id FROM items WHERE id = ?"},
		{name: "before pagination and locking", sql: "SELECT id FROM items LIMIT 1 FOR UPDATE", before: "SELECT id FROM items"},
		{
			name:   "after a named window",
			sql:    "SELECT id, rank() OVER w FROM items WINDOW w AS (ORDER BY id) LIMIT 1",
			before: "SELECT id, rank() OVER w FROM items WINDOW w AS (ORDER BY id)",
		},
		{
			name:   "after HAVING",
			sql:    "SELECT name, count(*) FROM items GROUP BY name HAVING count(*) > ? ORDER BY name",
			before: "SELECT name, count(*) FROM items GROUP BY name HAVING count(*) > ?",
		},
		{
			name:   "after the last branch of a compound query",
			sql:    "SELECT id FROM items UNION SELECT id FROM items WHERE id > 1 LIMIT 5",
			before: "SELECT id FROM items UNION SELECT id FROM items WHERE id > 1",
		},
		{name: "a SELECT with a terminator", sql: "SELECT id FROM items ;", before: "SELECT id FROM items"},
		{
			name:   "a SELECT after a CTE",
			sql:    "WITH c AS (SELECT id FROM items ORDER BY id LIMIT 1) SELECT id FROM c",
			before: "WITH c AS (SELECT id FROM items ORDER BY id LIMIT 1) SELECT id FROM c",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, statement := analyseStatement(t, testCase.sql)

			place := analysis.Spans.OrderByPlace
			require.Equal(t, place.Start, place.End)
			require.Equal(t, testCase.before, spanTextOf(testCase.sql, statement, ast.Span{Start: 0, End: place.Start}))
		})
	}
}

func TestOrderByPlaceIsLeftOutOfRowChanges(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"UPDATE items SET name = ? WHERE id = ? ORDER BY id LIMIT 1",
		"DELETE FROM items WHERE id = ? ORDER BY id LIMIT 1",
		"INSERT INTO items (id) SELECT id FROM items ORDER BY id",
	} {
		analysis, _ := analyseStatement(t, sql)

		require.Equal(t, ast.Span{}, analysis.Spans.OrderByPlace, sql)
	}
}

func TestReparseLeavesPanicsAndInterruptsToTheEntryPoint(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		interrupt func() error
		wantError error
		name      string
		wantLogs  int
	}{
		{name: "an interrupt", interrupt: func() error { return errWalkStopped }, wantError: errWalkStopped, wantLogs: 0},
		{name: "a panic", interrupt: func() error { panic("parser bug") }, wantError: fault.ErrPanic, wantLogs: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise("a, b DESC")
			require.NoError(t, err)
			p := newParser(tokens)
			p.Interrupt = testCase.interrupt
			ctx, logs := newLoggingContext(t)

			err = walkAtAnEntryPoint(ctx, func() { p.reparse(0, len(tokens), true, false) })

			require.ErrorIs(t, err, testCase.wantError)
			require.Len(t, decodeLogRecords(t, logs), testCase.wantLogs)
		})
	}
}

func walkAtAnEntryPoint(ctx context.Context, walk func()) (err error) {
	defer func() { dialectkit.Recover[any](ctx, "mysql: probe", recover(), nil, &err) }()
	walk()
	return nil
}
