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

// Command 028_migration_lifecycle opens a SQLite database and runs the example in
// example.go, which takes the embedded migrations through UpTo, Up, Down, Status and
// Validate, including a migration with no down file and an applied file edited later.
//
// Run it with `go run .`, or `go run . -db birdwatch.db` to keep the database; a second
// run against the same file starts from where the first one left it.
package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
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

// execute opens the database at path, or a temporary one when path is empty, and runs the
// example against it. The example migrates the database itself.
//
// Takes path (string) which is the database file, or empty.
//
// Returns error when the database cannot be opened or the example fails.
func execute(ctx context.Context, path string) error {
	if path == "" {
		directory, err := os.MkdirTemp("", "seagull-example-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(directory) }()
		path = filepath.Join(directory, "example.db")
	}
	database, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	return run(ctx, database, migrations, os.Stdout)
}
