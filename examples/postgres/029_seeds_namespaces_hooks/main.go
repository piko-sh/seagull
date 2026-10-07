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

// Command 029_seeds_namespaces_hooks connects to a PostgreSQL database and runs the
// example in example.go, which migrates it under its own namespace with progress hooks,
// including an index built CONCURRENTLY outside a transaction, and then applies the seed
// files.
//
// Run it with `go run . -dsn postgres://user:password@localhost:5432/example`, or set
// DATABASE_URL instead of passing -dsn. Give it an empty database of its own; a second
// run against the same database applies no migrations and no seeds.
package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"flag"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// files holds the migration and seed files, compiled into the binary.
//
//go:embed db/migrations db/seeds
var files embed.FS

func main() {
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "PostgreSQL connection string; defaults to $DATABASE_URL")
	flag.Parse()
	if err := execute(context.Background(), *dsn); err != nil {
		log.Fatal(err)
	}
}

// execute connects to the database at dsn and runs the example against it. The example
// migrates and seeds the database itself.
//
// Takes dsn (string) which is the PostgreSQL connection string.
//
// Returns error when the database cannot be opened or the example fails.
func execute(ctx context.Context, dsn string) error {
	if dsn == "" {
		return errors.New("set -dsn or DATABASE_URL to a PostgreSQL connection string")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	return run(ctx, database, files, os.Stdout)
}
