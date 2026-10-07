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
	"fmt"
	"io"

	"seagull.sh/seagull/examples/clickhouse/001_first_query_native/internal/store"
)

// run adds two tasks, completes the first and lists them all, through the methods sgl
// generated for the native clickhouse-go driver from db/queries/tasks.sql.
//
// The queries are the same as the database/sql variant's; only the generated code
// differs. store.DBTX is the subset of driver.Conn the methods call: Exec returns only an
// error, Query returns driver.Rows, and every {name:Type} placeholder is bound as
// clickhouse.Named("p_<name>", ...). ClickHouse has no auto-increment and no RETURNING,
// so the caller chooses each id, and CompleteTask is an ALTER TABLE ... UPDATE mutation
// declared asyncexec (concept 36). The connection sets mutations_sync=2, so the mutation
// has finished before the list is read.
//
// Takes connection (store.DBTX) which is a native connection to a migrated database.
// Takes output (io.Writer) which receives the task list.
//
// Returns error when a query fails.
func run(ctx context.Context, connection store.DBTX, output io.Writer) error {
	queries := store.New(connection)

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
