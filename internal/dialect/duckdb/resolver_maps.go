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
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// mapEngineName is the engine name of a DuckDB MAP type.
	mapEngineName = "map"

	// mapListArguments is the number of arguments map(keys, values) takes: a list of the
	// keys and a list of the values.
	mapListArguments = 2

	// entryValueField is the position of an entry's value among the fields of a
	// map_from_entries entry, after its key.
	entryValueField = 1
)

// resolveMapFromLists resolves map(keys, values), a map from each key of the first list
// to the value at the same position of the second, NULL when either list is. map() makes
// an empty map, typed without key or value types.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the two lists, or none.
//
// Returns *ast.FunctionResolution which types the call.
// Returns error which is always nil.
func resolveMapFromLists(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) != mapListArguments || argumentTypes[0].ElementType == nil || argumentTypes[1].ElementType == nil {
		return mapResolution(sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, mapEngineName), sqlmodel.FunctionNullableReturnsNullOnNull), nil
	}
	built := newMapType(*argumentTypes[0].ElementType, *argumentTypes[1].ElementType)
	return mapResolution(built, sqlmodel.FunctionNullableReturnsNullOnNull), nil
}

// resolveMapLiteral resolves a MAP {key: value, ...} literal, whose arguments are each
// entry's key followed by its value: a map from the keys' common type to the values'
// common type, whose values may be NULL when any value may be. A literal is never NULL.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the keys and values, alternately.
//
// Returns *ast.FunctionResolution which types the literal.
// Returns error which is always nil.
func resolveMapLiteral(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < mapListArguments {
		return mapResolution(sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, mapEngineName), sqlmodel.FunctionNullableNeverNull), nil
	}
	keys := make([]sqlmodel.SQLType, 0, len(argumentTypes)/mapListArguments)
	values := make([]sqlmodel.SQLType, 0, len(argumentTypes)/mapListArguments)
	for index := 0; index+1 < len(argumentTypes); index += mapListArguments {
		keys = append(keys, argumentTypes[index])
		values = append(values, argumentTypes[index+1])
	}
	built := newMapType(commonEntryType(keys), commonEntryType(values))
	return mapResolution(built, sqlmodel.FunctionNullableNeverNull), nil
}

// resolveMapFromEntries resolves map_from_entries, a map built from a list of entries,
// each a struct whose first field is the key and second the value, whatever their names;
// NULL when the list is.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the list of entries.
//
// Returns *ast.FunctionResolution which types the call, a map without key or value types
// when the entries' fields are not known.
// Returns error which is always nil.
func resolveMapFromEntries(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	entry := listElementType(argumentTypes)
	if entry.Category != sqlmodel.TypeCategoryStruct || len(entry.StructFields) <= entryValueField {
		return mapResolution(sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, mapEngineName), sqlmodel.FunctionNullableReturnsNullOnNull), nil
	}
	built := newMapType(entry.StructFields[0].SQLType, entry.StructFields[entryValueField].SQLType)
	return mapResolution(built, sqlmodel.FunctionNullableReturnsNullOnNull), nil
}

// commonEntryType returns the type DuckDB gives a map literal's keys or values: their
// common type, each wider one winning, NULL-able when any of them may be NULL, and
// unknown when none is known.
//
// Takes entries ([]sqlmodel.SQLType) which are the keys' or the values' types.
//
// Returns sqlmodel.SQLType which is the common type.
func commonEntryType(entries []sqlmodel.SQLType) sqlmodel.SQLType {
	common := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	nullable := false
	for index := range entries {
		nullable = nullable || entries[index].Nullable
		common = widerEntryType(common, entries[index])
	}
	common.Nullable = nullable
	return common
}

// widerEntryType widens map keys or values using DuckDB's numeric rules.
//
// Unknown types take the known type. Numeric pairs widen within integers, decimals and
// floats; other pairs keep the first type.
//
// Takes current (sqlmodel.SQLType) which is the common type so far.
// Takes next (sqlmodel.SQLType) which is the next entry's type.
//
// Returns sqlmodel.SQLType which is the wider type.
func widerEntryType(current, next sqlmodel.SQLType) sqlmodel.SQLType {
	switch {
	case current.Category == sqlmodel.TypeCategoryUnknown:
		return next
	case next.Category == sqlmodel.TypeCategoryUnknown:
		return current
	case current.Category == sqlmodel.TypeCategoryInteger && next.Category == sqlmodel.TypeCategoryInteger:
		if promotionRank(next) > promotionRank(current) {
			return next
		}
		return current
	case current.Category == sqlmodel.TypeCategoryFloat && next.Category == sqlmodel.TypeCategoryFloat:
		if next.Width > current.Width {
			return next
		}
		return current
	case current.Category == sqlmodel.TypeCategoryInteger && next.Category == sqlmodel.TypeCategoryDecimal,
		next.Category == sqlmodel.TypeCategoryFloat && isExactNumber(current.Category):
		return next
	default:
		return current
	}
}

// isExactNumber reports whether a category holds exact numbers, an integer or a DECIMAL.
//
// Takes category (sqlmodel.SQLTypeCategory) which is the category.
//
// Returns bool which is true for an integer or a DECIMAL.
func isExactNumber(category sqlmodel.SQLTypeCategory) bool {
	return category == sqlmodel.TypeCategoryInteger || category == sqlmodel.TypeCategoryDecimal
}

// newMapType returns the DuckDB map type from keys of one type to values of another. A
// key is never NULL.
//
// Takes key (sqlmodel.SQLType) which is the key type.
// Takes value (sqlmodel.SQLType) which is the value type.
//
// Returns sqlmodel.SQLType which is the map type.
func newMapType(key, value sqlmodel.SQLType) sqlmodel.SQLType {
	key.Nullable = false
	mapType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, mapEngineName)
	mapType.KeyType = &key
	mapType.ElementType = &value
	return mapType
}

// mapResolution types a call that builds a map and only reads data.
//
// Takes returnType (sqlmodel.SQLType) which is the map's type.
// Takes nullable (sqlmodel.FunctionNullableBehaviour) which says when the map is NULL.
//
// Returns *ast.FunctionResolution which types the call.
func mapResolution(returnType sqlmodel.SQLType, nullable sqlmodel.FunctionNullableBehaviour) *ast.FunctionResolution {
	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: nullable,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}
