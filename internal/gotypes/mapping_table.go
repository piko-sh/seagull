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

package gotypes

import (
	"slices"
	"strings"

	"seagull.sh/seagull/internal/sqlmodel"
)

// MappingTable maps SQL types to Go types: the configured type overrides first, then the
// structural mapping under the table's type slots. It implements sqlmodel.GoTypeResolver,
// the form emitters see it in.
type MappingTable struct {
	// Namer chooses names for generated enum and struct types. Unnamed types use the
	// structural mapping's ordinary Go representation.
	Namer func(sqlType sqlmodel.SQLType) (string, bool)

	// Slots holds the type slots the structural mapping uses; a zero slot keeps the built-in
	// default.
	Slots sqlmodel.TypeSlots

	// Mappings holds the overrides in order; later entries override earlier ones for the
	// same SQL type. An entry with a SQLName matches the type of that engine name, and one
	// without matches its whole category.
	Mappings []Mapping
}

// Resolve maps a SQL type to the Go type the table assigns it, for a NOT NULL or a
// nullable value.
//
// An override naming the type's engine name wins over one that covers the whole category,
// and a case-sensitive name match wins over a case-insensitive one. A type no override
// covers maps by its structure.
//
// Takes sqlType (sqlmodel.SQLType) which is the SQL type to map.
// Takes nullable (bool) which selects the nullable Go type.
//
// Returns sqlmodel.GoType which is the Go type.
func (t *MappingTable) Resolve(sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	return t.resolveDepth(sqlType, nullable, 0)
}

// NamesType reports whether a SQL type uses a generated enum or struct name.
//
// Overrides and configured type slots take precedence. The naming plan uses this result
// to decide which types to declare.
//
// Takes sqlType (sqlmodel.SQLType) which is the SQL type to check.
//
// Returns bool which is true when a name the Namer gives the type decides its Go type.
func (t *MappingTable) NamesType(sqlType sqlmodel.SQLType) bool {
	if exactMatch, categoryMatch := t.candidates(sqlType); exactMatch != nil || categoryMatch != nil {
		return false
	}
	var slots sqlmodel.TypeSlots
	if t != nil {
		slots = t.Slots
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryEnum:
		return slots.Enum.IsZero()
	case sqlmodel.TypeCategoryStruct:
		return slots.Struct.IsZero() && hasNamedFields(&sqlType)
	default:
		return false
	}
}

// OverridesWithin reports whether a type or a nested part uses a configured Go override.
//
// These user-selected types may need different driver handling from generated types. A
// nil table has no overrides.
//
// Takes sqlType (sqlmodel.SQLType) which is the SQL type.
//
// Returns bool which is true when an override chooses the type or a part of it.
func (t *MappingTable) OverridesWithin(sqlType sqlmodel.SQLType) bool {
	return t != nil && t.overridesWithin(&sqlType, 0)
}

// overridesWithin is OverridesWithin at one level of a type, bounded by the depth the
// structural mapping follows.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type at this level, or nil.
// Takes depth (int) which is how deep the type is nested.
//
// Returns bool which is true when an override chooses the type or a part of it.
func (t *MappingTable) overridesWithin(sqlType *sqlmodel.SQLType, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	if exactMatch, categoryMatch := t.candidates(*sqlType); exactMatch != nil || categoryMatch != nil {
		return true
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryArray:
		return t.overridesWithin(sqlType.ElementType, depth+1)
	case sqlmodel.TypeCategoryMap:
		return t.Slots.Map.IsZero() && (t.overridesWithin(sqlType.KeyType, depth+1) || t.overridesWithin(sqlType.ElementType, depth+1))
	case sqlmodel.TypeCategoryStruct:
		if !t.Slots.Struct.IsZero() {
			return false
		}
		for index := range sqlType.StructFields {
			if t.overridesWithin(&sqlType.StructFields[index].SQLType, depth+1) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// resolveDepth is Resolve with a bound on the recursion through element, key and field
// types, so a pathologically deep type cannot overflow the goroutine stack.
//
// Takes sqlType (sqlmodel.SQLType) which is the SQL type to map.
// Takes nullable (bool) which selects the nullable Go type.
// Takes depth (int) which is the current recursion depth.
//
// Returns sqlmodel.GoType which is the Go type.
func (t *MappingTable) resolveDepth(sqlType sqlmodel.SQLType, nullable bool, depth int) sqlmodel.GoType {
	exactMatch, categoryMatch := t.candidates(sqlType)
	chosen := exactMatch
	if chosen == nil {
		chosen = categoryMatch
	}
	if chosen != nil {
		if nullable {
			return chosen.Nullable
		}
		return chosen.NotNull
	}
	var slots sqlmodel.TypeSlots
	var namer func(sqlmodel.SQLType) (string, bool)
	if t != nil {
		slots = t.Slots
		namer = t.Namer
	}
	mapper := &structuralMapper{table: t, namer: namer, slots: slots, depth: depth}
	return mapper.goType(sqlType, nullable)
}

// candidates walks the overrides in reverse declaration order (later entries override
// earlier ones) and returns the best name match and the best category-level match for a
// SQL type. Either may be nil when no override covers the type.
//
// A case-sensitive name match is preferred over a case-insensitive one so names that
// differ only by case stay distinct. The fold remains a fallback so an override written
// in a different case still resolves.
//
// Takes sqlType (sqlmodel.SQLType) which provides the category and dialect type name to
// look up.
//
// Returns exactMatch which is the best name match, or nil when none matched.
// Returns categoryMatch which is the first mapping with no SQLName whose category
// matches, used when no name matches.
func (t *MappingTable) candidates(sqlType sqlmodel.SQLType) (exactMatch *Mapping, categoryMatch *Mapping) {
	if t == nil {
		return nil, nil
	}
	var foldMatch *Mapping
	for index := range slices.Backward(t.Mappings) {
		mapping := &t.Mappings[index]
		switch {
		case mapping.SQLCategory != sqlType.Category:
		case mapping.SQLName == "":
			if categoryMatch == nil {
				categoryMatch = mapping
			}
		case !mapping.coversSchema(sqlType.Schema):
		case mapping.SQLName == sqlType.EngineName:
			return mapping, categoryMatch
		case foldMatch == nil && strings.EqualFold(mapping.SQLName, sqlType.EngineName):
			foldMatch = mapping
		}
	}
	return foldMatch, categoryMatch
}

// Mapping maps a SQL type to Go types for both nullable and non-nullable contexts.
type Mapping struct {
	// NotNull is the Go type to use when the column is NOT NULL.
	NotNull sqlmodel.GoType

	// Nullable is the Go type to use when the column is nullable.
	Nullable sqlmodel.GoType

	// SQLName is an optional engine-specific name for finer-grained matching (e.g. "numeric"
	// vs "decimal"), where empty matches any name in the category.
	SQLName string

	// SQLSchema, when set, is the schema of the enum or composite type SQLName names, so the
	// mapping leaves a same-named type in another schema alone.
	SQLSchema string

	// SQLCategory is the type category this mapping applies to.
	SQLCategory sqlmodel.SQLTypeCategory
}

// coversSchema reports whether a mapping applies to a type of the given schema: a mapping
// with no schema applies to every schema, and a type with no schema to every mapping.
//
// Takes schema (string) which is the type's schema, or empty.
//
// Returns bool which is true when the mapping applies.
func (m *Mapping) coversSchema(schema string) bool {
	return m.SQLSchema == "" || schema == "" || strings.EqualFold(m.SQLSchema, schema)
}
