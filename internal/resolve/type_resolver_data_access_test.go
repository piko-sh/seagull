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
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func dataAccessCatalogue() *sqlmodel.Catalogue {
	catalogue := fakedialect.NewCatalogue("public")
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	integer := sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4")
	reader := sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "", Type: text}}, integer,
		sqlmodel.FunctionNullableReturnsNullOnNull, sqlmodel.WithFunctionName("measure"))
	reader.DataAccess = sqlmodel.DataAccessReadOnly
	writer := sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "", Type: text}}, integer,
		sqlmodel.FunctionNullableReturnsNullOnNull, sqlmodel.WithFunctionName("touch"))
	writer.DataAccess = sqlmodel.DataAccessModifiesData
	catalogue.Schemas["public"].Functions["measure"] = []*sqlmodel.Function{reader}
	catalogue.Schemas["public"].Functions["touch"] = []*sqlmodel.Function{writer}
	return catalogue
}

func callOf(name string, arguments ...ast.Expression) *ast.FunctionCallExpression {
	return &ast.FunctionCallExpression{FunctionName: name, Arguments: arguments}
}

func comparedWithOne(left ast.Expression) ast.Expression {
	return &ast.ComparisonExpression{Left: left, Operator: ">", Right: &ast.LiteralExpression{TypeName: "integer"}}
}

func TestCallsDataModifyingFunction(t *testing.T) {
	t.Parallel()

	name := &ast.ColumnRefExpression{ColumnName: "name"}
	nested := &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{{Expression: comparedWithOne(callOf("touch", name)), Clause: ast.ClauseWhere}}}

	tests := []struct {
		name     string
		analysis *ast.RawQueryAnalysis
		want     bool
	}{
		{name: "a read-only call in WHERE", analysis: &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{
			{Expression: comparedWithOne(callOf("measure", name)), Clause: ast.ClauseWhere}}}, want: false},
		{name: "a writing call in WHERE", analysis: &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{
			{Expression: comparedWithOne(callOf("touch", name)), Clause: ast.ClauseWhere}}}, want: true},
		{name: "an unknown call in ORDER BY", analysis: &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{
			{Expression: callOf("mystery", name), Clause: ast.ClauseOrderBy}}}, want: true},
		{name: "a writing call inside a comparison in the select list", analysis: &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{
			{Name: "flag", Expression: comparedWithOne(callOf("touch", name))}}}, want: true},
		{name: "a writing call nested in a read-only one", analysis: &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{
			{Expression: comparedWithOne(callOf("measure", callOf("touch", name))), Clause: ast.ClauseHaving}}}, want: true},
		{name: "a writing call in an EXISTS subquery", analysis: &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{
			{Expression: &ast.ExistsExpression{InnerQuery: nested}, Clause: ast.ClauseWhere}}}, want: true},
		{name: "a writing call in a predicate subquery", analysis: &ast.RawQueryAnalysis{PredicateSubqueries: []*ast.RawQueryAnalysis{nested}}, want: true},
		{name: "a writing call a struct constructor holds", analysis: &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{
			{Name: "pair", Expression: &ast.StructExpression{Names: []string{"written"}, Values: []ast.Expression{callOf("touch", name)}}}}}, want: true},
		{name: "no calls", analysis: &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{
			{Expression: comparedWithOne(name), Clause: ast.ClauseWhere}}}, want: false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := newTypeResolverWithCatalogue(dataAccessCatalogue())
			assert.Equal(t, testCase.want, resolver.CallsDataModifyingFunction(t.Context(), testCase.analysis, setupTypeResolverScope()))
			assert.Empty(t, resolver.notices, "typing the calls leaves no notices behind")
		})
	}
}

func TestFunctionsCalledInFromDecideWhetherTheQueryChangesData(t *testing.T) {
	t.Parallel()

	text := &ast.LiteralExpression{TypeName: "text"}
	testCases := []struct {
		name      string
		reference ast.RawTableValuedFunctionReference
		want      bool
	}{
		{name: "a function that changes data", reference: ast.RawTableValuedFunctionReference{FunctionName: "touch",
			Arguments: []ast.Expression{text}}, want: true},
		{name: "a schema-qualified function that changes data", reference: ast.RawTableValuedFunctionReference{FunctionName: "public.touch",
			Arguments: []ast.Expression{text}}, want: true},
		{name: "a function that only reads", reference: ast.RawTableValuedFunctionReference{FunctionName: "measure",
			Arguments: []ast.Expression{text}}, want: false},
		{name: "a table-valued function the resolver does not know", reference: ast.RawTableValuedFunctionReference{FunctionName: "json_each",
			Arguments: []ast.Expression{text}}, want: false},
		{name: "a function that changes data among several calls", reference: ast.RawTableValuedFunctionReference{FunctionName: "measure",
			Functions: []ast.TableFunctionCall{{FunctionName: "measure", Arguments: []ast.Expression{text}},
				{FunctionName: "touch", Arguments: []ast.Expression{text}}}}, want: true},
		{name: "a function that changes data in an argument", reference: ast.RawTableValuedFunctionReference{FunctionName: "json_each",
			Arguments: []ast.Expression{callOf("touch", text)}}, want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := newTypeResolverWithCatalogue(dataAccessCatalogue())
			analysis := &ast.RawQueryAnalysis{RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{testCase.reference}}
			require.Equal(t, testCase.want, resolver.CallsDataModifyingFunction(t.Context(), analysis, setupTypeResolverScope()))
		})
	}
}

func TestCallsDataModifyingFunctionStopsWhenTheDialectIsInterrupted(t *testing.T) {
	t.Parallel()

	text := &ast.LiteralExpression{TypeName: "text"}
	testCases := []struct {
		analysis *ast.RawQueryAnalysis
		name     string
	}{
		{name: "a call in the select list", analysis: &ast.RawQueryAnalysis{
			OutputColumns:     []ast.RawOutputColumn{{Name: "probed", Expression: callOf("probe", text)}},
			ClauseExpressions: []ast.ClauseExpression{{Expression: comparedWithOne(callOf("later", text)), Clause: ast.ClauseWhere}},
		}},
		{name: "a function a FROM item calls", analysis: &ast.RawQueryAnalysis{
			RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{{FunctionName: "probe", Arguments: []ast.Expression{text}}},
			PredicateSubqueries: []*ast.RawQueryAnalysis{{ClauseExpressions: []ast.ClauseExpression{
				{Expression: comparedWithOne(callOf("later", text)), Clause: ast.ClauseWhere},
			}}},
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var asked []string
			engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{}, ResolveFunc: func(_ context.Context, name string, _ []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				asked = append(asked, name)
				return nil, fmt.Errorf("walk: %w", context.DeadlineExceeded)
			}}
			catalogue := dataAccessCatalogue()
			resolver := NewTypeResolver(catalogue, NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, engine), engine)

			modifying := resolver.CallsDataModifyingFunction(t.Context(), testCase.analysis, setupTypeResolverScope())

			require.False(t, modifying, "an interrupted call is not taken to modify data")
			require.Equal(t, []string{"probe"}, asked)
		})
	}
}

func probedNestedExists(levels int) *ast.RawQueryAnalysis {
	text := &ast.LiteralExpression{TypeName: "text"}
	query := &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{{Expression: comparedWithOne(callOf("probe", text)), Clause: ast.ClauseWhere}}}
	for range levels {
		condition := &ast.LogicalOpExpression{Operator: "AND", Operands: []ast.Expression{
			comparedWithOne(callOf("probe", text)), &ast.ExistsExpression{InnerQuery: query},
		}}
		query = &ast.RawQueryAnalysis{
			ClauseExpressions:   []ast.ClauseExpression{{Expression: condition, Clause: ast.ClauseWhere}},
			PredicateSubqueries: []*ast.RawQueryAnalysis{query},
		}
	}
	return query
}

func TestCallsDataModifyingFunctionWalksEachNestedQueryOnce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		levels int
	}{
		{name: "no nested query", levels: 0},
		{name: "one EXISTS", levels: 1},
		{name: "forty nested EXISTS", levels: 40},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			asked := 0
			engine := &fakedialect.FunctionResolverEngine{Engine: &fakedialect.Engine{}, ResolveFunc: func(_ context.Context, _ string, _ []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
				asked++
				return &ast.FunctionResolution{
					ReturnType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4"),
					DataAccess: sqlmodel.DataAccessReadOnly,
				}, nil
			}}
			catalogue := dataAccessCatalogue()
			resolver := NewTypeResolver(catalogue, NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, engine), engine)
			analysis := probedNestedExists(testCase.levels)

			modifying := true
			scaling.Within(t, time.Second, func(ctx context.Context) {
				modifying = resolver.CallsDataModifyingFunction(ctx, analysis, setupTypeResolverScope())
			})

			require.False(t, modifying)
			require.Equal(t, testCase.levels+1, asked, "each call is typed once")
		})
	}
}

func chainedSum(first ast.Expression, terms int) ast.Expression {
	expression := first
	for range terms {
		expression = &ast.BinaryOpExpression{Left: expression, Operator: "+", Right: &ast.LiteralExpression{TypeName: "integer"}}
	}
	return expression
}

func TestCallsDataModifyingFunctionTakesAQueryDeeperThanItReadsToChangeData(t *testing.T) {
	t.Parallel()

	text := &ast.LiteralExpression{TypeName: "text"}
	testCases := []struct {
		name      string
		first     ast.Expression
		terms     int
		want      bool
		wantLimit bool
	}{
		{name: "a short chain of reads", first: callOf("measure", text), terms: 10, want: false, wantLimit: false},
		{name: "a short chain with a write", first: callOf("touch", text), terms: 10, want: true, wantLimit: false},
		{name: "a long chain with a write past the limit", first: callOf("touch", text), terms: 300, want: true, wantLimit: true},
		{name: "a long chain of reads past the limit", first: callOf("measure", text), terms: 300, want: true, wantLimit: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := newTypeResolverWithCatalogue(dataAccessCatalogue())
			analysis := &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{
				{Expression: comparedWithOne(chainedSum(testCase.first, testCase.terms)), Clause: ast.ClauseWhere},
			}}

			require.Equal(t, testCase.want, resolver.CallsDataModifyingFunction(t.Context(), analysis, setupTypeResolverScope()))

			limits := resolver.TakeDepthLimits()
			if !testCase.wantLimit {
				require.Empty(t, limits)
				return
			}
			require.Len(t, limits, 1)
			require.Equal(t, diag.CodeDepthLimit, limits[0].Code)
			require.Equal(t, diag.SeverityWarning, limits[0].Severity)
			require.Contains(t, limits[0].Message, "is taken to change data")
			require.Empty(t, resolver.TakeDepthLimits(), "taking the limits clears them")
		})
	}
}
