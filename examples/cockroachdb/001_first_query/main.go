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

// Command 001_first_query applies its migrations to a CockroachDB database, then runs the
// example in example.go: two tasks are added, one is completed, and both are listed.
//
// Run it with `go run .` against a local insecure node, which `docker run -d -p
// 26257:26257 cockroachdb/cockroach:latest-v25.1 start-single-node --insecure` starts, or
// point it elsewhere with `go run . -dsn <connection string>` or DATABASE_URL.
package main

import (
	"cmp"
	"context"
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_cockroachdb"
)

// localDSN reaches an insecure single node on this machine.
const localDSN = "postgresql://root@localhost:26257/defaultdb?sslmode=disable"

// migrations holds the migration files, compiled into the binary.
//
//go:embed db/migrations
var migrations embed.FS

func main() {
	dsn := flag.String("dsn", cmp.Or(os.Getenv("DATABASE_URL"), localDSN), "CockroachDB connection string")
	flag.Parse()
	if err := execute(context.Background(), *dsn); err != nil {
		log.Fatal(err)
	}
}

// execute connects to the database at dsn, migrates it with the CockroachDB backend and
// runs the example against it.
//
// Takes dsn (string) which is the connection string.
//
// Returns error when the database cannot be prepared or the example fails.
func execute(ctx context.Context, dsn string) error {
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	migrator, err := migrate.NewMigrator(database, migrate_backend_cockroachdb.New(), migrations, "db/migrations")
	if err != nil {
		return err
	}
	if _, err := migrator.Up(ctx); err != nil {
		return fmt.Errorf("migrating: %w", err)
	}
	return run(ctx, database, os.Stdout)
}
