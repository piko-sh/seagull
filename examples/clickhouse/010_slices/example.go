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

	"seagull.sh/seagull/examples/clickhouse/010_slices/internal/store"
)

// run lets out beach huts by passing Go slices where the SQL has an IN list. ClickHouse
// takes a whole array as one parameter, so the query says IN {beaches:Array(String)} and
// the method takes a []string; no kind: slice binding is needed and the SQL is not
// rewritten at call time. The slice is sent as one array value however long it is, and an
// empty slice is an empty array, which matches nothing.
//
// Other engines expand IN (?1) into one placeholder per element, which is why sgl vet
// refuses a kind: slice parameter on a batch query (Q017) or a dynamic: runtime query
// (Q018); an Array parameter is a single value, so neither applies here.
//
// ALTER TABLE ... UPDATE reports no changed rows, so BookHuts is asyncexec and the
// example counts the booked huts before and after each booking.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives each search and booking.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	huts := []store.AddHutParams{
		{Number: 1, Beach: "Polzeath", Colour: "blue"},
		{Number: 2, Beach: "Polzeath", Colour: "yellow"},
		{Number: 3, Beach: "Polzeath", Colour: "red"},
		{Number: 4, Beach: "Watergate", Colour: "blue"},
		{Number: 5, Beach: "Watergate", Colour: "red"},
		{Number: 6, Beach: "Fistral", Colour: "yellow"},
	}
	for _, hut := range huts {
		if err := queries.AddHut(ctx, hut); err != nil {
			return fmt.Errorf("adding hut %d: %w", hut.Number, err)
		}
	}

	for _, beaches := range [][]string{{"Polzeath", "Fistral"}, {"Watergate"}, {}} {
		found, err := queries.HutsOnBeaches(ctx, beaches)
		if err != nil {
			return fmt.Errorf("finding huts on %v: %w", beaches, err)
		}
		numbers := make([]uint64, 0, len(found))
		for _, hut := range found {
			numbers = append(numbers, hut.Number)
		}
		fmt.Fprintf(output, "huts on %v: %v\n", beaches, numbers)
	}

	// Hut 3 is taken by the first booking, so the second books only hut 5.
	for _, numbers := range [][]uint64{{1, 3, 4}, {3, 5}} {
		booked, err := bookHuts(ctx, queries, numbers)
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "booking huts %v booked %d\n", numbers, booked)
	}

	free, err := queries.FreeHutsInColours(ctx, store.FreeHutsInColoursParams{
		Beach:   "Polzeath",
		Colours: []string{"blue", "yellow"},
	})
	if err != nil {
		return fmt.Errorf("finding free huts: %w", err)
	}
	for _, hut := range free {
		fmt.Fprintf(output, "hut %d at Polzeath is %s and free\n", hut.Number, hut.Colour)
	}
	return nil
}

// bookHuts books the huts that are free among numbers and returns how many it booked, by
// counting the booked huts before and after. The connection sets mutations_sync=2, so the
// mutation has finished when BookHuts returns.
//
// Takes queries (*store.Queries) which runs the queries.
// Takes numbers ([]uint64) which are the huts to book.
//
// Returns uint64 which is how many huts were booked.
// Returns error when a query fails.
func bookHuts(ctx context.Context, queries *store.Queries, numbers []uint64) (uint64, error) {
	before, err := queries.CountBooked(ctx)
	if err != nil {
		return 0, fmt.Errorf("counting booked huts: %w", err)
	}
	if err := queries.BookHuts(ctx, numbers); err != nil {
		return 0, fmt.Errorf("booking huts %v: %w", numbers, err)
	}
	after, err := queries.CountBooked(ctx)
	if err != nil {
		return 0, fmt.Errorf("counting booked huts: %w", err)
	}
	return after.Huts - before.Huts, nil
}
