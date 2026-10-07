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

package main

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"

	"seagull.sh/seagull/examples/duckdb/027_migrations_at_startup/internal/store"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_duckdb"
)

// run starts the program twice against the same database. The first start applies both
// migrations before the program uses the schema; the second finds nothing to do.
//
// Takes database (*sql.DB) which is the database to migrate, empty or not.
// Takes migrations (fs.FS) which holds db/migrations, the embed.FS in main.go.
// Takes output (io.Writer) which receives what each start did.
//
// Returns error when migrating or a query fails.
func run(ctx context.Context, database *sql.DB, migrations fs.FS, output io.Writer) error {
	for _, boat := range []string{"Kittiwake", "Puffin"} {
		if err := start(ctx, database, migrations, boat, output); err != nil {
			return err
		}
	}
	return printHistory(ctx, database, migrations, output)
}

// start is what the program does each time it starts: migrate first, then work. Here the
// work is adding a boat and listing the harbour.
//
// Takes database (*sql.DB) which is the database.
// Takes migrations (fs.FS) which holds the migration files.
// Takes boat (string) which names the boat to add.
// Takes output (io.Writer) which receives what happened.
//
// Returns error when migrating or a query fails.
func start(ctx context.Context, database *sql.DB, migrations fs.FS, boat string, output io.Writer) error {
	// migrate_backend_duckdb.New() is the DuckDB backend: it knows how to create the history
	// table, binds its own queries with $N placeholders, and runs each migration in a
	// transaction, as DuckDB DDL is transactional. The backend takes no database lock;
	// callers must serialise migrations across migrator instances. NewMigrator reads the
	// numbered *.up.sql and *.down.sql files from the directory inside migrations.
	migrator, err := migrate.NewMigrator(database, migrate_backend_duckdb.New(), migrations, "db/migrations")
	if err != nil {
		return err
	}

	// Up applies every migration not yet in the history, in version order, and returns how
	// many it applied. On a database that is up to date it applies none.
	applied, err := migrator.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrating: %w", err)
	}
	fmt.Fprintf(output, "start-up: applied %d migrations\n", applied)

	// DuckDB cannot add a NOT NULL column to an existing table, so 0002 adds home_port with
	// a default only, and the column, like its field, may be NULL.
	queries := store.New(database)
	if err := queries.AddBoat(ctx, store.AddBoatParams{Name: boat, HomePort: new("Padstow")}); err != nil {
		return fmt.Errorf("adding %s: %w", boat, err)
	}
	boats, err := queries.ListBoats(ctx)
	if err != nil {
		return fmt.Errorf("listing boats: %w", err)
	}
	for _, row := range boats {
		homePort := "an unknown port"
		if row.HomePort != nil {
			homePort = *row.HomePort
		}
		fmt.Fprintf(output, "  %d %-9s of %s\n", row.ID, row.Name, homePort)
	}
	return nil
}

// printHistory prints the migration history the migrator keeps in the database.
//
// Takes database (*sql.DB) which is the migrated database.
// Takes migrations (fs.FS) which holds the migration files.
// Takes output (io.Writer) which receives the history.
//
// Returns error when the history cannot be read.
func printHistory(ctx context.Context, database *sql.DB, migrations fs.FS, output io.Writer) error {
	backend := migrate_backend_duckdb.New()
	migrator, err := migrate.NewMigrator(database, backend, migrations, "db/migrations")
	if err != nil {
		return err
	}
	statuses, err := migrator.Status(ctx)
	if err != nil {
		return fmt.Errorf("reading migration status: %w", err)
	}
	fmt.Fprintf(output, "history in %s:\n", backend.Names().History)
	for _, status := range statuses {
		fmt.Fprintf(output, "  %04d %-15s applied=%t\n", status.Version, status.Name, status.Applied)
	}
	return nil
}
