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

package fuzzcase

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var grammarEngines = []string{"postgres", "sqlite", "mysql", "mariadb", "duckdb", "clickhouse", "cockroachdb", "timescaledb"}

func TestQueryGrammarSeedsReachEveryAdmittedFeature(t *testing.T) {
	for _, engine := range grammarEngines {
		covered := make(map[string]bool)
		for _, seed := range QueryGrammarSeeds(engine) {
			require.LessOrEqual(t, len(seed), MaxBytes, "%s seed exceeds MaxBytes", engine)
			scenario, ok := DecodeScenario(engine, seed)
			require.True(t, ok, "%s rejected its own seed %v", engine, seed)
			require.Len(t, scenario.Queries, 1, "%s rejected its own seed %v", engine, seed)
			query := scenario.Queries[0]
			require.NotZero(t, query.Columns, "%s seed %v has no columns", engine, seed)
			require.Len(t, query.MayBeNull, query.Columns, "%s seed %v has the wrong number of nullability claims", engine, seed)
			require.True(t, strings.HasPrefix(query.SQL, "-- sgl.query(Grammar, many)\n") && strings.HasSuffix(query.SQL, query.Reference+";"),
				"%s seed %v lost its directive or reference: %s", engine, seed, query.SQL)
			for _, feature := range scenario.Features {
				assert.True(t, queryGrammarSupports(engine, feature), "%s seed %v uses excluded feature %s", engine, seed, feature)
				covered[feature] = true
			}
		}
		for _, feature := range QueryGrammarFeatures(engine) {
			assert.True(t, covered[feature], "%s has no seed for %s", engine, feature)
		}
	}
}

func TestQueryGrammarExclusionsCarryReasons(t *testing.T) {
	for _, engine := range grammarEngines {
		capabilities := QueryGrammarCapabilities(engine)
		require.Len(t, capabilities, len(queryGrammarFamilies)+len(queryGrammarVariantRules), "%s lists the wrong number of capabilities", engine)
		for _, capability := range capabilities {
			assert.Equal(t, capability.Status == capabilitySupported, capability.Reason == "",
				"%s %s has status %s and reason %q", engine, capability.Feature, capability.Status, capability.Reason)
		}
	}
	require.Nil(t, QueryGrammarCapabilities("unknown"), "an unknown engine has capabilities")
	require.Nil(t, QueryGrammarSeeds("unknown"), "an unknown engine has seeds")
	for _, excluded := range []struct{ engine, feature string }{{"clickhouse", "grammar/join-group"}, {"clickhouse", "grammar/natural"},
		{"mysql", "join/full"}, {"mariadb", "grammar/lateral"}, {"sqlite", "grammar/grouping-sets"}, {"cockroachdb", "grouping/cube"}} {
		assert.False(t, queryGrammarSupports(excluded.engine, excluded.feature), "%s admits %s", excluded.engine, excluded.feature)
	}
}

func TestQueryGrammarDecodingIsStableAndBounded(t *testing.T) {
	seed := QueryGrammarSeeds("postgres")[0]
	first, ok := DecodeScenario("postgres", seed)
	require.True(t, ok, "rejected the first grammar seed")
	second, ok := DecodeScenario("postgres", slices.Clone(seed))
	require.True(t, ok, "rejected a cloned grammar seed")
	require.Equal(t, first.Queries[0].SQL, second.Queries[0].SQL, "decoding is not deterministic")
	require.True(t, bytes.Equal(first.Data, seed), "decoding changed the retained input")
	_, ok = DecodeScenario("postgres", append(slices.Clone(seed), make([]byte, MaxBytes)...))
	require.False(t, ok, "an oversized grammar input was accepted")
	_, ok = DecodeScenario("clickhouse", []byte{QueryGrammarVersion, queryGrammarJoinGroup})
	require.False(t, ok, "an excluded family was accepted")
	for family := range queryGrammarFamilyCount {
		_, ok := DecodeScenario("postgres", []byte{QueryGrammarVersion, byte(family)})
		assert.True(t, ok, "a short input of family %d was rejected", family)
	}
}

func TestQueryGrammarJoinNullabilityRules(t *testing.T) {
	for _, test := range []struct {
		engine string
		data   []byte
		want   []bool
	}{

		{engine: "postgres", data: grammarInput(queryGrammarOuterJoin, 0, 1, 1, 0), want: []bool{false, true, true, true, false}},

		{engine: "postgres", data: grammarInput(queryGrammarOuterJoin, 0, 2, 1, 0), want: []bool{true, true, false, true, false}},
		{engine: "postgres", data: grammarInput(queryGrammarOuterJoin, 0, 3, 1, 0), want: []bool{true, true, true, true, true}},

		{engine: "clickhouse", data: grammarInput(queryGrammarOuterJoin, 0, 3, 1, 0), want: []bool{false, true, false, true, false}},

		{engine: "postgres", data: grammarInput(queryGrammarJoinGroup, 0, 2, 0, 0, 0, 1, 0), want: []bool{false, true, true, true, false, true, false}},

		{engine: "mysql", data: grammarInput(queryGrammarJoinGroup, 1, 0, 0, 0, 0, 1, 0), want: []bool{false, true, true, true, true, true, false}},

		{engine: "sqlite", data: grammarInput(queryGrammarUsing, 1, 3, 0, 0), want: []bool{false, true, true}},

		{engine: "duckdb", data: grammarInput(queryGrammarNatural, 0, 0, 1, 0), want: []bool{false, false, false, false}},
		{engine: "duckdb", data: grammarInput(queryGrammarNatural, 0, 1, 1, 0), want: []bool{false, true, false, true}},

		{engine: "postgres", data: grammarInput(queryGrammarLateral, 1, 0), want: []bool{false, true, false, true}},
		{engine: "postgres", data: grammarInput(queryGrammarLateral, 3, 0), want: []bool{false, true, true, true}},
		{engine: "postgres", data: grammarInput(queryGrammarLateral, 2, 0), want: []bool{false, true, false, true}},
	} {
		scenario, ok := DecodeScenario(test.engine, test.data)
		require.True(t, ok, "%s rejected %v", test.engine, test.data)
		assert.Equal(t, test.want, scenario.Queries[0].MayBeNull, "%s %s", test.engine, scenario.Queries[0].Reference)
	}
}

func TestQueryGrammarClauseNullabilityRules(t *testing.T) {
	for _, test := range []struct {
		engine string
		data   []byte
		want   []bool
	}{

		{engine: "postgres", data: grammarInput(queryGrammarRecursive, 0, 0, 0, 0), want: []bool{false, true, true, true}},

		{engine: "postgres", data: grammarInput(queryGrammarRecursive, 1, 1, 0, 0), want: []bool{false, false, true, false}},

		{engine: "postgres", data: grammarInput(queryGrammarRecursive, 2, 1, 0, 0), want: []bool{false, true, true, true}},

		{engine: "postgres", data: grammarInput(queryGrammarWindow, 0, 0, 0, 10, 1, 0, 0), want: []bool{false, true, false}},
		{engine: "postgres", data: grammarInput(queryGrammarWindow, 6, 1, 0, 6, 0, 0, 0), want: []bool{false, true, false}},

		{engine: "sqlite", data: grammarInput(queryGrammarWindow, 2, 0, 0, 3, 0, 0, 0), want: []bool{false, false, false}},

		{engine: "clickhouse", data: grammarInput(queryGrammarWindow, 0, 0, 0, 6, 1, 1, 0), want: []bool{false, false, true}},

		{engine: "mysql", data: grammarInput(queryGrammarScalarLimit, 0, 2), want: []bool{false, false, true}},
		{engine: "mysql", data: grammarInput(queryGrammarScalarLimit, 3, 4), want: []bool{false, true, false}},

		{engine: "postgres", data: grammarInput(queryGrammarGrouping, 0), want: []bool{true, false, true, true}},
		{engine: "mysql", data: grammarInput(queryGrammarGrouping, 0), want: []bool{true, false, true, true}},
		{engine: "clickhouse", data: grammarInput(queryGrammarGrouping, 0), want: []bool{false, false, false, true}},

		{engine: "postgres", data: grammarInput(queryGrammarWrapped, 2, 0, 1), want: []bool{false}},
		{engine: "duckdb", data: grammarInput(queryGrammarWrapped, 2, 0, 1), want: []bool{true}},
		{engine: "postgres", data: grammarInput(queryGrammarWrapped, 0, 2, 0, 1), want: []bool{true}},
		{engine: "postgres", data: grammarInput(queryGrammarIdentifiers, 0, 1, 2, 3), want: []bool{false, true, false}},
	} {
		scenario, ok := DecodeScenario(test.engine, test.data)
		require.True(t, ok, "%s rejected %v", test.engine, test.data)
		assert.Equal(t, test.want, scenario.Queries[0].MayBeNull, "%s %s", test.engine, scenario.Queries[0].Reference)
	}
}

func TestQueryGrammarSpellsEngineSyntax(t *testing.T) {
	for _, test := range []struct{ engine, want string }{
		{engine: "mysql", want: "`MixedCase`"}, {engine: "postgres", want: `"MixedCase"`},
	} {
		scenario, ok := DecodeScenario(test.engine, grammarInput(queryGrammarIdentifiers, 0, 0, 0, 0))
		require.True(t, ok, "%s rejected the identifier family", test.engine)
		assert.Contains(t, scenario.Queries[0].SQL, test.want, "%s did not quote %s", test.engine, test.want)
	}
	scenario, ok := DecodeScenario("clickhouse", grammarInput(queryGrammarRecursive, 0, 0, 0, 0))
	require.True(t, ok, "ClickHouse rejected the recursive family")
	assert.Contains(t, scenario.Queries[0].SQL, "toNullable(i.id)", "ClickHouse anchor cannot hold the NULL a later round writes")
	scenario, ok = DecodeScenario("clickhouse", grammarInput(queryGrammarWindow, 0, 0, 0, 0, 0, 0, 0))
	require.True(t, ok, "ClickHouse rejected the window family")
	assert.Contains(t, scenario.Queries[0].SQL, "lagInFrame(i.id) OVER (ORDER BY i.id ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING)", "ClickHouse reads a frame with lagInFrame")
}

func grammarInput(family int, choices ...byte) []byte {
	data := []byte{QueryGrammarVersion, byte(family), maxRows}
	data = append(data, make([]byte, maxRows)...)
	return append(data, choices...)
}
