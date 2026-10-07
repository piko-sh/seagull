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
	"strings"

	"seagull.sh/seagull/internal/sqlmodel"
)

// RestatesColumnType reports whether a declaration preserves the column's SQL type.
//
// Restating the type preserves its Go override. Omitted length, precision and scale
// modifiers do not change the type, and the declaration's own nullability is checked
// separately.
//
// Takes declared (sqlmodel.SQLType) which is the stated type.
// Takes column (sqlmodel.SQLType) which is the column's type.
//
// Returns bool which is true when the stated type is the column's own.
func RestatesColumnType(declared, column sqlmodel.SQLType) bool {
	return sameDeclaredType(&declared, &column, false, 0)
}

// sameDeclaredType compares a declared type with a column's type at one level of their
// structure.
//
// Takes declared (*sqlmodel.SQLType) which is the declared type at this level.
// Takes column (*sqlmodel.SQLType) which is the column's type at this level.
// Takes nested (bool) which is true below the top level, where nullability is part of the
// type.
// Takes depth (int) which is the level's depth; types that agree for
// sqlmodel.MaxTypeDepth levels do not agree past them.
//
// Returns bool which is true when the types agree.
func sameDeclaredType(declared, column *sqlmodel.SQLType, nested bool, depth int) bool {
	if declared == nil || column == nil {
		return declared == column
	}
	if depth >= sqlmodel.MaxTypeDepth || !sameTypeIdentity(declared, column) {
		return false
	}
	if nested && declared.Nullable != column.Nullable {
		return false
	}
	if !sameGivenModifier(declared.Length, column.Length) || !sameGivenModifier(declared.Precision, column.Precision) ||
		!sameGivenModifier(declared.Scale, column.Scale) {
		return false
	}
	return sameDeclaredType(declared.ElementType, column.ElementType, true, depth+1) &&
		sameDeclaredType(declared.KeyType, column.KeyType, true, depth+1) &&
		sameDeclaredFields(declared.StructFields, column.StructFields, depth+1) &&
		sameDeclaredMembers(declared.UnionMembers, column.UnionMembers, depth+1)
}

// sameTypeIdentity compares what names a type and its structural attributes: category,
// engine name, schema when both carry one, width, signedness, temporal and network kind,
// time zone and money.
//
// Takes declared (*sqlmodel.SQLType) which is the declared type.
// Takes column (*sqlmodel.SQLType) which is the column's type.
//
// Returns bool which is true when they agree.
func sameTypeIdentity(declared, column *sqlmodel.SQLType) bool {
	if declared.Category != column.Category || !strings.EqualFold(declared.EngineName, column.EngineName) {
		return false
	}
	if declared.Schema != "" && column.Schema != "" && !strings.EqualFold(declared.Schema, column.Schema) {
		return false
	}
	return declared.Width == column.Width && declared.Unsigned == column.Unsigned &&
		declared.Temporal == column.Temporal && declared.TimeZone == column.TimeZone &&
		declared.Network == column.Network && declared.Monetary == column.Monetary
}

// sameGivenModifier reports whether a modifier a declaration gives, a length, precision
// or scale, is the column's; a modifier the declaration leaves out agrees with any.
//
// Takes declared (*int) which is the declared modifier, or nil.
// Takes column (*int) which is the column's modifier, or nil.
//
// Returns bool which is true when they agree.
func sameGivenModifier(declared, column *int) bool {
	return declared == nil || (column != nil && *declared == *column)
}

// sameDeclaredFields compares the fields of two struct types by name, position and type.
//
// Takes declared ([]sqlmodel.StructField) which are the declared type's fields.
// Takes column ([]sqlmodel.StructField) which are the column type's fields.
// Takes depth (int) which bounds the walk.
//
// Returns bool which is true when the fields agree.
func sameDeclaredFields(declared, column []sqlmodel.StructField, depth int) bool {
	if len(declared) != len(column) {
		return false
	}
	for index := range declared {
		if declared[index].Positional != column[index].Positional ||
			!strings.EqualFold(declared[index].Name, column[index].Name) ||
			!sameDeclaredType(&declared[index].SQLType, &column[index].SQLType, true, depth) {
			return false
		}
	}
	return true
}

// sameDeclaredMembers compares the members of two union types by tag and type.
//
// Takes declared ([]sqlmodel.UnionMember) which are the declared type's members.
// Takes column ([]sqlmodel.UnionMember) which are the column type's members.
// Takes depth (int) which bounds the walk.
//
// Returns bool which is true when the members agree.
func sameDeclaredMembers(declared, column []sqlmodel.UnionMember, depth int) bool {
	if len(declared) != len(column) {
		return false
	}
	for index := range declared {
		if !strings.EqualFold(declared[index].Tag, column[index].Tag) ||
			!sameDeclaredType(&declared[index].SQLType, &column[index].SQLType, true, depth) {
			return false
		}
	}
	return true
}
