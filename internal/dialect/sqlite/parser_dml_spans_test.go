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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
)

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
			name: "a SELECT with every clause and every named and numbered placeholder form",
			sql: "-- leading comment\nSELECT DISTINCT i.id, i.name AS label, count(*) total, i.* FROM items i " +
				"JOIN items j ON j.id = i.id WHERE i.id > ?1 AND i.name NOT IN (?2) GROUP BY i.id, i.name " +
				"HAVING count(*) > :minimum ORDER BY i.id DESC NULLS LAST LIMIT @size OFFSET $skip ;",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Quantifier":     "DISTINCT",
					"Projection":     "i.id, i.name AS label, count(*) total, i.*",
					"From":           "FROM items i JOIN items j ON j.id = i.id",
					"Where":          "WHERE i.id > ?1 AND i.name NOT IN (?2)",
					"WhereCondition": "i.id > ?1 AND i.name NOT IN (?2)",
					"GroupBy":        "GROUP BY i.id, i.name",
					"Having":         "HAVING count(*) > :minimum",
					"OrderBy":        "ORDER BY i.id DESC NULLS LAST",
					"Pagination":     "LIMIT @size OFFSET $skip",
					"Terminator":     ";",
				},
				ProjectionItems: []string{"i.id|i.id||false", "i.name AS label|i.name|AS label|false",
					"count(*) total|count(*)|total|false", "i.*|i.*||true"},
				Placeholders: []string{"?1||1|", "?2||2|NOT IN(?2)", ":minimum|minimum|0|", "@size|size|0|", "$skip|skip|0|"},
			},
		},
		{
			name: "a compound query keeps only the clauses after its branches, with LIMIT a, b",
			sql:  "SELECT id FROM items WHERE id = ?1 UNION ALL SELECT id FROM items WHERE name = ?2 ORDER BY id LIMIT ?3, ?4",
			expected: renderedSpans{
				Clauses:      map[string]string{"OrderBy": "ORDER BY id", "Pagination": "LIMIT ?3, ?4"},
				Placeholders: []string{"?1||1|", "?2||2|", "?3||3|", "?4||4|"},
			},
		},
		{
			name: "a CTE body records nothing of its own",
			sql:  "WITH recent AS (SELECT id FROM items WHERE id > ?1 ORDER BY id LIMIT 5) SELECT r.* FROM recent r WHERE r.id IN (?2)",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Projection":     "r.*",
					"From":           "FROM recent r",
					"Where":          "WHERE r.id IN (?2)",
					"WhereCondition": "r.id IN (?2)",
				},
				ProjectionItems: []string{"r.*|r.*||true"},
				Placeholders:    []string{"?1||1|", "?2||2|IN(?2)"},
			},
		},
		{
			name: "an INSERT records its rows, its RETURNING clause and its terminator",
			sql: "INSERT INTO main.items AS t (id, name) VALUES (?1, ?2), (?3, lower(?4)) ON CONFLICT (id) DO UPDATE " +
				"SET name = excluded.name WHERE t.id > 0 RETURNING id, name AS label;",
			expected: renderedSpans{
				Clauses:        map[string]string{"Returning": "RETURNING id, name AS label", "Terminator": ";"},
				ReturningItems: []string{"id|id||false", "name AS label|name|AS label|false"},
				Values:         []string{"(?1, ?2)=?1;?2", "(?3, lower(?4))=?3;lower(?4)"},
				Placeholders:   []string{"?1||1|", "?2||2|", "?3||3|", "?4||4|"},
			},
		},
		{
			name: "an INSERT ... SELECT records none of its SELECT's clauses",
			sql:  "INSERT OR REPLACE INTO items (id, name) SELECT id, name FROM items WHERE id = :id",
			expected: renderedSpans{
				Clauses:      map[string]string{},
				Placeholders: []string{":id|id|0|"},
			},
		},
		{
			name: "an UPDATE records its WHERE and RETURNING clauses",
			sql:  "UPDATE items SET name = ?1 FROM other o WHERE o.id = items.id AND items.id = ?2 RETURNING *",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Where":          "WHERE o.id = items.id AND items.id = ?2",
					"WhereCondition": "o.id = items.id AND items.id = ?2",
					"Returning":      "RETURNING *",
				},
				ReturningItems: []string{"*|*||true"},
				Placeholders:   []string{"?1||1|", "?2||2|"},
			},
		},
		{
			name: "a DELETE locates the placeholders inside its subquery",
			sql:  "DELETE FROM items WHERE id IN (SELECT id FROM items WHERE name = ?1) RETURNING id",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Where":          "WHERE id IN (SELECT id FROM items WHERE name = ?1)",
					"WhereCondition": "id IN (SELECT id FROM items WHERE name = ?1)",
					"Returning":      "RETURNING id",
				},
				ReturningItems: []string{"id|id||false"},
				Placeholders:   []string{"?1||1|"},
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

func TestStatementSpansLocateAnonymousPlaceholders(t *testing.T) {
	t.Parallel()

	sql := "SELECT id FROM items WHERE id = ? AND name IN (?)"
	analysis, statement := analyseStatement(t, sql)

	require.Len(t, analysis.Spans.Placeholders, 2)
	require.Equal(t, "?", spanTextOf(sql, statement, analysis.Spans.Placeholders[0].Span))
	require.Equal(t, "IN", spanTextOf(sql, statement, analysis.Spans.Placeholders[1].List.Operator))
	require.Equal(t, "(?)", spanTextOf(sql, statement, analysis.Spans.Placeholders[1].List.List))
}

func TestStatementSpansRecordTheOrderByPlace(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantBefore string
	}{
		{name: "before a written ORDER BY", sql: "SELECT id FROM items WHERE id = ? ORDER BY id LIMIT 1", wantBefore: "SELECT id FROM items WHERE id = ?"},
		{name: "after the last clause", sql: "SELECT id FROM items GROUP BY id HAVING count(*) > 1", wantBefore: "SELECT id FROM items GROUP BY id HAVING count(*) > 1"},
		{name: "before the pagination", sql: "SELECT id FROM items  LIMIT ? OFFSET ?", wantBefore: "SELECT id FROM items"},
		{name: "after the last compound branch", sql: "SELECT id FROM items UNION SELECT id FROM items WHERE id > 1 LIMIT 2", wantBefore: "SELECT id FROM items UNION SELECT id FROM items WHERE id > 1"},
		{name: "before the terminator", sql: "SELECT id FROM items ;", wantBefore: "SELECT id FROM items"},
		{
			name:       "after the WINDOW clause",
			sql:        "SELECT row_number() OVER w FROM items WINDOW w AS (ORDER BY id) LIMIT 1",
			wantBefore: "SELECT row_number() OVER w FROM items WINDOW w AS (ORDER BY id)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, statement := analyseStatement(t, testCase.sql)

			place := analysis.Spans.OrderByPlace
			require.Equal(t, place.Start, place.End)
			require.Equal(t, testCase.wantBefore, testCase.sql[statement.Location:statement.Location+place.Start])
		})
	}
}

func TestStatementSpansRecordNoOrderByPlaceOutsideQueries(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"INSERT INTO items (id) SELECT id FROM items ORDER BY id",
		"UPDATE items SET name = ? WHERE id = ?",
		"DELETE FROM items WHERE id = ?",
	} {
		analysis, _ := analyseStatement(t, sql)

		require.Equal(t, ast.Span{}, analysis.Spans.OrderByPlace, sql)
	}
}

func TestStatementSpansRecordTheInsertTarget(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "INSERT INTO main.invoices AS i (id) VALUES (?1)")

	require.Equal(t, ast.TableReference{Schema: "main", Name: "invoices", Alias: "i"}, analysis.InsertTarget)
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
			sql: "SELECT i.id AS key FROM items i JOIN tags t ON t.item_id = i.id WHERE lower(i.name) = ?1 " +
				"GROUP BY i.id, lower(i.kind) HAVING count(*) > 1 ORDER BY key DESC NULLS LAST, i.id COLLATE NOCASE",
			expected: []string{
				"JOIN ON: column t.item_id = column i.id",
				"WHERE: call lower(column i.name) = *ast.ParameterExpression",
				"GROUP BY: column i.id",
				"GROUP BY: call lower(column i.kind)",
				"HAVING: call count() > *ast.LiteralExpression",
				"ORDER BY: column key",
				"ORDER BY: column i.id",
			},
		},
		{
			name: "an UPDATE reports its SET targets and values, its join and its WHERE condition",
			sql:  "UPDATE items SET name = upper(label), (kind, rank) = (?1, rank + 1) FROM other o WHERE o.id = items.id",
			expected: []string{
				"SET target: column name",
				"SET value: call upper(column label)",
				"SET target: column kind",
				"SET target: column rank",
				"SET value: *ast.ParameterExpression",
				"SET value: *ast.BinaryOpExpression",
				"WHERE: column o.id = column items.id",
			},
		},
		{
			name:     "an upsert action naming the excluded row reports only the VALUES row's values",
			sql:      "INSERT INTO items (id, name) VALUES (?1, ?2) ON CONFLICT (id) DO UPDATE SET name = excluded.name WHERE items.id > 0",
			expected: []string{"VALUES row value: *ast.ParameterExpression", "VALUES row value: *ast.ParameterExpression"},
		},
		{
			name:     "a VALUES list reports nothing",
			sql:      "VALUES (1, 2), (3, 4)",
			expected: nil,
		},
		{
			name:     "a DELETE reports its WHERE and ORDER BY expressions",
			sql:      "DELETE FROM items WHERE missing(id) ORDER BY rank",
			expected: []string{"WHERE: call missing(column id)", "ORDER BY: column rank"},
		},
		{
			name:     "a compound query reports its ORDER BY on the statement and each branch's WHERE on the branch",
			sql:      "SELECT id FROM items WHERE id = 1 UNION SELECT id FROM tags WHERE tag_id = 2 ORDER BY id",
			expected: []string{"WHERE: column id = *ast.LiteralExpression", "ORDER BY: column id"},
			nested:   []string{"WHERE: column tag_id = *ast.LiteralExpression"},
		},
		{
			name:     "the current time keywords and COLLATE names are not columns",
			sql:      "SELECT id FROM items WHERE created < CURRENT_TIMESTAMP AND name = ?1 COLLATE NOCASE",
			expected: []string{"WHERE: column created < *ast.LiteralExpression AND column name = *ast.ParameterExpression"},
		},
		{
			name:     "IS NOT DISTINCT FROM keeps the WHERE clause whole",
			sql:      "SELECT id FROM items WHERE kind IS NOT DISTINCT FROM ?1 ORDER BY id",
			expected: []string{"WHERE: column kind IS NOT *ast.ParameterExpression", "ORDER BY: column id"},
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

	sql := "WITH recent AS (SELECT a.id FROM items a, items c JOIN tags b ON b.id = c.id WHERE a.id > ?1) " +
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

func TestInListSubqueryInAnExpression(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "SELECT CASE WHEN id IN (SELECT item_id FROM tags WHERE tag = ?1) THEN 1 ELSE 0 END AS tagged FROM items")

	require.Len(t, analysis.OutputColumns, 1)
	require.Equal(t, "tagged", analysis.OutputColumns[0].Name)
	require.Len(t, analysis.ParameterReferences, 1)
	caseExpression, ok := analysis.OutputColumns[0].Expression.(*ast.CaseWhenExpression)
	require.True(t, ok)
	inList, ok := caseExpression.Branches[0].Condition.(*ast.InListExpression)
	require.True(t, ok)
	require.Len(t, inList.Values, 1)
	require.IsType(t, &ast.ScalarSubqueryExpression{}, inList.Values[0])
}

func TestCurrentTimeKeywordsAreText(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "SELECT CURRENT_TIMESTAMP AS now FROM items")

	require.Len(t, analysis.OutputColumns, 1)
	require.Equal(t, &ast.LiteralExpression{TypeName: literalTypeText}, analysis.OutputColumns[0].Expression)
}

func TestDistinctFromDoesNotEndTheWhereClause(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "SELECT id FROM items WHERE kind IS DISTINCT FROM ?1 AND id = ?2")

	require.Len(t, analysis.ParameterReferences, 2)
	require.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[1].Context)
	require.Equal(t, "id", analysis.ParameterReferences[1].ColumnReference.ColumnName)
}

func TestReparseLetsAPanicReachTheEntryPoint(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		configure     func(p *parser)
		name          string
		sql           string
		wantInterrupt bool
	}{
		{
			name: "an interrupt that stops the walk",
			sql:  "a + b",
			configure: func(p *parser) {
				p.Interrupt = func() error { return errAnalysisAbandoned }
			},
			wantInterrupt: true,
		},
		{
			name: "a type hook that panics",
			sql:  "CAST(a AS BOOLEAN)",
			configure: func(p *parser) {
				p.types = newTypeResolver([]func(string, []int) *sqlmodel.SQLType{panickingTypeHook}, nil)
			},
			wantInterrupt: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens := splitTokens(t, testCase.sql)[0]
			p := newParser(tokens)
			testCase.configure(p)

			recovered := recoveredFrom(func() { p.reparse(0, len(tokens), false, true) })

			require.NotNil(t, recovered)
			require.Equal(t, testCase.wantInterrupt, dialectkit.IsInterrupt(recovered))
		})
	}
}

func recoveredFrom(run func()) (recovered any) {
	defer func() { recovered = recover() }()
	run()
	return nil
}

func TestAnalyseQueryFailsWhenAClauseOnlyReparseReadsPanics(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "an ORDER BY key", sql: "SELECT a FROM t ORDER BY CAST(a AS BOOLEAN)"},
		{name: "a GROUP BY key", sql: "SELECT a FROM t GROUP BY CAST(a AS BOOLEAN)"},
		{name: "an UPDATE SET value", sql: "UPDATE t SET a = CAST(b AS BOOLEAN)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithTypeNormaliserHook(panickingTypeHook))
			statement := parseOne(t, engine, testCase.sql)
			ctx, _ := newCapturingContext(t)

			analysis, err := engine.AnalyseQuery(ctx, nil, statement)

			require.Nil(t, analysis)
			require.ErrorIs(t, err, fault.ErrPanic)
			require.EqualError(t, err, "sqlite: analyse query: panic: type hook bug")
		})
	}
}
