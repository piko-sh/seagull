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

	"seagull.sh/seagull/examples/duckdb/008_arrays/internal/store"
)

// run logs three fishing voyages and reads them back. A DuckDB list column becomes a
// typed Go slice: VARCHAR[] gives []*string and INTEGER[] gives []*int32, and array_agg
// over the NOT NULL id column gives []int64: the list can hold no NULL, so its elements
// are plain values.
//
// The elements are pointers because DuckDB can hold NULL in any list, whatever the
// column's type says, so a NULL element reads as nil, which elements prints as NULL.
// Parameters keep plain elements, so LogVoyage takes []string and []int32. When the data
// never holds a NULL element, non_null_elements: true opts out, in an sgl.column line of
// a query or migration or as go.types.non_null_elements in sgl.yaml: the elements then
// read as plain values, and a NULL one fails the read.
//
// A slice parameter is bound as a DuckDB list. duckdb-go would bind a nil slice as an
// empty list, so the generated code converts the nullable sightings parameter and binds a
// nil slice as NULL, as other drivers do: Salty Chip's Sightings: nil is stored as NULL,
// and the second Kittiwake voyage's empty slice as an empty list.
//
// The generated SQL selects each list column as it is, and the generated code reads the
// list duckdb-go returns into the slice; open internal/store to see it. A NULL list
// therefore scans to a nil slice, and an empty list to an empty one.
//
// VoyagesCallingAt searches inside a list with $1 = ANY(ports), whose parameter is pinned
// with sgl.param(port, type: varchar) as inference leaves it as any, and reads one
// element with ports[1], which is *string because a missing element is NULL. DuckDB lists
// count from 1, like PostgreSQL arrays.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the voyages and the searches.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	voyages := []store.LogVoyageParams{
		{Boat: "Kittiwake", Ports: []string{"Newlyn", "St Ives", "Mousehole"}, CatchKg: []int32{120, 85}, Sightings: []string{"dolphin", "basking shark"}},
		{Boat: "Salty Chip", Ports: []string{"Penzance", "Mousehole"}, CatchKg: []int32{40}, Sightings: nil},
		{Boat: "Kittiwake", Ports: []string{"Newlyn", "Lamorna"}, CatchKg: []int32{60, 30, 15}, Sightings: []string{}},
	}
	for _, voyage := range voyages {
		if err := queries.LogVoyage(ctx, voyage); err != nil {
			return fmt.Errorf("logging %s's voyage: %w", voyage.Boat, err)
		}
	}

	logged, err := queries.ListVoyages(ctx)
	if err != nil {
		return fmt.Errorf("listing voyages: %w", err)
	}
	for _, voyage := range logged {
		sightings := elements(voyage.Sightings)
		if voyage.Sightings == nil {
			sightings = "NULL"
		}
		fmt.Fprintf(output, "voyage %d  %-10s  ports %s, catch %s kg, sightings %s\n", voyage.ID, voyage.Boat, elements(voyage.Ports), elements(voyage.CatchKg), sightings)
	}

	calling, err := queries.VoyagesCallingAt(ctx, "Mousehole")
	if err != nil {
		return fmt.Errorf("finding voyages calling at Mousehole: %w", err)
	}
	fmt.Fprintln(output, "\ncalling at Mousehole:")
	for _, voyage := range calling {
		fmt.Fprintf(output, "  %-10s  from %s, %d stops\n", voyage.Boat, *voyage.FirstPort, voyage.Stops)
	}

	perBoat, err := queries.VoyagesPerBoat(ctx)
	if err != nil {
		return fmt.Errorf("grouping voyages by boat: %w", err)
	}
	fmt.Fprintln(output, "\nvoyages per boat:")
	for _, boat := range perBoat {
		fmt.Fprintf(output, "  %-10s  %v\n", boat.Boat, boat.Voyages)
	}
	return nil
}

// elements returns a list's elements as fmt prints a slice, with NULL for each element
// the database holds NULL in.
//
// Takes list ([]*T) which holds the elements, nil for a NULL one.
//
// Returns string which is the elements in brackets, separated by spaces.
func elements[T any](list []*T) string {
	texts := make([]string, 0, len(list))
	for _, element := range list {
		if element == nil {
			texts = append(texts, "NULL")
			continue
		}
		texts = append(texts, fmt.Sprint(*element))
	}
	return fmt.Sprint(texts)
}
