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

package fuzzcase

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeLifecycleRefusesAnotherVersionAndAShortInput(t *testing.T) {
	t.Parallel()

	_, ok := DecodeLifecycle("sqlite", []byte{ExecutionVersion, 0, 0, 0})
	assert.False(t, ok)
	_, ok = DecodeLifecycle("sqlite", []byte{lifecycleVersion, 0, 0})
	assert.False(t, ok)
}

func TestEveryLifecycleSeedDecodesOnEveryEngine(t *testing.T) {
	t.Parallel()

	for _, engine := range []string{"postgres", "sqlite", "duckdb", "mysql", "mariadb", "clickhouse", "cockroachdb", "timescaledb"} {
		for _, seed := range LifecycleSeeds() {
			model, ok := DecodeLifecycle(engine, seed)
			require.True(t, ok, "%s %v", engine, seed)
			require.NotEmpty(t, model.Steps)
			assert.False(t, model.Migrations[0].Hidden, "the seeds fill the first migration's table, so it is never hidden")
		}
	}
}

func TestDecodeLifecycleReadsTheLayout(t *testing.T) {
	t.Parallel()

	migrations := []byte{
		lifecycleMigrationByte(2, lifecycleCRLF, 0),
		lifecycleMigrationByte(1, lifecycleBOM, lifecycleHiddenBit|lifecycleWideGapBit|lifecycleNoDownBit|lifecycleNoTransactionBit),
	}
	model, ok := DecodeLifecycle("postgres", lifecycleInput(2, lifecycleMaxSeeds, 1, lifecycleFailure(1, 2, true), migrations, lifecycleUp))

	require.True(t, ok)
	require.Len(t, model.Migrations, 2)
	assert.Equal(t, lifecycleMaxSeeds, model.Seeds)
	first, second := model.Migrations[0], model.Migrations[1]
	assert.Equal(t, LifecycleMigration{Version: 1, Inserts: 2, Encoding: lifecycleCRLF, FailAt: 2, Down: true, DownFails: true, NoTransaction: false, Hidden: false}, first)
	assert.Equal(t, LifecycleMigration{Version: 1 + lifecycleWideGap, Inserts: 1, Encoding: lifecycleBOM, FailAt: -1, Down: false, DownFails: false, NoTransaction: true, Hidden: true}, second)
	assert.Equal(t, []LifecycleStep{{Operation: "up", Target: 0}}, model.Steps)
}

func TestAFailedTransactionalMigrationLeavesNothingBehind(t *testing.T) {
	t.Parallel()

	model, ok := DecodeLifecycle("postgres", lifecycleInput(1, 1, 0, lifecycleFailure(1, 2, false), []byte{lifecycleMigrationByte(2, lifecycleLF, 0)}, lifecycleUp))
	require.True(t, ok)
	state := model.InitialState()

	outcome := state.Advance(&model, LifecycleStep{Operation: "up", Target: 0})

	assert.Equal(t, LifecycleOutcome{Count: 0, Fails: true}, outcome)
	assert.Equal(t, LifecycleMigrationState{}, state.Migrations[0])
}

func TestAFailedMigrationWithoutATransactionResumesAfterTheFix(t *testing.T) {
	t.Parallel()

	model, ok := DecodeLifecycle("mysql", lifecycleInput(1, 1, 0, lifecycleFailure(1, 2, false), []byte{lifecycleMigrationByte(2, lifecycleLF, 0)}, lifecycleUp))
	require.True(t, ok)
	state := model.InitialState()

	assert.Equal(t, LifecycleOutcome{Count: 0, Fails: true}, state.Advance(&model, LifecycleStep{Operation: "up", Target: 0}))
	assert.Equal(t, LifecycleMigrationState{Rows: []int64{1}, Completed: 2, Applied: false, Dirty: true, RollingBack: false, HasTable: true}, state.Migrations[0])
	assert.Equal(t, LifecycleOutcome{Count: 0, Fails: true}, state.Advance(&model, LifecycleStep{Operation: "down", Target: 1}),
		"a rollback is refused while a migration is dirty")
	state.Advance(&model, LifecycleStep{Operation: "fix", Target: 0})

	assert.Equal(t, LifecycleOutcome{Count: 1, Fails: false}, state.Advance(&model, LifecycleStep{Operation: "up", Target: 0}))
	assert.Equal(t, []int64{1, 2}, state.Migrations[0].Rows, "the resumed run inserts each row once")
	assert.False(t, state.Migrations[0].Dirty)
}

func TestLifecycleFilesEncodeAndHideAsTheMigrationsSay(t *testing.T) {
	t.Parallel()

	migrations := []byte{lifecycleMigrationByte(0, lifecycleCRLF, 0), lifecycleMigrationByte(0, lifecycleBOM, lifecycleHiddenBit)}
	model, ok := DecodeLifecycle("sqlite", lifecycleInput(2, 1, 0, 0, migrations))
	require.True(t, ok)

	hidden := model.Files(false)
	assert.Contains(t, string(hidden["migrations/0000_life_0.up.sql"]), "CREATE TABLE fuzz_life_0 (id BIGINT NOT NULL);\r\n")
	assert.NotContains(t, hidden, "migrations/0001_life_1.up.sql")
	revealed := model.Files(true)
	assert.True(t, strings.HasPrefix(string(revealed["migrations/0001_life_1.up.sql"]), "\xef\xbb\xbf"))
	assert.Equal(t, "INSERT INTO fuzz_life_0 (id) VALUES (101);\n", string(revealed["seeds/0001_rows.sql"]))
}
