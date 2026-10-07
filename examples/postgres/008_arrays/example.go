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

	"seagull.sh/seagull/examples/postgres/008_arrays/internal/store"
)

// run logs three fishing voyages and reads them back. An array column becomes a typed Go
// slice: TEXT[] gives []*string and BIGINT[] gives []*int64, and array_agg gives a slice
// of its argument's type, here []int64: the id column it collects is NOT NULL, so the
// array can hold no NULL and its elements are plain values. A slice parameter is sent as
// a PostgreSQL array, and a nil slice is sent as NULL.
//
// The elements are pointers because PostgreSQL can hold NULL in any array, whatever the
// column's type says, so a NULL element reads as nil, which elements prints as NULL.
// Parameters take the same pointers, so LogVoyage takes []*int64 for catch_kg and
// []*string for sightings, and a nil element writes NULL; values builds them from plain
// values. When the data never holds a NULL element, non_null_elements: true opts out, in
// an sgl.column line of a query or migration or as go.types.non_null_elements in
// sgl.yaml: the elements then read and bind as plain values, and a NULL one fails the
// read. The migration declares it for ports, as a voyage never calls at a NULL port, so
// Ports is a plain []string in the model, the row and the parameters alike.
//
// database/sql cannot scan a PostgreSQL array, so the generated SQL wraps each array
// column in to_json() and decodes the JSON into the slice; open internal/store to see it.
// A NULL array therefore scans to a nil slice, and an empty array to an empty one.
//
// VoyagesCallingAt searches inside an array with $1::text = ANY(ports). The cast gives
// the parameter its type, for Seagull, which would otherwise leave it as any, and for
// CockroachDB, which cannot work it out from ANY. ports[1] reads one element, and is
// *string because a missing element is NULL.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the voyages and the searches.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	voyages := []store.LogVoyageParams{
		{Boat: "Kittiwake", Ports: []string{"Newlyn", "St Ives", "Mousehole"}, CatchKg: values[int64](120, 85), Sightings: values("dolphin", "basking shark")},
		{Boat: "Salty Chip", Ports: []string{"Penzance", "Mousehole"}, CatchKg: values[int64](40), Sightings: nil},
		{Boat: "Kittiwake", Ports: []string{"Newlyn", "Lamorna"}, CatchKg: values[int64](60, 30, 15), Sightings: []*string{}},
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
		fmt.Fprintf(output, "voyage %d  %-10s  ports %s, catch %s kg, sightings %s\n", voyage.ID, voyage.Boat, voyage.Ports, elements(voyage.CatchKg), sightings)
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

// elements returns an array's elements as fmt prints a slice, with NULL for each element
// the database holds NULL in.
//
// Takes array ([]*T) which holds the elements, nil for a NULL one.
//
// Returns string which is the elements in brackets, separated by spaces.
func elements[T any](array []*T) string {
	texts := make([]string, 0, len(array))
	for _, element := range array {
		if element == nil {
			texts = append(texts, "NULL")
			continue
		}
		texts = append(texts, fmt.Sprint(*element))
	}
	return fmt.Sprint(texts)
}

// values returns pointers to copies of plain values, as an array parameter whose elements
// may be NULL takes them.
//
// Takes items (...T) which are the elements.
//
// Returns []*T which points at the copies, none of them nil.
func values[T any](items ...T) []*T {
	pointed := make([]*T, len(items))
	for index := range items {
		pointed[index] = &items[index]
	}
	return pointed
}
