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

package harness

import (
	"maps"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

func TestCompareCataloguesListsWhatDiffers(t *testing.T) {
	t.Parallel()

	text := dialect.SQLType{Category: dialect.TypeCategoryText, EngineName: "text"}
	integer := dialect.SQLType{Category: dialect.TypeCategoryInteger, EngineName: "int4"}
	migrations := &dialect.Catalogue{Schemas: map[string]*dialect.Schema{"public": {
		Name: "public",
		Tables: map[string]*dialect.Table{
			"boats":    {Name: "boats", Columns: []dialect.Column{dialect.NewColumn("id", integer, false), dialect.NewColumn("name", text, true)}, PrimaryKey: []string{"id"}},
			"harbours": {Name: "harbours", Columns: []dialect.Column{dialect.NewColumn("id", integer, false)}},
		},
	}}}
	database := &dialect.Catalogue{Schemas: map[string]*dialect.Schema{"test_case": {
		Name: "test_case",
		Tables: map[string]*dialect.Table{
			"boats": {Name: "boats", Columns: []dialect.Column{dialect.NewColumn("id", integer, false), dialect.NewColumn("name", text, false)}, PrimaryKey: []string{"id"}},
		},
	}}}

	differences := CompareCatalogues(
		CatalogueSchema{Catalogue: migrations, Name: "public"},
		CatalogueSchema{Catalogue: database, Name: "test_case"},
	)

	require.Equal(t, []string{
		"table boats: column name: nullable differs: migrations true, database false",
		"table harbours is missing from the database",
	}, differences)
	require.Empty(t, CompareCatalogues(
		CatalogueSchema{Catalogue: migrations, Name: "public"},
		CatalogueSchema{Catalogue: migrations, Name: "public"},
	))
}

func TestCompareCataloguesListsPartitionsAndHiddenColumnsThatDiffer(t *testing.T) {
	t.Parallel()

	integer := dialect.SQLType{Category: dialect.TypeCategoryInteger, EngineName: "int8"}
	hidden := dialect.NewColumn("rowid", integer, false)
	hidden.Hidden = true
	migrations := &dialect.Catalogue{Schemas: map[string]*dialect.Schema{"public": {
		Name: "public",
		Tables: map[string]*dialect.Table{
			"readings":      {Name: "readings", Columns: []dialect.Column{dialect.NewColumn("id", integer, false)}},
			"readings_2026": {Name: "readings_2026", Columns: []dialect.Column{dialect.NewColumn("id", integer, false)}, PartitionOf: &dialect.TableName{Schema: "public", Name: "readings"}},
			"readings_2027": {Name: "readings_2027", Columns: []dialect.Column{dialect.NewColumn("id", integer, false)}, PartitionOf: &dialect.TableName{Schema: "public", Name: "readings"}},
			"notes":         {Name: "notes", Columns: []dialect.Column{hidden}},
		},
	}}}
	database := &dialect.Catalogue{Schemas: map[string]*dialect.Schema{"test_case": {
		Name: "test_case",
		Tables: map[string]*dialect.Table{
			"readings":      {Name: "readings", Columns: []dialect.Column{dialect.NewColumn("id", integer, false)}},
			"readings_2026": {Name: "readings_2026", Columns: []dialect.Column{dialect.NewColumn("id", integer, false)}, PartitionOf: &dialect.TableName{Schema: "test_case", Name: "readings"}},
			"readings_2027": {Name: "readings_2027", Columns: []dialect.Column{dialect.NewColumn("id", integer, false)}},
			"notes":         {Name: "notes", Columns: []dialect.Column{dialect.NewColumn("rowid", integer, false)}},
		},
	}}}

	differences := CompareCatalogues(
		CatalogueSchema{Catalogue: migrations, Name: "public"},
		CatalogueSchema{Catalogue: database, Name: "test_case"},
	)

	require.Equal(t, []string{
		"table notes: column rowid: hidden differs: migrations true, database false",
		"table readings_2027: partition differs: migrations partition of readings, database no partition",
	}, differences)
}

func TestCompareCataloguesReadsAMissingCatalogueAsAnEmptySchema(t *testing.T) {
	t.Parallel()

	integer := dialect.SQLType{Category: dialect.TypeCategoryInteger, EngineName: "integer"}
	replayed := &dialect.Catalogue{Schemas: map[string]*dialect.Schema{"main": {
		Name:   "main",
		Tables: map[string]*dialect.Table{"boats": {Name: "boats", Columns: []dialect.Column{dialect.NewColumn("id", integer, false)}}},
	}}}
	tests := []struct {
		database *dialect.Catalogue
		name     string
		want     []string
	}{
		{name: "no catalogue at all", database: nil, want: []string{"table boats is missing from the database"}},
		{name: "a catalogue without the schema", database: &dialect.Catalogue{}, want: []string{"table boats is missing from the database"}},
		{name: "a catalogue holding a nil schema", database: &dialect.Catalogue{Schemas: map[string]*dialect.Schema{"main": nil}},
			want: []string{"table boats is missing from the database"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, CompareCatalogues(
				CatalogueSchema{Catalogue: replayed, Name: "main"},
				CatalogueSchema{Catalogue: test.database, Name: "main"},
			))
		})
	}
}

func TestAllowlistReportsTheDifferencesItDoesNotHold(t *testing.T) {
	t.Parallel()

	allowlist := Allowlist{"001_boats: table boats is missing from the database": "the engine cannot introspect it"}
	tests := []struct {
		name        string
		caseName    string
		differences []string
		want        []string
		wantUsed    map[string]bool
	}{
		{name: "an allowed difference is marked used", caseName: "001_boats", differences: []string{"table boats is missing from the database"},
			want: nil, wantUsed: map[string]bool{"001_boats: table boats is missing from the database": true}},
		{name: "an unlisted difference is reported", caseName: "001_boats", differences: []string{"view docks is missing from the database"},
			want: []string{"introspection differs from migration replay: 001_boats: view docks is missing from the database"}, wantUsed: map[string]bool{}},
		{name: "an entry belongs to its own case only", caseName: "002_docks", differences: []string{"table boats is missing from the database"},
			want: []string{"introspection differs from migration replay: 002_docks: table boats is missing from the database"}, wantUsed: map[string]bool{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			used := map[string]bool{}
			require.Equal(t, test.want, allowlist.unexpected(test.caseName, test.differences, used))
			require.Equal(t, test.wantUsed, used)
			if test.want == nil {
				allowlist.Check(t, test.caseName, test.differences, map[string]bool{})
				allowlist.CheckUsed(t, map[string]bool{test.caseName: true}, used)
			}
		})
	}
}

func TestAllowlistReportsEntriesThatNoLongerOccur(t *testing.T) {
	t.Parallel()

	allowlist := Allowlist{
		"001_boats: table boats is missing from the database": "the engine cannot introspect it",
		"002_docks: view docks is missing from the database":  "the engine cannot introspect it",
	}
	tests := []struct {
		checked map[string]bool
		used    map[string]bool
		name    string
		want    []string
	}{
		{name: "a used entry is current", checked: map[string]bool{"001_boats": true},
			used: map[string]bool{"001_boats: table boats is missing from the database": true}, want: nil},
		{name: "an unused entry of a checked case is stale", checked: map[string]bool{"001_boats": true, "002_docks": true},
			used: map[string]bool{"001_boats: table boats is missing from the database": true},
			want: []string{"allowlisted difference no longer occurs, remove it: 002_docks: view docks is missing from the database"}},
		{name: "an entry of a case that did not run is left alone", checked: map[string]bool{}, used: map[string]bool{}, want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, allowlist.stale(test.checked, test.used))
		})
	}
}

func TestColumnNamesKeepsTheColumnOrder(t *testing.T) {
	t.Parallel()

	text := dialect.SQLType{Category: dialect.TypeCategoryText, EngineName: "text"}
	tests := []struct {
		name    string
		columns []dialect.Column
		want    []string
	}{
		{name: "no columns", columns: nil, want: []string{}},
		{name: "columns in declaration order", columns: []dialect.Column{dialect.NewColumn("name", text, true), dialect.NewColumn("harbour", text, false)},
			want: []string{"name", "harbour"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, columnNames(test.columns))
		})
	}
}

func TestReplayCatalogueBuildsTheCatalogueTheMigrationsDescribe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		files      fstest.MapFS
		name       string
		wantTables []string
	}{
		{name: "forward migrations only", files: fstest.MapFS{
			"migrations/0001_boats.up.sql":   {Data: []byte("CREATE TABLE boats (id INTEGER PRIMARY KEY, name TEXT NOT NULL);\n")},
			"migrations/0001_boats.down.sql": {Data: []byte("DROP TABLE boats;\n")},
			"migrations/0002_docks.up.sql":   {Data: []byte("CREATE TABLE docks (id INTEGER PRIMARY KEY);\n")},
		}, wantTables: []string{"boats", "docks"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			catalogue := ReplayCatalogue(t, dialect_engine_sqlite.New(), test.files)

			schema := CatalogueSchema{Catalogue: catalogue, Name: catalogue.DefaultSchema}.contents()
			require.ElementsMatch(t, test.wantTables, slices.Collect(maps.Keys(schema.Tables)))
		})
	}
}
