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

package fuzzruntime_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzruntime"
)

func TestPreparedOracleRejectsLeakedRepeatedAndUnusedStatements(t *testing.T) {
	valid := fuzzruntime.Response{
		Prepared: []fuzzruntime.PreparedEvent{
			{Operation: "prepare", SQL: "SELECT id, score FROM fuzz_items WHERE id >= $1"}, {Operation: "prepare", SQL: "SELECT id, score FROM fuzz_items WHERE id < $1"},
			{Operation: "use", SQL: "SELECT id, score FROM fuzz_items WHERE id >= $1"}, {Operation: "close", SQL: "SELECT id, score FROM fuzz_items WHERE id >= $1"}, {Operation: "close", SQL: "SELECT id, score FROM fuzz_items WHERE id < $1"},
		},
		Calls: []fuzzruntime.Call{{Statements: []fuzzruntime.Statement{{SQL: "SELECT id, score FROM fuzz_items WHERE id >= $1"}}}},
	}
	require.Empty(t, fuzzruntime.PreparedProblems(true, nil, valid))
	for _, index := range []int{0, 1, 2, 3, 4} {
		broken := valid
		broken.Prepared = slices.Delete(slices.Clone(valid.Prepared), index, index+1)
		require.NotEmpty(t, fuzzruntime.PreparedProblems(true, nil, broken))
	}
	for _, event := range []fuzzruntime.PreparedEvent{{Operation: "prepare", SQL: "SELECT id, score FROM fuzz_items WHERE id >= $1"}, {Operation: "close", SQL: "SELECT id, score FROM fuzz_items WHERE id >= $1"}, {Operation: "use", SQL: "SELECT id, score FROM fuzz_items WHERE id >= $1"}} {
		broken := valid
		broken.Prepared = append(slices.Clone(valid.Prepared), event)
		require.NotEmpty(t, fuzzruntime.PreparedProblems(true, nil, broken))
	}
	require.NotEmpty(t, fuzzruntime.PreparedProblems(true, nil, fuzzruntime.Response{}))
	require.NotEmpty(t, fuzzruntime.PreparedProblems(false, nil, valid))
}

func TestPreparedOracleRejectsSwallowedFailuresAndPartialLeaks(t *testing.T) {
	for _, input := range []byte{252, 253} {
		valid := fuzzruntime.Response{PreparationError: "injected preparation failure"}
		if input == 253 {
			valid.Prepared = append(valid.Prepared, fuzzruntime.PreparedEvent{Operation: "prepare", SQL: "SELECT id, score FROM fuzz_items WHERE id >= $1"})
		}
		valid.Prepared = append(valid.Prepared, fuzzruntime.PreparedEvent{Operation: "prepare-error", SQL: "SELECT id, score FROM fuzz_items WHERE id < $1"})
		if input == 253 {
			valid.Prepared = append(valid.Prepared, fuzzruntime.PreparedEvent{Operation: "close", SQL: "SELECT id, score FROM fuzz_items WHERE id >= $1"})
		}
		require.Empty(t, fuzzruntime.PreparedProblems(true, []byte{input}, valid))
		broken := valid
		broken.PreparationError = ""
		require.NotEmpty(t, fuzzruntime.PreparedProblems(true, []byte{input}, broken))
		broken = valid
		broken.Calls = []fuzzruntime.Call{{}}
		require.NotEmpty(t, fuzzruntime.PreparedProblems(true, []byte{input}, broken))
		if input == 253 {
			broken = valid
			broken.Prepared = broken.Prepared[:len(broken.Prepared)-1]
			require.NotEmpty(t, fuzzruntime.PreparedProblems(true, []byte{input}, broken))
		}
	}
}

func TestPreparedInstrumentationRejectsIncompleteRecorders(t *testing.T) {
	_, err := fuzzruntime.InstrumentPreparedRecorder("package main")
	require.Error(t, err)
	_, err = fuzzruntime.InstrumentPreparedRecorder("broken")
	require.Error(t, err)
	source, err := fuzzruntime.PopulateRecorder(fuzzruntime.Driver("databasesql").Source)
	require.NoError(t, err)
	source, err = fuzzruntime.InstrumentPreparedRecorder(source)
	require.NoError(t, err)
	require.Contains(t, source, "fuzzPrepare(query)")
	require.Contains(t, source, `fuzzPreparedEvent("close", statement.query)`)
}
