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

package fuzzbuilder

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

var engines = []string{enginePostgres, engineTimescale, engineCockroach, engineMySQL, engineMariaDB, engineSQLite, engineDuckDB, engineClickHouse}

func TestDecodeRejectsOtherVersionsAndEngines(t *testing.T) {
	t.Parallel()
	for _, data := range [][]byte{nil, {}, {15}, {17}, {fuzzcase.ExecutionVersion}, {fuzzcase.QueryGrammarVersion}, make([]byte, MaxBytes+1)} {
		_, ok := Decode(enginePostgres, data)
		assert.False(t, ok, "decoded %v", data)
	}
	_, ok := Decode("oracle", []byte{Version})
	assert.False(t, ok, "decoded an unmodelled engine")
}

func TestOtherDecodersLeaveVersionSixteen(t *testing.T) {
	t.Parallel()
	for _, engine := range engines {
		data := append([]byte{Version}, seedRows...)
		_, ok := fuzzcase.DecodeScenario(engine, data)
		assert.False(t, ok, "%s: scenario decoder claimed version 16", engine)
		_, ok = fuzzcase.DecodeRuntime(engine, data)
		assert.False(t, ok, "%s: runtime decoder claimed version 16", engine)
		_, ok = fuzzcase.Decode(engine, data)
		assert.False(t, ok, "%s: model decoder claimed version 16", engine)
	}
}

func TestCatalogueOrderMatchesKinds(t *testing.T) {
	t.Parallel()
	for index := range catalogue {
		require.Equal(t, index, int(catalogue[index].kind), "catalogue entry %d", index)
	}
}

func TestSeedsDecodeAndReachEveryOperation(t *testing.T) {
	t.Parallel()
	for _, engine := range engines {
		reached := make(map[Operation]bool)
		queries := make(map[int]bool)
		for _, seed := range Seeds(engine) {
			model, ok := Decode(engine, seed)
			require.True(t, ok, "%s: seed %v does not decode", engine, seed)
			queries[model.Query] = true
			for _, step := range model.Steps {
				reached[step.Op] = true
			}
			require.Len(t, model.outcomes(), len(model.Steps), "%s: outcomes do not cover the steps", engine)
		}
		assert.Len(t, reached, int(operationCount), "%s: operations reached", engine)
		assert.Len(t, queries, len(catalogue), "%s: queries reached", engine)
	}
}

func TestDecodeIsDeterministic(t *testing.T) {
	t.Parallel()
	for _, seed := range Seeds(engineMySQL) {
		first, ok := Decode(engineMySQL, seed)
		require.True(t, ok)
		second, ok := Decode(engineMySQL, seed)
		require.True(t, ok)
		require.Equal(t, first.String(), second.String(), "decoding %v differs", seed)
	}
}

func TestSchemaAndQueriesNameEveryColumn(t *testing.T) {
	t.Parallel()
	for _, engine := range engines {
		text := Queries(engine)
		assert.Equal(t, QueryCount(), strings.Count(text, "dynamic: runtime"), "%s: incomplete catalogue", engine)
		assert.Len(t, Schema(engine), 2, "%s: incomplete schema", engine)
	}
}

func TestLike(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		text, pattern string
		want          bool
	}{
		{text: "abc", pattern: "a%", want: true}, {text: "abc", pattern: "a_c", want: true}, {text: "ab", pattern: "a_c", want: false},
		{text: "", pattern: "%", want: true}, {text: "", pattern: "_", want: false}, {text: "ba", pattern: "%a", want: true},
		{text: "ab", pattern: "%b%", want: true}, {text: "", pattern: "", want: true}, {text: "a", pattern: "", want: false},
	} {
		assert.Equal(t, test.want, like(test.text, test.pattern), "like(%q, %q)", test.text, test.pattern)
	}
}

func TestThreeValuedConditions(t *testing.T) {
	t.Parallel()
	null, three := nullCell(), integerCell(3)
	for _, test := range []struct {
		condition predicate
		cell      Cell
		want      bool
	}{
		{condition: predicate{value: []int64{}, operator: operatorNotIn, column: 0}, cell: null, want: true},
		{condition: predicate{value: []int64{}, operator: operatorIn, column: 0}, cell: three, want: false},
		{condition: predicate{value: []int64{1}, operator: operatorNotIn, column: 0}, cell: null, want: false},
		{condition: predicate{value: int64(3), operator: operatorIn, column: 0}, cell: three, want: true},
		{condition: predicate{value: nil, operator: operatorEqual, column: 0}, cell: three, want: false},
		{condition: predicate{value: int64(3), operator: "<>", column: 0}, cell: null, want: false},
		{condition: predicate{value: nil, operator: operatorIsNull, column: 0}, cell: null, want: true},
	} {
		assert.Equal(t, test.want, test.condition.holds(test.cell), "%+v on %+v", test.condition, test.cell)
	}
}

func TestComparisonRejectsFaults(t *testing.T) {
	t.Parallel()
	seed := append([]byte{Version, 0, byte(kindPlain), 0, byte(len(seedRows) / rowWidth)}, seedRows...)
	seed = append(seed, byte(OperationOrder), 0, 1, byte(OperationAll), byte(OperationCount))
	model, ok := Decode(enginePostgres, seed)
	require.True(t, ok, "seed does not decode")
	expected := model.outcomes()
	good := []Observation{{Class: "", Error: "", Rows: nil, Count: 0}, {Class: "", Error: "", Rows: expected[1].page, Count: 0},
		{Class: "", Error: "", Rows: nil, Count: expected[2].count}}
	require.Empty(t, model.Problems(good), "the model rejects its own outcome")
	reversed := slices.Clone(expected[1].page)
	slices.Reverse(reversed)
	kind := slices.Clone(expected[1].page)
	kind[0] = slices.Clone(kind[0])
	kind[0][1] = textCell("0")
	for name, observations := range map[string][]Observation{
		"order": {good[0], {Class: "", Error: "", Rows: reversed, Count: 0}, good[2]},
		"count": {good[0], good[1], {Class: "", Error: "", Rows: nil, Count: expected[2].count + 1}},
		"kind":  {good[0], {Class: "", Error: "", Rows: kind, Count: 0}, good[2]},
		"short": {good[0], {Class: "", Error: "", Rows: expected[1].page[1:], Count: 0}, good[2]},
		"error": {good[0], {Class: ClassError, Error: "syntax", Rows: nil, Count: 0}, good[2]},
		"calls": {good[0]},
	} {
		assert.NotEmpty(t, model.Problems(observations), "planted %s fault passed", name)
	}
}

func TestTiedPagesAcceptAnyTiedRow(t *testing.T) {
	t.Parallel()
	model := Case{Engine: enginePostgres, Data: nil, Peers: nil, Steps: nil, Query: int(kindPlain), Parameter: 0, Mode: ModeDatabaseSQL,
		Rows: []Row{{Score: nil, Dotted: nil, Label: "a", Select: "", ID: 1, Group: 0, Mixed: 0},
			{Score: nil, Dotted: nil, Label: "b", Select: "", ID: 2, Group: 0, Mixed: 0}}}
	model.Steps = []Step{{Value: nil, Column: "grp", Operator: "", Direction: "ASC", Parameter: 0, Op: OperationOrder, Slot: 0, Query: 0, Number: 0},
		{Value: nil, Column: "", Operator: "", Direction: "", Parameter: 0, Op: OperationLimit, Slot: 0, Query: 0, Number: 1},
		{Value: nil, Column: "", Operator: "", Direction: "", Parameter: 0, Op: OperationAll, Slot: 0, Query: 0, Number: 0}}
	for _, row := range model.project(kindPlain) {
		observations := []Observation{{}, {}, {Class: "", Error: "", Rows: [][]Cell{row}, Count: 0}}
		assert.Empty(t, model.Problems(observations), "tied row %v refused", row)
	}
}

func TestRegressionMarkerIsIgnored(t *testing.T) {
	t.Parallel()
	data := append([]byte{Version, 0, byte(kindUnion), 0, 1, 1, 0x83, 0}, byte(OperationOrder), 0, 1, byte(OperationLimit), 2, byte(OperationAll))
	for _, engine := range engines {
		plain, ok := Decode(engine, data)
		require.True(t, ok)
		marked, ok := Decode(engine, append(slices.Clone(data), RegressionMarker...))
		require.True(t, ok)
		assert.Equal(t, int(kindUnion), plain.Query)
		assert.Len(t, plain.Steps, 3)
		assert.Equal(t, plain.Steps, marked.Steps, "%s: the marker changed the decoded input", engine)
		for _, entry := range Capabilities(engine) {
			assert.Equal(t, statusExcluded, entry.Status)
			assert.NotEmpty(t, entry.Reason, "%s: exclusion %s lacks a reason", engine, entry.Feature)
		}
	}
}

func TestInsertsAndDescriptionsCoverEveryStep(t *testing.T) {
	t.Parallel()
	for _, seed := range Seeds(enginePostgres) {
		model, ok := Decode(enginePostgres, seed)
		require.True(t, ok)
		require.Len(t, model.Inserts(), len(model.Rows)+len(model.Peers))
		for _, step := range model.Steps {
			require.NotEmpty(t, step.String())
		}
	}
}
