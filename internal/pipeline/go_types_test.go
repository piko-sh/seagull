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

package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestFinishedOverridesOfArraysNameTheirElements(t *testing.T) {
	t.Parallel()

	decimal := &sqlmodel.GoType{Package: "github.com/shopspring/decimal", Name: "Decimal"}
	tests := []struct {
		name     string
		override *sqlmodel.GoType
		sqlType  sqlmodel.SQLType
		want     *sqlmodel.GoType
	}{
		{name: "no override", override: nil, sqlType: arrayOf(numeric(), true), want: nil},
		{name: "a scalar keeps its override", override: decimal, sqlType: numeric(), want: decimal},
		{name: "an array of elements that may be NULL holds pointers to the type", override: decimal, sqlType: arrayOf(numeric(), true),
			want: &sqlmodel.GoType{Package: "github.com/shopspring/decimal", Name: "[]*Decimal"}},
		{name: "an array of non-null elements holds the type", override: decimal, sqlType: arrayOf(numeric(), false),
			want: &sqlmodel.GoType{Package: "github.com/shopspring/decimal", Name: "[]Decimal"}},
		{name: "a two-dimensional array holds slices of the type", override: decimal, sqlType: arrayOf(arrayOf(numeric(), false), false),
			want: &sqlmodel.GoType{Package: "github.com/shopspring/decimal", Name: "[][]Decimal"}},
		{name: "a pointer element stays one pointer", override: &sqlmodel.GoType{Name: "*string"}, sqlType: arrayOf(numeric(), true),
			want: &sqlmodel.GoType{Name: "[]*string"}},
		{name: "a slice names the whole array", override: &sqlmodel.GoType{Name: "[]*string"}, sqlType: arrayOf(numeric(), false),
			want: &sqlmodel.GoType{Name: "[]*string"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, finishedOverride(testCase.override, testCase.sqlType, ""))
		})
	}
}

func TestFinishedGoTypesLeaveTheGeneratedPackageUnqualified(t *testing.T) {
	t.Parallel()

	own := "example.com/app/store"
	label := sqlmodel.GoType{Package: own, Name: "Label"}
	other := sqlmodel.GoType{Package: "example.com/app/kinds", Name: "Kind"}
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	catalogue := &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{"main": {Name: "main", Tables: map[string]*sqlmodel.Table{
		"items": {Name: "items", Columns: []sqlmodel.Column{
			{Name: "label", SQLType: text, GoTypeOverride: new(label)},
			{Name: "kind", SQLType: text, GoTypeOverride: new(other)},
		}},
	}}}}
	query := &sqlmodel.AnalysedQuery{
		Parameters:    []sqlmodel.QueryParameter{{Name: "label", SQLType: text, GoTypeOverride: new(label)}},
		OutputColumns: []sqlmodel.OutputColumn{{Name: "label", SQLType: text, GoTypeOverride: new(label)}},
	}
	mappings := &gotypes.MappingTable{
		Mappings: []gotypes.Mapping{{SQLCategory: sqlmodel.TypeCategoryText, NotNull: label, Nullable: sqlmodel.GoType{Package: own, Name: "*Label"}}},
		Slots:    sqlmodel.TypeSlots{JSON: sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Package: own, Name: "Document"}}},
	}
	request := &emit.Request{Catalogue: catalogue, Queries: []*sqlmodel.AnalysedQuery{query}, Types: mappings}

	finishGoTypes(request, mappings, own)

	unqualified := &sqlmodel.GoType{Name: "Label"}
	columns := catalogue.Schemas["main"].Tables["items"].Columns
	require.Equal(t, unqualified, columns[0].GoTypeOverride)
	require.Equal(t, &other, columns[1].GoTypeOverride)
	require.Equal(t, unqualified, query.Parameters[0].GoTypeOverride)
	require.Equal(t, unqualified, query.OutputColumns[0].GoTypeOverride)
	require.Equal(t, sqlmodel.GoType{Name: "Label"}, mappings.Mappings[0].NotNull)
	require.Equal(t, sqlmodel.GoType{Name: "*Label"}, mappings.Mappings[0].Nullable)
	require.Equal(t, sqlmodel.GoType{Name: "Document"}, mappings.Slots.JSON.NotNull)
	require.Equal(t, label, sqlmodel.GoType{Package: own, Name: "Label"})
}

func numeric() sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric")
}

func arrayOf(element sqlmodel.SQLType, nullable bool) sqlmodel.SQLType {
	element.Nullable = nullable
	array := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, element.EngineName+"[]")
	array.ElementType = &element
	return array
}

func nestedGoType(packagePath string, depth int) sqlmodel.GoType {
	goType := sqlmodel.GoType{Package: packagePath, Name: "Leaf"}
	for range depth {
		goType = sqlmodel.GoType{Package: packagePath, Name: "Box", Composite: &sqlmodel.GoComposite{
			Kind:      sqlmodel.GoCompositeInstance,
			Arguments: []sqlmodel.GoType{goType},
		}}
	}
	return goType
}

func innermostGoType(goType sqlmodel.GoType) (sqlmodel.GoType, int) {
	depth := 0
	for goType.Composite != nil && len(goType.Composite.Arguments) > 0 {
		goType = goType.Composite.Arguments[0]
		depth++
	}
	return goType, depth
}

func TestLocalGoTypeStopsAtTheTypeDepthLimit(t *testing.T) {
	t.Parallel()

	own := "example.com/app/store"
	tests := []struct {
		name             string
		depth            int
		wantLeafPackage  string
		wantFirstPackage string
	}{
		{name: "a type within the limit is localised throughout", depth: sqlmodel.MaxTypeDepth - 1, wantLeafPackage: "", wantFirstPackage: ""},
		{name: "a type past the limit keeps the import path of its deepest parts", depth: sqlmodel.MaxTypeDepth, wantLeafPackage: own, wantFirstPackage: ""},
		{name: "a far deeper type is localised only to the limit", depth: 10 * sqlmodel.MaxTypeDepth, wantLeafPackage: own, wantFirstPackage: ""},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			localised := localGoType(nestedGoType(own, testCase.depth), own)
			leaf, depth := innermostGoType(localised)
			assert.Equal(t, testCase.depth, depth)
			assert.Equal(t, testCase.wantLeafPackage, leaf.Package)
			assert.Equal(t, testCase.wantFirstPackage, localised.Package)
		})
	}
}

func TestGoTypeTooDeepReportsAPartAtTheTypeDepthLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		goType *sqlmodel.GoType
		want   bool
	}{
		{name: "no type", goType: nil, want: false},
		{name: "a plain type", goType: &sqlmodel.GoType{Name: "int"}, want: false},
		{name: "a type one level within the limit", goType: new(nestedGoType("", sqlmodel.MaxTypeDepth-1)), want: false},
		{name: "a type reaching the limit", goType: new(nestedGoType("", sqlmodel.MaxTypeDepth)), want: true},
		{name: "a map whose value reaches the limit", goType: &sqlmodel.GoType{Name: "map", Composite: &sqlmodel.GoComposite{
			Kind: sqlmodel.GoCompositeMap, Key: &sqlmodel.GoType{Name: "string"}, Value: new(nestedGoType("", sqlmodel.MaxTypeDepth-1)),
		}}, want: true},
		{name: "a struct whose field reaches the limit", goType: &sqlmodel.GoType{Name: "struct", Composite: &sqlmodel.GoComposite{
			Kind: sqlmodel.GoCompositeStruct, Fields: []sqlmodel.GoField{{Name: "Deep", Type: nestedGoType("", sqlmodel.MaxTypeDepth-1)}},
		}}, want: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, goTypeTooDeep(testCase.goType))
		})
	}
}

func TestLocalGoTypeOfAVeryDeepOverrideFinishesWithinBudget(t *testing.T) {
	t.Parallel()

	deep := nestedGoType("example.com/app/store", 200_000)
	scaling.Within(t, 2*time.Second, func(context.Context) {
		localised := localGoType(deep, "example.com/app/store")
		assert.Empty(t, localised.Package)
		assert.True(t, goTypeTooDeep(&localised))
	})
}

func TestFinishGoTypesPassesOverNilCatalogueEntries(t *testing.T) {
	t.Parallel()

	own := "example.com/app/store"
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	tests := []struct {
		name    string
		schemas map[string]*sqlmodel.Schema
	}{
		{name: "a nil schema", schemas: map[string]*sqlmodel.Schema{"main": nil}},
		{name: "a nil table", schemas: map[string]*sqlmodel.Schema{"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"t": nil}}}},
		{name: "a nil view", schemas: map[string]*sqlmodel.Schema{"main": {Name: "main", Views: map[string]*sqlmodel.View{"v": nil}}}},
		{name: "a nil entry beside a real table", schemas: map[string]*sqlmodel.Schema{"main": {Name: "main", Tables: map[string]*sqlmodel.Table{
			"t":     nil,
			"items": {Name: "items", Columns: []sqlmodel.Column{{Name: "label", SQLType: text, GoTypeOverride: &sqlmodel.GoType{Package: own, Name: "Label"}}}},
		}}}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			request := &emit.Request{Catalogue: &sqlmodel.Catalogue{Schemas: testCase.schemas}}
			require.NotPanics(t, func() { finishGoTypes(request, nil, own) })
			if schema := testCase.schemas["main"]; schema != nil && schema.Tables["items"] != nil {
				assert.Equal(t, &sqlmodel.GoType{Name: "Label"}, schema.Tables["items"].Columns[0].GoTypeOverride)
			}
		})
	}
}
