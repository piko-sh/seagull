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
	_, err = conn.ExecContext(ctx, `CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration 001:", err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration 002a:", err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `ALTER TABLE users ADD COLUMN created_at TEXT NOT NULL DEFAULT '2025-01-01'`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration 002b:", err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `CREATE INDEX idx_users_email ON users (email)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration 003a:", err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `CREATE INDEX idx_users_role ON users (role)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration 003b:", err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `CREATE TABLE posts (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id),
		title TEXT NOT NULL,
		body TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT '2025-01-01'
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration 004:", err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `CREATE VIEW user_post_counts AS
		SELECT u.id, u.name, u.role, COUNT(p.id) AS post_count
		FROM users u
		LEFT JOIN posts p ON p.user_id = u.id
		GROUP BY u.id`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration 005:", err)
		os.Exit(1)
	}

	queries := db.New(conn)
	err = queries.InsertUser(ctx, db.InsertUserParams{
		ID:    int64(1),
		Name:  "Alice",
		Email: "alice@example.com",
		Role:  "admin",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert user 1:", err)
		os.Exit(1)
	}

	err = queries.InsertUser(ctx, db.InsertUserParams{
		ID:    int64(2),
		Name:  "Bob",
		Email: "bob@example.com",
		Role:  "user",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert user 2:", err)
		os.Exit(1)
	}

	err = queries.InsertUser(ctx, db.InsertUserParams{
		ID:    int64(3),
		Name:  "Charlie",
		Email: "charlie@example.com",
		Role:  "user",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert user 3:", err)
		os.Exit(1)
	}
	err = queries.InsertPost(ctx, db.InsertPostParams{
		ID:     int64(1),
		UserID: int64(1),
		Title:  "First Post",
		Body:   "Hello world",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert post 1:", err)
		os.Exit(1)
	}

	err = queries.InsertPost(ctx, db.InsertPostParams{
		ID:     int64(2),
		UserID: int64(1),
		Title:  "Second Post",
		Body:   "More content",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert post 2:", err)
		os.Exit(1)
	}

	err = queries.InsertPost(ctx, db.InsertPostParams{
		ID:     int64(3),
		UserID: int64(2),
		Title:  "Bob's Post",
		Body:   "Bob writes here",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert post 3:", err)
		os.Exit(1)
	}

	err = queries.InsertPost(ctx, db.InsertPostParams{
		ID:     int64(4),
		UserID: int64(2),
		Title:  "Another from Bob",
		Body:   "More from Bob",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert post 4:", err)
		os.Exit(1)
	}

	err = queries.InsertPost(ctx, db.InsertPostParams{
		ID:     int64(5),
		UserID: int64(3),
		Title:  "Charlie's Post",
		Body:   "Charlie joins in",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert post 5:", err)
		os.Exit(1)
	}
	postCounts, err := queries.GetUserPostCounts(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetUserPostCounts:", err)
		os.Exit(1)
	}
	regularUsers, err := queries.ListUsersByRole(ctx, "user")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListUsersByRole:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"post_counts":   postCounts,
		"regular_users": regularUsers,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
