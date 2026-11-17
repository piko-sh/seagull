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
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// fatalReporter is the part of testing.TB an oracle stops a test through.
type fatalReporter interface {
	// Helper marks the caller as a helper.
	Helper()

	require.TestingT
}

// FuzzDMLSequence checks native and rendered mutations against independent row states.
//
// Takes f (*testing.F) which registers versioned state transitions.
// Takes suite (FuzzSuite) which creates isolated native databases.
func FuzzDMLSequence(f *testing.F, suite FuzzSuite) {
	f.Helper()
	for _, seed := range fuzzcase.CommandSeeds(suite.Engine) {
		f.Add(seed)
	}
	var metadata sync.Once
	f.Fuzz(func(t *testing.T, data []byte) {
		sequence, ok := fuzzcase.DecodeCommands(suite.Engine, data)
		if !ok {
			t.Skip("unsupported command envelope")
		}
		runFuzzCommands(t, suite, sequence, &metadata)
	})
}

// runFuzzCommands observes both execution paths under one bounded lifecycle.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes suite (FuzzSuite) which supplies the dialect and isolated native database.
// Takes sequence (fuzzcase.CommandSequence) which retains independently predicted command
// states.
// Takes metadata (*sync.Once) which records native engine metadata once.
//
// Panics if native work ignores the cancellation deadline.
func runFuzzCommands(t *testing.T, suite FuzzSuite, sequence fuzzcase.CommandSequence, metadata *sync.Once) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(databaseInputDeadline), errFuzzInputDeadline)
	defer cancel()
	watchdog := time.AfterFunc(scaled(databaseWatchdogDeadline), func() { panic("stateful commands ignored cancellation: " + t.Name()) })
	defer watchdog.Stop()
	details := analyseFuzzScenario(ctx, t, suite, sequence.Scenario, false)
	require.LessOrEqual(t, len(sequence.Steps), len(details.Report.Queries), "the sequence has more commands than queries")
	for _, rendered := range []bool{false, true} {
		database := suite.Open(t)
		database.SetMaxOpenConns(1)
		metadata.Do(func() { writeFuzzEngineMetadata(ctx, t, database, suite.Engine) })
		if suite.Engine == engineClickHouse {
			fuzzExec(ctx, t, database, "SET mutations_sync = 2", nil)
		}
		fuzzApply(ctx, t, database, sequence.Scenario.Schema)
		for index, step := range sequence.Steps {
			before := fuzzRows(ctx, t, database, "SELECT COUNT(*) FROM fuzz_commands WHERE "+step.Predicate, nil)
			require.True(t, len(before) == 1 && len(before[0]) == 1 && fuzzValue(before[0][0]) == strconv.Itoa(step.Matches),
				"predicate selection differs at command %d for %v: %#v", index, sequence.Scenario.Data, before)
			expected := sequence.Scenario.Queries[index]
			statement, arguments := expected.Reference, []any(nil)
			if rendered {
				statement, arguments = renderScenario(t, suite, expected, details.Report.Queries[index], details.Report.Queries[index].Template)
			}
			result := fuzzExec(ctx, t, database, statement, arguments)
			checkCommandAffected(t, suite.Engine, result, step, index)
			checkCommandRows(ctx, t, database, step, index)
		}
		require.NoError(t, database.Close())
	}
}

// checkCommandRows retains untouched rows, SQL NULL and duplicate values after each step.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes database (*sql.DB) which owns the isolated native state.
// Takes step (fuzzcase.CommandStep) which records the expected state after one command.
// Takes index (int) which identifies the current observation.
func checkCommandRows(ctx context.Context, t *testing.T, database *sql.DB, step fuzzcase.CommandStep, index int) {
	t.Helper()
	actual := fuzzRows(ctx, t, database, "SELECT id, score FROM fuzz_commands ORDER BY id", nil)
	expected := make([][]any, len(step.Rows))
	for rowIndex, row := range step.Rows {
		expected[rowIndex] = make([]any, len(row))
		for column, value := range row {
			scalar, err := value.Scalar()
			require.NoError(t, err)
			expected[rowIndex][column] = scalar
		}
	}
	require.True(t, fuzzExactResultsEqual(expected, actual, true), "state differs after command %d: expected %#v, actual %#v", index, expected, actual)
}

// checkCommandAffected compares a mutation's reported row count with the predicted count.
//
// MySQL and MariaDB count changed UPDATE rows. ClickHouse asynchronous mutations have no
// reported count to compare.
//
// Takes t (fatalReporter) which reports a wrong count.
// Takes engine (string) which selects the contract.
// Takes result (sql.Result) which is what the driver reported.
// Takes step (fuzzcase.CommandStep) which holds the predicted counts.
// Takes index (int) which identifies the command.
func checkCommandAffected(t fatalReporter, engine string, result sql.Result, step fuzzcase.CommandStep, index int) {
	t.Helper()
	if engine == engineClickHouse || result == nil {
		return
	}
	reported, err := result.RowsAffected()
	require.NoError(t, err, "command %d reports no affected rows", index)
	want := step.Affected
	if engine == engineMySQL || engine == engineMariaDB {
		want = step.Changed
	}
	require.Equal(t, int64(want), reported, "command %d reports the wrong number of affected rows", index)
}
