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

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// pointerPrefix starts the name of a Go pointer type.
	pointerPrefix = "*"

	// slicePrefix starts the name of a Go slice type.
	slicePrefix = "[]"

	// mapPrefix starts the name of a Go map type.
	mapPrefix = "map["
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

	// NullValues resolves a nullable value to the generated Null[T] value type rather than
	// to a pointer, *T.
	NullValues bool
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
	resolved := t.resolveDepth(sqlType, nullable, 0)
	if nullable && t != nil && t.NullValues && holdsScalar(sqlType.Category) && !t.heldByPointer(sqlType) {
		return NullValue(resolved)
	}
	return resolved
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

// heldByPointer reports whether a type's NOT NULL value is already a pointer, as DuckDB's
// HUGEINT is a *big.Int, so its nullable value stays the same pointer, nil for NULL,
// rather than a Null of a value the driver hands over through a pointer.
//
// Takes sqlType (sqlmodel.SQLType) which is the SQL type.
//
// Returns bool which is true when the NOT NULL Go type is a pointer.
func (t *MappingTable) heldByPointer(sqlType sqlmodel.SQLType) bool {
	notNull := t.resolveDepth(sqlType, false, 0)
	return notNull.Composite == nil && strings.HasPrefix(notNull.Name, pointerPrefix)
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

// holdsScalar reports whether a SQL type category contains one scalar value.
//
// Structs, composites, maps, and arrays contain parts. Nullable values of those types
// stay pointers so drivers can handle their parts. Network addresses also stay pointers
// because drivers use their own types, such as clickhouse-go's net.IP.
//
// Takes category (sqlmodel.SQLTypeCategory) which is the SQL type's category.
//
// Returns bool which is true for a scalar category.
func holdsScalar(category sqlmodel.SQLTypeCategory) bool {
	switch category {
	case sqlmodel.TypeCategoryStruct, sqlmodel.TypeCategoryComposite, sqlmodel.TypeCategoryMap, sqlmodel.TypeCategoryArray,
		sqlmodel.TypeCategoryNetwork:
		return false
	default:
		return true
	}
}

// NullValue turns a pointer to a named type, *T, into the generated Null[T] value type,
// and leaves every other type, such as a slice, a map or a pointer to a struct, as it is.
//
// Takes goType (sqlmodel.GoType) which is the nullable Go type.
//
// Returns sqlmodel.GoType which is Null[T], or goType unchanged.
func NullValue(goType sqlmodel.GoType) sqlmodel.GoType {
	element, pointer := strings.CutPrefix(goType.Name, pointerPrefix)
	if goType.Composite != nil || !pointer || element == "" || !namesValueType(element) {
		return goType
	}
	inner := goast.WrittenGoType(goType.Package, element)
	return sqlmodel.NewGoInstance(sqlmodel.GoType{Composite: nil, Package: "", Name: emit.InlineNullTypeName}, inner)
}

// namesValueType reports whether a Go type name, written after a pointer's star, names a
// value type rather than another pointer, a slice or a map.
//
// Takes name (string) which is the type's name.
//
// Returns bool which is true for a value type.
func namesValueType(name string) bool {
	for _, prefix := range []string{pointerPrefix, slicePrefix, mapPrefix} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}
