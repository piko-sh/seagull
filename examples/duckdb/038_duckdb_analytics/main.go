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

// Command 038_duckdb_analytics applies its migrations to an in-memory DuckDB database,
// writes the fish market's price list to a Parquet file with COPY ... TO, then runs the
// example in example.go: a week of landings is analysed with QUALIFY, FILTER, macros and
// read_parquet.
//
// Run it with `go run .`, or `go run . -db example.duckdb` to keep the database in a
// file.
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

	_ "github.com/duckdb/duckdb-go/v2"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_duckdb"
)

// priceListName is the Parquet file the auction house's prices are written to.
const priceListName = "auction-prices.parquet"

// migrations holds the migration files, compiled into the binary.
//
//go:embed db/migrations
var migrations embed.FS

func main() {
	path := flag.String("db", "", "DuckDB database file; an in-memory database when empty")
	flag.Parse()
	if err := execute(context.Background(), *path); err != nil {
		log.Fatal(err)
	}
}

// execute opens and migrates the DuckDB database at path, or an in-memory one when path
// is empty, writes the price list to a temporary directory, and runs the example.
//
// Takes path (string) which is the database file, or empty.
//
// Returns error when the database or the price list cannot be prepared or the example
// fails.
func execute(ctx context.Context, path string) error {
	database, err := sql.Open("duckdb", path)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	migrator, err := migrate.NewMigrator(database, migrate_backend_duckdb.New(), migrations, "db/migrations")
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("migrating: %w", err)
	}

	directory, err := os.MkdirTemp("", "seagull-example-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	priceList := filepath.Join(directory, priceListName)
	if err := writePriceList(ctx, database, priceList); err != nil {
		return fmt.Errorf("writing the price list: %w", err)
	}
	return run(ctx, database, priceList, os.Stdout)
}

// writePriceList writes the auction house's prices, in pence per kilogram, to a Parquet
// file with DuckDB's COPY ... TO. The file name is a bound parameter, like any other
// value, so the path needs no quoting.
//
// Takes database (*sql.DB) which runs the COPY.
// Takes path (string) which is the Parquet file to write.
//
// Returns error when the file cannot be written.
func writePriceList(ctx context.Context, database *sql.DB, path string) error {
	_, err := database.ExecContext(ctx, `COPY (
    SELECT * FROM (VALUES ('cod', 900), ('hake', 650), ('mackerel', 350)) AS prices (species, pence_per_kg)
) TO $1 (FORMAT parquet)`, path)
	return err
}
