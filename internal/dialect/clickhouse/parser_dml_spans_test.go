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

package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
)

type renderedSpans struct {
	Clauses         map[string]string
	RowShaping      []string
	ProjectionItems []string
	Values          []string
	Placeholders    []string
}

func withoutSpans(expression ast.Expression) ast.Expression {
	for node := range ast.Preorder(expression) {
		switch typed := node.(type) {
		case *ast.ColumnRefExpression:
			typed.Span = ast.Span{}
		case *ast.FunctionCallExpression:
			typed.Span = ast.Span{}
		}
	}
	return expression
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
	for _, span := range spans.RowShaping {
		rendered.RowShaping = append(rendered.RowShaping, spanTextOf(sql, statement, span))
	}
	for _, item := range spans.ProjectionItems {
		rendered.ProjectionItems = append(rendered.ProjectionItems, fmt.Sprintf("%s|%s|%s|%t", spanTextOf(sql, statement, item.Span),
			spanTextOf(sql, statement, item.Expression), spanTextOf(sql, statement, item.Alias), item.Star))
	}
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
		rendered.Placeholders = append(rendered.Placeholders, fmt.Sprintf("%s|%s|%s|%s",
			spanTextOf(sql, statement, placeholder.Span), placeholder.Name, spanTextOf(sql, statement, placeholder.Type), list))
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
			name: "a SELECT with every ClickHouse clause",
			sql: "-- leading comment\nSELECT DISTINCT id, name AS label, count() total FROM items FINAL SAMPLE 0.1 ARRAY JOIN tags AS tag " +
				"JOIN owners o ON o.id = items.owner_id PREWHERE id > {min:UInt64} WHERE name = {name:String} " +
				"GROUP BY id, name WITH TOTALS HAVING count() > 1 QUALIFY total > 0 ORDER BY id WITH FILL " +
				"LIMIT 1 BY id LIMIT {limit:UInt64} SETTINGS max_threads = 1 FORMAT JSON ;",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Quantifier": "DISTINCT",
					"Projection": "id, name AS label, count() total",
					"From": "FROM items FINAL SAMPLE 0.1 ARRAY JOIN tags AS tag JOIN owners o ON o.id = items.owner_id " +
						"PREWHERE id > {min:UInt64}",
					"Where":          "WHERE name = {name:String}",
					"WhereCondition": "name = {name:String}",
					"GroupBy":        "GROUP BY id, name WITH TOTALS",
					"Having":         "HAVING count() > 1",
					"OrderBy":        "ORDER BY id WITH FILL",
					"Pagination":     "LIMIT {limit:UInt64}",
					"Trailing":       "SETTINGS max_threads = 1 FORMAT JSON",
					"Terminator":     ";",
				},
				RowShaping: []string{"QUALIFY total > 0", "LIMIT 1 BY id"},
				ProjectionItems: []string{"id|id||false", "name AS label|name|AS label|false",
					"count() total|count()|total|false"},
				Placeholders: []string{"{min:UInt64}|min|UInt64|", "{name:String}|name|String|", "{limit:UInt64}|limit|UInt64|"},
			},
		},
		{
			name: "LIMIT m, n and OFFSET are pagination",
			sql:  "SELECT t.* FROM items AS t WHERE id NOT IN ({ids:Array(UInt64)}) LIMIT 5, 10",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Projection":     "t.*",
					"From":           "FROM items AS t",
					"Where":          "WHERE id NOT IN ({ids:Array(UInt64)})",
					"WhereCondition": "id NOT IN ({ids:Array(UInt64)})",
					"Pagination":     "LIMIT 5, 10",
				},
				ProjectionItems: []string{"t.*|t.*||true"},
				Placeholders:    []string{"{ids:Array(UInt64)}|ids|Array(UInt64)|NOT IN({ids:Array(UInt64)})"},
			},
		},
		{
			name: "a compound query keeps the clauses that close its last arm",
			sql:  "SELECT id FROM items WHERE id = {a:UInt64} UNION ALL SELECT id FROM items ORDER BY id LIMIT 5 SETTINGS max_threads = 2",
			expected: renderedSpans{
				Clauses:      map[string]string{"OrderBy": "ORDER BY id", "Pagination": "LIMIT 5", "Trailing": "SETTINGS max_threads = 2"},
				Placeholders: []string{"{a:UInt64}|a|UInt64|"},
			},
		},
		{
			name: "a CTE body records nothing of its own",
			sql:  "WITH recent AS (SELECT id FROM items WHERE id > {min:UInt64} ORDER BY id LIMIT 5) SELECT r.id FROM recent r",
			expected: renderedSpans{
				Clauses:         map[string]string{"Projection": "r.id", "From": "FROM recent r"},
				ProjectionItems: []string{"r.id|r.id||false"},
				Placeholders:    []string{"{min:UInt64}|min|UInt64|"},
			},
		},
		{
			name: "an INSERT records its rows",
			sql:  "INSERT INTO default.items (id, name) VALUES ({a:UInt64}, {b:String}), (2, lower({c:String}))",
			expected: renderedSpans{
				Clauses:      map[string]string{},
				Values:       []string{"({a:UInt64}, {b:String})={a:UInt64};{b:String}", "(2, lower({c:String}))=2;lower({c:String})"},
				Placeholders: []string{"{a:UInt64}|a|UInt64|", "{b:String}|b|String|", "{c:String}|c|String|"},
			},
		},
		{
			name: "an INSERT row keeps the commas of its map literals inside one item",
			sql:  "INSERT INTO items (id, tags, nested) VALUES ({a:UInt64}, {'k': 1, 'j': 2}, [{'x': 1}, {}])",
			expected: renderedSpans{
				Clauses:      map[string]string{},
				Values:       []string{"({a:UInt64}, {'k': 1, 'j': 2}, [{'x': 1}, {}])={a:UInt64};{'k': 1, 'j': 2};[{'x': 1}, {}]"},
				Placeholders: []string{"{a:UInt64}|a|UInt64|"},
			},
		},
		{
			name: "an ALTER TABLE UPDATE records its WHERE clause",
			sql:  "ALTER TABLE items UPDATE name = upper(name) WHERE id = {id:UInt64} SETTINGS mutations_sync = 1",
			expected: renderedSpans{
				Clauses:      map[string]string{"Where": "WHERE id = {id:UInt64}", "WhereCondition": "id = {id:UInt64}"},
				Placeholders: []string{"{id:UInt64}|id|UInt64|"},
			},
		},
		{
			name: "a lightweight DELETE records its WHERE and RETURNING clauses",
			sql:  "DELETE FROM items WHERE id = {id:UInt64} RETURNING id",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Where":          "WHERE id = {id:UInt64}",
					"WhereCondition": "id = {id:UInt64}",
					"Returning":      "RETURNING id",
				},
				Placeholders: []string{"{id:UInt64}|id|UInt64|"},
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

	analysis, _ := analyseStatement(t, "INSERT INTO billing.invoices (id) VALUES ({id:UInt64})")

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
	case *ast.LambdaExpression:
		return "lambda " + strings.Join(typed.Parameters, ",") + " -> " + describeClauseExpression(sql, typed.Body)
	case *ast.StructFieldAccessExpression:
		return describeClauseExpression(sql, typed.Struct) + " field " + typed.FieldName
	case *ast.BinaryOpExpression:
		return describeClauseExpression(sql, typed.Left) + " " + typed.Operator + " " + describeClauseExpression(sql, typed.Right)
	case *ast.InListExpression:
		return describeClauseExpression(sql, typed.Inner) + " in list"
	default:
		return fmt.Sprintf("%T", expression)
	}
}

func clauseExpressionsOf(sql string, analysis *ast.RawQueryAnalysis) []string {
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
	}{
		{
			name: "a SELECT reports every clause outside its select list",
			sql: "SELECT i.id AS key FROM items i JOIN tags t ON t.item_id = i.id PREWHERE i.kind = 1 " +
				"WHERE arrayExists(x -> x > {min:UInt32}, i.scores) AND basket.fish = 'a' AND sumIf(i.a, i.b) > 0 " +
				"GROUP BY i.id, key WITH ROLLUP HAVING count() > 1 QUALIFY key > 0 ORDER BY key DESC LIMIT 1 BY i.kind, i.id",
			expected: []string{
				"JOIN ON: column t.item_id = column i.id",
				"WHERE: column i.kind = *ast.LiteralExpression",
				"WHERE: call arrayExists(lambda x -> column x > *ast.CastExpression, column i.scores) AND " +
					"column basket.fish = *ast.LiteralExpression AND call sumIf(column i.a, column i.b) > *ast.LiteralExpression",
				"GROUP BY: column i.id",
				"GROUP BY: column key",
				"HAVING: call count() > *ast.LiteralExpression",
				"row shaping: column key > *ast.LiteralExpression",
				"ORDER BY: column key",
				"row shaping: column i.kind",
				"row shaping: column i.id",
			},
		},
		{
			name: "a lightweight UPDATE reports its assignments up to IN PARTITION and its WHERE condition",
			sql:  "UPDATE items SET name = upper(name), qty = qty + 1 IN PARTITION 1 WHERE id = {id:UInt64}",
			expected: []string{
				"SET target: column name",
				"SET value: call upper(column name)",
				"SET target: column qty",
				"SET value: column qty + *ast.LiteralExpression",
				"WHERE: column id = *ast.CastExpression",
			},
		},
		{
			name: "an ALTER TABLE UPDATE reports its assignments and WHERE condition",
			sql:  "ALTER TABLE db.items UPDATE name = lower(name) WHERE id IN ({ids:Array(UInt64)})",
			expected: []string{
				"SET target: column name",
				"SET value: call lower(column name)",
				"WHERE: column id in list",
			},
		},
		{
			name:     "a WITH expression alias is a value, not a column",
			sql:      "WITH now() - 7 AS bound, '2019-08-01' AS day SELECT id FROM items WHERE created > bound AND day < created",
			expected: []string{"WHERE: column created > *ast.UnknownExpression AND *ast.UnknownExpression < column created"},
		},
		{
			name:     "an INSERT ... VALUES reports its row's values",
			sql:      "INSERT INTO items (id) VALUES ({id:UInt64})",
			expected: []string{"VALUES row value: *ast.CastExpression"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)
			require.Equal(t, testCase.expected, clauseExpressionsOf(testCase.sql, analysis))
		})
	}
}

func TestClauseExpressionsOfNestedQueriesStayOnThem(t *testing.T) {
	t.Parallel()

	sql := "WITH 5 AS limit_value, recent AS (SELECT a.id FROM items a JOIN tags b ON b.id = a.id WHERE a.id > limit_value) " +
		"SELECT r.id FROM recent r UNION ALL SELECT id FROM items WHERE id IN (SELECT id FROM tags WHERE n > limit_value) ORDER BY id"
	analysis, _ := analyseStatement(t, sql)

	require.Len(t, analysis.CTEDefinitions, 1)
	definition := analysis.CTEDefinitions[0]
	require.Equal(t, []string{"a", "b"}, definition.FromOrder)
	require.NotNil(t, definition.Query)
	require.Equal(t, []string{"JOIN ON: column b.id = column a.id", "WHERE: column a.id > *ast.UnknownExpression"},
		clauseExpressionsOf(sql, definition.Query))
	require.Empty(t, analysis.ClauseExpressions)
	require.Len(t, analysis.CompoundBranches, 1)
	branch := analysis.CompoundBranches[0].Query
	require.Equal(t, []string{"WHERE: column id in list", "ORDER BY: column id"}, clauseExpressionsOf(sql, branch))
	list, ok := branch.ClauseExpressions[0].Expression.(*ast.InListExpression)
	require.True(t, ok)
	subquery, ok := list.Values[0].(*ast.ScalarSubqueryExpression)
	require.True(t, ok)
	require.Equal(t, []string{"WHERE: column n > *ast.UnknownExpression"}, clauseExpressionsOf(sql, subquery.InnerQuery))
}

func TestLimitByThenLimitKeepsThePaginationParameter(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "SELECT id FROM items ORDER BY id LIMIT 1 BY kind LIMIT {page_size:UInt32}")

	require.Len(t, analysis.ParameterReferences, 1)
	require.Equal(t, sqlmodel.ParameterContextLimit, analysis.ParameterReferences[0].Context)
}

func TestQualifyStopsAtOrderBy(t *testing.T) {
	t.Parallel()

	analysis, statement := analyseStatement(t, "SELECT id FROM items QUALIFY id > 1 ORDER BY id")

	require.Equal(t, []string{"QUALIFY id > 1"}, renderSpans("SELECT id FROM items QUALIFY id > 1 ORDER BY id", statement, analysis.Spans).RowShaping)
}

func TestWithExpressionAliasesParse(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"WITH '2019-08-01 15:23:00' AS bound SELECT id FROM items WHERE created < bound",
		"WITH (SELECT max(id) FROM items) AS top, id AS ident SELECT ident FROM items WHERE id < top",
	} {
		analysis, _ := analyseStatement(t, sql)
		require.Len(t, analysis.OutputColumns, 1, sql)
	}
}

func TestOrderByColumnsSplitAtCommas(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "SELECT cat, ts FROM t ORDER BY cat, ts DESC WITH FILL FROM 1 TO 10, id LIMIT 3")

	require.Len(t, analysis.OrderByColumns, 3)
	require.Equal(t, "cat", analysis.OrderByColumns[0].Expression)
	require.Equal(t, ast.OrderDirectionUnspecified, analysis.OrderByColumns[0].Direction)
	require.Equal(t, "ts", analysis.OrderByColumns[1].Expression)
	require.Equal(t, ast.OrderDirectionDesc, analysis.OrderByColumns[1].Direction)
	require.Equal(t, "10", analysis.OrderByColumns[1].FillTo)
	require.Equal(t, "id", analysis.OrderByColumns[2].Expression)
	require.Equal(t, []string{"ORDER BY: column cat", "ORDER BY: column ts", "ORDER BY: column id"},
		clauseExpressionsOf("SELECT cat, ts FROM t ORDER BY cat, ts DESC WITH FILL FROM 1 TO 10, id LIMIT 3", analysis))
}

func reparseAtEntryPoint(ctx context.Context, statementParser *parser) (expressions []ast.Expression, err error) {
	defer func() { dialectkit.Recover(ctx, "clickhouse: analyse query", recover(), &expressions, &err) }()
	return statementParser.reparseList(0, len(statementParser.Tokens)), nil
}

func TestReparseReadsOneExpressionOrAWholeList(t *testing.T) {
	t.Parallel()

	const wholeStatement = -1
	testCases := []struct {
		reparse   func(statementParser *parser, first, end int) []ast.Expression
		name      string
		wantFirst string
		first     int
		end       int
		want      int
	}{
		{
			name: "an expression stops at the first comma", reparse: (*parser).reparseExpression,
			first: 0, end: wholeStatement, want: 1, wantFirst: "a",
		},
		{
			name: "a list reads every comma-separated expression", reparse: (*parser).reparseList,
			first: 0, end: wholeStatement, want: 3, wantFirst: "a",
		},
		{
			name: "an expression starts where the range starts", reparse: (*parser).reparseExpression,
			first: 6, end: wholeStatement, want: 1, wantFirst: "c",
		},
		{name: "an empty range holds no expression", reparse: (*parser).reparseExpression, first: 2, end: 2, want: 0},
		{name: "an empty range holds no list", reparse: (*parser).reparseList, first: 2, end: 2, want: 0},
		{name: "a range past the tokens holds no list", reparse: (*parser).reparseList, first: 0, end: 1_000, want: 0},
		{name: "a negative start holds no expression", reparse: (*parser).reparseExpression, first: -1, end: 2, want: 0},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statementParser := newParser(tokeniseStatement(t, "a, b + 1, c"))
			end := testCase.end
			if end == wholeStatement {
				end = len(statementParser.Tokens)
			}

			expressions := testCase.reparse(statementParser, testCase.first, end)
			require.Len(t, expressions, testCase.want)
			if testCase.want > 0 {
				column, ok := expressions[0].(*ast.ColumnRefExpression)
				require.True(t, ok, "first expression is %T", expressions[0])
				assert.Equal(t, testCase.wantFirst, column.ColumnName)
			}
		})
	}
}

func TestReparseLeavesAStoppedWalkToTheEntryPoint(t *testing.T) {
	t.Parallel()

	errStopped := errors.New("walk stopped")
	cases := []struct {
		interrupt   func() error
		wantErr     error
		name        string
		expressions int
		logged      int
	}{
		{name: "a walk that runs to the end", interrupt: nil, wantErr: nil, expressions: 3, logged: 0},
		{
			name:        "an interrupted walk",
			interrupt:   func() error { return errStopped },
			wantErr:     errStopped,
			expressions: 0,
			logged:      0,
		},
		{
			name:        "a parser bug",
			interrupt:   func() error { panic("parser bug") },
			wantErr:     fault.ErrPanic,
			expressions: 0,
			logged:      1,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, logs := newLoggingContext(t)
			statementParser := newParser(tokeniseStatement(t, "a, b + 1, c"))
			statementParser.Interrupt = testCase.interrupt

			expressions, err := reparseAtEntryPoint(ctx, statementParser)
			require.ErrorIs(t, err, testCase.wantErr)
			assert.Len(t, expressions, testCase.expressions)
			assert.Len(t, decodeLogRecords(t, logs), testCase.logged)
		})
	}
}
