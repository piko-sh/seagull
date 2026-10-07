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
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// operatorConcatenate joins two values as a String.
	operatorConcatenate = "||"

	// operatorDivide is ClickHouse's division, which yields a Float64 for numbers that are
	// not decimals.
	operatorDivide = "/"

	// operatorNot is the logical negation.
	operatorNot = "NOT"

	// operatorPlus is addition.
	operatorPlus = "+"

	// operatorMinus is subtraction, and as a prefix, negation.
	operatorMinus = "-"

	// operatorMultiply is multiplication.
	operatorMultiply = "*"

	// operatorModulo is the remainder of a division.
	operatorModulo = "%"
)

var (
	// operatorFeatures names the expression feature each ClickHouse operator that needs one
	// requires.
	operatorFeatures = map[string]ast.ExpressionFeature{
		operatorConcatenate: ast.SQLFeatureStringConcat,
		"LIKE":              ast.SQLFeaturePatternMatch,
		"ILIKE":             ast.SQLFeaturePatternMatch,
		"NOT LIKE":          ast.SQLFeaturePatternMatch,
		"NOT ILIKE":         ast.SQLFeaturePatternMatch,
	}

	// coreTypes holds ClickHouse's core types: Bool, String, UInt64 for counts and for a
	// LIMIT or OFFSET parameter, JSON, no implicit row identifier and no void type.
	coreTypes = dialect.CoreTypes{
		Boolean:  primitiveType("Bool"),
		Text:     primitiveType("String"),
		Count:    primitiveType("UInt64"),
		JSON:     primitiveType("JSON"),
		RowCount: primitiveType("UInt64"),
		RowID:    sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		Void:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
	}
)

// PredicateResult types ClickHouse's predicates as the engine does; see predicateResult.
//
// Takes predicate (ast.Expression) which is the predicate.
// Takes operands ([]sqlmodel.SQLType) which holds its operands' types.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is true for every predicate.
func (*Dialect) PredicateResult(predicate ast.Expression, operands []sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	return predicateResult(predicate, operands)
}

// binaryResult types ClickHouse's operators: || yields a String, and the arithmetic
// operators follow ClickHouse's own result rules, which differ from the common type of
// their operands; see arithmeticResult.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is false for an operator left to the structural default.
func binaryResult(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	if operator == operatorConcatenate {
		return primitiveType(engineNameString), sqlmodel.FunctionNullableReturnsNullOnNull, true
	}
	if kind, isArithmetic := arithmeticOperators[operator]; isArithmetic {
		return arithmeticResult(kind, left, right), sqlmodel.FunctionNullableReturnsNullOnNull, true
	}
	return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
}

// unaryResult types ClickHouse's supported prefix operators.
//
// NOT preserves Bool operands and otherwise yields UInt8. Unknown prefix operators use
// the structural default.
//
// Takes operator (string) which is the operator.
// Takes operand (sqlmodel.SQLType) which is the operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is true for NOT, minus and the element-reading operator.
func unaryResult(operator string, operand sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	switch {
	case strings.EqualFold(operator, operatorNot):
		return logicalResult(operand), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case operator == operatorMinus:
		return negationResult(operand), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case operator == operatorSubcolumns:
		subcolumns, behaviour := subcolumnsOf(operand)
		return subcolumns, behaviour, true
	default:
		return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
	}
}

// predicateResult types ClickHouse predicates and their nullability.
//
// Comparison and membership tests yield UInt8. Logical operators yield Bool when either
// operand is Bool. IS NULL and EXISTS are never nullable.
//
// Takes predicate (ast.Expression) which is the predicate.
// Takes operands ([]sqlmodel.SQLType) which holds its operands' types, as
// dialect.PredicateTyperPort orders them.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is false for an expression that is no predicate.
func predicateResult(predicate ast.Expression, operands []sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	switch predicate.(type) {
	case *ast.ComparisonExpression, *ast.BetweenExpression:
		return primitiveType(engineNameUInt8), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case *ast.InListExpression:
		return primitiveType(engineNameUInt8), sqlmodel.FunctionNullableNullWhenArgumentNull, true
	case *ast.IsNullExpression, *ast.ExistsExpression:
		return primitiveType(engineNameUInt8), sqlmodel.FunctionNullableNeverNull, true
	case *ast.LogicalOpExpression:
		return logicalResult(operands...), sqlmodel.FunctionNullableReturnsNullOnNull, true
	default:
		return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
	}
}
