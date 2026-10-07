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

package timescaledb_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/timescaledb"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	engineNameCounterSummary  = "counter_summary"
	engineNameStatsSummary2D  = "statssummary2d"
	engineNameTimeWeightSum   = "time_weight_summary"
	engineNameHeartbeat       = "heartbeat"
	engineNameStateSummary    = "state_summary"
	engineNameCompactStateAgg = "compact_state_agg"
	engineNameCandlestick     = "candlestick"
	engineNameTDigest         = "tdigest"
	engineNameUDDSketch       = "uddsketch"
	engineNameTstzrange       = "tstzrange"
	engineNameFloat8          = "float8"
	engineNameBigint          = "int8"
	engineNameInteger         = "int4"
	engineNameSmallint        = "int2"
	engineNameTimestamptz     = "timestamptz"
	engineNameInterval        = "interval"
	engineNameText            = "text"
	engineNameBoolean         = "bool"
)

const (
	engineNameRegclass                         = "regclass"
	engineNameRegproc                          = "regproc"
	engineNameVoid                             = "void"
	engineNameUUID                             = "uuid"
	engineNameJSONB                            = "jsonb"
	engineNameAny                              = ""
	engineNameName                             = "name"
	engineNameSpaceSavingBigintAgg             = "space_saving_bigint_aggregate"
	engineNameSpaceSavingTextAgg               = "space_saving_text_aggregate"
	engineNameSpaceSavingAgg                   = "space_saving_aggregate"
	engineNameMinNState                        = "min_n_state"
	engineNameMinNByState                      = "min_n_by_state"
	engineNameTimevectorTstzF64                = "timevector_tstz_f64"
	engineNameCountMinSketch                   = "count_min_sketch"
	engineNameHypertableCompressionStatsRecord = "hypertable_compression_stats_record"
	engineNameChunkCompressionStatsRecord      = "chunk_compression_stats_record"
	engineNameAlterJobRecord                   = "alter_job_record"
	engineNameRegclassArray                    = "regclass[]"
)

func findOverload(catalogue *ast.FunctionCatalogue, name string, engineNames []string) *sqlmodel.Function {
	overloads, exists := catalogue.Functions[name]
	if !exists {
		return nil
	}
	for _, signature := range overloads {
		if len(signature.Arguments) != len(engineNames) {
			continue
		}
		matched := true
		for index, argument := range signature.Arguments {
			if argument.Type.EngineName != engineNames[index] {
				matched = false
				break
			}
		}
		if matched {
			return signature
		}
	}
	return nil
}

func TestTimeDeltaReturnsFloat8(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "time_delta", []string{engineNameCounterSummary})
	require.NotNil(t, signature, "time_delta(counter_summary) must be registered")
	assert.Equal(t, engineNameFloat8, signature.ReturnType.EngineName,
		"time_delta returns float8 seconds, not an interval")
	assert.Equal(t, sqlmodel.TypeCategoryFloat, signature.ReturnType.Category)
}

func TestCorrIsRegistered(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "corr", []string{engineNameStatsSummary2D})
	require.NotNil(t, signature, "corr(statssummary2d) must be the canonical toolkit name")
	assert.Equal(t, engineNameFloat8, signature.ReturnType.EngineName)
}

func TestCorrelationAliasIsDropped(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "correlation", []string{engineNameStatsSummary2D})
	assert.Nil(t, signature, "correlation alias should be dropped; only corr is canonical")
}

func TestTimeBucketNgIsRemoved(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	_, exists := catalogue.Functions["time_bucket_ng"]
	assert.False(t, exists, "time_bucket_ng was removed upstream and must not resolve")
}

func TestTopkIsRemoved(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	_, exists := catalogue.Functions["topk"]
	assert.False(t, exists, "topk(k, value) does not exist in TimescaleDB and was removed")
}

func TestRateIntervalOverloadIsRemoved(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "rate", []string{engineNameCounterSummary, engineNameInterval})
	assert.Nil(t, signature, "rate(summary, interval) is not a real upstream overload")

	primary := findOverload(catalogue, "rate", []string{engineNameCounterSummary})
	require.NotNil(t, primary, "rate(summary) must remain registered")
	assert.Equal(t, engineNameFloat8, primary.ReturnType.EngineName)
}

func TestTimeBucketIntegerOverloads(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	cases := []struct {
		description string
		engineName  string
	}{
		{description: "smallint epoch column", engineName: engineNameSmallint},
		{description: "integer epoch column", engineName: engineNameInteger},
		{description: "bigint epoch column", engineName: engineNameBigint},
	}

	for _, testCase := range cases {
		t.Run(testCase.description, func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, "time_bucket", []string{testCase.engineName, testCase.engineName})
			require.NotNil(t, signature, "time_bucket(%s, %s) must be registered", testCase.engineName, testCase.engineName)
			assert.Equal(t, testCase.engineName, signature.ReturnType.EngineName)

			offsetSignature := findOverload(catalogue, "time_bucket",
				[]string{testCase.engineName, testCase.engineName, testCase.engineName})
			require.NotNil(t, offsetSignature,
				"time_bucket(%s, %s, %s) offset/origin overload must be registered",
				testCase.engineName, testCase.engineName, testCase.engineName)
			assert.Equal(t, testCase.engineName, offsetSignature.ReturnType.EngineName)
		})
	}
}

func TestTimeBucketGapfillTimezoneOverload(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "time_bucket_gapfill", []string{
		engineNameInterval, engineNameTimestamptz, engineNameText, engineNameTimestamptz, engineNameTimestamptz,
	})
	require.NotNil(t, signature, "time_bucket_gapfill 5-arg timezone form must be registered")
	assert.Equal(t, engineNameTimestamptz, signature.ReturnType.EngineName)
}

func TestCounterSummaryAccessors(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	cases := []struct {
		name       string
		returnName string
	}{
		{name: "num_changes", returnName: engineNameBigint},
		{name: "num_resets", returnName: engineNameBigint},
		{name: "num_elements", returnName: engineNameBigint},
		{name: "first_val", returnName: engineNameFloat8},
		{name: "last_val", returnName: engineNameFloat8},
		{name: "first_time", returnName: engineNameTimestamptz},
		{name: "last_time", returnName: engineNameTimestamptz},
		{name: "slope", returnName: engineNameFloat8},
		{name: "intercept", returnName: engineNameFloat8},
		{name: "corr", returnName: engineNameFloat8},
		{name: "counter_zero_time", returnName: engineNameTimestamptz},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, testCase.name, []string{engineNameCounterSummary})
			require.NotNil(t, signature, "%s(counter_summary) must be registered", testCase.name)
			assert.Equal(t, testCase.returnName, signature.ReturnType.EngineName)
		})
	}
}

func TestCounterExtrapolationMethodOverloads(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	cases := []string{"extrapolated_delta", "extrapolated_rate"}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, name, []string{engineNameCounterSummary, engineNameText})
			require.NotNil(t, signature, "%s(summary, method) must be registered", name)
			assert.Equal(t, engineNameFloat8, signature.ReturnType.EngineName)
		})
	}
}

func TestCounterWithBoundsAndThreeArgumentAggregate(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	withBounds := findOverload(catalogue, "with_bounds", []string{engineNameCounterSummary, engineNameTstzrange})
	require.NotNil(t, withBounds, "with_bounds(summary, tstzrange) must be registered")
	assert.Equal(t, engineNameCounterSummary, withBounds.ReturnType.EngineName)

	threeArgAgg := findOverload(catalogue, "counter_agg", []string{engineNameTimestamptz, engineNameFloat8, engineNameTstzrange})
	require.NotNil(t, threeArgAgg, "counter_agg(timestamptz, float8, tstzrange) must be registered")
	assert.True(t, threeArgAgg.IsAggregate)
	assert.Equal(t, engineNameCounterSummary, threeArgAgg.ReturnType.EngineName)
}

func TestStats2DAxisAccessors(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	names := []string{
		"average_x", "average_y",
		"sum_x", "sum_y",
		"stddev_x", "stddev_y",
		"variance_x", "variance_y",
		"skewness_x", "skewness_y",
		"kurtosis_x", "kurtosis_y",
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, name, []string{engineNameStatsSummary2D})
			require.NotNil(t, signature, "%s(statssummary2d) must be registered", name)
			assert.Equal(t, engineNameFloat8, signature.ReturnType.EngineName)
		})
	}
}

func TestStatsMethodTextOverloads(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	for _, name := range []string{"stddev", "variance", "skewness", "kurtosis"} {
		t.Run(name+" 1D method overload", func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, name, []string{"statssummary1d", engineNameText})
			require.NotNil(t, signature, "%s(statssummary1d, method) must be registered", name)
			assert.Equal(t, engineNameFloat8, signature.ReturnType.EngineName)
		})
		t.Run(name+" 2D method overload", func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, name, []string{engineNameStatsSummary2D, engineNameText})
			require.NotNil(t, signature, "%s(statssummary2d, method) must be registered", name)
			assert.Equal(t, engineNameFloat8, signature.ReturnType.EngineName)
		})
	}

	covariance := findOverload(catalogue, "covariance", []string{engineNameStatsSummary2D, engineNameText})
	require.NotNil(t, covariance, "covariance(statssummary2d, method) must be registered")
	assert.Equal(t, engineNameFloat8, covariance.ReturnType.EngineName)
}

func TestPercentileAggReturnsUDDSketch(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "percentile_agg", []string{engineNameFloat8})
	require.NotNil(t, signature, "percentile_agg(float8) must be registered as an aggregate")
	assert.True(t, signature.IsAggregate)
	assert.Equal(t, engineNameUDDSketch, signature.ReturnType.EngineName)
}

func TestPercentileAccessors(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	tdigestRank := findOverload(catalogue, "approx_percentile_rank", []string{engineNameFloat8, engineNameTDigest})
	require.NotNil(t, tdigestRank)
	assert.Equal(t, engineNameFloat8, tdigestRank.ReturnType.EngineName)

	uddsketchRank := findOverload(catalogue, "approx_percentile_rank", []string{engineNameFloat8, engineNameUDDSketch})
	require.NotNil(t, uddsketchRank)
	assert.Equal(t, engineNameFloat8, uddsketchRank.ReturnType.EngineName)

	tdigestMean := findOverload(catalogue, "mean", []string{engineNameTDigest})
	require.NotNil(t, tdigestMean)
	assert.Equal(t, engineNameFloat8, tdigestMean.ReturnType.EngineName)

	uddsketchMean := findOverload(catalogue, "mean", []string{engineNameUDDSketch})
	require.NotNil(t, uddsketchMean)
	assert.Equal(t, engineNameFloat8, uddsketchMean.ReturnType.EngineName)

	tdigestValueCount := findOverload(catalogue, "num_vals", []string{engineNameTDigest})
	require.NotNil(t, tdigestValueCount)
	assert.Equal(t, engineNameFloat8, tdigestValueCount.ReturnType.EngineName)

	uddsketchValueCount := findOverload(catalogue, "num_vals", []string{engineNameUDDSketch})
	require.NotNil(t, uddsketchValueCount)
	assert.Equal(t, engineNameFloat8, uddsketchValueCount.ReturnType.EngineName)

	minValue := findOverload(catalogue, "min_val", []string{engineNameTDigest})
	require.NotNil(t, minValue)
	assert.Equal(t, engineNameFloat8, minValue.ReturnType.EngineName)

	maxValue := findOverload(catalogue, "max_val", []string{engineNameTDigest})
	require.NotNil(t, maxValue)
	assert.Equal(t, engineNameFloat8, maxValue.ReturnType.EngineName)
}

func TestCandlestickConstructorAndTimeAccessors(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	constructor := findOverload(catalogue, "candlestick", []string{
		engineNameTimestamptz,
		engineNameFloat8, engineNameFloat8, engineNameFloat8, engineNameFloat8, engineNameFloat8,
	})
	require.NotNil(t, constructor, "candlestick(timestamptz, float8x5) constructor must be registered")
	assert.Equal(t, engineNameCandlestick, constructor.ReturnType.EngineName)

	highTime := findOverload(catalogue, "high_time", []string{engineNameCandlestick})
	require.NotNil(t, highTime)
	assert.Equal(t, engineNameTimestamptz, highTime.ReturnType.EngineName)

	lowTime := findOverload(catalogue, "low_time", []string{engineNameCandlestick})
	require.NotNil(t, lowTime)
	assert.Equal(t, engineNameTimestamptz, lowTime.ReturnType.EngineName)
}

func TestTimeWeightFamily(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	constructor := findOverload(catalogue, "time_weight",
		[]string{engineNameText, engineNameTimestamptz, engineNameFloat8})
	require.NotNil(t, constructor, "time_weight(method, timestamptz, float8) aggregate must be registered")
	assert.True(t, constructor.IsAggregate)
	assert.Equal(t, engineNameTimeWeightSum, constructor.ReturnType.EngineName)

	avg := findOverload(catalogue, "average", []string{engineNameTimeWeightSum})
	require.NotNil(t, avg)
	assert.Equal(t, engineNameFloat8, avg.ReturnType.EngineName)

	integralOne := findOverload(catalogue, "integral", []string{engineNameTimeWeightSum})
	require.NotNil(t, integralOne)
	assert.Equal(t, engineNameFloat8, integralOne.ReturnType.EngineName)

	integralTwo := findOverload(catalogue, "integral", []string{engineNameTimeWeightSum, engineNameText})
	require.NotNil(t, integralTwo, "integral(summary, unit) overload must be registered")
	assert.Equal(t, engineNameFloat8, integralTwo.ReturnType.EngineName)

	firstValue := findOverload(catalogue, "first_val", []string{engineNameTimeWeightSum})
	require.NotNil(t, firstValue)
	assert.Equal(t, engineNameFloat8, firstValue.ReturnType.EngineName)

	lastTime := findOverload(catalogue, "last_time", []string{engineNameTimeWeightSum})
	require.NotNil(t, lastTime)
	assert.Equal(t, engineNameTimestamptz, lastTime.ReturnType.EngineName)

	interpolatedAvg := findOverload(catalogue, "interpolated_average", []string{
		engineNameTimeWeightSum, engineNameTimestamptz, engineNameInterval,
		engineNameTimeWeightSum, engineNameTimeWeightSum,
	})
	require.NotNil(t, interpolatedAvg, "interpolated_average must be registered with 5 arguments")
	assert.Equal(t, engineNameFloat8, interpolatedAvg.ReturnType.EngineName)

	rollup := findOverload(catalogue, "rollup", []string{engineNameTimeWeightSum})
	require.NotNil(t, rollup, "rollup(time_weight_summary) must be registered")
	assert.True(t, rollup.IsAggregate)
	assert.Equal(t, engineNameTimeWeightSum, rollup.ReturnType.EngineName)
}

func TestHeartbeatAccessorFamily(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	live := findOverload(catalogue, "live_ranges", []string{engineNameHeartbeat})
	require.NotNil(t, live, "live_ranges(heartbeat) must be registered as a set-returning function")
	assert.True(t, live.ReturnsSet)

	dead := findOverload(catalogue, "dead_ranges", []string{engineNameHeartbeat})
	require.NotNil(t, dead)
	assert.True(t, dead.ReturnsSet)

	uptime := findOverload(catalogue, "uptime", []string{engineNameHeartbeat})
	require.NotNil(t, uptime)
	assert.Equal(t, engineNameInterval, uptime.ReturnType.EngineName)

	downtime := findOverload(catalogue, "downtime", []string{engineNameHeartbeat})
	require.NotNil(t, downtime)
	assert.Equal(t, engineNameInterval, downtime.ReturnType.EngineName)

	liveAt := findOverload(catalogue, "live_at", []string{engineNameHeartbeat, engineNameTimestamptz})
	require.NotNil(t, liveAt)
	assert.Equal(t, engineNameBoolean, liveAt.ReturnType.EngineName)

	liveCount := findOverload(catalogue, "num_live_ranges", []string{engineNameHeartbeat})
	require.NotNil(t, liveCount)
	assert.Equal(t, engineNameBigint, liveCount.ReturnType.EngineName)

	gapCount := findOverload(catalogue, "num_gaps", []string{engineNameHeartbeat})
	require.NotNil(t, gapCount)
	assert.Equal(t, engineNameBigint, gapCount.ReturnType.EngineName)

	interpolatedUp := findOverload(catalogue, "interpolated_uptime",
		[]string{engineNameHeartbeat, engineNameHeartbeat})
	require.NotNil(t, interpolatedUp)
	assert.Equal(t, engineNameInterval, interpolatedUp.ReturnType.EngineName)

	interpolatedDown := findOverload(catalogue, "interpolated_downtime",
		[]string{engineNameHeartbeat, engineNameHeartbeat})
	require.NotNil(t, interpolatedDown)
	assert.Equal(t, engineNameInterval, interpolatedDown.ReturnType.EngineName)

	trimTo := findOverload(catalogue, "trim_to",
		[]string{engineNameHeartbeat, engineNameTimestamptz, engineNameInterval})
	require.NotNil(t, trimTo)
	assert.Equal(t, engineNameHeartbeat, trimTo.ReturnType.EngineName)
}

func TestStateAggAdditions(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	bigintAgg := findOverload(catalogue, "state_agg", []string{engineNameTimestamptz, engineNameBigint})
	require.NotNil(t, bigintAgg, "state_agg(timestamptz, bigint) must be registered")
	assert.True(t, bigintAgg.IsAggregate)
	assert.Equal(t, engineNameStateSummary, bigintAgg.ReturnType.EngineName)

	compactText := findOverload(catalogue, "compact_state_agg", []string{engineNameTimestamptz, engineNameText})
	require.NotNil(t, compactText)
	assert.True(t, compactText.IsAggregate)
	assert.Equal(t, engineNameCompactStateAgg, compactText.ReturnType.EngineName)

	compactBigint := findOverload(catalogue, "compact_state_agg", []string{engineNameTimestamptz, engineNameBigint})
	require.NotNil(t, compactBigint)
	assert.Equal(t, engineNameCompactStateAgg, compactBigint.ReturnType.EngineName)

	durationSlice := findOverload(catalogue, "duration_in", []string{
		engineNameStateSummary, engineNameText, engineNameTimestamptz, engineNameInterval,
	})
	require.NotNil(t, durationSlice, "duration_in(summary, state, start, duration) must be registered")
	assert.Equal(t, engineNameInterval, durationSlice.ReturnType.EngineName)

	timeline := findOverload(catalogue, "state_timeline", []string{engineNameStateSummary})
	require.NotNil(t, timeline)
	assert.True(t, timeline.ReturnsSet)

	intTimeline := findOverload(catalogue, "state_int_timeline", []string{engineNameStateSummary})
	require.NotNil(t, intTimeline)
	assert.True(t, intTimeline.ReturnsSet)

	periods := findOverload(catalogue, "state_periods", []string{engineNameStateSummary, engineNameText})
	require.NotNil(t, periods)
	assert.True(t, periods.ReturnsSet)

	intPeriods := findOverload(catalogue, "state_int_periods", []string{engineNameStateSummary, engineNameBigint})
	require.NotNil(t, intPeriods)
	assert.True(t, intPeriods.ReturnsSet)

	stateAt := findOverload(catalogue, "state_at", []string{engineNameStateSummary, engineNameTimestamptz})
	require.NotNil(t, stateAt)
	assert.Equal(t, engineNameText, stateAt.ReturnType.EngineName)

	stateAtInt := findOverload(catalogue, "state_at_int", []string{engineNameStateSummary, engineNameTimestamptz})
	require.NotNil(t, stateAtInt)
	assert.Equal(t, engineNameBigint, stateAtInt.ReturnType.EngineName)

	interpolatedTimeline := findOverload(catalogue, "interpolated_state_timeline", []string{
		engineNameStateSummary, engineNameTimestamptz, engineNameInterval,
		engineNameStateSummary, engineNameStateSummary,
	})
	require.NotNil(t, interpolatedTimeline)
	assert.True(t, interpolatedTimeline.ReturnsSet)

	interpolatedPeriods := findOverload(catalogue, "interpolated_state_periods", []string{
		engineNameStateSummary, engineNameText, engineNameTimestamptz, engineNameInterval,
		engineNameStateSummary, engineNameStateSummary,
	})
	require.NotNil(t, interpolatedPeriods)
	assert.True(t, interpolatedPeriods.ReturnsSet)

	interpolatedDuration := findOverload(catalogue, "interpolated_duration_in", []string{
		engineNameStateSummary, engineNameText, engineNameTimestamptz, engineNameInterval,
		engineNameStateSummary, engineNameStateSummary,
	})
	require.NotNil(t, interpolatedDuration)
	assert.Equal(t, engineNameInterval, interpolatedDuration.ReturnType.EngineName)
}

func TestRollupHasTimeWeightAndCompactAndHeartbeat(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	timeWeight := findOverload(catalogue, "rollup", []string{engineNameTimeWeightSum})
	require.NotNil(t, timeWeight, "rollup(time_weight_summary) must be registered")

	compact := findOverload(catalogue, "rollup", []string{engineNameCompactStateAgg})
	require.NotNil(t, compact, "rollup(compact_state_agg) must be registered")

	heartbeat := findOverload(catalogue, "rollup", []string{engineNameHeartbeat})
	require.NotNil(t, heartbeat, "rollup(heartbeat) must be registered")
}

func TestTimeWeightSummaryTypeRegistered(t *testing.T) {
	t.Parallel()

	types := timescaledb.New().BuiltinTypes()

	timeWeightType, exists := types.Types[engineNameTimeWeightSum]
	require.True(t, exists, "time_weight_summary opaque type must be registered")
	assert.Equal(t, sqlmodel.TypeCategoryUnknown, timeWeightType.Category)
	assert.Equal(t, engineNameTimeWeightSum, timeWeightType.EngineName)

	compactType, exists := types.Types[engineNameCompactStateAgg]
	require.True(t, exists, "compact_state_agg opaque type must be registered")
	assert.Equal(t, sqlmodel.TypeCategoryUnknown, compactType.Category)
}

func TestParseAndResolveTimeWeight(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(),
		"SELECT average(time_weight('Linear', ts, value)) FROM readings",
	)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	catalogue := timescaledb.New().BuiltinFunctions()

	timeWeight := findOverload(catalogue, "time_weight",
		[]string{engineNameText, engineNameTimestamptz, engineNameFloat8})
	require.NotNil(t, timeWeight)

	avg := findOverload(catalogue, "average", []string{engineNameTimeWeightSum})
	require.NotNil(t, avg)
}

func TestParseAndResolveHeartbeat(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(),
		"SELECT num_live_ranges(heartbeat_agg(ts, $1, $2, $3)) FROM pings",
	)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	catalogue := timescaledb.New().BuiltinFunctions()

	heartbeat := findOverload(catalogue, "heartbeat_agg",
		[]string{engineNameTimestamptz, engineNameTimestamptz, engineNameInterval, engineNameInterval})
	require.NotNil(t, heartbeat)
	assert.True(t, heartbeat.IsAggregate)

	liveCount := findOverload(catalogue, "num_live_ranges", []string{engineNameHeartbeat})
	require.NotNil(t, liveCount)
}

func TestParseAndResolveStateAgg(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(),
		"SELECT duration_in(state_agg(ts, status), 'ok') FROM events",
	)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	catalogue := timescaledb.New().BuiltinFunctions()

	textAgg := findOverload(catalogue, "state_agg", []string{engineNameTimestamptz, engineNameText})
	require.NotNil(t, textAgg)
	assert.True(t, textAgg.IsAggregate)

	bigintAgg := findOverload(catalogue, "state_agg", []string{engineNameTimestamptz, engineNameBigint})
	require.NotNil(t, bigintAgg, "bigint state_agg overload must resolve")
}

func TestFirstAndLastTakeTheirValuesType(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()
	for _, name := range []string{"first", "last"} {
		overloads := catalogue.Functions[name]
		require.NotEmpty(t, overloads, name)
		var polymorphic *sqlmodel.Function
		for _, overload := range overloads {
			if overload.Arguments[0].Type.Category == sqlmodel.TypeCategoryUnknown {
				polymorphic = overload
			}
		}
		require.NotNil(t, polymorphic, "%s has a polymorphic overload", name)
		assert.Equal(t, 1, polymorphic.ReturnTypeFromArgument, "%s returns the type of its value", name)
	}
}

func TestReorderPolicyFunctionsRegistered(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	add := findOverload(catalogue, "add_reorder_policy", []string{
		engineNameRegclass, engineNameText, engineNameBoolean, engineNameTimestamptz, engineNameText,
	})
	require.NotNil(t, add, "add_reorder_policy 5-arg form must be registered")
	assert.Equal(t, engineNameInteger, add.ReturnType.EngineName)

	remove := findOverload(catalogue, "remove_reorder_policy", []string{
		engineNameRegclass, engineNameBoolean,
	})
	require.NotNil(t, remove, "remove_reorder_policy 2-arg form must be registered")
	assert.Equal(t, engineNameVoid, remove.ReturnType.EngineName)
}

func TestAddJobUsesRegproc(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "add_job", []string{
		engineNameRegproc,
		engineNameInterval,
		engineNameJSONB,
		engineNameTimestamptz,
		engineNameBoolean,
		engineNameRegproc,
		engineNameBoolean,
		engineNameText,
	})
	require.NotNil(t, signature, "add_job 8-arg signature must be registered with regproc proc")
	assert.Equal(t, engineNameInteger, signature.ReturnType.EngineName)
}

func TestAlterJobReturnsRecord(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "alter_job", []string{
		engineNameInteger,
		engineNameInterval,
		engineNameInterval,
		engineNameInteger,
		engineNameInterval,
		engineNameBoolean,
		engineNameJSONB,
		engineNameTimestamptz,
		engineNameBoolean,
		engineNameRegproc,
		engineNameBoolean,
		engineNameTimestamptz,
		engineNameText,
	})
	require.NotNil(t, signature, "alter_job 13-arg signature must be registered")
	assert.Equal(t, engineNameAlterJobRecord, signature.ReturnType.EngineName)
}

func TestExtendedRetentionPolicy(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "add_retention_policy", []string{
		engineNameRegclass,
		engineNameAny,
		engineNameInterval,
		engineNameTimestamptz,
		engineNameText,
		engineNameBoolean,
		engineNameInterval,
	})
	require.NotNil(t, signature, "add_retention_policy 7-arg form must be registered")
	assert.Equal(t, engineNameInteger, signature.ReturnType.EngineName)
}

func TestExtendedCompressionPolicy(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "add_compression_policy", []string{
		engineNameRegclass,
		engineNameAny,
		engineNameBoolean,
		engineNameInterval,
		engineNameTimestamptz,
		engineNameText,
		engineNameInterval,
	})
	require.NotNil(t, signature, "add_compression_policy full form must be registered")
	assert.Equal(t, engineNameInteger, signature.ReturnType.EngineName)
	assert.Equal(t, 2, signature.MinArguments,
		"add_compression_policy must require only hypertable and compress_after, like the sibling policies")
}

func TestExtendedContinuousAggregatePolicy(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "add_continuous_aggregate_policy", []string{
		engineNameRegclass,
		engineNameAny,
		engineNameAny,
		engineNameInterval,
		engineNameBoolean,
		engineNameTimestamptz,
		engineNameText,
		engineNameBoolean,
		engineNameInteger,
		engineNameInteger,
		engineNameBoolean,
	})
	require.NotNil(t, signature, "add_continuous_aggregate_policy 11-arg form must be registered")
	assert.Equal(t, engineNameInteger, signature.ReturnType.EngineName)
}

func TestShowChunksMultiArgumentOverload(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "show_chunks", []string{
		engineNameRegclass,
		engineNameAny,
		engineNameAny,
		engineNameAny,
		engineNameAny,
	})
	require.NotNil(t, signature, "show_chunks multi-arg form must be registered")
	assert.Equal(t, engineNameRegclass, signature.ReturnType.EngineName)
	assert.True(t, signature.ReturnsSet, "show_chunks must return a set")
}

func TestDropChunksMultiArgumentOverload(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "drop_chunks", []string{
		engineNameRegclass,
		engineNameAny,
		engineNameAny,
		engineNameBoolean,
		engineNameAny,
		engineNameAny,
	})
	require.NotNil(t, signature, "drop_chunks multi-arg form must be registered")
	assert.True(t, signature.ReturnsSet, "drop_chunks must return a set")
}

func TestHypercoreProcedures(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	cases := []struct {
		name    string
		argList []string
	}{
		{name: "convert_to_columnstore", argList: []string{engineNameRegclass, engineNameBoolean, engineNameBoolean}},
		{name: "convert_to_rowstore", argList: []string{engineNameRegclass, engineNameBoolean}},
		{name: "recompress_chunk", argList: []string{engineNameRegclass, engineNameBoolean}},
		{name: "rebuild_columnstore", argList: []string{engineNameRegclass}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, testCase.name, testCase.argList)
			require.NotNil(t, signature, "%s must be registered", testCase.name)
			assert.Equal(t, engineNameVoid, signature.ReturnType.EngineName)
		})
	}
}

func TestColumnstoreStatsAliases(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	testCases := []struct {
		name   string
		record string
	}{
		{name: "hypertable_columnstore_stats", record: engineNameHypertableCompressionStatsRecord},
		{name: "chunk_columnstore_stats", record: engineNameChunkCompressionStatsRecord},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, testCase.name, []string{engineNameRegclass})
			require.NotNil(t, signature, "%s must be registered", testCase.name)
			assert.Equal(t, testCase.record, signature.ReturnType.EngineName)
			assert.True(t, signature.ReturnsSet, "%s must return a set", testCase.name)
		})
	}
}

func TestMoveChunkSignature(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	signature := findOverload(catalogue, "move_chunk", []string{
		engineNameRegclass,
		engineNameName,
		engineNameName,
		engineNameRegclass,
		engineNameBoolean,
	})
	require.NotNil(t, signature, "move_chunk 5-arg form must be registered")
	assert.Equal(t, engineNameVoid, signature.ReturnType.EngineName)
}

func TestTablespaceAttachDetach(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	attach := findOverload(catalogue, "attach_tablespace", []string{
		engineNameText, engineNameRegclass, engineNameBoolean,
	})
	require.NotNil(t, attach, "attach_tablespace must be registered")
	assert.Equal(t, engineNameVoid, attach.ReturnType.EngineName)

	detach := findOverload(catalogue, "detach_tablespace", []string{
		engineNameText, engineNameRegclass, engineNameBoolean,
	})
	require.NotNil(t, detach, "detach_tablespace must be registered")
	assert.Equal(t, engineNameInteger, detach.ReturnType.EngineName)

	detachAll := findOverload(catalogue, "detach_tablespaces", []string{engineNameRegclass})
	require.NotNil(t, detachAll, "detach_tablespaces must be registered")
	assert.Equal(t, engineNameInteger, detachAll.ReturnType.EngineName)
}

func TestMergeAndSplitChunks(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	pair := findOverload(catalogue, "merge_chunks", []string{
		engineNameRegclass, engineNameRegclass, engineNameBoolean,
	})
	require.NotNil(t, pair, "merge_chunks(chunk1, chunk2, concurrently) must be registered")

	arrayForm := findOverload(catalogue, "merge_chunks", []string{
		engineNameRegclassArray, engineNameBoolean,
	})
	require.NotNil(t, arrayForm, "merge_chunks(chunks[], concurrently) must be registered")

	concurrently := findOverload(catalogue, "merge_chunks_concurrently", []string{engineNameRegclassArray})
	require.NotNil(t, concurrently, "merge_chunks_concurrently must be registered")
	assert.True(t, concurrently.IsVariadic, "merge_chunks_concurrently must be variadic")

	split := findOverload(catalogue, "split_chunk", []string{engineNameRegclass, engineNameTimestamptz})
	require.NotNil(t, split, "split_chunk must be registered")

	attach := findOverload(catalogue, "attach_chunk", []string{
		engineNameRegclass, engineNameRegclass, engineNameJSONB,
	})
	require.NotNil(t, attach, "attach_chunk must be registered")

	detach := findOverload(catalogue, "detach_chunk", []string{engineNameRegclass})
	require.NotNil(t, detach, "detach_chunk must be registered")

	create := findOverload(catalogue, "create_chunk", []string{
		engineNameRegclass, engineNameJSONB, engineNameText, engineNameText, engineNameRegclass,
	})
	require.NotNil(t, create, "create_chunk must be registered")

	drop := findOverload(catalogue, "drop_chunk", []string{engineNameRegclass})
	require.NotNil(t, drop, "drop_chunk must be registered")
	assert.Equal(t, engineNameBoolean, drop.ReturnType.EngineName)
}

func TestHypertableApproximateSize(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	scalar := findOverload(catalogue, "hypertable_approximate_size", []string{engineNameRegclass})
	require.NotNil(t, scalar, "hypertable_approximate_size must be registered")
	assert.Equal(t, engineNameBigint, scalar.ReturnType.EngineName)

	detailed := findOverload(catalogue, "hypertable_approximate_detailed_size", []string{engineNameRegclass})
	require.NotNil(t, detailed, "hypertable_approximate_detailed_size must be registered")
	assert.True(t, detailed.ReturnsSet, "hypertable_approximate_detailed_size must return a set")
}

func TestFreqAggregateConstructors(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	freqBigint := findOverload(catalogue, "freq_agg", []string{engineNameFloat8, engineNameBigint})
	require.NotNil(t, freqBigint, "freq_agg(frequency, bigint) must be registered")
	assert.True(t, freqBigint.IsAggregate)
	assert.Equal(t, engineNameSpaceSavingBigintAgg, freqBigint.ReturnType.EngineName)

	freqText := findOverload(catalogue, "freq_agg", []string{engineNameFloat8, engineNameText})
	require.NotNil(t, freqText, "freq_agg(frequency, text) must be registered")
	assert.Equal(t, engineNameSpaceSavingTextAgg, freqText.ReturnType.EngineName)

	mcvBigint := findOverload(catalogue, "mcv_agg", []string{engineNameInteger, engineNameBigint})
	require.NotNil(t, mcvBigint, "mcv_agg(count, bigint) must be registered")
	assert.Equal(t, engineNameSpaceSavingBigintAgg, mcvBigint.ReturnType.EngineName)

	mcvText := findOverload(catalogue, "mcv_agg", []string{engineNameInteger, engineNameText})
	require.NotNil(t, mcvText, "mcv_agg(count, text) must be registered")
	assert.Equal(t, engineNameSpaceSavingTextAgg, mcvText.ReturnType.EngineName)

	mcvSkewBigint := findOverload(catalogue, "mcv_agg", []string{
		engineNameInteger, engineNameFloat8, engineNameBigint,
	})
	require.NotNil(t, mcvSkewBigint, "mcv_agg(count, skew, bigint) must be registered")

	rawFreq := findOverload(catalogue, "raw_freq_agg", []string{engineNameFloat8, engineNameAny})
	require.NotNil(t, rawFreq, "raw_freq_agg must be registered")
	assert.Equal(t, engineNameSpaceSavingAgg, rawFreq.ReturnType.EngineName)
}

func TestFreqAggregateAccessors(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	topn := findOverload(catalogue, "topn", []string{engineNameSpaceSavingBigintAgg, engineNameInteger})
	require.NotNil(t, topn, "topn(bigint_agg, n) must be registered")
	assert.True(t, topn.ReturnsSet)

	intoValues := findOverload(catalogue, "into_values", []string{engineNameSpaceSavingTextAgg})
	require.NotNil(t, intoValues, "into_values(text_agg) must be registered")
	assert.True(t, intoValues.ReturnsSet)

	maxFreq := findOverload(catalogue, "max_frequency", []string{engineNameSpaceSavingAgg, engineNameAny})
	require.NotNil(t, maxFreq, "max_frequency(agg, value) must be registered")
	assert.Equal(t, engineNameFloat8, maxFreq.ReturnType.EngineName)

	minFreq := findOverload(catalogue, "min_frequency", []string{engineNameSpaceSavingAgg, engineNameAny})
	require.NotNil(t, minFreq, "min_frequency(agg, value) must be registered")
	assert.Equal(t, engineNameFloat8, minFreq.ReturnType.EngineName)
}

func TestFrequencyRollup(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	for _, stateName := range []string{
		engineNameSpaceSavingBigintAgg,
		engineNameSpaceSavingTextAgg,
		engineNameSpaceSavingAgg,
	} {
		t.Run(stateName, func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, "rollup", []string{stateName})
			require.NotNil(t, signature, "rollup(%s) must be registered", stateName)
			assert.True(t, signature.IsAggregate)
			assert.Equal(t, stateName, signature.ReturnType.EngineName)
		})
	}
}

func TestMinMaxNAggregates(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	for _, valueType := range []string{engineNameFloat8, engineNameBigint, engineNameText, engineNameTimestamptz} {
		t.Run("min_n/"+valueType, func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, "min_n", []string{valueType, engineNameInteger})
			require.NotNil(t, signature, "min_n(%s, n) must be registered", valueType)
			assert.True(t, signature.IsAggregate)
			assert.Equal(t, engineNameMinNState, signature.ReturnType.EngineName)
		})
		t.Run("max_n/"+valueType, func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, "max_n", []string{valueType, engineNameInteger})
			require.NotNil(t, signature, "max_n(%s, n) must be registered", valueType)
		})
		t.Run("min_n_by/"+valueType, func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, "min_n_by", []string{valueType, engineNameAny, engineNameInteger})
			require.NotNil(t, signature, "min_n_by(%s, by, n) must be registered", valueType)
			assert.Equal(t, engineNameMinNByState, signature.ReturnType.EngineName)
		})
		t.Run("max_n_by/"+valueType, func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, "max_n_by", []string{valueType, engineNameAny, engineNameInteger})
			require.NotNil(t, signature, "max_n_by(%s, by, n) must be registered", valueType)
		})
	}
}

func TestMinNAccessors(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	intoArray := findOverload(catalogue, "into_array", []string{engineNameMinNState})
	require.NotNil(t, intoArray, "into_array(min_n_state) must be registered")
	assert.Equal(t, sqlmodel.TypeCategoryArray, intoArray.ReturnType.Category)

	intoValues := findOverload(catalogue, "into_values", []string{engineNameMinNState})
	require.NotNil(t, intoValues, "into_values(min_n_state) must be registered")
	assert.True(t, intoValues.ReturnsSet)
}

func TestTimevectorFamily(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	lttbAgg := findOverload(catalogue, "lttb", []string{
		engineNameTimestamptz, engineNameFloat8, engineNameInteger,
	})
	require.NotNil(t, lttbAgg, "lttb aggregate must be registered")
	assert.True(t, lttbAgg.IsAggregate)
	assert.Equal(t, engineNameTimevectorTstzF64, lttbAgg.ReturnType.EngineName)

	lttbState := findOverload(catalogue, "lttb", []string{engineNameTimevectorTstzF64, engineNameInteger})
	require.NotNil(t, lttbState, "lttb state-to-state form must be registered")
	assert.False(t, lttbState.IsAggregate)

	asapAgg := findOverload(catalogue, "asap_smooth", []string{
		engineNameTimestamptz, engineNameFloat8, engineNameInteger,
	})
	require.NotNil(t, asapAgg, "asap_smooth aggregate must be registered")
	assert.True(t, asapAgg.IsAggregate)

	timevector := findOverload(catalogue, "timevector", []string{engineNameTimestamptz, engineNameFloat8})
	require.NotNil(t, timevector, "timevector aggregate must be registered")

	rollup := findOverload(catalogue, "rollup", []string{engineNameTimevectorTstzF64})
	require.NotNil(t, rollup, "rollup(timevector_tstz_f64) must be registered")

	unnest := findOverload(catalogue, "unnest", []string{engineNameTimevectorTstzF64})
	require.NotNil(t, unnest, "unnest(timevector_tstz_f64) must be registered")
	assert.True(t, unnest.ReturnsSet)
}

func TestCountMinSketch(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	agg := findOverload(catalogue, "count_min_sketch", []string{
		engineNameText, engineNameFloat8, engineNameFloat8,
	})
	require.NotNil(t, agg, "count_min_sketch aggregate must be registered")
	assert.True(t, agg.IsAggregate)
	assert.Equal(t, engineNameCountMinSketch, agg.ReturnType.EngineName)

	approx := findOverload(catalogue, "approx_count", []string{engineNameText, engineNameCountMinSketch})
	require.NotNil(t, approx, "approx_count(item, sketch) must be registered")
	assert.Equal(t, engineNameBigint, approx.ReturnType.EngineName)
}

func TestSaturatingMath(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	for _, name := range []string{
		"saturating_add",
		"saturating_sub",
		"saturating_mul",
		"saturating_add_pos",
		"saturating_sub_pos",
		"saturating_mul_pos",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			signature := findOverload(catalogue, name, []string{engineNameInteger, engineNameInteger})
			require.NotNil(t, signature, "%s must be registered", name)
			assert.Equal(t, engineNameInteger, signature.ReturnType.EngineName)
		})
	}
}

func TestTimeBucketUUIDOverloads(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	bare := findOverload(catalogue, "time_bucket", []string{engineNameInterval, engineNameUUID})
	require.NotNil(t, bare, "time_bucket(interval, uuid) must be registered")
	assert.Equal(t, engineNameTimestamptz, bare.ReturnType.EngineName)

	withTimezone := findOverload(catalogue, "time_bucket", []string{engineNameInterval, engineNameUUID, engineNameText})
	require.NotNil(t, withTimezone, "time_bucket(interval, uuid, timezone) must be registered")

	origin := findOverload(catalogue, "time_bucket", []string{
		engineNameInterval, engineNameUUID, engineNameTimestamptz,
	})
	require.NotNil(t, origin, "time_bucket(interval, uuid, origin) must be registered")

	offset := findOverload(catalogue, "time_bucket", []string{
		engineNameInterval, engineNameUUID, engineNameInterval,
	})
	require.NotNil(t, offset, "time_bucket(interval, uuid, offset) must be registered")
}

func TestExtensionLifecycleFunctions(t *testing.T) {
	t.Parallel()

	catalogue := timescaledb.New().BuiltinFunctions()

	preRestore := findOverload(catalogue, "timescaledb_pre_restore", nil)
	require.NotNil(t, preRestore, "timescaledb_pre_restore must be registered")
	assert.Equal(t, engineNameVoid, preRestore.ReturnType.EngineName)

	postRestore := findOverload(catalogue, "timescaledb_post_restore", nil)
	require.NotNil(t, postRestore, "timescaledb_post_restore must be registered")
	assert.Equal(t, engineNameVoid, postRestore.ReturnType.EngineName)
}
