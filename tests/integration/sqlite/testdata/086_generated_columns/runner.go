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

	_, err = conn.ExecContext(ctx, `CREATE TABLE items (
		id INTEGER PRIMARY KEY,
		price INTEGER NOT NULL,
		quantity INTEGER NOT NULL,
		total INTEGER GENERATED ALWAYS AS (price * quantity) STORED,
		label TEXT GENERATED ALWAYS AS (printf('Item #%d', id)) STORED
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	err = queries.InsertItem(ctx, db.InsertItemParams{
		ID:       int64(1),
		Price:    int64(10),
		Quantity: int64(5),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertItem 1:", err)
		os.Exit(1)
	}

	err = queries.InsertItem(ctx, db.InsertItemParams{
		ID:       int64(2),
		Price:    int64(25),
		Quantity: int64(2),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertItem 2:", err)
		os.Exit(1)
	}

	err = queries.InsertItem(ctx, db.InsertItemParams{
		ID:       int64(3),
		Price:    int64(100),
		Quantity: int64(1),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertItem 3:", err)
		os.Exit(1)
	}

	item, err := queries.GetItem(ctx, int64(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetItem:", err)
		os.Exit(1)
	}

	minTotal := int64(50)
	byMinTotal, err := queries.ListByMinTotal(ctx, &minTotal)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListByMinTotal:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"item":         item,
		"by_min_total": byMinTotal,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
