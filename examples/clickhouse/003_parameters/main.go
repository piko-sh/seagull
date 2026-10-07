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

// Command 003_parameters applies its migrations to a ClickHouse database, then runs the
// example in example.go: a ferry timetable is searched and booked through queries whose
// parameters are named by their {name:Type} placeholders.
//
// Run it with `go run .` against a local server, or `go run . -dsn <dsn>` to choose the
// database; give it an empty one. Keep mutations_sync=2 in the DSN so each booking has
// finished before the seats are read back.
package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/ClickHouse/clickhouse-go/v2"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_clickhouse"
)

// migrations holds the migration files, compiled into the binary.
//
//go:embed db/migrations
var migrations embed.FS

func main() {
	dsn := flag.String("dsn", "clickhouse://localhost:9000/default?mutations_sync=2", "ClickHouse DSN of an empty database")
	flag.Parse()
	if err := execute(context.Background(), *dsn); err != nil {
		log.Fatal(err)
	}
}

// execute opens and migrates the database the DSN names, and runs the example against it.
//
// Takes dsn (string) which names the ClickHouse database.
//
// Returns error when the database cannot be prepared or the example fails.
func execute(ctx context.Context, dsn string) error {
	database, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	migrator, err := migrate.NewMigrator(database, migrate_backend_clickhouse.New(), migrations, "db/migrations")
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("migrating: %w", err)
	}
	return run(ctx, database, os.Stdout)
}
