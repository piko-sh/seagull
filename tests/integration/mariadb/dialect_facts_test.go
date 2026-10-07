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

package mariadb_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialecttest"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mariadb"
)

func TestMariaDBDialectFactsHoldOnTheEngine(t *testing.T) {
	database := openMigrationDatabase(t)
	for _, statement := range []string{
		"CREATE TABLE facts (id BIGINT PRIMARY KEY, label VARCHAR(16) NOT NULL)",
		"INSERT INTO facts (id, label) VALUES (1, 'one'), (2, 'two'), (3, 'three')",
	} {
		_, err := database.ExecContext(t.Context(), statement)
		require.NoError(t, err)
	}

	dialecttest.RunEngineFacts(t, &dialecttest.EngineFixture{Database: database, Table: "facts", Info: fakedialect.Port(dialect_engine_mariadb.New()).Info()})
}
