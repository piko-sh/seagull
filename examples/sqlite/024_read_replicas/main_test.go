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

package main

import (
	"bytes"
	"database/sql"
	"testing"

	"seagull.sh/seagull/examples/sqlite/internal/exampledb"
)

func TestExample(t *testing.T) {
	writer := exampledb.Open(t, migrations)
	reader := openTestReader(t, writer)
	var output bytes.Buffer
	if err := run(t.Context(), writer, reader, &output); err != nil {
		t.Fatal(err)
	}
	exampledb.MatchOutput(t, output.String())
}

// openTestReader opens a read-only connection to the file behind writer, found with
// PRAGMA database_list. It is closed when the test ends.
//
// Takes t (*testing.T) which is the test.
// Takes writer (*sql.DB) which is the migrated database.
//
// Returns *sql.DB which is the read-only connection.
func openTestReader(t *testing.T, writer *sql.DB) *sql.DB {
	t.Helper()
	var (
		sequence int
		schema   string
		path     string
	)
	if err := writer.QueryRowContext(t.Context(), "PRAGMA database_list").Scan(&sequence, &schema, &path); err != nil {
		t.Fatal(err)
	}
	reader, err := openReader(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	return reader
}
