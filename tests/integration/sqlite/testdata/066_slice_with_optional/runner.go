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
		title TEXT NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO tasks (id, status, priority, title) VALUES
		('t1', 'PENDING', 1, 'Low'),
		('t2', 'PENDING', 3, 'High'),
		('t3', 'PROCESSING', 2, 'Mid'),
		('t4', 'COMPLETE', 1, 'Done'),
		('t5', 'FAILED', 3, 'Fail'),
		('t6', 'RETRYING', 2, 'Retry')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	withoutFilter, err := queries.FetchByStatusesWithOptionalPriority(ctx, db.FetchByStatusesWithOptionalPriorityParams{
		Statuses: []string{"PENDING", "RETRYING"},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FetchByStatusesWithOptionalPriority (no filter):", err)
		os.Exit(1)
	}

	minPriority2 := int64(2)
	withMinPriority2, err := queries.FetchByStatusesWithOptionalPriority(ctx, db.FetchByStatusesWithOptionalPriorityParams{
		Statuses:    []string{"PENDING", "PROCESSING", "RETRYING"},
		MinPriority: &minPriority2,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FetchByStatusesWithOptionalPriority (min_priority=2):", err)
		os.Exit(1)
	}

	minPriority3 := int64(3)
	withMinPriority3, err := queries.FetchByStatusesWithOptionalPriority(ctx, db.FetchByStatusesWithOptionalPriorityParams{
		Statuses:    []string{"PENDING", "RETRYING"},
		MinPriority: &minPriority3,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FetchByStatusesWithOptionalPriority (min_priority=3):", err)
		os.Exit(1)
	}

	result := map[string]any{
		"without_filter":      withoutFilter,
		"with_min_priority_2": withMinPriority2,
		"with_min_priority_3": withMinPriority3,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
