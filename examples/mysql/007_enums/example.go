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

	"seagull.sh/seagull/examples/mysql/007_enums/internal/menu"
	"seagull.sh/seagull/examples/mysql/007_enums/internal/store"
)

// run fills a chip shop menu whose section and status columns are MySQL ENUMs, marks an
// item sold out and reads the menu back. Each ENUM becomes a named Go type in the
// generated package unless an override names a type of your own:
//
//   - section has no override, so models.go declares store.MenuItemsSection, named after
//     the table and column that hold the ENUM, with a constant for each value, such as
//     store.MenuItemsSectionFish, and a Valid method; ListMenuRow.Section,
//     AddItemParams.Section and ItemsInSection's parameter are all a
//     store.MenuItemsSection;
//   - sgl.yaml's types.overrides names the column menu_items.status, so every query that
//     returns it as it is gives menu.Status, as in ListMenuRow.Status, and so does every
//     parameter compared with or assigned to it, as in SetStatusParams.Status; models.go
//     uses it too, and the generated package declares no type for that ENUM;
//   - sgl.column(section, go_type: ...) in ItemsInSection gives that one query's returned
//     Section the type menu.Section; the parameter it compares with stays a
//     store.MenuItemsSection.
//
// An ENUM sorts in the order its values are declared, not alphabetically, so ORDER BY
// section lists fish, then sides, then puddings. Valid reports whether a value is one the
// ENUM declares, so the shop can check a section before it binds it. In strict mode, the
// default since MySQL 5.7, a value the ENUM does not list is refused as it is stored as
// well; MySQL keeps an ENUM as the index of its value, and its error says the value was
// truncated.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the menu, the fish section and the refusal.
//
// Returns error when a query fails other than as the example expects.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	items := []store.AddItemParams{
		{Name: "cod", Section: store.MenuItemsSectionFish},
		{Name: "haddock", Section: store.MenuItemsSectionFish},
		{Name: "chips", Section: store.MenuItemsSectionSides},
		{Name: "mushy peas", Section: store.MenuItemsSectionSides},
		{Name: "sticky toffee pudding", Section: store.MenuItemsSectionPuddings},
	}
	for _, item := range items {
		if err := queries.AddItem(ctx, item); err != nil {
			return fmt.Errorf("adding %s: %w", item.Name, err)
		}
	}
	if _, err := queries.SetStatus(ctx, store.SetStatusParams{Status: menu.SoldOut, Name: "haddock"}); err != nil {
		return fmt.Errorf("selling out of haddock: %w", err)
	}

	listed, err := queries.ListMenu(ctx)
	if err != nil {
		return fmt.Errorf("listing the menu: %w", err)
	}
	var menuRow store.ListMenuRow
	fmt.Fprintf(output, "ListMenu: section %T, status %T\n", menuRow.Section, menuRow.Status)
	for _, item := range listed {
		fmt.Fprintf(output, "  %-9s %s%s\n", item.Section, item.Name, soldOut(item.Status))
	}

	fish, err := queries.ItemsInSection(ctx, store.MenuItemsSectionFish)
	if err != nil {
		return fmt.Errorf("listing the fish: %w", err)
	}
	var sectionRow store.ItemsInSectionRow
	fmt.Fprintf(output, "ItemsInSection: section %T, status %T\n", sectionRow.Section, sectionRow.Status)
	for _, item := range fish {
		fmt.Fprintf(output, "  %-9s %s%s\n", item.Section, item.Name, soldOut(item.Status))
	}

	drinks := store.MenuItemsSection("drinks")
	fmt.Fprintf(output, "section drinks is valid: %t\n", drinks.Valid())
	err = queries.AddItem(ctx, store.AddItemParams{Name: "lemonade", Section: drinks})
	if err == nil {
		return errors.New("added an item to a section the enum does not list")
	}
	fmt.Fprintf(output, "refused section drinks: %v\n", err)
	return nil
}

// soldOut returns a note for an item that has sold out, comparing against the typed
// constant rather than a string.
//
// Takes status (menu.Status) which is the item's status.
//
// Returns string which is the note, or empty when the item is available.
func soldOut(status menu.Status) string {
	if status == menu.SoldOut {
		return " (sold out)"
	}
	return ""
}
