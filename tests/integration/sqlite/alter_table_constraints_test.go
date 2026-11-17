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

package sqlite_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const alterTableConstraintsMigration = `
CREATE TABLE gauges (
	id INTEGER PRIMARY KEY,
	reading INT CONSTRAINT reading_present NOT NULL,
	level INT NOT NULL,
	depth INT CONSTRAINT depth_positive CHECK (depth > 0),
	scale INT CONSTRAINT scale_default DEFAULT 1 NOT NULL,
	CONSTRAINT level_bound CHECK (level < 100)
);
ALTER TABLE gauges ADD CONSTRAINT level_positive CHECK (level > 0);
ALTER TABLE gauges ADD CHECK (reading > 0);
ALTER TABLE gauges DROP CONSTRAINT reading_present;
ALTER TABLE gauges DROP CONSTRAINT depth_positive;
ALTER TABLE gauges DROP CONSTRAINT level_bound;
ALTER TABLE gauges ALTER COLUMN level DROP NOT NULL;
ALTER TABLE gauges ALTER depth SET NOT NULL;
ALTER TABLE gauges ADD COLUMN offset_value INT CONSTRAINT offset_present NOT NULL DEFAULT 0;
`

func TestAlterTableConstraintsReplayAsTheEngineAppliesThem(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	directory := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(directory, "migrations"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "migrations", "001_gauges.up.sql"), []byte(alterTableConstraintsMigration), 0o600))
	database := applyMigrations(t, directory)
	replayed := replayCatalogue(t, os.DirFS(directory))

	assert.Empty(t, compareMainSchemas(replayed, introspectCatalogue(t, database)))

	table := mainSchema(replayed).Tables["gauges"]
	require.NotNil(t, table)
	var checks []string
	for index := range table.Constraints {
		if table.Constraints[index].Kind.String() == "check" {
			checks = append(checks, table.Constraints[index].Name)
		}
	}
	assert.Equal(t, []string{"level_positive", ""}, checks)

	var definition string
	require.NoError(t, database.QueryRowContext(t.Context(), "SELECT sql FROM sqlite_schema WHERE name = 'gauges'").Scan(&definition))
	assert.Contains(t, definition, "CONSTRAINT level_positive CHECK (level > 0), CHECK (reading > 0)")
	assert.NotContains(t, definition, "level_bound")
}
