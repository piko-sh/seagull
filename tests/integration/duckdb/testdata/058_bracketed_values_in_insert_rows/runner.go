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

type results map[string]any

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

const schema = `CREATE TABLE readings (
  id INTEGER PRIMARY KEY,
  scores INTEGER[] NOT NULL,
  label VARCHAR NOT NULL,
  flagged BOOLEAN NOT NULL,
  tags VARCHAR[] NOT NULL
);`

func main() {
	conn, err := sql.Open("duckdb", "")
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
	output.record("add_reading", func() (any, error) {
		return "added", queries.AddReading(ctx, db.AddReadingParams{ID: 1, Label: "first", Flagged: true})
	})
	output.record("add_struct_reading", func() (any, error) {
		return "added", queries.AddStructReading(ctx, db.AddStructReadingParams{ID: 2, Label: "struct", Flagged: false})
	})
	output.record("add_nested_reading", func() (any, error) {
		return "added", queries.AddNestedReading(ctx, db.AddNestedReadingParams{ID: 3, Label: "nested", Flagged: true})
	})
	output.record("add_reading_pair", func() (any, error) {
		return "added", queries.AddReadingPair(ctx, db.AddReadingPairParams{ID: 4, Label: "fourth", Flagged: false,
			ID2: 5, Label2: "fifth", Flagged2: true})
	})
	output.record("set_reading", func() (any, error) {
		return "set", queries.SetReading(ctx, db.SetReadingParams{Label: "reset", Flagged: true, ID: 5})
	})
	output.record("readings", func() (any, error) {
		rows, err := queries.ListReadings(ctx)
		return rows, err
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
