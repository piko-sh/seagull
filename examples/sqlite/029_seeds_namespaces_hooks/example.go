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

	"seagull.sh/seagull/examples/sqlite/029_seeds_namespaces_hooks/internal/store"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
)

const (
	// namespace prefixes the tables that record this program's migrations and seeds.
	namespace = "chippy"
)

// run migrates the database under its own namespace with hooks that report progress,
// applies the seed files twice to show they run once, and prints the seeded menu.
//
// Takes database (*sql.DB) which is an empty database.
// Takes files (fs.FS) which holds db/migrations and db/seeds, the embed.FS in main.go.
// Takes output (io.Writer) which receives each step's result.
//
// Returns error when migrating, seeding or a query fails.
func run(ctx context.Context, database *sql.DB, files fs.FS, output io.Writer) error {
	// WithNamespace renames every table the migrator and seeder keep, so several programs or
	// modules can each keep their own history in one database. sgl.yaml sets
	// migrate.namespace to match, so `sgl migrate` uses the same tables.
	backend := migrate_backend_sqlite.New(migrate.WithNamespace(namespace))
	names := backend.Names()
	fmt.Fprintf(output, "history tables: %s and %s\n", names.History, names.Seeds)

	// Hooks run around the migrations: before the run with the pending versions, and after
	// each migration. Returning an error from a hook stops the run.
	migrator, err := migrate.NewMigrator(database, backend, files, "db/migrations",
		migrate.WithBeforeRun(func(_ context.Context, pending migrate.MigrationRunHookContext) error {
			fmt.Fprintf(output, "migrating: %d pending, versions %v\n", pending.PendingCount, pending.PendingVersions)
			return nil
		}),
		migrate.WithAfterMigration(func(_ context.Context, migration migrate.MigrationHookContext) error {
			fmt.Fprintf(output, "  applied %04d %s\n", migration.Version, migration.Name)
			return nil
		}),
	)
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("migrating: %w", err)
	}

	// Migration 0002 ran outside a transaction, as its sgl.migration directive asks.
	var journalMode string
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return fmt.Errorf("reading the journal mode: %w", err)
	}
	fmt.Fprintf(output, "journal mode: %s\n", journalMode)

	// The default namespace is a separate history in the same database, and it has recorded
	// nothing.
	if err := printOtherHistory(ctx, database, files, output); err != nil {
		return err
	}

	// A Seeder applies each seed file once and records it in the seed table, so calling
	// Apply at every start-up is safe. The seed file also says ON CONFLICT DO NOTHING, which
	// keeps it harmless if the seed history is ever cleared.
	seeder, err := migrate.NewSeeder(database, backend, files, "db/seeds")
	if err != nil {
		return err
	}
	for range 2 {
		seeded, err := seeder.Apply(ctx)
		if err != nil {
			return fmt.Errorf("seeding: %w", err)
		}
		fmt.Fprintf(output, "seeds applied: %d\n", seeded)
	}

	menu, err := store.New(database).ListMenu(ctx)
	if err != nil {
		return fmt.Errorf("listing the menu: %w", err)
	}
	for _, dish := range menu {
		fmt.Fprintf(output, "  %-17s %dp\n", dish.Dish, dish.PricePence)
	}
	return nil
}

// printOtherHistory reads the same migration files against the default namespace's
// history, which this program never wrote to.
//
// Takes database (*sql.DB) which is the migrated database.
// Takes files (fs.FS) which holds the migration files.
// Takes output (io.Writer) which receives the count.
//
// Returns error when the status cannot be read.
func printOtherHistory(ctx context.Context, database *sql.DB, files fs.FS, output io.Writer) error {
	backend := migrate_backend_sqlite.New()
	migrator, err := migrate.NewMigrator(database, backend, files, "db/migrations")
	if err != nil {
		return err
	}
	statuses, err := migrator.Status(ctx)
	if err != nil {
		return fmt.Errorf("reading the default history: %w", err)
	}
	applied := 0
	for _, status := range statuses {
		if status.Applied {
			applied++
		}
	}
	fmt.Fprintf(output, "%s (default namespace): %d of %d migrations applied\n", backend.Names().History, applied, len(statuses))
	return nil
}
