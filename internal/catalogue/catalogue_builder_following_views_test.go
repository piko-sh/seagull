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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func replayMigrationsWith(t *testing.T, engine dialect.DialectPort, migrations ...string) *sqlmodel.Catalogue {
	t.Helper()

	builder := newCatalogueBuilder(engine, namespace.Default())
	diagnostics := make([]diag.SourceError, 0, len(migrations))
	for index, migration := range migrations {
		diagnostics = append(diagnostics, applyMigration(t, builder, "migration.sql", migration, index)...)
	}
	require.Empty(t, diagnostics)
	return builder.Catalogue()
}

func viewColumnNames(t *testing.T, catalogue *sqlmodel.Catalogue, name string) []string {
	t.Helper()

	view := catalogue.LookupView("", name)
	require.NotNil(t, view, "the view %s", name)
	return columnNamesOf(view.Columns)
}

func TestAViewFollowsItsTablesWhereTheEngineReadsItsDefinitionAnew(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		engine     dialect.DialectPort
		wantViews  map[string][]string
		name       string
		migrations []string
	}{
		{
			name:   "a column SQLite adds to a table reaches the views reading it through a star",
			engine: sqlite.New(),
			migrations: []string{
				"CREATE TABLE items (id INTEGER NOT NULL, name TEXT);\n" +
					"CREATE VIEW star_items AS SELECT * FROM items;\n" +
					"CREATE VIEW qualified_items AS SELECT items.* FROM items;\n" +
					"CREATE VIEW listed_items AS SELECT id, name FROM items;\n" +
					"CREATE VIEW stacked_items AS SELECT * FROM star_items;",
				"ALTER TABLE items ADD COLUMN price REAL;",
			},
			wantViews: map[string][]string{
				"star_items":      {"id", "name", "price"},
				"qualified_items": {"id", "name", "price"},
				"listed_items":    {"id", "name"},
				"stacked_items":   {"id", "name", "price"},
			},
		},
		{
			name:   "a table SQLite drops and creates again gives a star view its new columns",
			engine: sqlite.New(),
			migrations: []string{
				"CREATE TABLE items (id INTEGER NOT NULL, name TEXT);\nCREATE VIEW star_items AS SELECT * FROM items;",
				"DROP TABLE items;\nCREATE TABLE items (code TEXT NOT NULL);",
			},
			wantViews: map[string][]string{"star_items": {"code"}},
		},
		{
			name:   "DuckDB views follow added, renamed and dropped columns",
			engine: duckdb.New(),
			migrations: []string{
				"CREATE TABLE items (id INTEGER NOT NULL, name VARCHAR);\n" +
					"CREATE VIEW star_items AS SELECT * FROM items;\n" +
					"CREATE VIEW named_items (item_id, item_name) AS SELECT * FROM items;\n" +
					"CREATE VIEW listed_items AS SELECT id, name FROM items;",
				"ALTER TABLE items ADD COLUMN price DOUBLE;\nALTER TABLE items ADD COLUMN stock INTEGER;",
				"ALTER TABLE items RENAME COLUMN name TO label;\nALTER TABLE items DROP COLUMN stock;",
			},
			wantViews: map[string][]string{
				"star_items":   {"id", "label", "price"},
				"named_items":  {"item_id", "item_name", "price"},
				"listed_items": {"id", "name"},
			},
		},
		{
			name:   "a PostgreSQL view keeps the columns its star stood for when it was created",
			engine: postgres.New(),
			migrations: []string{
				"CREATE TABLE items (id integer NOT NULL, name text);\nCREATE VIEW star_items AS SELECT * FROM items;",
				"ALTER TABLE items ADD COLUMN price double precision;",
			},
			wantViews: map[string][]string{"star_items": {"id", "name"}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := replayMigrationsWith(t, testCase.engine, testCase.migrations...)
			for view, want := range testCase.wantViews {
				require.Equal(t, want, viewColumnNames(t, catalogue, view), "the view %s", view)
			}
		})
	}
}

func TestADuckDBViewColumnTakesTheTypeItsTableColumnChangesTo(t *testing.T) {
	t.Parallel()

	catalogue := replayMigrationsWith(t, duckdb.New(),
		"CREATE TABLE items (id INTEGER NOT NULL, name VARCHAR);\n"+
			"CREATE VIEW star_items AS SELECT * FROM items;\n"+
			"CREATE VIEW listed_items AS SELECT id FROM items;",
		"ALTER TABLE items ALTER COLUMN id TYPE BIGINT;",
	)

	table := catalogue.LookupTable("", "items")
	require.NotNil(t, table)
	for _, name := range []string{"star_items", "listed_items"} {
		view := catalogue.LookupView("", name)
		require.NotNil(t, view)
		require.Equal(t, table.Columns[0].SQLType, view.Columns[0].SQLType, "the view %s", name)
		require.False(t, view.Columns[0].Nullable, "the view %s", name)
	}
}

func TestAViewColumnKeepsItsDirectiveWhenTheViewFollowsItsTable(t *testing.T) {
	t.Parallel()

	catalogue := replayMigrationsWith(t, sqlite.New(),
		"CREATE TABLE items (id INTEGER NOT NULL, name TEXT);\n"+
			"CREATE VIEW star_items AS SELECT * FROM items;\n"+
			"-- sgl.column(star_items.name, nullable: false, go_type: example.com/kinds.Name)",
		"ALTER TABLE items ADD COLUMN price REAL;",
	)

	view := catalogue.LookupView("", "star_items")
	require.NotNil(t, view)
	require.Equal(t, []string{"id", "name", "price"}, columnNamesOf(view.Columns))
	require.NotNil(t, view.Columns[1].NullableOverride)
	require.False(t, *view.Columns[1].NullableOverride)
	require.NotNil(t, view.Columns[1].GoTypeOverride)
	require.Equal(t, "Name", view.Columns[1].GoTypeOverride.Name)
}

func TestAFollowingViewTakesTheChangesOfWhatItReadsInOneStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		migrations []string
		wantViews  map[string][]string
	}{
		{
			name: "a chain of views over a table",
			migrations: []string{
				"CREATE TABLE items (id INTEGER);\nCREATE VIEW first AS SELECT * FROM items;\n" +
					"CREATE VIEW second AS SELECT * FROM first;\nCREATE VIEW third AS SELECT * FROM second;",
				"ALTER TABLE items ADD COLUMN price REAL;",
			},
			wantViews: map[string][]string{"first": {"id", "price"}, "second": {"id", "price"}, "third": {"id", "price"}},
		},
		{
			name: "a view created before the table it reads",
			migrations: []string{
				"CREATE VIEW early AS SELECT * FROM later;",
				"CREATE TABLE later (id INTEGER, name TEXT);",
			},
			wantViews: map[string][]string{"early": {"id", "name"}},
		},
		{
			name: "a view created before the view it reads",
			migrations: []string{
				"CREATE TABLE items (id INTEGER);\nCREATE VIEW outer_view AS SELECT * FROM inner_view;",
				"CREATE VIEW inner_view AS SELECT * FROM items;",
				"ALTER TABLE items ADD COLUMN price REAL;",
			},
			wantViews: map[string][]string{"inner_view": {"id", "price"}, "outer_view": {"id", "price"}},
		},
		{
			name: "a view reading a subquery and a join",
			migrations: []string{
				"CREATE TABLE items (id INTEGER);\nCREATE TABLE prices (item_id INTEGER);\n" +
					"CREATE VIEW nested AS SELECT * FROM (SELECT * FROM items) AS i JOIN prices ON prices.item_id = i.id;",
				"ALTER TABLE prices ADD COLUMN amount REAL;",
			},
			wantViews: map[string][]string{"nested": {"id", "item_id", "amount"}},
		},
		{
			name: "a view a renamed table leaves and one it reaches",
			migrations: []string{
				"CREATE TABLE items (id INTEGER);\nCREATE VIEW old_name AS SELECT * FROM items;\n" +
					"CREATE VIEW new_name AS SELECT * FROM goods;",
				"ALTER TABLE items RENAME TO goods;",
			},
			wantViews: map[string][]string{"old_name": {"id"}, "new_name": {"id"}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := replayMigrationsWith(t, sqlite.New(), testCase.migrations...)
			for view, want := range testCase.wantViews {
				require.Equal(t, want, viewColumnNames(t, catalogue, view), "the view %s", view)
			}
		})
	}
}

func TestViewsThatKeepChangingEachOtherWarnThatTheyDidNotSettle(t *testing.T) {
	t.Parallel()

	builder := newCatalogueBuilder(sqlite.New(), namespace.Default())

	diagnostics := applyMigration(t, builder, "001.sql", "CREATE VIEW a AS SELECT 1 AS seed;\n"+
		"CREATE VIEW b AS SELECT *, 2 AS more FROM a;\nDROP VIEW a;\nCREATE VIEW a AS SELECT *, 3 AS again FROM b;", 0)

	require.Len(t, diagnostics, 1)
	require.Equal(t, diag.CodeViewDerivationUnsettled, diagnostics[0].Code)
	require.Equal(t, diag.SeverityWarning, diagnostics[0].Severity)
	require.Equal(t, 4, diagnostics[0].Line)
}

func TestDerivingFollowingViewsGrowsLinearlyWithInterleavedTablesAndViews(t *testing.T) {
	t.Parallel()

	interleaved := func(pairs int) string {
		var migration strings.Builder
		for index := range pairs {
			fmt.Fprintf(&migration, "CREATE TABLE t%d (id INTEGER, name TEXT);\nCREATE VIEW v%d AS SELECT * FROM t%d;\n",
				index, index, index)
			fmt.Fprintf(&migration, "ALTER TABLE t%d ADD COLUMN note TEXT;\n", index)
		}
		return migration.String()
	}
	replay := func(migration string) func() {
		return func() {
			builder := newCatalogueBuilder(sqlite.New(), namespace.Default())
			diagnostics, err := builder.ApplyMigration(t.Context(), "001.sql", []byte(migration), 0)
			require.NoError(t, err)
			require.Empty(t, diagnostics)
		}
	}

	scaling.Linear(t, replay(interleaved(50)), replay(interleaved(200)))
}
