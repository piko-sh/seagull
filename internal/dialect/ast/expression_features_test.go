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

package ast

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSQLExpressionFeatureString_RendersEachSingleBit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		feature  ExpressionFeature
		expected string
	}{
		{name: "binary arithmetic", feature: SQLFeatureBinaryArithmetic, expected: "binary arithmetic (+, -, *, /, %)"},
		{name: "binary comparison", feature: SQLFeatureBinaryComparison, expected: "binary comparison (=, <>, <, >)"},
		{name: "string concat", feature: SQLFeatureStringConcat, expected: "string concatenation (||)"},
		{name: "case when", feature: SQLFeatureCaseWhen, expected: "CASE WHEN expression"},
		{name: "scalar subquery", feature: SQLFeatureScalarSubquery, expected: "scalar subquery"},
		{name: "exists", feature: SQLFeatureExists, expected: "EXISTS subquery"},
		{name: "is null", feature: SQLFeatureIsNull, expected: "IS NULL / IS NOT NULL"},
		{name: "in list", feature: SQLFeatureInList, expected: "IN list"},
		{name: "between", feature: SQLFeatureBetween, expected: "BETWEEN"},
		{name: "logical op", feature: SQLFeatureLogicalOp, expected: "logical operator (AND, OR)"},
		{name: "unary op", feature: SQLFeatureUnaryOp, expected: "unary operator (-, +, ~, NOT)"},
		{name: "window function", feature: SQLFeatureWindowFunction, expected: "window function (OVER)"},
		{name: "array subscript", feature: SQLFeatureArraySubscript, expected: "array subscript ([])"},
		{name: "json op", feature: SQLFeatureJSONOp, expected: "JSON operator (->, ->>)"},
		{name: "pattern match", feature: SQLFeaturePatternMatch, expected: "pattern match (LIKE, GLOB)"},
		{name: "bitwise op", feature: SQLFeatureBitwiseOp, expected: "bitwise operator (&, |, <<, >>)"},
		{name: "lambda", feature: SQLFeatureLambda, expected: "lambda expression (x -> expr)"},
		{name: "struct field access", feature: SQLFeatureStructFieldAccess, expected: "struct field access (s.field)"},
		{name: "struct construction", feature: SQLFeatureStructConstruction, expected: "struct construction ({'a': 1})"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.expected, testCase.feature.String())
		})
	}
}

func TestSQLExpressionFeatureString_RendersFallbackForZeroCombinationAndAllBits(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		feature  ExpressionFeature
		expected string
	}{
		{
			name:     "zero value uses numeric fallback",
			feature:  ExpressionFeature(0),
			expected: "ExpressionFeature(0)",
		},
		{
			name:     "combination of two bits uses numeric fallback",
			feature:  SQLFeatureBinaryArithmetic | SQLFeatureBinaryComparison,
			expected: "ExpressionFeature(3)",
		},
		{
			name:     "all bits uses numeric fallback",
			feature:  ^ExpressionFeature(0),
			expected: "ExpressionFeature(18446744073709551615)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.expected, testCase.feature.String())
		})
	}
}

func TestSQLExpressionFeatureHas_ReportsMembership(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		set      ExpressionFeature
		flag     ExpressionFeature
		expected bool
	}{
		{name: "single set bit is present", set: SQLFeatureInList, flag: SQLFeatureInList, expected: true},
		{name: "absent bit reports false", set: SQLFeatureInList, flag: SQLFeatureBetween, expected: false},
		{name: "base set contains comparison", set: SQLFeaturesBase, flag: SQLFeatureBinaryComparison, expected: true},
		{name: "base set lacks window function", set: SQLFeaturesBase, flag: SQLFeatureWindowFunction, expected: false},
		{name: "base set lacks scalar subquery", set: SQLFeaturesBase, flag: SQLFeatureScalarSubquery, expected: false},
		{name: "all set contains every flag", set: ^ExpressionFeature(0), flag: SQLFeatureStructFieldAccess, expected: true},
		{name: "zero set contains nothing", set: ExpressionFeature(0), flag: SQLFeatureInList, expected: false},
		{name: "combined set contains either member", set: SQLFeatureInList | SQLFeatureBetween, flag: SQLFeatureBetween, expected: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.expected, testCase.set.Has(testCase.flag))
		})
	}
}

func TestSQLFeaturesBase_CombinesExpectedBaselineFlags(t *testing.T) {
	t.Parallel()

	expected := SQLFeatureBinaryArithmetic |
		SQLFeatureBinaryComparison |
		SQLFeatureStringConcat |
		SQLFeatureCaseWhen |
		SQLFeatureExists |
		SQLFeatureIsNull |
		SQLFeatureInList |
		SQLFeatureBetween |
		SQLFeatureLogicalOp |
		SQLFeatureUnaryOp |
		SQLFeaturePatternMatch

	assert.Equal(t, expected, SQLFeaturesBase)
}

func TestRequiredFeature_JudgesTheExpressionsOwnKind(t *testing.T) {
	t.Parallel()

	operators := map[string]ExpressionFeature{"||": SQLFeatureStringConcat, "->": SQLFeatureJSONOp}
	testCases := []struct {
		expression Expression
		name       string
		want       ExpressionFeature
	}{
		{name: "an unlisted binary operator is arithmetic", expression: &BinaryOpExpression{Operator: "+"}, want: SQLFeatureBinaryArithmetic},
		{name: "a listed binary operator takes its feature", expression: &BinaryOpExpression{Operator: "||"}, want: SQLFeatureStringConcat},
		{name: "an unlisted comparison is a comparison", expression: &ComparisonExpression{Operator: "="}, want: SQLFeatureBinaryComparison},
		{name: "a listed comparison takes its feature", expression: &ComparisonExpression{Operator: "->"}, want: SQLFeatureJSONOp},
		{name: "a subscript", expression: &ArraySubscriptExpression{Array: &ColumnRefExpression{}}, want: SQLFeatureArraySubscript},
		{name: "a lambda", expression: &LambdaExpression{Parameters: []string{"x"}}, want: SQLFeatureLambda},
		{name: "a struct field", expression: &StructFieldAccessExpression{FieldName: "a"}, want: SQLFeatureStructFieldAccess},
		{name: "a struct constructor", expression: &StructExpression{Names: []string{"a"}, Values: []Expression{&LiteralExpression{TypeName: "integer"}}}, want: SQLFeatureStructConstruction},
		{name: "a scalar subquery", expression: &ScalarSubqueryExpression{}, want: SQLFeatureScalarSubquery},
		{name: "a window function", expression: &WindowFunctionExpression{}, want: SQLFeatureWindowFunction},
		{name: "a CASE", expression: &CaseWhenExpression{}, want: SQLFeatureCaseWhen},
		{name: "an IS NULL", expression: &IsNullExpression{}, want: SQLFeatureIsNull},
		{name: "an IN list", expression: &InListExpression{}, want: SQLFeatureInList},
		{name: "a BETWEEN", expression: &BetweenExpression{}, want: SQLFeatureBetween},
		{name: "a logical operator", expression: &LogicalOpExpression{Operator: "AND"}, want: SQLFeatureLogicalOp},
		{name: "a unary operator", expression: &UnaryOpExpression{Operator: "-"}, want: SQLFeatureUnaryOp},
		{name: "an EXISTS", expression: &ExistsExpression{}, want: SQLFeatureExists},
		{name: "a column needs nothing", expression: &ColumnRefExpression{ColumnName: "id"}, want: 0},
		{name: "a function call needs nothing", expression: &FunctionCallExpression{FunctionName: "f"}, want: 0},
		{name: "a literal needs nothing", expression: &LiteralExpression{TypeName: "integer"}, want: 0},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, RequiredFeature(testCase.expression, operators))
		})
	}
}

func TestSQLFeatureStructConstruction_FollowsTheEarlierFeatureBits(t *testing.T) {
	t.Parallel()

	assert.Equal(t, SQLFeatureStructFieldAccess<<1, SQLFeatureStructConstruction)
	assert.Equal(t, ExpressionFeature(1)<<17, SQLFeatureStructFieldAccess)
}

func TestSQLExpressionFeatureString_NamesEveryDeclaredBit(t *testing.T) {
	t.Parallel()

	for feature := SQLFeatureBinaryArithmetic; feature <= SQLFeatureStructConstruction; feature <<= 1 {
		t.Run(feature.String(), func(t *testing.T) {
			t.Parallel()

			assert.NotEqual(t, fmt.Sprintf("ExpressionFeature(%d)", uint64(feature)), feature.String())
		})
	}
}
