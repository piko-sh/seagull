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
	"strings"

	"seagull.sh/seagull/examples/clickhouse/007_enums/internal/domain"
	"seagull.sh/seagull/examples/clickhouse/007_enums/internal/store"
)

// run berths four boats and lists them twice. boats.status is an Enum8, and Seagull
// declares a named Go type for it in models.go, named after the table and column that
// hold it: store.BoatsStatus, with a constant for each label, such as
// store.BoatsStatusMoored, and a Valid method. ListBoats returns Status as a
// store.BoatsStatus. The status parameters are written {status:String}, so they are plain
// strings, and the server converts the label to the Enum8.
//
// BoatsNotIn adds sgl.column(status, go_type: "…/internal/domain.Status"), so its rows
// carry the harbour's own domain.Status type, with its constants and methods. The
// override changes only that query's result rows: parameters stay string, so the example
// converts with string(domain.StatusInDock) on the way in. A types.overrides entry in
// sgl.yaml would change every query's rows the same way.
//
// ClickHouse checks every label against the Enum8, so a status that is not one of its
// labels is refused by the server, not stored.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives both listings and the refusal.
//
// Returns error when a query fails or a bad status is accepted.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	boats := []store.AddBoatParams{
		{ID: 1, Name: "Kittiwake", Status: string(domain.StatusMoored)},
		{ID: 2, Name: "Salty Chip", Status: string(domain.StatusFishing)},
		{ID: 3, Name: "Gull Wing", Status: string(domain.StatusInDock)},
		{ID: 4, Name: "Driftwood", Status: string(domain.StatusFishing)},
	}
	for _, boat := range boats {
		if err := queries.AddBoat(ctx, boat); err != nil {
			return fmt.Errorf("adding %s: %w", boat.Name, err)
		}
	}

	listed, err := queries.ListBoats(ctx)
	if err != nil {
		return fmt.Errorf("listing boats: %w", err)
	}
	fmt.Fprintln(output, "all boats, status as store.BoatsStatus:")
	for _, boat := range listed {
		fmt.Fprintf(output, "  %d %-11s %s\n", boat.ID, boat.Name, boat.Status)
	}

	afloat, err := queries.BoatsNotIn(ctx, string(domain.StatusInDock))
	if err != nil {
		return fmt.Errorf("listing boats not in dock: %w", err)
	}
	fmt.Fprintln(output, "not in dock, status as domain.Status:")
	for _, boat := range afloat {
		fmt.Fprintf(output, "  %d %-11s %-8s at sea: %t\n", boat.ID, boat.Name, boat.Status, boat.Status.AtSea())
	}

	sunk := store.AddBoatParams{ID: 5, Name: "Old Barnacle", Status: "sunk"}
	err = queries.AddBoat(ctx, sunk)
	if err == nil {
		return fmt.Errorf("adding status %q: got no error, want the server's refusal", sunk.Status)
	}
	if !strings.Contains(err.Error(), "Unknown element 'sunk' for enum") {
		return fmt.Errorf("adding status %q: got %w, want the server's refusal", sunk.Status, err)
	}
	fmt.Fprintf(output, "status %q refused: not one of the Enum8 labels\n", sunk.Status)
	return nil
}
