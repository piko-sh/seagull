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
	"errors"
	"fmt"
	"io"
	"time"

	"seagull.sh/seagull/examples/timescaledb/033_timescale_continuous_aggregates/internal/store"
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

// readings are two buoys' samples either side of midnight, the same as in
// 032_timescale_hypertables.
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

// run records the buoys' readings, refreshes the hourly_readings continuous aggregate,
// and reads it back by the hour and rolled up over the whole night.
//
// Migration 0002 defines hourly_readings with CREATE MATERIALIZED VIEW ... WITH
// (timescaledb.continuous). TimescaleDB fills a new continuous aggregate as it creates
// it, which it refuses to do inside a transaction block, so the migration starts with
// sgl.migration(no_transaction: true) and runs outside one. Ending the view with WITH NO
// DATA would let it run in a transaction instead. Seagull reads the view's SELECT, so its
// columns arrive typed: bucket as time.Time, samples as int64.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the summaries.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)
	if err := record(ctx, queries, database); err != nil {
		return err
	}

	// timescaledb.materialized_only = true makes the view return only what a refresh has
	// written, so the readings are not in it yet.
	hours, err := queries.HourlyStats(ctx)
	if err != nil {
		return fmt.Errorf("reading the hourly summaries: %w", err)
	}
	fmt.Fprintf(output, "before the refresh, hourly_readings has %d rows\n", len(hours))

	if err := refresh(ctx, queries, database, output); err != nil {
		return err
	}
	if err := printHourly(ctx, queries, output); err != nil {
		return err
	}
	return printOverall(ctx, queries, output)
}

// record writes the night's readings in one transaction, so they arrive together.
//
// Takes queries (*store.Queries) which runs the inserts.
// Takes database (*sql.DB) which begins the transaction.
//
// Returns error when an insert fails; none of the readings are kept then.
func record(ctx context.Context, queries *store.Queries, database *sql.DB) error {
	start := time.Date(2026, time.May, 31, 22, 0, 0, 0, time.UTC)
	return queries.RunInTx(ctx, database, func(transaction *store.Queries) error {
		for _, sample := range readings {
			err := transaction.RecordReading(ctx, store.RecordReadingParams{
				Ts:          start.Add(time.Duration(sample.minutes) * time.Minute),
				Buoy:        sample.buoy,
				Temperature: sample.temperature,
			})
			if err != nil {
				return fmt.Errorf("recording a reading: %w", err)
			}
		}
		return nil
	})
}

// refresh materialises the continuous aggregate with CALL refresh_continuous_aggregate.
//
// The procedure commits as it goes, so TimescaleDB refuses to run it inside a transaction
// block: the call in RunInTx fails, and the one on the pool, outside any transaction,
// succeeds. Its window is NULL to NULL, everything, written into the SQL. Parameters
// would work too: Seagull names a CALL's parameters after the procedure's arguments and
// types these as *time.Time, so a nil bound leaves that end open.
//
// Takes queries (*store.Queries) which runs the refresh.
// Takes database (*sql.DB) which begins the transaction.
// Takes output (io.Writer) which receives what each attempt did.
//
// Returns error when the refresh outside the transaction fails, or the one inside
// succeeds.
func refresh(ctx context.Context, queries *store.Queries, database *sql.DB, output io.Writer) error {
	err := queries.RunInTx(ctx, database, func(transaction *store.Queries) error {
		return transaction.RefreshHourly(ctx)
	})
	if err == nil {
		return errors.New("refresh_continuous_aggregate ran inside a transaction")
	}
	fmt.Fprintf(output, "refreshing in RunInTx: %v\n", err)

	if err := queries.RefreshHourly(ctx); err != nil {
		return fmt.Errorf("refreshing hourly_readings: %w", err)
	}
	fmt.Fprintln(output, "refreshing outside a transaction: done")
	return nil
}

// printHourly prints each buoy's hourly row from the continuous aggregate. The view keeps
// a stats_agg summary per hour, and average reads the mean out of it.
//
// Takes queries (*store.Queries) which runs the query.
// Takes output (io.Writer) which receives the rows.
//
// Returns error when the query fails.
func printHourly(ctx context.Context, queries *store.Queries, output io.Writer) error {
	hours, err := queries.HourlyStats(ctx)
	if err != nil {
		return fmt.Errorf("reading the hourly summaries: %w", err)
	}
	fmt.Fprintf(output, "after the refresh, hourly_readings has %d rows:\n", len(hours))
	for _, hour := range hours {
		fmt.Fprintf(output, "  %s  %-13s  readings %d  mean %s\n",
			hour.Bucket.UTC().Format(clock), hour.Buoy, hour.Samples, celsius(hour.Mean))
	}
	return nil
}

// printOverall prints each buoy's statistics for the whole night, combined from the
// hourly summaries with rollup. A stats_agg summary keeps enough to combine exactly, so
// the rolled-up mean is the mean of every reading; averaging the hourly means instead
// weights a quiet hour the same as a busy one, and gives a different answer.
//
// Takes queries (*store.Queries) which runs the query.
// Takes output (io.Writer) which receives the statistics.
//
// Returns error when the query fails.
func printOverall(ctx context.Context, queries *store.Queries, output io.Writer) error {
	buoys, err := queries.OverallStats(ctx)
	if err != nil {
		return fmt.Errorf("rolling up the hourly summaries: %w", err)
	}
	fmt.Fprintln(output, "the whole night, rolled up from the hourly summaries:")
	for _, buoy := range buoys {
		samples := int64(0)
		if buoy.Samples != nil {
			samples = *buoy.Samples
		}
		fmt.Fprintf(output, "  %-13s  readings %d  mean %s  stddev %s  mean of hourly means %s\n",
			buoy.Buoy, samples, celsius(buoy.Mean), celsius(buoy.Spread), celsius(buoy.MeanOfHourlyMeans))
	}
	return nil
}

// celsius formats a temperature that may be NULL.
//
// Takes value (*float64) which is the temperature, or nil.
//
// Returns string which is the temperature to two decimal places, or a dash for nil.
func celsius(value *float64) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *value)
}
