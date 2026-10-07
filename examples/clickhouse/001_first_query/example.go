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
	"fmt"
	"io"

	"seagull.sh/seagull/examples/clickhouse/001_first_query/internal/store"
)

// run adds two tasks, completes the first and lists them all, through the methods sgl
// generated from db/queries/tasks.sql.
//
// ClickHouse has no auto-increment and no RETURNING, so the caller chooses each task's id
// and reads a task back with GetTask. CompleteTask uses ALTER TABLE ... UPDATE, which
// queues a mutation that the server applies in the background, so the query is declared
// asyncexec and returns only an error (concept 36 explains why). The connection sets
// mutations_sync=2, which makes the server finish the mutation before it replies, so the
// list below already shows the task completed.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the task list.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	tasks := []store.CreateTaskParams{
		{ID: 1, Title: "Feed the seagulls"},
		{ID: 2, Title: "Count the chips"},
	}
	for _, task := range tasks {
		if err := queries.CreateTask(ctx, task); err != nil {
			return fmt.Errorf("creating task %d: %w", task.ID, err)
		}
	}

	feed, err := queries.GetTask(ctx, 1)
	if err != nil {
		return fmt.Errorf("getting task 1: %w", err)
	}
	fmt.Fprintf(output, "created task %d, %s\n", feed.ID, feed.Title)

	if err := queries.CompleteTask(ctx, feed.ID); err != nil {
		return fmt.Errorf("completing task %d: %w", feed.ID, err)
	}
	fmt.Fprintf(output, "completed task %d\n", feed.ID)

	listed, err := queries.ListTasks(ctx)
	if err != nil {
		return fmt.Errorf("listing tasks: %w", err)
	}
	for _, task := range listed {
		fmt.Fprintf(output, "%d\t%-18s completed=%d\n", task.ID, task.Title, task.Completed)
	}
	return nil
}
