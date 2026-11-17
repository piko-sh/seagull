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

package harness

import "fmt"

// nativePGXFactory binds real pgx connections and transactions to generated methods.
const nativePGXFactory = `func(ctx context.Context, connection string) (*fuzznative.Session, error) {
 database, err := pgx.Connect(ctx, connection)
 if err != nil { return nil, err }
 queries := generated.New(database)
 return &fuzznative.Session{Queries: queries, Native: true,
  Close: func() error { return database.Close(ctx) },
  Begin: func(ctx context.Context) (*fuzznative.Transaction, error) {
   transaction, err := database.Begin(ctx)
   if err != nil { return nil, err }
   return &fuzznative.Transaction{Queries: queries.WithTx(transaction), Commit: transaction.Commit, Rollback: transaction.Rollback}, nil
  }}, nil
}`

// nativeClickHouseFactory binds the actual native ClickHouse connection interface.
const nativeClickHouseFactory = `func(ctx context.Context, connection string) (*fuzznative.Session, error) {
 options, err := clickhouse.ParseDSN(connection)
 if err != nil { return nil, err }
 options.MaxOpenConns = 1
 database, err := clickhouse.Open(options)
 if err != nil { return nil, err }
 return &fuzznative.Session{Queries: generated.New(database), Native: true, Close: database.Close}, nil
}`

// nativeMain constructs a standalone helper using actual generated driver methods.
//
// Takes engine (string) which selects independent native semantics.
// Takes driver (string) which names the linked database/sql driver.
// Takes shape (int) which selects preparation and the dedicated emitter.
//
// Returns string which compiles with actual generated files and pinned dependencies.
func nativeMain(engine, driver string, shape int) string {
	imports := `"database/sql"; "errors"`
	factory := nativeSQLFactory(engine, driver, shape%nativeDriverShape >= nativePreparedShape)
	if shape >= nativeDriverShape {
		if engine == engineClickHouse {
			imports, factory = `"github.com/ClickHouse/clickhouse-go/v2"`, nativeClickHouseFactory
		} else {
			imports, factory = `"github.com/jackc/pgx/v5"`, nativePGXFactory
		}
	}
	return fmt.Sprintf(`package main
import (
 "context"
 "fmt"
 "os"
 %s
 "seagull.sh/seagull/nativefuzz/generated"
 "seagull.sh/seagull/internal/testkit/fuzznative"
)
func main() {
 factory := %s
 if err := fuzznative.Serve(%q, factory, os.Stdin, os.Stdout); err != nil {
  fmt.Fprintln(os.Stderr, err)
  os.Exit(1)
 }
}
`, imports, factory, engine)
}

// nativeSQLFactory opens the actual database/sql profile and generated constructors.
//
// Takes engine (string) which selects native transaction support.
// Takes driver (string) which names the registered native driver.
// Takes prepared (bool) which enables explicit generated statement preparation.
//
// Returns string which owns native resources and binds real generated transactions.
func nativeSQLFactory(engine, driver string, prepared bool) string {
	setup := "queries := generated.New(database)"
	closeQueries := "nil"
	if prepared {
		setup = `statements, err := generated.Prepare(ctx, database)
  if err != nil { _ = database.Close(); return nil, err }
  queries := generated.New(statements)`
		closeQueries = "statements.Close()"
	}
	begin := "nil"
	if engine != engineClickHouse {
		begin = `func(ctx context.Context) (*fuzznative.Transaction, error) {
   transaction, err := database.BeginTx(ctx, nil)
   if err != nil { return nil, err }
   return &fuzznative.Transaction{Queries: queries.WithTx(transaction),
    Commit: func(context.Context) error { return transaction.Commit() },
    Rollback: func(context.Context) error { return transaction.Rollback() }}, nil
  }`
	}
	return fmt.Sprintf(`func(ctx context.Context, connection string) (*fuzznative.Session, error) {
  database, err := sql.Open(%q, connection)
  if err != nil { return nil, err }
  database.SetMaxOpenConns(1)
  %s
  return &fuzznative.Session{Queries: queries, Prepared: %t,
   Close: func() error { return errors.Join(%s, database.Close()) }, Begin: %s}, nil
 }`, driver, setup, prepared, closeQueries, begin)
}
