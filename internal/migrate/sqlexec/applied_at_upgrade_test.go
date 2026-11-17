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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryTimestampColumnMovesToDatetimeInUTC(t *testing.T) {
	t.Parallel()

	store := newProgressStore()
	store.timestampAppliedAt = true
	database := store.open()
	t.Cleanup(func() { _ = database.Close() })
	executor, err := NewExecutor(database, MySQL())
	require.NoError(t, err)

	require.NoError(t, executor.EnsureMigrationTable(t.Context()))
	executed := store.executedStatements()
	at := func(prefix string) int {
		return slices.IndexFunc(executed, func(statement string) bool { return strings.HasPrefix(statement, prefix) })
	}
	saved, utc, alter, restored := at("SET @sgl_time_zone"), at("SET time_zone = '+00:00'"), at("ALTER TABLE sgl_migrations MODIFY applied_at DATETIME(6)"), at("SET time_zone = @sgl_time_zone")
	require.True(t, saved >= 0 && utc > saved && alter > utc && restored > alter,
		"the zone is saved, set to UTC, the column changed and the zone restored, in that order: %v", executed)
	assert.False(t, store.timestampAppliedAt)

	store.resetExecuted()
	require.NoError(t, executor.EnsureMigrationTable(t.Context()))
	assert.Equal(t, -1, slices.IndexFunc(store.executedStatements(), func(statement string) bool {
		return strings.Contains(statement, "MODIFY applied_at")
	}), "a table already holding DATETIME is left alone")
}
