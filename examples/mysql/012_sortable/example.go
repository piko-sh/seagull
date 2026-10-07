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

	"seagull.sh/seagull/examples/mysql/012_sortable/internal/store"
)

// run lists a regatta's entries in four orders. sgl.sortable(order_by, [boat, crew,
// finish_seconds]) has no placeholder: it gives ListEntriesParams an OrderBy field, typed
// with one constant per allowed column, and an OrderByDirection. The method appends ORDER
// BY only when OrderBy is one of the allowed columns, so a value that is not, such as raw
// text from a request, adds nothing to the SQL and the rows come back unsorted. A
// sortable column the query does not select makes sgl vet report Q011.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives each listing.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	entries := []store.EnterBoatParams{
		{Boat: "Kittiwake", Crew: 4, FinishSeconds: 3725},
		{Boat: "Salty Chip", Crew: 3, FinishSeconds: 3410},
		{Boat: "Gull Wing", Crew: 5, FinishSeconds: 3990},
		{Boat: "Driftwood", Crew: 2, FinishSeconds: 3588},
	}
	for _, entry := range entries {
		if err := queries.EnterBoat(ctx, entry); err != nil {
			return fmt.Errorf("entering %s: %w", entry.Boat, err)
		}
	}

	orders := []struct {
		label      string
		parameters store.ListEntriesParams
	}{
		{label: "results", parameters: store.ListEntriesParams{OrderBy: store.ListEntriesOrderByFinishSeconds, OrderByDirection: store.OrderAsc}},
		{label: "by boat", parameters: store.ListEntriesParams{OrderBy: store.ListEntriesOrderByBoat, OrderByDirection: store.OrderAsc}},
		{label: "largest crew first", parameters: store.ListEntriesParams{OrderBy: store.ListEntriesOrderByCrew, OrderByDirection: store.OrderDesc}},
		{label: "raw text, ignored", parameters: store.ListEntriesParams{OrderBy: "boat; DROP TABLE entries", OrderByDirection: store.OrderAsc}},
	}
	for _, order := range orders {
		listed, err := queries.ListEntries(ctx, order.parameters)
		if err != nil {
			return fmt.Errorf("listing entries %s: %w", order.label, err)
		}
		fmt.Fprintf(output, "%s:\n", order.label)
		for _, entry := range listed {
			finish := time.Duration(entry.FinishSeconds) * time.Second
			fmt.Fprintf(output, "  %-10s crew %d  %s\n", entry.Boat, entry.Crew, finish)
		}
	}
	return nil
}
