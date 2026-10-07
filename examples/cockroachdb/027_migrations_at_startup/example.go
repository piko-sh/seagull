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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"

	"seagull.sh/seagull/examples/cockroachdb/027_migrations_at_startup/internal/store"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_cockroachdb"
)

// run deploys the program against an empty database the way an orchestrator does,
// starting several replicas at the same moment, each of which migrates before it works.
// Then one replica restarts and finds nothing to migrate.
//
// Takes database (*sql.DB) which is the database to migrate, empty or not.
// Takes migrations (fs.FS) which holds db/migrations, the embed.FS in main.go.
// Takes output (io.Writer) which receives what each start did.
//
// Returns error when migrating or a query fails.
func run(ctx context.Context, database *sql.DB, migrations fs.FS, output io.Writer) error {
	replicas := []string{"Kittiwake", "Puffin", "Gannet"}
	applied, err := deploy(ctx, database, migrations, replicas)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "%d replicas started at once and applied %d migrations between them\n", len(replicas), applied)

	restarted, err := start(ctx, database, migrations, "Cormorant")
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "a replica restarted and applied %d migrations\n", restarted)

	if err := printBoats(ctx, database, output); err != nil {
		return err
	}
	if err := refuseAdvisoryLock(database, migrations, output); err != nil {
		return err
	}
	return printHistory(ctx, database, migrations, output)
}

// deploy starts one replica per boat at the same moment and waits for them all.
//
// The replicas here share this process's connection pool, where real replicas are
// separate processes with pools of their own. The lock lives in the database, so it
// serialises them the same way.
//
// Takes database (*sql.DB) which is the database.
// Takes migrations (fs.FS) which holds the migration files.
// Takes boats ([]string) which names the boat each replica adds.
//
// Returns int which is how many migrations the replicas applied between them.
// Returns error which joins every replica's failure.
func deploy(ctx context.Context, database *sql.DB, migrations fs.FS, boats []string) (int, error) {
	applied := make([]int, len(boats))
	failures := make([]error, len(boats))
	var replicas sync.WaitGroup
	for index, boat := range boats {
		replicas.Go(func() {
			applied[index], failures[index] = start(ctx, database, migrations, boat)
		})
	}
	replicas.Wait()

	total := 0
	for _, count := range applied {
		total += count
	}
	return total, errors.Join(failures...)
}

// start is what the program does each time it starts: migrate first, then work. Here the
// work is adding a boat.
//
// Takes database (*sql.DB) which is the database.
// Takes migrations (fs.FS) which holds the migration files.
// Takes boat (string) which names the boat to add.
//
// Returns int which is how many migrations this start applied.
// Returns error when migrating or the query fails.
func start(ctx context.Context, database *sql.DB, migrations fs.FS, boat string) (int, error) {
	// migrate_backend_cockroachdb.New() is the PostgreSQL backend with a lock table in place
	// of an advisory lock, which CockroachDB does not have. Up creates sgl_migration_lock if
	// it is missing and holds its one row with SELECT ... FOR UPDATE in an open transaction,
	// so a second replica waits on that row until the first commits.
	//
	// By default CockroachDB keeps a SERIALIZABLE transaction's FOR UPDATE locks in memory
	// and drops them when a range splits or moves, which a schema change such as 0002's ADD
	// COLUMN ... DEFAULT can cause. The backend turns on
	// enable_durable_locking_for_serializable for its lock transaction, so the lock holds
	// until the migrations are done; that needs CockroachDB 23.2 or later.
	//
	// The migrations run on a second connection, one statement at a time and not in a
	// transaction: CockroachDB cannot roll a schema change back as part of a larger
	// transaction, so the backend records each finished statement instead. A file that fails
	// partway is marked dirty and resumes from the statement that failed once the cause is
	// fixed. Keeping one schema change per file keeps each step its own record.
	migrator, err := migrate.NewMigrator(database, migrate_backend_cockroachdb.New(), migrations, "db/migrations")
	if err != nil {
		return 0, err
	}

	// Up applies every migration not yet in the history and returns how many it applied. It
	// takes the lock only when something is pending, and reads the history again once it
	// holds the lock, so a replica that waited finds the work done.
	applied, err := migrator.Up(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrating: %w", err)
	}

	queries := store.New(database)
	if err := queries.AddBoat(ctx, store.AddBoatParams{Name: boat, HomePort: "Padstow"}); err != nil {
		return 0, fmt.Errorf("adding %s: %w", boat, err)
	}
	return applied, nil
}

// printBoats lists the harbour, which every replica wrote to once the schema existed.
//
// Takes database (*sql.DB) which is the migrated database.
// Takes output (io.Writer) which receives the list.
//
// Returns error when the query fails.
func printBoats(ctx context.Context, database *sql.DB, output io.Writer) error {
	boats, err := store.New(database).ListBoats(ctx)
	if err != nil {
		return fmt.Errorf("listing boats: %w", err)
	}
	for _, row := range boats {
		fmt.Fprintf(output, "  %-9s of %s\n", row.Name, row.HomePort)
	}
	return nil
}

// refuseAdvisoryLock shows that the CockroachDB backend rejects an advisory lock, as a
// configuration copied from PostgreSQL might ask for, before it touches the database.
//
// Takes database (*sql.DB) which is the database.
// Takes migrations (fs.FS) which holds the migration files.
// Takes output (io.Writer) which receives the refusal.
//
// Returns error when the backend accepts the advisory lock.
func refuseAdvisoryLock(database *sql.DB, migrations fs.FS, output io.Writer) error {
	backend := migrate_backend_cockroachdb.New(migrate.WithLock(migrate.LockAdvisory))
	_, err := migrate.NewMigrator(database, backend, migrations, "db/migrations")
	if err == nil {
		return errors.New("an advisory lock on CockroachDB gave no error, want migrate.ErrUnsupportedLock")
	}
	if !errors.Is(err, migrate.ErrUnsupportedLock) {
		return fmt.Errorf("an advisory lock on CockroachDB gave %w, want migrate.ErrUnsupportedLock", err)
	}
	fmt.Fprintf(output, "advisory lock refused: %v\n", err)
	return nil
}

// printHistory prints the migration history the migrator keeps in the database, and the
// table its lock uses.
//
// Takes database (*sql.DB) which is the migrated database.
// Takes migrations (fs.FS) which holds the migration files.
// Takes output (io.Writer) which receives the history.
//
// Returns error when the history cannot be read.
func printHistory(ctx context.Context, database *sql.DB, migrations fs.FS, output io.Writer) error {
	backend := migrate_backend_cockroachdb.New()
	migrator, err := migrate.NewMigrator(database, backend, migrations, "db/migrations")
	if err != nil {
		return err
	}
	statuses, err := migrator.Status(ctx)
	if err != nil {
		return fmt.Errorf("reading migration status: %w", err)
	}
	names := backend.Names()
	fmt.Fprintf(output, "history in %s, locked through %s:\n", names.History, names.MigrationLock)
	for _, status := range statuses {
		fmt.Fprintf(output, "  %04d %-15s applied=%t\n", status.Version, status.Name, status.Applied)
	}
	return nil
}
