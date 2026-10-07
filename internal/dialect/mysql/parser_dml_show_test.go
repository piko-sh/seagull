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

package mysql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestShowStatementsWhoseResultIsDeclaredReadItAsATableFunction(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		function string
	}{
		{name: "warnings", sql: "SHOW WARNINGS", function: "show warnings"},
		{name: "errors with a limit", sql: "SHOW ERRORS LIMIT 5", function: "show errors"},
		{name: "the count of warnings", sql: "SHOW COUNT(*) WARNINGS", function: "show count warnings"},
		{name: "the count of errors", sql: "SHOW COUNT(*) ERRORS", function: "show count errors"},
		{name: "global variables like a pattern", sql: "SHOW GLOBAL VARIABLES LIKE 'sql_mode'", function: "show variables"},
		{name: "session status where a condition holds", sql: "SHOW SESSION STATUS WHERE Variable_name = 'Uptime'", function: "show status"},
		{name: "databases", sql: "SHOW DATABASES", function: "show databases"},
		{name: "schemas like a pattern", sql: "SHOW SCHEMAS LIKE 'app%'", function: "show databases"},
		{name: "a table's definition", sql: "SHOW CREATE TABLE items", function: "show create table"},
		{name: "tables", sql: "SHOW TABLES LIKE 'item%'", function: "show tables"},
		{name: "tables with their types", sql: "SHOW FULL TABLES", function: "show full tables"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			require.True(t, analysis.ReadOnly)
			require.Len(t, analysis.RawTableValuedFunctions, 1)
			require.Equal(t, testCase.function, analysis.RawTableValuedFunctions[0].FunctionName)
			require.Len(t, analysis.OutputColumns, 1)
			require.True(t, analysis.OutputColumns[0].IsStar)
		})
	}
}

func TestShowStatementsWhoseResultIsNotDeclaredAreRejected(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		token string
	}{
		{name: "columns", sql: "SHOW COLUMNS FROM items", token: "COLUMNS"},
		{name: "the process list", sql: "SHOW FULL PROCESSLIST", token: "FULL"},
		{name: "a parameter", sql: "SHOW VARIABLES LIKE ?", token: "?"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.sql)

			require.Nil(t, analysis)
			requireParseErrorAt(t, err, testCase.token)
		})
	}
}

func TestShowResultColumnsAreTyped(t *testing.T) {
	t.Parallel()

	columns := New().TableValuedFunctionColumns("show warnings")

	require.Len(t, columns, 3)
	require.Equal(t, "Code", columns[1].Name)
	require.Equal(t, sqlmodel.TypeCategoryInteger, columns[1].SQLType.Category)
	require.True(t, columns[1].SQLType.Unsigned)
	require.False(t, columns[1].Nullable)
	settings := New().TableValuedFunctionColumns("show variables")
	require.True(t, settings[1].Nullable)
}

func TestShowTablesFromADatabaseNamesItsColumnAfterTheDatabase(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		renamed []string
	}{
		{name: "from a database", sql: "SHOW TABLES FROM shop", renamed: []string{"Tables_in_shop"}},
		{name: "in a database with their types", sql: "SHOW FULL TABLES IN shop WHERE Table_type = 'VIEW'", renamed: []string{"Tables_in_shop"}},
		{name: "the current database", sql: "SHOW TABLES", renamed: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			var renamed []string
			for _, definition := range analysis.RawTableValuedFunctions[0].ColumnDefinitions {
				renamed = append(renamed, definition.Name)
			}
			require.Equal(t, testCase.renamed, renamed)
		})
	}
}
