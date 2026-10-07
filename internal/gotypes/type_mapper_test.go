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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestNewTypeMapper(t *testing.T) {
	t.Parallel()

	t.Run("creates non-nil mapper with catalogue", func(t *testing.T) {
		t.Parallel()

		catalogue := &ast.TypeCatalogue{
			Types: map[string]sqlmodel.SQLType{
				"int4": sqlmodel.NewIntegerType("int4", 32, false),
			},
		}

		mapper := NewTypeMapper(catalogue)

		require.NotNil(t, mapper)
		assert.Equal(t, catalogue, mapper.typeCatalogue)
	})

	t.Run("creates non-nil mapper with nil catalogue", func(t *testing.T) {
		t.Parallel()

		mapper := NewTypeMapper(nil)

		require.NotNil(t, mapper)
		assert.Nil(t, mapper.typeCatalogue)
	})
}

func TestBuildMappingTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		catalogue        *ast.TypeCatalogue
		overrides        []sqlmodel.TypeOverride
		wantMappings     int
		wantLastSQLName  string
		wantLastNotNull  sqlmodel.GoType
		wantLastNullable sqlmodel.GoType
		wantLastCategory sqlmodel.SQLTypeCategory
	}{
		{
			name:      "no overrides returns default mappings",
			catalogue: nil,
			overrides: nil,

			wantMappings: 0,
		},
		{
			name:      "with override appends user mapping to table",
			catalogue: nil,
			overrides: []sqlmodel.TypeOverride{
				{
					SQLTypeName: "custom_money",
					GoPackage:   "example.com/types",
					GoName:      "Money",
				},
			},
			wantMappings:     1,
			wantLastSQLName:  "custom_money",
			wantLastNotNull:  sqlmodel.GoType{Package: "example.com/types", Name: "Money"},
			wantLastNullable: sqlmodel.GoType{Package: "example.com/types", Name: "*Money"},
			wantLastCategory: sqlmodel.TypeCategoryUnknown,
		},
		{
			name: "override resolves type from catalogue when name is known",
			catalogue: &ast.TypeCatalogue{
				Types: map[string]sqlmodel.SQLType{
					"citext": {
						Category:   sqlmodel.TypeCategoryText,
						EngineName: "citext",
					},
				},
			},
			overrides: []sqlmodel.TypeOverride{
				{
					SQLTypeName: "citext",
					GoPackage:   "example.com/ci",
					GoName:      "CIString",
				},
			},
			wantMappings:     1,
			wantLastSQLName:  "citext",
			wantLastNotNull:  sqlmodel.GoType{Package: "example.com/ci", Name: "CIString"},
			wantLastNullable: sqlmodel.GoType{Package: "example.com/ci", Name: "*CIString"},
			wantLastCategory: sqlmodel.TypeCategoryText,
		},
		{
			name: "override with unknown catalogue type falls back to TypeCategoryUnknown",
			catalogue: &ast.TypeCatalogue{
				Types: map[string]sqlmodel.SQLType{
					"int4": sqlmodel.NewIntegerType("int4", 32, false),
				},
			},
			overrides: []sqlmodel.TypeOverride{
				{
					SQLTypeName: "not_in_catalogue",
					GoPackage:   "example.com/custom",
					GoName:      "Widget",
				},
			},
			wantMappings:     1,
			wantLastSQLName:  "not_in_catalogue",
			wantLastNotNull:  sqlmodel.GoType{Package: "example.com/custom", Name: "Widget"},
			wantLastNullable: sqlmodel.GoType{Package: "example.com/custom", Name: "*Widget"},
			wantLastCategory: sqlmodel.TypeCategoryUnknown,
		},
		{
			name:      "multiple overrides are all appended in order",
			catalogue: nil,
			overrides: []sqlmodel.TypeOverride{
				{SQLTypeName: "first_type", GoPackage: "example.com/a", GoName: "First"},
				{SQLTypeName: "second_type", GoPackage: "example.com/b", GoName: "Second"},
			},
			wantMappings:     2,
			wantLastSQLName:  "second_type",
			wantLastNotNull:  sqlmodel.GoType{Package: "example.com/b", Name: "Second"},
			wantLastNullable: sqlmodel.GoType{Package: "example.com/b", Name: "*Second"},
			wantLastCategory: sqlmodel.TypeCategoryUnknown,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mapper := NewTypeMapper(testCase.catalogue)
			table := mapper.BuildMappingTable(testCase.overrides)

			require.NotNil(t, table)
			assert.Len(t, table.Mappings, testCase.wantMappings)

			if testCase.wantLastSQLName != "" {
				last := table.Mappings[len(table.Mappings)-1]
				assert.Equal(t, testCase.wantLastSQLName, last.SQLName)
				assert.Equal(t, testCase.wantLastNotNull, last.NotNull)
				assert.Equal(t, testCase.wantLastNullable, last.Nullable)
				assert.Equal(t, testCase.wantLastCategory, last.SQLCategory)
			}
		})
	}
}

func slotOf(packagePath, name string) sqlmodel.TypeSlot {
	return sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Package: packagePath, Name: name}, Nullable: sqlmodel.GoType{}}
}

func TestSlotLayersRankInOrder(t *testing.T) {
	t.Parallel()

	monetary := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "money")
	monetary.Monetary = true
	emitterSlots := sqlmodel.TypeSlots{
		Decimal:   slotOf("example.com/emitter", "Decimal"),
		BigInt:    slotOf("example.com/emitter", "Wide"),
		Interval:  slotOf("time", "Duration"),
		TimeOfDay: sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Name: "string"}, Nullable: sqlmodel.GoType{Name: "*string"}},
	}
	presetSlots := sqlmodel.TypeSlots{
		Decimal: slotOf("example.com/preset", "Decimal"),
		JSON:    sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Package: "example.com/preset", Name: "Doc"}, Nullable: sqlmodel.GoType{Package: "example.com/preset", Name: "Doc"}},
	}
	table := NewTypeMapper(nil).BuildMappingTable(nil, emitterSlots, presetSlots)

	assert.Equal(t, sqlmodel.GoType{Package: "example.com/preset", Name: "*Decimal"}, table.Resolve(sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"), true))
	assert.Equal(t, sqlmodel.GoType{Name: "string"}, table.Resolve(monetary, false),
		"a zero money slot keeps money as text whatever the decimal slots say")
	assert.Equal(t, sqlmodel.GoType{Package: "example.com/emitter", Name: "Wide"}, table.Resolve(sqlmodel.NewIntegerType("Int128", 128, false), false),
		"a zero preset slot leaves the emitter layer in force")
	assert.Equal(t, sqlmodel.GoType{Package: "time", Name: "*Duration"}, table.Resolve(sqlmodel.NewTemporalType("interval", sqlmodel.TemporalKindInterval, false), true))
	assert.Equal(t, sqlmodel.GoType{Name: "*string"}, table.Resolve(sqlmodel.NewTemporalType("time", sqlmodel.TemporalKindTime, false), true),
		"a time of day follows the TimeOfDay slot")
	require.Equal(t, sqlmodel.GoType{Name: "string"}, table.Resolve(sqlmodel.NewTemporalType("timetz", sqlmodel.TemporalKindTime, true), false),
		"a zoned time of day follows the TimeOfDay slot when a layer sets it")
	zoned := NewTypeMapper(nil).BuildMappingTable(nil, sqlmodel.TypeSlots{TimeOfDay: slotOf("time", "Time")})
	require.Equal(t, sqlmodel.GoType{Package: "time", Name: "*Time"}, zoned.Resolve(sqlmodel.NewTemporalType("timetz", sqlmodel.TemporalKindTime, true), true),
		"a TimeOfDay slot of time.Time holds a zoned time of day too")
	assert.Equal(t, sqlmodel.GoType{Package: "time", Name: "Time"}, table.Resolve(sqlmodel.NewTemporalType("date", sqlmodel.TemporalKindDate, false), false),
		"the TimeOfDay slot leaves dates alone")
	assert.Equal(t, sqlmodel.GoType{Package: "example.com/preset", Name: "Doc"}, table.Resolve(sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"), true))
	assert.Equal(t, sqlmodel.GoType{Name: "int64"}, table.Resolve(sqlmodel.NewIntegerType("int8", 64, false), false),
		"slots leave the fixed-width integers alone")
}

func TestMoneySlotOverridesDecimalForMoneyOnly(t *testing.T) {
	t.Parallel()

	monetary := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "money")
	monetary.Monetary = true
	table := NewTypeMapper(nil).BuildMappingTable(nil, sqlmodel.TypeSlots{Money: slotOf("example.com/cash", "Amount")})

	assert.Equal(t, sqlmodel.GoType{Package: "example.com/cash", Name: "Amount"}, table.Resolve(monetary, false))
	assert.Equal(t, sqlmodel.GoType{Package: goPackageDecimal, Name: "Decimal"}, table.Resolve(sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"), false))
}

func TestCompositeSlotsReplaceTheStructuralMapping(t *testing.T) {
	t.Parallel()

	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "String")
	mapType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, "Map")
	mapType.KeyType = &text
	mapType.ElementType = &text
	tuple := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "Tuple")
	tuple.StructFields = []sqlmodel.StructField{{Name: "a", SQLType: text}}
	slots := sqlmodel.TypeSlots{
		Map:       slotOf("example.com/kv", "Pairs"),
		Struct:    slotOf("example.com/kv", "Record"),
		IPAddress: slotOf("net", "IP"),
	}
	table := NewTypeMapper(nil).BuildMappingTable(nil, slots)

	assert.Equal(t, sqlmodel.GoType{Package: "example.com/kv", Name: "Pairs"}, table.Resolve(mapType, false))
	assert.Equal(t, sqlmodel.GoType{Package: "example.com/kv", Name: "*Record"}, table.Resolve(tuple, true))
	positional := tuple
	positional.StructFields = []sqlmodel.StructField{{Name: "_1", SQLType: text, Positional: true}}
	assert.Equal(t, sqlmodel.GoType{Name: "[]any"}, table.Resolve(positional, false), "the Struct slot maps only structs of named fields")
	assert.Equal(t, sqlmodel.GoType{Package: "net", Name: "IP"}, table.Resolve(sqlmodel.NewNetworkType("IPv6", sqlmodel.NetworkKindAddress), false))
}

func TestTypeOverridesOutrankEverySlotLayer(t *testing.T) {
	t.Parallel()

	presetSlots := sqlmodel.TypeSlots{Decimal: slotOf("example.com/preset", "Decimal")}
	overrides := []sqlmodel.TypeOverride{{SQLTypeName: "numeric", GoPackage: "", GoName: "string"}}
	catalogue := &ast.TypeCatalogue{Types: map[string]sqlmodel.SQLType{
		"numeric": sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"),
	}}
	table := NewTypeMapper(catalogue).BuildMappingTable(overrides, presetSlots)

	assert.Equal(t, sqlmodel.GoType{Name: "string"}, table.Resolve(sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"), false))
	assert.Equal(t, sqlmodel.GoType{Package: "example.com/preset", Name: "Decimal"}, table.Resolve(sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "decimal"), false))
}

func TestAnOverrideNarrowsOneEngineIntegerName(t *testing.T) {
	t.Parallel()

	catalogue := &ast.TypeCatalogue{Types: map[string]sqlmodel.SQLType{
		"integer": sqlmodel.NewIntegerType("integer", 64, false),
		"bigint":  sqlmodel.NewIntegerType("bigint", 64, false),
	}}
	overrides := []sqlmodel.TypeOverride{{SQLTypeName: "INTEGER", GoPackage: "", GoName: "int32"}}
	table := NewTypeMapper(catalogue).BuildMappingTable(overrides)

	assert.Equal(t, sqlmodel.GoType{Name: "int32"}, table.Resolve(sqlmodel.NewIntegerType("integer", 64, false), false))
	assert.Equal(t, sqlmodel.GoType{Name: "*int32"}, table.Resolve(sqlmodel.NewIntegerType("integer", 64, false), true))
	assert.Equal(t, sqlmodel.GoType{Name: "int64"}, table.Resolve(sqlmodel.NewIntegerType("bigint", 64, false), false))
}

func TestResolveOverrideType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		catalogue *ast.TypeCatalogue
		override  sqlmodel.TypeOverride
		want      sqlmodel.SQLType
	}{
		{
			name: "resolves known type from catalogue",
			catalogue: &ast.TypeCatalogue{
				Types: map[string]sqlmodel.SQLType{
					"citext": {Category: sqlmodel.TypeCategoryText, EngineName: "citext"},
				},
			},
			override: sqlmodel.TypeOverride{SQLTypeName: "citext", GoPackage: "x", GoName: "X"},
			want:     sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "citext"},
		},
		{
			name: "case-folds to lower when looking up catalogue",
			catalogue: &ast.TypeCatalogue{
				Types: map[string]sqlmodel.SQLType{
					"citext": {Category: sqlmodel.TypeCategoryText, EngineName: "citext"},
				},
			},
			override: sqlmodel.TypeOverride{SQLTypeName: "CITEXT", GoPackage: "x", GoName: "X"},
			want:     sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "citext"},
		},
		{
			name: "unknown type name falls back to unknown category",
			catalogue: &ast.TypeCatalogue{
				Types: map[string]sqlmodel.SQLType{},
			},
			override: sqlmodel.TypeOverride{SQLTypeName: "mystery", GoPackage: "x", GoName: "X"},
			want:     sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: "mystery"},
		},
		{
			name:      "nil catalogue falls back to unknown category",
			catalogue: nil,
			override:  sqlmodel.TypeOverride{SQLTypeName: "anything", GoPackage: "x", GoName: "X"},
			want:      sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: "anything"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mapper := NewTypeMapper(testCase.catalogue)
			got := mapper.resolveOverrideType(testCase.override)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestResolveOverrideType_CapsArrayDepth(t *testing.T) {
	t.Parallel()

	mapper := NewTypeMapper(&ast.TypeCatalogue{
		Types: map[string]sqlmodel.SQLType{},
	})

	override := sqlmodel.TypeOverride{SQLTypeName: "text[][][][][][][][]"}

	got := mapper.resolveOverrideType(override)

	wraps := 0
	current := got
	for current.Category == sqlmodel.TypeCategoryArray && current.ElementType != nil {
		wraps++
		current = *current.ElementType
	}

	assert.LessOrEqual(t, wraps, sqlmodel.MaxArrayDimensions, "array wraps must not exceed cap")
}

func TestAnOverrideMapsAnEnumTheMigrationsCreate(t *testing.T) {
	t.Parallel()

	schema := &sqlmodel.Catalogue{
		DefaultSchema: "public",
		SearchPath:    []string{"public"},
		Schemas: map[string]*sqlmodel.Schema{
			"public": {Name: "public", Enums: map[string]*sqlmodel.Enum{"mood": {Name: "mood", Schema: "public"}}},
			"audit":  {Name: "audit", Enums: map[string]*sqlmodel.Enum{"mood": {Name: "mood", Schema: "audit"}}},
		},
	}
	enumIn := func(schemaName string) sqlmodel.SQLType {
		enum := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood")
		enum.Schema = schemaName
		return enum
	}
	mood := sqlmodel.GoType{Package: "example.com/app", Name: "Mood"}

	unqualified := NewTypeMapper(nil).WithSchema(schema).BuildMappingTable([]sqlmodel.TypeOverride{{SQLTypeName: "mood", GoPackage: "example.com/app", GoName: "Mood"}})
	assert.Equal(t, mood, unqualified.Resolve(enumIn("public"), false))
	assert.Equal(t, sqlmodel.GoType{Package: "example.com/app", Name: "*Mood"}, unqualified.Resolve(enumIn("public"), true))
	assert.Equal(t, sqlmodel.GoType{Name: "string"}, unqualified.Resolve(enumIn("audit"), false))

	qualified := NewTypeMapper(nil).WithSchema(schema).BuildMappingTable([]sqlmodel.TypeOverride{{SQLTypeName: "audit.mood", GoPackage: "example.com/app", GoName: "Mood"}})
	assert.Equal(t, mood, qualified.Resolve(enumIn("audit"), false))
	assert.Equal(t, sqlmodel.GoType{Name: "string"}, qualified.Resolve(enumIn("public"), false))

	withoutSchema := NewTypeMapper(nil).BuildMappingTable([]sqlmodel.TypeOverride{{SQLTypeName: "mood", GoPackage: "example.com/app", GoName: "Mood"}})
	assert.Equal(t, sqlmodel.GoType{Name: "string"}, withoutSchema.Resolve(enumIn("public"), false))
}

func TestEnumSlotLayersRankInOrder(t *testing.T) {
	t.Parallel()

	enum := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood")
	emitterSlots := sqlmodel.TypeSlots{Enum: slotOf("example.com/emitter", "Label")}
	presetSlots := sqlmodel.TypeSlots{Enum: sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Name: "string"}}}

	tests := []struct {
		name   string
		layers []sqlmodel.TypeSlots
		want   sqlmodel.GoType
	}{
		{name: "no layer sets the slot", layers: nil, want: sqlmodel.GoType{Name: "*string"}},
		{name: "the emitter layer sets it", layers: []sqlmodel.TypeSlots{emitterSlots}, want: sqlmodel.GoType{Package: "example.com/emitter", Name: "*Label"}},
		{name: "the preset layer outranks the emitter layer", layers: []sqlmodel.TypeSlots{emitterSlots, presetSlots}, want: sqlmodel.GoType{Name: "*string"}},
		{name: "a zero preset slot keeps the emitter layer", layers: []sqlmodel.TypeSlots{emitterSlots, {}}, want: sqlmodel.GoType{Package: "example.com/emitter", Name: "*Label"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			table := NewTypeMapper(nil).BuildMappingTable(nil, testCase.layers...)
			require.Equal(t, testCase.want, table.Resolve(enum, true))
		})
	}
}
