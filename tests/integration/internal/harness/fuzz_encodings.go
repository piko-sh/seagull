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
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
)

// encodingDeadline bounds applying the encoded migration and seed.
const encodingDeadline = time.Minute

// CheckFileEncodings applies a migration and a seed an editor saved with a UTF-8 byte
// order mark and CRLF line endings, through the engine's real migration and seed
// services, and checks that both apply and that the history then validates.
//
// Takes t (*testing.T) which reports a file the services cannot apply.
// Takes suite (NativeFuzzSuite) which opens the database.
func CheckFileEncodings(t *testing.T, suite NativeFuzzSuite) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(encodingDeadline), errFuzzInputDeadline)
	defer cancel()
	database, _ := suite.OpenNative(t)
	table := "CREATE TABLE encoded (id BIGINT NOT NULL)"
	if suite.Engine == engineClickHouse {
		table = "CREATE TABLE encoded (id Int64) ENGINE = MergeTree ORDER BY id"
	}
	files := fstest.MapFS{
		"migrations/0001_encoded.up.sql": {Data: []byte("\xEF\xBB\xBF-- a migration saved on Windows\r\n" + table + ";\r\n")},
		"seeds/0001_encoded.sql":         {Data: []byte("\xEF\xBB\xBFINSERT INTO encoded (id) VALUES (1);\r\nINSERT INTO encoded (id) VALUES (2);\r\n")},
	}
	backend := executionBackend(suite.Engine)
	migrator, err := migrate.NewMigrator(database, backend, files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(ctx)
	require.NoError(t, err, "a migration with a byte order mark and CRLF line endings did not apply")
	seeder, err := migrate.NewSeeder(database, backend, files, "seeds")
	require.NoError(t, err)
	_, err = seeder.Apply(ctx)
	require.NoError(t, err, "a seed with a byte order mark and CRLF line endings did not apply")
	var rows int
	require.NoError(t, database.QueryRowContext(ctx, "SELECT count(*) FROM encoded").Scan(&rows))
	assert.Equal(t, 2, rows, "the seed inserted the wrong number of rows")
	assert.NoError(t, migrator.Validate(ctx), "the history does not validate")
}
