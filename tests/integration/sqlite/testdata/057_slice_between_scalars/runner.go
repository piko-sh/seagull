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

	_, err = conn.ExecContext(ctx, `CREATE TABLE tasks (
		id TEXT PRIMARY KEY,
		status TEXT NOT NULL,
		priority INTEGER NOT NULL,
		title TEXT NOT NULL,
		active INTEGER NOT NULL DEFAULT 1
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO tasks (id, status, priority, title, active) VALUES
		('t1', 'PENDING', 1, 'Low pending', 1),
		('t2', 'PENDING', 3, 'High pending', 1),
		('t3', 'PROCESSING', 2, 'Mid processing', 1),
		('t4', 'COMPLETE', 1, 'Done task', 1),
		('t5', 'FAILED', 3, 'High failed', 1),
		('t6', 'RETRYING', 2, 'Mid retrying', 1)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	fetched, err := queries.FetchByPriorityStatusesAndActive(ctx, db.FetchByPriorityStatusesAndActiveParams{
		Priority: int64(2),
		Statuses: []string{"PENDING", "PROCESSING", "RETRYING"},
		Active:   int64(1),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FetchByPriorityStatusesAndActive:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"fetch_by_priority_statuses_and_active": fetched,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
