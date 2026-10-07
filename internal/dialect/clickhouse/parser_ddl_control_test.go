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

func TestDDL_ExplainDescribeCheckTableReadOnly(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"EXPLAIN PLAN SELECT 1",
		"DESCRIBE TABLE users",
		"DESC users",
		"CHECK TABLE users",
		"CHECK TABLE users PARTITION '2026-01'",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()
			mutation, err := applyDDL(t, sql)
			require.NoError(t, err)
			assert.Nil(t, mutation, "expected read-only catalogue no-op")
		})
	}
}

func TestDDL_BackupRestore(t *testing.T) {
	t.Parallel()

	backupMutation, err := applyDDL(t, "BACKUP TABLE users TO Disk('archive', 'snap1')")
	require.NoError(t, err)
	require.NotNil(t, backupMutation)
	assert.Equal(t, ast.MutationBackup, backupMutation.Kind)
	assert.Contains(t, backupMutation.EngineSpecific["STATEMENT_BODY"], "users")

	restoreMutation, err := applyDDL(t, "RESTORE TABLE users FROM Disk('archive', 'snap1')")
	require.NoError(t, err)
	require.NotNil(t, restoreMutation)
	assert.Equal(t, ast.MutationRestore, restoreMutation.Kind)
}

func TestDDL_KillQueryKillMutation(t *testing.T) {
	t.Parallel()

	killQueryMutation, err := applyDDL(t, "KILL QUERY WHERE query_id = 'abc' SYNC")
	require.NoError(t, err)
	require.NotNil(t, killQueryMutation)
	assert.Equal(t, ast.MutationKillQuery, killQueryMutation.Kind)

	killMutationMutation, err := applyDDL(t, "KILL MUTATION WHERE mutation_id = 'abc'")
	require.NoError(t, err)
	require.NotNil(t, killMutationMutation)
	assert.Equal(t, ast.MutationKillMutation, killMutationMutation.Kind)
}

func TestDDL_AttachDetachTable(t *testing.T) {
	t.Parallel()

	attachMutation, err := applyDDL(t, "ATTACH TABLE users")
	require.NoError(t, err)
	require.NotNil(t, attachMutation)
	assert.Equal(t, ast.MutationAttachTable, attachMutation.Kind)
	assert.Equal(t, "users", attachMutation.TableName)

	detachMutation, err := applyDDL(t, "DETACH TABLE users PERMANENTLY SYNC")
	require.NoError(t, err)
	require.NotNil(t, detachMutation)
	assert.Equal(t, ast.MutationDetachTable, detachMutation.Kind)
	assert.Equal(t, "users", detachMutation.TableName)
}

func TestDetachRecordsWhetherItSaidIfExists(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantKind     ast.MutationKind
		wantIfExists bool
	}{
		{name: "a table", sql: "DETACH TABLE events", wantKind: ast.MutationDetachTable, wantIfExists: false},
		{name: "a table IF EXISTS", sql: "DETACH TABLE IF EXISTS db.events PERMANENTLY", wantKind: ast.MutationDetachTable, wantIfExists: true},
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
