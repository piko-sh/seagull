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

	_, err = conn.ExecContext(ctx, `CREATE TABLE accounts (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		balance INTEGER NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO accounts (id, name, balance) VALUES
		(1, 'Alice', 100),
		(2, 'Bob', 200)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	err = queries.RunInTx(ctx, conn, func(txQueries *db.Queries) error {
		err := txQueries.UpdateBalance(ctx, db.UpdateBalanceParams{
			Balance: int64(50),
			ID:      int64(1),
		})
		if err != nil {
			return err
		}
		return txQueries.UpdateBalance(ctx, db.UpdateBalanceParams{
			Balance: int64(250),
			ID:      int64(2),
		})
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "RunInTx commit:", err)
		os.Exit(1)
	}

	afterCommit, err := queries.ListAccounts(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListAccounts after commit:", err)
		os.Exit(1)
	}
	err = queries.RunInTx(ctx, conn, func(txQueries *db.Queries) error {
		err := txQueries.UpdateBalance(ctx, db.UpdateBalanceParams{
			Balance: int64(0),
			ID:      int64(1),
		})
		if err != nil {
			return err
		}
		return errors.New("simulated failure")
	})
	if err == nil {
		fmt.Fprintln(os.Stderr, "expected error from failed transaction")
		os.Exit(1)
	}

	afterRollback, err := queries.ListAccounts(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListAccounts after rollback:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"after_commit":   afterCommit,
		"after_rollback": afterRollback,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
