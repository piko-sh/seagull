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
	"strconv"

	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// widestNarrowInteger is the widest integer width ClickHouse widens arithmetic results
	// to; the 128 and 256 bit types keep their own width.
	widestNarrowInteger = widthLong

	// dateTime64DifferencePrecision is the precision of the Decimal a DateTime64 subtraction
	// yields.
	dateTime64DifferencePrecision = 18
)

// arithmeticKind names one of ClickHouse's arithmetic operations, each with its own rule
// for the type of an integer result.
type arithmeticKind uint8

const (
	// arithmeticAddition is plus.
	arithmeticAddition arithmeticKind = iota

	// arithmeticSubtraction is minus.
	arithmeticSubtraction

	// arithmeticMultiplication is multiply.
	arithmeticMultiplication

	// arithmeticDivision is divide, whose result is a Float64 unless a decimal is involved.
	arithmeticDivision

	// arithmeticModulo is the remainder.
	arithmeticModulo

	// arithmeticIntegerDivision is intDiv.
	arithmeticIntegerDivision

	// arithmeticPositiveModulo is positiveModulo, the remainder that is never negative.
	arithmeticPositiveModulo
)

var (
	// arithmeticOperators maps each arithmetic operator to its operation.
	arithmeticOperators = map[string]arithmeticKind{
		operatorPlus:     arithmeticAddition,
		operatorMinus:    arithmeticSubtraction,
		operatorMultiply: arithmeticMultiplication,
		operatorDivide:   arithmeticDivision,
		operatorModulo:   arithmeticModulo,
	}
)

// arithmeticResult types an arithmetic operation the way ClickHouse does.
//
// Two integers give an integer whose signedness and width follow ClickHouse's rules for
// the operation: addition and multiplication widen to the next width above the wider
// operand, signed when either operand is; subtraction does the same but is always signed;
// modulo takes the left operand's sign and the right operand's width, the next width
// above it when the left operand is signed, since a negative remainder of a narrow
// divisor may not fit it; positiveModulo is unsigned at the right operand's width; and
// intDiv keeps the left operand's width. Widening stops at 64 bits. A floating-point
// operand makes the result a Float64, as does division without a decimal; a decimal
// operand keeps the result a decimal unless the other operand is a float. A date or time
// moved by a number keeps its type, and by an interval takes the type the interval's unit
// moves it to; the difference of two dates or times is an Int32, or a Decimal for
// DateTime64.
//
// Takes kind (arithmeticKind) which is the operation.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the result's type, unknown when an operand's type is
// unknown or the operation is not defined on the operands.
func arithmeticResult(kind arithmeticKind, left, right sqlmodel.SQLType) sqlmodel.SQLType {
	if left.Category == sqlmodel.TypeCategoryTemporal || right.Category == sqlmodel.TypeCategoryTemporal {
		return temporalArithmeticResult(kind, left, right)
	}
	if !isPromotableNumericCategory(left.Category) || !isPromotableNumericCategory(right.Category) {
		return unknownType()
	}
	switch {
	case left.Category == sqlmodel.TypeCategoryFloat || right.Category == sqlmodel.TypeCategoryFloat:
		if kind == arithmeticIntegerDivision {
			return integerDivisionOfFloats(left)
		}
		return primitiveType(engineNameFloat64)
	case left.Category == sqlmodel.TypeCategoryDecimal || right.Category == sqlmodel.TypeCategoryDecimal:
		return decimalArithmeticResult(left, right)
	case kind == arithmeticDivision:
		return primitiveType(engineNameFloat64)
	default:
		return integerArithmeticResult(kind, left, right)
	}
}

// integerArithmeticResult applies ClickHouse's integer result rules; see
// arithmeticResult.
//
// Takes kind (arithmeticKind) which is the operation.
// Takes left (sqlmodel.SQLType) which is the left integer.
// Takes right (sqlmodel.SQLType) which is the right integer.
//
// Returns sqlmodel.SQLType which is the integer result.
func integerArithmeticResult(kind arithmeticKind, left, right sqlmodel.SQLType) sqlmodel.SQLType {
	leftSigned, rightSigned := !left.Unsigned, !right.Unsigned
	switch kind {
	case arithmeticSubtraction:
		return integerType(true, widenedInteger(max(left.Width, right.Width)))
	case arithmeticModulo:
		width := right.Width
		if leftSigned {
			width = widenedInteger(width)
		}
		return integerType(leftSigned, width)
	case arithmeticPositiveModulo:
		return integerType(false, right.Width)
	case arithmeticIntegerDivision:
		return integerType(leftSigned || rightSigned, left.Width)
	default:
		return integerType(leftSigned || rightSigned, widenedInteger(max(left.Width, right.Width)))
	}
}

// integerDivisionOfFloats types intDiv over a floating-point operand: the dividend's type
// when it is an integer, and Int64 otherwise.
//
// Takes left (sqlmodel.SQLType) which is the dividend's type.
//
// Returns sqlmodel.SQLType which is the integer result.
func integerDivisionOfFloats(left sqlmodel.SQLType) sqlmodel.SQLType {
	if left.Category == sqlmodel.TypeCategoryInteger {
		return left
	}
	return primitiveType("Int64")
}

// decimalArithmeticResult types arithmetic with a decimal operand: a float makes it a
// Float64, an integer keeps the decimal, and two decimals give the wider.
//
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
func decimalArithmeticResult(left, right sqlmodel.SQLType) sqlmodel.SQLType {
	switch {
	case left.Category != sqlmodel.TypeCategoryDecimal:
		return right
	case right.Category != sqlmodel.TypeCategoryDecimal:
		return left
	case decimalPrecision(right) > decimalPrecision(left):
		return right
	default:
		return left
	}
}

// decimalPrecision returns a decimal type's precision, or zero when it has none.
//
// Takes sqlType (sqlmodel.SQLType) which is the decimal type.
//
// Returns int which is the precision.
func decimalPrecision(sqlType sqlmodel.SQLType) int {
	if sqlType.Precision == nil {
		return 0
	}
	return *sqlType.Precision
}

// temporalArithmeticResult types arithmetic on dates and times: moving one by an interval
// types it by the interval's unit (see movedPointInTime), moving one by a number of its
// own units keeps its type, and subtracting two gives their difference.
//
// Takes kind (arithmeticKind) which is the operation.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the result's type, unknown for an operation dates and
// times do not support.
func temporalArithmeticResult(kind arithmeticKind, left, right sqlmodel.SQLType) sqlmodel.SQLType {
	leftPoint, rightPoint := isPointInTime(left), isPointInTime(right)
	switch {
	case leftPoint && rightPoint && kind == arithmeticSubtraction:
		return pointInTimeDifference(left)
	case leftPoint && !rightPoint && (kind == arithmeticAddition || kind == arithmeticSubtraction) && movesPointInTime(right):
		return movedByValue(left, right)
	case rightPoint && !leftPoint && kind == arithmeticAddition && movesPointInTime(left):
		return movedByValue(right, left)
	default:
		return unknownType()
	}
}

// isPointInTime reports whether a type is a date or a date and time rather than an
// interval.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns bool which is true for a date or timestamp.
func isPointInTime(sqlType sqlmodel.SQLType) bool {
	return sqlType.Category == sqlmodel.TypeCategoryTemporal && sqlType.Temporal != sqlmodel.TemporalKindInterval
}

// movesPointInTime reports whether a value can be added to a date or time: an interval or
// an integer number of its units.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns bool which is true for an interval or an integer.
func movesPointInTime(sqlType sqlmodel.SQLType) bool {
	return sqlType.Category == sqlmodel.TypeCategoryInteger ||
		sqlType.Category == sqlmodel.TypeCategoryTemporal && sqlType.Temporal == sqlmodel.TemporalKindInterval
}

// pointInTimeDifference types the difference of two dates or times: an Int32 number of
// days or seconds, or for DateTime64 a Decimal of seconds at the column's precision.
//
// Takes left (sqlmodel.SQLType) which is the minuend's type.
//
// Returns sqlmodel.SQLType which is the difference's type.
func pointInTimeDifference(left sqlmodel.SQLType) sqlmodel.SQLType {
	if left.Precision == nil {
		return primitiveType("Int32")
	}
	difference := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal,
		"Decimal("+strconv.Itoa(dateTime64DifferencePrecision)+", "+strconv.Itoa(*left.Precision)+")")
	difference.Precision = new(dateTime64DifferencePrecision)
	difference.Scale = new(*left.Precision)
	return difference
}

// negationResult types a prefix minus: an integer becomes signed, widening an unsigned
// one so its range fits, and a float or decimal keeps its type.
//
// Takes operand (sqlmodel.SQLType) which is the operand's type.
//
// Returns sqlmodel.SQLType which is the result's type, unknown for a non-numeric operand.
func negationResult(operand sqlmodel.SQLType) sqlmodel.SQLType {
	switch operand.Category {
	case sqlmodel.TypeCategoryInteger:
		if !operand.Unsigned {
			return operand
		}
		return integerType(true, widenedInteger(operand.Width))
	case sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryDecimal:
		return operand
	default:
		return unknownType()
	}
}

// absoluteResult types abs: an integer becomes unsigned at the same width, and a float or
// decimal keeps its type.
//
// Takes operand (sqlmodel.SQLType) which is the operand's type.
//
// Returns sqlmodel.SQLType which is the result's type, unknown for a non-numeric operand.
func absoluteResult(operand sqlmodel.SQLType) sqlmodel.SQLType {
	switch operand.Category {
	case sqlmodel.TypeCategoryInteger:
		return integerType(false, operand.Width)
	case sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryDecimal:
		return operand
	default:
		return unknownType()
	}
}

// widenedInteger returns the width ClickHouse widens an integer result to: double the
// width up to 64 bits, while 64, 128 and 256 bit operands keep their width.
//
// Takes width (int) which is the operand width in bits.
//
// Returns int which is the result width in bits.
func widenedInteger(width int) int {
	if width >= widestNarrowInteger {
		return width
	}
	return width * 2
}

// integerType returns the ClickHouse integer type of a signedness and width, such as
// Int16 or UInt64.
//
// Takes signed (bool) which is true for a signed type.
// Takes width (int) which is the width in bits.
//
// Returns sqlmodel.SQLType which is the type, unknown for a width ClickHouse lacks.
func integerType(signed bool, width int) sqlmodel.SQLType {
	name := "Int" + strconv.Itoa(width)
	if !signed {
		name = "U" + name
	}
	return primitiveType(name)
}

// unknownType returns the type of a value whose type cannot be told.
//
// Returns sqlmodel.SQLType which is the unknown type.
func unknownType() sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
}
