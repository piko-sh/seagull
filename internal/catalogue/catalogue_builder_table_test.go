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
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestAKeyReplacingAHiddenKeyDropsTheHiddenColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		replaces    bool
		hidden      bool
		newKey      []string
		wantColumns []string
	}{
		{name: "a hidden key replaced", replaces: true, hidden: true, newKey: []string{"a"}, wantColumns: []string{"a"}},
		{name: "a hidden key kept in the new key", replaces: true, hidden: true, newKey: []string{"rowid", "a"},
			wantColumns: []string{"a", "rowid"}},
		{name: "a visible key replaced", replaces: true, hidden: false, newKey: []string{"a"}, wantColumns: []string{"a", "rowid"}},
		{name: "a key the database did not supply", replaces: false, hidden: true, newKey: []string{"a"},
			wantColumns: []string{"a", "rowid"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := newCatalogueBuilder(postgres.New(), namespace.Default())
			rowID := sqlmodel.NewColumn("rowid", sqlmodel.NewIntegerType("int8", 64, false), false)
			rowID.Hidden = testCase.hidden
			create := ast.NewCatalogueMutation(ast.MutationCreateTable, "", "events",
				ast.WithColumns([]sqlmodel.Column{sqlmodel.NewColumn("a", sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), false), rowID}),
				ast.WithPrimaryKey([]string{"rowid"}))
			require.NoError(t, builder.applyMutation(t.Context(), create))
			addKey := ast.NewCatalogueMutation(ast.MutationAlterTableAddConstraint, "", "events", ast.WithPrimaryKey(testCase.newKey))
			addKey.ReplacesHiddenKey = testCase.replaces

			require.NoError(t, builder.applyMutation(t.Context(), addKey))

			table := builder.Catalogue().LookupTable("", "events")
			require.Equal(t, testCase.wantColumns, columnNamesOf(table.Columns))
			require.Equal(t, testCase.newKey, table.PrimaryKey)
		})
	}
}

func TestASequenceMutationChangesOnlyASequence(t *testing.T) {
	t.Parallel()

	sequenceMutation := func(kind ast.MutationKind, name, newName string, ifExists bool) *ast.CatalogueMutation {
		return ast.NewCatalogueMutation(kind, "", name, ast.WithNewName(newName), ast.WithIfExists(ifExists),
			ast.WithTargetKind(ast.ObjectKindSequence))
	}
	testCases := []struct {
		name          string
		mutation      *ast.CatalogueMutation
		wantErr       error
		wantTables    []string
		wantSequences map[string][]string
	}{
		{
			name:          "a rename naming a table",
			mutation:      sequenceMutation(ast.MutationAlterTableRenameTable, "items", "goods", false),
			wantErr:       errUnknownObject,
			wantTables:    []string{"items"},
			wantSequences: map[string][]string{"public": {"counter", "spare"}, "archive": nil},
		},
		{
			name:          "a rename naming a sequence",
			mutation:      sequenceMutation(ast.MutationAlterTableRenameTable, "counter", "tally", false),
			wantErr:       nil,
			wantTables:    []string{"items"},
			wantSequences: map[string][]string{"public": {"spare", "tally"}, "archive": nil},
		},
		{
			name:          "a rename onto another sequence",
			mutation:      sequenceMutation(ast.MutationAlterTableRenameTable, "counter", "spare", false),
			wantErr:       errDuplicateObject,
			wantTables:    []string{"items"},
			wantSequences: map[string][]string{"public": {"counter", "spare"}, "archive": nil},
		},
		{
			name:          "a move naming a table",
			mutation:      sequenceMutation(ast.MutationAlterTableSetSchema, "items", "archive", false),
			wantErr:       errUnknownObject,
			wantTables:    []string{"items"},
			wantSequences: map[string][]string{"public": {"counter", "spare"}, "archive": nil},
		},
		{
			name:          "a move naming a sequence",
			mutation:      sequenceMutation(ast.MutationAlterTableSetSchema, "counter", "archive", false),
			wantErr:       nil,
			wantTables:    []string{"items"},
			wantSequences: map[string][]string{"public": {"spare"}, "archive": {"counter"}},
		},
		{
			name:          "a rename written IF EXISTS naming a table",
			mutation:      sequenceMutation(ast.MutationAlterTableRenameTable, "items", "goods", true),
			wantErr:       nil,
			wantTables:    []string{"items"},
			wantSequences: map[string][]string{"public": {"counter", "spare"}, "archive": nil},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := newCatalogueBuilder(postgres.New(), namespace.Default())
			require.Empty(t, applyMigration(t, builder, "001.sql",
				"CREATE SCHEMA archive;\nCREATE TABLE items (id int);\nCREATE SEQUENCE counter;\nCREATE SEQUENCE spare;", 0))

			err := builder.applyMutation(t.Context(), testCase.mutation)

			if testCase.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, testCase.wantErr)
			}
			catalogue := builder.Catalogue()
			require.Equal(t, testCase.wantTables, schemaTableNames(catalogue, "public"))
			for schemaName, wantSequences := range testCase.wantSequences {
				require.Equal(t, wantSequences, schemaSequenceNames(catalogue, schemaName), "the sequences of %s", schemaName)
			}
		})
	}
}

func TestAlterSequenceNamingATableReportsAMissingSequence(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		statement string
	}{
		{name: "a rename", statement: "ALTER SEQUENCE items RENAME TO goods;"},
		{name: "a move", statement: "ALTER SEQUENCE items SET SCHEMA archive;"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t,
				"CREATE SCHEMA archive;\nCREATE TABLE items (id int);\n"+testCase.statement)

			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeUnknownObject, diagnostics[0].Code)
			require.Equal(t, 3, diagnostics[0].Line)
			require.Contains(t, diagnostics[0].Message, "sequence items not found")
			require.Equal(t, []string{"items"}, schemaTableNames(catalogue, "public"))
			require.Empty(t, schemaTableNames(catalogue, "archive"))
		})
	}
}

func TestRenamingOrMovingATableOntoATakenNameIsRefused(t *testing.T) {
	t.Parallel()

	const setup = "CREATE SCHEMA archive;\nCREATE TABLE a (x int, y int);\nCREATE TABLE b (id int);\n" +
		"CREATE TABLE archive.c (id int);\nCREATE TABLE c (id int);\nCREATE VIEW v AS SELECT id FROM b;\n" +
		"CREATE SEQUENCE s1;\nCREATE SEQUENCE s2;\n"
	testCases := []struct {
		name        string
		statement   string
		wantCode    string
		wantMessage string
	}{
		{name: "a table renamed onto a table", statement: "ALTER TABLE a RENAME TO b;", wantCode: diag.CodeDuplicateObject,
			wantMessage: "relation public.b already exists"},
		{name: "a table renamed onto a view", statement: "ALTER TABLE a RENAME TO v;", wantCode: diag.CodeDuplicateObject,
			wantMessage: "relation public.v already exists"},
		{name: "a table moved onto a table", statement: "ALTER TABLE c SET SCHEMA archive;", wantCode: diag.CodeDuplicateObject,
			wantMessage: "relation archive.c already exists"},
		{name: "a sequence renamed onto a sequence", statement: "ALTER SEQUENCE s1 RENAME TO s2;",
			wantCode: diag.CodeDuplicateObject, wantMessage: "sequence public.s2 already exists"},
		{name: "a column renamed onto a column", statement: "ALTER TABLE a RENAME COLUMN x TO y;",
			wantCode: diag.CodeDuplicateObject, wantMessage: "column y already exists in table a"},
		{name: "a missing table renamed", statement: "ALTER TABLE missing RENAME TO d;", wantCode: diag.CodeUnknownObject,
			wantMessage: "table missing not found"},
		{name: "a missing column renamed", statement: "ALTER TABLE a RENAME COLUMN z TO w;", wantCode: diag.CodeUnknownObject,
			wantMessage: "column z not found in table a"},
		{name: "a column added twice", statement: "ALTER TABLE a ADD COLUMN x int;", wantCode: diag.CodeDuplicateObject,
			wantMessage: "column x already exists in table a"},
		{name: "a table created twice", statement: "CREATE TABLE b (id int);", wantCode: diag.CodeDuplicateObject,
			wantMessage: "table public.b already exists"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, setup+testCase.statement)

			require.Len(t, diagnostics, 1)
			require.Equal(t, testCase.wantCode, diagnostics[0].Code)
			require.Equal(t, 9, diagnostics[0].Line)
			require.Contains(t, diagnostics[0].Message, testCase.wantMessage)
			require.Equal(t, []string{"a", "b", "c"}, schemaTableNames(catalogue, "public"))
			require.Equal(t, []string{"c"}, schemaTableNames(catalogue, "archive"))
			require.Equal(t, []string{"x", "y"}, columnNamesOf(catalogue.LookupTable("public", "a").Columns))
			require.Equal(t, []string{"s1", "s2"}, schemaSequenceNames(catalogue, "public"))
		})
	}
}

func TestRenamingATableToItsOwnNameKeepsIt(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t, "CREATE TABLE a (x int);\nALTER TABLE a RENAME COLUMN x TO x;")

	require.Empty(t, diagnostics)
	require.Equal(t, []string{"x"}, columnNamesOf(catalogue.LookupTable("", "a").Columns))
}

func schemaSequenceNames(catalogue *sqlmodel.Catalogue, schemaName string) []string {
	schema := catalogue.Schemas[schemaName]
	if schema == nil || len(schema.Sequences) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(schema.Sequences))
}

func TestATableCreatedFromAQueryTakesItsColumnsWithoutTheirSettings(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t,
		"CREATE TABLE users (id bigint NOT NULL, name text NOT NULL);\n"+
			"-- sgl.column(users.name, go_type: example.com/names.Name)",
		"CREATE TABLE copied AS SELECT id, name, upper(name) AS loud FROM users;")

	require.Empty(t, diagnosticMessages(diagnostics))
	copied := catalogue.LookupTable("", "copied")
	require.NotNil(t, copied)
	require.Equal(t, []string{"id", "name", "loud"}, columnNamesOf(copied.Columns))
	for index := range copied.Columns {
		column := &copied.Columns[index]
		require.True(t, column.Nullable, column.Name)
		require.Nil(t, column.GoTypeOverride, column.Name)
		require.Nil(t, column.NullableOverride, column.Name)
	}
	require.Equal(t, sqlmodel.TypeCategoryInteger, copied.Columns[0].SQLType.Category)
}

func TestACommentIsRecordedOnTheRelationOrColumnItNames(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t,
		"CREATE TABLE users (id bigint, name text);\nCREATE VIEW names AS SELECT name FROM users;",
		"COMMENT ON TABLE users IS 'people';\nCOMMENT ON COLUMN users.name IS 'what they are called';\n"+
			"COMMENT ON VIEW names IS 'just names';\nCOMMENT ON COLUMN names.name IS 'the name';\n"+
			"COMMENT ON TABLE missing IS 'nothing';\nCOMMENT ON COLUMN users.missing IS 'nothing';")

	require.Empty(t, diagnosticMessages(diagnostics))
	users := catalogue.LookupTable("", "users")
	names := catalogue.LookupView("", "names")
	testCases := []struct {
		name string
		got  string
		want string
	}{
		{name: "a table", got: users.Comment, want: "people"},
		{name: "a table's column", got: users.Columns[1].Comment, want: "what they are called"},
		{name: "a view", got: names.Comment, want: "just names"},
		{name: "a view's column", got: names.Columns[0].Comment, want: "the name"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, testCase.got)
		})
	}
}

func TestDroppingAColumnThatIsMissingIsReported(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		statement   string
		wantCode    string
		wantColumns []string
	}{
		{name: "a missing column", statement: "ALTER TABLE items DROP COLUMN missing;", wantCode: diag.CodeUnknownObject,
			wantColumns: []string{"id", "name"}},
		{name: "a missing column written IF EXISTS", statement: "ALTER TABLE items DROP COLUMN IF EXISTS missing;",
			wantColumns: []string{"id", "name"}},
		{name: "a column that is there", statement: "ALTER TABLE items DROP COLUMN name;", wantColumns: []string{"id"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, "CREATE TABLE items (id int, name text);\n"+testCase.statement)

			if testCase.wantCode == "" {
				require.Empty(t, diagnostics)
			} else {
				require.Len(t, diagnostics, 1)
				require.Equal(t, testCase.wantCode, diagnostics[0].Code)
				require.Equal(t, 2, diagnostics[0].Line)
				require.Contains(t, diagnostics[0].Message, "column missing not found in table items")
			}
			var columns []string
			for _, column := range catalogue.Schemas["public"].Tables["items"].Columns {
				columns = append(columns, column.Name)
			}
			require.Equal(t, testCase.wantColumns, columns)
		})
	}
}

func TestDroppingAMissingObjectIsReportedUnlessTheStatementSaysIfExists(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		statement   string
		wantMessage string
	}{
		{name: "a missing table", statement: "DROP TABLE missing;", wantMessage: "table missing not found"},
		{name: "a missing table written IF EXISTS", statement: "DROP TABLE IF EXISTS missing;"},
		{name: "a missing sequence", statement: "DROP SEQUENCE missing;", wantMessage: "sequence missing not found"},
		{name: "a missing sequence written IF EXISTS", statement: "DROP SEQUENCE IF EXISTS missing;"},
		{name: "a missing view", statement: "DROP VIEW missing;", wantMessage: "view missing not found"},
		{name: "a missing view written IF EXISTS", statement: "DROP VIEW IF EXISTS missing;"},
		{name: "a missing type", statement: "DROP TYPE missing;", wantMessage: "type missing not found"},
		{name: "a missing type written IF EXISTS", statement: "DROP TYPE IF EXISTS missing;"},
		{name: "a missing function", statement: "DROP FUNCTION missing();", wantMessage: "function missing not found"},
		{name: "a missing function written IF EXISTS", statement: "DROP FUNCTION IF EXISTS missing();"},
		{name: "an overload taking an enum", statement: "DROP FUNCTION cheer(mood);"},
		{name: "a table that is there", statement: "DROP TABLE items;"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, diagnostics := replayPostgresMigrations(t, "CREATE TABLE items (id int);\n"+
				"CREATE TYPE mood AS ENUM ('happy');\n"+
				"CREATE FUNCTION cheer(m mood) RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;\n"+testCase.statement)

			if testCase.wantMessage == "" {
				require.Empty(t, diagnostics)
				return
			}
			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeUnknownObject, diagnostics[0].Code)
			require.Equal(t, diag.SeverityError, diagnostics[0].Severity)
			require.Equal(t, 4, diagnostics[0].Line)
			require.Contains(t, diagnostics[0].Message, testCase.wantMessage)
		})
	}
}

func TestCreateIndexOnAMissingRelationIsRefused(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		engine    dialect.DialectPort
		name      string
		migration string
	}{
		{
			name:   "postgres, indexing the table under another dialect's name",
			engine: postgres.New(),
			migration: "CREATE TABLE orchestrator_tasks (id text, execute_at bigint, status text);\n" +
				"CREATE INDEX IF NOT EXISTS idx_tasks_pending_dispatch ON tasks (execute_at, id) WHERE status IN ('PENDING');",
		},
		{
			name:      "sqlite",
			engine:    sqlite.New(),
			migration: "CREATE TABLE orchestrator_tasks (id text, execute_at integer);\nCREATE INDEX idx_tasks ON tasks (execute_at);",
		},
		{
			name:      "duckdb",
			engine:    duckdb.New(),
			migration: "CREATE TABLE orchestrator_tasks (id text, execute_at bigint);\nCREATE INDEX idx_tasks ON tasks (execute_at);",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := newCatalogueBuilder(testCase.engine, namespace.Default())
			diagnostics := applyMigration(t, builder, "migration.sql", testCase.migration, 0)

			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeUnknownObject, diagnostics[0].Code)
			require.Equal(t, 2, diagnostics[0].Line)
			require.Contains(t, diagnostics[0].Message, "tasks not found")
			require.NotNil(t, builder.Catalogue().LookupTable("", "orchestrator_tasks"), "the table itself is still built")
		})
	}
}

func TestCreateIndexOnAMaterialisedViewIsAccepted(t *testing.T) {
	t.Parallel()

	_, diagnostics := replayPostgresMigrations(t,
		"CREATE TABLE orders (id int, total numeric);\n"+
			"CREATE MATERIALIZED VIEW order_totals AS SELECT id, total FROM orders;\n"+
			"CREATE UNIQUE INDEX idx_order_totals_id ON order_totals (id);")

	require.Empty(t, diagnostics)
}
