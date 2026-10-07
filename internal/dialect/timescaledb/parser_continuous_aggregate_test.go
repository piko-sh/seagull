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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/timescaledb"
	"seagull.sh/seagull/internal/fault"
)

func TestContinuousAggregate_Basic(t *testing.T) {
	t.Parallel()

	sql := `CREATE MATERIALIZED VIEW hourly_temps
		WITH (timescaledb.continuous = true)
		AS SELECT time_bucket('1 hour', ts) AS bucket, avg(temperature) FROM readings GROUP BY bucket`

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateView, mutation.Kind)
	assert.Equal(t, "hourly_temps", mutation.TableName)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_CONTINUOUS_AGGREGATE"])
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_CONTINUOUS_FLAG"])
	assert.NotEmpty(t, mutation.EngineSpecific["TIMESCALE_VIEW_BODY"])
}

func TestContinuousAggregate_ViewDefinitionTypesColumns(t *testing.T) {
	t.Parallel()

	sql := `CREATE MATERIALIZED VIEW hourly_temps
		WITH (timescaledb.continuous = true)
		AS SELECT time_bucket('1 hour', ts) AS bucket, avg(temperature) AS avg_temp FROM readings GROUP BY bucket`

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	require.NotNil(t, mutation.ViewDefinition, "continuous aggregate must carry a parsed view definition for column typing")
	require.Len(t, mutation.ViewDefinition.OutputColumns, 2)
	assert.Equal(t, "bucket", mutation.ViewDefinition.OutputColumns[0].Name)
	assert.Equal(t, "avg_temp", mutation.ViewDefinition.OutputColumns[1].Name)
}

func TestContinuousAggregate_MaterializedOnly(t *testing.T) {
	t.Parallel()

	sql := `CREATE MATERIALIZED VIEW daily_stats
		WITH (timescaledb.continuous = true, timescaledb.materialized_only = true)
		AS SELECT time_bucket('1 day', ts) AS bucket, count(*) FROM events GROUP BY bucket`

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_MATERIALIZED_ONLY"])
}

func TestContinuousAggregate_WithNoData(t *testing.T) {
	t.Parallel()

	sql := `CREATE MATERIALIZED VIEW hourly_temps
		WITH (timescaledb.continuous = true)
		AS SELECT time_bucket('1 hour', ts) FROM readings WITH NO DATA`

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "false", mutation.EngineSpecific["TIMESCALE_WITH_DATA"])
}

func TestContinuousAggregate_SchemaQualified(t *testing.T) {
	t.Parallel()

	sql := `CREATE MATERIALIZED VIEW analytics.hourly_temps
		WITH (timescaledb.continuous = true)
		AS SELECT 1`

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "analytics", mutation.SchemaName)
	assert.Equal(t, "hourly_temps", mutation.TableName)
}

func TestContinuousAggregate_CTEBody(t *testing.T) {
	t.Parallel()

	sql := `CREATE MATERIALIZED VIEW daily_summary
		WITH (timescaledb.continuous = true)
		AS WITH source AS (SELECT * FROM readings)
		SELECT time_bucket('1 day', ts) AS bucket FROM source GROUP BY bucket`

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	body := mutation.EngineSpecific["TIMESCALE_VIEW_BODY"]
	assert.Contains(t, body, "WITH source")
	assert.Contains(t, body, "GROUP BY bucket")
}

func TestContinuousAggregate_PreservesUnknownReloption(t *testing.T) {
	t.Parallel()

	sql := `CREATE MATERIALIZED VIEW hourly_temps
		WITH (timescaledb.continuous = true, timescaledb.future_option = 'value')
		AS SELECT 1`

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "value", mutation.EngineSpecific["TIMESCALE_RELOPTION_TIMESCALEDB.FUTURE_OPTION"],
		"unknown timescaledb.* reloptions must be preserved verbatim")
}

func TestContinuousAggregate_ViewBodyRoundTripsStringLiterals(t *testing.T) {
	t.Parallel()

	sql := `CREATE MATERIALIZED VIEW hourly_temps
		WITH (timescaledb.continuous = true)
		AS SELECT time_bucket('1 hour', ts) FROM readings WHERE kind = 'sensor'`

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	body := mutation.EngineSpecific["TIMESCALE_VIEW_BODY"]
	assert.Contains(t, body, "'1 hour'", "string literals must round-trip with surrounding quotes")
	assert.Contains(t, body, "'sensor'")
}

func TestContinuousAggregate_ViewBodyRoundTripsNonPlainLiterals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		description string
		body        string
		mustContain string
	}{
		{
			description: "bit-string literal keeps its B prefix and quotes",
			body:        `SELECT id FROM readings WHERE flags = B'1010'`,
			mustContain: "B'1010'",
		},
		{
			description: "dollar-quoted literal keeps its own tag",
			body:        `SELECT id FROM readings WHERE note = $tag$body$tag$`,
			mustContain: "$tag$body$tag$",
		},
		{
			description: "quoted identifier keeps its double quotes",
			body:        `SELECT "Weird Col" FROM readings`,
			mustContain: `"Weird Col"`,
		},
	}

	engine := timescaledb.New()

	for _, testCase := range cases {
		t.Run(testCase.description, func(t *testing.T) {
			t.Parallel()

			sql := "CREATE MATERIALIZED VIEW v\n" +
				"WITH (timescaledb.continuous = true)\n" +
				"AS " + testCase.body

			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)
			require.NotNil(t, mutation)

			body := mutation.EngineSpecific["TIMESCALE_VIEW_BODY"]
			assert.Contains(t, body, testCase.mustContain,
				"captured view body must re-wrap non-plain literals with their delimiters, got %q", body)
		})
	}
}

func TestContinuousAggregate_DoesNotClaimWhenContinuousMentionedInBody(t *testing.T) {
	t.Parallel()

	sql := `CREATE MATERIALIZED VIEW summary
		AS SELECT timescaledb.continuous FROM tsconfig`

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Empty(t, mutation.EngineSpecific["TIMESCALE_CONTINUOUS_AGGREGATE"])
}

func TestContinuousAggregate_RegularMaterializedViewUnaffected(t *testing.T) {
	t.Parallel()

	sql := `CREATE MATERIALIZED VIEW summary AS SELECT 1`

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Empty(t, mutation.EngineSpecific["TIMESCALE_CONTINUOUS_AGGREGATE"])
}

func TestContinuousAggregate_BareReloptionKeysMeanTrue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		withBody           string
		wantMaterialisedOn string
	}{
		{name: "bare continuous key", withBody: "(timescaledb.continuous)", wantMaterialisedOn: ""},
		{
			name:               "bare continuous and materialised-only keys",
			withBody:           "(timescaledb.continuous, timescaledb.materialized_only)",
			wantMaterialisedOn: "true",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sql := "CREATE MATERIALIZED VIEW hourly WITH " + testCase.withBody +
				" AS SELECT time_bucket('1 hour', ts) AS bucket, count(*) FROM events GROUP BY bucket"
			engine := timescaledb.New()
			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			mutation, err := engine.ApplyDDL(t.Context(), statements[0])

			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Equal(t, ast.MutationCreateView, mutation.Kind)
			assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_CONTINUOUS_AGGREGATE"])
			assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_CONTINUOUS_FLAG"])
			assert.Equal(t, testCase.wantMaterialisedOn, mutation.EngineSpecific["TIMESCALE_MATERIALIZED_ONLY"])
		})
	}
}

func TestContinuousAggregateWithDataClauseKeepsTypedColumns(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{"WITH NO DATA", "WITH DATA", ""} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()

			sql := `CREATE MATERIALIZED VIEW hourly_temperatures
				WITH (timescaledb.continuous = true, timescaledb.materialized_only = false)
				AS SELECT time_bucket('1 hour'::interval, ts) AS bucket, location_id, avg(temperature) AS mean_temperature,
					count(*) AS sample_count
				FROM temperatures
				GROUP BY bucket, location_id ` + suffix

			engine := timescaledb.New()
			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)

			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)
			require.NotNil(t, mutation)
			require.NotNil(t, mutation.ViewDefinition, "the body before WITH [NO] DATA must still be analysed")
			names := make([]string, 0, len(mutation.ViewDefinition.OutputColumns))
			for _, column := range mutation.ViewDefinition.OutputColumns {
				names = append(names, column.Name)
			}
			assert.Equal(t, []string{"bucket", "location_id", "mean_temperature", "sample_count"}, names)
		})
	}
}

func TestContinuousAggregate_ModernColumnstoreReloptions(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	sql := `CREATE MATERIALIZED VIEW hourly_temps
		WITH (timescaledb.continuous = true, timescaledb.enable_columnstore = true, timescaledb.segmentby = 'device_id', timescaledb.orderby = 'ts DESC')
		AS SELECT time_bucket('1 hour', ts) AS bucket, avg(temperature) FROM readings GROUP BY bucket`
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_COLUMNSTORE_ENABLED"])
	assert.Equal(t, "device_id", mutation.EngineSpecific["TIMESCALE_SEGMENTBY"])
	assert.Equal(t, "ts DESC", mutation.EngineSpecific["TIMESCALE_ORDERBY"])
}

func TestContinuousAggregateBodiesAreKeptWithoutTheirDataClause(t *testing.T) {
	t.Parallel()

	header := "CREATE MATERIALIZED VIEW v WITH (timescaledb.continuous) AS "
	testCases := []struct {
		name         string
		body         string
		wantBody     string
		wantWithData string
	}{
		{name: "WITH NO DATA", body: "SELECT 1 AS a WITH NO DATA", wantBody: "SELECT 1 AS a", wantWithData: "false"},
		{name: "WITH DATA", body: "SELECT 1 AS a\nWITH DATA", wantBody: "SELECT 1 AS a", wantWithData: "true"},
		{name: "no data clause", body: "SELECT 1 AS a", wantBody: "SELECT 1 AS a", wantWithData: "true"},
		{name: "a CTE named data", body: "WITH data AS (SELECT 1 AS a) SELECT a FROM data", wantBody: "WITH data AS (SELECT 1 AS a) SELECT a FROM data", wantWithData: "true"},
		{name: "a body spread over lines keeps its layout", body: "SELECT 1 AS a,\n       2 AS b\nWITH NO DATA", wantBody: "SELECT 1 AS a,\n       2 AS b", wantWithData: "false"},
		{name: "a body nested deeper than any capture limit", body: "SELECT " + strings.Repeat("(", 80) + "1" + strings.Repeat(")", 80) + " AS a", wantBody: "SELECT " + strings.Repeat("(", 80) + "1" + strings.Repeat(")", 80) + " AS a", wantWithData: "true"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyOne(t, timescaledb.New(), header+testCase.body)

			assert.Equal(t, testCase.wantBody, mutation.EngineSpecific["TIMESCALE_VIEW_BODY"])
			assert.Equal(t, testCase.wantWithData, mutation.EngineSpecific["TIMESCALE_WITH_DATA"])
		})
	}
}

func TestAMalformedContinuousAggregateFailsTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{name: "a body that is never closed", sql: "CREATE MATERIALIZED VIEW v WITH (timescaledb.continuous = true) AS SELECT (1", wantMessage: "expected ')' to close the parenthesised expression"},
		{name: "no AS", sql: "CREATE MATERIALIZED VIEW v WITH (timescaledb.continuous) SELECT 1", wantMessage: `continuous aggregate "v": missing AS clause`},
		{name: "options that are never closed", sql: "CREATE MATERIALIZED VIEW v WITH (timescaledb.continuous AS SELECT 1", wantMessage: "expected '=' after reloption key \"timescaledb.continuous\""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := timescaledb.New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)

			_, err = engine.ApplyDDL(t.Context(), statements[0])

			require.NotErrorIs(t, err, fault.ErrPanic)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}
