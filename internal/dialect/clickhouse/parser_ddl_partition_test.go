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

func TestDDL_AlterTablePartitionFreeze(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events FREEZE PARTITION '2026-01' WITH NAME 'snap1'")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTablePartition, mutation.Kind)
	assert.Equal(t, "FREEZE", mutation.EngineSpecific["PARTITION_OP"])
	assert.Equal(t, "PARTITION", mutation.EngineSpecific["PARTITION_TARGET"])
	assert.Contains(t, mutation.EngineSpecific["PARTITION_EXPR"], "2026-01")
	assert.Equal(t, "snap1", mutation.EngineSpecific["PARTITION_BACKUP_NAME"])
}

func TestDDL_AlterTablePartitionMove(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events MOVE PARTITION '2026-01' TO DISK 'cold'")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTablePartition, mutation.Kind)
	assert.Equal(t, "MOVE", mutation.EngineSpecific["PARTITION_OP"])
	assert.Contains(t, mutation.EngineSpecific["PARTITION_DEST"], "DISK")
	assert.Contains(t, mutation.EngineSpecific["PARTITION_DEST"], "cold")
}

func TestDDL_AlterTablePartitionAttachAllFrom(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events ATTACH PARTITION ALL FROM source_table")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTablePartition, mutation.Kind)
	assert.Equal(t, "ATTACH", mutation.EngineSpecific["PARTITION_OP"])
	assert.Equal(t, "ALL", mutation.EngineSpecific["PARTITION_EXPR"])
	assert.Equal(t, "source_table", mutation.EngineSpecific["PARTITION_FROM_TABLE"])
}

func TestDDL_AlterTablePartitionDropDetached(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events DROP DETACHED PARTITION '2026-01'")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTablePartition, mutation.Kind)
	assert.Equal(t, "DROP", mutation.EngineSpecific["PARTITION_OP"])
	assert.Equal(t, "true", mutation.EngineSpecific["PARTITION_DETACHED"])
}

func TestDDL_AlterTablePartitionDetachPart(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE events DETACH PART '2026-01_1_1_0'")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAlterTablePartition, mutation.Kind)
	assert.Equal(t, "DETACH", mutation.EngineSpecific["PARTITION_OP"])
	assert.Equal(t, "PART", mutation.EngineSpecific["PARTITION_TARGET"])
}
