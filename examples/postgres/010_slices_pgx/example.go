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
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"

	"seagull.sh/seagull/examples/postgres/010_slices_pgx/internal/store"
)

// run lets out beach huts by passing Go slices where the SQL has an IN list, with code
// generated for pgx. A binding such as $1 as sgl.param(beaches, kind: slice) makes the
// parameter a slice, and the pgx emitter rewrites IN ($1) to = ANY($1) when it generates
// the code; open internal/store to see it. The whole slice is sent as one PostgreSQL
// array parameter, so the SQL text never changes with the slice's length, pgx can cache
// the prepared statement, and there is no bind-variable limit to reach. An empty slice is
// an empty array and a nil slice is NULL, and = ANY matches nothing for either.
//
// sgl vet still refuses a slice parameter on a batch or copyfrom query (Q017) or a
// dynamic: runtime query (Q018).
//
// Takes pool (*pgxpool.Pool) which is a pool on a migrated database.
// Takes output (io.Writer) which receives each search and booking.
//
// Returns error when a query fails.
func run(ctx context.Context, pool *pgxpool.Pool, output io.Writer) error {
	queries := store.New(pool)

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
