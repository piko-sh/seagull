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

// Command 06_ci is a runnable checkpoint for the Seagull tutorials.
package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"log"
	"seagull.sh/seagull/tests/tutorials/06_ci/internal/store"
	"seagull.sh/seagull/tests/tutorials/internal/support"
)

// migrations embeds the tutorial migration files.
//
//go:embed db/migrations
var migrations embed.FS

// main runs the tutorial against a migrated database.
func main() {
	if err := support.Main(migrations, run); err != nil {
		log.Fatal(err)
	}
}

// run exercises generated queries and prints the tutorial results.
//
// Takes db (*sql.DB) which is the migrated tutorial database.
// Takes out (io.Writer) which receives the results.
//
// Returns error when a query fails or an expected row is missing.
func run(ctx context.Context, db *sql.DB, out io.Writer) error {
	queries := store.New(db)
	task, err := queries.CreateTask(ctx, "Feed the seagulls")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, task.ID, task.Title)
	if err := queries.SetNotes(ctx, store.SetNotesParams{Notes: store.NullOf("Bring chips"), ID: task.ID}); err != nil {
		return err
	}
	found, ok, err := queries.FindTask(ctx, task.ID)
	if err != nil {
		return err
	}
	if !ok || !found.Notes.Valid {
		return fmt.Errorf("task or notes missing")
	}
	fmt.Fprintln(out, found.Notes.V)
	_, ok, err = queries.FindTask(ctx, 999)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "missing:", !ok)
	if err := queries.AddProject(ctx, "Harbour"); err != nil {
		return err
	}
	if err := queries.AssignProject(ctx, store.AssignProjectParams{ProjectID: store.NullOf(int64(1)), ID: task.ID}); err != nil {
		return err
	}
	rows, err := queries.SearchTasks(ctx, store.SearchTasksParams{})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Projects == nil || !row.Projects.Name.Valid {
			return fmt.Errorf("project missing")
		}
		fmt.Fprintln(out, row.Title, "in", row.Projects.Name.V)
	}
	priority, err := queries.TaskPriority(ctx, task.ID)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "priority:", priority.Priority)
	stop := errors.New("roll back this transaction")
	err = queries.RunInTx(ctx, db, func(tx *store.Queries) error {
		if _, err := tx.CreateTask(ctx, "Do not keep this task"); err != nil {
			return err
		}
		return stop
	})
	if !errors.Is(err, stop) {
		return fmt.Errorf("expected rollback error, got %v", err)
	}
	tasks, err := queries.ListTasks(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "tasks after rollback:", len(tasks))

	return nil
}
