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

package sqlexec

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
)

func TestAppendOnlyRepairRecordsProgressAnUpResumesFrom(t *testing.T) {
	t.Parallel()

	store := newProgressStore()
	executor := newEditingExecutor(t, store, ClickHouse())
	record := boatsRecord()
	statements, err := ClickHouse().Split(string(record.Content))
	require.NoError(t, err)

	require.NoError(t, executor.RecordProgress(t.Context(), record, 2))
	assert.Equal(t, seedProgressRow{checksum: record.Checksum, last: 1}, store.migrationProgress[record.Version])
	_, exists := store.row(record.Version)
	assert.False(t, exists, "a partly applied migration has no history row on an append-only history")

	require.NoError(t, executor.ExecuteMigration(t.Context(), record, migrate.MigrationDirectionUp, false))
	assert.False(t, slices.Contains(store.executed, statements[0]), "a statement the interrupted run completed does not run again")
	assert.False(t, slices.Contains(store.executed, statements[1]), "a statement the interrupted run completed does not run again")
	for _, statement := range statements[2:] {
		assert.Contains(t, store.executed, statement)
	}
	assert.Equal(t, int64(-1), store.migrationProgress[record.Version].last, "a completed migration starts from the beginning when it runs again")
	_, exists = store.row(record.Version)
	assert.True(t, exists)
}

func TestAppendOnlyMigrationChangedSinceItStoppedIsNotResumed(t *testing.T) {
	t.Parallel()

	store := newProgressStore()
	store.migrationProgress = map[int64]seedProgressRow{1: {checksum: "an earlier file", last: 1}}
	executor := newEditingExecutor(t, store, ClickHouse())

	err := executor.ExecuteMigration(t.Context(), boatsRecord(), migrate.MigrationDirectionUp, false)
	var mismatch *migrate.ResumeMismatchError
	require.ErrorAs(t, err, &mismatch)
	assert.Equal(t, 1, mismatch.LastStatement)
	assert.Empty(t, store.rows, "nothing is recorded applied")
}

func TestAppendOnlyForceResetsProgress(t *testing.T) {
	t.Parallel()

	store := newProgressStore()
	store.migrationProgress = map[int64]seedProgressRow{1: {checksum: "an earlier file", last: 3}}
	executor := newEditingExecutor(t, store, ClickHouse())

	require.NoError(t, executor.RecordApplied(t.Context(), boatsRecord()))
	assert.Equal(t, int64(-1), store.migrationProgress[1].last, "a forced migration leaves no progress for a later run to resume from")
}
