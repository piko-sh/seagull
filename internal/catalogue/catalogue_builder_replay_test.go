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

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func integerColumn(name string) sqlmodel.Column {
	return sqlmodel.NewColumn(name, sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "integer"), true)
}

func TestIfExistsFormsReplayAsPostgresAppliesThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		migrations      []string
		wantMessages    []string
		wantItems       []string
		wantMood        []string
		wantSnapshotKey []string
	}{
		{
			name: "a repeated CREATE TABLE IF NOT EXISTS keeps the first table",
			migrations: []string{
				"CREATE TABLE items (id int PRIMARY KEY, name text);",
				"CREATE TABLE IF NOT EXISTS items (id bigint PRIMARY KEY, label text);",
			},
			wantItems: []string{"id", "name"},
		},
		{
			name: "a repeated CREATE TABLE is reported",
			migrations: []string{
				"CREATE TABLE items (id int PRIMARY KEY, name text);",
				"CREATE TABLE items (id bigint PRIMARY KEY);",
			},
			wantMessages: []string{"table public.items already exists"},
			wantItems:    []string{"id", "name"},
		},
		{
			name: "ADD COLUMN IF NOT EXISTS skips a column the table has",
			migrations: []string{
				"CREATE TABLE items (id int PRIMARY KEY, name text);",
				"ALTER TABLE items ADD COLUMN IF NOT EXISTS name text, ADD COLUMN IF NOT EXISTS price int;",
			},
			wantItems: []string{"id", "name", "price"},
		},
		{
			name: "ADD COLUMN of a column the table has is reported",
			migrations: []string{
				"CREATE TABLE items (id int PRIMARY KEY, name text);",
				"ALTER TABLE items ADD COLUMN name text;",
			},
			wantMessages: []string{"column name already exists in table items"},
			wantItems:    []string{"id", "name"},
		},
		{
			name: "ALTER TABLE IF EXISTS of a missing table changes nothing",
			migrations: []string{
				"CREATE TABLE items (id int PRIMARY KEY, name text);",
				"ALTER TABLE IF EXISTS gone RENAME TO deprecated_gone;",
				"ALTER TABLE IF EXISTS gone ADD COLUMN x int, DROP COLUMN y;",
			},
			wantItems: []string{"id", "name"},
		},
		{
			name: "ALTER TABLE of a missing table is reported",
			migrations: []string{
				"CREATE TABLE items (id int PRIMARY KEY, name text);",
				"ALTER TABLE gone ADD COLUMN x int;",
			},
			wantMessages: []string{"table gone not found in schema public"},
			wantItems:    []string{"id", "name"},
		},
		{
			name: "ADD VALUE IF NOT EXISTS skips a value the enum holds and BEFORE places a new one",
			migrations: []string{
				"CREATE TYPE mood AS ENUM ('calm', 'busy');",
				"ALTER TYPE mood ADD VALUE IF NOT EXISTS 'calm';",
				"ALTER TYPE mood ADD VALUE 'idle' BEFORE 'busy';",
				"ALTER TYPE mood ADD VALUE 'first' BEFORE 'calm';",
				"ALTER TYPE mood ADD VALUE 'last' AFTER 'busy';",
			},
			wantMood: []string{"first", "calm", "idle", "busy", "last"},
		},
		{
			name: "ADD VALUE of a value the enum holds is reported",
			migrations: []string{
				"CREATE TYPE mood AS ENUM ('calm', 'busy');",
				"ALTER TYPE mood ADD VALUE 'calm';",
			},
			wantMessages: []string{`enum mood already has the value "calm"`},
			wantMood:     []string{"calm", "busy"},
		},
		{
			name: "CREATE TYPE IF NOT EXISTS keeps the first type",
			migrations: []string{
				"CREATE TYPE mood AS ENUM ('calm', 'busy');",
				"CREATE TYPE IF NOT EXISTS mood AS ENUM ('other');",
			},
			wantMood: []string{"calm", "busy"},
		},
		{
			name: "CREATE MATERIALIZED VIEW IF NOT EXISTS keeps the first view",
			migrations: []string{
				"CREATE TABLE items (id int PRIMARY KEY, name text);",
				"CREATE MATERIALIZED VIEW snapshot AS SELECT id FROM items;",
				"CREATE MATERIALIZED VIEW IF NOT EXISTS snapshot AS SELECT id, name FROM items;",
			},
			wantItems:       []string{"id", "name"},
			wantSnapshotKey: []string{"id"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, testCase.migrations...)
			require.Equal(t, len(testCase.wantMessages), len(diagnostics), diagnosticMessages(diagnostics))
			for index, message := range testCase.wantMessages {
				require.Contains(t, diagnostics[index].Message, message)
			}
			if testCase.wantItems != nil {
				require.Equal(t, testCase.wantItems, columnNamesOf(catalogue.LookupTable("", "items").Columns))
			}
			if testCase.wantMood != nil {
				require.Equal(t, testCase.wantMood, catalogue.Schemas["public"].Enums["mood"].Values)
			}
			if testCase.wantSnapshotKey != nil {
				require.Equal(t, testCase.wantSnapshotKey, columnNamesOf(catalogue.LookupView("", "snapshot").Columns))
			}
		})
	}
}

func TestColumnPlacementsPutColumnsWhereTheMutationSays(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		mutations   []*ast.CatalogueMutation
		wantColumns []string
		wantError   string
	}{
		{
			name: "an added column goes first",
			mutations: []*ast.CatalogueMutation{ast.NewCatalogueMutation(ast.MutationAlterTableAddColumn, "public", "placed",
				ast.WithColumns([]sqlmodel.Column{integerColumn("z")}), ast.WithPlacement(ast.PlacementFirst, ""))},
			wantColumns: []string{"z", "a", "b", "c"},
		},
		{
			name: "added columns go after their anchor, in order",
			mutations: []*ast.CatalogueMutation{ast.NewCatalogueMutation(ast.MutationAlterTableAddColumn, "public", "placed",
				ast.WithColumns([]sqlmodel.Column{integerColumn("m"), integerColumn("n")}), ast.WithPlacement(ast.PlacementAfter, "a"))},
			wantColumns: []string{"a", "m", "n", "b", "c"},
		},
		{
			name: "an altered column moves first and after its anchor",
			mutations: []*ast.CatalogueMutation{
				ast.NewCatalogueMutation(ast.MutationAlterTableAlterColumn, "public", "placed", ast.WithColumnName("c"),
					ast.WithColumns([]sqlmodel.Column{integerColumn("c")}), ast.WithPlacement(ast.PlacementFirst, "")),
				ast.NewCatalogueMutation(ast.MutationAlterTableAlterColumn, "public", "placed", ast.WithColumnName("a"),
					ast.WithColumns([]sqlmodel.Column{integerColumn("a")}), ast.WithPlacement(ast.PlacementAfter, "b")),
			},
			wantColumns: []string{"c", "b", "a"},
		},
		{
			name: "an altered column with no placement stays where it is",
			mutations: []*ast.CatalogueMutation{ast.NewCatalogueMutation(ast.MutationAlterTableAlterColumn, "public", "placed",
				ast.WithColumnName("a"), ast.WithColumns([]sqlmodel.Column{integerColumn("a")}))},
			wantColumns: []string{"a", "b", "c"},
		},
		{
			name: "an anchor the table lacks is reported",
			mutations: []*ast.CatalogueMutation{ast.NewCatalogueMutation(ast.MutationAlterTableAddColumn, "public", "placed",
				ast.WithColumns([]sqlmodel.Column{integerColumn("z")}), ast.WithPlacement(ast.PlacementAfter, "missing"))},
			wantColumns: []string{"a", "b", "c"},
			wantError:   "column missing not found in table placed",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilderWithTable("placed", integerColumn("a"), integerColumn("b"), integerColumn("c"))
			var lastError error
			for _, mutation := range testCase.mutations {
				lastError = builder.applyMutation(t.Context(), mutation)
			}
			if testCase.wantError != "" {
				require.EqualError(t, lastError, testCase.wantError)
			} else {
				require.NoError(t, lastError)
			}
			require.Equal(t, testCase.wantColumns, columnNamesOf(builder.Catalogue().Schemas["public"].Tables["placed"].Columns))
		})
	}
}

func TestColumnIfExistsSkipsAMissingColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		mutation *ast.CatalogueMutation
	}{
		{
			name: "alter",
			mutation: ast.NewCatalogueMutation(ast.MutationAlterTableAlterColumn, "public", "placed",
				ast.WithColumnName("missing"), ast.WithColumns([]sqlmodel.Column{integerColumn("missing")})),
		},
		{
			name:     "rename",
			mutation: ast.NewCatalogueMutation(ast.MutationAlterTableRenameColumn, "public", "placed", ast.WithColumnName("missing"), ast.WithNewName("other")),
		},
		{
			name:     "drop",
			mutation: ast.NewCatalogueMutation(ast.MutationAlterTableDropColumn, "public", "placed", ast.WithColumnName("missing")),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilderWithTable("placed", integerColumn("a"))
			testCase.mutation.IfColumnExists = true
			require.NoError(t, builder.applyMutation(t.Context(), testCase.mutation))
			require.Equal(t, []string{"a"}, columnNamesOf(builder.Catalogue().Schemas["public"].Tables["placed"].Columns))
		})
	}
}

func TestEnumPlacementsPutValuesWhereTheMutationSays(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		placement  ast.Placement
		wantValues []string
		wantError  string
	}{
		{name: "first", placement: ast.Placement{Anchor: "", Kind: ast.PlacementFirst}, wantValues: []string{"new", "calm", "busy"}},
		{name: "before", placement: ast.Placement{Anchor: "busy", Kind: ast.PlacementBefore}, wantValues: []string{"calm", "new", "busy"}},
		{name: "after", placement: ast.Placement{Anchor: "calm", Kind: ast.PlacementAfter}, wantValues: []string{"calm", "new", "busy"}},
		{name: "last", placement: ast.Placement{Anchor: "", Kind: ast.PlacementDefault}, wantValues: []string{"calm", "busy", "new"}},
		{
			name:       "a missing anchor is reported",
			placement:  ast.Placement{Anchor: "gone", Kind: ast.PlacementAfter},
			wantValues: []string{"calm", "busy"},
			wantError:  `enum mood has no value "gone"`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilder()
			require.NoError(t, builder.applyMutation(t.Context(), ast.NewCatalogueMutation(ast.MutationCreateEnum, "public", "",
				ast.WithEnum("mood", []string{"calm", "busy"}))))
			addValue := ast.NewCatalogueMutation(ast.MutationAlterEnumAddValue, "public", "", ast.WithEnum("mood", []string{"new"}))
			addValue.Placement = testCase.placement
			err := builder.applyMutation(t.Context(), addValue)
			if testCase.wantError != "" {
				require.EqualError(t, err, testCase.wantError)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.wantValues, builder.Catalogue().Schemas["public"].Enums["mood"].Values)
		})
	}
}

func TestViewStarsTakeTheColumnsTheyStandFor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		view        string
		wantColumns []string
	}{
		{name: "a bare star", view: "CREATE VIEW v AS SELECT * FROM items;", wantColumns: []string{"id", "name"}},
		{name: "a qualified star", view: "CREATE VIEW v AS SELECT items.* FROM items;", wantColumns: []string{"id", "name"}},
		{name: "a star after a column", view: "CREATE VIEW v AS SELECT 1 AS one, * FROM items;", wantColumns: []string{"one", "id", "name"}},
		{name: "declared names over a star", view: "CREATE VIEW v (item_id, item_name) AS SELECT * FROM items;", wantColumns: []string{"item_id", "item_name"}},
		{name: "fewer declared names than a star's columns", view: "CREATE VIEW v (item_id) AS SELECT * FROM items;", wantColumns: []string{"item_id", "name"}},
		{name: "fewer declared names than columns", view: "CREATE VIEW v (item_id) AS SELECT id, name FROM items;", wantColumns: []string{"item_id", "name"}},
		{name: "a materialised view", view: "CREATE MATERIALIZED VIEW v AS SELECT * FROM items WITH NO DATA;", wantColumns: []string{"id", "name"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, "CREATE TABLE items (id int PRIMARY KEY, name text NOT NULL);", testCase.view)
			require.Empty(t, diagnosticMessages(diagnostics))
			view := catalogue.LookupView("", "v")
			require.NotNil(t, view)
			require.Equal(t, testCase.wantColumns, columnNamesOf(view.Columns))
			for index := range view.Columns {
				require.NotEqual(t, sqlmodel.TypeCategoryUnknown, view.Columns[index].SQLType.Category, view.Columns[index].Name)
			}
		})
	}
}
