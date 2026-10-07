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

package catalogue

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

type referenceWalker struct {
	addresses map[uintptr]string
	populated map[string]bool
}

func newReferenceWalker() *referenceWalker {
	return &referenceWalker{addresses: make(map[uintptr]string), populated: make(map[string]bool)}
}

func (w *referenceWalker) walk(value reflect.Value, path string) {
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return
		}
		w.addresses[value.Pointer()] = path
		w.walk(value.Elem(), path+".*")
	case reflect.Map:
		if value.IsNil() {
			return
		}
		w.addresses[value.Pointer()] = path
		iterator := value.MapRange()
		for iterator.Next() {
			w.walk(iterator.Value(), path+"["+iterator.Key().String()+"]")
		}
	case reflect.Slice:
		if value.IsNil() || value.Len() == 0 {
			return
		}
		w.addresses[value.Pointer()] = path
		for index := range value.Len() {
			w.walk(value.Index(index), path+"[]")
		}
	case reflect.Struct:
		typeName := value.Type().String()
		for field, fieldValue := range value.Fields() {
			key := typeName + "." + field.Name
			switch fieldValue.Kind() {
			case reflect.Pointer, reflect.Map, reflect.Slice:
				if _, seen := w.populated[key]; !seen {
					w.populated[key] = false
				}
				if !fieldValue.IsNil() {
					w.populated[key] = true
				}
			default:
			}
			w.walk(fieldValue, path+"."+field.Name)
		}
	default:
	}
}

func fullyPopulatedType() sqlmodel.SQLType {
	element := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "map")
	sqlType.Precision = new(10)
	sqlType.Scale = new(2)
	sqlType.Length = new(5)
	sqlType.ElementType = &element
	sqlType.KeyType = new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"))
	sqlType.EnumValues = []string{"a"}
	sqlType.StructFields = []sqlmodel.StructField{{Name: "f", SQLType: element, Positional: false}}
	sqlType.UnionMembers = []sqlmodel.UnionMember{{Tag: "t", SQLType: element}}
	return sqlType
}

func fullyPopulatedColumn() sqlmodel.Column {
	column := sqlmodel.NewColumn("id", fullyPopulatedType(), true)
	keyType := sqlmodel.GoType{Composite: nil, Package: "", Name: "string"}
	valueType := sqlmodel.GoType{Composite: nil, Package: "", Name: "int"}
	column.GoTypeOverride = &sqlmodel.GoType{
		Composite: &sqlmodel.GoComposite{
			Key:       &keyType,
			Value:     &valueType,
			Fields:    []sqlmodel.GoField{{Name: "F", Tag: "", Type: valueType}},
			Arguments: []sqlmodel.GoType{keyType},
			Kind:      sqlmodel.GoCompositeMap,
		},
		Package: "",
		Name:    "",
	}
	column.NullableOverride = new(false)
	column.NonNullElements = new(true)
	column.StructTags = map[string]string{"validate": "required"}
	return column
}

func fullyPopulatedCatalogue() *sqlmodel.Catalogue {
	columns := []sqlmodel.Column{fullyPopulatedColumn()}
	function := sqlmodel.NewFunction(
		[]sqlmodel.FunctionArgument{{Name: "a", Type: fullyPopulatedType(), IsOptional: false}},
		fullyPopulatedType(),
		sqlmodel.FunctionNullableCalledOnNull,
		sqlmodel.WithFunctionName("f"),
	)
	schema := newEmptySchema("public")
	schema.Tables["t"] = &sqlmodel.Table{
		Name:              "t",
		Columns:           columns,
		PrimaryKey:        []string{"id"},
		Indexes:           []sqlmodel.Index{{Name: "i", Columns: []string{"id"}}},
		Constraints:       []sqlmodel.Constraint{{Name: "c", Columns: []string{"id"}, ForeignColumns: []string{"id"}}},
		PartitionOf:       &sqlmodel.TableName{Schema: "public", Name: "parent"},
		Schema:            "public",
		Comment:           "",
		VirtualModuleName: "",
		Origin:            sqlmodel.MigrationOrigin{},
		IsVirtual:         false,
		IsWithoutRowID:    false,
	}
	schema.Views["v"] = &sqlmodel.View{Name: "v", Columns: []sqlmodel.Column{fullyPopulatedColumn()}}
	schema.Enums["e"] = &sqlmodel.Enum{Name: "e", Values: []string{"x"}}
	schema.CompositeTypes["c"] = &sqlmodel.CompositeType{Name: "c", Fields: []sqlmodel.Column{fullyPopulatedColumn()}}
	schema.Sequences["s"] = &sqlmodel.Sequence{Name: "s", Columns: []sqlmodel.Column{fullyPopulatedColumn()}}
	schema.Domains["d"] = &sqlmodel.Domain{Name: "d", BaseType: fullyPopulatedType()}
	schema.Functions["f"] = []*sqlmodel.Function{function}
	return &sqlmodel.Catalogue{
		Schemas:       map[string]*sqlmodel.Schema{"public": schema},
		Extensions:    map[string]struct{}{"x": {}},
		DefaultSchema: "public",
		SearchPath:    []string{"public"},
		Identifiers:   sqlmodel.IdentifierCase{},
	}
}

func TestCloneSharesNoMutableMemory(t *testing.T) {
	t.Parallel()

	original := fullyPopulatedCatalogue()
	copied := Clone(original)
	require.Equal(t, original, copied)

	originalReferences := newReferenceWalker()
	originalReferences.walk(reflect.ValueOf(original), "catalogue")
	for key, populated := range originalReferences.populated {
		require.True(t, populated, "the fixture must populate %s so the clone of it is checked", key)
	}

	copiedReferences := newReferenceWalker()
	copiedReferences.walk(reflect.ValueOf(copied), "catalogue")
	for address, path := range copiedReferences.addresses {
		originalPath, shared := originalReferences.addresses[address]
		require.False(t, shared, "the clone's %s shares memory with the original's %s", path, originalPath)
	}
}

func TestCloneOfNilIsNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, Clone(nil))
}

func TestCloneCopiesNestedTypesDownToTheDepthLimit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		depth int
	}{
		{name: "a type within the limit", depth: sqlmodel.MaxTypeDepth - 1},
		{name: "a type at the limit", depth: sqlmodel.MaxTypeDepth},
		{name: "a type far past the limit", depth: 100_000},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			original := sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "integer")
			goType := sqlmodel.GoType{Composite: nil, Package: "", Name: "int64"}
			for range testCase.depth {
				original = sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "integer[]", ElementType: new(original)}
				goType = sqlmodel.GoType{Composite: &sqlmodel.GoComposite{Kind: sqlmodel.GoCompositeMap, Value: new(goType)}, Package: "", Name: ""}
			}

			var copied sqlmodel.SQLType
			var copiedGo *sqlmodel.GoType
			scaling.Within(t, 100*time.Millisecond, func(context.Context) {
				copied = cloneSQLType(original)
				copiedGo = cloneGoType(&goType)
			})

			copiedLevel, originalLevel := &copied, &original
			copiedGoLevel, originalGoLevel := copiedGo, &goType
			for level := range min(testCase.depth, sqlmodel.MaxTypeDepth) {
				require.NotSame(t, originalLevel.ElementType, copiedLevel.ElementType, "level %d", level)
				require.NotSame(t, originalGoLevel.Composite, copiedGoLevel.Composite, "level %d", level)
				copiedLevel, originalLevel = copiedLevel.ElementType, originalLevel.ElementType
				copiedGoLevel, originalGoLevel = copiedGoLevel.Composite.Value, originalGoLevel.Composite.Value
			}
			require.Equal(t, sqlmodel.TypeTooDeep(&original), sqlmodel.TypeTooDeep(&copied))
		})
	}
}
