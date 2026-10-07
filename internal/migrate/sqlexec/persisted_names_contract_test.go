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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var updatePersistedNames = flag.Bool("update-persisted-names", false,
	"Re-record the persisted-name contract; it is frozen, so it is never part of the golden update lane")

const recordedPackageQualifier = "migration_sql."

type persistedNames struct {
	HistoryTable       string   `json:"historyTable"`
	CreateTableSQL     string   `json:"createTableSQL"`
	HistoryColumnSQL   []string `json:"historyColumnSQL"`
	CreateSeedTableSQL string   `json:"createSeedTableSQL"`
	InsertSeedSQL      string   `json:"insertSeedSQL"`
	DeleteHistorySQL   string   `json:"deleteHistorySQL"`
	LockStrategy       string   `json:"lockStrategy"`
	SeedLockStrategy   string   `json:"seedLockStrategy"`
	MigrationLockKey   string   `json:"migrationLockKey"`
	SeedLockKey        string   `json:"seedLockKey"`
}

func describePersistedNames(t *testing.T, backend Backend) persistedNames {
	t.Helper()
	require.NoError(t, backend.err)

	names := persistedNames{
		HistoryTable:       backend.historyTable(),
		CreateTableSQL:     createHistorySQL(backend),
		HistoryColumnSQL:   historyColumnSQL(backend),
		CreateSeedTableSQL: createSeedSQL(backend),
		InsertSeedSQL:      backend.engine.insertSeed(backend.seedTable(), "$1", "$2", "$3", "$4"),
		LockStrategy:       describeStrategy(backend.migrationLock, false),
		SeedLockStrategy:   describeStrategy(backend.seedLock, true),
	}
	if advisoryLock(backend.migrationLock) {
		names.MigrationLockKey = backend.Names().LockKey
	}
	if advisoryLock(backend.seedLock) {
		names.SeedLockKey = backend.Names().SeedLockKey
	}
	if backend.engine.deleteHistory != nil {
		names.DeleteHistorySQL = backend.engine.deleteHistory(backend.historyTable(), "$1")
	}
	return names
}

func historyColumnSQL(backend Backend) []string {
	statements := make([]string, 0, len(historyColumnAdditions))
	for _, column := range historyColumnAdditions {
		statements = append(statements, backend.engine.addHistoryColumn(backend.historyTable(), column))
	}
	return statements
}

func advisoryLock(strategy lockStrategy) bool {
	switch strategy.(type) {
	case *postgresAdvisoryLock, *pollingAdvisoryLock:
		return true
	}
	return false
}

func describeStrategy(strategy lockStrategy, seed bool) string {
	switch lock := strategy.(type) {
	case *tableLock:
		kind := "TableBasedLock"
		if seed {
			kind = "TableBasedSeedLock"
		}
		return fmt.Sprintf("&%s%s{CreateLockTableSQL:%#v}", recordedPackageQualifier, kind, lock.createSQL)
	case *cockroachDBTableLock:
		kind := "CockroachDBTableLock"
		if seed {
			kind = "CockroachDBTableSeedLock"
		}
		return fmt.Sprintf("&%s%s{CreateLockTableSQL:%#v, LockTableIDSQL:%#v}", recordedPackageQualifier, kind, lock.base.createSQL, lock.idSQL)
	case *postgresAdvisoryLock:
		if seed {
			return "&" + recordedPackageQualifier + "PostgresAdvisorySeedLock{}"
		}
		return fmt.Sprintf("&%sPostgresAdvisoryLock{LockKey:%#v}", recordedPackageQualifier, lock.key)
	case *pollingAdvisoryLock:
		return describeMySQLLock(lock, seed)
	case *noOpLock:
		return "&" + recordedPackageQualifier + "NoOpLock{}"
	}
	return fmt.Sprintf("%#v", strategy)
}

func describeMySQLLock(lock *pollingAdvisoryLock, seed bool) string {
	scope := ""
	if lock.nameSQL != "" {
		scope = fmt.Sprintf("NameSQL:%#v", lock.nameSQL)
	}
	if seed {
		return fmt.Sprintf("&%sMySQLAdvisorySeedLock{%s}", recordedPackageQualifier, scope)
	}
	if scope != "" {
		scope = ", " + scope
	}
	return fmt.Sprintf("&%sMySQLAdvisoryLock{LockKey:%#v%s}", recordedPackageQualifier, lock.key, scope)
}

func persistedNamesContract(t *testing.T, extra ...BackendOption) map[string]persistedNames {
	t.Helper()

	with := func(options ...BackendOption) []BackendOption {
		return append(options, extra...)
	}

	return map[string]persistedNames{
		"postgres":                 describePersistedNames(t, Postgres(with()...)),
		"postgres_pgbouncer":       describePersistedNames(t, PostgresPgBouncer(with()...)),
		"cockroachdb":              describePersistedNames(t, CockroachDB(with()...)),
		"timescaledb":              describePersistedNames(t, TimescaleDB(with()...)),
		"mysql":                    describePersistedNames(t, MySQL(with()...)),
		"mysql_with_dsn":           describePersistedNames(t, MySQLWithDSN("user:pass@tcp(localhost:3306)/app", with()...)),
		"mysql_server_wide_lock":   describePersistedNames(t, MySQL(with(WithServerWideLockKey())...)),
		"clickhouse":               describePersistedNames(t, ClickHouse(with()...)),
		"sqlite":                   describePersistedNames(t, SQLite(with()...)),
		"duckdb":                   describePersistedNames(t, DuckDB(with()...)),
		"postgres_renamed_history": describePersistedNames(t, Postgres(with(WithHistoryTable("app_schema_history"))...)),
	}
}

func assertPersistedNamesGolden(t *testing.T, goldenName string, contract map[string]persistedNames) {
	t.Helper()

	encoded, err := json.MarshalIndent(contract, "", "  ")
	require.NoError(t, err)
	encoded = append(encoded, '\n')

	goldenPath := filepath.Join("testdata", goldenName)
	if *updatePersistedNames {
		require.NoError(t, os.MkdirAll(filepath.Dir(goldenPath), 0o755))
		require.NoError(t, os.WriteFile(goldenPath, encoded, 0o644))
		return
	}

	expected, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "missing persisted-name contract; run with -update-persisted-names")
	require.Equal(t, string(expected), string(encoded))
}

func TestPersistedNamesContract(t *testing.T) {
	t.Parallel()

	assertPersistedNamesGolden(t, "persisted_names_contract_sgl.json", persistedNamesContract(t))
}

func TestPersistedNamesContractPikoNamespace(t *testing.T) {
	t.Parallel()

	assertPersistedNamesGolden(t, "persisted_names_contract_piko.json", persistedNamesContract(t, WithNamespace("piko")))
}
