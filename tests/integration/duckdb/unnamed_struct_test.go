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
	"context"
	"database/sql"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_duckdb"
)

func TestOnlyStructsWithoutFieldNamesAreRefusedAndUnreadable(t *testing.T) {
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
		{name: "a ROW", query: "SELECT ROW(id, name) AS pair FROM boats"},
		{name: "a parenthesised list of values", query: "SELECT (id, name) AS pair FROM boats"},
		{name: "a list of ROWs", query: "SELECT [ROW(id)] AS pairs FROM boats"},
		{name: "a ROW inside a named struct", query: "SELECT {'pair': ROW(id, name)} AS pair FROM boats"},
		{name: "a struct_pack", query: "SELECT struct_pack(id := id, name := name) AS pair FROM boats", readable: true},
		{name: "a struct literal", query: "SELECT {'id': id, 'name': name} AS pair FROM boats", readable: true},
		{name: "a list of struct literals", query: "SELECT [{'id': id}] AS pairs FROM boats", readable: true},
		{name: "a MAP literal of ROWs", query: "SELECT MAP {name: ROW(id, name)} AS pairs FROM boats"},
		{name: "a map of a list of ROWs", query: "SELECT map([name], [ROW(id, name)]) AS pairs FROM boats"},
		{name: "a map from entries holding ROWs", query: "SELECT map_from_entries([(name, ROW(id, name))]) AS pairs FROM boats"},
		{name: "a MAP literal of struct literals", query: "SELECT MAP {name: {'id': id}} AS pairs FROM boats", readable: true},
		{name: "a map from unnamed entries", query: "SELECT map_from_entries([(name, id)]) AS pairs FROM boats", readable: true},
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
			require.ErrorContains(t, readErr, "empty name")
			require.True(t, refused)
		})
	}
}

func readOneValue(ctx context.Context, database *sql.DB, query string) error {
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var value any
		if err := rows.Scan(&value); err != nil {
			return err
		}
	}
	return rows.Err()
}

func refusesQuery(t *testing.T, query string) bool {
	t.Helper()

	generator, err := seagull.New(dialect_engine_duckdb.New())
	require.NoError(t, err)
	files := fstest.MapFS{
		"migrations":                  &fstest.MapFile{Mode: fs.ModeDir},
		"migrations/001_boats.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE boats (id INTEGER NOT NULL, name VARCHAR NOT NULL);\n")},
		"queries/boats.sql":           &fstest.MapFile{Data: []byte("-- sgl.query(Probe, many)\n" + query + ";\n")},
	}
	result, err := generator.Generate(t.Context(), seagull.Input{Package: "db", Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	require.NoError(t, err)
	refused := false
	for _, diagnostic := range result.Diagnostics {
		require.Equal(t, seagull.CodeEmitterUnsupported, diagnostic.Code, diagnostic.Message)
		refused = true
	}
	return refused
}
