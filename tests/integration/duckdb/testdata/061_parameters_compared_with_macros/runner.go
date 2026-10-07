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
	"time"

	_ "github.com/duckdb/duckdb-go/v2"

	"case_runner/db"
)

type results map[string]any

const schema = `CREATE TABLE visits (
  id INTEGER PRIMARY KEY,
  visitor VARCHAR NOT NULL,
  visited DATE NOT NULL,
  stays INTEGER
);
CREATE MACRO year_of(day) AS year(day);
CREATE MACRO shout(word) AS upper(word) || '!';
CREATE MACRO doubled(amount) AS amount * 2;
CREATE MACRO kept(amount) AS coalesce(amount, 0);`

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
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	output := results{}
	visits := []db.AddVisitParams{
		{ID: 1, Visitor: "gannet", Visited: time.Date(2024, time.March, 3, 0, 0, 0, 0, time.UTC), Stays: new(int32(2))},
		{ID: 2, Visitor: "puffin", Visited: time.Date(2025, time.June, 9, 0, 0, 0, 0, time.UTC), Stays: nil},
		{ID: 3, Visitor: "tern", Visited: time.Date(2024, time.October, 1, 0, 0, 0, 0, time.UTC), Stays: new(int32(0))},
	}
	for _, visit := range visits {
		output.record(fmt.Sprintf("add_visit_%d", visit.ID), func() (any, error) {
			return "added", queries.AddVisit(ctx, visit)
		})
	}
	output.record("visits_in_2024", func() (any, error) {
		return queries.VisitsInYear(ctx, 2024)
	})
	output.record("visits_by_shout", func() (any, error) {
		return queries.VisitsByShout(ctx, "PUFFIN!")
	})
	output.record("visits_with_doubled_stays", func() (any, error) {
		return queries.VisitsWithDoubledStays(ctx, 4)
	})
	output.record("visits_with_kept_stays", func() (any, error) {
		return queries.VisitsWithKeptStays(ctx, 0)
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
