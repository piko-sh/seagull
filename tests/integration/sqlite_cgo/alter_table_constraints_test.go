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
//go:build integration

package sqlite_cgo_test

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundledSQLiteAppliesAlterTableConstraints(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })

	var version string
	require.NoError(t, database.QueryRowContext(t.Context(), "SELECT sqlite_version()").Scan(&version))
	t.Logf("bundled SQLite %s", version)

	_, err = database.ExecContext(t.Context(),
		"CREATE TABLE gauges (id INTEGER PRIMARY KEY, level INT CONSTRAINT level_present NOT NULL, CONSTRAINT level_bound CHECK (level < 100))")
	require.NoError(t, err)
	for _, statement := range []string{
		"ALTER TABLE gauges ADD CONSTRAINT level_positive CHECK (level > 0)",
		"ALTER TABLE gauges ADD CHECK (level > 1)",
		"ALTER TABLE gauges DROP CONSTRAINT level_present",
		"ALTER TABLE gauges DROP CONSTRAINT level_bound",
		"ALTER TABLE gauges ALTER COLUMN level SET NOT NULL",
	} {
		_, err := database.ExecContext(t.Context(), statement)
		assert.NoError(t, err, statement)
	}

	var definition string
	require.NoError(t, database.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_schema WHERE name = 'gauges'").Scan(&definition))
	assert.Contains(t, definition, "CONSTRAINT level_positive CHECK (level > 0), CHECK (level > 1)")
	assert.NotContains(t, definition, "level_bound")
}
