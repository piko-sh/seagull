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
	"fmt"
	"io"

	"seagull.sh/seagull/examples/duckdb/017_streaming/internal/store"
)

// floodWarningCm is the tide level, in centimetres, that raises a flood warning.
const floodWarningCm = 450

// run records a morning of tide gauge readings and reads them back twice with
// StreamReadings. The stream command returns a range-over-func iterator instead of a
// slice, so each row is scanned only when the loop asks for it and a large result is
// never held in memory at once. Every step yields a row and an error; return on the
// error, as the loop goes on otherwise.
//
// Breaking out of the loop stops the query: the iterator closes its rows, which hands the
// connection back to the pool.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the summary and the warning.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	readings := []store.RecordReadingParams{
		{TakenAt: "06:00", LevelCm: 210},
		{TakenAt: "07:00", LevelCm: 280},
		{TakenAt: "08:00", LevelCm: 350},
		{TakenAt: "09:00", LevelCm: 420},
		{TakenAt: "10:00", LevelCm: 470},
		{TakenAt: "11:00", LevelCm: 495},
		{TakenAt: "12:00", LevelCm: 460},
		{TakenAt: "13:00", LevelCm: 390},
	}
	for _, reading := range readings {
		if err := queries.RecordReading(ctx, reading); err != nil {
			return fmt.Errorf("recording the %s reading: %w", reading.TakenAt, err)
		}
	}

	count := 0
	var highest store.StreamReadingsRow
	for reading, err := range queries.StreamReadings(ctx) {
		if err != nil {
			return fmt.Errorf("streaming readings: %w", err)
		}
		count++
		if reading.LevelCm > highest.LevelCm {
			highest = reading
		}
	}
	fmt.Fprintf(output, "%d readings, high water %dcm at %s\n", count, highest.LevelCm, highest.TakenAt)

	read := 0
	for reading, err := range queries.StreamReadings(ctx) {
		if err != nil {
			return fmt.Errorf("streaming readings: %w", err)
		}
		read++
		if reading.LevelCm >= floodWarningCm {
			fmt.Fprintf(output, "flood warning at %s, %dcm, after reading %d of %d rows\n", reading.TakenAt, reading.LevelCm, read, count)
			break
		}
	}
	return nil
}
