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

// Command 07_postgres runs the task application using PostgreSQL and pgx.
package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_postgres"
	"seagull.sh/seagull/tests/tutorials/07_postgres/internal/store"
)

//go:embed db/migrations
var migrations embed.FS

func main() {
	if err := run(context.Background(), os.Getenv("SGL_EXAMPLE_DSN"), os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, dsn string, out io.Writer) error {
	if dsn == "" {
		return fmt.Errorf("set SGL_EXAMPLE_DSN to a disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	migrator, err := migrate.NewMigrator(db, migrate_backend_postgres.New(), migrations, "db/migrations")
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return err
	}
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer connection.Close(ctx)
	task, err := store.New(connection).CreateTask(ctx, "Feed the seagulls")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, task.Title)
	return nil
}
