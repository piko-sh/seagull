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

func TestParametersAreTypedInTheQueryThatComparesThem(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
	text := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users", sqlmodel.Column{Name: "code", SQLType: integer})
	catalogue.Schemas["public"].Tables["tags"] = fakedialect.NewTable("tags", sqlmodel.Column{Name: "code", SQLType: text},
		sqlmodel.Column{Name: "label", SQLType: text})
	compared := func(number int, qualifier, column string) ast.RawParameterReference {
		return ast.RawParameterReference{Number: number, Context: sqlmodel.ParameterContextComparison,
			ColumnReference: &ast.ColumnReference{TableAlias: qualifier, ColumnName: column}}
	}
	comparedWithCode := func(number int) ast.RawParameterReference { return compared(number, "", "code") }
	secondArm := &ast.RawQueryAnalysis{
		RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "d", JoinKind: ast.JoinInner, InnerQuery: &ast.RawQueryAnalysis{
			FromTables:    []ast.TableReference{{Name: "tags"}},
			OutputColumns: []ast.RawOutputColumn{{Name: "tag", ColumnName: "label"}},
		}}},
		OutputColumns:       []ast.RawOutputColumn{{Name: "tag", TableAlias: "d", ColumnName: "tag"}},
		ParameterReferences: []ast.RawParameterReference{compared(2, "d", "tag")},
	}
	cteBody := &ast.RawQueryAnalysis{
		RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "d", JoinKind: ast.JoinInner, InnerQuery: &ast.RawQueryAnalysis{
			FromTables:    []ast.TableReference{{Name: "tags"}},
			OutputColumns: []ast.RawOutputColumn{{Name: "code", ColumnName: "code"}},
		}}},
		OutputColumns:       []ast.RawOutputColumn{{Name: "code", TableAlias: "d", ColumnName: "code"}},
		ParameterReferences: []ast.RawParameterReference{comparedWithCode(3)},
	}
	query := &ast.RawQueryAnalysis{
		CTEDefinitions: []ast.RawCTEDefinition{{Name: "tagged", OutputColumns: cteBody.OutputColumns, Query: cteBody,
			ParameterReferences: cteBody.ParameterReferences}},
		FromTables:          []ast.TableReference{{Name: "users"}},
		OutputColumns:       []ast.RawOutputColumn{{Name: "code", ColumnName: "code"}},
		CompoundBranches:    []ast.RawCompoundBranch{{Operator: ast.CompoundUnionAll, Query: secondArm}},
		ParameterReferences: []ast.RawParameterReference{comparedWithCode(1), compared(2, "d", "tag"), comparedWithCode(3)},
	}

	builder := NewCatalogueScopeBuilder(&fakedialect.Engine{}, catalogue)
	scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	require.Empty(t, builder.BuildQueryScope(t.Context(), query, scope))
	parameters, problems := builder.TypeResolver.ResolveParameters(t.Context(), query, scope, nil)

	require.Empty(t, problems)
	require.Len(t, parameters, 3)
	require.Equal(t, sqlmodel.TypeCategoryInteger, parameters[0].SQLType.Category, "the first arm compares with users.code")
	require.Equal(t, sqlmodel.TypeCategoryText, parameters[1].SQLType.Category, "the second arm compares with its derived table's tag")
	require.Equal(t, sqlmodel.TypeCategoryText, parameters[2].SQLType.Category, "the CTE body compares with its derived table's code")
}

func TestCollectDirectSubqueriesFindsTheSubqueriesAnItemHolds(t *testing.T) {
	t.Parallel()

	inner := &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Name: "code", ColumnName: "code"}}}
	scalar := &ast.ScalarSubqueryExpression{InnerQuery: inner}
	testCases := []struct {
		expression ast.Expression
		name       string
	}{
		{name: "a subquery that is the item", expression: scalar},
		{name: "a subquery a struct constructor holds", expression: &ast.StructExpression{Names: []string{"code"}, Values: []ast.Expression{scalar}}},
		{name: "a subquery a row holds", expression: &ast.RowValueExpression{Elements: []ast.Expression{scalar}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			raw := &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Name: "item", Expression: testCase.expression}}}

			require.Equal(t, []*ast.RawQueryAnalysis{inner}, collectDirectSubqueries(raw))
		})
	}
}

func TestAParameterASubqueryCannotTypeIsLeftToTheFlatPassAndReportedOnce(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
	testCases := []struct {
		name         string
		column       string
		wantProblems int
		wantType     sqlmodel.SQLTypeCategory
	}{
		{name: "a column the subquery's table has", column: "code", wantProblems: 0, wantType: sqlmodel.TypeCategoryInteger},
		{name: "a column no table has", column: "missing", wantProblems: 1, wantType: sqlmodel.TypeCategoryUnknown},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users", sqlmodel.Column{Name: "code", SQLType: integer})
			engine := &fakedialect.Engine{}
			resolver := NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine)
			reference := ast.RawParameterReference{Number: 1, Context: sqlmodel.ParameterContextComparison,
				ColumnReference: &ast.ColumnReference{TableAlias: "", ColumnName: testCase.column}}
			subquery := &ast.RawQueryAnalysis{FromTables: []ast.TableReference{{Name: "users"}}, ParameterReferences: []ast.RawParameterReference{reference}}
			analysis := &ast.RawQueryAnalysis{
				PredicateSubqueries: []*ast.RawQueryAnalysis{subquery},
				ParameterReferences: []ast.RawParameterReference{reference},
			}

			parameters, problems := resolver.ResolveParameters(t.Context(), analysis, resolver.NewRootScope(ScopeKindQuery), nil)

			require.Len(t, problems, testCase.wantProblems, "%v", problems)
			require.Len(t, parameters, 1)
			require.Equal(t, testCase.wantType, parameters[0].SQLType.Category)
		})
	}
}
