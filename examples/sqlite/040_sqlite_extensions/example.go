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

	"seagull.sh/seagull/examples/sqlite/040_sqlite_extensions/internal/store"
)

// places is the harbour guide. Each place covers a box on a map measured in metres;
// R*Tree stores coordinates as 32-bit floats, which hold whole numbers like these
// exactly.
var places = []store.AddPlaceParams{
	{
		Name:        "The Old Pier",
		Description: "Wooden pier where the herring gulls gather at low tide",
		Tags:        `["gulls", "fishing"]`,
		West:        0,
		East:        20,
		South:       0,
		North:       6,
	},
	{
		Name:        "Harbour Wall",
		Description: "Stone breakwater with kittiwakes nesting on its seaward side",
		Tags:        `["birds", "walk"]`,
		West:        30,
		East:        90,
		South:       0,
		North:       4,
	},
	{
		Name:        "Chip Shop",
		Description: "Cod and chips to take away; guard them from the gulls",
		Tags:        `["food"]`,
		West:        10,
		East:        18,
		South:       20,
		North:       28,
	},
	{
		Name:        "Lifeboat Station",
		Description: "Open to visitors on Sunday afternoons",
		Tags:        `["walk", "history", "boats"]`,
		West:        60,
		East:        75,
		South:       15,
		North:       30,
	},
}

// run fills the harbour guide, then finds places three ways: by the words in their
// descriptions with FTS5, by a point on the map with R*Tree, and by tag with json_each.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives what each search found.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	// tag_count is a generated column: SQLite works it out from tags, and RETURNING hands it
	// back. A query that tried to write it would fail, and sgl vet reports Q013 before it
	// gets that far. The trigger in the migration copies each new place into the FTS5 and
	// R*Tree tables.
	for _, place := range places {
		added, err := queries.AddPlace(ctx, place)
		if err != nil {
			return fmt.Errorf("adding %s: %w", place.Name, err)
		}
		fmt.Fprintf(output, "added %d %-16s tag_count=%d\n", added.ID, place.Name, added.TagCount)
	}

	// FTS5 full-text search: MATCH takes a query in FTS5 syntax, gull* matches any word
	// starting gull, rank orders the best match first, and snippet marks the matching words.
	found, err := queries.SearchDescriptions(ctx, "gull*")
	if err != nil {
		return fmt.Errorf("searching descriptions: %w", err)
	}
	fmt.Fprintln(output, `descriptions matching "gull*":`)
	for _, place := range found {
		excerpt := ""
		if place.Excerpt != nil {
			excerpt = *place.Excerpt
		}
		fmt.Fprintf(output, "  %-12s %s\n", place.Name, excerpt)
	}

	// R*Tree: which boxes contain a point, answered from the spatial index.
	for _, point := range []store.PlacesAtParams{{X: 12, Y: 4}, {X: 70, Y: 20}} {
		here, err := queries.PlacesAt(ctx, point)
		if err != nil {
			return fmt.Errorf("finding what is at (%g, %g): %w", point.X, point.Y, err)
		}
		for _, place := range here {
			fmt.Fprintf(output, "at (%g, %g): %s\n", point.X, point.Y, place.Name)
		}
	}

	// json_each turns a JSON array into rows, one per element, so a WHERE clause can look
	// inside the tags column.
	tagged, err := queries.PlacesTagged(ctx, "walk")
	if err != nil {
		return fmt.Errorf("finding places tagged walk: %w", err)
	}
	for _, place := range tagged {
		fmt.Fprintf(output, "tagged walk: %-16s tag_count=%d\n", place.Name, place.TagCount)
	}
	return nil
}
