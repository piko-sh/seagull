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

	"seagull.sh/seagull/examples/clickhouse/019_ctes_and_windows/internal/store"
)

// run hatches a colony of gulls, records their snack thefts and reads them back three
// ways. Seagull follows each column's type and nullability through the SQL, by
// ClickHouse's own rules:
//
//   - FamilyTree's recursive CTE starts from one gull and joins its chicks generation by
//     generation. The literal 0 is a UInt8 and adding 1 to a UInt8 gives a UInt16, so
//     Seagull makes Generation a uint16. The server keeps the first SELECT's UInt8 for a
//     recursive CTE's column, and database/sql converts each value to the wider field.
//     parent is *string because the LEFT JOIN, with join_use_nulls = 1, finds no parent
//     for the first gull;
//   - TheftLeague counts thefts in a CTE, so stolen is uint64, then ranks gulls on each
//     beach with rank() OVER, a UInt64, and totals the beach with sum() OVER, a UInt64
//     too, since ClickHouse sums unsigned integers to UInt64 and sums no rows to 0, not
//     NULL. Neither can be NULL, so BeachRank and BeachThefts are plain uint64;
//   - BeachRecords reads the beach_records view from the migration as if it were a table,
//     and its columns keep the types the view's own SELECT gives them.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the three listings.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	gulls := []store.HatchGullParams{
		{ID: 1, Name: "Steven", ParentID: nil, Beach: "Porthmeor"},
		{ID: 2, Name: "Gwen", ParentID: new(uint64(1)), Beach: "Porthmeor"},
		{ID: 3, Name: "Jago", ParentID: new(uint64(1)), Beach: "Porthminster"},
		{ID: 4, Name: "Kerensa", ParentID: new(uint64(2)), Beach: "Porthmeor"},
		{ID: 5, Name: "Tristan", ParentID: nil, Beach: "Porthminster"},
		{ID: 6, Name: "Morwenna", ParentID: nil, Beach: "Gwithian"},
	}
	for _, gull := range gulls {
		if err := queries.HatchGull(ctx, gull); err != nil {
			return fmt.Errorf("hatching %s: %w", gull.Name, err)
		}
	}
	thefts := []store.RecordTheftParams{
		{ID: 1, GullID: 1, Snack: "chips", StolenOn: day(1)},
		{ID: 2, GullID: 1, Snack: "pasty", StolenOn: day(3)},
		{ID: 3, GullID: 1, Snack: "ice cream", StolenOn: day(4)},
		{ID: 4, GullID: 2, Snack: "chips", StolenOn: day(2)},
		{ID: 5, GullID: 3, Snack: "pasty", StolenOn: day(2)},
		{ID: 6, GullID: 3, Snack: "chips", StolenOn: day(5)},
		{ID: 7, GullID: 5, Snack: "sandwich", StolenOn: day(3)},
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
		fmt.Fprintf(output, "  %-13s #%d %-8s %d of %d\n", entry.Beach, entry.BeachRank, entry.Name, entry.Stolen, entry.BeachThefts)
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

// day returns a date in July 2026, at midnight UTC. A Date parameter binds the calendar
// date a time has in its own location.
//
// Takes date (int) which is the day of the month.
//
// Returns time.Time which is the date.
func day(date int) time.Time {
	return time.Date(2026, time.July, date, 0, 0, 0, 0, time.UTC)
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
