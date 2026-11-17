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

	"seagull.sh/seagull/examples/clickhouse/004_nullability/internal/store"
)

// run records gull sightings and reads nullable results.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives both listings.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	gulls := []store.RingGullParams{
		{ID: 1, Ring: "GB1001", Nickname: store.NullOf("Steven")},
		{ID: 2, Ring: "GB1002", Nickname: store.Null[string]{}},
		{ID: 3, Ring: "GB1003", Nickname: store.NullOf("Gwen")},
	}
	for _, gull := range gulls {
		if err := queries.RingGull(ctx, gull); err != nil {
			return fmt.Errorf("ringing %s: %w", gull.Ring, err)
		}
	}
	sightings := []store.RecordSightingParams{
		{ID: 1, GullID: 1, SeenOn: day(1), Place: "the pier"},
		{ID: 2, GullID: 1, SeenOn: day(3), Place: "the chip shop"},
		{ID: 3, GullID: 2, SeenOn: day(2), Place: "the harbour wall"},
	}
	for _, sighting := range sightings {
		if err := queries.RecordSighting(ctx, sighting); err != nil {
			return fmt.Errorf("recording a sighting: %w", err)
		}
	}

	rows, err := queries.ListSightings(ctx)
	if err != nil {
		return fmt.Errorf("listing sightings: %w", err)
	}
	fmt.Fprintln(output, "ring    nickname  place")
	for _, row := range rows {
		fmt.Fprintf(output, "%-7s %-9s %s\n", row.Ring, orNull(row.Nickname), orEmpty(row.Place))
	}

	summaries, err := queries.SummariseGulls(ctx)
	if err != nil {
		return fmt.Errorf("summarising gulls: %w", err)
	}
	fmt.Fprintln(output, "\nring    known as     times seen  last seen")
	for _, summary := range summaries {
		lastSeen := "NULL"
		if summary.LastSeen.Valid {
			lastSeen = summary.LastSeen.V.Format(time.DateOnly)
		}
		fmt.Fprintf(output, "%-7s %-12s %-11d %s\n", summary.Ring, summary.KnownAs, summary.TimesSeen, lastSeen)
	}
	return nil
}

// day returns a date in May 2026, at midnight UTC. A Date parameter binds the calendar
// date a time has in its own location.
//
// Takes date (int) which is the day of the month.
//
// Returns time.Time which is the date.
func day(date int) time.Time {
	return time.Date(2026, time.May, date, 0, 0, 0, 0, time.UTC)
}

// orNull returns the text a nullable field holds, or NULL when it holds none.
//
// Takes value (store.Null[string]) which is a nullable field.
//
// Returns string which is the text to print.
func orNull(value store.Null[string]) string {
	if !value.Valid {
		return "NULL"
	}
	return value.V
}

// orEmpty returns a string field's text, or (empty) when it holds the empty string, the
// default value ClickHouse gives a String column in an unmatched outer-join row.
//
// Takes value (string) which is a String column.
//
// Returns string which is the text to print.
func orEmpty(value string) string {
	if value == "" {
		return "(empty)"
	}
	return value
}
