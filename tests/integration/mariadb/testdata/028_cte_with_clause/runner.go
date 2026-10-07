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

	entries := []db.InsertCategoryParams{
		{ID: int32(1), Name: "Root", ParentID: nil},
		{ID: int32(2), Name: "Electronics", ParentID: new(int32(1))},
		{ID: int32(3), Name: "Clothing", ParentID: new(int32(1))},
		{ID: int32(4), Name: "Phones", ParentID: new(int32(2))},
		{ID: int32(5), Name: "Laptops", ParentID: new(int32(2))},
		{ID: int32(6), Name: "T-Shirts", ParentID: new(int32(3))},
		{ID: int32(7), Name: "iPhones", ParentID: new(int32(4))},
	}

	for index, entry := range entries {
		err = queries.InsertCategory(ctx, entry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "InsertCategory %d: %v\n", index+1, err)
			os.Exit(1)
		}
	}
	subtree, err := queries.GetSubtree(ctx, int32(2))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetSubtree:", err)
		os.Exit(1)
	}
	ancestors, err := queries.GetAncestors(ctx, int32(7))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetAncestors:", err)
		os.Exit(1)
	}
	roots, err := queries.ListRootCategories(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListRootCategories:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"subtree_from_electronics": subtree,
		"ancestors_of_iphones":     ancestors,
		"root_categories":          roots,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
