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

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/timescaledb"
	"seagull.sh/seagull/internal/fault"
)

func TestCompression_AlterTableEnable(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "ALTER TABLE readings SET (timescaledb.compress = true)")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "readings", mutation.TableName)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_COMPRESSION_ENABLED"])
}

func TestCompression_AlterTableSegmentByAndOrderBy(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	sql := `ALTER TABLE readings SET (
		timescaledb.compress = true,
		timescaledb.compress_segmentby = 'device_id',
		timescaledb.compress_orderby = 'ts DESC'
	)`
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_COMPRESSION_ENABLED"])
	assert.Equal(t, "device_id", mutation.EngineSpecific["TIMESCALE_COMPRESSION_SEGMENTBY"])
	assert.Equal(t, "ts DESC", mutation.EngineSpecific["TIMESCALE_COMPRESSION_ORDERBY"])
}

func TestCompression_AlterMaterializedView(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	sql := `ALTER MATERIALIZED VIEW hourly_temps SET (timescaledb.compress = true, timescaledb.compress_segmentby = 'bucket')`
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "hourly_temps", mutation.TableName)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_ALTER_MATERIALIZED"])
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_COMPRESSION_ENABLED"])
	assert.Equal(t, "bucket", mutation.EngineSpecific["TIMESCALE_COMPRESSION_SEGMENTBY"])
}

func TestCompression_AlterTableSetSchemaIsNotCompression(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "ALTER TABLE readings SET SCHEMA analytics")
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Empty(t, mutation.EngineSpecific["TIMESCALE_COMPRESSION_ENABLED"])
}

func TestCompression_AlterTableBareCompressKeyMeansTrue(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "ALTER TABLE readings SET (timescaledb.compress)")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])

	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_COMPRESSION_ENABLED"])
}

func TestCompression_ModernColumnstoreReloptions(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	sql := `ALTER TABLE readings SET (
		timescaledb.enable_columnstore = true,
		timescaledb.segmentby = 'device_id',
		timescaledb.orderby = 'ts DESC'
	)`
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_COLUMNSTORE_ENABLED"])
	assert.Equal(t, "device_id", mutation.EngineSpecific["TIMESCALE_SEGMENTBY"])
	assert.Equal(t, "ts DESC", mutation.EngineSpecific["TIMESCALE_ORDERBY"])
}

func TestACompressionChangeWithoutItsSetClauseIsALocatedSyntaxError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{
			name:        "a word between SET and the compression keys",
			sql:         "ALTER TABLE r SET WITHOUT (timescaledb.compress)",
			wantMessage: `compression alter "r": missing SET (timescaledb...) clause`,
		},
		{
			name:        "the compression keys after another SET's options",
			sql:         "ALTER MATERIALIZED VIEW v SET (fillfactor = 70), SET CLUSTER (timescaledb.compress)",
			wantMessage: `compression alter "v": missing SET (timescaledb...) clause`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := timescaledb.New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)

			_, err = engine.ApplyDDL(t.Context(), statements[0])

			require.ErrorIs(t, err, fault.ErrParse)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}
