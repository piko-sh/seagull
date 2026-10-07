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

package clickhouse_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate/migrate_backend_clickhouse"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

func prepareCase(ctx context.Context, t *testing.T, caseDirectory string) []string {
	t.Helper()

	database, dsn := harness.IsolatedDatabase(t, "clickhouse", testConnectionString)
	applyMigrations(ctx, t, caseDirectory, database)
	return []string{"DATABASE_URL=" + dsn}
}

func applyMigrations(ctx context.Context, t *testing.T, caseDirectory string, database *sql.DB) {
	t.Helper()

	backend := migrate_backend_clickhouse.New()
	for _, migration := range harness.CaseMigrations(t, caseDirectory) {
		statements, splitErr := backend.Split(migration.Content)
		require.NoError(t, splitErr, "splitting %s", migration.Name)
		for _, statement := range statements {
			_, execErr := database.ExecContext(ctx, statement)
			require.NoError(t, execErr, "applying %s: %s", migration.Name, statement)
		}
	}
}
