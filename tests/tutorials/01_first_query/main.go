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

// Command 01_first_query is a runnable checkpoint for the Seagull tutorials.
package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"log"

	"seagull.sh/seagull/tests/tutorials/01_first_query/internal/store"
	"seagull.sh/seagull/tests/tutorials/internal/support"
)

//go:embed db/migrations
var migrations embed.FS

func main() {
	if err := support.Main(migrations, run); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, db *sql.DB, out io.Writer) error {
	queries := store.New(db)
	task, err := queries.CreateTask(ctx, "Feed the seagulls")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, task.ID, task.Title)

	return nil
}
