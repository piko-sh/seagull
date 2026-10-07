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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"seagull.sh/seagull/examples/postgres/022_bulk_writes_pgx/internal/store"
)

const (
	// minutesPerWeek is how many readings each tide gauge takes in a week.
	minutesPerWeek = 7 * 24 * 60
)

// batchReporter is a store.DBTX that reports each pgx.Batch before sending it. Every
// other method comes from the embedded pool.
type batchReporter struct {
	*pgxpool.Pool

	// output receives a line per batch.
	output io.Writer
}

// SendBatch reports how many statements the batch holds, then sends it on the pool.
//
// Takes batch (*pgx.Batch) which holds the queued statements.
//
// Returns pgx.BatchResults which reads each statement's result.
func (reporter batchReporter) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	fmt.Fprintf(reporter.output, "sending one pgx.Batch of %d INSERT statements\n", batch.Len())
	return reporter.Pool.SendBatch(ctx, batch)
}

// run records a week of readings from one tide gauge with a batch query and loads the
// other gauge's week with a copyfrom query, then summarises what landed in the table.
//
// Takes pool (*pgxpool.Pool) which is a pool on a migrated database.
// Takes output (io.Writer) which receives each write and the summary.
//
// Returns error when a query fails.
func run(ctx context.Context, pool *pgxpool.Pool, output io.Writer) error {
	queries := store.New(batchReporter{Pool: pool, output: output})

	// RecordReadings is a batch query. With go.driver: pgx the generated method queues one
	// INSERT per row in a pgx.Batch and sends them together, without waiting for each reply,
	// then reads every result. pgx runs a batch in one implicit transaction, so a failing
	// row undoes the whole batch. It returns only error.
	harbour := tideReadings("harbour")
	if err := queries.RecordReadings(ctx, harbour); err != nil {
		return fmt.Errorf("recording the harbour readings: %w", err)
	}
	fmt.Fprintf(output, "harbour: the batch recorded %d readings\n", len(harbour))

	// LoadReadings is a copyfrom query: pgx streams the rows with PostgreSQL's COPY
	// protocol, the quickest way to load many rows, and returns how many the server copied.
	// The table and columns come from the INSERT, and the VALUES list only says which field
	// fills which column. Both queries insert the same columns, so their Params structs
	// convert into each other.
	pier := make([]store.LoadReadingsParams, 0, minutesPerWeek)
	for _, reading := range tideReadings("pier") {
		pier = append(pier, store.LoadReadingsParams(reading))
	}
	copied, err := queries.LoadReadings(ctx, pier)
	if err != nil {
		return fmt.Errorf("loading the pier readings: %w", err)
	}
	fmt.Fprintf(output, "pier: COPY loaded %d readings\n", copied)

	summary, err := queries.SummariseReadings(ctx)
	if err != nil {
		return fmt.Errorf("summarising readings: %w", err)
	}
	for _, row := range summary {
		fmt.Fprintf(output, "%-8s %d readings, %d with a note\n", row.Station, row.Readings, row.Noted)
	}
	return nil
}

// tideReadings makes one reading a minute for a week at a station. The height rises and
// falls in a simple saw-tooth, and the reading on the hour carries a note.
//
// Takes station (string) which names the gauge.
//
// Returns []store.RecordReadingsParams which holds every reading, ready for the batch.
func tideReadings(station string) []store.RecordReadingsParams {
	readings := make([]store.RecordReadingsParams, 0, minutesPerWeek)
	for minute := range int64(minutesPerWeek) {
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
	return readings
}
