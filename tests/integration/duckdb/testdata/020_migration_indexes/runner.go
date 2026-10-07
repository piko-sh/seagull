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

	_ "github.com/duckdb/duckdb-go/v2"

	"case_runner/db"
)

func main() {
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	_, err = conn.ExecContext(ctx, `CREATE TABLE products (
		id INTEGER PRIMARY KEY,
		name VARCHAR NOT NULL,
		category VARCHAR NOT NULL,
		price INTEGER NOT NULL,
		sku VARCHAR NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `CREATE INDEX idx_products_category ON products (category)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `CREATE UNIQUE INDEX idx_products_sku ON products (sku)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO products (id, name, category, price, sku) VALUES
		(1, 'Widget', 'hardware', 1999, 'HW-001'),
		(2, 'Gadget', 'hardware', 2999, 'HW-002'),
		(3, 'Notebook', 'stationery', 499, 'ST-001')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	byCategory, err := queries.ListByCategory(ctx, "hardware")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListByCategory:", err)
		os.Exit(1)
	}

	bySku, err := queries.GetBySku(ctx, "ST-001")
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetBySku:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"list_by_category": byCategory,
		"get_by_sku":       bySku,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
