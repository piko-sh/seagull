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
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"seagull.sh/seagull/examples/postgres/002_commands_pgx/internal/store"
)

// run moors three boats, moves one, looks one up, scraps one, puts the rest to sea and
// lists the harbour, with code sgl generated for pgx. Each query in db/queries/boats.sql
// names a command, and the command decides what its generated method returns:
//
//   - one returns a single row, or pgx.ErrNoRows when nothing matches; on an INSERT with
//     RETURNING it hands back the new row's ID;
//   - execresult returns the pgconn.CommandTag the server sent back, such as UPDATE 1,
//     which reports the kind of statement and RowsAffected without an error to check;
//   - many returns a slice of rows;
//   - execrows returns how many rows the statement changed;
//   - exec returns only an error.
//
// The command has to fit the statement: exec on a SELECT, or one on an INSERT without
// RETURNING, makes sgl vet report Q009.
//
// Takes pool (*pgxpool.Pool) which is a pool on a migrated database.
// Takes output (io.Writer) which receives what each call returned.
//
// Returns error when a query fails.
func run(ctx context.Context, pool *pgxpool.Pool, output io.Writer) error {
	queries := store.New(pool)

	arrivals := []store.MoorBoatParams{
		{Name: "Kittiwake", Berth: 1},
		{Name: "Salty Chip", Berth: 2},
		{Name: "Gull Wing", Berth: 5},
	}
	for _, arrival := range arrivals {
		moored, err := queries.MoorBoat(ctx, arrival)
		if err != nil {
			return fmt.Errorf("mooring %s: %w", arrival.Name, err)
		}
		fmt.Fprintf(output, "one         moored %s as boat %d\n", arrival.Name, moored.ID)
	}

	tag, err := queries.MoveBerth(ctx, store.MoveBerthParams{Berth: 4, ID: 2})
	if err != nil {
		return fmt.Errorf("moving boat 2: %w", err)
	}
	fmt.Fprintf(output, "execresult  moved boat 2 to berth 4, command tag %q\n", tag.String())
	fmt.Fprintf(output, "execresult  Update() %t, RowsAffected() %d\n", tag.Update(), tag.RowsAffected())

	boat, err := queries.GetBoat(ctx, 2)
	if err != nil {
		return fmt.Errorf("getting boat 2: %w", err)
	}
	fmt.Fprintf(output, "one         boat 2 is %s, at berth %d\n", boat.Name, boat.Berth)

	if err := queries.ScrapBoat(ctx, 3); err != nil {
		return fmt.Errorf("scrapping boat 3: %w", err)
	}
	fmt.Fprintln(output, "exec        scrapped boat 3")

	_, err = queries.GetBoat(ctx, 3)
	if err == nil {
		return errors.New("getting scrapped boat 3: got a boat, want pgx.ErrNoRows")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("getting scrapped boat 3: got %w, want pgx.ErrNoRows", err)
	}
	fmt.Fprintln(output, "one         boat 3 is gone, so GetBoat returns pgx.ErrNoRows")

	// PutToSea only changes boats still in harbour, so a second run changes none.
	for range 2 {
		sailed, err := queries.PutToSea(ctx)
		if err != nil {
			return fmt.Errorf("putting boats to sea: %w", err)
		}
		fmt.Fprintf(output, "execrows    %d boats put to sea\n", sailed)
	}

	boats, err := queries.ListBoats(ctx)
	if err != nil {
		return fmt.Errorf("listing boats: %w", err)
	}
	for _, boat := range boats {
		fmt.Fprintf(output, "many        berth %d  %-10s  at sea: %t\n", boat.Berth, boat.Name, boat.AtSea)
	}
	return nil
}
