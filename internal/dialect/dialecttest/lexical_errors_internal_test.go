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

package dialecttest

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
)

func TestUnterminatedProceduralBlockIsAParseError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		dialect   dialect.DialectPort
		name      string
		migration string
	}{
		{name: "postgres", dialect: postgres.New(), migration: "CREATE TABLE a (id integer);\nDO BEGIN PERFORM 1;"},
		{name: "mysql", dialect: mysql.New(), migration: "CREATE TABLE a (id INT);\nCREATE PROCEDURE p() BEGIN SELECT 1;"},
		{name: "sqlite", dialect: sqlite.New(), migration: "CREATE TABLE a (id INTEGER);\nCREATE TRIGGER t AFTER INSERT ON a BEGIN DELETE FROM a;"},
		{name: "duckdb", dialect: duckdb.New(), migration: "CREATE TABLE a (id INTEGER);\nCREATE FUNCTION f() AS BEGIN SELECT 1;"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, result, err := runGenerate(t.Context(), testCase.dialect, map[string]string{"001_a.up.sql": testCase.migration}, nil)
			require.NoError(t, err)
			require.True(t, slices.ContainsFunc(result.Diagnostics, func(diagnostic diag.SourceError) bool {
				return diagnostic.Code == diag.CodeParseError &&
					diagnostic.Severity == diag.SeverityError &&
					strings.HasSuffix(diagnostic.Filename, "001_a.up.sql") &&
					diagnostic.Line == 2 &&
					strings.Contains(diagnostic.Message, "unterminated procedural block near")
			}), "expected a parse error naming the unterminated block, got %s", formatDiagnostics(result.Diagnostics))
		})
	}
}
