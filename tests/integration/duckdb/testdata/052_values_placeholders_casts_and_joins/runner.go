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

const schema = `CREATE TABLE trips (id INTEGER PRIMARY KEY, city VARCHAR NOT NULL, fare DOUBLE NOT NULL, day DATE NOT NULL,
    started TIMESTAMP NOT NULL, note VARCHAR);
CREATE TABLE riders (id INTEGER PRIMARY KEY, trip_id INTEGER NOT NULL, name VARCHAR NOT NULL);
`

const seed = `INSERT INTO trips VALUES (1, 'leeds', 9.5, DATE '2024-03-01', TIMESTAMP '2024-03-01 10:30:00', '12'), (2, 'york', 4.0, DATE '2024-01-11', TIMESTAMP '2024-01-11 08:00:00', 'late'), (3, 'leeds', 6.0, DATE '2024-02-01', TIMESTAMP '2024-02-01 23:59:00', NULL);
INSERT INTO riders VALUES (1, 1, 'ada'), (2, 2, 'bo'), (3, 1, 'cy');`

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
	output.record("countdown", func() (any, error) { return queries.Countdown(ctx) })
	output.record("mixed", func() (any, error) { return queries.Mixed(ctx) })
	output.record("literal", func() (any, error) { return queries.Literal(ctx) })
	output.record("trips_for", func() (any, error) {
		return queries.TripsFor(ctx, db.TripsForParams{ID: 0, City: "leeds", Other: "york"})
	})
	output.record("days_since", func() (any, error) { return queries.DaysSince(ctx) })
	output.record("tried", func() (any, error) { return queries.Tried(ctx) })
	output.record("riding", func() (any, error) { return queries.Riding(ctx) })
	output.record("dearer", func() (any, error) { return queries.Dearer(ctx, 10) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
