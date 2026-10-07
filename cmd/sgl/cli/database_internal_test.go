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

package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_cockroachdb"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_duckdb"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_mysql"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_timescaledb"
)

func TestConnectionBackends(t *testing.T) {
	t.Parallel()

	tests := []struct {
		dialect string
		lock    string
		history string
	}{
		{dialect: "postgres", lock: "advisory", history: "app_history"},
		{dialect: "cockroachdb", lock: "table"},
		{dialect: "timescaledb", lock: "advisory"},
		{dialect: "mysql", lock: "none"},
		{dialect: "mariadb", lock: "advisory"},
		{dialect: "clickhouse"},
		{dialect: "sqlite", lock: "none"},
		{dialect: "duckdb", lock: "none", history: "app_history"},
	}
	for _, testCase := range tests {
		configured := &config.Database{}
		configured.Name, configured.Dialect = "app", testCase.dialect
		configured.Migrate.Namespace, configured.Migrate.Lock, configured.Migrate.HistoryTable = "app", testCase.lock, testCase.history
		built, err := dialect_registry.New(testCase.dialect, dialect_registry.Options{})
		require.NoError(t, err)
		opened := &connection{dialect: built, configured: configured, dsn: "user:pass@tcp(localhost)/app"}
		backend, err := opened.backend()
		require.NoError(t, err, testCase.dialect)
		assert.Equal(t, "app_seeds", backend.Names().Seeds, testCase.dialect)
		assert.Len(t, opened.bookkeepingTables(), 4)
	}

	for dialectName, wantUnsupported := range map[string]bool{"cockroachdb": true, "postgres": false, "duckdb": true} {
		configured := &config.Database{}
		configured.Name, configured.Dialect = "app", dialectName
		configured.Migrate.Namespace, configured.Migrate.Lock = "app", "advisory"
		built, err := dialect_registry.New(dialectName, dialect_registry.Options{})
		require.NoError(t, err)
		backend, err := (&connection{dialect: built, configured: configured}).backend()
		require.NoError(t, err)
		_, err = migrate.NewMigrator(nil, backend, fstest.MapFS{}, ".")
		assert.Equal(t, wantUnsupported, errors.Is(err, migrate.ErrUnsupportedLock), dialectName)
	}
}

func TestTheMigrationBackendSplitsAsTheDialectOptionsSay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ansiQuotes *bool
		name       string
		message    string
	}{
		{name: "unset", ansiQuotes: nil, message: "unterminated string literal"},
		{name: "off", ansiQuotes: new(false), message: "unterminated string literal"},
		{name: "on", ansiQuotes: new(true), message: "unterminated quoted identifier"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			configured := &config.Database{}
			configured.Name, configured.Dialect = "app", "mysql"
			configured.DialectOptions.ANSIQuotes = testCase.ansiQuotes
			built, err := dialect_registry.New("mysql", dialect_registry.Options{ANSIQuotes: testCase.ansiQuotes})
			require.NoError(t, err)
			backend, err := (&connection{dialect: built, configured: configured}).backend()
			require.NoError(t, err)
			_, err = backend.Split(`SELECT "open; SELECT 2;`)
			require.ErrorContains(t, err, testCase.message)
		})
	}
}

func TestBackendIsChosenByTheDialectsNameNotTheConfigurationKey(t *testing.T) {
	t.Parallel()

	configured := &config.Database{}
	configured.Name, configured.Dialect, configured.Migrate.Lock = "app", "crdb", "advisory"
	built, err := dialect_registry.New("cockroachdb", dialect_registry.Options{})
	require.NoError(t, err)

	backend, err := (&connection{dialect: built, configured: configured}).backend()
	require.NoError(t, err)
	_, err = migrate.NewMigrator(nil, backend, fstest.MapFS{}, ".")
	require.ErrorIs(t, err, migrate.ErrUnsupportedLock, "a CockroachDB dialect registered as crdb gets the CockroachDB backend, which has no advisory lock")
}

func TestBackendWithoutAFactoryNamesWithMigrationBackend(t *testing.T) {
	t.Parallel()

	configured := &config.Database{}
	configured.Name, configured.Dialect = "app", "exotic"
	built, err := dialect_registry.New("sqlite", dialect_registry.Options{})
	require.NoError(t, err)
	opened := &connection{dialect: fakedialect.Renamed(built, "exotic", "exotic"), configured: configured, program: defaultProgram}

	_, err = opened.backend()
	require.EqualError(t, err, `database "app": dialect exotic (named "exotic", family "exotic") has no migration backend; `+
		"a custom sgl registers one with cli.WithMigrationBackend")
	assert.Equal(t, exitUsage, exitCodeFor(err))
	assert.Nil(t, opened.bookkeepingTables())
}

func TestMigrationBackendFor(t *testing.T) {
	t.Parallel()

	registered := func(...migrate.BackendOption) migrate.Backend { return migrate_backend_sqlite.New() }
	tests := []struct {
		registered map[string]MigrationBackendFactory
		want       MigrationBackendFactory
		name       string
		dialect    string
		family     string
	}{
		{name: "stock backend of the name", dialect: "cockroachdb", family: "postgres", want: migrate_backend_cockroachdb.New},
		{name: "stock backend of a flavour", dialect: "timescaledb", family: "postgres", want: migrate_backend_timescaledb.New},
		{name: "MariaDB takes the MySQL backend", dialect: "mariadb", family: "mysql", want: migrate_backend_mysql.New},
		{name: "stock backend of the family", dialect: "lite", family: "sqlite", want: migrate_backend_sqlite.New},
		{name: "registered backend of a third-party name", dialect: "acme", family: "acme", registered: map[string]MigrationBackendFactory{"acme": registered}, want: registered},
		{name: "registered name replaces the stock backend", dialect: "cockroachdb", family: "postgres", registered: map[string]MigrationBackendFactory{"cockroachdb": registered}, want: registered},
		{name: "stock name comes before a registered family", dialect: "cockroachdb", family: "postgres", registered: map[string]MigrationBackendFactory{"postgres": registered}, want: migrate_backend_cockroachdb.New},
		{name: "registered family comes before the stock family", dialect: "acme", family: "postgres", registered: map[string]MigrationBackendFactory{"postgres": registered}, want: registered},
		{name: "unknown name and family", dialect: "exotic", family: "exotic"},
		{name: "empty name falls back to the family", dialect: "", family: "duckdb", want: migrate_backend_duckdb.New},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			factory, found := migrationBackendFor(&dialect.Facts{Name: testCase.dialect, Family: testCase.family}, testCase.registered)
			require.Equal(t, testCase.want != nil, found)
			if testCase.want != nil {
				assert.Equal(t, reflect.ValueOf(testCase.want).Pointer(), reflect.ValueOf(factory).Pointer())
			}
		})
	}
}

func TestMigrationFailure(t *testing.T) {
	t.Parallel()

	require.NoError(t, migrationFailure(nil))
	assert.ErrorIs(t, migrationFailure(context.Canceled), context.Canceled)
	assert.Equal(t, exitUsage, exitCodeFor(migrationFailure(seagull.ErrInvalidConfig)))
	assert.Equal(t, exitDatabase, exitCodeFor(migrationFailure(errors.New("deadlock"))))
}
