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

// Command 050_schema_from_live_database generates this example's store package against a
// live SQLite database instead of migration files, as a project with an existing database
// and no migrations would.
//
// Run it with `go run . -db club.db` against a database of your own, or with `go run .`
// to build one from legacy/schema.sql in a temporary directory first.
package main

import (
	"context"
	"database/sql"
	_ "embed"
	"flag"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// legacySchema is the schema of the club's existing database, which predates any
// migration files.
//
//go:embed legacy/schema.sql
var legacySchema string

func main() {
	path := flag.String("db", "", "SQLite database to read the schema from; a new one from legacy/schema.sql when empty")
	flag.Parse()
	if err := execute(context.Background(), *path); err != nil {
		log.Fatal(err)
	}
}

// execute opens the database at path, or builds a temporary one from the legacy schema
// when path is empty, and runs the example against it.
//
// Takes path (string) which is the database file, or empty.
//
// Returns error when the database cannot be opened or the example fails.
func execute(ctx context.Context, path string) error {
	create := path == ""
	if create {
		directory, err := os.MkdirTemp("", "seagull-example-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(directory) }()
		path = filepath.Join(directory, "club.db")
	}
	database, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	if create {
		if _, err := database.ExecContext(ctx, legacySchema); err != nil {
			return err
		}
	}
	return run(ctx, database, os.DirFS("."), os.Stdout)
}
