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
		id INTEGER PRIMARY KEY,
		workflow_id INTEGER NOT NULL,
		status TEXT NOT NULL,
		name TEXT NOT NULL,
		priority INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO tasks (id, workflow_id, status, name, priority, created_at, updated_at) VALUES
		(1, 100, 'COMPLETE', 'build-app',     3, 1000, 1010),
		(2, 100, 'FAILED',   'deploy-app',    5, 1001, 1020),
		(3, 100, 'RUNNING',  'test-app',      2, 1002, 1030),
		(4, 100, 'PENDING',  'lint-app',      1, 1003, 1040),
		(5, 200, 'COMPLETE', 'build-service', 4, 2000, 2010),
		(6, 200, 'COMPLETE', 'test-service',  2, 2001, 2020),
		(7, 200, 'RUNNING',  'deploy-service', 6, 2002, 2030)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	workflowSummary, err := queries.GetWorkflowSummary(ctx, 10)
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetWorkflowSummary:", err)
		os.Exit(1)
	}

	activeTasks, err := queries.GetActiveTasks(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetActiveTasks:", err)
		os.Exit(1)
	}

	outsidePriority, err := queries.GetTasksOutsidePriorityRange(ctx, db.GetTasksOutsidePriorityRangeParams{
		MinPriority: 2,
		MaxPriority: 4,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetTasksOutsidePriorityRange:", err)
		os.Exit(1)
	}

	notMatching, err := queries.GetTasksNotMatching(ctx, "%service%")
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetTasksNotMatching:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"workflow_summary": workflowSummary,
		"active_tasks":     activeTasks,
		"outside_priority": outsidePriority,
		"not_matching":     notMatching,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
