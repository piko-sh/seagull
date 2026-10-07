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

	"seagull.sh/seagull/examples/mysql/002_commands/internal/store"
)

// run moors three boats, looks one up, scraps one, puts the rest to sea and lists the
// harbour. Each query in db/queries/boats.sql names a command, and the command decides
// what its generated method returns:
//
//   - execresult returns sql.Result, which carries the new row's AUTO_INCREMENT id;
//   - one returns a single row, or sql.ErrNoRows when nothing matches;
//   - many returns a slice of rows;
//   - execrows returns how many rows the statement changed;
//   - exec returns only an error.
//
// The command has to fit the statement: exec on a SELECT, or one on an INSERT, makes sgl
// vet report Q009. MySQL has no RETURNING to turn a write into a one query, and sgl vet
// refuses one with Q052, so execresult is how an INSERT hands back its id.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives what each call returned.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	arrivals := []store.MoorBoatParams{
		{Name: "Kittiwake", Berth: 1},
		{Name: "Salty Chip", Berth: 2},
		{Name: "Gull Wing", Berth: 5},
	}
	for _, arrival := range arrivals {
		result, err := queries.MoorBoat(ctx, arrival)
		if err != nil {
			return fmt.Errorf("mooring %s: %w", arrival.Name, err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("reading %s's ID: %w", arrival.Name, err)
		}
		fmt.Fprintf(output, "execresult  moored %s as boat %d\n", arrival.Name, id)
	}

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
		return errors.New("getting scrapped boat 3: got a boat, want sql.ErrNoRows")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("getting scrapped boat 3: got %w, want sql.ErrNoRows", err)
	}
	fmt.Fprintln(output, "one         boat 3 is gone, so GetBoat returns sql.ErrNoRows")

	// PutToSea only changes boats still in harbour, so a second run changes none. MySQL
	// counts the rows a statement changed, not the rows its WHERE matched, unless the DSN
	// sets clientFoundRows=true.
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
