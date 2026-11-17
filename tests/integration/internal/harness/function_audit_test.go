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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSpecLineReadsArgumentsAndFlags(t *testing.T) {
	t.Parallel()
	function, ok := parseSpecLine("substr(value text, start integer, length integer?) never-null empty-text-nulls=0")
	require.True(t, ok)
	require.Equal(t, "substr", function.name)
	require.Equal(t, []string{"text", "integer"}, function.arguments, "an optional tail is left out")
	require.True(t, function.neverNull)
	require.Equal(t, 1, function.emptyTextNulls)
	for _, skipped := range []string{"", "# a comment", "count(value any) aggregate", "random()", "pg_catalog.lower(text)"} {
		_, ok := parseSpecLine(skipped)
		require.False(t, ok, skipped)
	}
}

func TestSplitSpecArgumentsKeepsNestedTypesWhole(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{"Array(Tuple(UInt8, String))", " x Int64"}, splitSpecArguments("Array(Tuple(UInt8, String)), x Int64"))
}

func TestCombinationsVaryTheLastArgumentFastestWithinTheLimit(t *testing.T) {
	t.Parallel()
	require.Equal(t, [][]string{{"a", "1"}, {"a", "2"}, {"b", "1"}}, combinations([][]string{{"a", "b"}, {"1", "2"}}, 3))
	require.Equal(t, [][]string{{}}, combinations(nil, 3))
}

func TestExcludeDeclaredNullsDropsOnlyTheDeclaredValues(t *testing.T) {
	t.Parallel()
	function := &specFunction{emptyTextNulls: 1, zeroNulls: 2}
	require.Equal(t, []string{"'a'"}, excludeDeclaredNulls(function, 0, []string{"''", "'a'"}))
	require.Equal(t, []string{"1"}, excludeDeclaredNulls(function, 1, []string{auditZero, "CAST(0 AS INT)", "1"}))
}
