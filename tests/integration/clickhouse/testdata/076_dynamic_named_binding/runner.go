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

	_ "github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open("clickhouse", os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	return exercise(ctx, db.New(conn))
}

func exercise(ctx context.Context, queries *db.Queries) error {
	items := []db.InsertItemParams{
		{ID: 1, Name: "Apple", Category: "fruit", Price: 100},
		{ID: 2, Name: "Banana", Category: "fruit", Price: 50},
		{ID: 3, Name: "Carrot", Category: "veg", Price: 75},
		{ID: 4, Name: "Date", Category: "fruit", Price: 200},
		{ID: 5, Name: "Eggplant", Category: "veg", Price: 150},
	}
	for _, item := range items {
		if err := queries.InsertItem(ctx, item); err != nil {
			return fmt.Errorf("insert %s: %w", item.Name, err)
		}
	}

	allItems, err := queries.SearchItems(ctx, db.SearchItemsParams{})
	if err != nil {
		return fmt.Errorf("optional search without category: %w", err)
	}

	fruit := "fruit"
	fruitFrom60, err := queries.SearchItems(ctx, db.SearchItemsParams{Category: &fruit, MinPrice: 60})
	if err != nil {
		return fmt.Errorf("optional search with category: %w", err)
	}

	sortedPage, err := queries.ListItemsSorted(ctx, db.ListItemsSortedParams{
		MinPrice:         60,
		PageSize:         2,
		PageOffset:       1,
		OrderBy:          db.ListItemsSortedOrderByPrice,
		OrderByDirection: db.OrderDesc,
	})
	if err != nil {
		return fmt.Errorf("sortable page: %w", err)
	}

	fruitCount, err := queries.ItemsInCategory("fruit").Count(ctx)
	if err != nil {
		return fmt.Errorf("runtime count: %w", err)
	}
	fruitCountFrom100, err := queries.ItemsInCategory("fruit").Where("price", ">=", int32(100)).Count(ctx)
	if err != nil {
		return fmt.Errorf("runtime count with where: %w", err)
	}
	fruitRowsFrom100, err := queries.ItemsInCategory("fruit").Where("price", ">=", int32(100)).OrderBy("price", "DESC").All(ctx)
	if err != nil {
		return fmt.Errorf("runtime all with where: %w", err)
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"all_items":            allItems,
		"fruit_from_60":        fruitFrom60,
		"sorted_page":          sortedPage,
		"fruit_count":          fruitCount,
		"fruit_count_from_100": fruitCountFrom100,
		"fruit_rows_from_100":  fruitRowsFrom100,
	})
}
