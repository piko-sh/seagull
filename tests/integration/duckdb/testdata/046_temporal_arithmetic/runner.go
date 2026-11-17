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

const schema = `CREATE TABLE shipments (
    id INTEGER PRIMARY KEY,
    ordered_at TIMESTAMP NOT NULL,
    shipped_at TIMESTAMP,
    ordered_on DATE NOT NULL,
    due_on DATE NOT NULL,
    handling INTERVAL NOT NULL
);
`

const seed = `INSERT INTO shipments VALUES (1, TIMESTAMP '2024-01-01 10:00:00', TIMESTAMP '2024-01-03 12:30:00', DATE '2024-01-01', DATE '2024-01-11', INTERVAL '2 days 3 hours'), (2, TIMESTAMP '2024-02-28 23:00:00', NULL, DATE '2024-02-28', DATE '2024-03-01', INTERVAL 45 MINUTE)`

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
	output.record("shipping_times", func() (any, error) { return queries.ShippingTimes(ctx) })
	output.record("days_to_due", func() (any, error) { return queries.DaysToDue(ctx) })
	output.record("deadlines", func() (any, error) { return queries.Deadlines(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
