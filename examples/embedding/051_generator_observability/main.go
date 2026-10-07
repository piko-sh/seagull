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

// Command 051_generator_observability generates this example's store package and migrates
// a SQLite database with an observer and a logger attached, printing every stage both run
// and the trace log they write.
//
// Run it with `go run .`, or `go run . -db tasks.db` to keep the database.
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

// project holds the migration and query files, compiled into the binary.
//
//go:embed db
var project embed.FS

func main() {
	path := flag.String("db", "", "SQLite database file; a temporary one when empty")
	flag.Parse()
	if err := execute(context.Background(), *path); err != nil {
		log.Fatal(err)
	}
}

// execute opens the database at path, or a temporary one when path is empty, and runs the
// example against it.
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
	return run(ctx, database, project, os.Stdout)
}
