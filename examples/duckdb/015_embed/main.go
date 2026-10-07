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

// Command 015_embed applies its migrations to an in-memory DuckDB database, then runs the
// example in example.go: boats are read back with their harbour as a nested struct and
// their skipper as a pointer that is nil when the boat has none.
//
// Run it with `go run .`, or `go run . -db example.duckdb` to keep the database in a
// file.
package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/duckdb/duckdb-go/v2"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_duckdb"
)

// migrations holds the migration files, compiled into the binary.
//
//go:embed db/migrations
var migrations embed.FS

func main() {
	path := flag.String("db", "", "DuckDB database file; an in-memory database when empty")
	flag.Parse()
	if err := execute(context.Background(), *path); err != nil {
		log.Fatal(err)
	}
}

// execute opens and migrates the DuckDB database at path, or an in-memory one when path
// is empty, and runs the example against it.
//
// Takes path (string) which is the database file, or empty.
//
// Returns error when the database cannot be prepared or the example fails.
func execute(ctx context.Context, path string) error {
	database, err := sql.Open("duckdb", path)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	migrator, err := migrate.NewMigrator(database, migrate_backend_duckdb.New(), migrations, "db/migrations")
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("migrating: %w", err)
	}
	return run(ctx, database, os.Stdout)
}
