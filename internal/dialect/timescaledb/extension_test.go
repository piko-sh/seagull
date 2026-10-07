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

package timescaledb_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/timescaledb"
)

func TestTheClassifierMatchesKeywordsAndParenthesesByKind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		wantKey string
		claimed bool
	}{
		{name: "a hypertable", sql: "CREATE HYPERTABLE r (ts timestamptz)", wantKey: "TIMESCALE_HYPERTABLE", claimed: true},
		{name: "a quoted hypertable keyword", sql: `CREATE "hypertable" r (ts timestamptz)`, wantKey: "TIMESCALE_HYPERTABLE", claimed: false},
		{name: "a continuous aggregate", sql: "CREATE MATERIALIZED VIEW v WITH (timescaledb.continuous) AS SELECT 1 AS a", wantKey: "TIMESCALE_CONTINUOUS_AGGREGATE", claimed: true},
		{name: "a continuous aggregate whose options hold a parenthesis literal", sql: "CREATE MATERIALIZED VIEW v WITH (note = '(', timescaledb.continuous) AS SELECT 1 AS a", wantKey: "TIMESCALE_CONTINUOUS_AGGREGATE", claimed: true},
		{name: "a quoted continuous option", sql: `CREATE MATERIALIZED VIEW v WITH ("timescaledb".continuous) AS SELECT 1 AS a`, wantKey: "TIMESCALE_CONTINUOUS_AGGREGATE", claimed: false},
		{name: "a quoted MATERIALIZED", sql: `CREATE "MATERIALIZED" VIEW v WITH (timescaledb.continuous) AS SELECT 1 AS a`, wantKey: "TIMESCALE_CONTINUOUS_AGGREGATE", claimed: false},
		{name: "a hypertable call", sql: "SELECT create_hypertable('r', 'ts')", wantKey: "TIMESCALE_HYPERTABLE", claimed: true},
		{name: "a quoted hypertable call", sql: `SELECT "create_hypertable"('r', 'ts')`, wantKey: "TIMESCALE_HYPERTABLE", claimed: false},
		{name: "a policy call", sql: "SELECT add_retention_policy('r', INTERVAL '7 days')", wantKey: "TIMESCALE_POLICY_OP", claimed: true},
		{name: "a quoted policy call", sql: `SELECT "add_retention_policy"('r', INTERVAL '7 days')`, wantKey: "TIMESCALE_POLICY_OP", claimed: false},
		{name: "a refresh", sql: "CALL refresh_continuous_aggregate('v', NULL, NULL)", wantKey: "TIMESCALE_POLICY_OP", claimed: true},
		{name: "a quoted refresh", sql: `CALL "refresh_continuous_aggregate"('v', NULL, NULL)`, wantKey: "TIMESCALE_POLICY_OP", claimed: false},
		{name: "a compression change", sql: "ALTER TABLE r SET (timescaledb.compress)", wantKey: "TIMESCALE_COMPRESSION_ENABLED", claimed: true},
		{name: "a quoted compression option", sql: `ALTER TABLE r SET ("timescaledb".compress)`, wantKey: "TIMESCALE_COMPRESSION_ENABLED", claimed: false},
		{name: "a quoted TABLE", sql: `ALTER "table" r SET (timescaledb.compress)`, wantKey: "TIMESCALE_COMPRESSION_ENABLED", claimed: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := timescaledb.New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			mutation, _ := engine.ApplyDDL(t.Context(), statements[0])

			claimed := mutation != nil && mutation.EngineSpecific[testCase.wantKey] != ""
			assert.Equal(t, testCase.claimed, claimed)
		})
	}
}
