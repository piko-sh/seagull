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

// Command 027_migrations_at_startup opens a DuckDB database and runs the example in
// example.go, which applies the migrations embedded in the binary each time the program
// starts, then uses the schema they built.
//
// Run it with `go run .` for an in-memory database, or `go run . -db harbour.duckdb` to
// keep the database in a file; a second run against the same file finds nothing to
// migrate.
package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"log"
	"os"

	_ "github.com/duckdb/duckdb-go/v2"
)

// migrations holds the migration files, compiled into the binary, so the program needs no
// files beside it to bring a database up to date.
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

// execute opens the DuckDB database at path, or an in-memory one when path is empty, and
// runs the example against it. The example migrates the database itself.
//
// Takes path (string) which is the database file, or empty.
//
// Returns error when the database cannot be opened or the example fails.
func execute(ctx context.Context, path string) error {
	database, err := sql.Open("duckdb", path)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	return run(ctx, database, migrations, os.Stdout)
}
