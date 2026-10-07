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
	conn, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	_, err = conn.ExecContext(ctx, `CREATE TABLE products (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		price INTEGER NOT NULL,
		category TEXT NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO products (id, name, price, category) VALUES
		(1, 'Apple', 100, 'fruit'),
		(2, 'Banana', 50, 'fruit'),
		(3, 'Carrot', 75, 'veg'),
		(4, 'Date', 200, 'fruit'),
		(5, 'Eggplant', 150, 'veg'),
		(6, 'Fig', 120, 'fruit'),
		(7, 'Grape', 80, 'fruit'),
		(8, 'Honeydew', 300, 'fruit')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	noFilterPage1, err := queries.SearchProducts(ctx, db.SearchProductsParams{
		PageSize:   3,
		PageOffset: 0,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchProducts (no filter page 1):", err)
		os.Exit(1)
	}
	fruitPage1, err := queries.SearchProducts(ctx, db.SearchProductsParams{
		Category:   new("fruit"),
		PageSize:   3,
		PageOffset: 0,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchProducts (fruit page 1):", err)
		os.Exit(1)
	}
	fruitPage2, err := queries.SearchProducts(ctx, db.SearchProductsParams{
		Category:   new("fruit"),
		PageSize:   3,
		PageOffset: 3,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchProducts (fruit page 2):", err)
		os.Exit(1)
	}
	vegAll, err := queries.SearchProducts(ctx, db.SearchProductsParams{
		Category:   new("veg"),
		PageSize:   20,
		PageOffset: 0,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchProducts (veg all):", err)
		os.Exit(1)
	}

	result := map[string]any{
		"no_filter_page1": noFilterPage1,
		"fruit_page1":     fruitPage1,
		"fruit_page2":     fruitPage2,
		"veg_all":         vegAll,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
