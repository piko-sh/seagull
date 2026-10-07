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
	"time"

	"seagull.sh/seagull/examples/timescaledb/032_timescale_hypertables/internal/store"
)

// clock prints bucket times as hours and minutes in UTC.
const clock = "15:04"

// reading is one sea temperature sample from a buoy.
type reading struct {
	// buoy names the buoy that took the sample.
	buoy string

	// minutes is how long after 22:00 UTC on 31 May the sample was taken.
	minutes int

	// temperature is the sea temperature in degrees Celsius.
	temperature float64
}

// readings are two buoys' samples either side of midnight. Doom Bar sends nothing between
// 23:50 and 01:15.
var readings = []reading{
	{buoy: "Doom Bar", minutes: 0, temperature: 14.0},
	{buoy: "Doom Bar", minutes: 20, temperature: 14.4},
	{buoy: "Doom Bar", minutes: 40, temperature: 14.2},
	{buoy: "Doom Bar", minutes: 70, temperature: 13.8},
	{buoy: "Doom Bar", minutes: 110, temperature: 13.6},
	{buoy: "Doom Bar", minutes: 195, temperature: 13.1},
	{buoy: "Doom Bar", minutes: 225, temperature: 13.3},
	{buoy: "Stepper Point", minutes: 5, temperature: 15.0},
	{buoy: "Stepper Point", minutes: 90, temperature: 14.6},
	{buoy: "Stepper Point", minutes: 210, temperature: 14.0},
}

// run records the buoys' readings in the readings hypertable, then summarises them by the
// hour with time_bucket, fills the missing hour with time_bucket_gapfill and locf, and
// picks each buoy's first and last reading.
//
// The migration makes readings a hypertable with create_hypertable, which TimescaleDB
// splits into one chunk per day. To Seagull it is still the table the CREATE TABLE
// declared, so inserting and selecting look as they would on PostgreSQL; what the
// TimescaleDB dialect adds is the types of its functions.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the summaries.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)
	start := time.Date(2026, time.May, 31, 22, 0, 0, 0, time.UTC)

	for _, sample := range readings {
		err := queries.RecordReading(ctx, store.RecordReadingParams{
			Ts:          start.Add(time.Duration(sample.minutes) * time.Minute),
			Buoy:        sample.buoy,
			Temperature: sample.temperature,
		})
		if err != nil {
			return fmt.Errorf("recording a reading: %w", err)
		}
	}

	// show_chunks lists the chunks behind the hypertable; the readings cross midnight, so
	// they fill two.
	chunks, err := queries.ListChunks(ctx)
	if err != nil {
		return fmt.Errorf("listing chunks: %w", err)
	}
	fmt.Fprintf(output, "recorded %d readings in %d chunks\n", len(readings), len(chunks))

	if err := printHourly(ctx, queries, output); err != nil {
		return err
	}
	if err := printGapfilled(ctx, queries, start, output); err != nil {
		return err
	}
	return printFirstAndLast(ctx, queries, output)
}

// printHourly prints Doom Bar's hourly means. time_bucket rounds each reading's time down
// to the hour, and the query groups by that bucket, so an hour without readings has no
// row.
//
// The width is written INTERVAL '1 hour'. A bare '1 hour' works as well: Seagull reads
// the quoted width as an interval, as TimescaleDB does, and types the bucket the same.
//
// Takes queries (*store.Queries) which runs the query.
// Takes output (io.Writer) which receives the means.
//
// Returns error when the query fails.
func printHourly(ctx context.Context, queries *store.Queries, output io.Writer) error {
	hours, err := queries.HourlyAverages(ctx, "Doom Bar")
	if err != nil {
		return fmt.Errorf("averaging by the hour: %w", err)
	}
	fmt.Fprintln(output, "Doom Bar by the hour (time_bucket):")
	for _, hour := range hours {
		fmt.Fprintf(output, "  %s  mean %s\n", hour.Bucket.UTC().Format(clock), celsius(hour.Mean))
	}
	return nil
}

// printGapfilled prints Doom Bar's hourly means from 22:00 to 02:00 with every hour
// present. time_bucket_gapfill makes a row for each bucket between start and finish,
// whose mean is NULL when the hour had no readings; locf carries the last mean forward
// into it.
//
// The query names its bounds with `$2 as sgl.param(start)` and `$3 as sgl.param(finish)`,
// and casts them to timestamptz where it passes them to time_bucket_gapfill, since
// PostgreSQL cannot tell their type from that call alone. Seagull types the gapfilled
// bucket as *time.Time, though it is never NULL.
//
// Takes queries (*store.Queries) which runs the query.
// Takes start (time.Time) which is the first hour.
// Takes output (io.Writer) which receives the means.
//
// Returns error when the query fails.
func printGapfilled(ctx context.Context, queries *store.Queries, start time.Time, output io.Writer) error {
	hours, err := queries.GapfilledHourly(ctx, store.GapfilledHourlyParams{
		Buoy:   "Doom Bar",
		Start:  start,
		Finish: start.Add(4 * time.Hour),
	})
	if err != nil {
		return fmt.Errorf("filling the gaps: %w", err)
	}
	fmt.Fprintln(output, "Doom Bar by the hour, gaps filled (time_bucket_gapfill, locf):")
	for _, hour := range hours {
		bucket := "--:--"
		if hour.Bucket != nil {
			bucket = hour.Bucket.UTC().Format(clock)
		}
		fmt.Fprintf(output, "  %s  mean %s  carried %s\n", bucket, celsius(hour.Mean), celsius(hour.Carried))
	}
	return nil
}

// printFirstAndLast prints each buoy's earliest and latest reading. first and last take
// the value at the smallest and largest time in each group, whatever order the rows
// arrive in.
//
// first, last and locf are polymorphic, returning the type of the value they are given.
// Seagull types first and last from their value, but leaves locf of an aggregate as any,
// so the queries cast their results to double precision, which arrives as *float64.
//
// Takes queries (*store.Queries) which runs the query.
// Takes output (io.Writer) which receives the readings.
//
// Returns error when the query fails.
func printFirstAndLast(ctx context.Context, queries *store.Queries, output io.Writer) error {
	buoys, err := queries.FirstAndLast(ctx)
	if err != nil {
		return fmt.Errorf("finding first and last readings: %w", err)
	}
	fmt.Fprintln(output, "first and last reading (first, last):")
	for _, buoy := range buoys {
		fmt.Fprintf(output, "  %-13s first %s  last %s\n", buoy.Buoy, celsius(buoy.FirstTemperature), celsius(buoy.LastTemperature))
	}
	return nil
}

// celsius formats a temperature that may be NULL.
//
// Takes value (*float64) which is the temperature, or nil.
//
// Returns string which is the temperature to one decimal place, or a dash for nil.
func celsius(value *float64) string {
	if value == nil {
		return "   -"
	}
	return fmt.Sprintf("%4.1f", *value)
}
