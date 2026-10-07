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

func TestDDL_CreateUserDropUser(t *testing.T) {
	t.Parallel()

	createMutation, err := applyDDL(t, "CREATE USER alice IDENTIFIED BY 'secret'")
	require.NoError(t, err)
	require.NotNil(t, createMutation)
	assert.Equal(t, ast.MutationCreateUser, createMutation.Kind)
	assert.Equal(t, "alice", createMutation.EngineSpecific["USER_NAME"])

	dropMutation, err := applyDDL(t, "DROP USER alice")
	require.NoError(t, err)
	require.NotNil(t, dropMutation)
	assert.Equal(t, ast.MutationDropUser, dropMutation.Kind)
	assert.Equal(t, "alice", dropMutation.EngineSpecific["USER_NAME"])
}

func TestDDL_CreateRoleAlterRoleDropRole(t *testing.T) {
	t.Parallel()

	createMutation, err := applyDDL(t, "CREATE ROLE admin")
	require.NoError(t, err)
	require.NotNil(t, createMutation)
	assert.Equal(t, ast.MutationCreateRole, createMutation.Kind)
	assert.Equal(t, "admin", createMutation.EngineSpecific["ROLE_NAME"])

	alterMutation, err := applyDDL(t, "ALTER ROLE admin SETTINGS max_memory_usage = 1000")
	require.NoError(t, err)
	require.NotNil(t, alterMutation)
	assert.Equal(t, ast.MutationAlterRole, alterMutation.Kind)

	dropMutation, err := applyDDL(t, "DROP ROLE admin")
	require.NoError(t, err)
	require.NotNil(t, dropMutation)
	assert.Equal(t, ast.MutationDropRole, dropMutation.Kind)
}

func TestDDL_CreateRowPolicy(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE ROW POLICY p1 ON users USING id = 1 TO alice")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreatePolicy, mutation.Kind)
	assert.Equal(t, "p1", mutation.EngineSpecific["POLICY_NAME"])
}

func TestDDL_CreateQuota(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE QUOTA q1 FOR INTERVAL 1 day MAX queries = 100")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateQuota, mutation.Kind)
	assert.Equal(t, "q1", mutation.EngineSpecific["QUOTA_NAME"])
}

func TestDDL_CreateSettingsProfile(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE SETTINGS PROFILE p1 SETTINGS max_memory_usage = 1000")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateSettingsProfile, mutation.Kind)
	assert.Equal(t, "p1", mutation.EngineSpecific["PROFILE_NAME"])
}

func TestDDL_GrantRevoke(t *testing.T) {
	t.Parallel()

	grantMutation, err := applyDDL(t, "GRANT SELECT ON db.t TO alice")
	require.NoError(t, err)
	require.NotNil(t, grantMutation)
	assert.Equal(t, ast.MutationGrantManagement, grantMutation.Kind)
	assert.Equal(t, "GRANT", grantMutation.EngineSpecific["RBAC_KIND"])
	assert.Contains(t, grantMutation.EngineSpecific["STATEMENT_BODY"], "SELECT")

	revokeMutation, err := applyDDL(t, "REVOKE SELECT ON db.t FROM alice")
	require.NoError(t, err)
	require.NotNil(t, revokeMutation)
	assert.Equal(t, ast.MutationGrantManagement, revokeMutation.Kind)
	assert.Equal(t, "REVOKE", revokeMutation.EngineSpecific["RBAC_KIND"])
}

func TestRBAC_CreateUserOrReplaceParsesName(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `CREATE USER OR REPLACE alice IDENTIFIED BY 'secret'`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "alice", mutation.EngineSpecific[engineKeyUserName])
}

func TestRBAC_CreateUserIfNotExistsStillParses(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `CREATE USER IF NOT EXISTS bob`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "bob", mutation.EngineSpecific[engineKeyUserName])
}

func TestRBACDropAndAlterRecordWhetherTheySaidIfExists(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantKind     ast.MutationKind
		wantIfExists bool
	}{
		{name: "a user", sql: "DROP USER alice", wantKind: ast.MutationDropUser, wantIfExists: false},
		{name: "a user IF EXISTS", sql: "DROP USER IF EXISTS alice", wantKind: ast.MutationDropUser, wantIfExists: true},
		{name: "a role altered IF EXISTS", sql: "ALTER ROLE IF EXISTS reader SETTINGS max_memory_usage = 1000", wantKind: ast.MutationAlterRole, wantIfExists: true},
		{name: "a policy IF EXISTS", sql: "DROP ROW POLICY IF EXISTS p1 ON t", wantKind: ast.MutationDropPolicy, wantIfExists: true},
		{name: "a settings profile IF EXISTS", sql: "DROP SETTINGS PROFILE IF EXISTS p1", wantKind: ast.MutationDropSettingsProfile, wantIfExists: true},
		{name: "a settings profile", sql: "DROP SETTINGS PROFILE p1", wantKind: ast.MutationDropSettingsProfile, wantIfExists: false},
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
