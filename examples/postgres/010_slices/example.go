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

	"seagull.sh/seagull/examples/postgres/010_slices/internal/store"
)

// run lets out beach huts by passing Go slices where the SQL has an IN list. A binding
// such as $1 as sgl.param(beaches, kind: slice) makes the parameter a slice, and the
// generated method rewrites IN ($1) to one placeholder per element at call time. A slice
// is numbered after every other parameter, so no other placeholder moves. An empty slice
// turns IN ($1) into = ANY('{}'), which matches nothing, and an expansion past
// PostgreSQL's limit of 65535 bind variables returns an error before the statement is
// sent.
//
// The rewrite happens before the statement runs, so sgl vet refuses a slice parameter on
// a batch or copyfrom query (Q017) or a dynamic: runtime query (Q018).
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
		found, err := queries.HutsOnBeaches(ctx, store.HutsOnBeachesParams{Beaches: beaches})
		if err != nil {
			return fmt.Errorf("finding huts on %v: %w", beaches, err)
		}
		numbers := make([]int64, 0, len(found))
		for _, hut := range found {
			numbers = append(numbers, hut.Number)
		}
		fmt.Fprintf(output, "huts on %v: %v\n", beaches, numbers)
	}

	// Hut 3 is taken by the first booking, so the second books only hut 5.
	for _, numbers := range [][]int64{{1, 3, 4}, {3, 5}} {
		booked, err := queries.BookHuts(ctx, store.BookHutsParams{Numbers: numbers})
		if err != nil {
			return fmt.Errorf("booking huts %v: %w", numbers, err)
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
