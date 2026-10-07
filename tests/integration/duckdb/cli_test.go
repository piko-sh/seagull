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

package duckdb_test

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

type sglResult struct {
	stdout string
	stderr string
	code   int
}

func runSgl(t *testing.T, directory string, arguments ...string) sglResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.MainContext(t.Context(), arguments,
		cli.WithDriver("duckdb", "duckdb"),
		cli.WithIO(nil, &stdout, &stderr),
		cli.WithDirectory(directory),
	)
	return sglResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func requireSgl(t *testing.T, directory string, code int, arguments ...string) sglResult {
	t.Helper()
	result := runSgl(t, directory, arguments...)
	require.Equal(t, code, result.code, "sgl %v\nstdout:\n%s\nstderr:\n%s", arguments, result.stdout, result.stderr)
	return result
}

func TestSglMigratesAndSeedsDuckDBWithItsDriverLinked(t *testing.T) {
	directory := t.TempDir()
	dsn := filepath.Join(directory, "app.duckdb")
	requireSgl(t, directory, 0, "init", "--dialect", "duckdb")

	result := requireSgl(t, directory, 0, "migrate", "status", "--dsn", dsn)
	require.Contains(t, result.stdout, "1        init  pending  -")

	result = requireSgl(t, directory, 0, "migrate", "up", "--dsn", dsn)
	require.Equal(t, "up 1 init\n1 applied\n", result.stdout)

	result = requireSgl(t, directory, 0, "migrate", "status", "--dsn", dsn)
	require.Contains(t, result.stdout, "init  applied  20")
	requireSgl(t, directory, 0, "migrate", "validate", "--dsn", dsn)

	result = requireSgl(t, directory, 0, "seed", "status", "--dsn", dsn)
	require.Contains(t, result.stdout, "authors  pending  -")
	result = requireSgl(t, directory, 0, "seed", "apply", "--dsn", dsn)
	require.Equal(t, "1 applied\n", result.stdout)
	result = requireSgl(t, directory, 0, "seed", "status", "--dsn", dsn)
	require.Contains(t, result.stdout, "authors  applied  20")
	require.Equal(t, 1, countAuthors(t, dsn))

	result = requireSgl(t, directory, 2, "schema", "dump", "--from-db", dsn)
	require.Contains(t, result.stderr, "dialect duckdb cannot be introspected")

	result = requireSgl(t, directory, 0, "migrate", "down", "--dsn", dsn)
	require.Equal(t, "down 1 init\n1 rolled back\n", result.stdout)
	result = requireSgl(t, directory, 0, "migrate", "status", "--dsn", dsn)
	require.Contains(t, result.stdout, "1        init  pending  -")
}

func countAuthors(t *testing.T, dsn string) int {
	t.Helper()

	database, err := sql.Open("duckdb", dsn)
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	return harness.CountOf(t, database, "SELECT count(*) FROM authors")
}
