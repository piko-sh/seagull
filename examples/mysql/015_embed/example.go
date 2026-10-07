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

	"seagull.sh/seagull/examples/mysql/015_embed/internal/store"
)

// run adds a small fleet and reads it back with joined tables as nested structs.
// sgl.embed(table) gathers the columns the query selects from that table into a field
// named after the table, here Harbours and Skippers, with its own struct type named after
// the query and the table, such as GetBoatHarbours.
//
// GetBoat uses an INNER JOIN, so every boat has a harbour and Harbours is a value.
// ListCrews uses a LEFT JOIN, so a boat may have no skipper: Skippers is a pointer that
// is nil when the joined row is missing, and its fields are pointers too, as each column
// of a LEFT JOIN can be NULL.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the boat and the crew list.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	if err := queries.AddHarbour(ctx, store.AddHarbourParams{Name: "Newlyn", County: "Cornwall"}); err != nil {
		return fmt.Errorf("adding a harbour: %w", err)
	}
	if err := queries.AddSkipper(ctx, store.AddSkipperParams{Name: "Morwenna Penrose", Licence: "MCA-1182"}); err != nil {
		return fmt.Errorf("adding a skipper: %w", err)
	}
	boats := []store.AddBoatParams{
		{Name: "Kittiwake", HarbourID: 1, SkipperID: new(int64(1))},
		{Name: "Salty Chip", HarbourID: 1, SkipperID: nil},
	}
	for _, boat := range boats {
		if err := queries.AddBoat(ctx, boat); err != nil {
			return fmt.Errorf("adding %s: %w", boat.Name, err)
		}
	}

	boat, err := queries.GetBoat(ctx, 2)
	if err != nil {
		return fmt.Errorf("getting boat 2: %w", err)
	}
	fmt.Fprintf(output, "%s is moored at %s, %s\n", boat.Name, boat.Harbours.Name, boat.Harbours.County)

	crews, err := queries.ListCrews(ctx)
	if err != nil {
		return fmt.Errorf("listing crews: %w", err)
	}
	for _, crew := range crews {
		if crew.Skippers == nil {
			fmt.Fprintf(output, "%s has no skipper\n", crew.Name)
			continue
		}
		fmt.Fprintf(output, "%s is skippered by %s, licence %s\n", crew.Name, *crew.Skippers.Name, *crew.Skippers.Licence)
	}
	return nil
}
