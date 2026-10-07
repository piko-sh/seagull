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
	"errors"
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

	_, err = conn.ExecContext(ctx, `CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT NOT NULL, views INTEGER NOT NULL)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO posts (id, title, views) VALUES (1, 'First', 10), (2, 'Second', 200)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	valid, err := queries.SearchPosts().Where("views", ">=", 100).OrderBy("views", "desc").All(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "valid query:", err)
		os.Exit(1)
	}

	_, unknownColumn := queries.SearchPosts().Where("secret", "=", 1).All(ctx)
	_, unknownOperator := queries.SearchPosts().Where("views", "<=>", 1).All(ctx)
	_, unknownDirection := queries.SearchPosts().OrderBy("views", "SIDEWAYS").One(ctx)
	_, countUnknownColumn := queries.SearchPosts().Where("secret", "=", 1).Count(ctx)
	_, firstErrorWins := queries.SearchPosts().Where("views", "<=>", 1).OrderBy("secret", "ASC").All(ctx)

	result := map[string]any{
		"valid":                valid,
		"unknown_column":       describe(unknownColumn, db.ErrSglUnknownColumn),
		"unknown_operator":     describe(unknownOperator, db.ErrSglUnknownOperator),
		"unknown_direction":    describe(unknownDirection, db.ErrSglUnknownDirection),
		"count_unknown_column": describe(countUnknownColumn, db.ErrSglUnknownColumn),
		"first_error_wins":     describe(firstErrorWins, db.ErrSglUnknownOperator),
		"wrapped_matches":      errors.Is(fmt.Errorf("search posts: %w", unknownColumn), db.ErrSglUnknownColumn),
		"values_are_distinct":  !errors.Is(unknownColumn, db.ErrSglUnknownOperator) && !errors.Is(unknownOperator, db.ErrSglUnknownDirection),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func describe(err error, target error) map[string]any {
	message := ""
	if err != nil {
		message = err.Error()
	}
	return map[string]any{
		"matches": errors.Is(err, target),
		"message": message,
	}
}
