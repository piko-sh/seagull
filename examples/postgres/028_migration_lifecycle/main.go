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

// Command 028_migration_lifecycle connects to a PostgreSQL database and runs the example
// in example.go, which takes the embedded migrations through UpTo, Up, Down, Status and
// Validate, including a migration with no down file and an applied file edited later, and
// then shows the advisory and table locks that keep two instances from migrating at once.
//
// Run it with `go run . -dsn postgres://user:password@localhost:5432/example`, or set
// DATABASE_URL instead of passing -dsn. Give it an empty database of its own; a second
// run against the same database starts from where the first one left it.
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

// execute connects to the database at dsn and runs the example against it. The example
// migrates the database itself.
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
	return run(ctx, database, migrations, os.Stdout)
}
