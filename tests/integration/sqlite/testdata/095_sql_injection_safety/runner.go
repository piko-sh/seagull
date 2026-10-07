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
		role TEXT NOT NULL,
		secret TEXT NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO users (id, name, role, secret) VALUES
		(1, 'Alice', 'admin', 'hunter2'),
		(2, 'Bob', 'user', 'password123'),
		(3, 'Charlie', 'user', 'qwerty')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	result := map[string]any{}

	normalQuery, err := queries.SearchUsers().Where("role", "=", "admin").All(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "normal query:", err)
		os.Exit(1)
	}
	result["normal_query"] = normalQuery

	tautologyResult, err := queries.SearchUsers().Where("name", "=", "' OR '1'='1").All(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tautology:", err)
		os.Exit(1)
	}
	result["value_tautology"] = tautologyResult

	unionResult, err := queries.SearchUsers().Where("name", "=", "' UNION SELECT id, secret, role FROM users --").All(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "union:", err)
		os.Exit(1)
	}
	result["value_union_injection"] = unionResult

	dropResult, err := queries.SearchUsers().Where("name", "=", "'; DROP TABLE users; --").All(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "drop:", err)
		os.Exit(1)
	}
	result["value_drop_injection"] = dropResult

	afterDropCheck, err := queries.SearchUsers().Where("role", "=", "admin").All(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "post-drop check:", err)
		os.Exit(1)
	}
	result["table_still_exists"] = afterDropCheck

	result["where_column_injection"] = expectRejection(func() error {
		_, err := queries.SearchUsers().Where("id; DROP TABLE users; --", "=", 1).All(ctx)
		return err
	})

	result["where_column_subquery"] = expectRejection(func() error {
		_, err := queries.SearchUsers().Where("(SELECT secret FROM users LIMIT 1)", "=", 1).All(ctx)
		return err
	})

	result["where_operator_injection"] = expectRejection(func() error {
		_, err := queries.SearchUsers().Where("id", "= 1 OR 1=1; --", 1).All(ctx)
		return err
	})

	result["orderby_column_injection"] = expectRejection(func() error {
		_, err := queries.SearchUsers().OrderBy("id; DROP TABLE users; --", "ASC").All(ctx)
		return err
	})

	result["orderby_column_subquery"] = expectRejection(func() error {
		_, err := queries.SearchUsers().OrderBy("(CASE WHEN (SELECT 1) THEN id ELSE name END)", "ASC").All(ctx)
		return err
	})

	result["orderby_direction_injection"] = expectRejection(func() error {
		_, err := queries.SearchUsers().OrderBy("id", "ASC; DROP TABLE users; --").All(ctx)
		return err
	})

	result["orderby_direction_subquery"] = expectRejection(func() error {
		_, err := queries.SearchUsers().OrderBy("id", "ASC, (SELECT secret FROM users LIMIT 1)").All(ctx)
		return err
	})

	// Hidden-column abuse: SearchUsers selects only id, name, role, so the `secret` column
	// (present on the users table but never projected) must not be reachable as a filter
	// oracle, a LIKE prefix oracle, an ORDER BY side-channel, or through a json/cast wrapper.
	result["where_hidden_column_equality"] = expectRejection(func() error {
		_, err := queries.SearchUsers().Where("secret", "=", "hunter2").All(ctx)
		return err
	})

	result["where_hidden_column_like_oracle"] = expectRejection(func() error {
		_, err := queries.SearchUsers().Where("secret", "LIKE", "h%").All(ctx)
		return err
	})

	result["orderby_hidden_column"] = expectRejection(func() error {
		_, err := queries.SearchUsers().OrderBy("secret", "ASC").All(ctx)
		return err
	})

	result["where_hidden_column_json_wrapper"] = expectRejection(func() error {
		_, err := queries.SearchUsers().Where("json_extract(secret, '$.x')", "=", "y").All(ctx)
		return err
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func expectRejection(run func() error) map[string]any {
	rejected := false
	panicked := false
	message := ""

	func() {
		defer func() {
			if recovery := recover(); recovery != nil {
				panicked = true
				rejected = true
				message = fmt.Sprintf("%v", recovery)
			}
		}()
		if err := run(); err != nil {
			rejected = true
			message = err.Error()
		}
	}()

	return map[string]any{
		"rejected": rejected,
		"panicked": panicked,
		"message":  message,
	}
}
