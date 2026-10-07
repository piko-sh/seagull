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

	_, err = conn.ExecContext(ctx, `CREATE TABLE events (id INTEGER PRIMARY KEY, name TEXT NOT NULL, data TEXT NOT NULL)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO events (id, name, data) VALUES
		(1, 'signup', '{"name":"Alice","category":"auth","user":{"email":"alice@test.com"}}'),
		(2, 'purchase', '{"name":"Bob","category":"sales","amount":99.99,"user":{"email":"bob@test.com"}}'),
		(3, 'login', '{"name":"Charlie","category":"auth","user":{"email":"charlie@test.com"}}')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	eventName, err := queries.GetEventName(ctx, int64(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetEventName:", err)
		os.Exit(1)
	}

	nestedValue, err := queries.GetNestedValue(ctx, int64(2))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetNestedValue:", err)
		os.Exit(1)
	}

	byCategory, err := queries.ListByCategory(ctx, "auth")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListByCategory:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"event_name":   eventName,
		"nested_value": nestedValue,
		"by_category":  byCategory,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
