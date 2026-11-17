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

package postgres

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckDistinctOnOrderMatchesTheLeadingOrderByKeys(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		options     []Option
		wantRefusal bool
	}{
		{name: "keys in the same order", sql: "SELECT DISTINCT ON (name, id) name, id FROM users ORDER BY name, id, email"},
		{name: "keys in another order", sql: "SELECT DISTINCT ON (name, id) name, id FROM users ORDER BY id, name"},
		{name: "a repeated DISTINCT ON key", sql: "SELECT DISTINCT ON (name, name) name FROM users ORDER BY name, id"},
		{name: "a repeated ORDER BY key", sql: "SELECT DISTINCT ON (name, id) name FROM users ORDER BY name, name, id"},
		{name: "a position", sql: "SELECT DISTINCT ON (name) name, id FROM users ORDER BY 2, name"},
		{name: "no ORDER BY", sql: "SELECT DISTINCT ON (name) name FROM users"},
		{name: "another key first", sql: "SELECT DISTINCT ON (name) name FROM users ORDER BY id, name", wantRefusal: true},
		{name: "a key left out after another", sql: "SELECT DISTINCT ON (name, id) name FROM users ORDER BY name, email", wantRefusal: true},
		{name: "an expression key", sql: "SELECT DISTINCT ON (lower(name)) name FROM users ORDER BY lower(name), id"},
		{
			name:        "an expression key matching columns only",
			sql:         "SELECT DISTINCT ON (lower(name)) name FROM users ORDER BY lower(name), id",
			options:     []Option{WithDistinctOnColumnsOnly()},
			wantRefusal: true,
		},
		{
			name:    "a column key matching columns only",
			sql:     "SELECT DISTINCT ON (name) name FROM users ORDER BY name, id",
			options: []Option{WithDistinctOnColumnsOnly()},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := analyseQueryWith(t, New(testCase.options...), testCase.sql)

			if testCase.wantRefusal {
				require.ErrorContains(t, err, distinctOnMismatch)
				return
			}
			require.NoError(t, err)
		})
	}
}
