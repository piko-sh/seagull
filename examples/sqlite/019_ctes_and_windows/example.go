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

	"seagull.sh/seagull/examples/sqlite/019_ctes_and_windows/internal/store"
)

// run reads gull thefts with CTE and window queries.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the three listings.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	gulls := []store.HatchGullParams{
		{Name: "Steven", ParentID: store.Null[int64]{}, Beach: "Porthmeor"},
		{Name: "Gwen", ParentID: store.NullOf(int64(1)), Beach: "Porthmeor"},
		{Name: "Jago", ParentID: store.NullOf(int64(1)), Beach: "Porthminster"},
		{Name: "Kerensa", ParentID: store.NullOf(int64(2)), Beach: "Porthmeor"},
		{Name: "Tristan", ParentID: store.Null[int64]{}, Beach: "Porthminster"},
		{Name: "Morwenna", ParentID: store.Null[int64]{}, Beach: "Gwithian"},
	}
	for _, gull := range gulls {
		if err := queries.HatchGull(ctx, gull); err != nil {
			return fmt.Errorf("hatching %s: %w", gull.Name, err)
		}
	}
	thefts := []store.RecordTheftParams{
		{GullID: 1, Snack: "chips", StolenOn: "2026-07-01"},
		{GullID: 1, Snack: "pasty", StolenOn: "2026-07-03"},
		{GullID: 1, Snack: "ice cream", StolenOn: "2026-07-04"},
		{GullID: 2, Snack: "chips", StolenOn: "2026-07-02"},
		{GullID: 3, Snack: "pasty", StolenOn: "2026-07-02"},
		{GullID: 3, Snack: "chips", StolenOn: "2026-07-05"},
		{GullID: 5, Snack: "sandwich", StolenOn: "2026-07-03"},
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

// orNull formats a nullable field, or returns NULL when it holds none.
//
// Takes value (store.Null[T]) which is a nullable field.
//
// Returns string which is the text to print.
func orNull[T any](value store.Null[T]) string {
	if !value.Valid {
		return "NULL"
	}
	return fmt.Sprint(value.V)
}
