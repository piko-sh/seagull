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

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func ungroupedBuilder(rule dialect.UngroupedColumnRule) *ScopeBuilder {
	base := groupingBuilder()
	engine := &fakedialect.Engine{BuiltinFunctionsFunc: base.TypeResolver.engine.(*fakedialect.Engine).BuiltinFunctionsFunc}
	info := engine.Info()
	info.UngroupedColumns = rule
	engine.InfoFunc = func() dialect.Info { return info }
	catalogue := base.catalogue
	return NewScopeBuilder(engine, catalogue, NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine))
}

func salesQuery(groupBy []ast.ColumnReference, items ...ast.RawOutputColumn) *ast.RawQueryAnalysis {
	return &ast.RawQueryAnalysis{FromTables: []ast.TableReference{{Name: "sales"}}, OutputColumns: items, GroupByColumns: groupBy}
}

func countItem() ast.RawOutputColumn {
	return ast.RawOutputColumn{Name: "total", Expression: call("count", groupingColumn("", "amount"))}
}

func resolveUngrouped(t *testing.T, rule dialect.UngroupedColumnRule, raw *ast.RawQueryAnalysis) Projection {
	t.Helper()

	builder := ungroupedBuilder(rule)
	scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	require.Empty(t, builder.BuildQueryScope(t.Context(), raw, scope))
	return builder.TypeResolver.ResolveProjection(t.Context(), raw.OutputColumns, nil, scope)
}

func TestColumnsBesideAnAggregateWithoutGroupByFollowTheDialect(t *testing.T) {
	t.Parallel()

	region := ast.RawOutputColumn{Name: "region", Expression: located("region", 7)}
	testCases := []struct {
		raw          *ast.RawQueryAnalysis
		name         string
		wantNullable map[string]bool
		wantProblems []diag.ErrorSeverity
		rule         dialect.UngroupedColumnRule
	}{
		{
			name: "an engine returning the row makes the column nullable", rule: dialect.UngroupedColumnsNullable,
			raw:          salesQuery(nil, region, countItem()),
			wantNullable: map[string]bool{"region": true, "total": false},
		},
		{
			name: "an engine whose mode decides warns and makes the column nullable", rule: dialect.UngroupedColumnsWarned,
			raw:          salesQuery(nil, region, countItem()),
			wantNullable: map[string]bool{"region": true, "total": false},
			wantProblems: []diag.ErrorSeverity{diag.SeverityWarning},
		},
		{
			name: "an engine rejecting the query reports an error", rule: dialect.UngroupedColumnsRejected,
			raw:          salesQuery(nil, region, countItem()),
			wantNullable: map[string]bool{"region": true, "total": false},
			wantProblems: []diag.ErrorSeverity{diag.SeverityError},
		},
		{
			name: "a column read through an expression outside the aggregate", rule: dialect.UngroupedColumnsRejected,
			raw:          salesQuery(nil, ast.RawOutputColumn{Name: "loud", Expression: call("lower", located("region", 13))}, countItem()),
			wantNullable: map[string]bool{"loud": true, "total": false},
			wantProblems: []diag.ErrorSeverity{diag.SeverityError},
		},
		{
			name: "a query grouping by the column", rule: dialect.UngroupedColumnsRejected,
			raw:          salesQuery([]ast.ColumnReference{{TableAlias: "", ColumnName: "region"}}, region, countItem()),
			wantNullable: map[string]bool{"region": false, "total": false},
		},
		{
			name: "a query aggregating alone", rule: dialect.UngroupedColumnsRejected,
			raw:          salesQuery(nil, countItem()),
			wantNullable: map[string]bool{"total": false},
		},
		{
			name: "a query without an aggregate", rule: dialect.UngroupedColumnsRejected,
			raw:          salesQuery(nil, region),
			wantNullable: map[string]bool{"region": false},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			projection := resolveUngrouped(t, testCase.rule, testCase.raw)

			nullability := make(map[string]bool, len(projection.Columns))
			for index := range projection.Columns {
				nullability[projection.Columns[index].Name] = projection.Columns[index].Nullable
			}
			require.Equal(t, testCase.wantNullable, nullability)
			var severities []diag.ErrorSeverity
			for _, problem := range projection.Problems {
				require.Equal(t, diag.CodeUngroupedColumn, problem.Code)
				severities = append(severities, problem.Severity)
			}
			require.Equal(t, testCase.wantProblems, severities)
		})
	}
}

func TestAnUngroupedColumnIsReportedWhereItIsWritten(t *testing.T) {
	t.Parallel()

	projection := resolveUngrouped(t, dialect.UngroupedColumnsRejected,
		salesQuery(nil, ast.RawOutputColumn{Name: "region", Expression: located("region", 7)}, countItem()))

	require.Len(t, projection.Problems, 1)
	require.Equal(t, Span{Start: 7, End: 13}, projection.Problems[0].Span)
}

func TestAScalarSubqueryGivingOneRowIsAsNullableAsItsColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		inner        *ast.RawQueryAnalysis
		name         string
		wantNullable bool
	}{
		{name: "a count without GROUP BY", inner: salesQuery(nil, countItem()), wantNullable: false},
		{name: "a count grouped by a column", inner: salesQuery([]ast.ColumnReference{{TableAlias: "", ColumnName: "region"}}, countItem()), wantNullable: true},
		{name: "a column without an aggregate", inner: salesQuery(nil, ast.RawOutputColumn{Name: "amount", Expression: groupingColumn("", "amount")}), wantNullable: true},
		{
			name: "a count filtered by HAVING",
			inner: &ast.RawQueryAnalysis{FromTables: []ast.TableReference{{Name: "sales"}}, OutputColumns: []ast.RawOutputColumn{countItem()},
				ClauseExpressions: []ast.ClauseExpression{{Clause: ast.ClauseHaving, Expression: call("count", groupingColumn("", "amount"))}}},
			wantNullable: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := ungroupedBuilder(dialect.UngroupedColumnsNullable)
			scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
			require.Empty(t, builder.BuildQueryScope(t.Context(), salesQuery(nil), scope))

			sqlType, nullable, err := builder.TypeResolver.ResolveExpressionType(t.Context(), &ast.ScalarSubqueryExpression{InnerQuery: testCase.inner}, scope, new(false))

			require.NoError(t, err)
			require.Equal(t, sqlmodel.TypeCategoryInteger, sqlType.Category)
			require.Equal(t, testCase.wantNullable, nullable)
		})
	}
}
