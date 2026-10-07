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

package databasesql_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/postgres"
)

func TestTheOverrideScanAdapterIsGeneratedOnlyForTheColumnsItReads(t *testing.T) {
	t.Parallel()

	schema := "CREATE TABLE labels (id BIGINT PRIMARY KEY, name TEXT NOT NULL, codes uuid[] NOT NULL);\n"
	testCases := []struct {
		name     string
		queries  string
		scan     string
		withFile bool
	}{
		{
			name:    "an overridden array column, which the SQL wraps as JSON",
			queries: "-- sgl.query(GetCodes, one)\n-- sgl.column(codes, go_type: \"[]*example.com/kinds.Code\")\nSELECT codes FROM labels WHERE id = $1;\n",
			scan:    "Scan(sglScanInto(&row.Codes))",
		},
		{
			name:     "an overridden text column, which the adapter reads",
			queries:  "-- sgl.query(GetName, one)\n-- sgl.column(name, go_type: \"example.com/kinds.Name\")\nSELECT name FROM labels WHERE id = $1;\n",
			scan:     "Scan(sglScanOverride(&row.Name))",
			withFile: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, diagnostics := generateDatabaseSQLFiles(t, postgres.New(), schema, testCase.queries)

			require.Empty(t, diagnostics)
			require.Contains(t, files["queries.sql.go"], testCase.scan)
			_, generated := files["driver_composites.go"]
			require.Equal(t, testCase.withFile, generated)
		})
	}
}
