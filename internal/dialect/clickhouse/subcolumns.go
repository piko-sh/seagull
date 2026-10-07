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

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// operatorSubcolumns is the prefix operator the parser wraps around a value whose
	// element it reads, `value.name`, so the analyser types the element from the value's
	// subcolumns. The spelling is no ClickHouse operator, so it never meets one a query
	// writes.
	operatorSubcolumns = "$subcolumns"

	// subcolumnSizePrefix starts the name of the subcolumn holding the lengths of an array
	// or a map at one nesting level, such as size0.
	subcolumnSizePrefix = "size"

	// subcolumnNull names the subcolumn that marks which values of a Nullable column, or
	// which elements of an array of Nullable values, are NULL.
	subcolumnNull = "null"

	// subcolumnKeys names the subcolumn holding the keys of a map.
	subcolumnKeys = "keys"

	// subcolumnValues names the subcolumn holding the values of a map.
	subcolumnValues = "values"

	// engineNameUInt64 is the canonical ClickHouse UInt64 engine name.
	engineNameUInt64 = "UInt64"

	// engineNameUInt8 is the canonical ClickHouse UInt8 engine name.
	engineNameUInt8 = "UInt8"
)

// subcolumnAccess builds access to a named element or ClickHouse subcolumn.
//
// It handles tuple fields, Nested fields and type-specific subcolumns while preserving
// normal column validation.
//
// Takes container (ast.Expression) which is the value.
// Takes element (string) which is the element's name.
//
// Returns ast.Expression which is the access.
func subcolumnAccess(container ast.Expression, element string) ast.Expression {
	return &ast.StructFieldAccessExpression{
		Struct:    &ast.UnaryOpExpression{Operand: container, Operator: operatorSubcolumns},
		FieldName: element,
	}
}

// subcolumnsOf describes the fields and subcolumns available on a value.
//
// It exposes known subcolumns as tuple fields. Types with dynamic or unknown elements
// keep their original type.
//
// Takes container (sqlmodel.SQLType) which is the value's type.
//
// Returns sqlmodel.SQLType which is the tuple of the value's elements, or the value's own
// type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the elements are NULL.
func subcolumnsOf(container sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour) {
	switch container.Category {
	case sqlmodel.TypeCategoryStruct:
		return tupleOf(tupleSubcolumns(container.StructFields)), sqlmodel.FunctionNullableReturnsNullOnNull
	case sqlmodel.TypeCategoryArray:
		return tupleOf(arraySubcolumns(container)), sqlmodel.FunctionNullableReturnsNullOnNull
	case sqlmodel.TypeCategoryMap:
		return tupleOf(mapSubcolumns(container)), sqlmodel.FunctionNullableReturnsNullOnNull
	case sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryDecimal,
		sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryBoolean,
		sqlmodel.TypeCategoryTemporal, sqlmodel.TypeCategoryUUID, sqlmodel.TypeCategoryNetwork,
		sqlmodel.TypeCategoryEnum:
		return tupleOf([]sqlmodel.StructField{subcolumn(subcolumnNull, primitiveType(engineNameUInt8))}), sqlmodel.FunctionNullableNeverNull
	default:
		return container, sqlmodel.FunctionNullableReturnsNullOnNull
	}
}

// arraySubcolumns lists an array's length, null-mask and tuple-field subcolumns.
//
// Nested lengths use sizeN. Nullable arrays expose a null mask, and tuple arrays expose
// each field as an array.
//
// Takes array (sqlmodel.SQLType) which is the array's type.
//
// Returns []sqlmodel.StructField which holds the subcolumns.
func arraySubcolumns(array sqlmodel.SQLType) []sqlmodel.StructField {
	var fields []sqlmodel.StructField
	sizes := primitiveType(engineNameUInt64)
	level := 0
	for current := &array; current != nil && current.Category == sqlmodel.TypeCategoryArray; current = current.ElementType {
		fields = append(fields, subcolumn(subcolumnSizePrefix+strconv.Itoa(level), sizes))
		sizes = arrayOf(sizes)
		level++
	}
	element := array.ElementType
	if element == nil {
		return fields
	}
	if element.Nullable {
		fields = append(fields, subcolumn(subcolumnNull, arrayOf(primitiveType(engineNameUInt8))))
	}
	if element.Category == sqlmodel.TypeCategoryStruct {
		for index := range element.StructFields {
			fields = append(fields, subcolumn(element.StructFields[index].Name, arrayOf(element.StructFields[index].SQLType)))
		}
	}
	return fields
}

// tupleSubcolumns lists the elements of a tuple as a query may read them: each field by
// its name and, for a named field, also by its position, as t.1 reads a named tuple's
// first field.
//
// Takes fields ([]sqlmodel.StructField) which are the tuple's fields, left unchanged.
//
// Returns []sqlmodel.StructField which holds the elements.
func tupleSubcolumns(fields []sqlmodel.StructField) []sqlmodel.StructField {
	elements := append([]sqlmodel.StructField(nil), fields...)
	for index := range fields {
		position := synthesiseAnonymousFieldName(index + 1)
		if fields[index].Positional || fields[index].Name == position {
			continue
		}
		elements = append(elements, subcolumn(position, fields[index].SQLType))
	}
	return elements
}

// mapSubcolumns lists the subcolumns of a map: its keys and its values, each an array,
// and size0, the number of entries.
//
// Takes mapType (sqlmodel.SQLType) which is the map's type.
//
// Returns []sqlmodel.StructField which holds the subcolumns.
func mapSubcolumns(mapType sqlmodel.SQLType) []sqlmodel.StructField {
	fields := []sqlmodel.StructField{subcolumn(subcolumnSizePrefix+"0", primitiveType(engineNameUInt64))}
	if mapType.KeyType != nil {
		fields = append(fields, subcolumn(subcolumnKeys, arrayOf(*mapType.KeyType)))
	}
	if mapType.ElementType != nil {
		fields = append(fields, subcolumn(subcolumnValues, arrayOf(*mapType.ElementType)))
	}
	return fields
}

// subcolumn builds one named element of the tuple subcolumnsOf types a value as.
//
// Takes name (string) which is the element's name.
// Takes sqlType (sqlmodel.SQLType) which is its type.
//
// Returns sqlmodel.StructField which is the element.
func subcolumn(name string, sqlType sqlmodel.SQLType) sqlmodel.StructField {
	return sqlmodel.StructField{Name: name, SQLType: sqlType, Positional: false}
}
