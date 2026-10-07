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

	_, err = conn.ExecContext(ctx, `INSERT INTO posts (title, category, views, published) VALUES
		('Go Tips', 'tech', 100, 1),
		('Rust Guide', 'tech', 200, 1),
		('Cooking', 'food', 50, 1),
		('Draft', 'tech', 0, 0),
		('Go Advanced', 'tech', 150, 1)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	allPublished, err := queries.SearchPosts().Where("published", "=", 1).All(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "All published:", err)
		os.Exit(1)
	}
	techPosts, err := queries.SearchPosts().Where("published", "=", 1).Where("category", "=", "tech").All(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Tech posts:", err)
		os.Exit(1)
	}
	techByViews, err := queries.SearchPosts().Where("published", "=", 1).Where("category", "=", "tech").OrderBy("views", "DESC").All(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Tech by views:", err)
		os.Exit(1)
	}
	limited, err := queries.SearchPosts().Where("published", "=", 1).Limit(2).All(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Limited:", err)
		os.Exit(1)
	}
	combined, err := queries.SearchPosts().Where("published", "=", 1).Where("category", "=", "tech").OrderBy("views", "DESC").Limit(2).All(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Combined:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"all_published": allPublished,
		"tech_posts":    techPosts,
		"tech_by_views": techByViews,
		"limited":       limited,
		"combined":      combined,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
