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

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type arrayTypingEngine struct {
	*fakedialect.Engine
}

func (arrayTypingEngine) ArrayOf(element sqlmodel.SQLType) sqlmodel.SQLType {
	array := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, element.EngineName+"[]")
	array.ElementType = &element
	return array
}

func parameter(number int) *ast.ParameterExpression {
	return &ast.ParameterExpression{Number: number}
}

func filterCrew(body ast.Expression) *ast.FunctionCallExpression {
	return &ast.FunctionCallExpression{FunctionName: "apply", Arguments: []ast.Expression{column("crew"), lambda(body, "x")}}
}

func TestAParameterComparedWithALambdaParameterTakesItsType(t *testing.T) {
	t.Parallel()

	compared := &ast.ComparisonExpression{Operator: ">", Left: column("x"), Right: parameter(1)}
	testCases := []struct {
		name         string
		analysis     *ast.RawQueryAnalysis
		wantCategory sqlmodel.SQLTypeCategory
	}{
		{
			name: "in the select list",
			analysis: &ast.RawQueryAnalysis{
				OutputColumns:       []ast.RawOutputColumn{{Name: "kept", Expression: filterCrew(compared)}},
				ParameterReferences: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison}},
			},
			wantCategory: sqlmodel.TypeCategoryInteger,
		},
		{
			name: "in a clause",
			analysis: &ast.RawQueryAnalysis{
				ClauseExpressions:   []ast.ClauseExpression{{Expression: filterCrew(compared)}},
				ParameterReferences: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison}},
			},
			wantCategory: sqlmodel.TypeCategoryInteger,
		},
		{
			name: "named as the column it is compared with",
			analysis: &ast.RawQueryAnalysis{
				ClauseExpressions: []ast.ClauseExpression{{Expression: filterCrew(compared)}},
				ParameterReferences: []ast.RawParameterReference{{
					Number: 1, Context: sqlmodel.ParameterContextComparison, ColumnReference: &ast.ColumnReference{ColumnName: "x"},
				}},
			},
			wantCategory: sqlmodel.TypeCategoryInteger,
		},
		{
			name: "on the left of the comparison",
			analysis: &ast.RawQueryAnalysis{
				ClauseExpressions:   []ast.ClauseExpression{{Expression: filterCrew(&ast.ComparisonExpression{Operator: "=", Left: parameter(1), Right: column("x")})}},
				ParameterReferences: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison}},
			},
			wantCategory: sqlmodel.TypeCategoryInteger,
		},
		{
			name: "compared with nothing typed",
			analysis: &ast.RawQueryAnalysis{
				ClauseExpressions:   []ast.ClauseExpression{{Expression: filterCrew(&ast.ComparisonExpression{Operator: "=", Left: parameter(1), Right: column("y")})}},
				ParameterReferences: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison}},
			},
			wantCategory: sqlmodel.TypeCategoryUnknown,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope, _ := newLambdaTestResolver(t)

			parameters, problems := resolver.ResolveParameters(t.Context(), testCase.analysis, scope, nil)

			require.Empty(t, problems)
			require.Len(t, parameters, 1)
			require.Equal(t, testCase.wantCategory, parameters[0].SQLType.Category)
		})
	}
}

func TestAParameterInAnINListOrBetweenTakesTheTypeOfWhatItIsComparedWith(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		expression ast.Expression
		wantEngine string
	}{
		{name: "a value of an IN list", expression: &ast.InListExpression{Inner: column("x"), Values: []ast.Expression{parameter(1)}}, wantEngine: "int4"},
		{name: "a bound of a BETWEEN", expression: &ast.BetweenExpression{Inner: column("x"), Low: parameter(1), High: column("x")}, wantEngine: "int4"},
		{name: "the value a BETWEEN tests", expression: &ast.BetweenExpression{Inner: parameter(1), Low: column("x"), High: column("x")}, wantEngine: "int4"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope, _ := newLambdaTestResolver(t)
			analysis := &ast.RawQueryAnalysis{
				ClauseExpressions:   []ast.ClauseExpression{{Expression: filterCrew(testCase.expression)}},
				ParameterReferences: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison}},
			}

			parameters, problems := resolver.ResolveParameters(t.Context(), analysis, scope, nil)

			require.Empty(t, problems)
			require.Len(t, parameters, 1)
			require.Equal(t, testCase.wantEngine, parameters[0].SQLType.EngineName)
		})
	}
}

func TestAParameterKeepsTheTypeOfTheColumnItNames(t *testing.T) {
	t.Parallel()

	resolver, scope, _ := newLambdaTestResolver(t)
	analysis := &ast.RawQueryAnalysis{
		ClauseExpressions: []ast.ClauseExpression{{Expression: filterCrew(&ast.ComparisonExpression{Operator: "=", Left: column("x"), Right: parameter(1)})}},
		ParameterReferences: []ast.RawParameterReference{{
			Number: 1, Context: sqlmodel.ParameterContextComparison, ColumnReference: &ast.ColumnReference{ColumnName: "name"},
		}},
	}

	parameters, problems := resolver.ResolveParameters(t.Context(), analysis, scope, nil)

	require.Empty(t, problems)
	require.Len(t, parameters, 1)
	require.Equal(t, "text", parameters[0].SQLType.EngineName)
}

func TestAParameterComparedWithAValueThatMayBeNullTakesNoNull(t *testing.T) {
	t.Parallel()

	resolver, scope, _ := newLambdaTestResolver(t)
	ratings := &ast.FunctionCallExpression{FunctionName: "apply", Arguments: []ast.Expression{
		column("ratings"), lambda(&ast.ComparisonExpression{Operator: ">", Left: column("x"), Right: parameter(1)}, "x"),
	}}
	analysis := &ast.RawQueryAnalysis{
		ClauseExpressions:   []ast.ClauseExpression{{Expression: ratings}},
		ParameterReferences: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison}},
	}

	parameters, problems := resolver.ResolveParameters(t.Context(), analysis, scope, nil)

	require.Empty(t, problems)
	require.Len(t, parameters, 1)
	require.Equal(t, "int4", parameters[0].SQLType.EngineName)
	require.False(t, parameters[0].Nullable)
}

func TestAParameterComparedInsideASubqueryIsTypedInTheSubquerysScope(t *testing.T) {
	t.Parallel()

	resolver, _, _ := newLambdaTestResolver(t)
	subquery := &ast.RawQueryAnalysis{
		FromTables: []ast.TableReference{{Name: "boats"}},
		ClauseExpressions: []ast.ClauseExpression{{Expression: &ast.ComparisonExpression{
			Operator: "=", Left: &ast.ArraySubscriptExpression{Array: column("crew"), Index: &ast.LiteralExpression{TypeName: "integer"}},
			Right: parameter(1),
		}}},
		ParameterReferences: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison}},
	}
	analysis := &ast.RawQueryAnalysis{
		PredicateSubqueries: []*ast.RawQueryAnalysis{subquery},
		ParameterReferences: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison}},
	}

	parameters, problems := resolver.ResolveParameters(t.Context(), analysis, resolver.NewRootScope(ScopeKindQuery), nil)

	require.Empty(t, problems)
	require.Len(t, parameters, 1)
	require.Equal(t, "int4", parameters[0].SQLType.EngineName)
}

func TestAParameterComparedWithASelectListAliasFollowsTheDialectsAliasOperands(t *testing.T) {
	t.Parallel()

	bumped := &ast.BinaryOpExpression{Operator: "+", Left: column("id"), Right: &ast.LiteralExpression{TypeName: "int4"}}
	testCases := []struct {
		name             string
		alias            string
		operands         ast.Clause
		clause           ast.Clause
		expectedCategory sqlmodel.SQLTypeCategory
		expectProblem    bool
	}{
		{name: "an alias the clause may name types the parameter", alias: "bumped", operands: ast.ClauseWhere,
			clause: ast.ClauseWhere, expectedCategory: sqlmodel.TypeCategoryInteger},
		{name: "an alias in a clause the dialect does not list is unknown", alias: "bumped", operands: ast.ClauseHaving,
			clause: ast.ClauseWhere, expectedCategory: sqlmodel.TypeCategoryUnknown, expectProblem: true},
		{name: "a dialect listing no clause leaves the alias unknown", alias: "bumped", operands: 0,
			clause: ast.ClauseWhere, expectedCategory: sqlmodel.TypeCategoryUnknown, expectProblem: true},
		{name: "a column the clause can see wins over an alias of its name", alias: "name", operands: ast.ClauseWhere,
			clause: ast.ClauseWhere, expectedCategory: sqlmodel.TypeCategoryText},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, base := newExpressionTestResolver(t)
			rules := testScopeRules
			rules.AliasOperands = testCase.operands
			scope := NewRootScopeChain(ScopeKindQuery, rules)
			users, _ := base.Table("users")
			scope.AddTable(ast.TableReference{Name: "users"}, ast.JoinInner, users.CatalogueTable)
			analysis := &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{{Name: testCase.alias, Expression: bumped}},
				ClauseExpressions: []ast.ClauseExpression{{Clause: testCase.clause,
					Expression: &ast.ComparisonExpression{Operator: ">", Left: column(testCase.alias), Right: parameter(1)}}},
				ParameterReferences: []ast.RawParameterReference{{
					Number: 1, Context: sqlmodel.ParameterContextComparison, ColumnReference: &ast.ColumnReference{ColumnName: testCase.alias},
				}},
			}

			projection := resolver.ResolveProjection(t.Context(), analysis.OutputColumns, nil, scope)
			require.Empty(t, projection.Problems)
			parameters, problems := resolver.ResolveParameters(t.Context(), analysis, scope, nil)

			require.Len(t, parameters, 1)
			require.Equal(t, testCase.expectedCategory, parameters[0].SQLType.Category)
			require.Equal(t, testCase.expectProblem, len(problems) > 0)
		})
	}
}

func TestAParameterComparedWithAnAliasThatShadowsAColumnTakesTheAliasType(t *testing.T) {
	t.Parallel()

	bumped := &ast.BinaryOpExpression{Operator: "+", Left: column("id"), Right: &ast.LiteralExpression{TypeName: "int4"}}
	testCases := []struct {
		name             string
		shadowing        ast.Clause
		clause           ast.Clause
		expectedCategory sqlmodel.SQLTypeCategory
	}{
		{name: "the alias wins in a clause where aliases shadow columns", shadowing: ast.ClauseWhere | ast.ClauseOrderBy,
			clause: ast.ClauseWhere, expectedCategory: sqlmodel.TypeCategoryInteger},
		{name: "the alias wins in the ORDER BY clause too", shadowing: ast.ClauseWhere | ast.ClauseOrderBy,
			clause: ast.ClauseOrderBy, expectedCategory: sqlmodel.TypeCategoryInteger},
		{name: "the column wins in a clause where they do not", shadowing: ast.ClauseHaving,
			clause: ast.ClauseWhere, expectedCategory: sqlmodel.TypeCategoryText},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, base := newExpressionTestResolver(t)
			rules := testScopeRules
			rules.ShadowingAliases = testCase.shadowing
			scope := NewRootScopeChain(ScopeKindQuery, rules)
			users, _ := base.Table("users")
			scope.AddTable(ast.TableReference{Name: "users"}, ast.JoinInner, users.CatalogueTable)
			analysis := &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{{Name: "name", Expression: bumped}},
				ClauseExpressions: []ast.ClauseExpression{{Clause: testCase.clause,
					Expression: &ast.ComparisonExpression{Operator: ">", Left: column("name"), Right: parameter(1)}}},
				ParameterReferences: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison}},
			}

			projection := resolver.ResolveProjection(t.Context(), analysis.OutputColumns, nil, scope)
			require.Empty(t, projection.Problems)
			parameters, problems := resolver.ResolveParameters(t.Context(), analysis, scope, nil)

			require.Empty(t, problems)
			require.Len(t, parameters, 1)
			require.Equal(t, testCase.expectedCategory, parameters[0].SQLType.Category)
		})
	}
}

func TestAParameterComparedWithAFieldOrAKeyedEntryIsNamedAfterIt(t *testing.T) {
	t.Parallel()

	berth := &ast.ArraySubscriptExpression{Array: column("berths"), Index: integerLiteral()}
	pontoon := &ast.StructFieldAccessExpression{Struct: berth, FieldName: "pontoon"}
	colour := &ast.ArraySubscriptExpression{Array: column("document"), Index: &ast.LiteralExpression{TypeName: "text"}, Key: "colour"}
	lowered := &ast.FunctionCallExpression{FunctionName: "lower", Arguments: []ast.Expression{parameter(1)}}
	lambdaField := &ast.StructFieldAccessExpression{Struct: column("x"), FieldName: "pontoon"}
	testCases := []struct {
		name       string
		expression ast.Expression
		wantNames  []string
	}{
		{name: "a field compared with the parameter", expression: &ast.ComparisonExpression{Operator: "=", Left: pontoon, Right: parameter(1)},
			wantNames: []string{"pontoon"}},
		{name: "the parameter compared with a field", expression: &ast.ComparisonExpression{Operator: "<>", Left: parameter(1), Right: pontoon},
			wantNames: []string{"pontoon"}},
		{name: "an entry read by its key", expression: &ast.ComparisonExpression{Operator: "=", Left: colour, Right: parameter(1)},
			wantNames: []string{"colour"}},
		{name: "a field listed against", expression: &ast.InListExpression{Inner: pontoon, Values: []ast.Expression{parameter(1), parameter(2)}},
			wantNames: []string{"pontoon", "pontoon_2"}},
		{name: "a field bounded", expression: &ast.BetweenExpression{Inner: pontoon, Low: parameter(1), High: parameter(2)},
			wantNames: []string{"pontoon_from", "pontoon_to"}},
		{name: "a field compared with a call over the parameter", expression: &ast.ComparisonExpression{Operator: "=", Left: pontoon, Right: lowered},
			wantNames: []string{"pontoon"}},
		{name: "an element read by its position names nothing",
			expression: &ast.ComparisonExpression{Operator: "=", Left: &ast.ArraySubscriptExpression{Array: column("crew"), Index: integerLiteral()},
				Right: parameter(1)},
			wantNames: []string{"p1"}},
		{name: "a field of a lambda parameter names nothing",
			expression: &ast.FunctionCallExpression{FunctionName: "apply", Arguments: []ast.Expression{
				column("berths"), lambda(&ast.ComparisonExpression{Operator: "=", Left: lambdaField, Right: parameter(1)}, "x"),
			}},
			wantNames: []string{"p1"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope, _ := newLambdaTestResolver(t)
			analysis := &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{{Clause: ast.ClauseWhere, Expression: testCase.expression}}}
			for number := range testCase.wantNames {
				analysis.ParameterReferences = append(analysis.ParameterReferences,
					ast.RawParameterReference{Number: number + 1, Context: sqlmodel.ParameterContextComparison})
			}

			parameters, _ := resolver.ResolveParameters(t.Context(), analysis, scope, nil)

			names := make([]string, 0, len(parameters))
			for index := range parameters {
				names = append(names, parameters[index].Name)
			}
			require.Equal(t, testCase.wantNames, names)
		})
	}
}

func TestAParameterComparedWithAnExpressionTakesNoTypeFromTheColumnTheDialectRecords(t *testing.T) {
	t.Parallel()

	mystery := &ast.FunctionCallExpression{FunctionName: "mystery", Arguments: []ast.Expression{column("name")}}
	defaulted := &ast.CoalesceExpression{Arguments: []ast.Expression{column("id"), integerLiteral()}}
	testCases := []struct {
		name         string
		expression   ast.Expression
		wantCategory sqlmodel.SQLTypeCategory
		wantNullable bool
	}{
		{name: "a call whose result type is unknown leaves the parameter any",
			expression: &ast.ComparisonExpression{Operator: "=", Left: mystery, Right: parameter(1)}, wantCategory: sqlmodel.TypeCategoryUnknown},
		{name: "a parameter on the left of such a call is any too",
			expression: &ast.ComparisonExpression{Operator: "<>", Left: parameter(1), Right: mystery}, wantCategory: sqlmodel.TypeCategoryUnknown},
		{name: "a value listed against such a call is any",
			expression: &ast.InListExpression{Inner: mystery, Values: []ast.Expression{parameter(1)}}, wantCategory: sqlmodel.TypeCategoryUnknown},
		{name: "an expression of a known type gives its type",
			expression: &ast.ComparisonExpression{Operator: "=", Left: defaulted, Right: parameter(1)}, wantCategory: sqlmodel.TypeCategoryInteger},
		{name: "the column itself gives its type",
			expression: &ast.ComparisonExpression{Operator: "=", Left: column("name"), Right: parameter(1)}, wantCategory: sqlmodel.TypeCategoryText},
		{name: "the column a later predicate compares the parameter with gives its type",
			expression: &ast.LogicalOpExpression{Operator: "OR", Operands: []ast.Expression{
				&ast.ComparisonExpression{Operator: "=", Left: mystery, Right: parameter(1)},
				&ast.ComparisonExpression{Operator: "=", Left: column("name"), Right: parameter(1)},
			}},
			wantCategory: sqlmodel.TypeCategoryText},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope, _ := newLambdaTestResolver(t)
			analysis := &ast.RawQueryAnalysis{
				ClauseExpressions: []ast.ClauseExpression{{Clause: ast.ClauseWhere, Expression: testCase.expression}},
				ParameterReferences: []ast.RawParameterReference{{
					Number: 1, Context: sqlmodel.ParameterContextComparison, ColumnReference: &ast.ColumnReference{ColumnName: "name"},
				}},
			}

			parameters, _ := resolver.ResolveParameters(t.Context(), analysis, scope, nil)

			require.Len(t, parameters, 1)
			require.Equal(t, "name", parameters[0].Name)
			require.Equal(t, testCase.wantCategory, parameters[0].SQLType.Category)
			require.Equal(t, testCase.wantNullable, parameters[0].Nullable)
		})
	}
}

func TestAParameterAQuantifiedComparisonReadsIsAnArrayOfTheOtherSide(t *testing.T) {
	t.Parallel()

	var _ dialect.ArrayTyperPort = arrayTypingEngine{}
	testCases := []struct {
		engine   TypeRules
		left     ast.Expression
		name     string
		wantType string
	}{
		{name: "a dialect that names array types", engine: arrayTypingEngine{Engine: &fakedialect.Engine{}},
			left: column("id"), wantType: "int4[]"},
		{name: "a dialect that names none", engine: &fakedialect.Engine{}, left: column("id"), wantType: ""},
		{name: "a left side of unknown type", engine: arrayTypingEngine{Engine: &fakedialect.Engine{}},
			left: column("missing"), wantType: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			table := fakedialect.NewTable("boats", sqlmodel.Column{Name: "id", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4")})
			catalogue.Schemas["public"].Tables["boats"] = table
			resolver := NewTypeResolver(catalogue, NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, testCase.engine), testCase.engine)
			scope := resolver.NewRootScope(ScopeKindQuery)
			scope.AddTable(ast.TableReference{Name: "boats"}, ast.JoinInner, table)
			analysis := &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{{Clause: ast.ClauseWhere,
				Expression: &ast.ComparisonExpression{Operator: "=", Quantifier: "ANY", Left: testCase.left, Right: parameter(1)}}}}

			compared := resolver.collectComparedParameters(t.Context(), analysis, scope)

			value, found := compared.values[1]
			require.Equal(t, testCase.wantType != "", found)
			if found {
				require.Equal(t, testCase.wantType, value.sqlType.EngineName)
				require.Equal(t, "int4", value.sqlType.ElementType.EngineName)
			}
		})
	}
}
