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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestElementAccessIsNamedByItsPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		wantName string
	}{
		{name: "a field of a Nested column", sql: "SELECT legs.colony FROM flights", wantName: "legs.colony"},
		{name: "a field read through the table", sql: "SELECT flights.legs.minutes FROM flights", wantName: "legs.minutes"},
		{name: "a field read through an alias", sql: "SELECT f.legs.minutes FROM flights AS f", wantName: "legs.minutes"},
		{name: "a tuple element", sql: "SELECT position.lat FROM sightings", wantName: "position.lat"},
		{name: "a quoted field column", sql: "SELECT `legs.colony` FROM flights", wantName: "legs.colony"},
		{name: "an aliased field", sql: "SELECT legs.colony AS colony FROM flights", wantName: "colony"},
		{name: "a field aliased to the name of the field", sql: "SELECT legs.colony AS colony FROM flights", wantName: "colony"},
		{name: "a deeper field aliased to the name of the field", sql: "SELECT f.legs.minutes AS minutes FROM flights AS f", wantName: "minutes"},
		{name: "a column read through its table", sql: "SELECT flights.id FROM flights", wantName: "id"},
		{name: "a column read through an alias", sql: "SELECT f.id FROM flights AS f", wantName: "id"},
		{name: "an aliased column read through its table", sql: "SELECT f.id AS id FROM flights AS f", wantName: "id"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			require.NotEmpty(t, analysis.OutputColumns)
			require.Equal(t, testCase.wantName, analysis.OutputColumns[0].Name)
		})
	}
}

func TestArrayJoinAliasReadsAsAValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		wantName string
		wantPath string
	}{
		{name: "a field of an aliased element", sql: "SELECT leg.colony FROM flights ARRAY JOIN legs AS leg", wantName: "leg.colony", wantPath: "leg"},
		{name: "a field of a column unfolded in place", sql: "SELECT legs.colony FROM flights ARRAY JOIN legs", wantName: "legs.colony", wantPath: "legs"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			require.NotEmpty(t, analysis.OutputColumns)
			require.Equal(t, testCase.wantName, analysis.OutputColumns[0].Name)
			access, isAccess := analysis.OutputColumns[0].Expression.(*ast.StructFieldAccessExpression)
			require.True(t, isAccess)
			operator, isOperator := access.Struct.(*ast.UnaryOpExpression)
			require.True(t, isOperator)
			column, isColumn := operator.Operand.(*ast.ColumnRefExpression)
			require.True(t, isColumn)
			require.Equal(t, testCase.wantPath, column.ColumnName)
		})
	}
}

func TestArrayJoinExpressionSourceReadsElementsOfColumns(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT x FROM flights ARRAY JOIN arrayMap(v -> v * 2, legs.minutes) AS x")
	require.Len(t, analysis.ArrayJoinClauses, 1)
	call, isCall := analysis.ArrayJoinClauses[0].Source.(*ast.FunctionCallExpression)
	require.True(t, isCall)
	require.Len(t, call.Arguments, 2)
	_, isAccess := call.Arguments[1].(*ast.StructFieldAccessExpression)
	require.True(t, isAccess)
}

func TestArrayJoinOfAFlattenedColumnReadsTheColumn(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT legs.colony, legs.minutes FROM flights ARRAY JOIN legs.colony")
	require.Len(t, analysis.OutputColumns, 2)
	require.Equal(t, &ast.ColumnRefExpression{TableAlias: "", ColumnName: "legs.colony", Span: ast.Span{}}, analysis.OutputColumns[0].Expression)
	require.Equal(t, "legs.colony", analysis.OutputColumns[0].Name)
	_, isAccess := analysis.OutputColumns[1].Expression.(*ast.StructFieldAccessExpression)
	require.True(t, isAccess)
}

func TestElementReadsAreFoundInEveryKindOfExpression(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "an IS NULL test", sql: "SELECT position.lat IS NULL FROM sightings"},
		{name: "an IN test", sql: "SELECT position.lat IN (1, 2) FROM sightings"},
		{name: "a value of an IN list", sql: "SELECT 1 IN (position.lat, 2) FROM sightings"},
		{name: "a BETWEEN test", sql: "SELECT position.lat BETWEEN 1 AND 2 FROM sightings"},
		{name: "a subscript", sql: "SELECT position.tags[1] FROM sightings"},
		{name: "a lambda body", sql: "SELECT arrayMap(x -> x + position.lat, [1]) FROM sightings"},
		{name: "a CASE branch", sql: "SELECT CASE WHEN position.lat > 0 THEN 1 END FROM sightings"},
		{name: "a window function", sql: "SELECT sum(position.lat) OVER () FROM sightings"},
		{name: "a logical operand", sql: "SELECT position.lat > 0 AND position.lon < 0 FROM sightings"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			require.Len(t, analysis.OutputColumns, 1)
			accesses := 0
			for node := range ast.Preorder(analysis.OutputColumns[0].Expression) {
				if column, isColumn := node.(*ast.ColumnRefExpression); isColumn {
					require.Empty(t, column.TableAlias, "a read of an element through a column is not a column of a table")
				}
				if _, isAccess := node.(*ast.StructFieldAccessExpression); isAccess {
					accesses++
				}
			}
			require.Positive(t, accesses)
		})
	}
}

func TestArrayJoinOfAFlattenedColumnIsReadInEveryKindOfExpression(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a lambda body", sql: "SELECT arrayMap(x -> x + legs.colony, [1]) FROM flights ARRAY JOIN legs.colony"},
		{name: "an IS NULL test", sql: "SELECT legs.colony IS NULL FROM flights ARRAY JOIN legs.colony"},
		{name: "a value of an IN list", sql: "SELECT 1 IN (legs.colony, 2) FROM flights ARRAY JOIN legs.colony"},
		{name: "a window function", sql: "SELECT max(legs.colony) OVER () FROM flights ARRAY JOIN legs.colony"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			require.Len(t, analysis.OutputColumns, 1)
			reads := 0
			for node := range ast.Preorder(analysis.OutputColumns[0].Expression) {
				require.NotEqual(t, "legs.colony", elementAccessPath(node), "the unfolded column is read, not an element of legs")
				if column, isColumn := node.(*ast.ColumnRefExpression); isColumn && column.ColumnName == "legs.colony" {
					reads++
				}
			}
			require.Equal(t, 1, reads)
		})
	}
}

func TestDottedNamesOutsideScopeReadElements(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT o.id, basket.fish AS fish, length(o.basket) AS size, toString(pos.name) AS label "+
		"FROM orders AS o GROUP BY o.id, basket.fish")
	require.Len(t, analysis.OutputColumns, 4)
	assert.Equal(t, "o", analysis.OutputColumns[0].TableAlias)
	assert.Nil(t, analysis.OutputColumns[0].Expression)

	fish := analysis.OutputColumns[1]
	assert.Empty(t, fish.TableAlias)
	assert.Empty(t, fish.ColumnName)
	assert.Equal(t, "fish", fish.Name)
	assert.Equal(t, elementAccess("basket", "fish"), fish.Expression)

	size, ok := analysis.OutputColumns[2].Expression.(*ast.FunctionCallExpression)
	require.True(t, ok)
	assert.Equal(t, &ast.ColumnRefExpression{TableAlias: "o", ColumnName: "basket"}, withoutSpans(size.Arguments[0]))

	label, ok := analysis.OutputColumns[3].Expression.(*ast.FunctionCallExpression)
	require.True(t, ok)
	assert.Equal(t, elementAccess("pos", "name"), label.Arguments[0])

	assert.Equal(t, []ast.ColumnReference{{TableAlias: "o", ColumnName: "id"}}, analysis.GroupByColumns)
}

func TestDottedNamesInNestedScopesKeepTheirTables(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "WITH totals AS (SELECT g.id, g.doc.size AS size FROM gulls AS g) "+
		"SELECT totals.id FROM totals UNION ALL SELECT s.id FROM (SELECT x.id FROM sightings AS x) AS s")
	require.Len(t, analysis.CTEDefinitions, 1)
	assert.Equal(t, "g", analysis.CTEDefinitions[0].OutputColumns[0].TableAlias)
	assert.Equal(t, subcolumnAccess(&ast.ColumnRefExpression{TableAlias: "g", ColumnName: "doc"}, "size"),
		withoutSpans(analysis.CTEDefinitions[0].OutputColumns[1].Expression))
	assert.Equal(t, "totals", analysis.OutputColumns[0].TableAlias)
	require.Len(t, analysis.CompoundBranches, 1)
	branch := analysis.CompoundBranches[0].Query
	assert.Equal(t, "s", branch.OutputColumns[0].TableAlias)
	assert.Equal(t, "x", branch.RawDerivedTables[0].InnerQuery.OutputColumns[0].TableAlias)
}

func TestDottedNamesAreRewrittenThroughEveryOperand(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT CASE WHEN -pos.value > 1 THEN coalesce(doc.a, 'x') ELSE CAST(doc.b AS String) END AS label, "+
		"doc.a = 'y' AS matches FROM orders")
	require.Len(t, analysis.OutputColumns, 2)
	caseExpression, ok := analysis.OutputColumns[0].Expression.(*ast.CaseWhenExpression)
	require.True(t, ok)
	condition, ok := caseExpression.Branches[0].Condition.(*ast.ComparisonExpression)
	require.True(t, ok)
	negation, ok := condition.Left.(*ast.UnaryOpExpression)
	require.True(t, ok)
	assert.Equal(t, elementAccess("pos", "value"), negation.Operand)
	coalesce, ok := caseExpression.Branches[0].Result.(*ast.CoalesceExpression)
	require.True(t, ok)
	assert.Equal(t, elementAccess("doc", "a"), coalesce.Arguments[0])
	cast, ok := caseExpression.ElseResult.(*ast.CastExpression)
	require.True(t, ok)
	assert.Equal(t, elementAccess("doc", "b"), cast.Inner)
	comparison, ok := analysis.OutputColumns[1].Expression.(*ast.ComparisonExpression)
	require.True(t, ok)
	assert.Equal(t, elementAccess("doc", "a"), comparison.Left)
}
