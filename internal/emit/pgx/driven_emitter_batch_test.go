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

package pgx_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBatchReturnsTheErrorClosingItsResultsReports(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		queries string
		method  string
		wants   []string
		absents []string
	}{
		{
			name:    "an insert queued once per row",
			queries: "-- sgl.query(InsertEvents, batch)\nINSERT INTO events (id, action) VALUES ($1, $2);\n",
			method:  "InsertEvents",
			wants: []string{
				"results := queries.db.SendBatch(ctx, batch)\n" +
					"\tfor range params {\n" +
					"\t\tif _, err := results.Exec(); err != nil {\n" +
					"\t\t\tif closeErr := results.Close(); closeErr != nil && !errors.Is(closeErr, err) {\n" +
					"\t\t\t\treturn errors.Join(err, closeErr)\n" +
					"\t\t\t}\n" +
					"\t\t\treturn err\n" +
					"\t\t}\n" +
					"\t}\n" +
					"\treturn results.Close()",
			},
			absents: []string{"defer results.Close()", "return nil"},
		},
		{
			name: "an upsert queued once per row",
			queries: "-- sgl.query(UpsertEvents, batch)\n" +
				"INSERT INTO events (id, action) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET action = EXCLUDED.action;\n",
			method:  "UpsertEvents",
			wants:   []string{"return errors.Join(err, closeErr)", "\treturn results.Close()"},
			absents: []string{"defer results.Close()"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, diagnostics := generatePgx(t, testCase.queries)
			require.Empty(t, diagnostics)
			source := files["audit.sql.go"]
			require.Contains(t, source, "\t\"errors\"\n", "the file imports errors for the join")
			method := methodSource(t, source, testCase.method)
			for _, want := range testCase.wants {
				require.Contains(t, method, want)
			}
			for _, absent := range testCase.absents {
				require.NotContains(t, method, absent)
			}
		})
	}
}
