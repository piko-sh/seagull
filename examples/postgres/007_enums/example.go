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
	"errors"
	"fmt"
	"io"

	"seagull.sh/seagull/examples/postgres/007_enums/internal/store"
)

// run gives the harbour's boats a status from the boat_status enum and lists them twice.
// The enum becomes a named Go type in the generated package: models.go declares
// store.BoatStatus, named after the enum type, with a constant for each label, such as
// store.BoatStatusMoored, and a Valid method. Every column and parameter of the enum
// takes it, in models.go, in Params structs and in Row structs, as ListBoats shows. Valid
// reports whether a value is one of the labels, and the server still enforces them, so a
// status outside the enum is refused when the statement runs.
//
// For a type of your own, here harbour.Status with a method that describes each label,
// name it with a go_type override. BoatsWithStatus does that for one query with
// sgl.column(status, go_type: ...); a types.overrides entry in sgl.yaml, naming the enum
// type boat_status or the column boats.status, would do it for every query, and for the
// parameters bound to the column too. A query's sgl.column override changes only that
// query's returned column, so AddBoat, SetStatus and BoatsWithStatus's own parameter
// still take the generated store.BoatStatus.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives both listings.
//
// Returns error when a query fails or a status outside the enum is accepted.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	boats := []store.AddBoatParams{
		{Name: "Kittiwake", Status: store.BoatStatusMoored},
		{Name: "Salty Chip", Status: store.BoatStatusAtSea},
		{Name: "Gull Wing", Status: store.BoatStatusDryDock},
		{Name: "Driftwood", Status: store.BoatStatusMoored},
	}
	for _, boat := range boats {
		if err := queries.AddBoat(ctx, boat); err != nil {
			return fmt.Errorf("adding %s: %w", boat.Name, err)
		}
	}

	sunk := store.BoatStatus("sunk")
	if err := queries.AddBoat(ctx, store.AddBoatParams{Name: "Wreck", Status: sunk}); err == nil {
		return errors.New("adding a boat with status sunk: got no error, want the enum to refuse it")
	}
	fmt.Fprintf(output, "status %q: Valid() is %t, and the server refuses it, as it is not a boat_status label\n", sunk, sunk.Valid())

	if _, err := queries.SetStatus(ctx, store.SetStatusParams{Status: store.BoatStatusAtSea, Name: "Kittiwake"}); err != nil {
		return fmt.Errorf("sending Kittiwake to sea: %w", err)
	}

	listed, err := queries.ListBoats(ctx)
	if err != nil {
		return fmt.Errorf("listing boats: %w", err)
	}
	fmt.Fprintln(output, "\nListBoats, where status is the generated store.BoatStatus:")
	for _, boat := range listed {
		fmt.Fprintf(output, "  %-10s  %-8s  %T\n", boat.Name, boat.Status, boat.Status)
	}

	atSea, err := queries.BoatsWithStatus(ctx, store.BoatStatusAtSea)
	if err != nil {
		return fmt.Errorf("listing boats at sea: %w", err)
	}
	fmt.Fprintln(output, "\nBoatsWithStatus, where a go_type override makes status a harbour.Status:")
	for _, boat := range atSea {
		fmt.Fprintf(output, "  %-10s  %-14s  %T\n", boat.Name, boat.Status.Describe(), boat.Status)
	}
	return nil
}
