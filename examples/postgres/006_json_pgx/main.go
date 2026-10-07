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

// Command 006_json_pgx applies its migrations to a PostgreSQL database through a pgxpool
// pool, then runs the example in example.go: chip shop orders are stored as JSONB
// documents and searched with ->, ->> and @>.
//
// Run it with `go run . -dsn postgres://user:password@localhost:5432/example`, or set
// DATABASE_URL instead of passing -dsn. Give it an empty database of its own, as the
// migrations create its tables and the example adds rows each time it runs.
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_postgres"
)

// migrations holds the migration files, compiled into the binary.
//
//go:embed db/migrations
var migrations embed.FS

func main() {
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "PostgreSQL connection string; defaults to $DATABASE_URL")
	flag.Parse()
	if err := execute(context.Background(), *dsn); err != nil {
		log.Fatal(err)
	}
}

// execute opens and migrates the database at dsn, and runs the example against it.
//
// Takes dsn (string) which is the PostgreSQL connection string.
//
// Returns error when the database cannot be prepared or the example fails.
func execute(ctx context.Context, dsn string) error {
	if dsn == "" {
		return errors.New("set -dsn or DATABASE_URL to a PostgreSQL connection string")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	// The migrator works on database/sql, so it borrows connections from the pool.
	database := stdlib.OpenDBFromPool(pool)
	defer func() { _ = database.Close() }()
	migrator, err := migrate.NewMigrator(database, migrate_backend_postgres.New(), migrations, "db/migrations")
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("migrating: %w", err)
	}
	return run(ctx, pool, os.Stdout)
}
