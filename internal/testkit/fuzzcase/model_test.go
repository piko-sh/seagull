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
	"encoding/json"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecoderSeedsAndShrinking(t *testing.T) {
	for _, engine := range []string{"postgres", "sqlite", "mysql", "mariadb", "duckdb", "clickhouse", "cockroachdb", "timescaledb"} {
		t.Run(engine, func(t *testing.T) {
			covered := make(map[int]bool)
			for _, seed := range Seeds(engine) {
				model, ok := Decode(engine, seed)
				require.True(t, ok, "rejected seed %v", seed)
				covered[model.Shape] = true
				require.Equal(t, model.Query(), model.Query(), "query is not deterministic")
				for length := 1; length < len(seed); length++ {
					short, accepted := Decode(engine, seed[:length])
					require.False(t, accepted && (short.Rows > 8 || len(short.Query().Values) > 8), "shrinking exceeded bounds")
				}
			}
			for shape := range shapeCount {
				require.Equal(t, supportsShape(engine, shape), covered[shape], "missing seed for shape %d", shape)
			}
		})
	}
}

func TestBundleRejectsUnsafeAndDuplicatePaths(t *testing.T) {
	for _, name := range []string{"../x.sql", "/queries/x.sql", "queries/../x.sql", "queries/a/b.sql", "queries\\x.sql", "queries/x.txt"} {
		encoded, err := json.Marshal(Bundle{Version: Version, Files: []File{{Name: name, SQL: "SELECT 1"}}})
		require.NoError(t, err)
		_, err = DecodeBundle(encoded)
		require.Error(t, err, "accepted %q", name)
	}
	encoded := []byte(`{"version":1,"files":[{"name":"queries/x.sql"},{"name":"queries/x.sql"}]}`)
	_, err := DecodeBundle(encoded)
	require.Error(t, err, "accepted duplicate path")
}

func TestModelReferencesExposeDistinctBindings(t *testing.T) {
	model, ok := Decode("postgres", []byte{1, 1, 0, 8, 3, 7})
	require.True(t, ok, "rejected model")
	query := model.Query()
	require.Len(t, query.Values, 2, "binding expectations lost identity: %+v", query)
	require.NotEqual(t, query.Values[0], query.Values[1], "binding expectations lost identity: %+v", query)
	require.Len(t, query.Occurrences, 3, "binding expectations lost identity: %+v", query)
	require.Equal(t, query.Occurrences[0], query.Occurrences[2], "binding expectations lost identity: %+v", query)
}

func TestGrammarSeedsCoverEveryEnabledFeature(t *testing.T) {
	for engine := range capabilities {
		t.Run(engine, func(t *testing.T) {
			covered := make(map[string]bool)
			for _, seed := range Seeds(engine) {
				model, ok := Decode(engine, seed)
				require.True(t, ok, "seed rejected: %v", seed)
				for _, feature := range model.Features() {
					covered[feature] = true
				}
			}
			for _, feature := range RequiredFeatures(engine) {
				assert.True(t, covered[feature], "missing positive seed for %s", feature)
			}
		})
	}
}

func TestGrammarBoundsAndIndependentTransforms(t *testing.T) {
	for engine := range capabilities {
		random := rand.New(rand.NewPCG(0, 1))
		for iteration := range 128 {
			data := make([]byte, 1+iteration*31)
			for index := range data {
				data[index] = byte(random.Uint32())
			}
			data[0] = grammarVersion
			model, ok := Decode(engine, data)
			require.True(t, ok, "rejected grammar for %s", engine)
			query := model.Query()
			require.False(t, len(query.Values) > 8 || len(query.Occurrences) > 8 || query.Columns > 8 || model.Rows > 8,
				"model exceeded bounds: %+v", query)
			projections, predicate := model.grammarExpressions()
			for _, root := range append(projections, predicate) {
				require.LessOrEqual(t, measuredExpressionDepth(root), expressionDepth, "expression depth exceeds bound")
			}
			for _, variant := range model.Variants() {
				require.True(t, slices.Equal(query.Values, variant.Values) && slices.Equal(query.Occurrences, variant.Occurrences) && query.Columns == variant.Columns,
					"transformation changed independent binding expectations: %+v", variant)
			}
		}
	}
}

func TestVersionOneCorpusMeaningRemainsStable(t *testing.T) {
	model, ok := Decode("postgres", []byte{1, 1, 0, 8, 3, 7})
	require.True(t, ok, "version one input was rejected")
	expected := Query{
		SQL:            "-- sgl.query(name: FuzzQuery, command: many, dynamic: runtime)\nSELECT i.id, i.category FROM fuzz_items i WHERE (i.id >= $1 AND i.category < $2) OR i.id = $1 ORDER BY 1, 2;",
		Reference:      "SELECT i.id, i.category FROM fuzz_items i WHERE (i.id >= 0 AND i.category < 9) OR i.id = 0 ORDER BY 1, 2",
		CountReference: "SELECT i.id, i.category FROM fuzz_items i WHERE (i.id >= 0 AND i.category < 9) OR i.id = 0",
		Values:         []Value{{Integer: 0}, {Integer: 9}}, Occurrences: []Value{{Integer: 0}, {Integer: 9}, {Integer: 0}}, Columns: 2, Writes: false, Ordered: true,
	}
	require.Equal(t, expected, model.Query(), "version one changed")
	require.Empty(t, model.Variants(), "version two transforms changed version one")
}

func TestGrammarPermanentOperationIdentifiers(t *testing.T) {
	for _, operations := range [][]int{
		{sourceTable, sourceLeftJoin, sourceInnerJoin, sourceCTE, sourceDerived, grammarSourceCount},
		{numericColumn, numericLiteral, numericParameter, numericAdd, numericSubtract, numericCoalesce, numericCase, numericNegate, numericOperationCount},
		{booleanComparison, booleanAnd, booleanOr, booleanNot, booleanNull, booleanNotNull, booleanIn, booleanNotIn, booleanOperationCount},
	} {
		for index, operation := range operations {
			require.Equal(t, index, operation, "corpus operation %d changed", index)
		}
	}
}

func TestMembershipModelsKeepNativeOperandCapabilities(t *testing.T) {
	for engine := range capabilities {
		model, ok := Decode(engine, []byte("\x02000000000007"))
		require.True(t, ok, "missing membership model for %s", engine)
		want := "i.id NOT IN (i.id, i.id)"
		if engine == "clickhouse" {
			want = "i.id NOT IN (CAST(0 AS Int64), CAST(0 AS Int64))"
		}
		require.Contains(t, model.Query().Reference, want, "%s membership changed", engine)
	}
}

func measuredExpressionDepth(node expression) int {
	depth := 0
	for _, child := range node.children {
		depth = max(depth, measuredExpressionDepth(child))
	}
	return depth + 1
}
