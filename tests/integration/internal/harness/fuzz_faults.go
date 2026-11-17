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
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/emit"
)

// oracleFaultRows gives categories with both repeated and singleton values.
const oracleFaultRows = 4

// CheckFuzzCountOracle proves that count-critical SQL mutations are detected.
//
// Takes t (*testing.T) which reports an ineffective oracle.
// Takes suite (FuzzSuite) which supplies a real isolated database.
func CheckFuzzCountOracle(t *testing.T, suite FuzzSuite) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(databaseInputDeadline), errFuzzInputDeadline)
	defer cancel()
	model, ok := fuzzcase.Decode(suite.Engine, []byte{fuzzcase.Version, 0, 0, oracleFaultRows})
	require.True(t, ok, "count oracle engine has no model")
	database := suite.Open(t)
	database.SetMaxOpenConns(1)
	fuzzApply(ctx, t, database, append(model.Migrations(), model.Inserts()...))
	for _, fault := range []struct{ name, reference, changed string }{
		{name: "lost distinct", reference: "SELECT DISTINCT category FROM fuzz_items", changed: "SELECT category FROM fuzz_items"},
		{name: "lost having", reference: "SELECT category FROM fuzz_items GROUP BY category HAVING COUNT(*) > 1",
			changed: "SELECT category FROM fuzz_items GROUP BY category"},
		{name: "retained pagination", reference: "SELECT id FROM fuzz_items",
			changed: "SELECT id FROM fuzz_items ORDER BY id LIMIT 1 OFFSET 1"},
	} {
		t.Run(fault.name, func(t *testing.T) {
			expected := fuzzRows(ctx, t, database, fault.reference, nil)
			correct := fuzzRows(ctx, t, database, "SELECT COUNT(*) FROM ("+fault.reference+") AS count_oracle", nil)
			require.Empty(t, fuzzCountProblem(expected, correct))
			changed := fuzzRows(ctx, t, database, "SELECT COUNT(*) FROM ("+fault.changed+") AS count_oracle", nil)
			require.NotEmpty(t, fuzzCountProblem(expected, changed), "oracle accepted %s", fault.name)
		})
	}
}

// fuzzCountProblem compares an observed count with independent reference rows.
//
// Takes reference ([][]any) which contains rows before removable pagination.
// Takes actual ([][]any) which contains the generated count result.
//
// Returns string which describes a malformed or incorrect count.
func fuzzCountProblem(reference, actual [][]any) string {
	if len(actual) != 1 || len(actual[0]) != 1 || fuzzValue(actual[0][0]) != strconv.Itoa(len(reference)) {
		return fmt.Sprintf("count mismatch: want %d, got %#v", len(reference), actual)
	}
	return ""
}

// fuzzNullProblems rejects observed NULLs that contradict inferred outputs.
//
// Takes values ([][]any) which contains observed database values.
// Takes columns ([]emit.OutputColumn) which contains inferred output metadata.
//
// Returns []string which reports unsafe nullability or a different result width.
func fuzzNullProblems(values [][]any, columns []emit.OutputColumn) []string {
	var problems []string
	for _, row := range values {
		if len(row) != len(columns) {
			problems = append(problems, "inferred result width differs from the engine")
			continue
		}
		for index, value := range row {
			if value == nil && !columns[index].Nullable {
				problems = append(problems, fmt.Sprintf("column %s is NULL but inferred non-nullable", columns[index].Name))
			}
		}
	}
	return problems
}
