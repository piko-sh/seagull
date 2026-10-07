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

	"seagull.sh/seagull/examples/clickhouse/008_arrays/internal/store"
)

// run lands four hauls of fish and reads them back. An Array(T) column arrives as a Go
// slice of T's type, so species, an Array(String), is []string and weights_kg, an
// Array(UInt32), is []uint32, in result rows and in parameters alike: LandHaul takes the
// slices and binds each as one {species:Array(String)} value.
//
// HaulsWith filters with has(species, {fish:String}), which looks inside the array on the
// server. An Array column cannot be Nullable in ClickHouse, so an empty haul is an empty
// array, never NULL.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the hauls.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	hauls := []store.LandHaulParams{
		{ID: 1, Boat: "Kittiwake", Species: []string{"mackerel", "pollock"}, WeightsKg: []uint32{120, 45}},
		{ID: 2, Boat: "Salty Chip", Species: []string{"crab", "lobster"}, WeightsKg: []uint32{30, 12}},
		{ID: 3, Boat: "Gull Wing", Species: []string{"mackerel"}, WeightsKg: []uint32{80}},
		{ID: 4, Boat: "Driftwood", Species: []string{}, WeightsKg: []uint32{}},
	}
	for _, haul := range hauls {
		if err := queries.LandHaul(ctx, haul); err != nil {
			return fmt.Errorf("landing haul %d: %w", haul.ID, err)
		}
	}

	first, err := queries.GetHaul(ctx, 1)
	if err != nil {
		return fmt.Errorf("getting haul 1: %w", err)
	}
	fmt.Fprintf(output, "haul 1: species %T %v, weights_kg %T %v\n", first.Species, first.Species, first.WeightsKg, first.WeightsKg)

	mackerel, err := queries.HaulsWith(ctx, "mackerel")
	if err != nil {
		return fmt.Errorf("finding mackerel: %w", err)
	}
	fmt.Fprintln(output, "hauls with mackerel:")
	for _, haul := range mackerel {
		fmt.Fprintf(output, "  #%d %-10s %v %v kg\n", haul.ID, haul.Boat, haul.Species, haul.WeightsKg)
	}

	empty, err := queries.GetHaul(ctx, 4)
	if err != nil {
		return fmt.Errorf("getting haul 4: %w", err)
	}
	fmt.Fprintf(output, "haul 4 landed %d species, an empty array: %v\n", len(empty.Species), empty.Species)
	return nil
}
