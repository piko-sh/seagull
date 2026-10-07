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

package duckdb_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOnlyMapsKeyedByCompositesAreRefusedAndUnreadable(t *testing.T) {
	database := openMigrationDatabase(t)
	for _, statement := range []string{
		"CREATE TABLE boats (id INTEGER NOT NULL, name VARCHAR NOT NULL)",
		"INSERT INTO boats VALUES (1, 'Swift')",
	} {
		_, err := database.ExecContext(t.Context(), statement)
		require.NoError(t, err)
	}

	testCases := []struct {
		name     string
		query    string
		readable bool
	}{
		{name: "a map keyed by a list", query: "SELECT MAP {[id]: name} AS keyed FROM boats"},
		{name: "a map keyed by a struct", query: "SELECT MAP {{'id': id}: name} AS keyed FROM boats"},
		{name: "a map keyed by a map", query: "SELECT MAP {MAP {name: id}: id} AS keyed FROM boats"},
		{name: "a list of maps keyed by a list", query: "SELECT [MAP {[id]: name}] AS keyed FROM boats"},
		{name: "a struct holding a map keyed by a struct", query: "SELECT {'inner': MAP {{'id': id}: name}} AS keyed FROM boats"},
		{name: "a map keyed by text", query: "SELECT MAP {name: id} AS keyed FROM boats", readable: true},
		{name: "a map keyed by a date", query: "SELECT MAP {DATE '2024-01-01': id} AS keyed FROM boats", readable: true},
		{name: "the entries of a map keyed by a list", query: "SELECT map_entries(MAP {[id]: name}) AS entries FROM boats", readable: true},
		{name: "the keys of a map keyed by a struct", query: "SELECT map_keys(MAP {{'id': id}: name}) AS keys FROM boats", readable: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			readErr := readOneValue(t.Context(), database, testCase.query)
			refused := refusesQuery(t, testCase.query)

			if testCase.readable {
				require.NoError(t, readErr)
				require.False(t, refused)
				return
			}
			require.ErrorContains(t, readErr, "MAP key type not supported")
			require.True(t, refused)
		})
	}
}
