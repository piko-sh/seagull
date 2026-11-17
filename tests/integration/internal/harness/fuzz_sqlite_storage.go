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
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/emit"
)

// checkFuzzSQLiteStorage separates declared affinity from observed row storage classes.
//
// Takes t (*testing.T) which reports inconsistent observations.
// Takes database (*sql.DB) which owns the independently populated fixture.
// Takes suite (FuzzSuite) which supplies SQLite type normalisation.
// Takes model (fuzzcase.Model) which supplies expected storage classes.
// Takes query (*emit.AnalysedQuery) which supplies declared output types.
// Takes metadata ([]*sql.ColumnType) which supplies live declared column types.
func checkFuzzSQLiteStorage(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, model fuzzcase.Model, query *emit.AnalysedQuery, metadata []*sql.ColumnType) {
	t.Helper()
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		checkFuzzType(t, suite, column.SQLType, metadata[index].DatabaseTypeName(), column.Name)
	}
	rows := fuzzRows(ctx, t, database, "SELECT id, typeof(flexible) FROM fuzz_values ORDER BY id", nil)
	for _, problem := range fuzzSQLiteStorageProblems(model.SQLiteStorageClasses(), rows) {
		assert.Fail(t, problem)
	}
}

// fuzzSQLiteStorageProblems compares every row's independent storage expectation.
//
// Takes expected ([]string) which comes from the bounded insertion model.
// Takes observed ([][]any) which holds live row identities and storage classes.
//
// Returns []string which describes changed, missing or reordered observations.
func fuzzSQLiteStorageProblems(expected []string, observed [][]any) []string {
	if len(expected) != len(observed) {
		return []string{fmt.Sprintf("SQLite storage has %d rows, expected %d", len(observed), len(expected))}
	}
	var problems []string
	for index, class := range expected {
		row := observed[index]
		if len(row) != 2 || fuzzValue(row[0]) != strconv.Itoa(index+1) || fuzzValue(row[1]) != class {
			problems = append(problems, fmt.Sprintf("SQLite row %d has storage %v, expected %s", index+1, row, class))
		}
	}
	return problems
}
