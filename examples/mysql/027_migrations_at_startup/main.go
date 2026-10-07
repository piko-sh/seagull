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

// Command 027_migrations_at_startup connects to a MySQL database and runs the example in
// example.go, which applies the migrations embedded in the binary each time the program
// starts, then uses the schema they built.
//
// Run it with `go run . -dsn 'user:password@tcp(localhost:3306)/harbour'`, or set
// DATABASE_URL instead of passing -dsn. A second run against the same database finds
// nothing to migrate.
package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"flag"
	"log"
	"os"

	"github.com/go-sql-driver/mysql"
)

// migrations holds the migration files, compiled into the binary, so the program needs no
// files beside it to bring a database up to date.
//
//go:embed db/migrations
var migrations embed.FS

func main() {
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"), "MySQL data source name; defaults to $DATABASE_URL")
	flag.Parse()
	if err := execute(context.Background(), *dsn); err != nil {
		log.Fatal(err)
	}
}

// execute opens the database dsn names and runs the example against it. The example
// migrates the database itself.
//
// Takes dsn (string) which is the database's data source name.
//
// Returns error when the database cannot be opened or the example fails.
func execute(ctx context.Context, dsn string) error {
	if dsn == "" {
		return errors.New("set -dsn or DATABASE_URL to a MySQL data source name")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return err
	}
	// Seagull maps DATETIME and TIMESTAMP columns to time.Time, which the driver returns
	// only when parseTime is set.
	config.ParseTime = true

	database, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	return run(ctx, database, migrations, os.Stdout)
}
