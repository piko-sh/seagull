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
	"testing/fstest"

	"seagull.sh/seagull/examples/sqlite/028_migration_lifecycle/internal/store"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
)

const (
	// migrationsDirectory is where the migration files sit inside the file system.
	migrationsDirectory = "db/migrations"

	// editedFile is the applied migration the example edits to break its checksum.
	editedFile = migrationsDirectory + "/0001_sightings.up.sql"
)

// run takes a database through a migration's life: status, up to a version, up, down, a
// down that cannot happen, and validation before and after an applied file is edited.
//
// Takes database (*sql.DB) which is an empty database.
// Takes migrations (fs.FS) which holds db/migrations, the embed.FS in main.go.
// Takes output (io.Writer) which receives each step's result.
//
// Returns error when a step fails other than as the example expects.
func run(ctx context.Context, database *sql.DB, migrations fs.FS, output io.Writer) error {
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), migrations, migrationsDirectory)
	if err != nil {
		return err
	}

	// Status reads the files and the history without changing anything, so it works on a
	// database that has never been migrated.
	fmt.Fprintln(output, "before migrating:")
	if err := printStatus(ctx, migrator, output); err != nil {
		return err
	}

	// UpTo applies pending migrations up to and including a version.
	applied, err := migrator.UpTo(ctx, 1)
	if err != nil {
		return fmt.Errorf("migrating to version 1: %w", err)
	}
	fmt.Fprintf(output, "UpTo(1) applied %d\n", applied)
	if err := recordSightings(ctx, store.New(database)); err != nil {
		return err
	}

	// Up applies the rest: 0002 tidies the names recorded above, 0003 adds a column.
	applied, err = migrator.Up(ctx)
	if err != nil {
		return fmt.Errorf("migrating up: %w", err)
	}
	fmt.Fprintf(output, "Up applied %d\n", applied)
	if err := printSightings(ctx, store.New(database), output); err != nil {
		return err
	}

	// Down rolls back the newest applied migrations by running their down files.
	rolledBack, err := migrator.Down(ctx, 1)
	if err != nil {
		return fmt.Errorf("rolling back: %w", err)
	}
	fmt.Fprintf(output, "Down(1) rolled back %d\n", rolledBack)

	// The next one down, 0002, has no down file, so Down stops with an error and leaves it
	// applied. Roll forward with a new migration instead.
	_, err = migrator.Down(ctx, 1)
	if err == nil {
		return errors.New("rolling back 0002: want a NoDownMigrationError, got no error")
	}
	noDown, isNoDown := errors.AsType[*migrate.NoDownMigrationError](err)
	if !isNoDown {
		return fmt.Errorf("rolling back 0002: want a NoDownMigrationError, got %w", err)
	}
	fmt.Fprintf(output, "Down(1) refused: migration %d has no down file\n", noDown.Version)

	fmt.Fprintln(output, "after rolling back:")
	if err := printStatus(ctx, migrator, output); err != nil {
		return err
	}
	return validate(ctx, database, migrations, output)
}

// validate checks the recorded checksums against the files, then edits an applied
// migration in a copy of the files and shows that Validate and Up both refuse it.
//
// Takes database (*sql.DB) which is the migrated database.
// Takes migrations (fs.FS) which holds the migration files.
// Takes output (io.Writer) which receives the results.
//
// Returns error when validation does not behave as the example expects.
func validate(ctx context.Context, database *sql.DB, migrations fs.FS, output io.Writer) error {
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), migrations, migrationsDirectory)
	if err != nil {
		return err
	}
	if err := migrator.Validate(ctx); err != nil {
		return fmt.Errorf("validating: %w", err)
	}
	fmt.Fprintln(output, "Validate: every applied migration matches its file")

	// Each applied migration's SHA-256 is recorded in the history. Changing an applied file,
	// even only a comment, makes Validate and Up fail: the database no longer matches what
	// the files say it should be.
	edited, err := editedCopy(migrations, editedFile, "-- tidied up after it was applied\n")
	if err != nil {
		return err
	}
	drifted, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), edited, migrationsDirectory)
	if err != nil {
		return err
	}
	err = drifted.Validate(ctx)
	if err == nil {
		return errors.New("validating the edited files: want a ChecksumMismatchError, got no error")
	}
	mismatch, isMismatch := errors.AsType[*migrate.ChecksumMismatchError](err)
	if !isMismatch {
		return fmt.Errorf("validating the edited files: want a ChecksumMismatchError, got %w", err)
	}
	fmt.Fprintf(output, "Validate after editing %04d_%s: checksum mismatch\n", mismatch.Version, mismatch.Name)
	if err := printStatus(ctx, drifted, output); err != nil {
		return err
	}
	_, err = drifted.Up(ctx)
	if err == nil {
		return errors.New("migrating the edited files: want a ChecksumMismatchError, got no error")
	}
	if _, isMismatch := errors.AsType[*migrate.ChecksumMismatchError](err); !isMismatch {
		return fmt.Errorf("migrating the edited files: want a ChecksumMismatchError, got %w", err)
	}
	fmt.Fprintln(output, "Up after editing: refused for the same reason")
	return nil
}

// printStatus writes one line per migration: whether it is applied, whether an applied
// file still matches its recorded checksum, and whether it has a down file.
//
// Takes migrator (*migrate.Migrator) which reads the status.
// Takes output (io.Writer) which receives the table.
//
// Returns error when the status cannot be read.
func printStatus(ctx context.Context, migrator *migrate.Migrator, output io.Writer) error {
	statuses, err := migrator.Status(ctx)
	if err != nil {
		return fmt.Errorf("reading status: %w", err)
	}
	for _, status := range statuses {
		state := "pending"
		if status.Applied {
			state = "applied"
		}
		down := "no down file"
		if status.HasDownMigration {
			down = "down file"
		}
		edited := ""
		if status.Applied && !status.ChecksumMatch {
			edited = ", edited since it was applied"
		}
		fmt.Fprintf(output, "  %04d %-15s %-8s %s%s\n", status.Version, status.Name, state, down, edited)
	}
	return nil
}

// recordSightings adds two sightings whose species names need tidying.
//
// Takes queries (*store.Queries) which add the rows.
//
// Returns error when a row cannot be added.
func recordSightings(ctx context.Context, queries *store.Queries) error {
	sightings := []store.RecordSightingParams{
		{Species: "  Herring Gull", Place: "the pier"},
		{Species: "KITTIWAKE ", Place: "the harbour wall"},
	}
	for _, sighting := range sightings {
		if err := queries.RecordSighting(ctx, sighting); err != nil {
			return fmt.Errorf("recording a sighting: %w", err)
		}
	}
	return nil
}

// printSightings writes every sighting, quoting the species to show its spacing.
//
// Takes queries (*store.Queries) which read the rows.
// Takes output (io.Writer) which receives them.
//
// Returns error when the rows cannot be read.
func printSightings(ctx context.Context, queries *store.Queries, output io.Writer) error {
	sightings, err := queries.ListSightings(ctx)
	if err != nil {
		return fmt.Errorf("listing sightings: %w", err)
	}
	for _, sighting := range sightings {
		fmt.Fprintf(output, "  sighting %d: %q at %s\n", sighting.ID, sighting.Species, sighting.Place)
	}
	return nil
}

// editedCopy copies every file in files into memory and puts a line at the top of one of
// them, as if someone had edited it.
//
// Takes files (fs.FS) which holds the originals.
// Takes name (string) which is the path of the file to edit.
// Takes line (string) which is put at the top of it.
//
// Returns fstest.MapFS which holds the copy.
// Returns error when a file cannot be read or name is missing.
func editedCopy(files fs.FS, name, line string) (fstest.MapFS, error) {
	copied := fstest.MapFS{}
	err := fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(files, path)
		if err != nil {
			return err
		}
		copied[path] = &fstest.MapFile{Data: data}
		return nil
	})
	if err != nil {
		return nil, err
	}
	original, ok := copied[name]
	if !ok {
		return nil, fmt.Errorf("no file %s to edit", name)
	}
	original.Data = append([]byte(line), original.Data...)
	return copied, nil
}
