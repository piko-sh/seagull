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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/emit"
)

type failureRecorder struct{ failed bool }

type reportedRows int64

func (*failureRecorder) Helper() {}

func (recorder *failureRecorder) Errorf(string, ...any) {
	recorder.failed = true
}

func (*failureRecorder) FailNow() {
	panic("stopped")
}

func (reportedRows) LastInsertId() (int64, error) { return 0, nil }

func (rows reportedRows) RowsAffected() (int64, error) { return int64(rows), nil }

func TestResultOracleDetectsOrderDuplicatesAndNulls(t *testing.T) {
	original := [][]any{{int64(1), nil}, {int64(2), int64(7)}, {int64(2), int64(7)}}
	reordered := [][]any{original[1], original[0], original[2]}
	require.False(t, fuzzResultsEqual(original, reordered, true), "ordered oracle accepted reordered rows")
	require.True(t, fuzzResultsEqual(original, reordered, false), "multiset oracle rejected equivalent rows")
	for _, broken := range [][][]any{
		original[:2],
		{{int64(1), int64(0)}, original[1], original[2]},
		{original[0], {int64(2), int64(8)}, original[2]},
	} {
		for _, ordered := range []bool{false, true} {
			require.False(t, fuzzResultsEqual(original, broken, ordered), "oracle accepted changed results %v", broken)
		}
	}
	equivalent := [][]any{{int(1), nil}, {[]byte("2"), int32(7)}, {int64(2), []byte("7")}}
	require.True(t, fuzzResultsEqual(original, equivalent, true), "oracle rejected equivalent exact integer representations")
}

func TestNullOracleRejectsUnsafeInference(t *testing.T) {
	columns := []emit.OutputColumn{{Name: "nullable", Nullable: true}, {Name: "required", Nullable: false}}
	require.Empty(t, fuzzNullProblems([][]any{{nil, int64(3)}}, columns))
	require.NotEmpty(t, fuzzNullProblems([][]any{{nil, nil}}, columns))
	require.NotEmpty(t, fuzzNullProblems([][]any{{nil}}, columns))
	columns[0].Nullable = false
	require.NotEmpty(t, fuzzNullProblems([][]any{{nil, int64(3)}}, columns))
	require.Empty(t, fuzzNullProblems([][]any{{int64(2), int64(3)}}, columns))
}

func TestResultOraclePreservesLiteralNullTextAndArbitraryBytes(t *testing.T) {
	for _, values := range [][2]any{
		{nil, "NULL"},
		{nil, []byte("NULL")},
		{nil, ""},
		{[]byte{0xff}, []byte{0xfe}},
		{[]byte{0, 1}, []byte{0, 2}},
		{"case", "CASE"},
		{"a\x00b", "ab"},
	} {
		for _, ordered := range []bool{false, true} {
			require.False(t, fuzzResultsEqual([][]any{{values[0]}}, [][]any{{values[1]}}, ordered))
		}
	}
	require.True(t, fuzzResultsEqual([][]any{{[]byte{0xff, 0, 0xfe}}}, [][]any{{[]byte{0xff, 0, 0xfe}}}, true))
	require.False(t, fuzzResultsEqual([][]any{{[]byte{0xff, 0, 0xfe}}}, [][]any{{string([]byte{0xff, 0, 0xfe})}}, true),
		"bytes and text of the same content are different storage kinds")
}

func TestCountOracleRejectsMalformedResults(t *testing.T) {
	reference := [][]any{{int64(1)}, {int64(2)}}
	require.Empty(t, fuzzCountProblem(reference, [][]any{{int64(2)}}))
	require.Empty(t, fuzzCountProblem(reference, [][]any{{[]byte("2")}}))
	for _, broken := range [][][]any{nil, {{}}, {{nil}}, {{int64(1)}}, {{int64(2), int64(2)}}, {{int64(2)}, {int64(2)}}} {
		require.NotEmpty(t, fuzzCountProblem(reference, broken))
	}
}

func TestSQLiteStorageOracleRejectsAffinitySubstitutionAndMissingRows(t *testing.T) {
	expected := []string{"integer", "text", "blob", "null", "integer"}
	correct := [][]any{{int64(1), "integer"}, {int64(2), "text"}, {int64(3), "blob"}, {int64(4), "null"}, {int64(5), "integer"}}
	require.Empty(t, fuzzSQLiteStorageProblems(expected, correct))
	for index := range correct {
		changed := slices.Clone(correct)
		changed[index] = []any{int64(index + 1), "INTEGER"}
		require.NotEmpty(t, fuzzSQLiteStorageProblems(expected, changed))
	}
	require.NotEmpty(t, fuzzSQLiteStorageProblems(expected, correct[:4]))
	require.NotEmpty(t, fuzzSQLiteStorageProblems(expected, [][]any{{int64(1)}}))
	require.NotEmpty(t, fuzzSQLiteStorageProblems(expected, [][]any{correct[1], correct[0], correct[2], correct[3], correct[4]}))
}

func TestExactResultOracleRejectsChangedStorageKinds(t *testing.T) {
	for _, values := range [][2]any{{int64(2), "2"}, {int64(2), []byte("x")}, {"x", []byte("x")}, {float64(2), int64(2)}, {nil, "NULL"}, {[]byte{255}, []byte{254}}, {float64(2.5), []byte("2")}} {
		for _, ordered := range []bool{false, true} {
			require.False(t, fuzzExactResultsEqual([][]any{{values[0]}}, [][]any{{values[1]}}, ordered))
		}
	}
	require.True(t, fuzzExactResultsEqual([][]any{{int32(2)}}, [][]any{{int64(2)}}, true))
	require.True(t, fuzzExactResultsEqual([][]any{{int64(2)}}, [][]any{{[]byte("2")}}, true),
		"a MySQL-family text protocol returns a number as bytes that spell it")
	require.True(t, fuzzExactResultsEqual([][]any{{[]byte{255, 0, 254}}}, [][]any{{[]byte{255, 0, 254}}}, true))
}

func TestCommandAffectedOracleRejectsAWrongCount(t *testing.T) {
	t.Parallel()
	step := fuzzcase.CommandStep{Affected: 3, Changed: 2}
	for _, test := range []struct {
		name     string
		engine   string
		reported reportedRows
		fails    bool
	}{
		{name: "matched rows on PostgreSQL", engine: "postgres", reported: 3},
		{name: "a dropped row on PostgreSQL", engine: "postgres", reported: 2, fails: true},
		{name: "changed rows on MySQL", engine: "mysql", reported: 2},
		{name: "matched rather than changed rows on MySQL", engine: "mysql", reported: 3, fails: true},
		{name: "no count from ClickHouse mutations", engine: "clickhouse", reported: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			recorder := &failureRecorder{}
			func() {
				defer func() { _ = recover() }()
				checkCommandAffected(recorder, test.engine, test.reported, step, 0)
			}()
			require.Equal(t, test.fails, recorder.failed)
		})
	}
}
