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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func deeplyNestedArray() sqlmodel.SQLType {
	sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	for range 100 {
		element := sqlType
		sqlType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "array")
		sqlType.ElementType = &element
	}
	return sqlType
}

func TestDepthLimitDiagnostics(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["deep"] = &sqlmodel.Table{
		Name:    "deep",
		Columns: []sqlmodel.Column{sqlmodel.NewColumn("shallow", sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), false), sqlmodel.NewColumn("nested", deeplyNestedArray(), true)},
		Origin:  sqlmodel.MigrationOrigin{Filename: "0001_init.up.sql"},
	}
	query := &sqlmodel.AnalysedQuery{
		Name:          "Deep",
		Filename:      "deep.sql",
		Line:          3,
		OutputColumns: []sqlmodel.OutputColumn{{Name: "nested", SQLType: deeplyNestedArray()}},
		Parameters:    []sqlmodel.QueryParameter{{Name: "filter", SQLType: deeplyNestedArray()}},
	}
	overrides := []sqlmodel.TypeOverride{
		{SQLTypeName: "text" + strings.Repeat("[]", sqlmodel.MaxArrayDimensions+1), GoName: "Deep"},
		{SQLTypeName: "text[]", GoName: "Shallow"},
	}

	diagnostics := depthLimitDiagnostics(catalogue, []*sqlmodel.AnalysedQuery{query}, overrides)
	require.Len(t, diagnostics, 4)
	for _, diagnostic := range diagnostics {
		assert.Equal(t, diag.CodeDepthLimit, diagnostic.Code)
		assert.Equal(t, diag.SeverityWarning, diagnostic.Severity)
	}
	assert.Contains(t, diagnostics[1].Message, "deep.nested")
	assert.Equal(t, "0001_init.up.sql", diagnostics[1].Filename)
	assert.Equal(t, "deep.sql", diagnostics[2].Filename)
	assert.Equal(t, 3, diagnostics[2].Line)
	assert.Contains(t, diagnostics[3].Message, "filter")
}

func TestDepthLimitDiagnosticsWarnAboutGoTypeOverridesNestedTooDeeply(t *testing.T) {
	t.Parallel()

	deep := nestedGoType("example.com/app/kinds", sqlmodel.MaxTypeDepth)
	shallow := nestedGoType("example.com/app/kinds", 2)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["items"] = &sqlmodel.Table{
		Name: "items",
		Columns: []sqlmodel.Column{
			{Name: "deep", SQLType: text, GoTypeOverride: new(deep)},
			{Name: "shallow", SQLType: text, GoTypeOverride: new(shallow)},
		},
		Origin: sqlmodel.MigrationOrigin{Filename: "0001_init.up.sql"},
	}
	catalogue.Schemas["public"].Views["item_view"] = &sqlmodel.View{
		Name:    "item_view",
		Columns: []sqlmodel.Column{{Name: "deep", SQLType: text, GoTypeOverride: new(deep)}},
		Origin:  sqlmodel.MigrationOrigin{Filename: "0002_view.up.sql"},
	}
	query := &sqlmodel.AnalysedQuery{
		Name:          "Items",
		Filename:      "items.sql",
		Line:          2,
		OutputColumns: []sqlmodel.OutputColumn{{Name: "deep", SQLType: text, GoTypeOverride: new(deep)}},
		Parameters:    []sqlmodel.QueryParameter{{Name: "filter", SQLType: text, GoTypeOverride: new(deep)}, {Name: "plain", SQLType: text}},
	}

	diagnostics := depthLimitDiagnostics(catalogue, []*sqlmodel.AnalysedQuery{query}, nil)
	tests := []struct {
		filename string
		message  string
	}{
		{filename: "0001_init.up.sql", message: "column items.deep has a Go type override nested more deeply than the generated code follows"},
		{filename: "0002_view.up.sql", message: "column item_view.deep has a Go type override nested more deeply than the generated code follows"},
		{filename: "items.sql", message: `output column "deep" of query "Items" has a Go type override nested more deeply than the generated code follows`},
		{filename: "items.sql", message: `parameter "filter" of query "Items" has a Go type override nested more deeply than the generated code follows`},
	}
	require.Len(t, diagnostics, len(tests))
	for index, testCase := range tests {
		assert.Equal(t, diag.CodeDepthLimit, diagnostics[index].Code)
		assert.Equal(t, diag.SeverityWarning, diagnostics[index].Severity)
		assert.Equal(t, testCase.filename, diagnostics[index].Filename)
		assert.Equal(t, testCase.message, diagnostics[index].Message)
	}
}

func TestTableDepthDiagnosticsPassOverNilCatalogueEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		schemas map[string]*sqlmodel.Schema
	}{
		{name: "a nil schema", schemas: map[string]*sqlmodel.Schema{"main": nil}},
		{name: "a nil table", schemas: map[string]*sqlmodel.Schema{"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"t": nil}}}},
		{name: "a nil view", schemas: map[string]*sqlmodel.Schema{"main": {Name: "main", Views: map[string]*sqlmodel.View{"v": nil}}}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var diagnostics []diag.SourceError
			require.NotPanics(t, func() { diagnostics = tableDepthDiagnostics(&sqlmodel.Catalogue{Schemas: testCase.schemas}) })
			assert.Empty(t, diagnostics)
		})
	}
}
