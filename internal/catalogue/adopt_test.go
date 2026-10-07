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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestAdoptKeysAnExternalCatalogueByTheDialectRules(t *testing.T) {
	t.Parallel()

	catalogue := &sqlmodel.Catalogue{
		Schemas: map[string]*sqlmodel.Schema{
			"Main": {
				Name:   "Main",
				Tables: map[string]*sqlmodel.Table{"Authors": {Name: "Authors"}},
				Views:  map[string]*sqlmodel.View{"x": {Name: "Recent"}},
				Enums:  map[string]*sqlmodel.Enum{"Mood": nil},
			},
			"empty": nil,
		},
	}
	info := dialect.Info{
		SchemaSearchPath: []string{"Main", "temp"},
		Identifiers:      sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
	}

	Adopt(catalogue, &info)

	require.Equal(t, info.Identifiers, catalogue.Identifiers)
	require.Equal(t, []string{"Main", "temp"}, catalogue.SearchPath)
	require.Equal(t, "Main", catalogue.DefaultSchema)
	require.Contains(t, catalogue.Schemas, "main")
	require.Contains(t, catalogue.Schemas, "empty")
	require.NotNil(t, catalogue.Schemas["main"].Tables["authors"])
	require.NotNil(t, catalogue.Schemas["main"].Views["recent"])
	require.Contains(t, catalogue.Schemas["main"].Enums, "mood")
	require.Nil(t, catalogue.Schemas["main"].CompositeTypes)
	require.NotNil(t, catalogue.LookupTable("", "AUTHORS"))
}

func TestAdoptKeepsAConfiguredSearchPathAndFoldsTheDialectOne(t *testing.T) {
	t.Parallel()

	kept := &sqlmodel.Catalogue{DefaultSchema: "public", SearchPath: []string{"tenant", "public"}}
	folded := &sqlmodel.Catalogue{}
	info := dialect.Info{
		SchemaSearchPath: []string{"dbo"},
		Identifiers:      sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingUpper, Insensitive: false},
	}

	Adopt(kept, &info)
	Adopt(folded, &info)
	Adopt(nil, &info)

	require.Equal(t, []string{"tenant", "public"}, kept.SearchPath)
	require.Equal(t, "public", kept.DefaultSchema)
	require.Equal(t, []string{"DBO"}, folded.SearchPath)
	require.Equal(t, "DBO", folded.DefaultSchema)
}

func TestLeaveOutTablesRemovesTheNamedTablesFromEverySchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		names    []string
		expected map[string][]string
	}{
		{
			name:     "names match without regard to case, in every schema",
			names:    []string{"sgl_migrations", "SGL_SEEDS"},
			expected: map[string][]string{"main": {"authors"}, "audit": {"events"}},
		},
		{
			name:     "no names leave every table",
			names:    nil,
			expected: map[string][]string{"main": {"SGL_Migrations", "authors", "sgl_seeds"}, "audit": {"events", "sgl_migrations"}},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
				"main": {Name: "main", Tables: map[string]*sqlmodel.Table{
					"sgl_migrations": {Name: "SGL_Migrations"}, "sgl_seeds": nil, "authors": {Name: "authors"},
				}},
				"audit": {Name: "audit", Tables: map[string]*sqlmodel.Table{
					"events": {Name: "events"}, "sgl_migrations": {Name: "sgl_migrations"},
				}},
				"empty": nil,
			}}

			LeaveOutTables(catalogue, testCase.names)
			LeaveOutTables(nil, testCase.names)

			for schemaName, tables := range testCase.expected {
				var names []string
				for key, table := range catalogue.Schemas[schemaName].Tables {
					if table == nil {
						names = append(names, key)
						continue
					}
					names = append(names, table.Name)
				}
				require.ElementsMatch(t, tables, names, schemaName)
			}
		})
	}
}

func TestAdoptKeysFunctionsByTheDialectRules(t *testing.T) {
	t.Parallel()

	quoted := &sqlmodel.Function{Name: "Foo"}
	unquoted := &sqlmodel.Function{Name: "foo"}
	mixed := &sqlmodel.Function{Name: "MyFunc"}
	shouted := &sqlmodel.Function{Name: "MYFUNC"}
	unnamed := &sqlmodel.Function{Name: ""}

	testCases := []struct {
		functions   map[string][]*sqlmodel.Function
		want        map[string][]*sqlmodel.Function
		name        string
		identifiers sqlmodel.IdentifierCase
	}{
		{
			name:        "a quoted and an unquoted function filed together are parted",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false},
			functions:   map[string][]*sqlmodel.Function{"foo": {quoted, unquoted}},
			want:        map[string][]*sqlmodel.Function{"Foo": {quoted}, "foo": {unquoted}},
		},
		{
			name:        "a name kept as written is filed in its own case",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: false},
			functions:   map[string][]*sqlmodel.Function{"myfunc": {mixed}},
			want:        map[string][]*sqlmodel.Function{"MyFunc": {mixed}},
		},
		{
			name:        "names a case-insensitive dialect matches are filed together in the order of their old keys",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
			functions:   map[string][]*sqlmodel.Function{"MyFunc": {mixed}, "MYFUNC": {shouted}},
			want:        map[string][]*sqlmodel.Function{"myfunc": {shouted, mixed}},
		},
		{
			name:        "an overload with no name keeps its old key under the rules",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
			functions:   map[string][]*sqlmodel.Function{"Bare": {unnamed, nil}},
			want:        map[string][]*sqlmodel.Function{"bare": {unnamed, nil}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
				"public": {Name: "public", Functions: testCase.functions},
			}}
			Adopt(catalogue, &dialect.Info{SchemaSearchPath: []string{"public"}, Identifiers: testCase.identifiers})

			require.Equal(t, testCase.want, catalogue.Schemas["public"].Functions)
			for key, overloads := range testCase.want {
				for index, overload := range overloads {
					require.Same(t, overload, catalogue.Schemas["public"].Functions[key][index])
				}
			}
		})
	}
}
