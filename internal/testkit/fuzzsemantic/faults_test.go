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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeliberatelyWrongEnginesAreCaught(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mistake fault
		feature string
		engine  string
		always  bool
	}{
		{mistake: faultNotKeepsNull, feature: featurePartitionRows, always: true},
		{mistake: faultTwoValued, feature: featurePivot},
		{mistake: faultInIgnoresNull, feature: featurePivot},
		{mistake: faultSumOffByOne, feature: featurePartitionAggregates, always: true},
		{mistake: faultMinIsMax, feature: featurePartitionAggregates},
		{mistake: faultCountStar, feature: featurePartitionAggregates, always: true},
		{mistake: faultCommonCount, feature: featureSubqueryCounts, always: true},
		{mistake: faultRowNumberZero, feature: featureWindow, always: true},
		{mistake: faultRunningReverse, feature: featureWindow},
		{mistake: faultLagCurrent, feature: featureWindow, always: true},
		{mistake: faultGroupSum, feature: featureWindow, always: true},
		{mistake: faultWindowPrefilter, feature: featureWindowFilter, always: true},
		{mistake: faultLeftIsInner, feature: featureLeftJoin, always: true},
		{mistake: faultFullIsLeft, feature: featureFullJoin, always: true},
		{mistake: faultSemiDuplicates, feature: featureSemiJoin, always: true},
		{mistake: faultExistsAll, feature: featureSemiJoin, always: true},
		{mistake: faultUnionAllDedup, feature: featureUnionAll, always: true},
		{mistake: faultUnionKeepsDups, feature: featureUnion, always: true},
		{mistake: faultExceptAll, feature: featureSetOperations, always: true},
		{mistake: faultIntersectAll, feature: featureSetOperations, always: true},
		{mistake: faultExceptAll, feature: featureSetOperations, engine: engineClickHouse, always: true},
		{mistake: faultIntersectAll, feature: featureSetOperations, engine: engineClickHouse, always: true},
		{mistake: faultBareSetDistinct, feature: featureSetOperations, engine: engineClickHouse, always: true},
		{mistake: faultOffsetShift, feature: featureStaticPages, always: true},
		{mistake: faultLimitIgnored, feature: featureBuilderPages, always: true},
		{mistake: faultCountPlusOne, feature: featureBuilderPages, always: true},
		{mistake: faultBuilderWhere, feature: featureBuilderPages, always: true},
		{mistake: faultDescNullsSplit, feature: featureBuilderPages, always: true},
		{mistake: faultLimitPrefilter, feature: featureBuilderLimit, always: true},
		{mistake: faultLimitPrefilter, feature: featureBuilderLimit, engine: engineClickHouse, always: true},
	}
	inputs := randomInputs(1500)
	for _, test := range tests {
		engine := test.engine
		if engine == "" {
			engine = "postgres"
		}
		t.Run(engine+"/"+string(test.mistake), func(t *testing.T) {
			t.Parallel()
			caught := 0
			for _, data := range inputs {
				model, _ := Decode(engine, data)
				observations := simulate(model, test.mistake)
				if reflect.DeepEqual(observations, simulate(model, faultNone)) {
					continue
				}
				reported := false
				for _, problem := range model.Problems(observations) {
					require.Equal(t, KindMismatch, problem.Kind, problem.String())
					reported = reported || problem.Relation == test.feature
				}
				require.True(t, reported || !test.always, "%s changed the observations of %v without breaking %s\n%s", test.mistake, data, test.feature, model)
				if reported {
					caught++
				}
			}
			require.Positive(t, caught, "%s was never caught", test.mistake)
		})
	}
}

func TestNativeErrorsAreNotMismatches(t *testing.T) {
	t.Parallel()
	model, ok := Decode("postgres", Seeds("postgres")[0])
	require.True(t, ok)
	observations := simulate(model, faultNone)
	for index := range observations {
		if observations[index].Label == labelUnion {
			observations[index] = Observation{Label: labelUnion, Class: ClassNative, Error: "syntax error", Rows: nil, Count: 0}
		}
		if observations[index].Label == labelUnionAll {
			observations[index].Rows = observations[index].Rows[1:]
		}
	}
	problems := model.Problems(observations)
	require.Len(t, problems, 2, "%v", problems)
	require.Equal(t, Problem{Kind: KindNativeError, Relation: methodUnion + " (" + labelUnion + ")", Detail: "native: syntax error"}, problems[0])
	require.Equal(t, KindMismatch, problems[1].Kind)
	require.Equal(t, featureUnionAll, problems[1].Relation)
}

func TestHarnessFaultsAreSeparate(t *testing.T) {
	t.Parallel()
	model, ok := Decode("postgres", Seeds("postgres")[0])
	require.True(t, ok)
	observations := simulate(model, faultNone)
	require.Equal(t, KindHarness, model.Problems(observations[1:])[0].Kind)
	observations[0], observations[1] = observations[1], observations[0]
	require.Equal(t, KindHarness, model.Problems(observations)[0].Kind)
	observations[0], observations[1] = observations[1], observations[0]
	observations[0].Rows = append(observations[0].Rows, []Cell{{Text: "1", Null: false}})
	problems := model.Problems(observations)
	require.Equal(t, KindMismatch, problems[0].Kind)
	require.Contains(t, problems[0].Detail, "does not hold 5 columns")
}
