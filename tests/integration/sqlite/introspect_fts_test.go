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

//go:build integration

package sqlite_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect"
)

func TestIntrospectionMatchesMigrationReplayForVirtualAndWithoutRowIDTables(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tests := []struct {
		name       string
		migration  string
		wantTables []string
	}{
		{
			name:       "an FTS5 table without its shadow tables",
			migration:  "CREATE VIRTUAL TABLE docs USING fts5(title, body);",
			wantTables: []string{"docs"},
		},
		{
			name:       "an FTS5 table with a quoted name and an option",
			migration:  `CREATE VIRTUAL TABLE "Field Notes" USING FTS5(entry, tokenize = 'porter');`,
			wantTables: []string{"Field Notes"},
		},
		{
			name:       "a WITHOUT ROWID table",
			migration:  "CREATE TABLE kv (k TEXT PRIMARY KEY, v BLOB) WITHOUT ROWID;",
			wantTables: []string{"kv"},
		},
		{
			name: "an FTS5 table beside a WITHOUT ROWID table and a STRICT table",
			migration: `CREATE VIRTUAL TABLE docs USING fts5(title, body);
CREATE TABLE kv (k TEXT PRIMARY KEY, v BLOB) WITHOUT ROWID;
CREATE TABLE ledger (id INTEGER PRIMARY KEY, amount REAL NOT NULL) STRICT;`,
			wantTables: []string{"docs", "kv", "ledger"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(directory, "migrations"), 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(directory, "migrations", "001_tables.up.sql"), []byte(tt.migration), 0o600))
			database := applyMigrations(t, directory)

			replayed := replayCatalogue(t, os.DirFS(directory))
			introspected := introspectCatalogue(t, database)

			assert.Equal(t, tt.wantTables, slices.Sorted(maps.Keys(mainSchema(introspected).Tables)))
			assert.Empty(t, compareMainSchemas(replayed, introspected))
			assert.Empty(t, compareTableKinds(mainSchema(keyedByFoldedName(replayed)), mainSchema(keyedByFoldedName(introspected))))
		})
	}
}

func compareTableKinds(expected, actual *dialect.Schema) []string {
	var differences []string
	for _, name := range slices.Sorted(maps.Keys(expected.Tables)) {
		expectedTable, actualTable := expected.Tables[name], actual.Tables[name]
		if expectedTable == nil || actualTable == nil {
			continue
		}
		for _, attribute := range []struct {
			label    string
			expected any
			actual   any
		}{
			{label: "virtual", expected: expectedTable.IsVirtual, actual: actualTable.IsVirtual},
			{label: "module", expected: expectedTable.VirtualModuleName, actual: actualTable.VirtualModuleName},
			{label: "without rowid", expected: expectedTable.IsWithoutRowID, actual: actualTable.IsWithoutRowID},
			{label: "hidden columns", expected: hiddenColumnNames(expectedTable.Columns), actual: hiddenColumnNames(actualTable.Columns)},
			{label: "visible columns", expected: visibleColumnNames(expectedTable.Columns), actual: visibleColumnNames(actualTable.Columns)},
		} {
			if fmt.Sprint(attribute.expected) != fmt.Sprint(attribute.actual) {
				differences = append(differences, fmt.Sprintf("table %s: %s differs: migrations %v, database %v",
					name, attribute.label, attribute.expected, attribute.actual))
			}
		}
	}
	return differences
}

func hiddenColumnNames(columns []dialect.Column) []string {
	var names []string
	for _, column := range columns {
		if column.Hidden {
			names = append(names, column.Name)
		}
	}
	slices.Sort(names)
	return names
}

func visibleColumnNames(columns []dialect.Column) []string {
	var names []string
	for _, column := range columns {
		if !column.Hidden {
			names = append(names, column.Name)
		}
	}
	return names
}
