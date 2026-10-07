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
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func nullabilityOf(columns []sqlmodel.Column) []bool {
	nullable := make([]bool, 0, len(columns))
	for index := range columns {
		nullable = append(nullable, columns[index].Nullable)
	}
	return nullable
}

func TestInheritedAndCopiedColumnsReplayAsPostgresBuildsThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		table        string
		migrations   []string
		wantColumns  []string
		wantNullable []bool
	}{
		{
			name:  "the parent's columns come first and a redeclared column merges into them",
			table: "child",
			migrations: []string{
				"CREATE TABLE parent (id bigint NOT NULL, created_at timestamptz NOT NULL);",
				"CREATE TABLE child (extra text, id bigint NOT NULL) INHERITS (parent);",
			},
			wantColumns:  []string{"id", "created_at", "extra"},
			wantNullable: []bool{false, false, true},
		},
		{
			name:  "a column several parents share appears once",
			table: "example_c",
			migrations: []string{
				"CREATE TABLE example_a (id integer NOT NULL, name varchar(32));",
				"CREATE TABLE example_b (extra int) INHERITS (example_a);",
				"CREATE TABLE example_c () INHERITS (example_a, example_b);",
			},
			wantColumns:  []string{"id", "name", "extra"},
			wantNullable: []bool{false, true, true},
		},
		{
			name:  "a LIKE of the parent merges into what INHERITS gives",
			table: "lookup_risk_type",
			migrations: []string{
				"CREATE TABLE lookup (key text NOT NULL, parent text, PRIMARY KEY (key));",
				"CREATE TABLE lookup_risk_type (LIKE lookup INCLUDING ALL) INHERITS (lookup);",
			},
			wantColumns:  []string{"key", "parent"},
			wantNullable: []bool{false, true},
		},
		{
			name:  "a LIKE copies its columns where it stands",
			table: "copy",
			migrations: []string{
				"CREATE TABLE source (a int NOT NULL, b text);",
				"CREATE TABLE copy (id int, LIKE source, extra int);",
			},
			wantColumns:  []string{"id", "a", "b", "extra"},
			wantNullable: []bool{true, false, true, true},
		},
		{
			name:  "a LIKE takes no later change to its source",
			table: "copy",
			migrations: []string{
				"CREATE TABLE source (a int NOT NULL);",
				"CREATE TABLE copy (LIKE source);",
				"ALTER TABLE source ADD COLUMN b text;",
			},
			wantColumns:  []string{"a"},
			wantNullable: []bool{false},
		},
		{
			name:  "a column added to the parent reaches the child and the grandchild",
			table: "grandchild",
			migrations: []string{
				"CREATE TABLE parent (id int PRIMARY KEY);",
				"CREATE TABLE child (extra text) INHERITS (parent);",
				"CREATE TABLE grandchild (more text) INHERITS (child);",
				"ALTER TABLE parent ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();",
			},
			wantColumns:  []string{"id", "extra", "more", "created_at"},
			wantNullable: []bool{false, true, true, false},
		},
		{
			name:  "a column dropped from the parent leaves the child",
			table: "child",
			migrations: []string{
				"CREATE TABLE parent (id int, legacy text);",
				"CREATE TABLE child (extra text) INHERITS (parent);",
				"ALTER TABLE parent DROP COLUMN legacy;",
			},
			wantColumns:  []string{"id", "extra"},
			wantNullable: []bool{true, true},
		},
		{
			name:  "a column the child declares itself survives the parent's drop",
			table: "child",
			migrations: []string{
				"CREATE TABLE parent (id int, legacy text);",
				"CREATE TABLE child (legacy text) INHERITS (parent);",
				"ALTER TABLE parent DROP COLUMN legacy;",
			},
			wantColumns:  []string{"id", "legacy"},
			wantNullable: []bool{true, true},
		},
		{
			name:  "a drop from ONLY the parent leaves the child its column",
			table: "child",
			migrations: []string{
				"CREATE TABLE parent (id int, legacy text);",
				"CREATE TABLE child (extra text) INHERITS (parent);",
				"ALTER TABLE ONLY parent DROP COLUMN legacy;",
			},
			wantColumns:  []string{"id", "legacy", "extra"},
			wantNullable: []bool{true, true, true},
		},
		{
			name:  "a rename, a type change and SET NOT NULL on the parent reach the child",
			table: "child",
			migrations: []string{
				"CREATE TABLE parent (id int, label text);",
				"CREATE TABLE child (extra text) INHERITS (parent);",
				"ALTER TABLE parent RENAME COLUMN label TO title;",
				"ALTER TABLE parent ALTER COLUMN id TYPE bigint;",
				"ALTER TABLE parent ALTER COLUMN title SET NOT NULL;",
			},
			wantColumns:  []string{"id", "title", "extra"},
			wantNullable: []bool{true, false, true},
		},
		{
			name:  "a table-level key on an inherited column makes it NOT NULL",
			table: "child",
			migrations: []string{
				"CREATE TABLE parent (id int, label text);",
				"CREATE TABLE child (extra text, PRIMARY KEY (id)) INHERITS (parent);",
			},
			wantColumns:  []string{"id", "label", "extra"},
			wantNullable: []bool{false, true, true},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, diagnostics := replayPostgresMigrations(t, testCase.migrations...)
			require.Empty(t, diagnosticMessages(diagnostics))
			table := catalogue.LookupTable("", testCase.table)
			require.NotNil(t, table)
			require.Equal(t, testCase.wantColumns, columnNamesOf(table.Columns))
			require.Equal(t, testCase.wantNullable, nullabilityOf(table.Columns))
		})
	}
}

func TestDroppingAParentDropsTheTablesThatInheritFromIt(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t,
		"CREATE TABLE parent (id int);",
		"CREATE TABLE child (extra text) INHERITS (parent);",
		"CREATE TABLE grandchild () INHERITS (child);",
		"CREATE TABLE unrelated (id int);",
		"DROP TABLE parent CASCADE;",
		"CREATE TABLE parent (id int);",
		"ALTER TABLE parent ADD COLUMN later text;",
	)
	require.Empty(t, diagnosticMessages(diagnostics))
	require.Equal(t, []string{"parent", "unrelated"}, schemaTableNames(catalogue, "public"))
	require.Equal(t, []string{"id", "later"}, columnNamesOf(catalogue.LookupTable("", "parent").Columns))
}

func TestAChangeReachesEveryTableOfADiamondOnceAndQuickly(t *testing.T) {
	t.Parallel()

	const levels = 40
	diamond := diamondInheritance(levels)
	bottom := fmt.Sprintf("a%d", levels)
	testCases := []struct {
		name        string
		change      string
		wantColumns []string
		wantTables  int
	}{
		{name: "a column added", change: "ALTER TABLE root ADD COLUMN note text;", wantColumns: []string{"id", "label", "note"},
			wantTables: 2*levels + 1},
		{name: "a column renamed", change: "ALTER TABLE root RENAME COLUMN label TO title;", wantColumns: []string{"id", "title"},
			wantTables: 2*levels + 1},
		{name: "a column's type changed", change: "ALTER TABLE root ALTER COLUMN id TYPE bigint;", wantColumns: []string{"id", "label"},
			wantTables: 2*levels + 1},
		{name: "a column dropped", change: "ALTER TABLE root DROP COLUMN label;", wantColumns: []string{"id"},
			wantTables: 2*levels + 1},
		{name: "the root dropped", change: "DROP TABLE root CASCADE;", wantColumns: nil, wantTables: 0},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := newCatalogueBuilder(postgres.New(), namespace.Default())
			require.Empty(t, applyMigration(t, builder, "001.sql", diamond, 0))

			var diagnostics []diag.SourceError
			scaling.Within(t, 100*time.Millisecond, func(context.Context) {
				diagnostics = applyMigration(t, builder, "002.sql", testCase.change, 1)
			})

			require.Empty(t, diagnosticMessages(diagnostics))
			catalogue := builder.Catalogue()
			require.Len(t, catalogue.Schemas["public"].Tables, testCase.wantTables)
			if testCase.wantColumns != nil {
				require.Equal(t, testCase.wantColumns, columnNamesOf(catalogue.LookupTable("", bottom).Columns))
			}
		})
	}
}

func TestADroppedColumnStaysWhereAnotherPathStillInheritsIt(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t,
		"CREATE TABLE root (id int, legacy text);",
		"CREATE TABLE keeper (legacy text) INHERITS (root);",
		"CREATE TABLE plain () INHERITS (root);",
		"CREATE TABLE deeper () INHERITS (plain);",
		"CREATE TABLE joined () INHERITS (deeper, keeper);",
		"CREATE TABLE lost () INHERITS (deeper);",
		"ALTER TABLE root DROP COLUMN legacy;",
	)

	require.Empty(t, diagnosticMessages(diagnostics))
	testCases := []struct {
		table       string
		wantColumns []string
	}{
		{table: "keeper", wantColumns: []string{"id", "legacy"}},
		{table: "plain", wantColumns: []string{"id"}},
		{table: "deeper", wantColumns: []string{"id"}},
		{table: "joined", wantColumns: []string{"id", "legacy"}},
		{table: "lost", wantColumns: []string{"id"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.table, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.wantColumns, columnNamesOf(catalogue.LookupTable("", testCase.table).Columns))
		})
	}
}

func TestAChangeStopsAtTheInheritanceDepthLimitWithAWarning(t *testing.T) {
	t.Parallel()

	var chain strings.Builder
	chain.WriteString("CREATE TABLE level0 (id int);\n")
	for level := 1; level <= maxInheritanceDepth+1; level++ {
		fmt.Fprintf(&chain, "CREATE TABLE level%d () INHERITS (level%d);\n", level, level-1)
	}
	builder := newCatalogueBuilder(postgres.New(), namespace.Default())
	require.Empty(t, applyMigration(t, builder, "001.sql", chain.String(), 0))

	diagnostics := applyMigration(t, builder, "002.sql", "ALTER TABLE level0 ADD COLUMN note text;", 1)

	require.Len(t, diagnostics, 1)
	require.Equal(t, diag.CodeDepthLimit, diagnostics[0].Code)
	require.Equal(t, diag.SeverityWarning, diagnostics[0].Severity)
	require.Contains(t, diagnostics[0].Message, "table level0")
	catalogue := builder.Catalogue()
	require.Equal(t, []string{"id", "note"}, columnNamesOf(catalogue.LookupTable("", fmt.Sprintf("level%d", maxInheritanceDepth)).Columns))
	require.Equal(t, []string{"id"}, columnNamesOf(catalogue.LookupTable("", fmt.Sprintf("level%d", maxInheritanceDepth+1)).Columns))
}

func diamondInheritance(levels int) string {
	var migration strings.Builder
	migration.WriteString("CREATE TABLE root (id int, label text);\n")
	migration.WriteString("CREATE TABLE a1 () INHERITS (root);\nCREATE TABLE b1 () INHERITS (root);\n")
	for level := 2; level <= levels; level++ {
		fmt.Fprintf(&migration, "CREATE TABLE a%d () INHERITS (a%d, b%d);\n", level, level-1, level-1)
		fmt.Fprintf(&migration, "CREATE TABLE b%d () INHERITS (a%d, b%d);\n", level, level-1, level-1)
	}
	return migration.String()
}
