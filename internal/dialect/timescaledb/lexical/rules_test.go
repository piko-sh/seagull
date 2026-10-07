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

package lexical_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	postgres_lexical "seagull.sh/seagull/internal/dialect/postgres/lexical"
	"seagull.sh/seagull/internal/dialect/timescaledb/lexical"
	"seagull.sh/seagull/internal/sqllex"
)

func TestRulesRequiresNoTransaction(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "a continuous aggregate created with data",
			input:    "CREATE MATERIALIZED VIEW daily WITH (timescaledb.continuous) AS SELECT time_bucket('1 day', t) FROM m GROUP BY 1;",
			expected: true,
		},
		{
			name:     "or replace, upper case and an explicit with data",
			input:    "CREATE OR REPLACE MATERIALIZED VIEW daily WITH (TIMESCALEDB.CONTINUOUS = true) AS SELECT 1 WITH DATA;",
			expected: true,
		},
		{
			name:     "a continuous aggregate created with no data",
			input:    "CREATE MATERIALIZED VIEW daily WITH (timescaledb.continuous) AS SELECT 1 WITH NO DATA;",
			expected: false,
		},
		{
			name:     "the option switched off",
			input:    "CREATE MATERIALIZED VIEW daily WITH (timescaledb.continuous = false) AS SELECT 1;",
			expected: false,
		},
		{
			name:     "the option switched off as a string",
			input:    "CREATE MATERIALIZED VIEW daily WITH (timescaledb.continuous = 'off') AS SELECT 1;",
			expected: false,
		},
		{
			name:     "a plain materialised view",
			input:    "CREATE MATERIALIZED VIEW daily AS SELECT 1;",
			expected: false,
		},
		{
			name:     "a refresh",
			input:    "CALL refresh_continuous_aggregate('daily', NULL, NULL);",
			expected: true,
		},
		{
			name:     "a schema-qualified refresh",
			input:    "CALL public.refresh_continuous_aggregate('daily', NULL, NULL);",
			expected: true,
		},
		{
			name:     "another procedure",
			input:    "CALL run_job(1000);",
			expected: false,
		},
		{
			name:     "a mention inside a literal",
			input:    "SELECT 'CALL refresh_continuous_aggregate';",
			expected: false,
		},
		{
			name:     "the base rules still apply",
			input:    "CREATE INDEX CONCURRENTLY i ON m (t);",
			expected: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			required, err := sqllex.RequiresNoTransaction(lexical.Rules(), testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, required)
		})
	}
}

func TestRulesMatchPostgresLexically(t *testing.T) {
	t.Parallel()

	input := "CREATE TABLE \"a;b\" (begin int); SELECT E'x\\';y', $$z;$$; /* a /* b */ */ SELECT (1; 2);"
	timescale, err := sqllex.Split(lexical.Rules(), input)
	require.NoError(t, err)
	postgres, err := sqllex.Split(postgres_lexical.Rules(), input)
	require.NoError(t, err)
	require.Equal(t, postgres, timescale)
}
