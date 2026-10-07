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

// Command 024_read_replicas applies its migrations to a SQLite database, opens a second,
// read-only connection to the same file to stand in for a replica, then runs the example
// in example.go: writes go to the writer, reads to the reader.
//
// Run it with `go run .`, or `go run . -db sightings.db` to keep the database.
package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
)

// migrations holds the migration files, compiled into the binary.
//
//go:embed db/migrations
var migrations embed.FS

func main() {
	path := flag.String("db", "", "SQLite database file; a temporary one when empty")
	flag.Parse()
	if err := execute(context.Background(), *path); err != nil {
		log.Fatal(err)
	}
}

// execute opens and migrates the database at path, or a temporary one when path is empty,
// opens a read-only connection to it, and runs the example against both.
//
// Takes path (string) which is the database file, or empty.
//
// Returns error when the database cannot be prepared or the example fails.
func execute(ctx context.Context, path string) error {
	if path == "" {
		directory, err := os.MkdirTemp("", "seagull-example-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(directory) }()
		path = filepath.Join(directory, "example.db")
	}
	writer, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return err
	}
	defer func() { _ = writer.Close() }()

	migrator, err := migrate.NewMigrator(writer, migrate_backend_sqlite.New(), migrations, "db/migrations")
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("migrating: %w", err)
	}

	reader, err := openReader(path)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	return run(ctx, writer, reader, os.Stdout)
}

// openReader opens a read-only connection to the SQLite file at path. mode=ro makes
// SQLite refuse every write on it, as a replica would.
//
// Takes path (string) which is the database file.
//
// Returns *sql.DB which is the read-only connection.
// Returns error when the connection cannot be opened.
func openReader(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+path+"?mode=ro")
}
