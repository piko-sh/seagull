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

// Command 027_migrations_at_startup connects to a CockroachDB database and runs the
// example in example.go, which starts several replicas at once, each applying the
// migrations embedded in the binary before it works, then restarts one.
//
// Run it with `go run .` against a local insecure node, which `docker run -d -p
// 26257:26257 cockroachdb/cockroach:latest-v25.1 start-single-node --insecure` starts, or
// point it elsewhere with `go run . -dsn <connection string>` or DATABASE_URL. The
// migrations run against an empty database; a second run finds nothing to migrate.
package main

import (
	"cmp"
	"context"
	"embed"
	"flag"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// localDSN reaches an insecure single node on this machine.
const localDSN = "postgresql://root@localhost:26257/defaultdb?sslmode=disable"

// migrations holds the migration files, compiled into the binary, so the program needs no
// files beside it to bring a database up to date.
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

// execute connects to the database at dsn and runs the example against it. The example
// migrates the database itself.
//
// Takes dsn (string) which is the connection string.
//
// Returns error when the connection string is invalid or the example fails.
func execute(ctx context.Context, dsn string) error {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return err
	}
	database := stdlib.OpenDB(*config)
	defer func() { _ = database.Close() }()
	return run(ctx, database, migrations, os.Stdout)
}
