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

	"seagull.sh/seagull/examples/mariadb/020_returning/internal/store"
)

// run places three orders, cancels one, serves the rest and then tries to cancel an order
// that has already been served. The INSERT and the DELETE use RETURNING, so the rows they
// touched come back from the same statement.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives what each write returned.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	// PlaceOrder is an INSERT declared as one: the id MariaDB assigned, the default status
	// and a computed price all come back in a PlaceOrderRow.
	orders := []store.PlaceOrderParams{
		{Item: "chips", Quantity: 2},
		{Item: "battered cod", Quantity: 1},
		{Item: "mushy peas", Quantity: 1},
	}
	for _, order := range orders {
		placed, err := queries.PlaceOrder(ctx, order)
		if err != nil {
			return fmt.Errorf("placing an order for %s: %w", order.Item, err)
		}
		fmt.Fprintf(output, "placed    #%d %-12s %s, %dp\n", placed.ID, order.Item, placed.Status, placed.PricePence)
	}

	// CancelOrder is a DELETE declared as one, so it returns the row it removed.
	cancelled, err := queries.CancelOrder(ctx, 2)
	if err != nil {
		return fmt.Errorf("cancelling order 2: %w", err)
	}
	fmt.Fprintf(output, "cancelled #2 %-12s x%d\n", cancelled.Item, cancelled.Quantity)

	// MariaDB has RETURNING on INSERT, REPLACE and DELETE, but not on UPDATE, so
	// ServeWaitingOrders is execrows and ServedOrders reads the changed rows back.
	if _, err := queries.ServeWaitingOrders(ctx); err != nil {
		return fmt.Errorf("serving orders: %w", err)
	}
	served, err := queries.ServedOrders(ctx)
	if err != nil {
		return fmt.Errorf("listing served orders: %w", err)
	}
	for _, order := range served {
		fmt.Fprintf(output, "served    #%d %-12s x%d\n", order.ID, order.Item, order.Quantity)
	}

	// A one query that matches no row returns sql.ErrNoRows, writes included: order 1 has
	// been served, so the DELETE's WHERE clause no longer matches it.
	_, err = queries.CancelOrder(ctx, 1)
	if errors.Is(err, sql.ErrNoRows) {
		fmt.Fprintln(output, "cannot cancel #1: it is no longer waiting")
		return nil
	}
	if err != nil {
		return fmt.Errorf("cancelling order 1: %w", err)
	}
	return errors.New("cancelled an order that had already been served")
}
