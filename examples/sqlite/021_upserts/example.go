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

	"seagull.sh/seagull/examples/sqlite/021_upserts/internal/store"
)

// run books three deliveries into the stock table, two of them for the same item, then
// adds two items only if they are new, and prints the stock that results.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives each upsert's outcome and the final stock.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	// ReceiveDelivery inserts a new item, or adds to the quantity of one already in stock.
	// excluded.quantity is the value the INSERT tried to write, so the parameters stay plain
	// Item and Quantity fields.
	deliveries := []store.ReceiveDeliveryParams{
		{Item: "cod", Quantity: 10},
		{Item: "potatoes", Quantity: 40},
		{Item: "cod", Quantity: 5},
	}
	for _, delivery := range deliveries {
		row, err := queries.ReceiveDelivery(ctx, delivery)
		if err != nil {
			return fmt.Errorf("receiving %s: %w", delivery.Item, err)
		}
		fmt.Fprintf(output, "%-8s delivery %d: +%d, now %d in stock\n",
			row.Item, row.Deliveries, delivery.Quantity, row.Quantity)
	}

	// AddItem uses DO NOTHING, and execrows reports whether the row went in: 1 for a new
	// item, 0 when the item was already there and the insert was skipped.
	for _, item := range []string{"peas", "cod"} {
		added, err := queries.AddItem(ctx, item)
		if err != nil {
			return fmt.Errorf("adding %s: %w", item, err)
		}
		if added == 0 {
			fmt.Fprintf(output, "%s is already stocked, nothing changed\n", item)
			continue
		}
		fmt.Fprintf(output, "added %s with no stock yet\n", item)
	}

	stock, err := queries.ListStock(ctx)
	if err != nil {
		return fmt.Errorf("listing stock: %w", err)
	}
	for _, row := range stock {
		fmt.Fprintf(output, "%-8s %2d\n", row.Item, row.Quantity)
	}
	return nil
}
