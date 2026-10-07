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

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// TypedTableValuedFunctionColumns types the column of generate_series and range, which
// count integers as BIGINT and step timestamps, by their bounds, and the column of unnest
// by the element type of the list it reads, as DuckDB resolves their overloads.
//
// Other functions, and calls whose arguments do not decide the type, are left to
// TableValuedFunctionColumns.
//
// Takes functionName (string) which is the function's name as the reference writes it.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of its arguments.
//
// Returns []ast.ScopedColumn which holds the typed column, or nil.
func (*Dialect) TypedTableValuedFunctionColumns(
	_ *sqlmodel.Catalogue,
	functionName string,
	argumentTypes []sqlmodel.SQLType,
) []ast.ScopedColumn {
	name := strings.ToLower(functionName)
	switch name {
	case "generate_series", "range":
		if seriesType, typed := seriesType(argumentTypes); typed {
			return []ast.ScopedColumn{{Name: name, SQLType: seriesType, Nullable: false}}
		}
	case "unnest":
		if len(argumentTypes) == 1 && argumentTypes[0].Category == sqlmodel.TypeCategoryArray && argumentTypes[0].ElementType != nil {
			return []ast.ScopedColumn{{Name: name, SQLType: *argumentTypes[0].ElementType, Nullable: true}}
		}
	}
	return nil
}

// seriesType determines the values' type for generate_series and range from the bounds
// and step.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of the call's arguments:
// the bounds, then the step.
//
// Returns sqlmodel.SQLType which is the type of the values.
// Returns bool which is false when the bounds do not decide it.
func seriesType(argumentTypes []sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	bounds := argumentTypes[:min(len(argumentTypes), 2)]
	pointInTime, timeZone, integer := false, false, false
	for index := range bounds {
		pointInTime = pointInTime || isPointInTime(bounds[index])
		timeZone = timeZone || bounds[index].TimeZone
		integer = integer || bounds[index].Category == sqlmodel.TypeCategoryInteger
	}
	switch {
	case pointInTime && timeZone:
		return builtinTypeMap["timestamptz"], true
	case pointInTime:
		return builtinTypeMap["timestamp"], true
	case integer:
		return builtinTypeMap["bigint"], true
	case len(argumentTypes) > 2 && isInterval(argumentTypes[2]):
		return builtinTypeMap["timestamp"], true
	default:
		return sqlmodel.SQLType{}, false
	}
}
