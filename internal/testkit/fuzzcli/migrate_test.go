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

package fuzzcli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingWorkspace struct {
	files    map[string]string
	status   string
	commands [][]string
}

func (workspace *recordingWorkspace) Write(name, content string) { workspace.files[name] = content }

func (workspace *recordingWorkspace) Read(name string) string { return workspace.files[name] }

func (workspace *recordingWorkspace) Remove(name string) { delete(workspace.files, name) }

func (workspace *recordingWorkspace) Run(arguments ...string) Result {
	workspace.commands = append(workspace.commands, arguments)
	return Result{Stdout: workspace.status, Stderr: "", Code: 0}
}

func newModel(t *testing.T, migrations int, downs map[int]bool) *migrateModel {
	t.Helper()
	workspace := &recordingWorkspace{files: map[string]string{}, status: "", commands: nil}
	model := &migrateModel{workspace: workspace, observe: nil, hasDown: map[int]bool{}, edited: map[int]bool{},
		dialect: DialectFor("sqlite"), applied: nil, files: 0, seedRows: 0, seeded: false}
	for version := 1; version <= migrations; version++ {
		model.addMigration(downs[version])
	}
	return model
}

func TestMigrateSeedsCoverEveryStartingShape(t *testing.T) {
	seeds := MigrateSeeds()
	require.Len(t, seeds, maxMigrations*3)
	for _, seed := range seeds {
		require.Equal(t, byte(migrateVersion), seed[0])
		require.Len(t, seed, migrateHeader+len(seedOperations))
	}
}

func TestDialectForWritesEngineSpecificTables(t *testing.T) {
	assert.Equal(t, "CREATE TABLE m2 (id INTEGER);\n", DialectFor("postgres").CreateTable(2))
	assert.Contains(t, DialectFor("clickhouse").CreateTable(2), "ENGINE = MergeTree")
	assert.Equal(t, "DROP TABLE m3;\n", DialectFor("sqlite").DropTable(3))
	assert.Equal(t, "db/migrations/0004_m4.down.sql", migrationFile(4, directionDown))
}

func TestModelRefusesRollbacksWithoutDownFiles(t *testing.T) {
	model := newModel(t, 3, map[int]bool{2: true, 3: true})
	require.True(t, model.up())
	require.Equal(t, []int{1, 2, 3}, model.applied)
	require.True(t, model.down(2))
	require.Equal(t, []int{1}, model.applied)
	require.False(t, model.down(1), "migration 1 has no down file")
	require.False(t, model.to(0))
	require.True(t, model.to(3))
	require.Equal(t, []int{1, 2, 3}, model.applied)
}

func TestModelBlocksChangedMigrationsAndTracksSeedRows(t *testing.T) {
	model := newModel(t, 2, map[int]bool{1: true, 2: true})
	require.False(t, model.seed(), "the seed needs m1")
	require.True(t, model.up())
	require.True(t, model.seed())
	require.True(t, model.seed(), "a recorded seed does not run again")
	require.Equal(t, 1, model.seedRows)
	require.True(t, model.reseed())
	require.Equal(t, 2, model.seedRows)
	model.runEdit(t, 0)
	require.True(t, model.changed())
	require.Equal(t, "changed", model.state(1))
	require.False(t, model.up())
	require.False(t, model.down(1))
	require.False(t, model.to(0))
	model.edited = map[int]bool{}
	require.True(t, model.to(0))
	require.Zero(t, model.seedRows, "dropping m1 drops the seed's rows")
	require.Equal(t, "pending", model.state(1))
}

func TestModelCapsAddedMigrations(t *testing.T) {
	model := newModel(t, maxMigrations, nil)
	for range maxAddedMigrations + 2 {
		model.runAdd(t, 0)
	}
	require.Equal(t, maxMigrations+maxAddedMigrations, model.files)
}

func TestOperateDispatchesEveryOperationKind(t *testing.T) {
	model := newModel(t, 2, map[int]bool{1: true, 2: true})
	model.observe = func() Observation { return Observation{Tables: nil, SeedRows: 0} }
	for choice := range byte(operationKinds) {
		if choice == operationValidate || choice == operationEdit || choice == operationTo || choice == operationDown {
			continue
		}
		model.operate(t, choice)
	}
	workspace, ok := model.workspace.(*recordingWorkspace)
	require.True(t, ok)
	require.Contains(t, workspace.commands, []string{migrateCommand, directionUp})
	require.Contains(t, workspace.commands, []string{seedCommand, "apply"})
	require.Contains(t, workspace.commands, []string{migrateCommand, "status"})
}

func TestRunMigrateSkipsAnotherEnvelope(t *testing.T) {
	workspace := &recordingWorkspace{files: map[string]string{}, status: "", commands: nil}
	skipped := t.Run("envelope", func(t *testing.T) {
		RunMigrate(t, []byte{migrateVersion + 1, 1, 0}, workspace, DialectFor("sqlite"), nil)
	})
	require.True(t, skipped)
	require.Empty(t, workspace.commands)
}

func TestCheckComparesTablesSeedRowsAndStatus(t *testing.T) {
	model := newModel(t, 2, nil)
	require.True(t, model.up())
	require.True(t, model.seed())
	model.edited[2] = true
	workspace, ok := model.workspace.(*recordingWorkspace)
	require.True(t, ok)
	workspace.status = "1 m1 applied\n2 m2 changed\n"
	model.observe = func() Observation { return Observation{Tables: []string{"m2", "m1"}, SeedRows: 1} }
	model.check(t)
}
