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

package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestDDL_CreateMaterializedViewRefreshEvery(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE MATERIALIZED VIEW mv
		REFRESH EVERY 1 HOUR OFFSET 5 MINUTE
		ENGINE = MergeTree() ORDER BY id
		AS SELECT id FROM t
	`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "EVERY", mutation.EngineSpecific["MV_REFRESH_KIND"])
	assert.Contains(t, mutation.EngineSpecific["MV_REFRESH_INTERVAL"], "HOUR")
	assert.Contains(t, mutation.EngineSpecific["MV_REFRESH_OFFSET"], "MINUTE")
}

func TestDDL_CreateMaterializedViewRefreshAfter(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE MATERIALIZED VIEW mv
		REFRESH AFTER 1 DAY
		RANDOMIZE FOR 1 HOUR
		DEPENDS ON other_mv
		APPEND
		ENGINE = MergeTree() ORDER BY id
		AS SELECT id FROM t
	`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "AFTER", mutation.EngineSpecific["MV_REFRESH_KIND"])
	assert.Contains(t, mutation.EngineSpecific["MV_REFRESH_AFTER"], "DAY")
	assert.Contains(t, mutation.EngineSpecific["MV_REFRESH_RANDOMIZE"], "HOUR")
	assert.Equal(t, "other_mv", mutation.EngineSpecific["MV_REFRESH_DEPENDS_ON"])
	assert.Equal(t, "true", mutation.EngineSpecific["MV_REFRESH_APPEND"])
}

func TestDDL_CreateMaterializedViewEmptyAndSecurity(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE MATERIALIZED VIEW mv
		ENGINE = MergeTree() ORDER BY id
		EMPTY
		DEFINER = admin
		SQL SECURITY DEFINER
		COMMENT 'a refresh view'
		AS SELECT id FROM t
	`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "true", mutation.EngineSpecific["MV_REFRESH_EMPTY"])
	assert.Equal(t, "admin", mutation.EngineSpecific["MV_DEFINER"])
	assert.Equal(t, "DEFINER", mutation.EngineSpecific["MV_SQL_SECURITY"])
	assert.Equal(t, "a refresh view", mutation.EngineSpecific["COMMENT"])
}

func TestDDL_CreateMaterializedViewToTarget(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE MATERIALIZED VIEW mv TO target_table
		AS SELECT id FROM t
	`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateView, mutation.Kind)
	assert.Equal(t, "true", mutation.EngineSpecific["MATERIALIZED"])
	assert.Equal(t, "target_table", mutation.EngineSpecific["MATERIALIZED_TARGET"])
}

func TestDDL_CreateMaterializedViewToQualifiedTarget(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE MATERIALIZED VIEW analytics.mv TO analytics.target
		AS SELECT id FROM analytics.source
	`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "mv", mutation.TableName)
	assert.Equal(t, "analytics", mutation.SchemaName)
	assert.Equal(t, "target", mutation.EngineSpecific["MATERIALIZED_TARGET"])
}

func TestDropDictionaryRecordsWhetherItSaidIfExists(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantKind     ast.MutationKind
		wantIfExists bool
	}{
		{name: "a dictionary", sql: "DROP DICTIONARY lookups", wantKind: ast.MutationDropDictionary, wantIfExists: false},
		{name: "a dictionary IF EXISTS", sql: "DROP DICTIONARY IF EXISTS db.lookups", wantKind: ast.MutationDropDictionary, wantIfExists: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Equal(t, testCase.wantKind, mutation.Kind)
			assert.Equal(t, testCase.wantIfExists, mutation.IfExists)
		})
	}
}
