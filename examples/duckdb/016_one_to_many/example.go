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

	"seagull.sh/seagull/examples/duckdb/016_one_to_many/internal/store"
)

// run takes three chip shop orders and lists them with their lines. The join in
// ListOrders returns one row per order line, repeating the order's columns each time.
// group_by: orders.id collapses those rows: each distinct orders.id becomes one
// ListOrdersRow, holding the order's columns in Orders and every matching line in the
// OrderLines slice. Groups keep the order the rows arrive in.
//
// It is a LEFT JOIN, so an order with no lines still appears, with an empty slice, and
// the line fields are pointers. sgl vet checks the directive: group_by must name a column
// the query returns (Q014), the other tables need sgl.embed (Q015), and the command must
// be many (Q016).
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives each order and its lines.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	for _, customer := range []string{"Steven", "Gwen", "Jago"} {
		if err := queries.OpenOrder(ctx, customer); err != nil {
			return fmt.Errorf("opening %s's order: %w", customer, err)
		}
	}
	lines := []store.AddLineParams{
		{OrderID: 1, Dish: "cod and chips", Quantity: 2},
		{OrderID: 1, Dish: "mushy peas", Quantity: 1},
		{OrderID: 1, Dish: "pickled egg", Quantity: 3},
		{OrderID: 2, Dish: "chips", Quantity: 1},
	}
	for _, line := range lines {
		if err := queries.AddLine(ctx, line); err != nil {
			return fmt.Errorf("adding %s to order %d: %w", line.Dish, line.OrderID, err)
		}
	}

	orders, err := queries.ListOrders(ctx)
	if err != nil {
		return fmt.Errorf("listing orders: %w", err)
	}
	for _, order := range orders {
		fmt.Fprintf(output, "order %d for %s\n", order.Orders.ID, order.Orders.Customer)
		if len(order.OrderLines) == 0 {
			fmt.Fprintln(output, "  nothing ordered yet")
		}
		for _, line := range order.OrderLines {
			fmt.Fprintf(output, "  %d x %s\n", *line.Quantity, *line.Dish)
		}
	}
	return nil
}
