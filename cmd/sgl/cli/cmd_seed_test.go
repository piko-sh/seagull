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

package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedLifecycle(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	requireCode(t, project.Run("migrate", "up", "--dsn", dsn), 0)

	result := project.Run("seed", "status", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "1        authors  pending  -")

	result = project.Run("seed", "apply", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "1 applied\n", result.Stdout)

	result = project.Run("seed", "status", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "authors  applied  20")

	project.Write("db/seeds/0001_authors.sql", "INSERT INTO authors (name) VALUES ('Grace Hopper');\n")
	result = project.Run("seed", "status", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "changed")

	result = project.Run("seed", "apply", "--dsn", dsn)
	requireCode(t, result, 0)
	require.Equal(t, "0 applied\n", result.Stdout)
	require.Contains(t, result.Stderr, "level=WARN")
	result = project.Run("--log-level", "error", "seed", "apply", "--dsn", dsn)
	requireCode(t, result, 0)
	require.Empty(t, result.Stderr)

	result = project.Run("seed", "reseed", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "1 applied\n", result.Stdout)

	requireCode(t, project.Run("seed", "--dsn", dsn), 2)
	requireCode(t, project.Run("seed", "plant", "--dsn", dsn), 2)

	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: sqlite\n")
	result = project.Run("seed", "status", "--dsn", dsn)
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "no seeds directory")
}

func TestSeedFailureIsADatabaseError(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	result := project.Run("seed", "apply", "--dsn", dsn)
	requireCode(t, result, 4)
	require.NotEmpty(t, result.Stderr)
}
