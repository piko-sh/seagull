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

	_, err = conn.ExecContext(ctx, `INSERT INTO users (id, name, email, role) VALUES
		(1, 'Alice', 'alice@test.com', 'admin'),
		(2, 'Bob', 'bob@test.com', 'user'),
		(3, 'Alice', 'alice2@test.com', 'user'),
		(4, 'Charlie', 'charlie@test.com', 'admin')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	noFilter, err := queries.SearchUsers(ctx, db.SearchUsersParams{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchUsers (no filter):", err)
		os.Exit(1)
	}
	byName, err := queries.SearchUsers(ctx, db.SearchUsersParams{
		Name: new("Alice"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchUsers (by name):", err)
		os.Exit(1)
	}
	byRole, err := queries.SearchUsers(ctx, db.SearchUsersParams{
		Role: new("admin"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchUsers (by role):", err)
		os.Exit(1)
	}
	byNameAndRole, err := queries.SearchUsers(ctx, db.SearchUsersParams{
		Name: new("Alice"),
		Role: new("user"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchUsers (by name and role):", err)
		os.Exit(1)
	}

	result := map[string]any{
		"no_filter":        noFilter,
		"by_name":          byName,
		"by_role":          byRole,
		"by_name_and_role": byNameAndRole,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
