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
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect"
)

// fuzzSchemaQuery observes column names, types and nullability in engine metadata.
//
// Takes engine (string) which selects the database catalogue.
//
// Returns string which reads only the isolated model table.
func fuzzSchemaQuery(engine string) string {
	switch engine {
	case engineSQLite:
		return `SELECT name, type, CASE WHEN "notnull" = 0 THEN 'YES' ELSE 'NO' END FROM pragma_table_info('fuzz_items') ORDER BY cid`
	case engineClickHouse:
		return "SELECT name, type, if(startsWith(type, 'Nullable('), 'YES', 'NO') FROM system.columns WHERE database = currentDatabase() AND table = 'fuzz_items' ORDER BY position"
	case engineMySQL, engineMariaDB:
		return "SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = database() AND table_name = 'fuzz_items' ORDER BY ordinal_position"
	default:
		return "SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'fuzz_items' ORDER BY ordinal_position"
	}
}

// checkFuzzSchema validates live column types and nullability after replay.
//
// Takes t (*testing.T) which reports differences.
// Takes database (*sql.DB) which holds the live schema.
// Takes suite (FuzzSuite) which supplies metadata parsing.
// Takes columns ([]dialect.Column) which contains replayed columns.
func checkFuzzSchema(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, columns []dialect.Column) {
	t.Helper()
	rows := fuzzRows(ctx, t, database, fuzzSchemaQuery(suite.Engine), nil)
	actual := make(map[string][]any, len(rows))
	for _, row := range rows {
		actual[fuzzValue(row[0])] = row
	}
	for index := range columns {
		column := &columns[index]
		if column.Hidden {
			continue
		}
		row, ok := actual[column.Name]
		require.True(t, ok, "database metadata lost column %s", column.Name)
		name := fuzzValue(row[1])
		checkFuzzType(t, suite, column.SQLType, name, column.Name)
		assert.Empty(t, fuzzNullableProblem(column.Name, column.Nullable, strings.EqualFold(fuzzValue(row[2]), "YES")))
	}
}

// fuzzNullableProblem compares replayed nullability with live schema metadata.
//
// Takes column (string) which identifies the mismatch.
// Takes inferred (bool) which comes from migration replay.
// Takes actual (bool) which comes from database metadata.
//
// Returns string which describes a mismatch or is empty.
func fuzzNullableProblem(column string, inferred, actual bool) string {
	if inferred != actual {
		return fmt.Sprintf("column %s nullability differs: replay %t, database %t", column, inferred, actual)
	}
	return ""
}
