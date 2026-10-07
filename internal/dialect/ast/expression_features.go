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
)

const (
	// SQLFeaturesBase is the set of expression features supported by most SQL dialects:
	// arithmetic, comparisons, string concat, CASE, IS NULL, IN, BETWEEN, logical ops, unary
	// ops, and pattern matching. Dialects compose this baseline with their own additions or
	// removals.
	SQLFeaturesBase = SQLFeatureBinaryArithmetic |
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
)

// ExpressionFeature is a bitmask that declares which SQL expression kinds a dialect
// supports. Each dialect reports its supported set in dialect.Info.Expressions, allowing
// the type resolver to validate expressions against dialect capabilities.
type ExpressionFeature uint64

const (
	// SQLFeatureBinaryArithmetic allows arithmetic operators (+, -, *, /, %).
	SQLFeatureBinaryArithmetic ExpressionFeature = 1 << iota

	// SQLFeatureBinaryComparison allows comparison operators (=, <>, <, >, <=, >=).
	SQLFeatureBinaryComparison

	// SQLFeatureStringConcat allows the string concatenation operator (||).
	SQLFeatureStringConcat

	// SQLFeatureCaseWhen allows CASE WHEN/THEN/ELSE/END expressions.
	SQLFeatureCaseWhen

	// SQLFeatureScalarSubquery allows scalar subqueries in expression position.
	SQLFeatureScalarSubquery

	// SQLFeatureExists allows EXISTS(...) subquery expressions.
	SQLFeatureExists

	// SQLFeatureIsNull allows IS NULL and IS NOT NULL expressions, and the comparisons that
	// compare NULL as a value, such as IS DISTINCT FROM, which are never NULL.
	SQLFeatureIsNull

	// SQLFeatureInList allows IN (...) expressions.
	SQLFeatureInList

	// SQLFeatureBetween allows BETWEEN x AND y expressions.
	SQLFeatureBetween

	// SQLFeatureLogicalOp allows AND, OR logical operators.
	SQLFeatureLogicalOp

	// SQLFeatureUnaryOp allows unary operators (-, +, ~, NOT).
	SQLFeatureUnaryOp

	// SQLFeatureWindowFunction allows window functions with OVER clauses.
	SQLFeatureWindowFunction

	// SQLFeatureArraySubscript allows array subscript access (arr[1]).
	SQLFeatureArraySubscript

	// SQLFeatureJSONOp allows JSON operators (->, ->>, #>, #>>).
	SQLFeatureJSONOp

	// SQLFeaturePatternMatch allows LIKE, GLOB, REGEXP, MATCH operators.
	SQLFeaturePatternMatch

	// SQLFeatureBitwiseOp allows bitwise operators (&, |, <<, >>).
	SQLFeatureBitwiseOp

	// SQLFeatureLambda allows lambda expressions (x -> x + 1) in function arguments.
	SQLFeatureLambda

	// SQLFeatureStructFieldAccess allows struct field access (s.field_name).
	SQLFeatureStructFieldAccess

	// SQLFeatureStructConstruction allows building a struct value from named fields, such as
	// {'a': 1} or struct_pack(a := 1).
	SQLFeatureStructConstruction
)

var (
	// expressionFeatureNames holds the display name of each single expression feature.
	expressionFeatureNames = map[ExpressionFeature]string{
		SQLFeatureBinaryArithmetic:   "binary arithmetic (+, -, *, /, %)",
		SQLFeatureBinaryComparison:   "binary comparison (=, <>, <, >)",
		SQLFeatureStringConcat:       "string concatenation (||)",
		SQLFeatureCaseWhen:           "CASE WHEN expression",
		SQLFeatureScalarSubquery:     "scalar subquery",
		SQLFeatureExists:             "EXISTS subquery",
		SQLFeatureIsNull:             "IS NULL / IS NOT NULL",
		SQLFeatureInList:             "IN list",
		SQLFeatureBetween:            "BETWEEN",
		SQLFeatureLogicalOp:          "logical operator (AND, OR)",
		SQLFeatureUnaryOp:            "unary operator (-, +, ~, NOT)",
		SQLFeatureWindowFunction:     "window function (OVER)",
		SQLFeatureArraySubscript:     "array subscript ([])",
		SQLFeatureJSONOp:             "JSON operator (->, ->>)",
		SQLFeaturePatternMatch:       "pattern match (LIKE, GLOB)",
		SQLFeatureBitwiseOp:          "bitwise operator (&, |, <<, >>)",
		SQLFeatureLambda:             "lambda expression (x -> expr)",
		SQLFeatureStructFieldAccess:  "struct field access (s.field)",
		SQLFeatureStructConstruction: "struct construction ({'a': 1})",
	}
)

// Has reports whether the feature set includes the given feature.
//
// Takes flag (ExpressionFeature) which is the feature to test for.
//
// Returns bool which is true when the feature set includes the flag.
func (feature ExpressionFeature) Has(flag ExpressionFeature) bool {
	return feature&flag != 0
}

// String returns a human-readable name for a single expression feature flag.
//
// Returns string which is the feature's display name, or the set's number when it is not
// a single declared feature.
func (feature ExpressionFeature) String() string {
	if name, declared := expressionFeatureNames[feature]; declared {
		return name
	}
	return fmt.Sprintf("ExpressionFeature(%d)", uint64(feature))
}

// RequiredFeature identifies the dialect feature needed by an expression's own kind.
//
// Nested expressions are checked separately. The resolver and conformance suite use this
// same classification.
//
// Takes expression (Expression) which is the expression to judge.
// Takes operatorFeatures (map[string]ExpressionFeature) which maps each operator that
// needs a feature of its own to it, keyed as the dialect's parser spells the operator.
//
// Returns ExpressionFeature which is the feature, or zero for a kind every dialect has,
// such as a column reference, a literal or a function call.
func RequiredFeature(expression Expression, operatorFeatures map[string]ExpressionFeature) ExpressionFeature {
	switch typed := expression.(type) {
	case *BinaryOpExpression:
		return operatorFeature(operatorFeatures, typed.Operator, SQLFeatureBinaryArithmetic)
	case *ComparisonExpression:
		return operatorFeature(operatorFeatures, typed.Operator, SQLFeatureBinaryComparison)
	default:
		return kindFeature(expression)
	}
}

// operatorFeature returns the feature an operator needs: the one listed for it, or the
// given default.
//
// Takes operatorFeatures (map[string]ExpressionFeature) which lists the operators that
// need a feature of their own.
// Takes operator (string) which is the operator.
// Takes fallback (ExpressionFeature) which is the feature of an unlisted operator.
//
// Returns ExpressionFeature which is the feature the operator needs.
func operatorFeature(operatorFeatures map[string]ExpressionFeature, operator string,
	fallback ExpressionFeature) ExpressionFeature {
	if feature, listed := operatorFeatures[operator]; listed {
		return feature
	}
	return fallback
}

// kindFeature returns the feature an expression kind other than an operator needs.
//
// Takes expression (Expression) which is the expression.
//
// Returns ExpressionFeature which is the feature, or zero when every dialect has the
// kind.
func kindFeature(expression Expression) ExpressionFeature {
	switch expression.(type) {
	case *IsNullExpression:
		return SQLFeatureIsNull
	case *InListExpression:
		return SQLFeatureInList
	case *BetweenExpression:
		return SQLFeatureBetween
	case *LogicalOpExpression:
		return SQLFeatureLogicalOp
	case *UnaryOpExpression:
		return SQLFeatureUnaryOp
	case *CaseWhenExpression:
		return SQLFeatureCaseWhen
	case *ExistsExpression:
		return SQLFeatureExists
	case *WindowFunctionExpression:
		return SQLFeatureWindowFunction
	case *ScalarSubqueryExpression:
		return SQLFeatureScalarSubquery
	case *ArraySubscriptExpression:
		return SQLFeatureArraySubscript
	case *LambdaExpression:
		return SQLFeatureLambda
	case *StructFieldAccessExpression:
		return SQLFeatureStructFieldAccess
	case *StructExpression:
		return SQLFeatureStructConstruction
	default:
		return 0
	}
}
