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

	"seagull.sh/seagull/examples/clickhouse/013_pagination/internal/store"
)

// run logs seven rock pool finds and pages through them. A placeholder that is the whole
// LIMIT or OFFSET count becomes a plain int, whatever ClickHouse type it names, so
// {page_size:UInt32} gives an int field. On page_size, default: 3 replaces a zero value
// and max: 5 caps a larger one, before the query runs, so a caller cannot ask for every
// row at once; a negative size or offset becomes 0.
//
// ClickHouse has no auto-increment, so each find's id is a parameter.
//
// SearchFinds is the shape of a search endpoint: an optional filter, a fixed order and a
// page, all in one static query.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives each page.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	finds := []store.LogFindParams{
		{ID: 1, Creature: "crab", Pool: "north", SizeMm: 45},
		{ID: 2, Creature: "starfish", Pool: "east", SizeMm: 80},
		{ID: 3, Creature: "crab", Pool: "east", SizeMm: 60},
		{ID: 4, Creature: "blenny", Pool: "north", SizeMm: 70},
		{ID: 5, Creature: "crab", Pool: "south", SizeMm: 30},
		{ID: 6, Creature: "prawn", Pool: "north", SizeMm: 25},
		{ID: 7, Creature: "crab", Pool: "north", SizeMm: 55},
	}
	for _, find := range finds {
		if err := queries.LogFind(ctx, find); err != nil {
			return fmt.Errorf("logging a %s: %w", find.Creature, err)
		}
	}

	pages := []struct {
		label      string
		parameters store.ListFindsParams
	}{
		{label: "size 0 uses the default of 3", parameters: store.ListFindsParams{PageSize: 0, PageOffset: 0}},
		{label: "the next page, offset 3", parameters: store.ListFindsParams{PageSize: 3, PageOffset: 3}},
		{label: "size 50 is capped at 5", parameters: store.ListFindsParams{PageSize: 50, PageOffset: 0}},
	}
	for _, page := range pages {
		listed, err := queries.ListFinds(ctx, page.parameters)
		if err != nil {
			return fmt.Errorf("listing finds, %s: %w", page.label, err)
		}
		fmt.Fprintf(output, "%s:\n", page.label)
		for _, find := range listed {
			fmt.Fprintf(output, "  #%d %s in the %s pool\n", find.ID, find.Creature, find.Pool)
		}
	}

	for _, offset := range []int{0, 2} {
		crabs, err := queries.SearchFinds(ctx, store.SearchFindsParams{
			Creature:   new("crab"),
			PageSize:   2,
			PageOffset: offset,
		})
		if err != nil {
			return fmt.Errorf("searching crabs from %d: %w", offset, err)
		}
		fmt.Fprintf(output, "biggest crabs, offset %d:\n", offset)
		for _, crab := range crabs {
			fmt.Fprintf(output, "  #%d %dmm\n", crab.ID, crab.SizeMm)
		}
	}
	return nil
}
