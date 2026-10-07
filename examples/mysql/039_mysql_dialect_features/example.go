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

	"seagull.sh/seagull/examples/mysql/039_mysql_dialect_features/internal/store"
)

// run keeps a harbour's register with SQL that only MySQL and MariaDB speak, and prints
// what each statement did:
//
//   - kit is a SET column, which holds any number of the values it lists. It maps to a
//     string of the chosen values joined by commas, and MySQL stores them in the order
//     the SET lists them, once each. FIND_IN_SET looks one up;
//   - crossings is INT UNSIGNED, so Crossings is a uint32, and berth is TINYINT UNSIGNED,
//     so Berth is a uint8;
//   - RegisterBoat is INSERT IGNORE: a registration already on the books counts 0 rows
//     instead of failing. IGNORE turns other errors into warnings too, so a kit item the
//     SET does not list is dropped without a word;
//   - AssignBerth is REPLACE INTO, which deletes every row that clashes with the new one
//     on any unique key, then inserts it. execrows counts the deleted rows as well;
//   - Harbour gathers each boat's crew into one string with GROUP_CONCAT, which is NULL
//     for a boat with no crew. The crew's `rank` column is quoted with backticks, as RANK
//     is a reserved word since MySQL 8.0;
//   - boats documents itself with MySQL's COMMENT, after a column's definition and as the
//     table's COMMENT = option. sgl.yaml sets go.doc_comments, so they become the doc
//     comments of the Boats model and its Kit and Crossings fields: open internal/store
//     to see them.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives what each statement did.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	boats := []store.RegisterBoatParams{
		{Registration: "PZ1001", Name: "Kittiwake", Kit: "radar,radio,radar"},
		{Registration: "PZ2002", Name: "Salty Chip", Kit: "liferaft"},
		{Registration: "PZ1001", Name: "Kittiwake II", Kit: "winch"},
		{Registration: "PZ3003", Name: "Gull Wing", Kit: "radio,sonar"},
	}
	for _, boat := range boats {
		registered, err := queries.RegisterBoat(ctx, boat)
		if err != nil {
			return fmt.Errorf("registering %s: %w", boat.Name, err)
		}
		if registered == 0 {
			fmt.Fprintf(output, "INSERT IGNORE  %s is already registered, so %s was ignored\n", boat.Registration, boat.Name)
			continue
		}
		fmt.Fprintf(output, "INSERT IGNORE  registered %s as %s with kit %q\n", boat.Name, boat.Registration, boat.Kit)
	}

	for _, registration := range []string{"PZ1001", "PZ1001", "PZ3003"} {
		if _, err := queries.LogCrossing(ctx, registration); err != nil {
			return fmt.Errorf("logging a crossing for %s: %w", registration, err)
		}
	}
	crew := []store.SignOnParams{
		{Registration: "PZ1001", Name: "Morwenna", Rank: "skipper"},
		{Registration: "PZ1001", Name: "Jago", Rank: "deckhand"},
		{Registration: "PZ3003", Name: "Kerensa", Rank: "skipper"},
	}
	for _, member := range crew {
		if err := queries.SignOn(ctx, member); err != nil {
			return fmt.Errorf("signing on %s: %w", member.Name, err)
		}
	}

	// Moving Salty Chip to berth 1 clashes twice: berth 1 is the Kittiwake's, and
	// registration is UNIQUE, so Salty Chip's own berth 2 goes too.
	berths := []store.AssignBerthParams{
		{Berth: 1, Registration: "PZ1001"},
		{Berth: 2, Registration: "PZ2002"},
		{Berth: 1, Registration: "PZ2002"},
	}
	for _, berth := range berths {
		affected, err := queries.AssignBerth(ctx, berth)
		if err != nil {
			return fmt.Errorf("assigning berth %d: %w", berth.Berth, err)
		}
		fmt.Fprintf(output, "REPLACE INTO   berth %d for %s, rows affected: %d\n", berth.Berth, berth.Registration, affected)
	}
	assigned, err := queries.ListBerths(ctx)
	if err != nil {
		return fmt.Errorf("listing berths: %w", err)
	}
	for _, berth := range assigned {
		fmt.Fprintf(output, "  berth %d (%T): %s\n", berth.Berth, berth.Berth, berth.Name)
	}

	withRadio, err := queries.BoatsWithKit(ctx, "radio")
	if err != nil {
		return fmt.Errorf("finding boats with a radio: %w", err)
	}
	for _, boat := range withRadio {
		fmt.Fprintf(output, "FIND_IN_SET    %s has a radio among %q\n", boat.Name, boat.Kit)
	}

	harbour, err := queries.Harbour(ctx)
	if err != nil {
		return fmt.Errorf("listing the harbour: %w", err)
	}
	for _, boat := range harbour {
		crewList := "no crew"
		if boat.Crew != nil {
			crewList = *boat.Crew
		}
		fmt.Fprintf(output, "GROUP_CONCAT   %s %-10s crossings %d (%T), crew: %s\n", boat.Registration, boat.Name, boat.Crossings, boat.Crossings, crewList)
	}
	return nil
}
