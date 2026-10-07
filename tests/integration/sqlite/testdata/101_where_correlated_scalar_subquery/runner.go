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

	schema := []string{
		`CREATE TABLE accounts (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE account_versions (id INTEGER PRIMARY KEY, account_id INTEGER NOT NULL, email TEXT NOT NULL, status TEXT NOT NULL)`,
	}
	for _, ddl := range schema {
		if _, err = conn.ExecContext(ctx, ddl); err != nil {
			fmt.Fprintln(os.Stderr, "schema:", err)
			os.Exit(1)
		}
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO accounts (id) VALUES (1), (2)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed accounts:", err)
		os.Exit(1)
	}
	// Account 1 has two versions of the same email; version 20 is the latest, version 10 older.
	_, err = conn.ExecContext(ctx, `INSERT INTO account_versions (id, account_id, email, status) VALUES `+
		`(10, 1, 'a@x.com', 'active'), (20, 1, 'a@x.com', 'suspended'), (30, 2, 'b@x.com', 'active')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed versions:", err)
		os.Exit(1)
	}

	queries := db.New(conn)

	// Latest version of a@x.com is version 20 ('suspended').
	latest, err := queries.GetAccountByEmail(ctx, "a@x.com")
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetAccountByEmail:", err)
		os.Exit(1)
	}

	// Latest version of a@x.com strictly before version 20 is version 10 ('active'): this
	// exercises the parameter (?2) that lives inside the correlated WHERE subquery.
	atTime, err := queries.GetAccountByEmailAtTime(ctx, db.GetAccountByEmailAtTimeParams{
		Email:           "a@x.com",
		BeforeVersionID: int64(20),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetAccountByEmailAtTime:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"latest":           latest,
		"latest_before_20": atTime,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
