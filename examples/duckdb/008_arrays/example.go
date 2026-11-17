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

// run records fishing voyages and reads their array fields.
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
		fmt.Fprintf(output, "  %-10s  from %s, %d stops\n", voyage.Boat, voyage.FirstPort.V, voyage.Stops)
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
