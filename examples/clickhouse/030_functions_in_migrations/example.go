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

	"seagull.sh/seagull/examples/clickhouse/030_functions_in_migrations/internal/store"
)

// run prices the chip shop's menu with a service charge worked out by a SQL function that
// a migration defines. In ClickHouse a SQL function is a lambda, CREATE FUNCTION
// with_service AS pence -> intDiv(pence * 110, 100), and it belongs to the server rather
// than to one database, so the migration says CREATE OR REPLACE and the down migration
// drops it. A lambda can only compute from its arguments; it cannot read or write a
// table, so a query that calls one stays read-only and there is no volatility to declare.
//
// Seagull reads the lambda from the migration and types each call from its body, with the
// argument in place of pence. price_pence is a UInt32; ClickHouse multiplies it by 110
// into a UInt64, the next wider type, and intDiv keeps that, so TotalPence is a uint64,
// as the server returns. The call reads no table, so the query stays a read, and a store
// built with NewWithReplica sends it to the reader.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the priced menu.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	dishes := []store.AddDishParams{
		{ID: 1, Dish: "cod and chips", PricePence: 850},
		{ID: 2, Dish: "haddock and chips", PricePence: 920},
		{ID: 3, Dish: "mushy peas", PricePence: 150},
	}
	for _, dish := range dishes {
		if err := queries.AddDish(ctx, dish); err != nil {
			return fmt.Errorf("adding %s: %w", dish.Dish, err)
		}
	}

	menu, err := queries.MenuWithService(ctx)
	if err != nil {
		return fmt.Errorf("pricing the menu: %w", err)
	}
	for _, dish := range menu {
		fmt.Fprintf(output, "%-18s %4dp, with service %dp\n", dish.Dish, dish.PricePence, dish.TotalPence)
	}
	return nil
}
