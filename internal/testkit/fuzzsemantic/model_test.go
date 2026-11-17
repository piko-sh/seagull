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

package fuzzsemantic

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzbuilder"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/internal/testkit/fuzztyped"
)

var engines = []string{"postgres", "timescaledb", "cockroachdb", "mysql", "mariadb", "sqlite", "duckdb", "clickhouse"}

func randomInputs(count int) [][]byte {
	random := rand.New(rand.NewPCG(19, 23))
	inputs := make([][]byte, 0, count)
	for range count {
		data := make([]byte, 1+random.IntN(headerWidth+maxItems*itemWidth+maxPeers*peerWidth))
		data[0] = SemanticVersion
		for index := 1; index < len(data); index++ {
			data[index] = byte(random.UintN(256))
		}
		inputs = append(inputs, data)
	}
	return inputs
}

func TestDecodeRejectsOtherEnvelopes(t *testing.T) {
	t.Parallel()
	for _, data := range [][]byte{nil, {}, {SemanticVersion - 1}, {SemanticVersion + 1}, {fuzzbuilder.Version}, make([]byte, MaxBytes+1)} {
		_, ok := Decode("postgres", data)
		require.False(t, ok, "%v", data)
	}
	_, ok := Decode("oracle", []byte{SemanticVersion})
	require.False(t, ok)
	model, ok := Decode("postgres", []byte{SemanticVersion})
	require.True(t, ok)
	require.Empty(t, model.Items)
	require.Equal(t, 1, model.PageSize)
}

func TestOtherDecodersRejectVersion19(t *testing.T) {
	t.Parallel()
	for _, engine := range engines {
		for _, data := range append(randomInputs(64), Seeds(engine)...) {
			_, ok := fuzzcase.Decode(engine, data)
			require.False(t, ok, "fuzzcase.Decode accepted %v", data)
			_, ok = fuzzcase.DecodeMigration(engine, data)
			require.False(t, ok, "fuzzcase.DecodeMigration accepted %v", data)
			_, ok = fuzzcase.DecodeRejection(engine, data)
			require.False(t, ok, "fuzzcase.DecodeRejection accepted %v", data)
			_, ok = fuzzcase.DecodeScenario(engine, data)
			require.False(t, ok, "fuzzcase.DecodeScenario accepted %v", data)
			_, ok = fuzzcase.DecodeHistory(engine, data)
			require.False(t, ok, "fuzzcase.DecodeHistory accepted %v", data)
			_, ok = fuzzcase.DecodeCommands(engine, data)
			require.False(t, ok, "fuzzcase.DecodeCommands accepted %v", data)
			_, ok = fuzzcase.DecodeRuntime(engine, data)
			require.False(t, ok, "fuzzcase.DecodeRuntime accepted %v", data)
			_, ok = fuzzcase.DecodeExecution(engine, data)
			require.False(t, ok, "fuzzcase.DecodeExecution accepted %v", data)
			_, ok = fuzzcase.DecodeLifecycle(engine, data)
			require.False(t, ok, "fuzzcase.DecodeLifecycle accepted %v", data)
			_, ok = fuzzbuilder.Decode(engine, data)
			require.False(t, ok, "fuzzbuilder.Decode accepted %v", data)
			_, ok = fuzztyped.Decode(engine, data)
			require.False(t, ok, "fuzztyped.Decode accepted %v", data)
		}
	}
}

func TestDecodeReadsRows(t *testing.T) {
	t.Parallel()
	model, ok := Decode("postgres", Seeds("postgres")[0])
	require.True(t, ok)
	require.Len(t, model.Items, maxItems)
	require.Len(t, model.Peers, maxPeers)
	require.Equal(t, int64(1), model.Items[0].ID)
	require.Equal(t, int64(2), *model.Items[0].A)
	require.Nil(t, model.Items[0].B)
	require.Equal(t, "a", *model.Items[0].Label)
	require.Nil(t, model.Items[1].Label)
	require.Nil(t, model.Peers[2].ItemID)
	require.Equal(t, int64(orphanID), *model.Peers[4].ItemID)
	require.Len(t, model.Inserts(), maxItems+maxPeers)
	require.Contains(t, model.Inserts()[0], "VALUES (1, 0, 2, NULL, 'a')")
}

func TestRegressionMarkerIsIgnored(t *testing.T) {
	t.Parallel()
	for _, engine := range engines {
		for _, data := range append(Seeds(engine), randomInputs(64)...) {
			plain, ok := Decode(engine, data)
			require.True(t, ok)
			marked, ok := Decode(engine, append(slices.Clone(data), fuzzbuilder.RegressionMarker...))
			require.True(t, ok)
			marked.Data = plain.Data
			require.Equal(t, plain, marked, "%s %v", engine, data)
			require.Equal(t, plain.Calls(), marked.Calls())
		}
	}
}

func TestSeedsReachEveryRelation(t *testing.T) {
	t.Parallel()
	for _, engine := range engines {
		reached := map[string]bool{}
		predicatesReached := map[int]bool{}
		modes := map[fuzzbuilder.Mode]bool{}
		for _, seed := range Seeds(engine) {
			model, ok := Decode(engine, seed)
			require.True(t, ok)
			for _, feature := range model.Features() {
				reached[feature] = true
			}
			predicatesReached[model.Predicate] = true
			modes[model.Mode] = true
			require.Empty(t, model.Problems(simulate(model, faultNone)), "%s seed %v", engine, seed)
		}
		for _, feature := range features {
			require.Equal(t, admits(engine, feature), reached[feature], "%s %s", engine, feature)
		}
		require.Len(t, predicatesReached, len(predicates), engine)
		for _, mode := range []fuzzbuilder.Mode{fuzzbuilder.ModeDatabaseSQL, fuzzbuilder.ModePrepared, fuzzbuilder.ModeNative} {
			require.Equal(t, fuzzbuilder.ModeSupported(engine, mode), modes[mode], "%s mode %d", engine, mode)
		}
	}
}

func TestCallsUseCatalogueMethods(t *testing.T) {
	t.Parallel()
	for _, engine := range engines {
		entries := map[string]query{}
		for _, entry := range catalogue(engine) {
			_, duplicate := entries[entry.name]
			require.False(t, duplicate, "%s repeats %s", engine, entry.name)
			entries[entry.name] = entry
			require.NotContains(t, entry.sql, parameterMark)
		}
		require.Equal(t, len(entries), QueryCount(engine))
		require.Equal(t, len(entries), strings.Count(Queries(engine), "-- sgl.query("))
		for _, data := range append(Seeds(engine), randomInputs(256)...) {
			model, _ := Decode(engine, data)
			labels := map[string]bool{}
			for _, current := range model.Calls() {
				entry, ok := entries[current.Method]
				require.True(t, ok, "%s calls %s outside the catalogue", engine, current.Method)
				require.Equal(t, entry.dynamic, current.Dynamic, current.Method)
				require.False(t, labels[current.Label], "%s repeats %s", engine, current.Label)
				labels[current.Label] = true
				if engine == "postgres" {
					require.Equal(t, strings.Count(entry.sql, "$"), len(current.Arguments), current.Method)
				}
			}
		}
	}
}

func TestSimulatedEngineHoldsEveryRelation(t *testing.T) {
	t.Parallel()
	for _, engine := range engines {
		for _, data := range randomInputs(1000) {
			model, ok := Decode(engine, data)
			require.True(t, ok)
			require.Empty(t, model.Problems(simulate(model, faultNone)), "%s input %v\n%s", engine, data, model)
		}
	}
}

func TestPredicatesFollowThreeValuedLogic(t *testing.T) {
	t.Parallel()
	one, two, minus := int64(1), int64(2), int64(-1)
	tests := []struct {
		name      string
		engine    string
		predicate int
		parameter int64
		item      Item
		peers     []Peer
		want      truth
	}{
		{name: "NULL comparison", engine: "postgres", predicate: 0, item: Item{A: nil, B: nil, Label: nil, ID: 1, Group: 0}, want: truthUnknown},
		{name: "BETWEEN with a NULL bound and a failing lower bound", engine: "postgres", predicate: 3, parameter: 2,
			item: Item{A: &one, B: nil, Label: nil, ID: 1, Group: 0}, want: truthFalse},
		{name: "OR with a true side", engine: "postgres", predicate: 4, parameter: 2, item: Item{A: nil, B: nil, Label: nil, ID: 1, Group: 0}, want: truthTrue},
		{name: "CASE takes ELSE on NULL", engine: "postgres", predicate: 5, parameter: 0, item: Item{A: &two, B: nil, Label: nil, ID: 1, Group: 0}, want: truthUnknown},
		{name: "truncated modulo", engine: "postgres", predicate: 8, parameter: -1, item: Item{A: &minus, B: nil, Label: nil, ID: 1, Group: 0}, want: truthTrue},
		{name: "MAX over no peers", engine: "postgres", predicate: 9, parameter: 0, item: Item{A: &one, B: nil, Label: nil, ID: 1, Group: 0}, want: truthUnknown},
		{name: "IN with an unmatched NULL", engine: "postgres", predicate: 12, parameter: 0, item: Item{A: nil, B: nil, Label: nil, ID: 1, Group: 0},
			peers: []Peer{{ItemID: nil, C: &one, ID: 1}}, want: truthUnknown},
		{name: "ClickHouse IN ignores an unmatched NULL", engine: "clickhouse", predicate: 12, parameter: 0,
			item: Item{A: nil, B: nil, Label: nil, ID: 1, Group: 0}, peers: []Peer{{ItemID: nil, C: &one, ID: 1}}, want: truthFalse},
		{name: "IN with a match beside NULL", engine: "postgres", predicate: 12, parameter: 0, item: Item{A: nil, B: nil, Label: nil, ID: 1, Group: 0},
			peers: []Peer{{ItemID: nil, C: &one, ID: 1}, {ItemID: &one, C: &two, ID: 2}}, want: truthTrue},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			model := Case{Engine: test.engine, Predicate: test.predicate, Parameter: test.parameter, Items: []Item{test.item}, Peers: test.peers}
			require.Equal(t, test.want, model.truthOf(test.item))
		})
	}
}

func TestCapabilitiesGiveReasons(t *testing.T) {
	t.Parallel()
	for _, engine := range engines {
		for _, capability := range Capabilities(engine) {
			require.Equal(t, statusExcluded, capability.Status)
			require.NotEmpty(t, capability.Reason, "%s %s", engine, capability.Feature)
		}
	}
	require.Empty(t, Capabilities("postgres"))
	require.Len(t, Capabilities("mysql"), 2)
	require.Len(t, Capabilities("clickhouse"), 1)
}
