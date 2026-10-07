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

	"github.com/jackc/pgx/v5/pgxpool"

	"seagull.sh/seagull/examples/postgres/001_first_query_pgx/internal/store"
)

// run adds two tasks, completes the first and lists them all, through the methods sgl
// generated from db/queries/tasks.sql with go.driver: pgx. The generated DBTX interface
// is pgx's own Exec, Query and QueryRow, with SendBatch, CopyFrom and Begin, so store.New
// takes a *pgxpool.Pool as it is, and a pgx.Conn or a pgx.Tx just as well. The queries
// and the Row structs are the same as with database/sql: BIGSERIAL gives int64 and
// TIMESTAMPTZ gives time.Time.
//
// Takes pool (*pgxpool.Pool) which is a pool on a migrated database.
// Takes output (io.Writer) which receives the task list.
//
// Returns error when a query fails.
func run(ctx context.Context, pool *pgxpool.Pool, output io.Writer) error {
	queries := store.New(pool)

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
		fmt.Fprintf(output, "%d\t%-18s completed=%t\n", task.ID, task.Title, task.Completed)
	}
	return nil
}
