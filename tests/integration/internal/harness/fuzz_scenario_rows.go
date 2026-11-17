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
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// nonNullDriverNil distinguishes a decoded JSON null from SQL NULL.
type nonNullDriverNil struct{}

// scenarioRows observes SQL nullness independently of a driver's decoded value.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes database (*sql.DB) which owns the isolated native state.
// Takes suite (FuzzSuite) which supplies the dialect and isolated native database.
// Takes statement (string) which contains executable native SQL.
// Takes arguments ([]any) which preserves native parameter order.
// Takes ordered (bool) which requests sequence comparison.
//
// Returns [][]any which preserves SQL NULL separately from decoded JSON null.
func scenarioRows(ctx context.Context, t *testing.T, database *sql.DB, suite FuzzSuite, statement string, arguments []any, ordered bool) [][]any {
	t.Helper()
	columns := fuzzColumnNames(ctx, t, database, statement, arguments...)
	quote := suite.Dialect().Info().IdentifierQuote
	projections := make([]string, 0, 1+len(columns))
	projections = append(projections, "fuzz_observed.*")
	for index, column := range columns {
		projections = append(projections, fmt.Sprintf("(fuzz_observed.%s IS NULL) AS fuzz_null%d", quote.Quote(column), index))
	}
	wrapped := "SELECT " + strings.Join(projections, ", ") + " FROM (" + strings.TrimSuffix(strings.TrimSpace(statement), ";") + ") fuzz_observed"
	if ordered {
		wrapped += " ORDER BY 1"
	}
	observed := fuzzRows(ctx, t, database, wrapped, arguments)
	return scenarioNulls(t, observed, len(columns))
}

// scenarioNulls distinguishes SQL NULL from a nil decoded JSON value.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes observed ([][]any) which holds the actual native observation.
// Takes width (int) which records the original result width.
//
// Returns [][]any which retains values with independently observed nullness.
func scenarioNulls(t *testing.T, observed [][]any, width int) [][]any {
	t.Helper()
	for index, row := range observed {
		require.Len(t, row, width*2, "nullness probe changed result width")
		values := row[:width]
		for column, value := range values {
			if fuzzMetadataBool(t, row[column+width]) {
				require.Nil(t, value, "driver returned a value for SQL NULL")
				continue
			}
			if value == nil {
				values[column] = nonNullDriverNil{}
			}
		}
		observed[index] = values
	}
	return observed
}
