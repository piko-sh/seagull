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

	"seagull.sh/seagull/examples/mysql/019_ctes_and_windows/internal/store"
)

// run hatches a colony of gulls, records their snack thefts and reads them back three
// ways. Seagull follows each column's type and nullability through the SQL:
//
//   - FamilyTree's recursive CTE starts from one gull and joins its chicks generation by
//     generation; generation is an integer from 0 AS generation and generation + 1, and
//     parent is *string because the LEFT JOIN finds no parent for the first gull;
//   - TheftLeague counts thefts in a CTE, then ranks gulls on each beach with RANK() and
//     totals the beach with SUM() OVER; RANK is never NULL, while SUM is nullable, as SQL
//     sums no rows to NULL, and it is a DECIMAL, as MySQL sums integers exactly into a
//     DECIMAL, so BeachThefts is *apd.Decimal;
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
		{GullID: 1, Snack: "chips", StolenOn: day(1)},
		{GullID: 1, Snack: "pasty", StolenOn: day(3)},
		{GullID: 1, Snack: "ice cream", StolenOn: day(4)},
		{GullID: 2, Snack: "chips", StolenOn: day(2)},
		{GullID: 3, Snack: "pasty", StolenOn: day(2)},
		{GullID: 3, Snack: "chips", StolenOn: day(5)},
		{GullID: 5, Snack: "sandwich", StolenOn: day(3)},
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
		fmt.Fprintf(output, "  %-13s #%d %-8s %d of %s\n", entry.Beach, entry.BeachRank, entry.Name, entry.Thefts, orNull(entry.BeachThefts))
	}

	records, err := queries.BeachRecords(ctx)
	if err != nil {
		return fmt.Errorf("reading beach records: %w", err)
	}
	fmt.Fprintln(output, "beach records:")
	for _, record := range records {
		fmt.Fprintf(output, "  %-13s gulls %d, thefts %d, last theft %s\n", record.Beach, record.Gulls, record.Thefts, orNull(record.LastTheft))
	}
	return nil
}

// day returns midnight UTC on a day in July 2026, the value a DATE column holds.
//
// Takes date (int) which is the day of the month.
//
// Returns time.Time which is the start of that day.
func day(date int) time.Time {
	return time.Date(2026, time.July, date, 0, 0, 0, 0, time.UTC)
}

// orNull formats a nullable field, or returns NULL when the field is nil. A date prints
// as YYYY-MM-DD, and a decimal through its String method, which apd.Decimal declares on
// its pointer.
//
// Takes value (*T) which is a nullable field.
//
// Returns string which is the text to print.
func orNull[T any](value *T) string {
	if value == nil {
		return "NULL"
	}
	switch field := any(value).(type) {
	case *time.Time:
		return field.Format(time.DateOnly)
	case fmt.Stringer:
		return field.String()
	default:
		return fmt.Sprint(*value)
	}
}
