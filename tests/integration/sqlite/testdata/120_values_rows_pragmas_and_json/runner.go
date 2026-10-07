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
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"case_runner/db"
)

const schema = `CREATE TABLE settings (id INTEGER PRIMARY KEY, doc JSONB NOT NULL, label STRING NOT NULL, blob_doc BLOB);
CREATE TABLE ledger (id INTEGER PRIMARY KEY, amount INTEGER NOT NULL, parts INTEGER NOT NULL, category TEXT NOT NULL, dir TEXT NOT NULL);
INSERT INTO settings VALUES (1, '{"a":1}', 'hello', CAST('{"a":"text in a blob"}' AS BLOB)), (2, '{"a":[2]}', '012', jsonb('{"a":2}'));
INSERT INTO ledger VALUES (1, 7, 0, 'tools', 'hammers'), (2, 9, 2, 'tools', 'saws'), (3, 5, 5, 'garden', 'hoses');
PRAGMA user_version = 4;`

type results map[string]any

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func main() {
	conn, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	output := results{}
	output.record("countdown", func() (any, error) { return queries.Countdown(ctx) })
	output.record("pairs", func() (any, error) { return queries.Pairs(ctx) })
	output.record("ledger_after", func() (any, error) {
		return queries.LedgerAfter(ctx, db.LedgerAfterParams{Category: "garden", ID: 3})
	})
	output.record("move_ledger", func() (any, error) {
		return "moved", queries.MoveLedger(ctx, db.MoveLedgerParams{Category: "garden", Dir: "rakes", ID: 1})
	})
	output.record("moved_entry", func() (any, error) { return queries.LedgerEntry(ctx, 1) })
	output.record("shares", func() (any, error) { return queries.Shares(ctx) })
	output.record("schema_version", func() (any, error) { return queries.SchemaVersion(ctx) })
	output.record("ledger_columns", func() (any, error) { return queries.LedgerColumns(ctx) })
	output.record("set_version", func() (any, error) { return "set", queries.SetVersion(ctx) })
	output.record("schema_version_after", func() (any, error) { return queries.SchemaVersion(ctx) })
	output.record("paths", func() (any, error) { return queries.Paths(ctx, "garden/%") })
	output.record("patterned", func() (any, error) { return queries.Patterned(ctx, "ool") })
	output.record("documents", func() (any, error) { return queries.Documents(ctx) })
	output.record("blob_fields", func() (any, error) { return queries.BlobFields(ctx) })
	output.record("store_binary", func() (any, error) {
		return "stored", queries.StoreBinary(ctx, db.StoreBinaryParams{ID: 3, Doc: `{"b":true}`, Label: "binary"})
	})
	output.record("read_binary", func() (any, error) { return queries.ReadBinary(ctx, 3) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
