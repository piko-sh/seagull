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

	"seagull.sh/seagull/examples/mysql/022_bulk_writes/internal/store"
)

const (
	// minutesPerWeek is how many readings each tide gauge takes in a week.
	minutesPerWeek = 7 * 24 * 60
)

// statementCounter is a store.DBTX that counts the statements sent through ExecContext.
// QueryContext and QueryRowContext come from the embedded *sql.DB.
type statementCounter struct {
	*sql.DB

	// statements is how many times ExecContext has been called.
	statements int
}

// ExecContext counts the statement, then runs it on the database.
//
// Takes query (string) which is the SQL to run.
// Takes arguments (...any) which are its bind values.
//
// Returns sql.Result which is the database's result.
// Returns error when the statement fails.
func (counter *statementCounter) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	counter.statements++
	return counter.DB.ExecContext(ctx, query, arguments...)
}

// run records a week of readings from two tide gauges in one batch call, shows how many
// INSERT statements that took, and summarises what landed in the table.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the statement counts and the summary.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	counter := &statementCounter{DB: database}
	queries := store.New(counter)

	// An empty batch is a no-op: nothing is sent to the database.
	if err := queries.RecordReadings(ctx, nil); err != nil {
		return fmt.Errorf("recording no readings: %w", err)
	}
	fmt.Fprintf(output, "0 readings sent in %d statements\n", counter.statements)

	readings := tideReadings("harbour", "pier")

	// A batch query takes a slice of Params, one per row. On database/sql the generated
	// method writes them as multi-row INSERTs, chunked so that no statement binds more than
	// 65,535 variables, the most a MySQL prepared statement can hold: four columns a row
	// allows 16,383 rows a statement. copyfrom generates the same code on database/sql.
	//
	// Each chunk is its own statement, so run the call inside RunInTx (concept 23) when
	// every row must land or none.
	if err := queries.RecordReadings(ctx, readings); err != nil {
		return fmt.Errorf("recording readings: %w", err)
	}
	fmt.Fprintf(output, "%d readings sent in %d statements\n", len(readings), counter.statements)

	summary, err := queries.SummariseReadings(ctx)
	if err != nil {
		return fmt.Errorf("summarising readings: %w", err)
	}
	for _, row := range summary {
		fmt.Fprintf(output, "%-8s %d readings, %d with a note\n", row.Station, row.Readings, row.Noted)
	}
	return nil
}

// tideReadings makes one reading a minute for a week at each station. The height rises
// and falls in a simple saw-tooth, and the reading on the hour carries a note.
//
// Takes stations (...string) which name the gauges.
//
// Returns []store.RecordReadingsParams which holds every reading, ready for the batch.
func tideReadings(stations ...string) []store.RecordReadingsParams {
	readings := make([]store.RecordReadingsParams, 0, len(stations)*minutesPerWeek)
	for _, station := range stations {
		for minute := range int32(minutesPerWeek) {
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
