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
	"time"

	_ "github.com/go-sql-driver/mysql"

	"case_runner/db"
)

func mustParseDate(value string) time.Time {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return parsed
}

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

	err = queries.InsertArticle(ctx, db.InsertArticleParams{
		Title:       "Introduction to MariaDB",
		Body:        "MariaDB is a powerful open-source relational database management system.",
		Author:      "Alice",
		PublishedAt: mustParseDate("2025-01-15"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertArticle 1:", err)
		os.Exit(1)
	}

	err = queries.InsertArticle(ctx, db.InsertArticleParams{
		Title:       "Advanced SQL Queries",
		Body:        "Learn about window functions, CTEs, and fulltext search in SQL databases.",
		Author:      "Bob",
		PublishedAt: mustParseDate("2025-02-20"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertArticle 2:", err)
		os.Exit(1)
	}

	err = queries.InsertArticle(ctx, db.InsertArticleParams{
		Title:       "MariaDB Performance Tuning",
		Body:        "Tips and tricks for optimising MariaDB performance with proper indexing.",
		Author:      "Alice",
		PublishedAt: mustParseDate("2025-03-10"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertArticle 3:", err)
		os.Exit(1)
	}

	byAuthor, err := queries.GetByAuthor(ctx, "Alice")
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetByAuthor:", err)
		os.Exit(1)
	}

	byTitle, err := queries.GetByTitle(ctx, "Advanced SQL Queries")
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetByTitle:", err)
		os.Exit(1)
	}

	fulltext, err := queries.FulltextSearch(ctx, "+MariaDB")
	if err != nil {
		fmt.Fprintln(os.Stderr, "FulltextSearch:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"by_author": byAuthor,
		"by_title":  byTitle,
		"fulltext":  fulltext,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
