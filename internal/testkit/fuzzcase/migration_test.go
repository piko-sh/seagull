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

package fuzzcase

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrationModelKeepsAssignedOperationsAndBounds(t *testing.T) {
	require.Equal(t, []byte{4, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, []byte{migrationVersion, migrationCreateTable, migrationAddColumn, migrationRenameColumn,
		migrationDropColumn, migrationRenameTable, migrationDropTable, migrationCreateIndex, migrationDropIndex, migrationCreateView, migrationDropView},
		"changed an assigned migration operation")
	for engine := range capabilities {
		for _, seed := range MigrationSeeds(engine) {
			model, ok := DecodeMigration(engine, seed)
			require.True(t, ok && len(model.Steps) > 0 && len(model.Steps) <= 8,
				"migration seed did not reach bounded operations: %s %v", engine, seed)
			replay, _ := DecodeMigration(engine, seed)
			require.Equal(t, model, replay, "decoding changed across calls")
			for length := 2; length < len(seed); length++ {
				shorter, accepted := DecodeMigration(engine, seed[:length])
				require.True(t, accepted, "shortening rejected an earlier migration step")
				require.Equal(t, model.Steps[:length-1], shorter.Steps, "shortening changed an earlier migration step")
			}
		}
	}
	for _, seed := range [][]byte{nil, {1}, {3}, bytes.Repeat([]byte{4}, MaxBytes+1)} {
		_, ok := DecodeMigration("sqlite", seed)
		require.False(t, ok, "accepted invalid migration envelope %v", seed[:min(len(seed), 8)])
	}
	_, ok := DecodeMigration("missing", []byte{4})
	require.False(t, ok, "accepted an unknown engine")
	long, ok := DecodeMigration("sqlite", bytes.Repeat([]byte{4}, MaxBytes))
	require.True(t, ok, "rejected a maximum-size migration sequence")
	require.Len(t, long.Steps, 8, "migration sequence exceeded its operation bound")
	for _, operation := range []byte{6, 7} {
		_, ok := DecodeMigration("clickhouse", []byte{4, operation})
		require.False(t, ok, "enabled ordinary indexes on the ClickHouse Memory fixture")
	}
}

func TestMigrationModelCoversCompleteObjectLifecycles(t *testing.T) {
	model, ok := DecodeMigration("sqlite", []byte{4, 0, 1, 2, 3, 4, 5})
	want := []string{
		"CREATE TABLE fuzz_stage (id INTEGER NOT NULL, score INTEGER);",
		"ALTER TABLE fuzz_stage ADD COLUMN extra INTEGER;",
		"ALTER TABLE fuzz_stage RENAME COLUMN extra TO renamed;",
		"ALTER TABLE fuzz_stage DROP COLUMN renamed;",
		"ALTER TABLE fuzz_stage RENAME TO fuzz_stage_renamed;",
		"DROP TABLE fuzz_stage_renamed;",
	}
	require.True(t, ok, "rejected the table lifecycle")
	require.Equal(t, want, model.Steps, "incomplete table lifecycle")
	for engine := range capabilities {
		input := []byte{4, 8, 9}
		wantSteps := 2
		if engine != engineClickHouse {
			input = append(input, 6, 7)
			wantSteps = 4
		}
		auxiliary, accepted := DecodeMigration(engine, input)
		require.True(t, accepted, "rejected the %s auxiliary lifecycle", engine)
		require.Len(t, auxiliary.Steps, wantSteps, "incomplete %s auxiliary lifecycle", engine)
		require.True(t, strings.HasPrefix(auxiliary.Steps[0], "CREATE VIEW"), "incomplete view lifecycle: %+v", auxiliary)
		require.Equal(t, "DROP VIEW fuzz_view;", auxiliary.Steps[1], "incomplete view lifecycle")
		if engine != engineClickHouse {
			require.True(t, strings.HasPrefix(auxiliary.Steps[2], "CREATE INDEX") && strings.HasPrefix(auxiliary.Steps[3], "DROP INDEX"),
				"incomplete index lifecycle: %+v", auxiliary)
		}
	}
}

func TestIndexMigrationKeepsAssignedInteractionsAndDialectBoundaries(t *testing.T) {
	require.Equal(t, []byte{6, 0, 1, 2, 3, 4, 5, 6, 7, 8, 16, 32}, []byte{indexMigrationVersion, indexCreate, indexDropColumn, indexRenameColumn,
		indexRenameTable, indexDrop, indexPeer, indexCreateUnique, indexDropTable, indexMigrationSingle, indexMigrationUnique, indexMigrationReverse},
		"changed an assigned index migration operation")
	_, accepted := DecodeMigration("clickhouse", []byte{6, 0})
	require.False(t, accepted, "enabled ordinary indexes on the ClickHouse Memory fixture")
	cases := []struct {
		engine    string
		statement string
		input     []byte
	}{
		{engine: "postgres", input: []byte{6, 32}, statement: "CREATE INDEX IF NOT EXISTS fuzz_model_index ON fuzz_items (id, category);"},
		{engine: "mysql", input: []byte{6, 0, 1, 4}, statement: "DROP INDEX fuzz_model_index ON fuzz_items;"},
		{engine: "cockroachdb", input: []byte{6, 0, 5, 5}, statement: "DROP INDEX fuzz_peers@fuzz_model_index;"},
		{engine: "sqlite", input: []byte{6, 0, 1, 1}, statement: "ALTER TABLE fuzz_items DROP COLUMN category;"},
		{engine: "duckdb", input: []byte{6, 0, 3, 3}, statement: "ALTER TABLE fuzz_items RENAME TO fuzz_items_renamed;"},
		{engine: "mariadb", input: []byte{6, 16, 1}, statement: "DROP INDEX fuzz_model_index ON fuzz_items;"},
		{engine: "mariadb", input: []byte{6, 48, 1, 1}, statement: "ALTER TABLE fuzz_items DROP COLUMN category;"},
		{engine: "mariadb", input: []byte{6, 24, 1}, statement: "ALTER TABLE fuzz_items DROP COLUMN category;"},
		{engine: "mariadb", input: []byte{6, 0, 6, 1}, statement: "ALTER TABLE fuzz_items DROP COLUMN category;"},
		{engine: "mysql", input: []byte{6, 16, 1}, statement: "ALTER TABLE fuzz_items DROP COLUMN category;"},
		{engine: "duckdb", input: []byte{6, 1, 0, 1, 1}, statement: "DROP INDEX fuzz_model_index;"},
		{engine: "duckdb", input: []byte{6, 1, 0, 1, 2}, statement: "DROP INDEX fuzz_model_index;"},
		{engine: "duckdb", input: []byte{6, 1, 0, 1, 1, 1}, statement: "ALTER TABLE fuzz_items DROP COLUMN category;"},
		{engine: "duckdb", input: []byte{6, 1, 0, 1, 2, 2}, statement: "ALTER TABLE fuzz_items RENAME COLUMN category TO renamed;"},
	}
	for _, current := range cases {
		model, accepted := DecodeMigration(current.engine, current.input)
		require.True(t, accepted, "rejected %s migration %v", current.engine, current.input)
		require.NotEmpty(t, model.Steps, "empty %s migration %v", current.engine, current.input)
		require.Equal(t, current.statement, model.Steps[len(model.Steps)-1], "changed %s migration meaning: %+v", current.engine, model)
	}
}

func TestIndexScopeMigrationPreservesNamespacesAndBatchDrops(t *testing.T) {
	require.Equal(t, []byte{7, 8, 64}, []byte{indexScopeMigrationVersion, indexScopeMigrationOperations, indexScopeSequence},
		"changed an assigned index scope operation")
	for engine := range capabilities {
		model, ok := DecodeMigration(engine, []byte{7, 0, 1, 2})
		if engine != "postgres" && engine != "cockroachdb" && engine != "timescaledb" {
			assert.False(t, ok, "enabled unsupported multiple-index drops for %s", engine)
			continue
		}
		drop := "DROP INDEX IF EXISTS fuzz_first_index, fuzz_second_index;"
		if engine == "cockroachdb" {
			drop = "DROP INDEX IF EXISTS fuzz_items@fuzz_first_index, fuzz_peers@fuzz_second_index;"
		}
		require.True(t, ok, "rejected batch drop for %s", engine)
		require.Len(t, model.Steps, 3, "changed batch drop semantics for %s", engine)
		require.Equal(t, drop, model.Steps[2], "changed batch drop semantics for %s", engine)
		collision, ok := DecodeMigration(engine, []byte{7, 3, 6, 7})
		last := "CREATE INDEX IF NOT EXISTS fuzz_model_index ON fuzz_items (category, id);"
		if engine == "cockroachdb" {
			last = "DROP INDEX fuzz_items@fuzz_model_index;"
		}
		require.True(t, ok, "rejected shared namespace sequence for %s", engine)
		require.Len(t, collision.Steps, 3, "changed shared namespace semantics for %s", engine)
		require.Equal(t, last, collision.Steps[2], "changed shared namespace semantics for %s", engine)
	}
}
