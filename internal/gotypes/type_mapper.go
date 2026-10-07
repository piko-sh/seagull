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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// TypeMapper builds the mapping table for one generation run from the type slots and the
// user's type overrides.
type TypeMapper struct {
	// typeCatalogue holds the dialect's type catalogue, used to resolve an override's SQL
	// type name to the structured type it names.
	typeCatalogue *ast.TypeCatalogue

	// schema holds the schema catalogue, used to resolve an override's SQL type name to an
	// enum or composite type the migrations create; nil resolves built-in names only.
	schema *sqlmodel.Catalogue
}

// NewTypeMapper creates a new type mapper with the given type catalogue.
//
// Takes typeCatalogue (*ast.TypeCatalogue) which holds the engine-specific SQL type
// definitions.
//
// Returns *TypeMapper which is ready to build mapping tables and resolve SQL-to-Go type
// mappings.
func NewTypeMapper(typeCatalogue *ast.TypeCatalogue) *TypeMapper {
	return &TypeMapper{
		typeCatalogue: typeCatalogue,
		schema:        nil,
	}
}

// WithSchema returns a copy of the mapper that also resolves an override's SQL type name
// to the enums and composite types of a schema catalogue, so an override such as `sql:
// mood` or `sql: public.mood` maps a type a migration creates.
//
// Takes schema (*sqlmodel.Catalogue) which holds the enums and composite types.
//
// Returns *TypeMapper which is the copy.
func (m *TypeMapper) WithSchema(schema *sqlmodel.Catalogue) *TypeMapper {
	return &TypeMapper{typeCatalogue: m.typeCatalogue, schema: schema}
}

// BuildMappingTable creates the mapping table from the supplied type slot layers and the
// user's type overrides.
//
// The structural mapping uses the slots of every layer, later layers taking precedence
// slot by slot (an emitter's TypeDefaulterPort slots, then the preset's slots); a zero
// slot in a layer leaves the lower layers in force. Type overrides rank above the
// structural mapping. A go_type directive on a column, and a column type override,
// outrank the whole table; column overrides are applied to the catalogue instead, so this
// skips them, as it skips an override that names no Go type and declares only its
// elements' nullability.
//
// Takes overrides ([]sqlmodel.TypeOverride) which holds user-provided SQL-to-Go type
// override definitions.
// Takes slotLayers (...sqlmodel.TypeSlots) which holds the slot layers, lowest precedence
// first.
//
// Returns *MappingTable which holds the combined mapping table.
func (m *TypeMapper) BuildMappingTable(
	overrides []sqlmodel.TypeOverride,
	slotLayers ...sqlmodel.TypeSlots,
) *MappingTable {
	var mappings []Mapping
	for _, override := range overrides {
		if override.Column != "" || override.GoName == "" {
			continue
		}
		sqlType := m.resolveOverrideType(override)
		goType := goast.WrittenGoType(override.GoPackage, override.GoName)

		schema := ""
		if sqlType.Category == sqlmodel.TypeCategoryEnum || sqlType.Category == sqlmodel.TypeCategoryComposite {
			schema = sqlType.Schema
		}
		mappings = append(mappings, Mapping{
			SQLCategory: sqlType.Category,
			SQLName:     sqlType.EngineName,
			SQLSchema:   schema,
			NotNull:     goType,
			Nullable:    goast.WrittenGoType(override.GoPackage, "*"+override.GoName),
		})
	}

	return &MappingTable{
		Namer:    nil,
		Mappings: mappings,
		Slots:    mergeSlotLayers(slotLayers),
	}
}

// ResolveOverrideType resolves a type override's SQL type name to the type it names, as
// the mapping table resolves it: among the dialect's built-in types, then among the
// schema's enums and composite types, unwrapping array suffixes.
//
// Takes override (sqlmodel.TypeOverride) which holds the SQL type name to resolve.
//
// Returns sqlmodel.SQLType which is the resolved type, or the unknown category.
func (m *TypeMapper) ResolveOverrideType(override sqlmodel.TypeOverride) sqlmodel.SQLType {
	return m.resolveOverrideType(override)
}

// resolveOverrideType resolves a user-provided type override to its SQL type by looking
// up the type name among the dialect's built-in types and then among the schema's enums
// and composite types, falling back to an unknown category if not found.
//
// Takes override (sqlmodel.TypeOverride) which holds the SQL type name to resolve.
//
// Returns sqlmodel.SQLType which is the resolved SQL type with its category and engine
// name.
func (m *TypeMapper) resolveOverrideType(override sqlmodel.TypeOverride) sqlmodel.SQLType {
	return m.resolveOverrideTypeDepth(override, 0)
}

// resolveOverrideTypeDepth performs the array-suffix recursion with a depth counter so a
// pathological override cannot allocate unboundedly.
//
// Once depth reaches sqlmodel.MaxArrayDimensions the function stops unwrapping further
// `[]` suffixes and returns an unknown type instead.
//
// Takes override (sqlmodel.TypeOverride) which carries the SQL type name to resolve.
// Takes depth (int) which is the current array-unwrap recursion depth.
//
// Returns sqlmodel.SQLType which is the resolved type, or the unknown category once the
// depth limit is reached.
func (m *TypeMapper) resolveOverrideTypeDepth(override sqlmodel.TypeOverride, depth int) sqlmodel.SQLType {
	lower := strings.ToLower(override.SQLTypeName)

	if m.typeCatalogue != nil {
		if sqlType, exists := m.typeCatalogue.Types[lower]; exists {
			return sqlType
		}
	}
	if named := m.schema.ResolveNamedType(sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, override.SQLTypeName)); named.Category != sqlmodel.TypeCategoryUnknown {
		return named
	}

	if depth < sqlmodel.MaxArrayDimensions {
		if stripped, found := strings.CutSuffix(lower, "[]"); found {
			arrayType := sqlmodel.SQLType{}
			arrayType.Category = sqlmodel.TypeCategoryArray
			arrayType.EngineName = lower
			arrayType.ElementType = new(m.resolveOverrideTypeDepth(sqlmodel.TypeOverride{
				SQLTypeName:     stripped,
				Column:          "",
				GoPackage:       "",
				GoName:          "",
				NonNullElements: nil,
				StructTags:      nil,
			}, depth+1))
			return arrayType
		}
	}

	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, override.SQLTypeName)
}

// mergeSlotLayers folds slot layers into the slots in force, a later layer's non-zero
// slot replacing an earlier one.
//
// Takes layers ([]sqlmodel.TypeSlots) which holds the layers, lowest precedence first.
//
// Returns sqlmodel.TypeSlots which holds the slots in force.
func mergeSlotLayers(layers []sqlmodel.TypeSlots) sqlmodel.TypeSlots {
	var merged sqlmodel.TypeSlots
	for index := range layers {
		layer := layers[index]
		merged.Decimal = overlay(merged.Decimal, layer.Decimal)
		merged.Money = overlay(merged.Money, layer.Money)
		merged.BigInt = overlay(merged.BigInt, layer.BigInt)
		merged.JSON = overlay(merged.JSON, layer.JSON)
		merged.JSONScanInto = overlay(merged.JSONScanInto, layer.JSONScanInto)
		merged.Interval = overlay(merged.Interval, layer.Interval)
		merged.TimeOfDay = overlay(merged.TimeOfDay, layer.TimeOfDay)
		merged.IPAddress = overlay(merged.IPAddress, layer.IPAddress)
		merged.Enum = overlay(merged.Enum, layer.Enum)
		merged.Map = overlay(merged.Map, layer.Map)
		merged.Struct = overlay(merged.Struct, layer.Struct)
	}
	return merged
}

// overlay returns the upper slot when it is set, and the lower one otherwise.
//
// Takes lower (sqlmodel.TypeSlot) which is the slot in force so far.
// Takes upper (sqlmodel.TypeSlot) which is the higher-precedence layer's slot.
//
// Returns sqlmodel.TypeSlot which is the slot in force.
func overlay(lower sqlmodel.TypeSlot, upper sqlmodel.TypeSlot) sqlmodel.TypeSlot {
	if upper.IsZero() {
		return lower
	}
	return upper
}
