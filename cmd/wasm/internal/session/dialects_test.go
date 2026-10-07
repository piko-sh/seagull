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

package session_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/wasm/internal/session"
)

func TestDialectsListDriversByFamily(t *testing.T) {
	t.Parallel()

	listed, err := session.Dialects()
	require.NoError(t, err)

	byName := map[string]session.DialectView{}
	for _, view := range listed {
		byName[view.Name] = view
	}
	testCases := []struct {
		name        string
		family      string
		placeholder string
		drivers     []string
		options     []string
	}{
		{name: "sqlite", family: "sqlite", placeholder: "?1", drivers: []string{"database/sql"}, options: []string{}},
		{name: "postgres", family: "postgres", placeholder: "$1", drivers: []string{"database/sql", "pgx"}, options: []string{"search_path"}},
		{name: "cockroachdb", family: "postgres", placeholder: "$1", drivers: []string{"database/sql", "pgx"}, options: []string{"search_path"}},
		{name: "timescaledb", family: "postgres", placeholder: "$1", drivers: []string{"database/sql", "pgx"}, options: []string{"search_path"}},
		{name: "mysql", family: "mysql", placeholder: "?", drivers: []string{"database/sql"}, options: []string{"ansi_quotes"}},
		{name: "mariadb", family: "mysql", placeholder: "?", drivers: []string{"database/sql"}, options: []string{"ansi_quotes"}},
		{name: "clickhouse", family: "clickhouse", placeholder: "{name:Type}", drivers: []string{"database/sql", "clickhouse"}, options: []string{"group_by_use_nulls"}},
		{name: "duckdb", family: "duckdb", placeholder: "$1", drivers: []string{"database/sql"}, options: []string{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			view, found := byName[testCase.name]
			require.True(t, found)
			require.Equal(t, testCase.family, view.Family)
			require.Equal(t, testCase.placeholder, view.Placeholder)
			require.Equal(t, testCase.drivers, view.Drivers)
			require.Equal(t, []string{"database/sql"}, view.PreparedDrivers)
			require.Equal(t, testCase.options, view.Options)
		})
	}
}
