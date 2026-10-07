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

const schema = `CREATE TABLE plain (code TEXT PRIMARY KEY, note TEXT);
CREATE TABLE IF NOT EXISTS plain (id INTEGER PRIMARY KEY);
CREATE TABLE pairs (left_id INTEGER, right_id INTEGER, note TEXT, PRIMARY KEY (left_id, right_id));
CREATE TABLE strict_keys (code TEXT PRIMARY KEY, note TEXT) STRICT;
CREATE TABLE numbered (id INTEGER PRIMARY KEY, note TEXT);
CREATE VIEW IF NOT EXISTS plain_view (plain_code, plain_note) AS SELECT * FROM plain;
CREATE VIEW IF NOT EXISTS plain_view AS SELECT code FROM plain;
`

var seed = []string{
	`INSERT INTO plain VALUES (NULL, 'no key'), ('a', 'keyed')`,
	`INSERT INTO pairs VALUES (NULL, NULL, 'unkeyed'), (1, 2, 'keyed')`,
	`INSERT INTO strict_keys VALUES ('s', NULL)`,
	`INSERT INTO numbered VALUES (NULL, 'assigned')`,
}

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
	for _, statement := range append([]string{schema}, seed...) {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("plain", func() (any, error) { return queries.ListPlain(ctx) })
	output.record("pairs", func() (any, error) { return queries.ListPairs(ctx) })
	output.record("strict", func() (any, error) { return queries.ListStrict(ctx) })
	output.record("numbered", func() (any, error) { return queries.ListNumbered(ctx) })
	output.record("plain_view", func() (any, error) { return queries.ListPlainView(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
