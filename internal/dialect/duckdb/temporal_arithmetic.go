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
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// operatorAdd adds numbers, or moves a date, time or timestamp by an interval.
	operatorAdd = "+"

	// operatorSubtract subtracts numbers, measures the time between two dates or timestamps,
	// or moves a date, time or timestamp back by an interval.
	operatorSubtract = "-"

	// operatorMultiply multiplies numbers, or scales an interval.
	operatorMultiply = "*"
)

// temporalArithmeticResult types DuckDB arithmetic involving dates, times, timestamps or
// intervals.
//
// Date differences yield BIGINT and timestamp differences yield INTERVAL. Adding an
// interval or time to a date yields TIMESTAMP.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns bool which is false when no rule applies.
func temporalArithmeticResult(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	switch operator {
	case operatorAdd:
		if result, ok := temporalShift(left, right); ok {
			return result, true
		}
		return temporalShift(right, left)
	case operatorSubtract:
		switch {
		case left.Temporal == sqlmodel.TemporalKindDate && right.Temporal == sqlmodel.TemporalKindDate:
			return builtinTypeMap["bigint"], true
		case isPointInTime(left) && isPointInTime(right):
			return builtinTypeMap["interval"], true
		default:
			return temporalShift(left, right)
		}
	case operatorMultiply:
		if isInterval(left) && isNumeric(right) || isNumeric(left) && isInterval(right) {
			return builtinTypeMap["interval"], true
		}
	case operatorDivide:
		if isInterval(left) && isNumeric(right) {
			return builtinTypeMap["interval"], true
		}
	}
	return sqlmodel.SQLType{}, false
}

// temporalShift types a point moved by an offset: a date by a number of days stays a
// date, a date by an interval or combined with a time becomes a timestamp, and a
// timestamp or a time by an interval keeps its type.
//
// Takes point (sqlmodel.SQLType) which is the operand being moved.
// Takes offset (sqlmodel.SQLType) which is the operand it is moved by.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns bool which is false when the operands are not a point and an offset.
func temporalShift(point, offset sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	switch point.Temporal {
	case sqlmodel.TemporalKindDate:
		switch {
		case offset.Category == sqlmodel.TypeCategoryInteger:
			return builtinTypeMap["date"], true
		case offset.Temporal == sqlmodel.TemporalKindInterval:
			return builtinTypeMap["timestamp"], true
		case offset.Temporal == sqlmodel.TemporalKindTime && offset.TimeZone:
			return builtinTypeMap["timestamptz"], true
		case offset.Temporal == sqlmodel.TemporalKindTime:
			return builtinTypeMap["timestamp"], true
		}
	case sqlmodel.TemporalKindTimestamp, sqlmodel.TemporalKindTime:
		if offset.Temporal == sqlmodel.TemporalKindInterval {
			return sqlmodel.NewTemporalType(point.EngineName, point.Temporal, point.TimeZone), true
		}
	case sqlmodel.TemporalKindNone, sqlmodel.TemporalKindInterval:
	}
	return sqlmodel.SQLType{}, false
}

// isPointInTime reports whether a type holds a calendar date or a timestamp.
//
// Takes sqlType (sqlmodel.SQLType) which is the type to test.
//
// Returns bool which is true for a date or a timestamp.
func isPointInTime(sqlType sqlmodel.SQLType) bool {
	return sqlType.Temporal == sqlmodel.TemporalKindDate || sqlType.Temporal == sqlmodel.TemporalKindTimestamp
}

// isInterval reports whether a type holds a length of time.
//
// Takes sqlType (sqlmodel.SQLType) which is the type to test.
//
// Returns bool which is true for an interval.
func isInterval(sqlType sqlmodel.SQLType) bool {
	return sqlType.Temporal == sqlmodel.TemporalKindInterval
}
