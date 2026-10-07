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
	"encoding/json"
	"fmt"
	"os"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()

	if _, err := conn.ExecContext(ctx, `CREATE TABLE products (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		price INTEGER NOT NULL,
		category TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO products (id, name, price, category) VALUES
		(1, 'Apple', 100, 'fruit'),
		(2, 'Banana', 50, 'fruit'),
		(3, 'Carrot', 75, 'veg'),
		(4, 'Date', 200, 'fruit'),
		(5, 'Eggplant', 150, 'veg'),
		(6, 'Fig', 120, 'fruit')`); err != nil {
		return err
	}

	queries := db.New(conn)

	cheapestPage, err := queries.ListProductsPage(ctx, db.ListProductsPageParams{
		MinPrice:         60,
		PageSize:         2,
		PageOffset:       1,
		OrderBy:          db.ListProductsPageOrderByPrice,
		OrderByDirection: db.OrderAsc,
	})
	if err != nil {
		return fmt.Errorf("ListProductsPage (price ASC): %w", err)
	}

	namesDescending, err := queries.ListProductsPage(ctx, db.ListProductsPageParams{
		OrderBy:          db.ListProductsPageOrderByName,
		OrderByDirection: db.OrderDesc,
	})
	if err != nil {
		return fmt.Errorf("ListProductsPage (name DESC): %w", err)
	}

	unordered, err := queries.ListProductsPage(ctx, db.ListProductsPageParams{PageSize: 2})
	if err != nil {
		return fmt.Errorf("ListProductsPage (no order): %w", err)
	}

	vegByPrice, err := queries.ListProductsInCategories(ctx, db.ListProductsInCategoriesParams{
		Categories:       []string{"veg", "fruit"},
		PageSize:         3,
		OrderBy:          db.ListProductsInCategoriesOrderByPrice,
		OrderByDirection: db.OrderDesc,
	})
	if err != nil {
		return fmt.Errorf("ListProductsInCategories: %w", err)
	}

	cheapest, err := queries.FirstProduct(ctx, db.FirstProductParams{
		OrderBy:          db.FirstProductOrderByPrice,
		OrderByDirection: db.OrderAsc,
	})
	if err != nil {
		return fmt.Errorf("FirstProduct: %w", err)
	}

	var streamed []db.StreamProductsRow
	for row, err := range queries.StreamProducts(ctx, db.StreamProductsParams{
		PageSize:         2,
		OrderBy:          db.StreamProductsOrderByPrice,
		OrderByDirection: db.OrderDesc,
	}) {
		if err != nil {
			return fmt.Errorf("StreamProducts: %w", err)
		}
		streamed = append(streamed, row)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"cheapest_page_from_60": cheapestPage,
		"names_descending":      namesDescending,
		"unordered_first_two":   unordered,
		"categories_by_price":   vegByPrice,
		"cheapest":              cheapest,
		"streamed_dearest_two":  streamed,
	})
}
