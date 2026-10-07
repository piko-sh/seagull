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

	"seagull.sh/seagull/examples/mariadb/001_first_query/internal/store"
)

// run adds two tasks, completes the first and lists them all, through the methods sgl
// generated from db/queries/tasks.sql.
//
// MariaDB, unlike MySQL, has INSERT ... RETURNING, so CreateTask is a one query that
// hands back the new row, the AUTO_INCREMENT id and the column defaults included.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the task list.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	feed, err := queries.CreateTask(ctx, "Feed the seagulls")
	if err != nil {
		return fmt.Errorf("creating a task: %w", err)
	}
	if _, err := queries.CreateTask(ctx, "Count the chips"); err != nil {
		return fmt.Errorf("creating a task: %w", err)
	}

	completed, err := queries.CompleteTask(ctx, feed.ID)
	if err != nil {
		return fmt.Errorf("completing task %d: %w", feed.ID, err)
	}
	fmt.Fprintf(output, "completed %d task\n", completed)

	tasks, err := queries.ListTasks(ctx)
	if err != nil {
		return fmt.Errorf("listing tasks: %w", err)
	}
	for _, task := range tasks {
		fmt.Fprintf(output, "%d\t%-18s completed=%d\n", task.ID, task.Title, task.Completed)
	}
	return nil
}
