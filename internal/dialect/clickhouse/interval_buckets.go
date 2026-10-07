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
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// bucketIntervalArgument is the position of the interval among the arguments of
	// toStartOfInterval, after the date or time it buckets.
	bucketIntervalArgument = 1

	// transformDefaultArguments is the number of arguments of a transform call that gives a
	// default for the values it does not map.
	transformDefaultArguments = 4

	// transformReplacementsArgument is the position of the replacements among the arguments
	// of transform.
	transformReplacementsArgument = 2
)

// dateBucketUnits names the interval units toStartOfInterval truncates a date or time to
// a date by, as an Interval type names them.
var dateBucketUnits = map[string]bool{"Week": true, "Month": true, "Quarter": true, "Year": true}

// resolveStartOfInterval types an interval bucket from its value and unit.
//
// Weeks and larger units yield Date; fractional units yield DateTime64; other units yield
// DateTime. Nullability follows the value.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
//
// Returns *ast.FunctionResolution which types the bucket, or nil when the second argument
// is not an interval of a known unit.
func resolveStartOfInterval(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) <= bucketIntervalArgument {
		return nil
	}
	interval := argumentTypes[bucketIntervalArgument]
	unit, isInterval := intervalUnit(interval)
	if !isInterval {
		return nil
	}
	unitName := interval.EngineName[len(intervalTypePrefix):]
	value := argumentTypes[0]
	bucket := primitiveType(engineNameDateTime)
	switch {
	case dateBucketUnits[unitName]:
		bucket = primitiveType(engineNameDate)
	case unit.precision > 0 && value.Precision != nil:
		bucket = dateTime64Type(max(*value.Precision, unit.precision))
	case unit.precision > 0:
		bucket = dateTime64Type(unit.precision)
	}
	return &ast.FunctionResolution{
		ReturnType:        bucket,
		NullingArgument:   0,
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}

// resolveTransform types transform(value, from, to[, default]) as ClickHouse does: as the
// value when the call gives no default, since the replacements must then be of the
// value's type, and otherwise as the common type of the replacements' elements and the
// default. The result is NULL when an argument is.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
//
// Returns *ast.FunctionResolution which types the call, or nil when the type it takes is
// unknown.
func resolveTransform(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	if len(argumentTypes) <= transformReplacementsArgument {
		return nil
	}
	result := argumentTypes[0]
	if len(argumentTypes) >= transformDefaultArguments {
		replacements := argumentTypes[transformReplacementsArgument]
		if replacements.ElementType == nil {
			return nil
		}
		result = unifyBranchTypes([]sqlmodel.SQLType{*replacements.ElementType, argumentTypes[transformDefaultArguments-1]})
	}
	if result.Category == sqlmodel.TypeCategoryUnknown {
		return nil
	}
	return &ast.FunctionResolution{
		ReturnType:        result,
		NullingArgument:   0,
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}
