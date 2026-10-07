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

	queries := db.New(conn)

	acc1, err := queries.InsertAccount(ctx, db.InsertAccountParams{
		Username: "alice",
		Status:   "active",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	acc2, err := queries.InsertAccount(ctx, db.InsertAccountParams{
		Username: "bob",
		Status:   "inactive",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	acc3, err := queries.InsertAccount(ctx, db.InsertAccountParams{
		Username: "charlie",
		Status:   "active",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	acc4, err := queries.InsertAccount(ctx, db.InsertAccountParams{
		Username: "diana",
		Status:   "suspended",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	activeAccounts, err := queries.ListByStatus(ctx, "active")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	allAccounts, err := queries.ListAllAccounts(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	result := map[string]any{
		"acc_1":           acc1,
		"acc_2":           acc2,
		"acc_3":           acc3,
		"acc_4":           acc4,
		"active_accounts": activeAccounts,
		"all_accounts":    allAccounts,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
