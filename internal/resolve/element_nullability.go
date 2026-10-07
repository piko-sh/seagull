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

package resolve

import (
	"slices"
	"strings"

	"seagull.sh/seagull/internal/sqlmodel"
)

// ElementNullability decides when generated collection elements use pointers.
//
// For engines that allow NULL elements regardless of the declared type, column settings,
// overrides and the preset determine the default. Elements already known to be nullable
// remain nullable.
type ElementNullability struct {
	// holds reports whether the engine can hold NULL among the elements or values of a type;
	// nil for an engine whose types say where NULL can be.
	holds func(sqlmodel.SQLType) bool

	// typeChoices holds what the type overrides declare for their types' elements.
	typeChoices []ElementTypeChoice

	// nonNull is the preset's choice.
	nonNull bool
}

// NewElementNullability returns the element nullability of one generation.
//
// Takes holds (func(sqlmodel.SQLType) bool) which reports whether the engine can hold
// NULL among a type's elements or values, or is nil for an engine whose types say so.
// Takes nonNull (bool) which is the preset's choice: true declares elements non-null.
// Takes typeChoices ([]ElementTypeChoice) which hold the type overrides' choices.
//
// Returns *ElementNullability which applies them.
func NewElementNullability(holds func(sqlmodel.SQLType) bool, nonNull bool, typeChoices []ElementTypeChoice) *ElementNullability {
	return &ElementNullability{holds: holds, typeChoices: slices.Clone(typeChoices), nonNull: nonNull}
}

// DeclaredOnly returns the element nullability of values whose elements are plain unless
// the value itself declares them nullable, as the parameters of a driver that cannot bind
// a NULL element take it: the preset's choice and the type overrides' give way to
// non-null elements.
//
// Returns *ElementNullability which applies it, or nil for a nil receiver.
func (e *ElementNullability) DeclaredOnly() *ElementNullability {
	if e == nil {
		return nil
	}
	return &ElementNullability{holds: e.holds, typeChoices: nil, nonNull: true}
}

// Apply returns sqlType with each element and map value the engine can hold NULL in
// marked nullable, unless declared non-null, at every depth: in nested arrays, in the
// values of maps and in the fields of structs.
//
// Map keys, which hold no NULL, are left alone. The type given is not changed.
//
// Takes sqlType (sqlmodel.SQLType) which is a column's value type.
// Takes column (*bool) which is the column's own choice, or nil when it declares none.
//
// Returns sqlmodel.SQLType which is the marked copy.
func (e *ElementNullability) Apply(sqlType sqlmodel.SQLType, column *bool) sqlmodel.SQLType {
	if e == nil || e.holds == nil {
		return sqlType
	}
	return e.mark(sqlType, column, 0)
}

// mark marks one level of a type and the levels below it.
//
// Takes sqlType (sqlmodel.SQLType) which is the type at this level, a copy.
// Takes column (*bool) which is the column's own choice, or nil.
// Takes depth (int) which is the level's depth, the column's own type at zero; a type
// nested sqlmodel.MaxTypeDepth or more levels deep is left as it is.
//
// Returns sqlmodel.SQLType which is the marked type.
func (e *ElementNullability) mark(sqlType sqlmodel.SQLType, column *bool, depth int) sqlmodel.SQLType {
	if depth >= sqlmodel.MaxTypeDepth {
		return sqlType
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryArray, sqlmodel.TypeCategoryMap:
		if sqlType.ElementType == nil {
			return sqlType
		}
		element := e.mark(*sqlType.ElementType, column, depth+1)
		if e.holds(sqlType) && !e.declaredNonNull(sqlType, column) {
			element.Nullable = true
		}
		sqlType.ElementType = &element
	case sqlmodel.TypeCategoryStruct:
		fields := slices.Clone(sqlType.StructFields)
		for index := range fields {
			fields[index].SQLType = e.mark(fields[index].SQLType, column, depth+1)
		}
		sqlType.StructFields = fields
	default:
	}
	return sqlType
}

// declaredNonNull reports whether a type's elements are declared non-null: by the column,
// or else by the last type override of the type, or else by the preset.
//
// Takes sqlType (sqlmodel.SQLType) which is the array, list or map type.
// Takes column (*bool) which is the column's own choice, or nil.
//
// Returns bool which is true when the elements are declared non-null.
func (e *ElementNullability) declaredNonNull(sqlType sqlmodel.SQLType, column *bool) bool {
	if column != nil {
		return *column
	}
	for index := len(e.typeChoices) - 1; index >= 0; index-- {
		if sameElementsType(&e.typeChoices[index].SQLType, &sqlType, 0) {
			return e.typeChoices[index].NonNull
		}
	}
	return e.nonNull
}

// ElementTypeChoice is what a type override declares for the elements and values of its
// SQL type.
type ElementTypeChoice struct {
	// SQLType is the overridden type, an array, list or map.
	SQLType sqlmodel.SQLType

	// NonNull is true when the type's elements are declared non-null, and false when they
	// are declared nullable.
	NonNull bool
}

// sameElementsType reports whether a type override's type is a value's type: the same
// category and engine name, ignoring case, at every level of their elements and keys.
//
// An array is matched by its elements alone, since the name of an array type differs with
// how it was written, int4[] or integer[].
//
// Takes declared (*sqlmodel.SQLType) which is the override's type at this level.
// Takes value (*sqlmodel.SQLType) which is the value's type at this level.
// Takes depth (int) which is the level's depth; types that agree for
// sqlmodel.MaxTypeDepth levels are not the same past them.
//
// Returns bool which is true when they are the same type.
func sameElementsType(declared, value *sqlmodel.SQLType, depth int) bool {
	if declared == nil || value == nil {
		return declared == value
	}
	if depth >= sqlmodel.MaxTypeDepth || declared.Category != value.Category || declared.Schema != value.Schema {
		return false
	}
	if declared.Category != sqlmodel.TypeCategoryArray && !strings.EqualFold(declared.EngineName, value.EngineName) {
		return false
	}
	return sameElementsType(declared.ElementType, value.ElementType, depth+1) &&
		sameElementsType(declared.KeyType, value.KeyType, depth+1)
}
