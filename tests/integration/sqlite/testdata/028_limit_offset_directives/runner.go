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

	_, err = conn.ExecContext(ctx, `CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT NOT NULL, price REAL NOT NULL)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO products (id, name, price) VALUES (1, 'Alpha', 10.0), (2, 'Bravo', 25.0), (3, 'Charlie', 5.0), (4, 'Delta', 50.0), (5, 'Echo', 15.0)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	page1, err := queries.ListProductsPaginated(ctx, db.ListProductsPaginatedParams{
		PageSize:   int(2),
		PageOffset: int(0),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	page2, err := queries.ListProductsPaginated(ctx, db.ListProductsPaginatedParams{
		PageSize:   int(2),
		PageOffset: int(2),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	filtered, err := queries.ListProductsByMinPrice(ctx, db.ListProductsByMinPriceParams{
		MinimumPrice: 15.0,
		PageSize:     int(2),
		PageOffset:   int(0),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	result := map[string]any{
		"page1":    page1,
		"page2":    page2,
		"filtered": filtered,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
