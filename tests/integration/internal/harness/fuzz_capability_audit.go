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

	"github.com/stretchr/testify/require"
)

// CheckFuzzCapabilityAudit checks native expression and index dependency rules.
//
// Takes t (*testing.T) which reports differences from the model's capabilities.
// Takes suite (FuzzSuite) which supplies an isolated engine connection.
func CheckFuzzCapabilityAudit(t *testing.T, suite FuzzSuite) {
	t.Helper()
	for operation, sqlText := range map[string]string{
		"IN":     "SELECT id FROM (SELECT 1 AS id) AS input WHERE id IN (id, id)",
		"NOT IN": "SELECT id FROM (SELECT 1 AS id) AS input WHERE id NOT IN (id, id)",
	} {
		t.Run(operation, func(t *testing.T) {
			database := suite.Open(t)
			ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(databaseInputDeadline), errFuzzInputDeadline)
			defer cancel()
			rows, err := database.QueryContext(ctx, sqlText)
			if suite.Engine == engineClickHouse {
				require.ErrorContains(t, err, "second argument is constant or table expression")
			} else {
				require.NoError(t, err)
			}
			if rows != nil {
				require.NoError(t, rows.Close())
			}
		})
	}
	if suite.Engine == engineClickHouse {
		return
	}
	for operation, statement := range map[string]string{
		"DROP COLUMN":   "ALTER TABLE capability_items DROP COLUMN category",
		"RENAME COLUMN": "ALTER TABLE capability_items RENAME COLUMN category TO renamed",
	} {
		t.Run(operation, func(t *testing.T) {
			database := suite.Open(t)
			ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(databaseInputDeadline), errFuzzInputDeadline)
			defer cancel()
			fuzzExec(ctx, t, database, "CREATE TABLE capability_items (id BIGINT, category BIGINT)", nil)
			fuzzExec(ctx, t, database, "CREATE INDEX capability_index ON capability_items (id)", nil)
			_, err := database.ExecContext(ctx, statement)
			if suite.Engine == engineDuckDB {
				require.ErrorContains(t, err, "depend")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
