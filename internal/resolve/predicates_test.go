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

type predicateTypingEngine struct {
	*fakedialect.Engine
	result func(predicate ast.Expression, operands []sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool)
}

func (engine predicateTypingEngine) PredicateResult(predicate ast.Expression, operands []sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	return engine.result(predicate, operands)
}

func flagType() sqlmodel.SQLType {
	return sqlmodel.NewIntegerType("flag8", 8, true)
}

func newPredicateResolver(engine TypeRules) *TypeResolver {
	catalogue := fakedialect.NewCatalogue("public")
	builtins := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{}}
	return NewTypeResolver(catalogue, NewFunctionResolver(builtins, catalogue, engine), engine)
}

func integerLiteral() *ast.LiteralExpression {
	return &ast.LiteralExpression{TypeName: "integer"}
}

func TestResolveProjectionReportsAnUnknownColumnAPredicateReadsWhereItIs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		expression ast.Expression
		name       string
		want       []expectedProblem
	}{
		{
			name:       "an unknown column compared",
			expression: &ast.ComparisonExpression{Operator: "=", Left: located("nme", 7), Right: located("email", 13)},
			want:       []expectedProblem{{code: diag.CodeUnknownColumn, suggestion: "name", span: Span{Start: 7, End: 10}}},
		},
		{
			name:       "an unknown column tested for NULL",
			expression: &ast.IsNullExpression{Inner: located("emial", 7)},
			want:       []expectedProblem{{code: diag.CodeUnknownColumn, suggestion: "email", span: Span{Start: 7, End: 12}}},
		},
		{
			name:       "an unknown column in an IN list",
			expression: &ast.InListExpression{Inner: located("id", 7), Values: []ast.Expression{located("idd", 14)}},
			want:       []expectedProblem{{code: diag.CodeUnknownColumn, suggestion: "id", span: Span{Start: 14, End: 17}}},
		},
		{
			name:       "an unknown column bounding a BETWEEN",
			expression: &ast.BetweenExpression{Inner: located("id", 7), Low: integerLiteral(), High: located("idd", 25)},
			want:       []expectedProblem{{code: diag.CodeUnknownColumn, suggestion: "id", span: Span{Start: 25, End: 28}}},
		},
		{
			name: "every unknown column of a logical operator",
			expression: &ast.LogicalOpExpression{Operator: "AND", Operands: []ast.Expression{
				&ast.IsNullExpression{Inner: located("nme", 7)},
				&ast.IsNullExpression{Inner: located("emial", 24)},
			}},
			want: []expectedProblem{
				{code: diag.CodeUnknownColumn, suggestion: "name", span: Span{Start: 7, End: 10}},
				{code: diag.CodeUnknownColumn, suggestion: "email", span: Span{Start: 24, End: 29}},
			},
		},
		{
			name: "an unknown column a CASE condition reads",
			expression: &ast.CaseWhenExpression{
				Branches:   []ast.CaseWhenBranch{{Condition: &ast.IsNullExpression{Inner: located("nme", 12)}, Result: located("id", 28)}},
				ElseResult: integerLiteral(),
			},
			want: []expectedProblem{{code: diag.CodeUnknownColumn, suggestion: "name", span: Span{Start: 12, End: 15}}},
		},
		{
			name:       "an unknown column a FILTER condition reads",
			expression: &ast.FunctionCallExpression{FunctionName: "count", FilterExpression: &ast.IsNullExpression{Inner: located("nme", 27)}},
			want:       []expectedProblem{{code: diag.CodeUnknownColumn, suggestion: "name", span: Span{Start: 27, End: 30}}},
		},
		{
			name:       "an unknown column a subscript reads",
			expression: &ast.ArraySubscriptExpression{Array: located("name", 7), Index: located("idd", 12)},
			want:       []expectedProblem{{code: diag.CodeUnknownColumn, suggestion: "id", span: Span{Start: 12, End: 15}}},
		},
		{
			name: "a column an EXISTS subquery reads is left to the subquery",
			expression: &ast.ExistsExpression{InnerQuery: &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{{Expression: located("anything", 20)}},
			}},
			want: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			raw := []ast.RawOutputColumn{{Name: "value", Expression: testCase.expression}}
			projection := newProjectionResolver().ResolveProjection(t.Context(), raw, []Span{{Start: 7, End: 60}}, setupTypeResolverScope())

			var problems []expectedProblem
			for _, problem := range projection.Problems {
				problems = append(problems, expectedProblem{code: problem.Code, suggestion: problem.Suggestion, span: problem.Span})
			}
			require.Equal(t, testCase.want, problems)
		})
	}
}

func nullSafeEngine() *fakedialect.Engine {
	engine := &fakedialect.Engine{}
	info := engine.Info()
	info.OperatorFeatures = map[string]ast.ExpressionFeature{"IS DISTINCT FROM": ast.SQLFeatureIsNull}
	engine.InfoFunc = func() dialect.Info { return info }
	return engine
}

func emailSubquery() *ast.ScalarSubqueryExpression {
	return &ast.ScalarSubqueryExpression{InnerQuery: &ast.RawQueryAnalysis{
		OutputColumns: []ast.RawOutputColumn{{Name: "email", ColumnName: "email", Expression: column("email")}},
	}}
}

func idSubquery() *ast.ScalarSubqueryExpression {
	return &ast.ScalarSubqueryExpression{InnerQuery: &ast.RawQueryAnalysis{
		OutputColumns: []ast.RawOutputColumn{{Name: "id", ColumnName: "id", Expression: column("id")}},
	}}
}

func TestCorePredicatesAreNullWhereAnOperandMayBe(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		expression   ast.Expression
		name         string
		wantNullable bool
	}{
		{name: "a comparison of a column that may be NULL", expression: &ast.ComparisonExpression{Operator: "=", Left: column("email"), Right: column("name")}, wantNullable: true},
		{name: "a comparison of columns never NULL", expression: &ast.ComparisonExpression{Operator: "<", Left: column("id"), Right: integerLiteral()}, wantNullable: false},
		{name: "a comparison of a column never NULL with a parameter", expression: &ast.ComparisonExpression{Operator: "=", Left: column("id"), Right: &ast.ParameterExpression{Number: 1}}, wantNullable: false},
		{name: "a comparison of a column that may be NULL with a parameter", expression: &ast.ComparisonExpression{Operator: "=", Left: column("email"), Right: &ast.ParameterExpression{Number: 1}}, wantNullable: true},
		{name: "a comparison the dialect lists as a NULL test", expression: &ast.ComparisonExpression{Operator: "IS DISTINCT FROM", Left: column("email"), Right: column("name")}, wantNullable: false},
		{name: "an IS NULL test", expression: &ast.IsNullExpression{Inner: column("email")}, wantNullable: false},
		{name: "an IN list holding a value that may be NULL", expression: &ast.InListExpression{Inner: column("id"), Values: []ast.Expression{integerLiteral(), column("email")}}, wantNullable: true},
		{name: "an IN list of a value that may be NULL", expression: &ast.InListExpression{Inner: column("email"), Values: []ast.Expression{column("name")}}, wantNullable: true},
		{name: "an IN list of values never NULL", expression: &ast.InListExpression{Inner: column("id"), Values: []ast.Expression{integerLiteral()}}, wantNullable: false},
		{name: "an IN subquery selecting a column never NULL", expression: &ast.InListExpression{Inner: column("id"), Values: []ast.Expression{idSubquery()}}, wantNullable: false},
		{name: "an IN subquery selecting a column that may be NULL", expression: &ast.InListExpression{Inner: column("name"), Values: []ast.Expression{emailSubquery()}}, wantNullable: true},
		{name: "a BETWEEN bounded by literals", expression: &ast.BetweenExpression{Inner: column("id"), Low: integerLiteral(), High: integerLiteral()}, wantNullable: false},
		{name: "a BETWEEN of a value that may be NULL", expression: &ast.BetweenExpression{Inner: column("email"), Low: column("name"), High: column("name")}, wantNullable: true},
		{name: "a logical operator over an operand that may be NULL", expression: &ast.LogicalOpExpression{Operator: "OR", Operands: []ast.Expression{column("email"), column("name")}}, wantNullable: true},
		{name: "a logical operator over NULL tests", expression: &ast.LogicalOpExpression{Operator: "AND", Operands: []ast.Expression{
			&ast.IsNullExpression{Inner: column("email")}, &ast.ComparisonExpression{Operator: "=", Left: column("id"), Right: integerLiteral()},
		}}, wantNullable: false},
		{name: "an EXISTS", expression: &ast.ExistsExpression{InnerQuery: &ast.RawQueryAnalysis{}}, wantNullable: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sqlType, nullable, err := newPredicateResolver(nullSafeEngine()).ResolveExpressionType(t.Context(), testCase.expression, setupTypeResolverScope(), new(false))

			require.NoError(t, err)
			require.Equal(t, sqlmodel.TypeCategoryBoolean, sqlType.Category)
			require.Equal(t, testCase.wantNullable, nullable)
		})
	}
}

func TestANegatedPredicateIsNullWhereThePredicateMayBe(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		operand      ast.Expression
		name         string
		wantNullable bool
	}{
		{name: "a comparison of a column that may be NULL", operand: &ast.ComparisonExpression{Operator: ">", Left: column("email"), Right: column("name")}, wantNullable: true},
		{name: "an IS NULL test", operand: &ast.IsNullExpression{Inner: column("email")}, wantNullable: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, nullable, err := newPredicateResolver(&fakedialect.Engine{}).ResolveExpressionType(
				t.Context(),
				&ast.UnaryOpExpression{Operator: "NOT", Operand: testCase.operand}, setupTypeResolverScope(), new(false))

			require.NoError(t, err)
			require.Equal(t, testCase.wantNullable, nullable)
		})
	}
}

func TestPredicatesTakeTheTypeTheDialectGivesThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		expression   ast.Expression
		name         string
		wantOperands []string
		wantNullable bool
	}{
		{
			name:         "a comparison is NULL when an operand may be",
			expression:   &ast.ComparisonExpression{Operator: "=", Left: column("email"), Right: column("name")},
			wantOperands: []string{"text", "text"},
			wantNullable: true,
		},
		{
			name:         "a comparison of columns never NULL is never NULL",
			expression:   &ast.ComparisonExpression{Operator: "<", Left: column("id"), Right: column("id")},
			wantOperands: []string{"int4", "int4"},
			wantNullable: false,
		},
		{
			name:         "an IN list passes its tested value and then its values",
			expression:   &ast.InListExpression{Inner: column("id"), Values: []ast.Expression{column("name"), column("email")}},
			wantOperands: []string{"int4", "text", "text"},
			wantNullable: true,
		},
		{
			name:         "a BETWEEN passes its value and its bounds",
			expression:   &ast.BetweenExpression{Inner: column("name"), Low: column("email"), High: column("id")},
			wantOperands: []string{"text", "text", "int4"},
			wantNullable: true,
		},
		{
			name:         "an EXISTS passes nothing",
			expression:   &ast.ExistsExpression{InnerQuery: &ast.RawQueryAnalysis{}},
			wantOperands: []string{},
			wantNullable: false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var seen []string
			engine := predicateTypingEngine{Engine: &fakedialect.Engine{}, result: func(_ ast.Expression, operands []sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
				seen = make([]string, 0, len(operands))
				for _, operand := range operands {
					seen = append(seen, operand.EngineName)
				}
				return flagType(), sqlmodel.FunctionNullableReturnsNullOnNull, true
			}}

			sqlType, nullable, err := newPredicateResolver(engine).ResolveExpressionType(t.Context(), testCase.expression, setupTypeResolverScope(), new(false))

			require.NoError(t, err)
			require.Equal(t, flagType(), sqlType)
			require.Equal(t, testCase.wantNullable, nullable)
			require.Equal(t, testCase.wantOperands, seen)
		})
	}
}

func TestAPredicateTheDialectDeclinesIsTheCoreBooleanNullWhereAnOperandMayBe(t *testing.T) {
	t.Parallel()

	engine := predicateTypingEngine{Engine: &fakedialect.Engine{}, result: func(ast.Expression, []sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
		return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
	}}

	sqlType, nullable, err := newPredicateResolver(engine).ResolveExpressionType(
		t.Context(),
		&ast.ComparisonExpression{Operator: "=", Left: column("email"), Right: column("name")}, setupTypeResolverScope(), new(false))

	require.NoError(t, err)
	require.Equal(t, sqlmodel.TypeCategoryBoolean, sqlType.Category)
	require.True(t, nullable)
}

func row(elements ...ast.Expression) *ast.RowValueExpression {
	return &ast.RowValueExpression{Elements: elements}
}

func TestParametersInARowComparedWithARowTakeTheirCounterpartsTypes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		clause   ast.Expression
		name     string
		wantType []sqlmodel.SQLTypeCategory
	}{
		{
			name:     "a row compared with a row of parameters",
			clause:   &ast.ComparisonExpression{Operator: "<", Left: row(column("name"), column("id")), Right: row(parameter(1), parameter(2))},
			wantType: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryInteger},
		},
		{
			name: "a row tested against a list of rows of parameters",
			clause: &ast.InListExpression{Inner: row(column("id"), column("name")),
				Values: []ast.Expression{row(parameter(1), parameter(2))}},
			wantType: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryText},
		},
		{
			name:     "rows of different lengths type nothing",
			clause:   &ast.ComparisonExpression{Operator: "=", Left: row(column("id")), Right: row(parameter(1), parameter(2))},
			wantType: []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryUnknown, sqlmodel.TypeCategoryUnknown},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{
				ParameterReferences: []ast.RawParameterReference{{Number: 1}, {Number: 2}},
				ClauseExpressions:   []ast.ClauseExpression{{Clause: ast.ClauseWhere, Expression: testCase.clause}},
			}
			parameters, _ := newProjectionResolver().ResolveParameters(t.Context(), analysis, setupTypeResolverScope(), nil)

			categories := make([]sqlmodel.SQLTypeCategory, len(parameters))
			for index := range parameters {
				categories[index] = parameters[index].SQLType.Category
			}
			require.Equal(t, testCase.wantType, categories)
		})
	}
}

func TestARowIsNullWhereAnElementOtherThanAParameterMayBe(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		row          *ast.RowValueExpression
		name         string
		wantNullable bool
	}{
		{name: "columns never NULL", row: row(column("id"), column("name")), wantNullable: false},
		{name: "a column that may be NULL", row: row(column("id"), column("email")), wantNullable: true},
		{name: "a parameter", row: row(column("id"), parameter(1)), wantNullable: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, nullable, err := newProjectionResolver().ResolveExpressionType(t.Context(), testCase.row, setupTypeResolverScope(), new(false))

			require.NoError(t, err)
			require.Equal(t, testCase.wantNullable, nullable)
		})
	}
}
