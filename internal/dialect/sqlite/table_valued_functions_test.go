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

package sqlite

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func fullTextTable(module string, columns ...sqlmodel.Column) *sqlmodel.Table {
	return &sqlmodel.Table{Name: "docs", VirtualModuleName: module, IsVirtual: module != "", Columns: columns}
}

func TestTableValuedFunctionColumnsFromCatalogueReadsAFullTextTable(t *testing.T) {
	t.Parallel()

	title := sqlmodel.Column{Name: "title", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: true}
	body := sqlmodel.Column{Name: "body", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: false}
	hidden := sqlmodel.Column{Name: "rank", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "real"), Hidden: true}
	testCases := []struct {
		catalogue *sqlmodel.Catalogue
		name      string
		function  string
		want      []string
	}{
		{name: "an FTS5 table gives its columns", function: "docs", want: []string{"title", "body"},
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
				"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"docs": fullTextTable("fts5", title, body)}},
			}}},
		{name: "the table and module names are matched ignoring case", function: "DOCS", want: []string{"title"},
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
				"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"Docs": fullTextTable("FTS5", title)}},
			}}},
		{name: "hidden columns are left out", function: "docs", want: []string{"body"},
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
				"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"docs": fullTextTable("fts5", hidden, body)}},
			}}},
		{name: "the first schema by name decides", function: "docs", want: []string{"title"},
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
				"temp": {Name: "temp", Tables: map[string]*sqlmodel.Table{"docs": fullTextTable("fts5", body)}},
				"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"docs": fullTextTable("fts5", title)}},
			}}},
		{name: "a table of another module gives nothing", function: "docs", want: nil,
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
				"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"docs": fullTextTable("rtree", title)}},
			}}},
		{name: "an ordinary table gives nothing", function: "docs", want: nil,
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
				"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"docs": fullTextTable("", title)}},
			}}},
		{name: "another name gives nothing", function: "notes", want: nil,
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
				"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"docs": fullTextTable("fts5", title)}},
			}}},
		{name: "missing schemas and tables are passed over", function: "docs", want: []string{"title"},
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
				"aux":  nil,
				"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"blank": nil, "docs": fullTextTable("fts5", title)}},
			}}},
		{name: "no catalogue gives nothing", function: "docs", want: nil, catalogue: nil},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns, err := New().TableValuedFunctionColumnsFromCatalogue(t.Context(), testCase.catalogue, testCase.function)

			require.NoError(t, err)
			var names []string
			for _, column := range columns {
				names = append(names, column.Name)
			}
			assert.Equal(t, testCase.want, names)
		})
	}
}

func TestTableValuedFunctionColumnsFromCatalogueKeepsEachColumnsTypeAndNullability(t *testing.T) {
	t.Parallel()

	body := sqlmodel.Column{Name: "body", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), Nullable: false}
	catalogue := &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
		"main": {Name: "main", Tables: map[string]*sqlmodel.Table{"docs": fullTextTable("fts5", body)}},
	}}

	columns, err := New().TableValuedFunctionColumnsFromCatalogue(t.Context(), catalogue, "docs")

	require.NoError(t, err)
	require.Len(t, columns, 1)
	assert.Equal(t, body.SQLType, columns[0].SQLType)
	assert.False(t, columns[0].Nullable)
}
