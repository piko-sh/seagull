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

	_ "github.com/duckdb/duckdb-go/v2"

	"case_runner/db"
)

const schema = `CREATE TABLE trips (
    id INTEGER PRIMARY KEY,
    city VARCHAR NOT NULL,
    note VARCHAR NOT NULL,
    fare DOUBLE NOT NULL,
    started_at TIMESTAMP NOT NULL,
    stops INTEGER[] NOT NULL,
    doc JSON NOT NULL
);
`

const seed = `INSERT INTO trips VALUES (1, 'leeds', 'route 12-34  late', 9.5, TIMESTAMP '2024-03-05 10:00:00', [1, 3], '{"a": "x", "b": 2, "meta": {"k": 1, "j": 2}}'), (2, 'leeds', 'no route', 9.5, TIMESTAMP '2024-03-06 22:30:00', [2], '{"a": [1]}'), (3, 'york', 'route 5-6', 4.0, TIMESTAMP '2024-03-07 00:00:00', [], '{}')`

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
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, statement := range []string{schema, seed} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("usual_fares", func() (any, error) { return queries.UsualFares(ctx) })
	output.record("parsed", func() (any, error) { return queries.Parsed(ctx) })
	output.record("lists", func() (any, error) { return queries.Lists(ctx) })
	output.record("documents", func() (any, error) { return queries.Documents(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
