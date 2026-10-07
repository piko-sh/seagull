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

// Command 024_read_replicas applies its migrations to a PostgreSQL database, opens a
// second, read-only pool on the same database to stand in for a replica, then runs the
// example in example.go: writes go to the writer, reads to the reader.
//
// Run it with `go run . -dsn postgres://user:password@localhost:5432/example`, or set
// DATABASE_URL instead of passing -dsn. Give it an empty database of its own, as the
// migrations create its tables and the example adds rows each time it runs. Pass -replica
// with a real replica's connection string to read from it instead.
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

	"github.com/jackc/pgx/v5"
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
	replica := flag.String("replica", "", "read replica's connection string; the primary, read-only, when empty")
	flag.Parse()
	if err := execute(context.Background(), *dsn, *replica); err != nil {
		log.Fatal(err)
	}
}

// execute opens and migrates the database at dsn, opens a read-only pool on the replica,
// or on dsn when replica is empty, and runs the example against both.
//
// Takes dsn (string) which is the primary's connection string.
// Takes replica (string) which is the replica's connection string, or empty.
//
// Returns error when the databases cannot be prepared or the example fails.
func execute(ctx context.Context, dsn, replica string) error {
	if dsn == "" {
		return errors.New("set -dsn or DATABASE_URL to a PostgreSQL connection string")
	}
	writer, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = writer.Close() }()

	migrator, err := migrate.NewMigrator(writer, migrate_backend_postgres.New(), migrations, "db/migrations")
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("migrating: %w", err)
	}

	if replica == "" {
		replica = dsn
	}
	reader, err := openReader(replica)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	return run(ctx, writer, reader, os.Stdout)
}

// openReader opens a pool whose sessions start with default_transaction_read_only on, so
// its transactions default to read-only and refuse writes to the sightings table.
//
// Takes dsn (string) which is the connection string, as a URL or keyword/value pairs.
//
// Returns *sql.DB which is the read-only pool.
// Returns error when the connection string cannot be parsed.
func openReader(dsn string) (*sql.DB, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	config.RuntimeParams["default_transaction_read_only"] = "on"
	return stdlib.OpenDB(*config), nil
}
