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

	"seagull.sh/seagull/examples/postgres/019_ctes_and_windows/internal/store"
)

// run hatches a colony of gulls, records their snack thefts and reads them back three
// ways. Seagull follows each column's type and nullability through the SQL:
//
//   - FamilyTree's recursive CTE starts from one gull and joins its chicks generation by
//     generation; generation is an int64 from 0::BIGINT AS generation and generation + 1,
//     and parent is *string because the LEFT JOIN finds no parent for the first gull;
//   - TheftLeague counts thefts in a CTE, then ranks gulls on each beach with RANK() and
//     totals the beach with SUM() OVER; COUNT and RANK are bigint, so int64, while SUM of
//     a bigint is numeric in PostgreSQL, so BeachThefts is a nullable *apd.Decimal;
//   - BeachRecords reads the beach_records view from the migration as if it were a table,
//     and its columns keep the types the view's own SELECT gives them: MAX over a DATE
//     column is a *time.Time, nil for a beach with no thefts. The query is written as
//     SELECT *, and the generated SQL names the view's four columns instead, so a
//     migration that later adds a column to the view does not break the code already
//     deployed, which scans exactly those four. go.keep_stars: true in sgl.yaml keeps the
//     star as written.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the three listings.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	gulls := []store.HatchGullParams{
		{Name: "Steven", ParentID: nil, Beach: "Porthmeor"},
		{Name: "Gwen", ParentID: new(int64(1)), Beach: "Porthmeor"},
		{Name: "Jago", ParentID: new(int64(1)), Beach: "Porthminster"},
		{Name: "Kerensa", ParentID: new(int64(2)), Beach: "Porthmeor"},
		{Name: "Tristan", ParentID: nil, Beach: "Porthminster"},
		{Name: "Morwenna", ParentID: nil, Beach: "Gwithian"},
	}
	for _, gull := range gulls {
		if err := queries.HatchGull(ctx, gull); err != nil {
			return fmt.Errorf("hatching %s: %w", gull.Name, err)
		}
	}
	thefts := []store.RecordTheftParams{
		{GullID: 1, Snack: "chips", StolenOn: july(1)},
		{GullID: 1, Snack: "pasty", StolenOn: july(3)},
		{GullID: 1, Snack: "ice cream", StolenOn: july(4)},
		{GullID: 2, Snack: "chips", StolenOn: july(2)},
		{GullID: 3, Snack: "pasty", StolenOn: july(2)},
		{GullID: 3, Snack: "chips", StolenOn: july(5)},
		{GullID: 5, Snack: "sandwich", StolenOn: july(3)},
	}
	for _, theft := range thefts {
		if err := queries.RecordTheft(ctx, theft); err != nil {
			return fmt.Errorf("recording a theft: %w", err)
		}
	}

	family, err := queries.FamilyTree(ctx, 1)
	if err != nil {
		return fmt.Errorf("tracing Steven's family: %w", err)
	}
	fmt.Fprintln(output, "Steven's family:")
	for _, member := range family {
		fmt.Fprintf(output, "  generation %d  %-8s parent %s\n", member.Generation, member.Name, orNull(member.Parent))
	}

	league, err := queries.TheftLeague(ctx)
	if err != nil {
		return fmt.Errorf("ranking thieves: %w", err)
	}
	fmt.Fprintln(output, "theft league:")
	for _, entry := range league {
		beachThefts := "NULL"
		if entry.BeachThefts != nil {
			beachThefts = entry.BeachThefts.String()
		}
		fmt.Fprintf(output, "  %-13s #%d %-8s %d of %s\n", entry.Beach, entry.BeachRank, entry.Name, entry.Thefts, beachThefts)
	}

	records, err := queries.BeachRecords(ctx)
	if err != nil {
		return fmt.Errorf("reading beach records: %w", err)
	}
	fmt.Fprintln(output, "beach records:")
	for _, record := range records {
		lastTheft := "NULL"
		if record.LastTheft != nil {
			lastTheft = record.LastTheft.Format(time.DateOnly)
		}
		fmt.Fprintf(output, "  %-13s gulls %d, thefts %d, last theft %s\n", record.Beach, record.Gulls, record.Thefts, lastTheft)
	}
	return nil
}

// july returns a day in July 2026, the month the thefts happen in.
//
// Takes day (int) which is the day of the month.
//
// Returns time.Time which is midnight UTC on that day, as a DATE column holds it.
func july(day int) time.Time {
	return time.Date(2026, time.July, day, 0, 0, 0, 0, time.UTC)
}

// orNull formats a nullable field, or returns NULL when the field is nil.
//
// Takes value (*T) which is a nullable field.
//
// Returns string which is the text to print.
func orNull[T any](value *T) string {
	if value == nil {
		return "NULL"
	}
	return fmt.Sprint(*value)
}
