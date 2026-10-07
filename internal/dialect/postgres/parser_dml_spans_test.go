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

package postgres

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

type renderedSpans struct {
	Clauses         map[string]string
	RowShaping      []string
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
	for _, span := range spans.RowShaping {
		rendered.RowShaping = append(rendered.RowShaping, spanTextOf(sql, statement, span))
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
			sql: "-- leading comment\nSELECT DISTINCT ON (i.id) i.id, i.name AS label, count(*) total, i.* FROM items i " +
				"JOIN items j ON j.id = i.id WHERE i.id > $1 AND i.name NOT IN ($2) GROUP BY i.id, i.name " +
				"HAVING count(*) > $3 ORDER BY i.id DESC NULLS LAST LIMIT $4 OFFSET $5 FOR UPDATE OF i ;",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Quantifier":     "DISTINCT ON (i.id)",
					"Projection":     "i.id, i.name AS label, count(*) total, i.*",
					"From":           "FROM items i JOIN items j ON j.id = i.id",
					"Where":          "WHERE i.id > $1 AND i.name NOT IN ($2)",
					"WhereCondition": "i.id > $1 AND i.name NOT IN ($2)",
					"GroupBy":        "GROUP BY i.id, i.name",
					"Having":         "HAVING count(*) > $3",
					"OrderBy":        "ORDER BY i.id DESC NULLS LAST",
					"Pagination":     "LIMIT $4 OFFSET $5",
					"Locking":        "FOR UPDATE OF i",
					"Terminator":     ";",
				},
				ProjectionItems: []string{"i.id|i.id||false", "i.name AS label|i.name|AS label|false",
					"count(*) total|count(*)|total|false", "i.*|i.*||true"},
				Placeholders: []string{"$1||1|", "$2||2|NOT IN($2)", "$3||3|", "$4||4|", "$5||5|"},
			},
		},
		{
			name: "a compound query keeps only the clauses after its branches",
			sql:  "SELECT id FROM items WHERE id = $1 UNION ALL SELECT id FROM items WHERE name = :name ORDER BY id FETCH FIRST 3 ROWS ONLY",
			expected: renderedSpans{
				Clauses:      map[string]string{"OrderBy": "ORDER BY id", "Pagination": "FETCH FIRST 3 ROWS ONLY"},
				Placeholders: []string{"$1||1|", ":name|name|0|"},
			},
		},
		{
			name: "a CTE body records nothing of its own",
			sql:  "WITH recent AS (SELECT id FROM items WHERE id > $1 ORDER BY id LIMIT 5) SELECT r.* FROM recent r WHERE r.id IN ($2)",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Projection":     "r.*",
					"From":           "FROM recent r",
					"Where":          "WHERE r.id IN ($2)",
					"WhereCondition": "r.id IN ($2)",
				},
				ProjectionItems: []string{"r.*|r.*||true"},
				Placeholders:    []string{"$1||1|", "$2||2|IN($2)"},
			},
		},
		{
			name: "an INSERT records its rows, its RETURNING clause and its terminator",
			sql: "INSERT INTO public.items AS t (id, name) VALUES ($1, $2), ($3, lower($4)) ON CONFLICT (id) DO UPDATE " +
				"SET name = EXCLUDED.name WHERE t.id > 0 RETURNING id, name AS label;",
			expected: renderedSpans{
				Clauses:        map[string]string{"Returning": "RETURNING id, name AS label", "Terminator": ";"},
				ReturningItems: []string{"id|id||false", "name AS label|name|AS label|false"},
				Values:         []string{"($1, $2)=$1;$2", "($3, lower($4))=$3;lower($4)"},
				Placeholders:   []string{"$1||1|", "$2||2|", "$3||3|", "$4||4|"},
			},
		},
		{
			name: "an INSERT ... SELECT records none of its SELECT's clauses",
			sql:  "INSERT INTO items (id, name) SELECT id, name FROM items WHERE id = $1",
			expected: renderedSpans{
				Clauses:      map[string]string{},
				Placeholders: []string{"$1||1|"},
			},
		},
		{
			name: "an UPDATE records its WHERE and RETURNING clauses",
			sql:  "UPDATE items SET name = $1 WHERE id = $2 RETURNING *",
			expected: renderedSpans{
				Clauses:        map[string]string{"Where": "WHERE id = $2", "WhereCondition": "id = $2", "Returning": "RETURNING *"},
				ReturningItems: []string{"*|*||true"},
				Placeholders:   []string{"$1||1|", "$2||2|"},
			},
		},
		{
			name: "a DELETE locates the placeholders inside its subquery",
			sql:  "DELETE FROM items WHERE id IN (SELECT id FROM items WHERE name = $1) RETURNING id",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Where":          "WHERE id IN (SELECT id FROM items WHERE name = $1)",
					"WhereCondition": "id IN (SELECT id FROM items WHERE name = $1)",
					"Returning":      "RETURNING id",
				},
				ReturningItems: []string{"id|id||false"},
				Placeholders:   []string{"$1||1|"},
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

	analysis, _ := analyseStatement(t, "INSERT INTO billing.invoices AS i (id) VALUES ($1)")

	require.Equal(t, ast.TableReference{Schema: "billing", Name: "invoices", Alias: "i"}, analysis.InsertTarget)
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
			sql: "SELECT i.id AS key FROM items i JOIN tags t ON t.item_id = i.id WHERE lower(i.name) = $1 " +
				"GROUP BY i.id, ROLLUP (i.kind) HAVING count(*) > 1 ORDER BY key DESC NULLS LAST, i.id",
			expected: []string{
				"JOIN ON: column t.item_id = column i.id",
				"WHERE: call lower(column i.name) = *ast.ParameterExpression",
				"GROUP BY: column i.id",
				"GROUP BY: column i.kind",
				"HAVING: call count() > *ast.LiteralExpression",
				"ORDER BY: column key",
				"ORDER BY: column i.id",
			},
		},
		{
			name: "an UPDATE reports its SET targets and values and its WHERE condition",
			sql:  "UPDATE items SET name = upper(label), (kind, rank) = ($1, rank + 1) WHERE public.is_live(id)",
			expected: []string{
				"SET target: column name",
				"SET value: call upper(column label)",
				"SET target: column kind",
				"SET target: column rank",
				"SET value: *ast.ParameterExpression",
				"SET value: *ast.BinaryOpExpression",
				"WHERE: call public.is_live(column id)",
			},
		},
		{
			name:     "an ON CONFLICT action naming the EXCLUDED row reports the VALUES row's values and the columns its SET assigns",
			sql:      "INSERT INTO items (id, name) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name WHERE items.id > 0",
			expected: []string{"VALUES row value: *ast.ParameterExpression", "VALUES row value: *ast.ParameterExpression", "SET target: column name"},
		},
		{
			name:     "a VALUES list reports the ORDER BY that sorts it",
			sql:      "VALUES (1, 2) ORDER BY column1",
			expected: []string{"ORDER BY: column column1"},
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

	sql := "WITH recent AS (SELECT a.id FROM items a, items c JOIN tags b ON b.id = c.id WHERE a.id > $1) " +
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
		after  string
	}{
		{name: "a SELECT without ORDER BY ends at the place", sql: "SELECT id FROM items WHERE id = $1", before: "id = $1", after: ""},
		{name: "a written ORDER BY starts at the place", sql: "SELECT id FROM items ORDER BY id LIMIT 1", before: "items", after: " ORDER BY id LIMIT 1"},
		{name: "pagination follows the place", sql: "SELECT id FROM items LIMIT $1 OFFSET 2", before: "items", after: " LIMIT $1 OFFSET 2"},
		{name: "locking follows the place", sql: "SELECT id FROM items FOR UPDATE", before: "items", after: " FOR UPDATE"},
		{name: "a named window precedes the place", sql: "SELECT rank() OVER w FROM items WINDOW w AS (ORDER BY id) LIMIT 3",
			before: "WINDOW w AS (ORDER BY id)", after: " LIMIT 3"},
		{name: "a compound query places it after its last branch", sql: "SELECT id FROM items UNION SELECT id FROM other LIMIT 1",
			before: "UNION SELECT id FROM other", after: " LIMIT 1"},
		{name: "HAVING precedes the place", sql: "SELECT id FROM items GROUP BY id HAVING count(*) > 1;", before: "HAVING count(*) > 1", after: ""},
		{name: "a VALUES list records it", sql: "VALUES (1), (2) ORDER BY 1", before: "(2)", after: " ORDER BY 1"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, statement := analyseStatement(t, testCase.sql)
			place := analysis.Spans.OrderByPlace
			require.Positive(t, place.Start)
			require.Equal(t, place.Start, place.End)
			text := testCase.sql[statement.Location : statement.Location+statement.Length]
			require.True(t, strings.HasSuffix(text[:place.Start], testCase.before), "the place follows %q in %q", testCase.before, text)
			require.Equal(t, testCase.after, text[place.Start:])
		})
	}
}

func TestOrderByPlaceIsLeftOutOfWrites(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"INSERT INTO items (id) SELECT id FROM other",
		"UPDATE items SET name = $1 WHERE id = $2",
		"DELETE FROM items WHERE id = $1",
	} {
		analysis, _ := analyseStatement(t, sql)
		require.Equal(t, ast.Span{}, analysis.Spans.OrderByPlace, sql)
	}
}

func TestClauseRereadsLetAStoppedWalkUnwindToTheEntryPoint(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		read func(p *parser)
		name string
		sql  string
	}{
		{name: "a re-read clause value", sql: "SELECT 1 LIMIT 2", read: func(p *parser) { p.reparse(3, 4, false, false) }},
		{name: "a re-read list of values", sql: "SELECT 1 ORDER BY 2, 3", read: func(p *parser) { p.reparse(4, 7, true, false) }},
		{name: "a condition", sql: "SELECT 1 WHERE true", read: func(p *parser) { p.parseConditionTokens(3, 4) }},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statementParser := New().newStatementParser(stopEveryWalk, parsedStatementOf(t, testCase.sql))

			err := walkUnderRecover(t.Context(), func() { testCase.read(statementParser) })

			require.ErrorIs(t, err, errWalkStopped)
		})
	}
}

func TestConditionsAreReadWhole(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		message string
	}{
		{name: "a token after a WHERE condition", sql: "SELECT id FROM items WHERE id = 1 name", message: `near "name"`},
		{name: "a token after a HAVING condition", sql: "SELECT id FROM items GROUP BY id HAVING count(*) > 1 name", message: `near "name"`},
		{name: "a token after a join condition", sql: "SELECT a.id FROM items a JOIN items b ON a.id = b.id name", message: `near "name"`},
		{name: "a token after an UPDATE condition", sql: "UPDATE items SET name = $1 WHERE id = $2 name", message: `near "name"`},
		{name: "a token after a DELETE condition", sql: "DELETE FROM items WHERE id = $1 name", message: `near "name"`},
		{name: "an empty WHERE", sql: "SELECT id FROM items WHERE;", message: "expected a condition"},
		{name: "an empty conflict target predicate", sql: "INSERT INTO items (id) VALUES (1) ON CONFLICT (id) WHERE DO NOTHING",
			message: "expected a condition"},
		{name: "a token after a conflict target predicate", sql: "INSERT INTO items (id) VALUES (1) ON CONFLICT (id) WHERE id > 0 name DO NOTHING",
			message: `near "name"`},
		{name: "a token after a conflict action condition",
			sql:     "INSERT INTO items (id) VALUES (1) ON CONFLICT (id) DO UPDATE SET name = 'b' WHERE items.id = 1 name",
			message: `near "name"`},
		{name: "an unclosed parenthesis", sql: "SELECT id FROM items WHERE id = (;", message: "expected an expression"},
		{name: "an operator without its right operand", sql: "SELECT id FROM items WHERE id = 1 AND;", message: "expected an expression"},
		{name: "an operator running into the next clause", sql: "SELECT id FROM items WHERE id = 1 AND FROM",
			message: `near "FROM": expected an expression`},
		{name: "a comparison without its right operand", sql: "SELECT id FROM items WHERE id =", message: "expected an expression"},
		{name: "a BETWEEN without AND", sql: "SELECT id FROM items WHERE id BETWEEN 1 3", message: "expected keyword AND"},
		{name: "an unknown IS test", sql: "SELECT id FROM items WHERE name IS foo", message: "after IS"},
		{name: "an unclosed function call", sql: "SELECT id FROM items WHERE lower(name", message: "expected ')' to close the function call"},
		{name: "a token after a FILTER condition", sql: "SELECT count(*) FILTER (WHERE id > 1 name) FROM items", message: "FILTER clause"},
		{name: "a FILTER without WHERE", sql: "SELECT count(*) FILTER (id > 1) FROM items", message: "expected keyword WHERE"},
		{name: "a token after a subquery's condition", sql: "SELECT id FROM items WHERE id IN (SELECT id FROM other WHERE id = 1 name)",
			message: `near "name"`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)
			_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
			require.ErrorContains(t, err, testCase.message)
		})
	}
}

func TestConditionsReadPostgreSQLPredicateSyntax(t *testing.T) {
	t.Parallel()

	for _, condition := range []string{
		"id IS DISTINCT FROM $1 AND name IS NOT DISTINCT FROM $2",
		"doc IS JSON AND doc IS NOT JSON OBJECT WITH UNIQUE KEYS AND doc IS JSON ARRAY WITHOUT UNIQUE",
		"xmlv IS DOCUMENT AND name IS NFC NORMALIZED AND name IS NOT NORMALIZED AND name IS NOT OF (text)",
		"created AT TIME ZONE 'UTC' > now() AND created AT LOCAL > now()",
		"name COLLATE \"C\" = $1 AND name = $2 COLLATE \"en_US\"",
		"id OPERATOR(pg_catalog.=) $1 AND id OPERATOR(pg_catalog.+) 1 > 2",
		"id = ANY($1) AND id <> ALL (SELECT id FROM other) AND id = SOME (SELECT id FROM other)",
		"(id, name) = (1, 'a') AND ROW(id, name) = ROW(1, 'a')",
		"tags[1:2] = ARRAY['a'] AND tags[1] = 'a'",
		"created > date '2020-01-01' AND created > now() - INTERVAL '1' DAY",
		"name SIMILAR TO 'a%' ESCAPE '!' AND name NOT SIMILAR TO 'b%'",
		"(created, created) OVERLAPS (now(), now())",
		"doc @> '{\"a\":1}' AND doc ? 'a' AND doc ?| array['a'] AND doc ?& array['b'] AND doc @? '$.a'",
		"doc #- '{a}' IS NOT NULL AND @ id = 1 AND |/ id > 1",
		"id BETWEEN SYMMETRIC 5 AND 4 AND id NOT BETWEEN 1 AND 3",
		"EXISTS (SELECT 1 FROM other) AND NOT EXISTS (SELECT 1 FROM other)",
		"(id = 1) IS TRUE AND (id = 2) IS NOT FALSE AND (id = 3) IS UNKNOWN AND name ISNULL",
		"extract(year FROM created) = 2020 AND substring(name FROM 1 FOR 2) = 'ab' AND trim(BOTH ' ' FROM name) = 'a'",
		"xmlexists('//a' PASSING BY REF xmlv) AND json_value(doc, '$.a' RETURNING int ERROR ON ERROR) = 1",
		"(doc).a = 1 AND (doc).* IS NOT NULL",
		"length(string_agg(name, ',' ORDER BY name)) > 1",
	} {
		sql := "SELECT id FROM items WHERE " + condition
		analysis, _ := analyseStatement(t, sql)
		require.NotEmpty(t, analysis.ClauseExpressions, sql)
	}
}
