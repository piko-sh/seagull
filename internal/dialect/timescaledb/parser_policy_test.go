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
)

func TestPolicy_AddCompressionPolicy(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT add_compression_policy('readings', INTERVAL '7 days')`)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableAlterColumn, mutation.Kind)
	assert.Equal(t, "readings", mutation.TableName)
	assert.Equal(t, "add_compression_policy", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "readings", mutation.EngineSpecific["TIMESCALE_TABLE"])
	assert.Contains(t, mutation.EngineSpecific["TIMESCALE_POLICY_ARGS"], "INTERVAL")
}

func TestPolicy_AddColumnstorePolicy(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT add_columnstore_policy('readings', after => INTERVAL '7 days')`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "add_columnstore_policy", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "readings", mutation.TableName)
}

func TestPolicy_RemoveCompressionPolicy(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT remove_compression_policy('readings')`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "remove_compression_policy", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "readings", mutation.TableName)
}

func TestPolicy_AddRetentionPolicy(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT add_retention_policy('readings', INTERVAL '30 days')`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "add_retention_policy", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "readings", mutation.TableName)
}

func TestPolicy_RemoveRetentionPolicy(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT remove_retention_policy('readings')`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "remove_retention_policy", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
}

func TestPolicy_AddContinuousAggregatePolicy(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	sql := `SELECT add_continuous_aggregate_policy('hourly_temps', start_offset => INTERVAL '1 month', end_offset => INTERVAL '1 hour', schedule_interval => INTERVAL '1 hour')`
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "add_continuous_aggregate_policy", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "hourly_temps", mutation.TableName)
	assert.Contains(t, mutation.EngineSpecific["TIMESCALE_POLICY_ARGS"], "start_offset")
}

func TestPolicy_RemoveContinuousAggregatePolicy(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT remove_continuous_aggregate_policy('hourly_temps')`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "remove_continuous_aggregate_policy", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
}

func TestPolicy_AddReorderPolicy(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT add_reorder_policy('readings', 'readings_ts_device_idx')`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "add_reorder_policy", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "readings", mutation.TableName)
}

func TestPolicy_AddJob(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT add_job('my_proc', '1h', config => '{"a":1}'::jsonb)`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "add_job", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "my_proc", mutation.EngineSpecific["TIMESCALE_JOB_PROC"])
}

func TestPolicy_AlterJob(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT alter_job(1000, scheduled => false)`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "alter_job", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "1000", mutation.EngineSpecific["TIMESCALE_JOB_ID"])
}

func TestPolicy_DeleteJob(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT delete_job(1000)`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "delete_job", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "1000", mutation.EngineSpecific["TIMESCALE_JOB_ID"])
}

func TestPolicy_RunJob(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT run_job(1000)`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "run_job", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "1000", mutation.EngineSpecific["TIMESCALE_JOB_ID"])
}

func TestPolicy_AddDimensionLegacyColumn(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT add_dimension('readings', 'device_id', number_partitions => 4)`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "add_dimension", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "readings", mutation.TableName)
	assert.Equal(t, "device_id", mutation.EngineSpecific["TIMESCALE_TIME_COLUMN"])
	assert.Empty(t, mutation.EngineSpecific["TIMESCALE_DIMENSION_BUILDER"])
}

func TestPolicy_AddDimensionChunkTimeInterval(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT add_dimension('readings', 'ts', chunk_time_interval => INTERVAL '1 day')`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "add_dimension", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "readings", mutation.TableName)
	assert.Equal(t, "ts", mutation.EngineSpecific["TIMESCALE_TIME_COLUMN"])
	assert.Contains(t, mutation.EngineSpecific["TIMESCALE_POLICY_ARGS"], "chunk_time_interval")
}

func TestPolicy_AddDimensionByRange(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT add_dimension('readings', by_range('device_id', INTERVAL '1 day'))`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "by_range", mutation.EngineSpecific["TIMESCALE_DIMENSION_BUILDER"])
	assert.Equal(t, "device_id", mutation.EngineSpecific["TIMESCALE_TIME_COLUMN"])
}

func TestPolicy_AddDimensionByHash(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT add_dimension('readings', by_hash('device_id', 4))`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "by_hash", mutation.EngineSpecific["TIMESCALE_DIMENSION_BUILDER"])
	assert.Equal(t, "device_id", mutation.EngineSpecific["TIMESCALE_TIME_COLUMN"])
}

func TestPolicy_SetChunkTimeInterval(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT set_chunk_time_interval('readings', INTERVAL '12 hours')`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "set_chunk_time_interval", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "readings", mutation.TableName)
}

func TestPolicy_SetIntegerNowFunc(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT set_integer_now_func('readings', 'now_epoch')`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "set_integer_now_func", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
	assert.Equal(t, "readings", mutation.TableName)
}

func TestPolicy_EnableChunkSkipping(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT enable_chunk_skipping('readings', 'device_id')`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "enable_chunk_skipping", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
}

func TestPolicy_DisableChunkSkipping(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT disable_chunk_skipping('readings', 'device_id')`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "disable_chunk_skipping", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
}

func TestSelectFormJobCallsKeepTheirParametersInQueries(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	for _, sql := range []string{
		"SELECT delete_job($1)",
		"SELECT run_job($1)",
		"SELECT alter_job($1, scheduled => $2)",
		"SELECT add_retention_policy('conditions', drop_after => $1)",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
			require.NoError(t, err)
			require.NotNil(t, analysis)
			require.NotEmpty(t, analysis.ParameterReferences)
			assert.Equal(t, 1, analysis.ParameterReferences[0].Number)
		})
	}
}
