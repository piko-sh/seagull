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

package duckdb

import (
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// operatorConcatenate joins text, lists or blobs.
	operatorConcatenate = "||"

	// operatorDivide is DuckDB's division, which is never an integer division.
	operatorDivide = "/"

	// operatorJSONExtract extracts a JSON value by path or key.
	operatorJSONExtract = "->"

	// operatorJSONText extracts a JSON value by path or key as text.
	operatorJSONText = "->>"

	// operatorColon separates a struct literal's key from its value, a lambda's parameters
	// from its body and the bounds of a slice.
	operatorColon = ":"
)

var (
	// operatorFeatures names the expression feature each DuckDB operator that needs one
	// requires. IS [NOT] DISTINCT FROM compares NULL as a value, so it is a NULL test.
	operatorFeatures = map[string]ast.ExpressionFeature{
		"IS DISTINCT FROM":     ast.SQLFeatureIsNull,
		"IS NOT DISTINCT FROM": ast.SQLFeatureIsNull,
		operatorConcatenate:    ast.SQLFeatureStringConcat,
		operatorJSONExtract:    ast.SQLFeatureJSONOp,
		operatorJSONText:       ast.SQLFeatureJSONOp,
		"&":                    ast.SQLFeatureBitwiseOp,
		"|":                    ast.SQLFeatureBitwiseOp,
		"#":                    ast.SQLFeatureBitwiseOp,
		"<<":                   ast.SQLFeatureBitwiseOp,
		">>":                   ast.SQLFeatureBitwiseOp,
		"LIKE":                 ast.SQLFeaturePatternMatch,
		"ILIKE":                ast.SQLFeaturePatternMatch,
		"GLOB":                 ast.SQLFeaturePatternMatch,
		"SIMILAR TO":           ast.SQLFeaturePatternMatch,
		"~":                    ast.SQLFeaturePatternMatch,
		"!~":                   ast.SQLFeaturePatternMatch,
		"~*":                   ast.SQLFeaturePatternMatch,
		"!~*":                  ast.SQLFeaturePatternMatch,
		"~~":                   ast.SQLFeaturePatternMatch,
		"!~~":                  ast.SQLFeaturePatternMatch,
		"~~*":                  ast.SQLFeaturePatternMatch,
		"!~~*":                 ast.SQLFeaturePatternMatch,
		"^@":                   ast.SQLFeaturePatternMatch,
	}

	// coreTypes holds DuckDB's core types: BOOLEAN, VARCHAR, BIGINT for counts, JSON,
	// INTEGER for a LIMIT or OFFSET parameter, no implicit row identifier and no void type.
	//
	// DuckDB binds a LIMIT or OFFSET parameter as BIGINT; INTEGER keeps the generated
	// signatures callers already use and is widened by the engine.
	coreTypes = dialect.CoreTypes{
		Boolean:  builtinTypeMap["boolean"],
		Text:     builtinTypeMap["varchar"],
		Count:    builtinTypeMap["bigint"],
		JSON:     builtinTypeMap["json"],
		RowCount: builtinTypeMap["integer"],
		RowID:    sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		Void:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
	}
)

// binaryResult types DuckDB operators whose result differs from their operands' common
// type.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is false for an operator left to the structural default.
func binaryResult(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	if result, ok := temporalArithmeticResult(operator, left, right); ok {
		return result, sqlmodel.FunctionNullableReturnsNullOnNull, true
	}
	switch operator {
	case operatorConcatenate:
		if left.Category == sqlmodel.TypeCategoryArray || left.Category == sqlmodel.TypeCategoryBinary {
			return left, sqlmodel.FunctionNullableReturnsNullOnNull, true
		}
		return builtinTypeMap["varchar"], sqlmodel.FunctionNullableReturnsNullOnNull, true
	case operatorDivide:
		if left.Category == sqlmodel.TypeCategoryDecimal && right.Category == sqlmodel.TypeCategoryDecimal {
			return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
		}
		if isNumeric(left) && isNumeric(right) {
			return builtinTypeMap["double"], sqlmodel.FunctionNullableReturnsNullOnNull, true
		}
		return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
	case operatorJSONExtract:
		return builtinTypeMap["json"], sqlmodel.FunctionNullableCalledOnNull, true
	case operatorJSONText:
		return builtinTypeMap["varchar"], sqlmodel.FunctionNullableCalledOnNull, true
	}
	return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
}

// isNumeric reports whether a type is an integer, a float or a decimal.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns bool which is true for a number.
func isNumeric(sqlType sqlmodel.SQLType) bool {
	switch sqlType.Category {
	case sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryDecimal:
		return true
	default:
		return false
	}
}

// unaryResult types NOT as a boolean that is NULL when its operand is, and leaves every
// other prefix operator to the structural default.
//
// Takes operator (string) which is the operator.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is true for NOT.
func unaryResult(operator string) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	if strings.EqualFold(operator, keywordNOT) {
		return builtinTypeMap["boolean"], sqlmodel.FunctionNullableReturnsNullOnNull, true
	}
	return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
}
