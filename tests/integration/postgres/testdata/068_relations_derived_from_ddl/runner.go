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

	_ "github.com/jackc/pgx/v5/stdlib"

	"case_runner/db"
)

const seed = `INSERT INTO readings_2025 (id, taken_on, value) VALUES (1, '2025-06-01', 5)`

type results map[string]any

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func day(year int, month time.Month, date int) time.Time {
	return time.Date(year, month, date, 0, 0, 0, 0, time.UTC)
}

func main() {
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, statement := range []string{fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA")), seed} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	value, note := int32(7), "first"
	output := results{}
	output.record("add_2026", func() (any, error) {
		return "added", queries.AddReading(ctx, db.AddReadingParams{ID: 10, TakenOn: day(2026, 3, 1), Value: &value, Note: &note})
	})
	output.record("add_2027", func() (any, error) {
		return "added", queries.AddReading(ctx, db.AddReadingParams{ID: 20, TakenOn: day(2027, 3, 1), Value: nil, Note: nil})
	})
	output.record("add_2028", func() (any, error) {
		return "added", queries.AddReading(ctx, db.AddReadingParams{ID: 30, TakenOn: day(2028, 3, 1), Value: &value, Note: &note})
	})
	output.record("readings_2026", func() (any, error) { return queries.ListReadings2026(ctx) })
	output.record("low_readings", func() (any, error) { return queries.ListLowReadings(ctx) })
	output.record("attached_readings", func() (any, error) { return queries.ListAttachedReadings(ctx) })
	output.record("detached_readings", func() (any, error) { return queries.ListDetachedReadings(ctx) })
	output.record("next_reading_number", func() (any, error) { return queries.NextReadingNumber(ctx) })
	output.record("reading_numbers", func() (any, error) { return queries.ReadingNumbers(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
