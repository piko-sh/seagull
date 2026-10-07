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

	_ "github.com/go-sql-driver/mysql"

	"case_runner/db"
)

func main() {
	connectionString := os.Getenv("DATABASE_URL")

	conn, err := sql.Open("mysql", connectionString)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	queries := db.New(conn)

	err = queries.InsertLineItem(ctx, db.InsertLineItemParams{
		Product:     "Widget",
		Quantity:    int32(5),
		UnitPrice:   int32(200),
		DiscountPct: int32(0),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertLineItem 1:", err)
		os.Exit(1)
	}

	err = queries.InsertLineItem(ctx, db.InsertLineItemParams{
		Product:     "Gadget",
		Quantity:    int32(3),
		UnitPrice:   int32(150),
		DiscountPct: int32(10),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertLineItem 2:", err)
		os.Exit(1)
	}

	err = queries.InsertLineItem(ctx, db.InsertLineItemParams{
		Product:     "Gizmo",
		Quantity:    int32(10),
		UnitPrice:   int32(50),
		DiscountPct: int32(25),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertLineItem 3:", err)
		os.Exit(1)
	}

	item, err := queries.GetLineItem(ctx, int32(2))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetLineItem:", err)
		os.Exit(1)
	}

	allItems, err := queries.ListLineItems(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListLineItems:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"item":      item,
		"all_items": allItems,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
