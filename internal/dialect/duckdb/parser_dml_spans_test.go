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
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
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
			sql: "-- leading comment\nSELECT DISTINCT ON (i.id) i.id, i.name AS label, count(*) total, " +
				"row_number() OVER w AS rank FROM items i JOIN items j ON j.id = i.id WHERE i.id > $1 AND i.name NOT IN ($2) " +
				"GROUP BY i.id, i.name HAVING count(*) > $3 WINDOW w AS (ORDER BY i.id) QUALIFY rank = 1 " +
				"ORDER BY i.id DESC NULLS LAST LIMIT $4 OFFSET $5 ;",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Quantifier":     "DISTINCT ON (i.id)",
					"Projection":     "i.id, i.name AS label, count(*) total, row_number() OVER w AS rank",
					"From":           "FROM items i JOIN items j ON j.id = i.id",
					"Where":          "WHERE i.id > $1 AND i.name NOT IN ($2)",
					"WhereCondition": "i.id > $1 AND i.name NOT IN ($2)",
					"GroupBy":        "GROUP BY i.id, i.name",
					"Having":         "HAVING count(*) > $3",
					"OrderBy":        "ORDER BY i.id DESC NULLS LAST",
					"Pagination":     "LIMIT $4 OFFSET $5",
					"Terminator":     ";",
				},
				RowShaping: []string{"QUALIFY rank = 1"},
				ProjectionItems: []string{"i.id|i.id||false", "i.name AS label|i.name|AS label|false",
					"count(*) total|count(*)|total|false", "row_number() OVER w AS rank|row_number() OVER w|AS rank|false"},
				Placeholders: []string{"$1||1|", "$2||2|NOT IN($2)", "$3||3|", "$4||4|", "$5||5|"},
			},
		},
		{
			name: "a named placeholder",
			sql:  "SELECT * FROM items WHERE id = :id LIMIT 1",
			expected: renderedSpans{
				Clauses: map[string]string{
					"Projection":     "*",
					"From":           "FROM items",
					"Where":          "WHERE id = :id",
					"WhereCondition": "id = :id",
					"Pagination":     "LIMIT 1",
				},
				ProjectionItems: []string{"*|*||true"},
				Placeholders:    []string{":id|id|0|"},
			},
		},
		{
			name: "a compound query keeps only the clauses after its branches",
			sql:  "SELECT id FROM items WHERE id = $1 UNION ALL SELECT id FROM items WHERE name = $2 ORDER BY id LIMIT 10%",
			expected: renderedSpans{
				Clauses:      map[string]string{"OrderBy": "ORDER BY id", "Pagination": "LIMIT 10%"},
				Placeholders: []string{"$1||1|", "$2||2|"},
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
			name: "a FROM-first query records the clauses after its select list only",
			sql:  "FROM items SELECT id WHERE id = $1",
			expected: renderedSpans{
				Clauses:      map[string]string{"Where": "WHERE id = $1", "WhereCondition": "id = $1"},
				Placeholders: []string{"$1||1|"},
			},
		},
		{
			name: "an INSERT records its rows, its RETURNING clause and its terminator",
			sql: "INSERT INTO main.items AS t (id, name) VALUES ($1, $2), ($3, lower($4)) ON CONFLICT (id) DO UPDATE " +
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
	case *ast.WindowFunctionExpression:
		return "window " + describeClauseExpression(sql, typed.Function)
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
	case *ast.NamedArgumentExpression:
		return typed.Name + " := " + describeClauseExpression(sql, typed.Value)
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
			name: "a SELECT reports its join, WHERE, GROUP BY, HAVING, QUALIFY and ORDER BY expressions",
			sql: "SELECT i.id AS key FROM items i JOIN tags t ON t.item_id = i.id WHERE lower(i.name) = $1 " +
				"GROUP BY i.id, ROLLUP (i.kind) HAVING count(*) > 1 QUALIFY row_number() OVER (ORDER BY i.id) = 1 " +
				"ORDER BY key DESC NULLS LAST, i.id",
			expected: []string{
				"JOIN ON: column t.item_id = column i.id",
				"WHERE: call lower(column i.name) = *ast.ParameterExpression",
				"GROUP BY: column i.id",
				"GROUP BY: column i.kind",
				"HAVING: call count() > *ast.LiteralExpression",
				"row shaping: window call row_number() = *ast.LiteralExpression",
				"ORDER BY: column key",
				"ORDER BY: column i.id",
			},
		},
		{
			name: "lambdas and struct fields in a WHERE condition",
			sql:  "SELECT id FROM items WHERE len(list_filter(tags, x -> x > 1)) > 0 AND address.city = $1",
			expected: []string{
				"WHERE: call len(call list_filter(column tags, lambda x -> column x > *ast.LiteralExpression)) > " +
					"*ast.LiteralExpression AND column address.city = *ast.ParameterExpression",
			},
		},
		{
			name: "a pattern matched against ANY or ALL of a list is no call",
			sql:  "SELECT id FROM items WHERE name ILIKE ANY (['a%', 'b%']) AND name NOT LIKE ALL ($1)",
			expected: []string{
				"WHERE: column name ILIKE *ast.UnknownExpression AND *ast.UnaryOpExpression",
			},
		},
		{
			name: "an UPDATE reports its SET targets and values and its WHERE condition",
			sql:  "UPDATE items SET name = upper(label), (kind, rank) = ($1, rank + 1) WHERE main.is_live(id)",
			expected: []string{
				"SET target: column name",
				"SET value: call upper(column label)",
				"SET target: column kind",
				"SET target: column rank",
				"SET value: *ast.ParameterExpression",
				"SET value: *ast.BinaryOpExpression",
				"WHERE: call main.is_live(column id)",
			},
		},
		{
			name:     "an ON CONFLICT action naming the EXCLUDED row reports only the VALUES row's values",
			sql:      "INSERT INTO items (id, name) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name WHERE items.id > 0",
			expected: []string{"VALUES row value: *ast.ParameterExpression", "VALUES row value: *ast.ParameterExpression"},
		},
		{
			name:     "a VALUES list reports only its ordering",
			sql:      "VALUES (1, 2) ORDER BY col0",
			expected: []string{"ORDER BY: column col0"},
		},
		{
			name:     "ORDER BY ALL and GROUP BY ALL name no column",
			sql:      "SELECT name, count(*) FROM items GROUP BY ALL ORDER BY ALL DESC",
			expected: nil,
		},
		{
			name:     "a compound query reports its ORDER BY on the statement and each branch's WHERE on the branch",
			sql:      "SELECT id FROM items WHERE id = 1 UNION SELECT id FROM tags WHERE tag_id = 2 ORDER BY id",
			expected: []string{"WHERE: column id = *ast.LiteralExpression", "ORDER BY: column id"},
			nested:   []string{"WHERE: column tag_id = *ast.LiteralExpression"},
		},
		{
			name:     "a PIVOT statement reports nothing",
			sql:      "PIVOT items ON kind IN ('a', 'b') USING sum(price) GROUP BY name ORDER BY name",
			expected: nil,
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

func TestClauseExpressionsReadThreePartNamesAsColumnsOfSchemaRelations(t *testing.T) {
	t.Parallel()

	sql := "SELECT id FROM main.users WHERE main.users.id = 1"
	analysis := analyseQuery(t, buildTestCatalogue(), sql)

	require.Equal(t, []string{"WHERE: column main.users.id = *ast.LiteralExpression"}, clauseExpressionsOf(sql, analysis))
}

func TestClauseExpressionsReadFieldsOfStructColumns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{
			name:     "a struct column's field compared with a parameter",
			sql:      "SELECT id FROM boats WHERE berth['pontoon'] = $1",
			expected: []string{"WHERE: column berth field pontoon = *ast.ParameterExpression"},
		},
		{
			name:     "a parameter compared with a struct column's field",
			sql:      "SELECT id FROM boats WHERE $1 = berth['number']",
			expected: []string{"WHERE: *ast.ParameterExpression = column berth field number"},
		},
		{
			name:     "a map column's value stays a subscript",
			sql:      "SELECT id FROM boats WHERE fees['July'] = $1",
			expected: []string{"WHERE: *ast.ArraySubscriptExpression = *ast.ParameterExpression"},
		},
		{
			name:     "a struct column's field written with a dot",
			sql:      "SELECT id FROM boats WHERE berth.pontoon = $1",
			expected: []string{"WHERE: column berth.pontoon field pontoon = *ast.ParameterExpression"},
		},
		{
			name:     "a relation's column written with a dot stays a column",
			sql:      "SELECT id FROM boats b WHERE b.id = $1",
			expected: []string{"WHERE: column b.id = *ast.ParameterExpression"},
		},
		{
			name:     "a name that is neither a relation nor a struct column stays a column",
			sql:      "SELECT id FROM boats WHERE nowhere.pontoon = $1",
			expected: []string{"WHERE: column nowhere.pontoon = *ast.ParameterExpression"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, nestedTypeCatalogue(), testCase.sql)

			require.Equal(t, testCase.expected, clauseExpressionsOf(testCase.sql, analysis))
		})
	}
}

func TestClauseExpressionsOfNestedQueriesStayOnThem(t *testing.T) {
	t.Parallel()

	sql := "WITH recent AS (SELECT a.id FROM items a, items c JOIN tags b ON b.id = c.id WHERE a.id > $1) " +
		"SELECT r.id FROM recent r WHERE EXISTS (SELECT 1 FROM tags x WHERE x.item_id = r.id) " +
		"AND r.id NOT IN (SELECT y.id FROM items y SEMI JOIN tags z ON z.item_id = y.id)"
	analysis, _ := analyseStatement(t, sql)

	require.Len(t, analysis.CTEDefinitions, 1)
	definition := analysis.CTEDefinitions[0]
	require.Equal(t, []string{"a", "c", "b"}, definition.FromOrder)
	require.NotNil(t, definition.Query)
	require.Equal(t, []string{"JOIN ON: column b.id = column c.id", "WHERE: column a.id > *ast.ParameterExpression"},
		clauseExpressionsOf(sql, definition.Query))
	require.Equal(t, []string{"WHERE: *ast.ExistsExpression AND *ast.UnaryOpExpression"}, clauseExpressionsOf(sql, analysis))
	require.Len(t, analysis.PredicateSubqueries, 2)
	require.Equal(t, []string{"WHERE: column x.item_id = column r.id"}, clauseExpressionsOf(sql, analysis.PredicateSubqueries[0]))
	semi := analysis.PredicateSubqueries[1].PredicateSubqueries
	require.Len(t, semi, 1)
	require.Equal(t, []string{"JOIN ON: column z.item_id = column y.id"}, clauseExpressionsOf(sql, semi[0]))
}

func TestImplicitColumnAliases(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "SELECT id ident, count(*) total, lower(name) label FROM items")

	require.Len(t, analysis.OutputColumns, 3)
	require.Equal(t, "ident", analysis.OutputColumns[0].Name)
	require.Equal(t, "total", analysis.OutputColumns[1].Name)
	require.Equal(t, "label", analysis.OutputColumns[2].Name)
}

func TestNamedArguments(t *testing.T) {
	t.Parallel()

	sql := "SELECT add_default(1, b := $1) AS total FROM items WHERE main.scale(id, factor => $2) > 3 " +
		"AND id IN (SELECT add_default(2, b := $3) FROM items)"
	analysis, _ := analyseStatement(t, sql)

	require.Equal(t, map[int]string{1: "b", 2: "factor", 3: "b"}, analysis.ArgumentNames)
	call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
	require.True(t, isCall)
	require.Len(t, call.Arguments, 2)
	named, isNamed := call.Arguments[1].(*ast.NamedArgumentExpression)
	require.True(t, isNamed)
	require.Equal(t, "b", named.Name)
	require.Equal(t, []string{
		"WHERE: call main.scale(column id, factor := *ast.ParameterExpression) > *ast.LiteralExpression AND column id in list",
	}, clauseExpressionsOf(sql, analysis))
}

func TestPredicateParametersInCallsAreFunctionArguments(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "SELECT id FROM items WHERE scale_price(id, $1) > 3 AND main.discount($2, 4) < 1")

	require.Len(t, analysis.ParameterReferences, 2)
	first, second := analysis.ParameterReferences[0], analysis.ParameterReferences[1]
	require.Equal(t, sqlmodel.ParameterContextFunctionArgument, first.Context)
	require.Equal(t, "scale_price", first.EnclosingFunctionName)
	require.Equal(t, 1, first.ArgumentOrdinal)
	require.Equal(t, sqlmodel.ParameterContextFunctionArgument, second.Context)
	require.Equal(t, "main.discount", second.EnclosingFunctionName)
	require.Equal(t, 0, second.ArgumentOrdinal)
}

func TestListComprehensionInAWhereCondition(t *testing.T) {
	t.Parallel()

	sql := "SELECT id FROM items WHERE [x * 2 FOR x IN tags IF x > $1] <> [] AND id = $2"
	analysis, _ := analyseStatement(t, sql)

	require.Len(t, analysis.ParameterReferences, 2)
	require.Equal(t, []string{
		"WHERE: call (call (column tags, lambda x -> column x > *ast.ParameterExpression), " +
			"lambda x -> *ast.BinaryOpExpression) <> call () AND column id = *ast.ParameterExpression",
	}, clauseExpressionsOf(sql, analysis))
}

func TestLikeAnyListKeepsItsParameters(t *testing.T) {
	t.Parallel()

	analysis, _ := analyseStatement(t, "SELECT name LIKE ANY ($1, $2) AS matched FROM items")

	require.Len(t, analysis.ParameterReferences, 2)
	require.IsType(t, &ast.ComparisonExpression{}, analysis.OutputColumns[0].Expression)
}

func TestConditionsMustReadAsOneExpression(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		sql     string
		message string
	}{
		{sql: "SELECT id FROM items WHERE", message: "expected a condition"},
		{sql: "SELECT id FROM items GROUP BY id HAVING", message: "expected a condition"},
		{sql: "SELECT a.id FROM items a JOIN items b ON", message: "expected a condition"},
		{sql: "SELECT id FROM items QUALIFY ORDER BY id", message: "expected a condition"},
		{sql: "UPDATE items SET name = 'x' WHERE", message: "expected a condition"},
		{sql: "SELECT id FROM items WHERE id = (", message: "expected an expression"},
		{sql: "SELECT id FROM items WHERE (id = 1", message: "expected ')'"},
		{sql: "SELECT id FROM items WHERE id = 1 AND", message: "expected an expression"},
		{sql: "SELECT id FROM items WHERE id = 1 OR", message: "expected an expression"},
		{sql: "SELECT id FROM items WHERE id =", message: "expected an expression"},
		{sql: "SELECT id FROM items WHERE id = 1 AND ORDER BY id", message: `near "ORDER": expected an expression`},
		{sql: "SELECT id FROM items WHERE id = 1 OR OR id = 2", message: `near "OR": expected an expression`},
		{sql: "SELECT id FROM items WHERE id = 1 name", message: `near "name": expected the end of the condition`},
		{sql: "SELECT id FROM items GROUP BY id HAVING count(*) > 1 name", message: "expected the end of the condition"},
		{sql: "SELECT a.id FROM items a JOIN items b ON a.id = b.id name", message: "expected the end of the condition"},
		{sql: "DELETE FROM items WHERE id = 1 name", message: "expected the end of the condition"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.sql, func(t *testing.T) {
			t.Parallel()

			err := analyseQueryError(t, testCase.sql)

			require.ErrorIs(t, err, fault.ErrParse)
			require.Contains(t, err.Error(), testCase.message)
		})
	}
}

func TestConditionsReadEveryPredicateForm(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"SELECT id FROM items WHERE name GLOB $1 AND name NOT GLOB 'x*'",
		"SELECT id FROM items WHERE name ISNULL OR name NOTNULL",
		"SELECT id FROM items WHERE id IN (SELECT id FROM items WHERE id > $1) AND EXISTS (SELECT 1 FROM items)",
		"SELECT id FROM items WHERE len(list_filter(tags, x -> x > $1)) > 0",
		"SELECT id FROM items WHERE name IS NOT DISTINCT FROM $1 AND id BETWEEN $2 AND $3",
		"SELECT id FROM items WHERE name SIMILAR TO 'a.*' ESCAPE '!' AND name ILIKE ANY ($1, $2)",
		`SELECT id FROM items WHERE "or" = 1 AND COLUMNS('na.*') = $1`,
		"SELECT a.id FROM items a JOIN items b ON a.id = b.id AND b.name ^@ 'x' WHERE a.tags @> [1]",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, analyseQueryError(t, sql))
		})
	}
}

func TestOrderByPlace(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		sql    string
		before string
	}{
		{name: "without ORDER BY", sql: "SELECT id FROM items WHERE id = $1 LIMIT 1", before: "SELECT id FROM items WHERE id = $1"},
		{name: "with ORDER BY", sql: "SELECT id FROM items ORDER BY id LIMIT 1", before: "SELECT id FROM items"},
		{
			name:   "after QUALIFY and WINDOW",
			sql:    "SELECT id, row_number() OVER w AS r FROM items WINDOW w AS (ORDER BY id) QUALIFY r = 1 LIMIT 5",
			before: "SELECT id, row_number() OVER w AS r FROM items WINDOW w AS (ORDER BY id) QUALIFY r = 1",
		},
		{
			name:   "after the last arm of a compound query",
			sql:    "SELECT id FROM items UNION ALL SELECT id FROM items WHERE id > 1 LIMIT 3",
			before: "SELECT id FROM items UNION ALL SELECT id FROM items WHERE id > 1",
		},
		{name: "a FROM-first query", sql: "FROM items SELECT id WHERE id = 1", before: "FROM items SELECT id WHERE id = 1"},
		{name: "an INSERT records none", sql: "INSERT INTO items (id) SELECT id FROM items", before: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, statement := analyseStatement(t, testCase.sql)

			place := analysis.Spans.OrderByPlace
			if testCase.before == "" {
				require.Equal(t, ast.Span{}, place)
				return
			}
			require.Equal(t, place.Start, place.End)
			require.Equal(t, testCase.before, testCase.sql[statement.Location:statement.Location+place.Start])
		})
	}
}

func TestAWalkStoppedInsideAClauseFailsTheStatement(t *testing.T) {
	t.Parallel()

	additions := strings.Repeat(" + 1", walkedItems)
	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a WHERE condition", sql: "SELECT 1 FROM t WHERE " + strings.Repeat("c = 1 AND ", walkedItems) + "c = $1::hooked"},
		{name: "an ORDER BY list", sql: "SELECT c FROM t ORDER BY " + numberedList("c%d", walkedItems) + ", $1::hooked"},
		{name: "an INSERT values row", sql: "INSERT INTO t VALUES (" + numberedList("%d", walkedItems) + ", $1::hooked)"},
		{name: "an UPDATE SET value", sql: "UPDATE t SET c = 1" + additions + " + $1::hooked"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			engine := New(cancelOnType("hooked", cancel))

			analysis, err := engine.AnalyseQuery(ctx, nil, parseOne(t, engine, testCase.sql))

			require.ErrorIs(t, err, errWalkStopped)
			assert.Nil(t, analysis)
		})
	}
}
