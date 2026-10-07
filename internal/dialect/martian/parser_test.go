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
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func parseOne(t *testing.T, sql string) ast.ParsedStatement {
	t.Helper()

	statements, err := New().ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	return statements[0]
}

func TestParseStatementsSplits(t *testing.T) {
	t.Parallel()

	statements, err := New().ParseStatements(t.Context(), "CREATE TABLE a (x WHOLE32);;\nCREATE TABLE b (y WHOLE32);")
	require.NoError(t, err)
	require.Len(t, statements, 2)
	require.Equal(t, 0, statements[0].Location)
	require.Equal(t, len("CREATE TABLE a (x WHOLE32)"), statements[0].Length)
}

func TestParseStatementsRejects(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		sql             string
		messageContains string
	}{
		{name: "trailing tokens", sql: "DELETE FROM items WHERE id = @p1 extra garbage", messageContains: "the end of the statement"},
		{name: "an unknown statement", sql: "DROP TABLE items", messageContains: "CREATE, SELECT, INSERT, UPDATE or DELETE"},
		{name: "a value count mismatch", sql: "INSERT INTO items (a, b) VALUES (@p1)", messageContains: "one value per named column"},
		{name: "a missing FROM", sql: "SELECT id", messageContains: "expected FROM but the input ended"},
		{name: "a missing type", sql: "CREATE TABLE items (id)", messageContains: "a type name"},
		{name: "a bad type modifier", sql: "CREATE TABLE items (id TEXTUAL(x))", messageContains: "a number"},
		{name: "a dangling qualifier", sql: "SELECT items.( FROM items", messageContains: "a name"},
		{name: "an incomplete null test", sql: "SELECT id FROM items WHERE id IS NOT", messageContains: "NULL"},
		{
			name:            "nesting beyond the guard",
			sql:             "SELECT " + strings.Repeat("(", maximumExpressionDepth+1) + "1" + strings.Repeat(")", maximumExpressionDepth+1) + " FROM items",
			messageContains: "nests deeper than",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := New().ParseStatements(t.Context(), testCase.sql)
			require.ErrorIs(t, err, errSyntax)
			require.ErrorContains(t, err, testCase.messageContains)
		})
	}
}

func TestApplyDDLCreatesTable(t *testing.T) {
	t.Parallel()

	engine := New()
	statement := parseOne(t, "CREATE TABLE dbo.[Item List] (id WHOLE64, label TEXTUAL(40) NOT NULL DEFAULT 'x', price EXACT(10, 2) NULL, PRIMARY KEY (id))")
	mutation, err := engine.ApplyDDL(t.Context(), statement)
	require.NoError(t, err)
	require.Equal(t, ast.MutationCreateTable, mutation.Kind)
	require.Equal(t, "DBO", mutation.SchemaName)
	require.Equal(t, "Item List", mutation.TableName)
	require.Equal(t, []string{"ID"}, mutation.PrimaryKey)
	require.Len(t, mutation.Columns, 3)

	identifier := mutation.Columns[0]
	require.Equal(t, "ID", identifier.Name)
	require.False(t, identifier.Nullable)
	require.Equal(t, typeWhole64, identifier.SQLType.EngineName)

	label := mutation.Columns[1]
	require.False(t, label.Nullable)
	require.True(t, label.HasDefault)
	require.Equal(t, 40, *label.SQLType.Length)

	price := mutation.Columns[2]
	require.True(t, price.Nullable)
	require.Equal(t, sqlmodel.TypeCategoryDecimal, price.SQLType.Category)
	require.Equal(t, 10, *price.SQLType.Precision)
	require.Equal(t, 2, *price.SQLType.Scale)
}

func TestApplyDDLIgnoresQueries(t *testing.T) {
	t.Parallel()

	mutation, err := New().ApplyDDL(t.Context(), parseOne(t, "DELETE FROM items"))
	require.NoError(t, err)
	require.Nil(t, mutation)
}

func TestApplyDDLHonoursCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := New().ApplyDDL(ctx, parseOne(t, "CREATE TABLE a (x WHOLE32)"))
	require.ErrorIs(t, err, context.Canceled)
}

func TestAnalyseSelect(t *testing.T) {
	t.Parallel()

	analysis, err := New().AnalyseQuery(t.Context(), nil, parseOne(t,
		"SELECT i.id, CONCAT(name, 'x') AS label, *, quantity + 1 next FROM items i "+
			"WHERE id = @p1 AND quantity IN (@p2, @p3) OR name IS NULL ORDER BY id DESC, name ASC"))
	require.NoError(t, err)
	require.True(t, analysis.ReadOnly)
	require.True(t, analysis.HasWhereClause)
	require.Equal(t, []ast.TableReference{{Schema: "", Name: "ITEMS", Alias: "I"}}, analysis.FromTables)
	require.Len(t, analysis.OutputColumns, 4)
	require.Equal(t, "ID", analysis.OutputColumns[0].ColumnName)
	require.Equal(t, "I", analysis.OutputColumns[0].TableAlias)
	require.Equal(t, "LABEL", analysis.OutputColumns[1].Name)
	require.IsType(t, &ast.FunctionCallExpression{}, analysis.OutputColumns[1].Expression)
	require.True(t, analysis.OutputColumns[2].IsStar)
	require.Equal(t, "NEXT", analysis.OutputColumns[3].Name)
	require.IsType(t, &ast.BinaryOpExpression{}, analysis.OutputColumns[3].Expression)

	require.Len(t, analysis.ParameterReferences, 3)
	require.Equal(t, 1, analysis.ParameterReferences[0].Number)
	require.Equal(t, sqlmodel.ParameterContextComparison, analysis.ParameterReferences[0].Context)
	require.Equal(t, &ast.ColumnReference{TableAlias: "", ColumnName: "ID"}, analysis.ParameterReferences[0].ColumnReference)
	require.Equal(t, sqlmodel.ParameterContextInList, analysis.ParameterReferences[2].Context)
	require.Equal(t, "QUANTITY", analysis.ParameterReferences[2].ColumnReference.ColumnName)
}

func TestAnalyseWrites(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		sql             string
		expectedNumbers []int
		expectedContext sqlmodel.ParameterContext
		hasWhere        bool
	}{
		{
			name:            "insert binds each value to its column",
			sql:             "INSERT INTO items (id, name) VALUES (@p1, @p2)",
			expectedNumbers: []int{1, 2},
			expectedContext: sqlmodel.ParameterContextAssignment,
			hasWhere:        false,
		},
		{
			name:            "update binds assignments and the filter",
			sql:             "UPDATE items SET name = @p2 WHERE id = @p1",
			expectedNumbers: []int{2, 1},
			expectedContext: sqlmodel.ParameterContextAssignment,
			hasWhere:        true,
		},
		{
			name:            "delete binds the filter",
			sql:             "DELETE FROM items WHERE id <> @p1",
			expectedNumbers: []int{1},
			expectedContext: sqlmodel.ParameterContextComparison,
			hasWhere:        true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := New().AnalyseQuery(t.Context(), nil, parseOne(t, testCase.sql))
			require.NoError(t, err)
			require.False(t, analysis.ReadOnly)
			require.Equal(t, testCase.hasWhere, analysis.HasWhereClause)
			numbers := make([]int, 0, len(analysis.ParameterReferences))
			for _, reference := range analysis.ParameterReferences {
				numbers = append(numbers, reference.Number)
				require.NotNil(t, reference.ColumnReference)
			}
			require.Equal(t, testCase.expectedNumbers, numbers)
			require.Equal(t, testCase.expectedContext, analysis.ParameterReferences[0].Context)
		})
	}
}

func TestInsertRecordsTarget(t *testing.T) {
	t.Parallel()

	analysis, err := New().AnalyseQuery(t.Context(), nil, parseOne(t, "INSERT INTO items (id, created) VALUES (@p1, NOW())"))
	require.NoError(t, err)
	require.Equal(t, "ITEMS", analysis.InsertTable)
	require.Equal(t, []string{"ID", "CREATED"}, analysis.InsertColumns)
	require.Len(t, analysis.ParameterReferences, 1)
}
