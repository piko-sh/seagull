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

	_ "github.com/jackc/pgx/v5/stdlib"

	"case_runner/db"
)

func main() {
	connectionString := os.Getenv("DATABASE_URL")
	schemaName := os.Getenv("DATABASE_SCHEMA")

	conn, err := sql.Open("pgx", connectionString)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	_, err = conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", schemaName))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	emptyBatch := []db.InsertItemsBatchParams{}
	err = queries.InsertItemsBatch(ctx, emptyBatch)
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertItemsBatch (empty):", err)
		os.Exit(1)
	}

	emptyCount, err := queries.CountItems(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "CountItems (empty):", err)
		os.Exit(1)
	}
	singleBatch := []db.InsertItemsBatchParams{
		{ID: int32(1), Name: "Solo", Category: "misc", Price: int32(999), Description: nil},
	}
	err = queries.InsertItemsBatch(ctx, singleBatch)
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertItemsBatch (single):", err)
		os.Exit(1)
	}

	singleItem, err := queries.GetItem(ctx, int32(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetItem (single):", err)
		os.Exit(1)
	}
	_, err = conn.ExecContext(ctx, "DELETE FROM items")
	if err != nil {
		fmt.Fprintln(os.Stderr, "DELETE (before small):", err)
		os.Exit(1)
	}
	smallBatch := []db.InsertItemsBatchParams{
		{ID: int32(1), Name: "Apple", Category: "fruit", Price: int32(100), Description: new("A crisp fruit")},
		{ID: int32(2), Name: "Banana", Category: "fruit", Price: int32(50), Description: nil},
		{ID: int32(3), Name: "Carrot", Category: "veg", Price: int32(75), Description: new("An orange root")},
	}
	err = queries.InsertItemsBatch(ctx, smallBatch)
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertItemsBatch (small):", err)
		os.Exit(1)
	}

	smallItems, err := queries.ListItems(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListItems (small):", err)
		os.Exit(1)
	}
	_, err = conn.ExecContext(ctx, "DELETE FROM items")
	if err != nil {
		fmt.Fprintln(os.Stderr, "DELETE (before boundary):", err)
		os.Exit(1)
	}
	boundarySize := 199
	boundaryBatch := make([]db.InsertItemsBatchParams, boundarySize)
	for index := range boundaryBatch {
		boundaryBatch[index] = db.InsertItemsBatchParams{
			ID:          int32(index + 1),
			Name:        fmt.Sprintf("item_%d", index+1),
			Category:    "bulk",
			Price:       int32(index * 10),
			Description: new(fmt.Sprintf("desc_%d", index+1)),
		}
	}
	err = queries.InsertItemsBatch(ctx, boundaryBatch)
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertItemsBatch (boundary):", err)
		os.Exit(1)
	}

	boundaryCount, err := queries.CountItems(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "CountItems (boundary):", err)
		os.Exit(1)
	}
	_, err = conn.ExecContext(ctx, "DELETE FROM items")
	if err != nil {
		fmt.Fprintln(os.Stderr, "DELETE (before boundary+1):", err)
		os.Exit(1)
	}
	overBoundarySize := 200
	overBoundaryBatch := make([]db.InsertItemsBatchParams, overBoundarySize)
	for index := range overBoundaryBatch {
		overBoundaryBatch[index] = db.InsertItemsBatchParams{
			ID:          int32(index + 1),
			Name:        fmt.Sprintf("item_%d", index+1),
			Category:    "bulk",
			Price:       int32(index * 10),
			Description: new(fmt.Sprintf("desc_%d", index+1)),
		}
	}
	err = queries.InsertItemsBatch(ctx, overBoundaryBatch)
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertItemsBatch (boundary+1):", err)
		os.Exit(1)
	}

	overBoundaryCount, err := queries.CountItems(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "CountItems (boundary+1):", err)
		os.Exit(1)
	}
	overBoundaryLastItem, err := queries.GetItem(ctx, int32(200))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetItem (boundary+1 last):", err)
		os.Exit(1)
	}
	_, err = conn.ExecContext(ctx, "DELETE FROM items")
	if err != nil {
		fmt.Fprintln(os.Stderr, "DELETE (before large):", err)
		os.Exit(1)
	}
	largeSize := 300
	largeBatch := make([]db.InsertItemsBatchParams, largeSize)
	for index := range largeBatch {
		largeBatch[index] = db.InsertItemsBatchParams{
			ID:          int32(index + 1),
			Name:        fmt.Sprintf("item_%d", index+1),
			Category:    "bulk",
			Price:       int32(index * 10),
			Description: new(fmt.Sprintf("desc_%d", index+1)),
		}
	}
	err = queries.InsertItemsBatch(ctx, largeBatch)
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertItemsBatch (large):", err)
		os.Exit(1)
	}

	largeCount, err := queries.CountItems(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "CountItems (large):", err)
		os.Exit(1)
	}
	largeFirstItem, err := queries.GetItem(ctx, int32(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetItem (large first):", err)
		os.Exit(1)
	}

	largeLastItem, err := queries.GetItem(ctx, int32(300))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetItem (large last):", err)
		os.Exit(1)
	}

	result := map[string]any{
		"01_empty_count":             emptyCount,
		"02_single_item":             singleItem,
		"03_small_items":             smallItems,
		"04_boundary_count":          boundaryCount,
		"05_over_boundary_count":     overBoundaryCount,
		"06_over_boundary_last_item": overBoundaryLastItem,
		"07_large_count":             largeCount,
		"08_large_first_item":        largeFirstItem,
		"09_large_last_item":         largeLastItem,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
