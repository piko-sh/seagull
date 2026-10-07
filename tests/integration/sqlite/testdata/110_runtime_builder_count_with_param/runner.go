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
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE posts (id INTEGER PRIMARY KEY, author TEXT NOT NULL, title TEXT NOT NULL, views INTEGER NOT NULL)`); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO posts (id, author, title, views) VALUES
		(1, 'ada', 'Engines', 10),
		(2, 'ada', 'Notes', 200),
		(3, 'ada', 'Looms', 300),
		(4, 'grace', 'Compilers', 500)`); err != nil {
		return err
	}

	queries := db.New(conn)

	all, err := queries.PostsByAuthor("ada").Count(ctx)
	if err != nil {
		return fmt.Errorf("count by author: %w", err)
	}
	popular, err := queries.PostsByAuthor("ada").Where("views", ">=", 100).Count(ctx)
	if err != nil {
		return fmt.Errorf("count by author with where: %w", err)
	}
	popularRows, err := queries.PostsByAuthor("ada").Where("views", ">=", 100).OrderBy("views", "DESC").All(ctx)
	if err != nil {
		return fmt.Errorf("list by author with where: %w", err)
	}
	named, err := queries.PostsByAuthorNamed(db.PostsByAuthorNamedParams{Author: "ada", MinViews: 100}).Count(ctx)
	if err != nil {
		return fmt.Errorf("count by named parameters: %w", err)
	}
	namedFiltered, err := queries.PostsByAuthorNamed(db.PostsByAuthorNamedParams{Author: "ada", MinViews: 100}).Where("title", "=", "Looms").Count(ctx)
	if err != nil {
		return fmt.Errorf("count by named parameters with where: %w", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"ada_count":                all,
		"ada_popular_count":        popular,
		"ada_popular_rows":         popularRows,
		"ada_named_count":          named,
		"ada_named_filtered_count": namedFiltered,
	})
}
