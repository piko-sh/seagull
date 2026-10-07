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
	"seagull.sh/seagull/internal/dialect/timescaledb"
)

func TestCreateTable_PlainNoLift(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `CREATE TABLE widgets (id BIGINT, name TEXT)`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	assert.Empty(t, mutation.EngineSpecific["TIMESCALE_HYPERTABLE"])
}

func TestCreateTable_WithTsdbHypertableBareActivator(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	sql := `CREATE TABLE readings (ts TIMESTAMPTZ NOT NULL, value DOUBLE PRECISION) WITH (tsdb.hypertable)`
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	assert.Equal(t, "readings", mutation.TableName)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_HYPERTABLE"])
}

func TestCreateTable_WithTsdbHypertableFullForm(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	sql := `CREATE TABLE readings (ts TIMESTAMPTZ NOT NULL, value DOUBLE PRECISION)
		WITH (tsdb.hypertable, tsdb.partition_column = 'ts', tsdb.chunk_interval = '1 day')`
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_HYPERTABLE"])

	assert.Equal(t, "ts", mutation.EngineSpecific["TIMESCALE_TIME_COLUMN"])
	assert.Equal(t, "1 day", mutation.EngineSpecific["TIMESCALE_CHUNK_TIME_INTERVAL"])
}

func TestCreateTable_WithTimescaledbHypertableForm(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	sql := `CREATE TABLE readings (ts TIMESTAMPTZ NOT NULL, value DOUBLE PRECISION)
		WITH (timescaledb.hypertable, timescaledb.partition_column = 'ts', timescaledb.segmentby = 'device_id', timescaledb.enable_columnstore = true)`
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_HYPERTABLE"])
	assert.Equal(t, "ts", mutation.EngineSpecific["TIMESCALE_TIME_COLUMN"])
	assert.Equal(t, "device_id", mutation.EngineSpecific["TIMESCALE_SEGMENTBY"])
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_COLUMNSTORE_ENABLED"])
}

func TestCreateTable_WithUnrelatedRelopt(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	sql := `CREATE TABLE widgets (id BIGINT, name TEXT) WITH (fillfactor = 80)`
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	assert.Empty(t, mutation.EngineSpecific["TIMESCALE_HYPERTABLE"])
}

func TestCreateTableHypertableOptionsAreLiftedFromTheStorageParameters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		want map[string]string
		name string
		sql  string
	}{
		{
			name: "string values lose their quotes and unknown keys are kept",
			sql:  "CREATE TABLE r (ts timestamptz) WITH (tsdb.hypertable, tsdb.partition_column = 'ts', tsdb.orderby = 'ts DESC', tsdb.future = 'it''s')",
			want: map[string]string{
				"TIMESCALE_HYPERTABLE":            "true",
				"TIMESCALE_TIME_COLUMN":           "ts",
				"TIMESCALE_ORDERBY":               "ts DESC",
				"TIMESCALE_RELOPTION_TSDB.FUTURE": "it's",
			},
		},
		{
			name: "a value of several tokens keeps its spelling",
			sql:  "CREATE TABLE r (ts timestamptz) WITH (timescaledb.hypertable, timescaledb.chunk_interval = INTERVAL '1 day')",
			want: map[string]string{
				"TIMESCALE_HYPERTABLE":          "true",
				"TIMESCALE_CHUNK_TIME_INTERVAL": "INTERVAL '1 day'",
			},
		},
		{
			name: "keys written in capitals",
			sql:  "CREATE TABLE r (ts timestamptz) WITH (TSDB.Hypertable, TSDB.Segmentby = 'device')",
			want: map[string]string{
				"TIMESCALE_HYPERTABLE": "true",
				"TIMESCALE_SEGMENTBY":  "device",
			},
		},
		{
			name: "an activator inside a string is no activator",
			sql:  "CREATE TABLE r (ts timestamptz) WITH (note = 'tsdb.hypertable')",
			want: map[string]string{},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyOne(t, timescaledb.New(), testCase.sql)

			lifted := map[string]string{}
			for key, value := range mutation.EngineSpecific {
				if strings.HasPrefix(key, "TIMESCALE_") {
					lifted[key] = value
				}
			}
			assert.Equal(t, testCase.want, lifted)
		})
	}
}
