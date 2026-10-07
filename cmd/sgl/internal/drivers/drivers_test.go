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

package drivers

import (
	"bytes"
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/cli"
)

func dialectDrivers(t *testing.T, options ...cli.Option) map[string]string {
	t.Helper()

	var stdout, stderr bytes.Buffer
	options = append(options, cli.WithIO(nil, &stdout, &stderr), cli.WithDirectory(t.TempDir()))
	code := cli.MainContext(t.Context(), []string{"dialects"}, options...)
	require.Zero(t, code, stderr.String())

	drivers := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n")[1:] {
		fields := strings.Fields(line)
		drivers[fields[0]] = fields[len(fields)-1]
	}
	return drivers
}

func TestOptionsGiveEveryStockDialectARegisteredDriver(t *testing.T) {
	t.Parallel()

	expected := map[string]string{
		"clickhouse":  "clickhouse",
		"cockroachdb": "pgx",
		"duckdb":      "none",
		"mariadb":     "mysql",
		"mysql":       "mysql",
		"postgres":    "pgx",
		"sqlite":      "sqlite",
		"timescaledb": "pgx",
	}
	require.Equal(t, expected, dialectDrivers(t, Options()...))

	registered := sql.Drivers()
	for _, name := range stock {
		require.True(t, slices.Contains(registered, name), "driver %q is not registered", name)
	}
}

func TestCLIWithoutOptionsHasNoDriver(t *testing.T) {
	t.Parallel()

	for dialectName, driver := range dialectDrivers(t) {
		require.Equal(t, "none", driver, dialectName)
	}
}

func TestLaterOptionsReplaceTheStockDriver(t *testing.T) {
	t.Parallel()

	drivers := dialectDrivers(t, append(Options(), cli.WithDriver("cockroachdb", "custom"))...)

	require.Equal(t, "custom", drivers["cockroachdb"])
	require.Equal(t, "pgx", drivers["postgres"])
}
