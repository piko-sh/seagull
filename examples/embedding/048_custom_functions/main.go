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

// Command 048_custom_functions registers slugify, a Go function, with the SQLite driver,
// then runs the example in example.go: Seagull types the queries that call slugify once
// it is declared, and the generated methods run it inside SQLite.
//
// Run it with `go run .`, or `go run . -db articles.db` to keep the database.
package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	sqlitedriver "modernc.org/sqlite"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
)

// project holds the migration and query files, compiled into the binary.
//
//go:embed db
var project embed.FS

// init registers slugify with the driver. Every connection opened afterwards can call it,
// so it runs before main and before any test opens a database.
func init() {
	sqlitedriver.MustRegisterDeterministicScalarFunction("slugify", 1, callSlugify)
}

// callSlugify adapts slugify to the driver's calling convention: NULL gives NULL, and
// text gives its slug.
//
// Takes arguments ([]driver.Value) which holds the one SQL argument.
//
// Returns driver.Value which is the slug, or nil for NULL.
// Returns error when the argument is not text.
func callSlugify(_ *sqlitedriver.FunctionContext, arguments []driver.Value) (driver.Value, error) {
	switch value := arguments[0].(type) {
	case nil:
		return nil, nil
	case string:
		return slugify(value), nil
	default:
		return nil, fmt.Errorf("slugify takes text, not %T", value)
	}
}

func main() {
	path := flag.String("db", "", "SQLite database file; a temporary one when empty")
	flag.Parse()
	if err := execute(context.Background(), *path); err != nil {
		log.Fatal(err)
	}
}

// execute opens and migrates the database at path, or a temporary one when path is empty,
// and runs the example against it.
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
	database, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), project, "db/migrations")
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("migrating: %w", err)
	}
	return run(ctx, database, project, os.Stdout)
}
