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

// CheckAppliedAtUpgrade checks that a legacy MySQL history column is upgraded. The
// upgrade must preserve a recorded time across time zones and accept dates after 2038.
//
// Takes t (*testing.T) which reports a column left unchanged or a time moved.
// Takes suite (NativeFuzzSuite) which opens the database and names its driver.
func CheckAppliedAtUpgrade(t *testing.T, suite NativeFuzzSuite) {
	t.Helper()
	if suite.Engine != engineMySQL && suite.Engine != engineMariaDB {
		t.Skipf("capability exclusion: %s records applied_at in a type that holds every time", suite.Engine)
	}
	database, _ := suite.OpenNative(t)
	ctx := t.Context()
	connection, err := database.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = connection.Close() }()
	files := fstest.MapFS{"migrations/0001_items.up.sql": {Data: []byte("CREATE TABLE items (id INT);\n")}}
	migrator, err := migrate.NewMigrator(database, executionBackend(suite.Engine), files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(ctx)
	require.NoError(t, err)
	for _, statement := range []string{
		"SET time_zone = '+00:00'",
		"ALTER TABLE sgl_migrations MODIFY applied_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)",
		"UPDATE sgl_migrations SET applied_at = '2030-06-01 12:00:00' WHERE version = 1",
		"SET time_zone = '+05:00'",
	} {
		_, err := connection.ExecContext(ctx, statement)
		require.NoError(t, err, "%s", statement)
	}
	files["migrations/0002_more.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE more_items (id INT);\n")}
	migrator, err = migrate.NewMigrator(database, executionBackend(suite.Engine), files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(ctx)
	require.NoError(t, err, "the run after an earlier release's history table failed")
	var dataType, applied string
	require.NoError(t, connection.QueryRowContext(ctx, "SELECT data_type FROM information_schema.columns "+
		"WHERE table_schema = DATABASE() AND table_name = 'sgl_migrations' AND column_name = 'applied_at'").Scan(&dataType))
	assert.Equal(t, "datetime", dataType, "applied_at kept the type of an earlier release")
	require.NoError(t, connection.QueryRowContext(ctx, "SELECT DATE_FORMAT(applied_at, '%Y-%m-%d %H:%i:%s') FROM sgl_migrations WHERE version = 1").Scan(&applied))
	assert.Equal(t, "2030-06-01 12:00:00", applied, "the recorded time moved in the change")
	_, err = connection.ExecContext(ctx, "UPDATE sgl_migrations SET applied_at = '2040-01-01 00:00:00' WHERE version = 1")
	assert.NoError(t, err, "applied_at refuses a time after 2038")
}
