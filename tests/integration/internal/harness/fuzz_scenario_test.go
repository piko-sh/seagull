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

	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/sdk/dialect"
)

func TestScenarioRelationsDetectWrongValuesAndMultiplicities(t *testing.T) {
	complete := [][]any{{int64(1)}, {int64(1)}, {nil}}
	observations := [][][]any{complete, {{int64(1)}}, {{int64(1)}}, {{nil}}}
	relation := fuzzcase.Relation{Kind: relationPartition, Queries: []int{0, 1, 2, 3}}
	require.Empty(t, scenarioRelationProblem(relation, observations))
	observations[2] = nil
	require.NotEmpty(t, scenarioRelationProblem(relation, observations), "partition oracle accepted a lost duplicate")
	observations[2] = [][]any{{"1"}}
	require.NotEmpty(t, scenarioRelationProblem(relation, observations), "partition oracle accepted a changed storage kind")
	observations[2] = [][]any{{int64(1)}}
	observations[3] = [][]any{{"NULL"}}
	require.NotEmpty(t, scenarioRelationProblem(relation, observations), "partition oracle conflated NULL with text")
}

func TestScenarioRelationsRejectMalformedRelations(t *testing.T) {
	observations := [][][]any{{{int64(1)}}, {{int64(1)}}}
	for _, relation := range []fuzzcase.Relation{
		{Kind: relationEqual, Queries: []int{0}},
		{Kind: relationEqual, Queries: []int{0, 2}},
		{Kind: "unknown", Queries: []int{0, 1}},
	} {
		require.NotEmpty(t, scenarioRelationProblem(relation, observations), "%+v", relation)
	}
	require.Empty(t, scenarioRelationProblem(fuzzcase.Relation{Kind: relationEqual, Queries: []int{0, 1}}, observations))
}

func TestScenarioTruthCountDetectsWrongCounts(t *testing.T) {
	relation := fuzzcase.Relation{Kind: relationCountTruth, Queries: []int{0, 1}}
	observations := [][][]any{{{int64(2)}}, {{int64(1)}, {int64(0)}, {int64(1)}}}
	require.Empty(t, scenarioRelationProblem(relation, observations))
	observations[0][0][0] = int64(3)
	require.NotEmpty(t, scenarioRelationProblem(relation, observations), "truth count accepted a changed count")
	require.NotEmpty(t, scenarioTruthProblem([][]any{{int64(1)}}, [][]any{{int64(2)}}), "truth count accepted a non-Boolean truth")
	require.NotEmpty(t, scenarioTruthProblem([][]any{{int64(1)}}, [][]any{{int64(1), int64(1)}}), "truth count accepted a wide projection")
}

func TestSQLiteScenarioTypesRejectUnsafeAffinity(t *testing.T) {
	scenario, ok := fuzzcase.DecodeScenario(engineSQLite, []byte{fuzzcase.TypedVersion, 1, 0})
	require.True(t, ok, "missing decimal fixture")
	require.Equal(t, "type/decimal", scenario.Features[0], "missing decimal fixture")
	for _, storage := range []string{"integer", "real"} {
		handled, problem := scenarioSQLiteType(scenario, dialect.SQLType{Category: dialect.TypeCategoryDecimal}, storage)
		require.True(t, handled, "valid numeric affinity was not handled")
		require.Empty(t, problem, "valid numeric affinity was rejected")
	}
	for _, category := range []dialect.SQLTypeCategory{dialect.TypeCategoryInteger, dialect.TypeCategoryFloat, dialect.TypeCategoryUnknown} {
		_, problem := scenarioSQLiteType(scenario, dialect.SQLType{Category: category}, "real")
		require.NotEmpty(t, problem, "unsafe declaration was accepted")
	}
	_, problem := scenarioSQLiteType(scenario, dialect.SQLType{Category: dialect.TypeCategoryDecimal}, "text")
	require.NotEmpty(t, problem, "non-numeric result was accepted")
}

func TestSQLiteScenarioTypesIgnoreScenariosWithoutFeatures(t *testing.T) {
	handled, problem := scenarioSQLiteType(fuzzcase.Scenario{Engine: engineSQLite}, dialect.SQLType{Category: dialect.TypeCategoryDecimal}, "text")
	require.False(t, handled)
	require.Empty(t, problem)
}

func TestScenarioWrapsConvertingReadsOnlyWrapperBytes(t *testing.T) {
	require.False(t, scenarioWrapsConverting([]byte{0, 0, 0, 0, 0, 0, 0, 0, 1}))
	require.True(t, scenarioWrapsConverting([]byte{0, 0, 0, 0, 0, 1}))
	require.False(t, scenarioWrapsConverting([]byte{0, 0}))
}
