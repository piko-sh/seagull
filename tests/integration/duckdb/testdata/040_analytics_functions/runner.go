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

	"github.com/cockroachdb/apd/v3"
	_ "github.com/duckdb/duckdb-go/v2"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err = conn.ExecContext(ctx, `CREATE TABLE readings (
		id INTEGER PRIMARY KEY,
		station VARCHAR NOT NULL,
		taken_at TIMESTAMP NOT NULL,
		taken_on DATE NOT NULL,
		reading DOUBLE NOT NULL,
		price DECIMAL(10, 2) NOT NULL,
		tally INTEGER NOT NULL,
		valid BOOLEAN NOT NULL,
		label VARCHAR NOT NULL,
		day_text VARCHAR NOT NULL,
		scores INTEGER[] NOT NULL,
		payload VARCHAR NOT NULL
	)`); err != nil {
		return err
	}
	queries := db.New(conn)
	for _, reading := range readings() {
		if err := queries.InsertReading(ctx, reading); err != nil {
			return fmt.Errorf("InsertReading %d: %w", reading.ID, err)
		}
	}
	summary, err := queries.StationSummary(ctx)
	if err != nil {
		return fmt.Errorf("StationSummary: %w", err)
	}
	calendar, err := queries.ReadingCalendar(ctx)
	if err != nil {
		return fmt.Errorf("ReadingCalendar: %w", err)
	}
	values, err := queries.ReadingValues(ctx)
	if err != nil {
		return fmt.Errorf("ReadingValues: %w", err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"station_summary":  summary,
		"reading_calendar": calendar,
		"reading_values":   values,
	})
}

func readings() []db.InsertReadingParams {
	day := func(month time.Month, date int) time.Time {
		return time.Date(2024, month, date, 9, 30, 0, 0, time.UTC)
	}
	return []db.InsertReadingParams{
		{ID: 1, Station: "north", TakenAt: day(time.March, 1), TakenOn: day(time.March, 1), Reading: 1.5, Price: *apd.New(250, -2), Tally: 3, Valid: true, Label: "a1,b2", DayText: "2024-03-01", Scores: []int32{1, 3}, Payload: `{"a": {"k": "first"}}`},
		{ID: 2, Station: "north", TakenAt: day(time.March, 5), TakenOn: day(time.March, 5), Reading: 4.0, Price: *apd.New(125, -2), Tally: 7, Valid: false, Label: "a1", DayText: "not a day", Scores: []int32{}, Payload: `{"b": 1}`},
		{ID: 3, Station: "south", TakenAt: day(time.April, 2), TakenOn: day(time.April, 2), Reading: 2.5, Price: *apd.New(375, -2), Tally: 2, Valid: true, Label: "c3", DayText: "2024-04-02", Scores: []int32{5}, Payload: `{"a": 2}`},
	}
}
