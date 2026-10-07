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

package resolve

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func groupingBuilder() *ScopeBuilder {
	catalogue := fakedialect.NewCatalogue("public")
	integer := sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger}
	text := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	catalogue.Schemas["public"].Tables["sales"] = &sqlmodel.Table{
		Name:   "sales",
		Schema: "public",
		Columns: []sqlmodel.Column{
			{Name: "region", SQLType: text},
			{Name: "city", SQLType: text},
			{Name: "amount", SQLType: integer},
		},
	}
	engine := &fakedialect.Engine{BuiltinFunctionsFunc: func() *ast.FunctionCatalogue {
		return &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{
			"count": {sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "value", Type: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")}}, integer,
				sqlmodel.FunctionNullableNeverNull, sqlmodel.WithFunctionName("count"), sqlmodel.WithAggregate())},
			"lower": {sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "value", Type: text}}, text,
				sqlmodel.FunctionNullableReturnsNullOnNull, sqlmodel.WithFunctionName("lower"))},
		}}
	}}
	return NewScopeBuilder(engine, catalogue, NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine))
}

func groupingColumn(qualifier, name string) *ast.ColumnRefExpression {
	return &ast.ColumnRefExpression{TableAlias: qualifier, ColumnName: name}
}

func salesSubtotals(sets *ast.GroupingSets) *ast.RawQueryAnalysis {
	return &ast.RawQueryAnalysis{
		FromTables: []ast.TableReference{{Name: "sales"}},
		OutputColumns: []ast.RawOutputColumn{
			{Name: "region", ColumnName: "region", Expression: groupingColumn("", "region")},
			{Name: "city", ColumnName: "city", TableAlias: "sales", Expression: groupingColumn("sales", "city")},
			{Name: "loud_city", Expression: &ast.FunctionCallExpression{FunctionName: "lower", Arguments: []ast.Expression{groupingColumn("", "city")}}},
			{Name: "fallback", Expression: &ast.CoalesceExpression{Arguments: []ast.Expression{groupingColumn("", "city"), groupingColumn("", "region")}}},
			{Name: "sales", Expression: &ast.FunctionCallExpression{FunctionName: "count", Arguments: []ast.Expression{groupingColumn("", "city")}}},
		},
		GroupByColumns: []ast.ColumnReference{{TableAlias: "", ColumnName: "region"}, {TableAlias: "", ColumnName: "city"}},
		GroupingSets:   sets,
	}
}

func resolveGrouped(t *testing.T, raw *ast.RawQueryAnalysis) map[string]bool {
	t.Helper()

	builder := groupingBuilder()
	scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	require.Empty(t, builder.BuildQueryScope(t.Context(), raw, scope))
	columns, _, problems := builder.TypeResolver.ResolveOutputColumns(t.Context(), raw.OutputColumns, scope)
	require.Empty(t, problems)
	nullability := make(map[string]bool, len(columns))
	for index := range columns {
		nullability[columns[index].Name] = columns[index].Nullable
	}
	return nullability
}

func TestGroupingSetsMakeTheKeysTheyLeaveOutNullable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sets     *ast.GroupingSets
		expected map[string]bool
	}{
		{
			name:     "a plain GROUP BY leaves every column as it is",
			sets:     nil,
			expected: map[string]bool{"region": false, "city": false, "loud_city": false, "fallback": false, "sales": false},
		},
		{
			name:     "a rolled-up key is NULL in the subtotal rows, and so is what is computed from it",
			sets:     &ast.GroupingSets{Keys: []ast.Expression{groupingColumn("", "city")}},
			expected: map[string]bool{"region": false, "city": true, "loud_city": true, "fallback": false, "sales": false},
		},
		{
			name:     "a qualified key names the same column",
			sets:     &ast.GroupingSets{Keys: []ast.Expression{groupingColumn("sales", "region"), groupingColumn("sales", "city")}},
			expected: map[string]bool{"region": true, "city": true, "loud_city": true, "fallback": true, "sales": false},
		},
		{
			name:     "a key inside an expression makes its columns nullable",
			sets:     &ast.GroupingSets{Keys: []ast.Expression{&ast.FunctionCallExpression{FunctionName: "lower", Arguments: []ast.Expression{groupingColumn("", "region")}}}},
			expected: map[string]bool{"region": true, "city": false, "loud_city": false, "fallback": false, "sales": false},
		},
		{
			name:     "an engine that fills defaults leaves every column as it is",
			sets:     &ast.GroupingSets{Keys: []ast.Expression{groupingColumn("", "city")}, FillsDefaults: true},
			expected: map[string]bool{"region": false, "city": false, "loud_city": false, "fallback": false, "sales": false},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, resolveGrouped(t, salesSubtotals(testCase.sets)))
		})
	}
}

func TestGroupingSetsKeyNamingASelectItemByAliasMakesTheItemNullable(t *testing.T) {
	t.Parallel()

	raw := &ast.RawQueryAnalysis{
		FromTables: []ast.TableReference{{Name: "sales"}},
		OutputColumns: []ast.RawOutputColumn{
			{Name: "place", Expression: &ast.FunctionCallExpression{FunctionName: "lower", Arguments: []ast.Expression{groupingColumn("", "city")}}},
			{Name: "sales", Expression: &ast.FunctionCallExpression{FunctionName: "count", Arguments: []ast.Expression{groupingColumn("", "city")}}},
		},
		GroupingSets: &ast.GroupingSets{Keys: []ast.Expression{groupingColumn("", "place")}},
	}

	require.Equal(t, map[string]bool{"place": true, "sales": false}, resolveGrouped(t, raw))
}

func TestGroupingSetsReachTheQueryOfADerivedTable(t *testing.T) {
	t.Parallel()

	inner := salesSubtotals(&ast.GroupingSets{Keys: []ast.Expression{groupingColumn("", "city")}})
	outer := &ast.RawQueryAnalysis{
		OutputColumns: []ast.RawOutputColumn{
			{Name: "city", ColumnName: "city", Expression: groupingColumn("", "city")},
			{Name: "region", ColumnName: "region", Expression: groupingColumn("", "region")},
		},
		RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "subtotals", InnerQuery: inner, JoinKind: ast.JoinInner}},
	}

	require.Equal(t, map[string]bool{"city": true, "region": false}, resolveGrouped(t, outer))
}
