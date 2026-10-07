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

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestAPragmaReturnsTheColumnsSQLiteGivesIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sql     string
		columns []string
	}{
		{name: "reading a setting", sql: "PRAGMA user_version", columns: []string{"user_version"}},
		{name: "reading a setting of a schema", sql: "PRAGMA main.user_version", columns: []string{"user_version"}},
		{name: "reading the busy timeout", sql: "PRAGMA busy_timeout", columns: []string{"timeout"}},
		{name: "setting a value that echoes it", sql: "PRAGMA journal_mode = WAL", columns: []string{"journal_mode"}},
		{name: "setting the timeout, which echoes it", sql: "PRAGMA busy_timeout = 5000", columns: []string{"timeout"}},
		{name: "setting a value in parentheses that echoes it", sql: "PRAGMA journal_mode(WAL)", columns: []string{"journal_mode"}},
		{name: "describing a table", sql: "PRAGMA table_info(users)", columns: []string{"cid", "name", "type", "notnull", "dflt_value", "pk"}},
		{name: "describing a table of a schema", sql: "PRAGMA main.table_info('users')", columns: []string{"cid", "name", "type", "notnull", "dflt_value", "pk"}},
		{name: "checkpointing the log", sql: "PRAGMA wal_checkpoint(TRUNCATE)", columns: []string{"busy", "log", "checkpointed"}},
		{name: "checking integrity", sql: "PRAGMA integrity_check", columns: []string{"integrity_check"}},
		{name: "setting a value that gives nothing back", sql: "PRAGMA foreign_keys = ON"},
		{name: "setting a version that gives nothing back", sql: "PRAGMA user_version = 3"},
		{name: "setting a version in parentheses", sql: "PRAGMA user_version(4)"},
		{name: "a pragma SQLite does not have", sql: "PRAGMA no_such_pragma"},
		{name: "shrinking memory", sql: "PRAGMA shrink_memory"},
	}
	engine := New()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newSQLiteCatalogue(), test.sql)
			require.False(t, analysis.ReadOnly)
			if len(test.columns) == 0 {
				require.Empty(t, analysis.OutputColumns)
				require.Empty(t, analysis.RawTableValuedFunctions)
				return
			}
			require.Len(t, analysis.OutputColumns, 1)
			require.True(t, analysis.OutputColumns[0].IsStar)
			require.Len(t, analysis.RawTableValuedFunctions, 1)
			columns := engine.TableValuedFunctionColumns(analysis.RawTableValuedFunctions[0].FunctionName)
			names := make([]string, 0, len(columns))
			for _, column := range columns {
				names = append(names, column.Name)
			}
			require.Equal(t, test.columns, names)
		})
	}
}

func TestAPragmaReadsItsSettingsAsTheirTypes(t *testing.T) {
	t.Parallel()

	engine := New()
	tests := []struct {
		pragma   string
		category sqlmodel.SQLTypeCategory
		nullable bool
	}{
		{pragma: "user_version", category: sqlmodel.TypeCategoryInteger},
		{pragma: "journal_mode", category: sqlmodel.TypeCategoryText},
		{pragma: "foreign_keys", category: sqlmodel.TypeCategoryInteger},
		{pragma: "locking_mode", category: sqlmodel.TypeCategoryText},
	}
	for _, test := range tests {
		analysis := analyseQuery(t, newSQLiteCatalogue(), "PRAGMA "+test.pragma)
		require.Len(t, analysis.RawTableValuedFunctions, 1, test.pragma)
		columns := engine.TableValuedFunctionColumns(analysis.RawTableValuedFunctions[0].FunctionName)
		require.Len(t, columns, 1, test.pragma)
		require.Equal(t, test.category, columns[0].SQLType.Category, test.pragma)
		require.Equal(t, test.nullable, columns[0].Nullable, test.pragma)
	}
}

func TestAPragmaWhoseResultIsNotDeclaredOrThatBindsIsRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sql     string
		message string
	}{
		{sql: "PRAGMA user_version = ?", message: "a PRAGMA statement cannot bind a parameter"},
		{sql: "PRAGMA table_info(:name)", message: "a PRAGMA statement cannot bind a parameter"},
		{sql: "PRAGMA count_changes", message: "the columns PRAGMA count_changes returns are not declared"},
		{sql: "PRAGMA short_column_names", message: "the columns PRAGMA short_column_names returns are not declared"},
	}
	for _, test := range tests {
		t.Run(test.sql, func(t *testing.T) {
			t.Parallel()

			require.ErrorContains(t, analyseQueryError(t, test.sql), test.message)
		})
	}
}
