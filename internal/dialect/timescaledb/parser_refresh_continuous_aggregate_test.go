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

func TestRefreshContinuousAggregate_Call(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `CALL refresh_continuous_aggregate('hourly_temps', '2020-01-01', '2021-01-01')`)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableAlterColumn, mutation.Kind)
	assert.Equal(t, "hourly_temps", mutation.TableName)
	assert.Equal(t, "hourly_temps", mutation.EngineSpecific["TIMESCALE_REFRESH_CONTINUOUS_AGGREGATE_TARGET"])
	assert.Equal(t, "'2020-01-01'", mutation.EngineSpecific["TIMESCALE_REFRESH_WINDOW_START"])
	assert.Equal(t, "'2021-01-01'", mutation.EngineSpecific["TIMESCALE_REFRESH_WINDOW_END"])
	assert.Equal(t, "refresh_continuous_aggregate", mutation.EngineSpecific["TIMESCALE_POLICY_OP"])
}

func TestRefreshContinuousAggregate_NullWindow(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `CALL refresh_continuous_aggregate('hourly_temps', NULL, NULL)`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "NULL", mutation.EngineSpecific["TIMESCALE_REFRESH_WINDOW_START"])
	assert.Equal(t, "NULL", mutation.EngineSpecific["TIMESCALE_REFRESH_WINDOW_END"])
}

func TestRefreshContinuousAggregate_DoesNotClaimBareCall(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `CALL some_other_proc('x')`)
	require.NoError(t, err)
	require.NotEmpty(t, statements)
}
