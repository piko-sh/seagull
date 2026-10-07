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

	"seagull.sh/seagull/examples/sqlite/018_optional_one/internal/store"
)

// run rings gulls and looks them up. Both queries are one queries with optional: true, so
// they return (row, ok, error): a missing row gives ok false and a nil error instead of
// sql.ErrNoRows, and error is left for real failures.
//
// RingGull inserts with ON CONFLICT (ring) DO NOTHING RETURNING. A ring that is already
// taken skips the insert, so RETURNING has no row and ok is false. Without optional: true
// sgl vet warns about this (Q046), as the call would fail with sql.ErrNoRows. optional
// applies only to a one query; on any other command sgl vet refuses it (Q047).
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives each ringing and lookup.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	attempts := []store.RingGullParams{
		{Ring: "GB1001", Species: "herring gull"},
		{Ring: "GB1002", Species: "kittiwake"},
		{Ring: "GB1001", Species: "black-headed gull"},
	}
	for _, attempt := range attempts {
		ringed, ok, err := queries.RingGull(ctx, attempt)
		if err != nil {
			return fmt.Errorf("ringing %s: %w", attempt.Ring, err)
		}
		if !ok {
			fmt.Fprintf(output, "ring %s is already taken, so the %s was not ringed\n", attempt.Ring, attempt.Species)
			continue
		}
		fmt.Fprintf(output, "ringed a %s as %s, gull %d\n", attempt.Species, ringed.Ring, ringed.ID)
	}

	for _, ring := range []string{"GB1002", "GB9999"} {
		gull, ok, err := queries.FindGull(ctx, ring)
		if err != nil {
			return fmt.Errorf("finding %s: %w", ring, err)
		}
		if !ok {
			fmt.Fprintf(output, "no gull wears ring %s\n", ring)
			continue
		}
		fmt.Fprintf(output, "ring %s is on gull %d, a %s\n", ring, gull.ID, gull.Species)
	}
	return nil
}
