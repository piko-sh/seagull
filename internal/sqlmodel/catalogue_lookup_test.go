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

package sqlmodel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogueFindColumn(t *testing.T) {
	t.Parallel()

	catalogue := &Catalogue{
		DefaultSchema: "public",
		Identifiers:   IdentifierCase{Unquoted: IdentifierFoldingNone, Insensitive: true},
		Schemas: map[string]*Schema{
			"public": {
				Name: "public",
				Tables: map[string]*Table{
					"users": {Name: "users", Schema: "public", Columns: []Column{{Name: "id"}, {Name: "Email"}, {Name: "code", Comment: "public"}}},
				},
				Views: map[string]*View{
					"active_users": {Name: "active_users", Schema: "public", Columns: []Column{{Name: "id", Comment: "view"}}},
				},
			},
			"audit": {
				Name: "audit",
				Tables: map[string]*Table{
					"events": {Name: "events", Schema: "audit", Columns: []Column{{Name: "id"}}},
					"users":  {Name: "users", Schema: "audit", Columns: []Column{{Name: "code", Comment: "audit"}}},
				},
				Views: map[string]*View{
					"users": {Name: "users", Schema: "audit", Columns: []Column{{Name: "code", Comment: "audit view"}}},
				},
			},
			"empty": nil,
		},
	}
	auditFirst := *catalogue
	auditFirst.SearchPath = []string{"audit", "public"}

	testCases := []struct {
		name        string
		catalogue   *Catalogue
		schema      string
		table       string
		column      string
		wantColumn  string
		wantComment string
	}{
		{name: "exact match", catalogue: catalogue, table: "users", column: "id", wantColumn: "id"},
		{name: "case-insensitive match", catalogue: catalogue, table: "USERS", column: "email", wantColumn: "Email"},
		{name: "unqualified follows the search path", catalogue: catalogue, table: "users", column: "code", wantColumn: "code", wantComment: "public"},
		{name: "qualified names its own schema", catalogue: catalogue, schema: "audit", table: "users", column: "code", wantColumn: "code", wantComment: "audit"},
		{name: "a view is found when no table has the name", catalogue: catalogue, table: "active_users", column: "id", wantColumn: "id", wantComment: "view"},
		{name: "a table hides a view of the same name", catalogue: catalogue, schema: "audit", table: "users", column: "code", wantColumn: "code", wantComment: "audit"},
		{name: "a schema off the search path needs its qualifier", catalogue: catalogue, table: "events", column: "id"},
		{name: "qualified second schema", catalogue: catalogue, schema: "AUDIT", table: "events", column: "ID", wantColumn: "id"},
		{name: "search path order decides", catalogue: &auditFirst, table: "users", column: "code", wantColumn: "code", wantComment: "audit"},
		{name: "a qualified table lacking the column does not fall back", catalogue: catalogue, schema: "audit", table: "users", column: "id"},
		{name: "unknown column", catalogue: catalogue, table: "users", column: "missing"},
		{name: "unknown table", catalogue: catalogue, table: "missing", column: "id"},
		{name: "unknown schema", catalogue: catalogue, schema: "missing", table: "users", column: "id"},
		{name: "nil catalogue", catalogue: nil, table: "users", column: "id"},
		{name: "case-sensitive catalogue misses another case", catalogue: caseSensitive(catalogue), table: "users", column: "email"},
		{name: "case-sensitive catalogue matches exactly", catalogue: caseSensitive(catalogue), table: "users", column: "Email", wantColumn: "Email"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			column := testCase.catalogue.FindColumn(testCase.schema, testCase.table, testCase.column)
			if testCase.wantColumn == "" {
				assert.Nil(t, column)
				return
			}
			require.NotNil(t, column)
			assert.Equal(t, testCase.wantColumn, column.Name)
			assert.Equal(t, testCase.wantComment, column.Comment)
		})
	}
}

func TestCatalogueFindColumnAliasesCatalogue(t *testing.T) {
	t.Parallel()

	table := &Table{Name: "users", Columns: []Column{{Name: "id"}}}
	catalogue := &Catalogue{DefaultSchema: "public", Schemas: map[string]*Schema{"public": {Tables: map[string]*Table{"users": table}}}}

	column := catalogue.FindColumn("", "users", "id")
	require.NotNil(t, column)
	column.Nullable = true

	assert.True(t, table.Columns[0].Nullable)
}

func caseSensitive(catalogue *Catalogue) *Catalogue {
	copied := *catalogue
	copied.Identifiers = IdentifierCase{Unquoted: IdentifierFoldingLower, Insensitive: false}
	return &copied
}

func TestCatalogueLookupFollowsTheSearchPath(t *testing.T) {
	t.Parallel()

	shadowed := &Table{Name: "items", Schema: "temp"}
	catalogue := &Catalogue{
		DefaultSchema: "main",
		SearchPath:    []string{"main", "temp"},
		Identifiers:   IdentifierCase{Unquoted: IdentifierFoldingNone, Insensitive: true},
		Schemas: map[string]*Schema{
			"main": {Name: "main", Tables: map[string]*Table{"items": {Name: "Items", Schema: "main"}}, Views: map[string]*View{}},
			"temp": {Name: "temp", Tables: map[string]*Table{"items": shadowed, "scratch": {Name: "scratch"}}, Views: map[string]*View{"recent": {Name: "recent"}}},
		},
	}

	testCases := []struct {
		name     string
		schema   string
		table    string
		expected string
	}{
		{name: "the first schema on the path wins", schema: "", table: "ITEMS", expected: "Items"},
		{name: "a later schema on the path is searched", schema: "", table: "scratch", expected: "scratch"},
		{name: "a qualified name searches only its schema", schema: "TEMP", table: "items", expected: "items"},
		{name: "an unknown schema finds nothing", schema: "other", table: "items", expected: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			table := catalogue.LookupTable(testCase.schema, testCase.table)
			if testCase.expected == "" {
				require.Nil(t, table)
				return
			}
			require.NotNil(t, table)
			require.Equal(t, testCase.expected, table.Name)
		})
	}

	require.NotNil(t, catalogue.LookupView("", "Recent"))
	require.Nil(t, catalogue.LookupView("main", "recent"))
	require.Equal(t, "main", catalogue.LookupSchema("").Name)
	require.True(t, catalogue.OnSearchPath("Temp"))
	require.False(t, catalogue.OnSearchPath("other"))
	require.Equal(t, []string{"main", "temp"}, catalogue.SchemaNamesFor(""))
}

func TestCatalogueWithoutSearchPathUsesTheDefaultSchema(t *testing.T) {
	t.Parallel()

	catalogue := &Catalogue{DefaultSchema: "dbo", Schemas: map[string]*Schema{"dbo": {Name: "dbo", Tables: map[string]*Table{"items": {Name: "items"}}}}}
	require.Equal(t, []string{"dbo"}, catalogue.SchemaNamesFor(""))
	require.NotNil(t, catalogue.LookupTable("", "items"))

	var absent *Catalogue
	require.Nil(t, absent.SchemaNamesFor(""))
	require.Equal(t, "Name", absent.Key("Name"))
}

func TestCatalogueResolveNamedType(t *testing.T) {
	t.Parallel()

	catalogue := &Catalogue{
		DefaultSchema: "public",
		SearchPath:    []string{"public"},
		Identifiers:   IdentifierCase{Unquoted: IdentifierFoldingLower, Insensitive: true},
		Schemas: map[string]*Schema{
			"public": {
				Name:           "public",
				Enums:          map[string]*Enum{"mood": {Name: "mood", Schema: "public", Values: []string{"happy"}}},
				CompositeTypes: map[string]*CompositeType{"address": {Name: "address", Schema: "public"}},
			},
			"audit": {
				Name:  "audit",
				Enums: map[string]*Enum{"mood": {Name: "mood", Schema: "audit", Values: []string{"logged"}}, "level": {Name: "level", Values: []string{"high"}}},
			},
		},
	}
	unknown := func(name string) SQLType { return NewSQLType(TypeCategoryUnknown, name) }
	qualified := unknown("mood")
	qualified.Schema = "audit"

	tests := []struct {
		name         string
		sqlType      SQLType
		wantCategory SQLTypeCategory
		wantSchema   string
		wantName     string
		wantValues   []string
	}{
		{name: "unqualified follows the search path", sqlType: unknown("mood"), wantCategory: TypeCategoryEnum, wantSchema: "public", wantName: "mood", wantValues: []string{"happy"}},
		{name: "a schema prefix selects the schema", sqlType: unknown("audit.mood"), wantCategory: TypeCategoryEnum, wantSchema: "audit", wantName: "mood", wantValues: []string{"logged"}},
		{name: "the Schema field selects the schema", sqlType: qualified, wantCategory: TypeCategoryEnum, wantSchema: "audit", wantName: "mood", wantValues: []string{"logged"}},
		{name: "a type off the search path still resolves", sqlType: unknown("level"), wantCategory: TypeCategoryEnum, wantSchema: "audit", wantName: "level", wantValues: []string{"high"}},
		{name: "a composite type", sqlType: unknown("ADDRESS"), wantCategory: TypeCategoryComposite, wantSchema: "public", wantName: "ADDRESS"},
		{name: "a qualified miss stays unknown", sqlType: unknown("audit.address"), wantCategory: TypeCategoryUnknown, wantName: "audit.address"},
		{name: "a known category is left alone", sqlType: NewSQLType(TypeCategoryText, "mood"), wantCategory: TypeCategoryText, wantName: "mood"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolved := catalogue.ResolveNamedType(testCase.sqlType)
			assert.Equal(t, testCase.wantCategory, resolved.Category)
			assert.Equal(t, testCase.wantSchema, resolved.Schema)
			assert.Equal(t, testCase.wantName, resolved.EngineName)
			assert.Equal(t, testCase.wantValues, resolved.EnumValues)
		})
	}

	var empty *Catalogue
	assert.Equal(t, unknown("mood"), empty.ResolveNamedType(unknown("mood")))
}

func TestResolveNamedTypeSharesNoValuesWithTheEnum(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		kind string
	}{
		{name: "an enum", kind: "mood"},
		{name: "a domain over an enum", kind: "feeling"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			enumType := NewSQLType(TypeCategoryEnum, "mood")
			enumType.EnumValues = []string{"happy", "sad"}
			catalogue := &Catalogue{
				DefaultSchema: "public",
				SearchPath:    []string{"public"},
				Identifiers:   IdentifierCase{Unquoted: IdentifierFoldingLower, Insensitive: true},
				Schemas: map[string]*Schema{
					"public": {
						Name:    "public",
						Enums:   map[string]*Enum{"mood": {Name: "mood", Schema: "public", Values: []string{"happy", "sad"}}},
						Domains: map[string]*Domain{"feeling": {Name: "feeling", Schema: "public", BaseType: enumType}},
					},
				},
			}

			resolved := catalogue.ResolveNamedType(NewSQLType(TypeCategoryUnknown, testCase.kind))
			require.Equal(t, []string{"happy", "sad"}, resolved.EnumValues)
			resolved.EnumValues[0] = "changed"

			assert.Equal(t, []string{"happy", "sad"}, catalogue.Schemas["public"].Enums["mood"].Values)
			assert.Equal(t, []string{"happy", "sad"}, catalogue.Schemas["public"].Domains["feeling"].BaseType.EnumValues)
		})
	}
}

func TestLookupSequenceFollowsTheSearchPath(t *testing.T) {
	t.Parallel()

	catalogue := &Catalogue{SearchPath: []string{"app", "public"}, Schemas: map[string]*Schema{
		"public": {Name: "public", Sequences: map[string]*Sequence{"counter": {Name: "counter", Schema: "public"}}},
		"app":    {Name: "app", Sequences: map[string]*Sequence{"other": {Name: "other", Schema: "app"}}},
	}}

	require.Equal(t, "public", catalogue.LookupSequence("", "counter").Schema)
	require.Equal(t, "app", catalogue.LookupSequence("app", "other").Schema)
	require.Nil(t, catalogue.LookupSequence("app", "counter"))
	require.Nil(t, catalogue.LookupSequence("", "missing"))
}

func TestCatalogueFunctionsOnPathFollowsLookupOrder(t *testing.T) {
	t.Parallel()

	overloads := func(schema string, count int) []*Function {
		functions := make([]*Function, 0, count)
		for range count {
			functions = append(functions, &Function{Name: "stats", Schema: schema})
		}
		return functions
	}
	catalogue := &Catalogue{
		DefaultSchema: "app",
		SearchPath:    []string{"app", "missing", "bare", "public"},
		Identifiers:   IdentifierCase{Unquoted: IdentifierFoldingNone, Insensitive: true},
		Schemas: map[string]*Schema{
			"app":       {Name: "app", Functions: map[string][]*Function{"stats": overloads("app", 2)}},
			"bare":      {Name: "bare", Functions: map[string][]*Function{"stats": {}, "other": overloads("bare", 1)}},
			"public":    {Name: "public", Functions: map[string][]*Function{"stats": overloads("public", 1)}},
			"analytics": {Name: "analytics", Functions: map[string][]*Function{"stats": overloads("analytics", 1), "report": overloads("analytics", 1)}},
			"empty":     nil,
		},
	}
	reversed := *catalogue
	reversed.SearchPath = []string{"public", "app"}
	repeated := *catalogue
	repeated.SearchPath = []string{"app", "APP", "public", "app"}
	withoutPath := *catalogue
	withoutPath.SearchPath = nil
	withoutPath.DefaultSchema = "Public"

	testCases := []struct {
		catalogue *Catalogue
		name      string
		schema    string
		function  string
		want      []string
		limit     int
	}{
		{name: "a qualified reference yields only its schema", catalogue: catalogue, schema: "analytics", function: "stats", want: []string{"analytics"}},
		{name: "an unqualified reference follows the search path in order", catalogue: catalogue, function: "stats", want: []string{"app", "public"}},
		{name: "the search path order decides", catalogue: &reversed, function: "stats", want: []string{"public", "app"}},
		{name: "a schema off the search path is not yielded", catalogue: catalogue, function: "report", want: nil},
		{name: "a schema without the function is skipped", catalogue: catalogue, schema: "bare", function: "stats", want: nil},
		{name: "a qualified unknown schema yields nothing", catalogue: catalogue, schema: "elsewhere", function: "stats", want: nil},
		{name: "a qualified schema with no contents yields nothing", catalogue: catalogue, schema: "empty", function: "stats", want: nil},
		{name: "an unknown function yields nothing", catalogue: catalogue, function: "missing", want: nil},
		{name: "names fold to the catalogue's keys", catalogue: catalogue, schema: "ANALYTICS", function: "STATS", want: []string{"analytics"}},
		{name: "an unqualified name folds before the search", catalogue: catalogue, function: "Stats", want: []string{"app", "public"}},
		{name: "a case-sensitive catalogue keys schemas exactly", catalogue: caseSensitive(catalogue), schema: "ANALYTICS", function: "stats", want: nil},
		{name: "a case-sensitive catalogue matches a function only in the case it is filed under", catalogue: caseSensitive(catalogue), schema: "analytics", function: "Stats", want: nil},
		{name: "a case-sensitive catalogue finds a function by its exact key", catalogue: caseSensitive(catalogue), schema: "analytics", function: "stats", want: []string{"analytics"}},
		{name: "a schema listed twice on the search path is yielded once", catalogue: &repeated, function: "stats", want: []string{"app", "public"}},
		{name: "a catalogue without a search path uses its default schema", catalogue: &withoutPath, function: "stats", want: []string{"public"}},
		{name: "a nil catalogue yields nothing", catalogue: nil, function: "stats", want: nil},
		{name: "a consumer that stops early ends the walk", catalogue: catalogue, function: "stats", want: []string{"app"}, limit: 1},
		{name: "a consumer that stops on a qualified reference ends the walk", catalogue: catalogue, schema: "public", function: "stats", want: []string{"public"}, limit: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var yielded []string
			for schema, functions := range testCase.catalogue.FunctionsOnPath(testCase.schema, testCase.function) {
				require.NotNil(t, schema)
				require.NotEmpty(t, functions)
				for _, function := range functions {
					assert.Equal(t, schema.Name, function.Schema)
				}
				yielded = append(yielded, schema.Name)
				if testCase.limit > 0 && len(yielded) == testCase.limit {
					break
				}
			}
			assert.Equal(t, testCase.want, yielded)
		})
	}
}

func TestCatalogueFunctionsOnPathYieldsTheSchemasOwnOverloads(t *testing.T) {
	t.Parallel()

	overloads := []*Function{{Name: "stats", Schema: "public"}, {Name: "stats", Schema: "public"}}
	catalogue := &Catalogue{
		DefaultSchema: "public",
		Schemas:       map[string]*Schema{"public": {Name: "public", Functions: map[string][]*Function{"stats": overloads}}},
	}

	testCases := []struct {
		name   string
		schema string
	}{
		{name: "an unqualified reference", schema: ""},
		{name: "a qualified reference", schema: "public"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			count := 0
			for schema, functions := range catalogue.FunctionsOnPath(testCase.schema, "stats") {
				count++
				assert.Same(t, catalogue.Schemas["public"], schema)
				require.Len(t, functions, len(overloads))
				for index := range overloads {
					assert.Same(t, overloads[index], functions[index])
				}
			}
			assert.Equal(t, 1, count)
		})
	}
}

func TestCatalogueFunctionsOnPathKeysFunctionNamesByTheDialectsRules(t *testing.T) {
	t.Parallel()

	quoted := &Function{Name: "Foo", Schema: "public"}
	unquoted := &Function{Name: "foo", Schema: "public"}
	mixed := &Function{Name: "MyFunc", Schema: "default"}

	testCases := []struct {
		catalogue *Catalogue
		name      string
		function  string
		want      []*Function
	}{
		{
			name:      "an unquoted name folded to lower case finds the unquoted function",
			catalogue: functionCatalogue("public", IdentifierCase{Unquoted: IdentifierFoldingLower, Insensitive: false}, quoted, unquoted),
			function:  "foo",
			want:      []*Function{unquoted},
		},
		{
			name:      "a quoted mixed-case name finds the quoted function",
			catalogue: functionCatalogue("public", IdentifierCase{Unquoted: IdentifierFoldingLower, Insensitive: false}, quoted, unquoted),
			function:  "Foo",
			want:      []*Function{quoted},
		},
		{
			name:      "a name kept as written finds the function of that case",
			catalogue: functionCatalogue("default", IdentifierCase{Unquoted: IdentifierFoldingNone, Insensitive: false}, mixed),
			function:  "MyFunc",
			want:      []*Function{mixed},
		},
		{
			name:      "a name kept as written does not find a function of another case",
			catalogue: functionCatalogue("default", IdentifierCase{Unquoted: IdentifierFoldingNone, Insensitive: false}, mixed),
			function:  "myfunc",
			want:      nil,
		},
		{
			name:      "a case-insensitive catalogue finds the function in any case",
			catalogue: functionCatalogue("default", IdentifierCase{Unquoted: IdentifierFoldingNone, Insensitive: true}, mixed),
			function:  "MYFUNC",
			want:      []*Function{mixed},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var found []*Function
			for _, overloads := range testCase.catalogue.FunctionsOnPath("", testCase.function) {
				found = append(found, overloads...)
			}
			assert.Equal(t, testCase.want, found)
		})
	}
}

func functionCatalogue(schema string, identifiers IdentifierCase, functions ...*Function) *Catalogue {
	catalogue := &Catalogue{
		DefaultSchema: schema,
		SearchPath:    []string{schema},
		Identifiers:   identifiers,
		Schemas:       map[string]*Schema{schema: {Name: schema, Functions: map[string][]*Function{}}},
	}
	for _, function := range functions {
		key := identifiers.Key(function.Name)
		catalogue.Schemas[schema].Functions[key] = append(catalogue.Schemas[schema].Functions[key], function)
	}
	return catalogue
}
