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

package harness

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
)

// seedVacuumEngines lists the engines whose seeds run in a transaction and refuse VACUUM
// inside one.
var seedVacuumEngines = map[string]bool{enginePostgres: true, engineTimescaleDB: true, engineSQLite: true, engineDuckDB: true}

// CheckSeedOutsideTransaction applies a seed that inserts a row and then runs VACUUM,
// which these engines refuse inside a transaction, and checks that the seed applies,
// recorded once, with its row, rather than failing in the transaction seeds normally run
// in.
//
// Takes t (*testing.T) which reports a refused seed or a missing row.
// Takes suite (NativeFuzzSuite) which opens the database and names its driver.
func CheckSeedOutsideTransaction(t *testing.T, suite NativeFuzzSuite) {
	t.Helper()
	if !seedVacuumEngines[suite.Engine] {
		t.Skipf("capability exclusion: %s seeds do not run in a transaction or have no VACUUM", suite.Engine)
	}
	database, _ := suite.OpenNative(t)
	ctx := t.Context()
	vacuum := "VACUUM;\n"
	if suite.Engine == enginePostgres || suite.Engine == engineTimescaleDB {
		vacuum = "VACUUM vacuumed_items;\n"
	}
	files := fstest.MapFS{
		"migrations/0001_items.up.sql": {Data: []byte("CREATE TABLE vacuumed_items (id INTEGER);\n")},
		"seeds/0001_fill.sql":          {Data: []byte("INSERT INTO vacuumed_items VALUES (1);\n" + vacuum)},
	}
	backend := executionBackend(suite.Engine)
	migrator, err := migrate.NewMigrator(database, backend, files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(ctx)
	require.NoError(t, err)
	seeder, err := migrate.NewSeeder(database, backend, files, "seeds")
	require.NoError(t, err)
	_, err = seeder.Apply(ctx)
	require.NoError(t, err, "a seed holding VACUUM was not applied")
	var rows int
	require.NoError(t, database.QueryRowContext(ctx, "SELECT count(*) FROM vacuumed_items").Scan(&rows))
	assert.Equal(t, 1, rows, "the seed left the wrong number of rows")
	applied, err := seeder.Apply(ctx)
	assert.NoError(t, err, "applying the seed a second time")
	assert.Zero(t, applied, "the seed was not recorded, so a second apply ran it again")
}
