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

// Command 027_migrations_at_startup opens a ClickHouse database and runs the example in
// example.go, which applies the migrations embedded in the binary each time the program
// starts, then uses the schema they built.
//
// Run it with `go run .` against a local server, or `go run . -dsn <dsn>` to choose the
// database; give it an empty one. A second run against the same database finds nothing to
// migrate.
package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"log"
	"os"

	_ "github.com/ClickHouse/clickhouse-go/v2"
)

// migrations holds the migration files, compiled into the binary, so the program needs no
// files beside it to bring a database up to date.
//
//go:embed db/migrations
var migrations embed.FS

func main() {
	dsn := flag.String("dsn", "clickhouse://localhost:9000/default", "ClickHouse DSN of an empty database")
	flag.Parse()
	if err := execute(context.Background(), *dsn); err != nil {
		log.Fatal(err)
	}
}

// execute opens the database the DSN names and runs the example against it. The example
// migrates the database itself.
//
// Takes dsn (string) which names the ClickHouse database.
//
// Returns error when the database cannot be opened or the example fails.
func execute(ctx context.Context, dsn string) error {
	database, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	return run(ctx, database, migrations, os.Stdout)
}
