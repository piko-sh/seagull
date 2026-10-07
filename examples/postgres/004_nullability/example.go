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

	"seagull.sh/seagull/examples/postgres/004_nullability/internal/store"
)

// run rings three gulls, records where two of them were seen, and lists them twice.
// Seagull decides each field's nullability from the SQL, and a field that can be NULL is
// a pointer:
//
//   - gulls.nickname has no NOT NULL, so Nickname is *string, as a parameter and a
//     column;
//   - sightings.place is NOT NULL, but the LEFT JOIN gives a gull with no sightings a
//     NULL place, so Place is *string;
//   - COALESCE with a non-NULL last argument is never NULL, so KnownAs is string;
//   - COUNT is never NULL, but MAX over no rows is, so TimesSeen is int64 and LastSeen is
//     *time.Time, keeping the DATE column's type.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives both listings.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	gulls := []store.RingGullParams{
		{Ring: "GB1001", Nickname: new("Steven")},
		{Ring: "GB1002", Nickname: nil},
		{Ring: "GB1003", Nickname: new("Gwen")},
	}
	for _, gull := range gulls {
		if err := queries.RingGull(ctx, gull); err != nil {
			return fmt.Errorf("ringing %s: %w", gull.Ring, err)
		}
	}
	sightings := []store.RecordSightingParams{
		{GullID: 1, SeenOn: mayDay(1), Place: "the pier"},
		{GullID: 1, SeenOn: mayDay(3), Place: "the chip shop"},
		{GullID: 2, SeenOn: mayDay(2), Place: "the harbour wall"},
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
		fmt.Fprintf(output, "%-7s %-9s %s\n", row.Ring, orNull(row.Nickname), orNull(row.Place))
	}

	summaries, err := queries.SummariseGulls(ctx)
	if err != nil {
		return fmt.Errorf("summarising gulls: %w", err)
	}
	fmt.Fprintln(output, "\nring    known as     times seen  last seen")
	for _, summary := range summaries {
		lastSeen := "NULL"
		if summary.LastSeen != nil {
			lastSeen = summary.LastSeen.Format(time.DateOnly)
		}
		fmt.Fprintf(output, "%-7s %-12s %-11d %s\n", summary.Ring, summary.KnownAs, summary.TimesSeen, lastSeen)
	}
	return nil
}

// mayDay returns a day in May 2026, for the DATE column.
//
// Takes day (int) which is the day of the month.
//
// Returns time.Time which is midnight UTC on that day.
func mayDay(day int) time.Time {
	return time.Date(2026, time.May, day, 0, 0, 0, 0, time.UTC)
}

// orNull returns the text a nullable field holds, or NULL when the field is nil.
//
// Takes value (*string) which is a nullable field.
//
// Returns string which is the text to print.
func orNull(value *string) string {
	if value == nil {
		return "NULL"
	}
	return *value
}
