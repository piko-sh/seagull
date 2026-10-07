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

	"seagull.sh/seagull/examples/postgres/009_overrides/internal/menu"
	"seagull.sh/seagull/examples/postgres/009_overrides/internal/store"
)

// run fills a chip shop menu and reads it back. Five overrides replace the Go types
// Seagull would infer:
//
//   - sgl.yaml's types.decimal slot maps every NUMERIC to string, the server's exact
//     text, in place of apd.Decimal, so the module needs no decimal package; a slot
//     changes models.go and parameters as well as returned columns;
//   - sgl.yaml's types.overrides gives menu_items.status the type menu.Status;
//   - sgl.column(menu_items.price, go_type: ...) in the migration gives price the type
//     menu.Price in every query that returns the column as it is;
//   - sgl.column(kcal, type: bigint, nullable: false) in ListMenu types nutrition ->>
//     'kcal', which is nullable text, as the number every document holds; a cast,
//     (nutrition ->> 'kcal')::bigint, would type it too, and make the server check it;
//   - in CheapestPerSection, MIN(price) is a new value, so the migration's override no
//     longer applies and the aggregate could be NULL; sgl.column(cheapest, go_type: ...,
//     nullable: false) makes it menu.Price again, as every group has a row.
//
// Column overrides also reach models.go and the parameters bound to those columns, so
// AddItemParams takes a menu.Status and a menu.Price.
//
// sgl vet catches mistakes in overrides: Q036 for a query override naming a column the
// query does not return, Q037 for an override naming a table column that does not exist,
// Q038 for type: and go_type: together, and Q049 for a go_type the generator cannot use.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the menu and the cheapest item per section.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	items := []store.AddItemParams{
		{Name: "Cod and chips", Section: "fish", Status: menu.Available, Price: "9.50", Nutrition: store.JSON(`{"kcal": 1050}`)},
		{Name: "Haddock and chips", Section: "fish", Status: menu.SoldOut, Price: "10.50", Nutrition: store.JSON(`{"kcal": 980}`)},
		{Name: "Chips", Section: "sides", Status: menu.Available, Price: "3.50", Nutrition: store.JSON(`{"kcal": 560}`)},
		{Name: "Mushy peas", Section: "sides", Status: menu.Available, Price: "1.75", Nutrition: store.JSON(`{"kcal": 120}`)},
	}
	for _, item := range items {
		if err := queries.AddItem(ctx, item); err != nil {
			return fmt.Errorf("adding %s: %w", item.Name, err)
		}
	}

	listed, err := queries.ListMenu(ctx)
	if err != nil {
		return fmt.Errorf("listing the menu: %w", err)
	}
	for _, item := range listed {
		if item.Status == menu.SoldOut {
			fmt.Fprintf(output, "%-18s sold out\n", item.Name)
			continue
		}
		fmt.Fprintf(output, "%-18s %6s  %4d kcal\n", item.Name, item.Price, item.Kcal)
	}

	cheapest, err := queries.CheapestPerSection(ctx)
	if err != nil {
		return fmt.Errorf("finding the cheapest items: %w", err)
	}
	for _, section := range cheapest {
		fmt.Fprintf(output, "cheapest %-6s from %s\n", section.Section, section.Cheapest)
	}
	return nil
}
