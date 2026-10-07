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

package mysql

import (
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// operatorDivide is MySQL's division, whose result is exact for exact operands.
	operatorDivide = "/"

	// operatorSubtract subtracts numbers, and dates, times and datetimes read as numbers.
	operatorSubtract = "-"

	// operatorJSONExtract extracts a JSON value by path.
	operatorJSONExtract = "->"

	// operatorJSONUnquote extracts a JSON value by path and unquotes it to text.
	operatorJSONUnquote = "->>"

	// divisionScaleIncrement is the number of decimal places MySQL adds to the scale of a
	// division's left operand, its div_precision_increment default.
	divisionScaleIncrement = 4
)

var (
	// operatorFeatures names the expression feature each MySQL operator that needs one
	// requires. The NULL-safe equality <=> compares NULL as a value, so it is a NULL test.
	operatorFeatures = map[string]ast.ExpressionFeature{
		"<=>":               ast.SQLFeatureIsNull,
		operatorJSONExtract: ast.SQLFeatureJSONOp,
		operatorJSONUnquote: ast.SQLFeatureJSONOp,
		"&":                 ast.SQLFeatureBitwiseOp,
		"|":                 ast.SQLFeatureBitwiseOp,
		"^":                 ast.SQLFeatureBitwiseOp,
		"<<":                ast.SQLFeatureBitwiseOp,
		">>":                ast.SQLFeatureBitwiseOp,
		"LIKE":              ast.SQLFeaturePatternMatch,
		"REGEXP":            ast.SQLFeaturePatternMatch,
		"SOUNDS LIKE":       ast.SQLFeaturePatternMatch,
	}

	// bitOperators are MySQL's bit operators, which yield a 64-bit unsigned integer for
	// numeric operands.
	bitOperators = []string{"&", "|", "^", "<<", ">>"}
)

// coreTypes returns MySQL's core types, each through the dialect's normaliser: BOOLEAN,
// TEXT, BIGINT for counts (what COUNT returns), JSON, INT for a LIMIT or OFFSET
// parameter, no implicit row identifier and no void type.
//
// Returns dialect.CoreTypes which holds the core types.
func (engine *Dialect) coreTypes() dialect.CoreTypes {
	return dialect.CoreTypes{
		Boolean:  engine.NormaliseTypeName("boolean"),
		Text:     engine.NormaliseTypeName("text"),
		Count:    engine.NormaliseTypeName("bigint"),
		JSON:     engine.NormaliseTypeName("json"),
		RowCount: engine.NormaliseTypeName("int"),
		RowID:    sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		Void:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
	}
}

// binaryResult types MySQL operators whose result differs from their operands' common
// type.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is false for an operator left to the structural default.
func (engine *Dialect) binaryResult(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	switch {
	case operator == operatorSubtract && isTemporalValue(left) && isTemporalValue(right):
		return engine.temporalDifferenceResult(left, right), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case operator == operatorDivide:
		return engine.divisionResult(left, right), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case strings.EqualFold(operator, keywordDIV):
		return engine.integerDivisionResult(left, right), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case slices.Contains(bitOperators, operator):
		if left.Category == sqlmodel.TypeCategoryBinary {
			return left, sqlmodel.FunctionNullableReturnsNullOnNull, true
		}
		return engine.NormaliseTypeName("bigint unsigned"), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case operator == operatorJSONExtract:
		return engine.NormaliseTypeName("json"), sqlmodel.FunctionNullableCalledOnNull, true
	case operator == operatorJSONUnquote:
		return engine.NormaliseTypeName("longtext"), sqlmodel.FunctionNullableCalledOnNull, true
	default:
		return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
	}
}

// temporalDifferenceResult types - between two dates, times or datetimes, which MySQL
// reads as numbers such as 20240102030405 and subtracts: a BIGINT, or a DECIMAL whose
// scale is the larger fractional-seconds precision when either operand has one.
//
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the difference's type.
func (engine *Dialect) temporalDifferenceResult(left, right sqlmodel.SQLType) sqlmodel.SQLType {
	scale := max(fractionalSeconds(left), fractionalSeconds(right))
	if scale == 0 {
		return engine.NormaliseTypeName("bigint")
	}
	difference := engine.NormaliseTypeName("decimal")
	difference.Scale = &scale
	return difference
}

// divisionResult types /: DOUBLE when either operand is a float or of unknown kind
// treated as a string, and otherwise DECIMAL, whose scale is the left operand's scale
// plus MySQL's division precision increment.
//
// Takes left (sqlmodel.SQLType) which is the dividend's type.
// Takes right (sqlmodel.SQLType) which is the divisor's type.
//
// Returns sqlmodel.SQLType which is the quotient's type.
func (engine *Dialect) divisionResult(left, right sqlmodel.SQLType) sqlmodel.SQLType {
	if left.Category == sqlmodel.TypeCategoryFloat || right.Category == sqlmodel.TypeCategoryFloat ||
		left.Category == sqlmodel.TypeCategoryText || right.Category == sqlmodel.TypeCategoryText {
		return engine.NormaliseTypeName("double")
	}
	scale := divisionScaleIncrement
	if left.Scale != nil {
		scale += *left.Scale
	}
	quotient := engine.NormaliseTypeName("decimal")
	quotient.Scale = &scale
	return quotient
}

// integerDivisionResult types DIV: BIGINT UNSIGNED when either operand is unsigned, and
// BIGINT otherwise.
//
// Takes left (sqlmodel.SQLType) which is the dividend's type.
// Takes right (sqlmodel.SQLType) which is the divisor's type.
//
// Returns sqlmodel.SQLType which is the quotient's type.
func (engine *Dialect) integerDivisionResult(left, right sqlmodel.SQLType) sqlmodel.SQLType {
	if left.Unsigned || right.Unsigned {
		return engine.NormaliseTypeName("bigint unsigned")
	}
	return engine.NormaliseTypeName("bigint")
}

// unaryResult types NOT as a boolean that is NULL when its operand is and ~ as a BIGINT
// UNSIGNED, and leaves every other prefix operator to the structural default.
//
// Takes operator (string) which is the operator.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is false for an operator left to the structural default.
func (engine *Dialect) unaryResult(operator string) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	switch {
	case strings.EqualFold(operator, keywordNOT):
		return engine.NormaliseTypeName("boolean"), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case operator == "~":
		return engine.NormaliseTypeName("bigint unsigned"), sqlmodel.FunctionNullableReturnsNullOnNull, true
	default:
		return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
	}
}

// isTemporalValue reports whether a type holds a date, a time or a datetime, which MySQL
// reads as a number in arithmetic.
//
// Takes sqlType (sqlmodel.SQLType) which is the type to test.
//
// Returns bool which is true for a temporal type other than an interval.
func isTemporalValue(sqlType sqlmodel.SQLType) bool {
	return sqlType.Category == sqlmodel.TypeCategoryTemporal && sqlType.Temporal != sqlmodel.TemporalKindInterval
}

// fractionalSeconds returns the fractional-seconds precision of a temporal type.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns int which is the precision, zero when the type has none.
func fractionalSeconds(sqlType sqlmodel.SQLType) int {
	if sqlType.Precision == nil {
		return 0
	}
	return *sqlType.Precision
}

// promotionRank orders the integer types for promotion: by width, an unsigned type
// ranking one step above the signed type of its width, so it promotes to the next wider
// signed type.
//
// Takes sqlType (sqlmodel.SQLType) which is an integer type.
//
// Returns int which is the rank.
func promotionRank(sqlType sqlmodel.SQLType) int {
	rank := sqlType.Width * 2
	if sqlType.Unsigned {
		rank++
	}
	return rank
}
