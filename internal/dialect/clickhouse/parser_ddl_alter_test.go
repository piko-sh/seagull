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

func TestDDL_AlterAddProjection(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events ADD PROJECTION p_user (SELECT user_id, count() GROUP BY user_id)")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableAddProjection, mutation.Kind)
	assert.Equal(t, "p_user", mutation.EngineSpecific["PROJECTION_NAME"])
	assert.Contains(t, mutation.EngineSpecific["PROJECTION_SELECT"], "user_id")
}

func TestDDL_AlterDropProjection(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events DROP PROJECTION p_user")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableDropProjection, mutation.Kind)
	assert.Equal(t, "p_user", mutation.EngineSpecific["PROJECTION_NAME"])
}

func TestDDL_AlterMaterializeProjection(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events MATERIALIZE PROJECTION p_user IN PARTITION '2026-01'")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableMaterializeProjection, mutation.Kind)
	assert.Equal(t, "p_user", mutation.EngineSpecific["PROJECTION_NAME"])
	assert.Contains(t, mutation.EngineSpecific["MATERIALIZE_PARTITION"], "2026-01")
}

func TestDDL_AlterAddSkippingIndex(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events ADD INDEX idx col TYPE bloom_filter(0.01) GRANULARITY 2")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableAddSkippingIndex, mutation.Kind)
	assert.Equal(t, "idx", mutation.EngineSpecific["INDEX_NAME"])
	assert.Equal(t, "col", mutation.EngineSpecific["INDEX_EXPR"])
	assert.Contains(t, mutation.EngineSpecific["INDEX_TYPE"], "bloom_filter")
	assert.Equal(t, "2", mutation.EngineSpecific["INDEX_GRANULARITY"])
}

func TestDDL_AlterDropSkippingIndex(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events DROP INDEX idx")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableDropSkippingIndex, mutation.Kind)
	assert.Equal(t, "idx", mutation.EngineSpecific["INDEX_NAME"])
}

func TestDDL_AlterMaterializeIndex(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events MATERIALIZE INDEX idx")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableMaterializeIndex, mutation.Kind)
	assert.Equal(t, "idx", mutation.EngineSpecific["INDEX_NAME"])
}

func TestDDL_AlterAddConstraint(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events ADD CONSTRAINT chk_age CHECK age >= 0")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableAddConstraint, mutation.Kind)
	assert.Equal(t, "chk_age", mutation.EngineSpecific["CONSTRAINT_NAME"])
	assert.Contains(t, mutation.EngineSpecific["CONSTRAINT_CHECK"], "age")
}

func TestDDL_AlterDropConstraint(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events DROP CONSTRAINT chk_age")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableDropConstraint, mutation.Kind)
	assert.Equal(t, "chk_age", mutation.EngineSpecific["CONSTRAINT_NAME"])
}

func TestDDL_AlterAddStatistics(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events ADD STATISTICS a, b TYPE tdigest, uniq")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableAddStatistics, mutation.Kind)
	assert.Equal(t, "a,b", mutation.EngineSpecific["STATS_COLUMNS"])
	assert.Equal(t, "tdigest,uniq", mutation.EngineSpecific["STATS_TYPES"])
}

func TestDDL_AlterDropStatistics(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events DROP STATISTICS a, b")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableDropStatistics, mutation.Kind)
	assert.Equal(t, "a,b", mutation.EngineSpecific["STATS_COLUMNS"])
}

func TestDDL_AlterModifyStatistics(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events MODIFY STATISTICS a TYPE tdigest")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableModifyStatistics, mutation.Kind)
	assert.Equal(t, "a", mutation.EngineSpecific["STATS_COLUMNS"])
	assert.Equal(t, "tdigest", mutation.EngineSpecific["STATS_TYPES"])
}

func TestDDL_AlterMaterializeStatistics(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events MATERIALIZE STATISTICS a")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableMaterializeStatistics, mutation.Kind)
	assert.Equal(t, "a", mutation.EngineSpecific["STATS_COLUMNS"])
}

func TestDDL_AlterModifyQuery(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE mv MODIFY QUERY SELECT id, name FROM t")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableModifyQuery, mutation.Kind)
	assert.Contains(t, mutation.EngineSpecific["NEW_QUERY"], "SELECT")
	assert.Contains(t, mutation.EngineSpecific["NEW_QUERY"], "FROM")
}

func TestDDL_AlterModifyRefresh(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE mv MODIFY REFRESH EVERY 1 HOUR")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableModifyRefresh, mutation.Kind)
	assert.Equal(t, "REFRESH", mutation.EngineSpecific["MODIFY_REFRESH_KIND"])
	assert.Equal(t, "EVERY", mutation.EngineSpecific["MV_REFRESH_KIND"])
	assert.Contains(t, mutation.EngineSpecific["MV_REFRESH_INTERVAL"], "HOUR")
}

func TestDDL_AlterModifySQLSecurity(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE mv MODIFY SQL SECURITY INVOKER")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableModifyRefresh, mutation.Kind)
	assert.Equal(t, "SQL_SECURITY", mutation.EngineSpecific["MODIFY_REFRESH_KIND"])
	assert.Equal(t, "INVOKER", mutation.EngineSpecific["MV_SQL_SECURITY"])
}

func TestDDL_AlterModifyDefiner(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE mv MODIFY DEFINER = admin")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableModifyRefresh, mutation.Kind)
	assert.Equal(t, "DEFINER", mutation.EngineSpecific["MODIFY_REFRESH_KIND"])
	assert.Equal(t, "admin", mutation.EngineSpecific["MV_DEFINER"])
}

func TestDDL_AlterModifyColumnRemove(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events MODIFY COLUMN ts REMOVE DEFAULT")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableModifyColumn, mutation.Kind)
	assert.Equal(t, "ts", mutation.ColumnName)
	assert.Equal(t, "DEFAULT", mutation.EngineSpecific["COLUMN_REMOVE"])
}

func TestDDL_AlterModifyColumnModifyComment(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events MODIFY COLUMN ts MODIFY COMMENT 'updated timestamp'")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTableModifyColumn, mutation.Kind)
	assert.Equal(t, "updated timestamp", mutation.EngineSpecific["COLUMN_MODIFY_COMMENT"])
}
