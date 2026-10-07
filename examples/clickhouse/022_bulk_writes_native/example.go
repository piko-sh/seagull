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
	"fmt"
	"io"

	"seagull.sh/seagull/examples/clickhouse/022_bulk_writes_native/internal/store"
)

const (
	// minutesPerDay is how many readings each tide gauge takes in a day.
	minutesPerDay = 24 * 60
)

// run records a day of readings from two tide gauges in one batch call, shows how many
// parts that wrote, and summarises what landed in the table.
//
// A batch query takes a slice of Params, one per row. On the native driver the generated
// method calls PrepareBatch, appends every row with batch.Append and sends them with
// batch.Send: one INSERT, its rows sent column by column in blocks, with no bind
// variables and so no chunking. The {name:Type} placeholders in the VALUES list only name
// and type the columns; the driver takes the values from Append. copyfrom generates the
// same code.
//
// Each INSERT writes a new part, which ClickHouse merges in the background, so writing
// rows one at a time leaves many small parts to merge. CountParts reads the _part virtual
// column to show that the whole day landed as one part.
//
// Takes connection (store.DBTX) which is a native connection to a migrated database.
// Takes output (io.Writer) which receives the part counts and the summary.
//
// Returns error when a query fails.
func run(ctx context.Context, connection store.DBTX, output io.Writer) error {
	queries := store.New(connection)

	// An empty batch returns at once, without preparing an INSERT, so it writes no part.
	if err := queries.RecordReadings(ctx, nil); err != nil {
		return fmt.Errorf("recording no readings: %w", err)
	}
	if err := printParts(ctx, queries, 0, output); err != nil {
		return err
	}

	readings := tideReadings("harbour", "pier")
	if err := queries.RecordReadings(ctx, readings); err != nil {
		return fmt.Errorf("recording readings: %w", err)
	}
	if err := printParts(ctx, queries, len(readings), output); err != nil {
		return err
	}

	summary, err := queries.SummariseReadings(ctx)
	if err != nil {
		return fmt.Errorf("summarising readings: %w", err)
	}
	for _, row := range summary {
		fmt.Fprintf(output, "%-8s %d readings, %d with a note\n", row.Station, row.Readings, row.Noted)
	}
	return nil
}

// printParts prints how many parts hold the table's rows.
//
// Takes queries (*store.Queries) which runs the query.
// Takes sent (int) which is how many readings were sent.
// Takes output (io.Writer) which receives the line.
//
// Returns error when the query fails.
func printParts(ctx context.Context, queries *store.Queries, sent int, output io.Writer) error {
	parts, err := queries.CountParts(ctx)
	if err != nil {
		return fmt.Errorf("counting parts: %w", err)
	}
	fmt.Fprintf(output, "%d readings sent, parts holding rows: %d\n", sent, parts.Parts)
	return nil
}

// tideReadings makes one reading a minute for a day at each station. The height rises and
// falls in a simple saw-tooth, and the reading on the hour carries a note.
//
// Takes stations (...string) which name the gauges.
//
// Returns []store.RecordReadingsParams which holds every reading, ready for the batch.
func tideReadings(stations ...string) []store.RecordReadingsParams {
	readings := make([]store.RecordReadingsParams, 0, len(stations)*minutesPerDay)
	for _, station := range stations {
		for minute := range uint16(minutesPerDay) {
			var note *string
			if minute%60 == 0 {
				note = new(fmt.Sprintf("hour %d check", minute/60))
			}
			readings = append(readings, store.RecordReadingsParams{
				Station:  station,
				Minute:   minute,
				HeightCm: 200 + minute%360,
				Note:     note,
			})
		}
	}
	return readings
}
