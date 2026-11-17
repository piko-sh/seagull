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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/testkit/fuzzruntime"
)

func TestRuntimeOracleRejectsLostPredicatesAndSwappedValues(t *testing.T) {
	info := postgres.New().Info()
	facts := info.Facts()
	want, err := fuzzruntime.SQLValues(&facts, fuzzruntime.Statement{SQL: "SELECT id FROM fuzz_items WHERE id >= 1 AND score = 2"})
	require.NoError(t, err)
	for _, broken := range []fuzzruntime.Statement{
		{SQL: "SELECT id FROM fuzz_items WHERE id >= $2 AND score = $1", Arguments: []fuzzruntime.Argument{{Values: []string{"1"}}, {Values: []string{"2"}}}},
		{SQL: "SELECT id FROM fuzz_items WHERE id >= $1", Arguments: []fuzzruntime.Argument{{Values: []string{"1"}}}},
		{SQL: "SELECT id FROM fuzz_items WHERE score >= $1 AND id = $2", Arguments: []fuzzruntime.Argument{{Values: []string{"1"}}, {Values: []string{"2"}}}},
	} {
		got, err := fuzzruntime.SQLValues(&facts, broken)
		require.NoError(t, err)
		require.False(t, slices.Equal(want, got))
	}
}

func TestRuntimeOracleRejectsMissingAndUnusedArguments(t *testing.T) {
	info := postgres.New().Info()
	facts := info.Facts()
	for _, statement := range []fuzzruntime.Statement{
		{SQL: "SELECT $1"},
		{SQL: "SELECT 1", Arguments: []fuzzruntime.Argument{{Values: []string{"2"}}}},
	} {
		_, err := fuzzruntime.SQLValues(&facts, statement)
		require.Error(t, err)
	}
}

func TestRowOracleRejectsWrongScansSwallowedErrorsAndLeaks(t *testing.T) {
	query := "SELECT id, score FROM fuzz_items"
	valid := fuzzruntime.Call{Values: []string{"6101", "6202"}}
	require.Empty(t, fuzzruntime.RowProblems([]byte{48}, query, valid))
	for _, broken := range []fuzzruntime.Call{
		{Values: []string{"6202", "6101"}},
		{Values: []string{"6101", "0"}},
		{Values: []string{"6101", "6202"}, OpenRows: 1},
		{Values: []string{"6101", "6202"}, Error: "scan failed"},
	} {
		require.NotEmpty(t, fuzzruntime.RowProblems([]byte{48}, query, broken))
	}
	require.Empty(t, fuzzruntime.RowProblems([]byte{96}, query, fuzzruntime.Call{Values: []string{"6101", "NULL"}}))
	require.NotEmpty(t, fuzzruntime.RowProblems([]byte{96}, query, valid))
	for _, mode := range []byte{144, 192} {
		require.NotEmpty(t, fuzzruntime.RowProblems([]byte{mode}, query, fuzzruntime.Call{}))
		require.Empty(t, fuzzruntime.RowProblems([]byte{mode}, query, fuzzruntime.Call{Error: "injected failure"}))
		require.NotEmpty(t, fuzzruntime.RowProblems([]byte{mode}, query, fuzzruntime.Call{Error: "injected failure", OpenRows: 1}))
	}
}

func TestRuntimeOracleRejectsStateLeakingIntoFreshBuilders(t *testing.T) {
	info := postgres.New().Info()
	facts := info.Facts()
	expected, err := fuzzruntime.SQLValues(&facts, fuzzruntime.Statement{SQL: "SELECT id, score FROM fuzz_items WHERE id >= 1001"})
	require.NoError(t, err)
	for _, leaked := range []string{" AND score IS NULL", " AND id >= 2001", " ORDER BY id DESC", " LIMIT 3", " LIMIT 9 OFFSET 2"} {
		actual, err := fuzzruntime.SQLValues(&facts, fuzzruntime.Statement{
			SQL:       "SELECT id, score FROM fuzz_items WHERE id >= $1" + leaked,
			Arguments: []fuzzruntime.Argument{{Values: []string{"1001"}}},
		})
		require.NoError(t, err)
		require.NotEqual(t, expected, actual)
	}
}

func TestOptionalRuntimeOracleRetainsPresenceBindingsAndGrouping(t *testing.T) {
	info := postgres.New().Info()
	facts := info.Facts()
	representations := map[string]bool{}
	for _, operation := range []byte{11, 23, 35, 47} {
		for _, count := range []bool{false, true} {
			expected := fuzzruntime.OptionalSQL(operation, count)
			want, err := fuzzruntime.SQLValues(&facts, fuzzruntime.Statement{SQL: expected})
			require.NoError(t, err)
			key := strings.Join(want, " ")
			require.False(t, representations[key], "optional states collapsed")
			representations[key] = true
			for _, broken := range []string{
				strings.ReplaceAll(strings.ReplaceAll(expected, "(", ""), ")", ""),
				strings.Replace(expected, "id < 7000", "id < 7001", 1),
				strings.Replace(expected, " OR ", " AND ", 1),
				strings.Replace(expected, "category =", "id =", 1),
			} {
				got, err := fuzzruntime.SQLValues(&facts, fuzzruntime.Statement{SQL: broken})
				require.NoError(t, err)
				require.NotEqual(t, want, got)
			}
		}
	}
	grouped := "SELECT id FROM fuzz_items WHERE ((id > 0 AND (score IS NULL OR score = 7))) AND id < 9"
	ungrouped := "SELECT id FROM fuzz_items WHERE id > 0 AND (score IS NULL OR score = 7) AND id < 9"
	want, err := fuzzruntime.SQLValues(&facts, fuzzruntime.Statement{SQL: grouped})
	require.NoError(t, err)
	got, err := fuzzruntime.SQLValues(&facts, fuzzruntime.Statement{SQL: ungrouped})
	require.NoError(t, err)
	require.Equal(t, want, got)
	for _, broken := range []string{"SELECT id WHERE (id > 0", "SELECT id WHERE id > 0)"} {
		_, err := fuzzruntime.SQLValues(&facts, fuzzruntime.Statement{SQL: broken})
		require.Error(t, err)
	}
}
