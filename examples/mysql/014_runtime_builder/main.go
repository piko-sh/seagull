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

// Command 014_runtime_builder applies its migrations to a MySQL database, then runs the
// example in example.go: unclaimed lost property is searched with filters, an order, a
// limit and a count chosen at run time, and unsafe inputs are refused.
//
// Run it with `go run . -dsn 'user:password@tcp(localhost:3306)/pier'`, or set
// DATABASE_URL instead of passing -dsn. Give it an empty database of its own, as the
// migrations create its tables and the example adds rows each time it runs.
package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/go-sql-driver/mysql"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_mysql"
)

// migrations holds the migration files, compiled into the binary.
//
//go:embed db/migrations
var migrations embed.FS

func main() {
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "MySQL data source name; defaults to $DATABASE_URL")
	flag.Parse()
	if err := execute(context.Background(), *dsn); err != nil {
		log.Fatal(err)
	}
}

// execute opens and migrates the database dsn names, and runs the example against it.
//
// Takes dsn (string) which is the database's data source name.
//
// Returns error when the database cannot be prepared or the example fails.
func execute(ctx context.Context, dsn string) error {
	if dsn == "" {
		return errors.New("set -dsn or DATABASE_URL to a MySQL data source name")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return err
	}
	// Seagull maps DATETIME and TIMESTAMP columns to time.Time, which the driver returns
	// only when parseTime is set.
	config.ParseTime = true
	dsn = config.FormatDSN()

	database, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	migrator, err := migrate.NewMigrator(database, migrate_backend_mysql.New(migrate.WithDriverMultiStatements(config.MultiStatements)), migrations, "db/migrations")
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("migrating: %w", err)
	}
	return run(ctx, database, os.Stdout)
}
