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
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestRenamedAndMovedTypesTakeTheirColumnsWithThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		migrations []string
		wantSchema string
		wantName   string
		wantType   sqlmodel.SQLTypeCategory
	}{
		{
			name: "an enum renamed after its column was declared",
			migrations: []string{
				"CREATE TYPE event AS ENUM ('START', 'STOP');",
				"CREATE TABLE logs (id int PRIMARY KEY, e event NOT NULL);",
				"ALTER TYPE event RENAME TO new_event;",
			},
			wantSchema: "public", wantName: "new_event", wantType: sqlmodel.TypeCategoryEnum,
		},
		{
			name: "an enum renamed before its column was declared",
			migrations: []string{
				"CREATE TYPE event AS ENUM ('START', 'STOP');",
				`ALTER TYPE event RENAME TO "new_event";`,
				"ALTER TYPE new_event ADD VALUE 'PAUSE';",
				"CREATE TABLE logs (id int PRIMARY KEY, e new_event NOT NULL);",
			},
			wantSchema: "public", wantName: "new_event", wantType: sqlmodel.TypeCategoryEnum,
		},
		{
			name: "an enum moved to another schema",
			migrations: []string{
				"CREATE SCHEMA core;",
				"CREATE TYPE role_types AS ENUM ('admin', 'user');",
				"CREATE TABLE logs (id int PRIMARY KEY, e role_types NOT NULL);",
				"ALTER TYPE role_types SET SCHEMA core;",
			},
			wantSchema: "core", wantName: "role_types", wantType: sqlmodel.TypeCategoryEnum,
		},
		{
			name: "a composite type renamed",
			migrations: []string{
				"CREATE TYPE point2 AS (x int, y int);",
				"CREATE TABLE logs (id int PRIMARY KEY, e point2);",
				"ALTER TYPE point2 RENAME TO position;",
			},
			wantSchema: "public", wantName: "position", wantType: sqlmodel.TypeCategoryComposite,
		},
		{
			name: "an enum array column follows",
			migrations: []string{
				"CREATE TYPE event AS ENUM ('START', 'STOP');",
				"CREATE TABLE logs (id int PRIMARY KEY, e event[]);",
				"ALTER TYPE event RENAME TO new_event;",
			},
			wantSchema: "public", wantName: "new_event", wantType: sqlmodel.TypeCategoryEnum,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, testCase.migrations...)
			require.Empty(t, diagnosticMessages(diagnostics))
			column := catalogue.FindColumn("", "logs", "e")
			require.NotNil(t, column)
			require.Equal(t, testCase.wantType, column.SQLType.Category)
			require.Equal(t, testCase.wantName, column.SQLType.EngineName)
			require.Equal(t, testCase.wantSchema, column.SQLType.Schema)
			schema, name := newCatalogueBuilderOver(catalogue).lookupType(testCase.wantSchema, testCase.wantName)
			require.NotNil(t, schema)
			require.Equal(t, testCase.wantName, name)
		})
	}
}

func TestADomainRenamedKeepsResolving(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t,
		"CREATE DOMAIN email AS text NOT NULL;",
		"ALTER DOMAIN email RENAME TO address;",
		"CREATE TABLE contacts (id int PRIMARY KEY, a address);",
	)
	require.Empty(t, diagnosticMessages(diagnostics))
	column := catalogue.FindColumn("", "contacts", "a")
	require.NotNil(t, column)
	require.Equal(t, sqlmodel.TypeCategoryText, column.SQLType.Category)
	require.False(t, column.Nullable)
}

func TestRenamedAndMovedViewsAreKnownByTheirNewNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		migrations  []string
		wantSchema  string
		wantView    string
		wantColumns []string
	}{
		{
			name: "ALTER VIEW RENAME TO",
			migrations: []string{
				"CREATE VIEW authors_v AS SELECT * FROM authors;",
				"ALTER VIEW authors_v RENAME TO authors_v2;",
			},
			wantSchema: "public", wantView: "authors_v2", wantColumns: []string{"id", "name"},
		},
		{
			name: "ALTER MATERIALIZED VIEW RENAME TO, then a drop of another",
			migrations: []string{
				"CREATE MATERIALIZED VIEW authors_mv AS SELECT * FROM authors;",
				"ALTER MATERIALIZED VIEW authors_mv RENAME TO authors_mv_new;",
				"CREATE MATERIALIZED VIEW authors_mv_old AS SELECT name FROM authors;",
				"ALTER MATERIALIZED VIEW authors_mv_old RENAME TO authors_mv_older;",
				"DROP MATERIALIZED VIEW authors_mv_older;",
			},
			wantSchema: "public", wantView: "authors_mv_new", wantColumns: []string{"id", "name"},
		},
		{
			name: "ALTER MATERIALIZED VIEW SET SCHEMA",
			migrations: []string{
				"CREATE MATERIALIZED VIEW something AS SELECT * FROM authors;",
				"CREATE SCHEMA computed_tables;",
				`ALTER MATERIALIZED VIEW "something" SET SCHEMA "computed_tables";`,
			},
			wantSchema: "computed_tables", wantView: "something", wantColumns: []string{"id", "name"},
		},
		{
			name: "ALTER TABLE RENAME TO of a view",
			migrations: []string{
				"CREATE VIEW authors_v AS SELECT * FROM authors;",
				"ALTER TABLE authors_v RENAME TO authors_v2;",
			},
			wantSchema: "public", wantView: "authors_v2", wantColumns: []string{"id", "name"},
		},
		{
			name: "ALTER VIEW RENAME COLUMN",
			migrations: []string{
				"CREATE VIEW authors_v AS SELECT * FROM authors;",
				"ALTER VIEW authors_v RENAME COLUMN name TO full_name;",
			},
			wantSchema: "public", wantView: "authors_v", wantColumns: []string{"id", "full_name"},
		},
		{
			name: "ALTER VIEW IF EXISTS of a missing view",
			migrations: []string{
				"CREATE VIEW authors_v AS SELECT * FROM authors;",
				"ALTER VIEW IF EXISTS missing RENAME TO other;",
			},
			wantSchema: "public", wantView: "authors_v", wantColumns: []string{"id", "name"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			migrations := append([]string{"CREATE TABLE authors (id bigint PRIMARY KEY, name text NOT NULL);"}, testCase.migrations...)
			catalogue, diagnostics := replayPostgresMigrations(t, migrations...)
			require.Empty(t, diagnosticMessages(diagnostics))
			schema := catalogue.Schemas[testCase.wantSchema]
			require.NotNil(t, schema)
			require.Len(t, schema.Views, 1)
			view := schema.Views[testCase.wantView]
			require.NotNil(t, view)
			require.Equal(t, testCase.wantView, view.Name)
			require.Equal(t, testCase.wantSchema, view.Schema)
			require.Equal(t, testCase.wantColumns, columnNamesOf(view.Columns))
		})
	}
}

func newCatalogueBuilderOver(catalogue *sqlmodel.Catalogue) *catalogueBuilder {
	builder, _ := setupBuilder()
	builder.catalogue = catalogue
	return builder
}

func TestRenamingOrMovingAViewOrATypeOntoATakenNameIsRefused(t *testing.T) {
	t.Parallel()

	const setup = "CREATE SCHEMA archive;\nCREATE TABLE t (id int, name text);\nCREATE VIEW v AS SELECT id, name FROM t;\n" +
		"CREATE VIEW archive.v AS SELECT id FROM t;\nCREATE TYPE mood AS ENUM ('a');\nCREATE TYPE colour AS ENUM ('b');\n" +
		"CREATE TYPE archive.mood AS ENUM ('c');\n"
	testCases := []struct {
		name        string
		statement   string
		wantCode    string
		wantMessage string
	}{
		{name: "a view renamed onto a table", statement: "ALTER VIEW v RENAME TO t;", wantCode: diag.CodeDuplicateObject,
			wantMessage: "relation public.t already exists"},
		{name: "a view moved onto a view", statement: "ALTER VIEW v SET SCHEMA archive;", wantCode: diag.CodeDuplicateObject,
			wantMessage: "relation archive.v already exists"},
		{name: "a view column renamed onto a column", statement: "ALTER VIEW v RENAME COLUMN id TO name;",
			wantCode: diag.CodeDuplicateObject, wantMessage: "column name already exists in view v"},
		{name: "a type renamed onto a type", statement: "ALTER TYPE mood RENAME TO colour;", wantCode: diag.CodeDuplicateObject,
			wantMessage: "type public.colour already exists"},
		{name: "a type moved onto a type", statement: "ALTER TYPE mood SET SCHEMA archive;", wantCode: diag.CodeDuplicateObject,
			wantMessage: "type archive.mood already exists"},
		{name: "a missing view renamed", statement: "ALTER VIEW missing RENAME TO w;", wantCode: diag.CodeUnknownObject,
			wantMessage: "missing not found"},
		{name: "a missing type renamed", statement: "ALTER TYPE missing RENAME TO other;", wantCode: diag.CodeUnknownObject,
			wantMessage: "type missing not found"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, setup+testCase.statement)

			require.Len(t, diagnostics, 1)
			require.Equal(t, testCase.wantCode, diagnostics[0].Code)
			require.Equal(t, 8, diagnostics[0].Line)
			require.Contains(t, diagnostics[0].Message, testCase.wantMessage)
			public := catalogue.Schemas["public"]
			require.Equal(t, []string{"t"}, slices.Sorted(maps.Keys(public.Tables)))
			require.Equal(t, []string{"v"}, slices.Sorted(maps.Keys(public.Views)))
			require.Equal(t, []string{"id", "name"}, columnNamesOf(public.Views["v"].Columns))
			require.Equal(t, []string{"colour", "mood"}, slices.Sorted(maps.Keys(public.Enums)))
			require.Equal(t, []string{"mood"}, slices.Sorted(maps.Keys(catalogue.Schemas["archive"].Enums)))
		})
	}
}

func TestEachNestedTypeVisitsEveryTypeDownToTheDepthLimit(t *testing.T) {
	t.Parallel()

	nested := func(levels int) sqlmodel.SQLType {
		sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood")
		for range levels {
			sqlType = sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "array", ElementType: new(sqlType)}
		}
		return sqlType
	}
	structured := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "row")
	structured.StructFields = []sqlmodel.StructField{{Name: "a", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood")}}
	structured.UnionMembers = []sqlmodel.UnionMember{{Tag: "b", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood")}}
	keyed := sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, "map")
	keyed.KeyType = new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood"))
	keyed.ElementType = &structured

	testCases := []struct {
		name      string
		sqlType   sqlmodel.SQLType
		wantEnums int
	}{
		{name: "an enum inside arrays within the limit", sqlType: nested(sqlmodel.MaxTypeDepth - 1), wantEnums: 1},
		{name: "an enum inside arrays past the limit", sqlType: nested(sqlmodel.MaxTypeDepth), wantEnums: 0},
		{name: "enums as a key, a field and a member", sqlType: keyed, wantEnums: 3},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			enums := 0
			eachNestedType(&testCase.sqlType, func(visited *sqlmodel.SQLType) {
				if visited.Category == sqlmodel.TypeCategoryEnum {
					enums++
				}
			})
			require.Equal(t, testCase.wantEnums, enums)
		})
	}
}
