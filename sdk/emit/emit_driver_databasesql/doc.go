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

// Package emit_driver_databasesql is the emitter for the standard database/sql
// interfaces, and the generator's default.
//
// The code it generates runs every query through database/sql's ExecContext, QueryContext
// and QueryRowContext, so it works with every dialect and any database/sql driver the
// application links. [New] returns the emitter as an emit.EmitterPort for
// seagull.WithEmitter; a generator given no emitter uses one built with no options.
//
// # Choosing it
//
// Choose it for a portable code base, and for a database whose driver has no native
// emitter, such as SQLite, MySQL, MariaDB or DuckDB. For code written against pgx v5's
// native interfaces choose emit_driver_pgx, and for clickhouse-go v2's native interface
// choose emit_driver_clickhouse. In sgl.yaml it is go.driver: database/sql.
//
// # Generated code
//
// The generated DBTX is satisfied by a *sql.DB, a *sql.Tx, a *sql.Conn or a PreparedDBTX.
// The querier offers New, NewWithReplica, which routes read-only queries to a reader,
// WithTx and RunInTx. A batch query is sent as a multi-row INSERT, or as one statement
// per item when a clause follows its VALUES row, and a copyfrom query as a multi-row
// INSERT. A slice parameter's IN list is expanded to one placeholder per element, or
// bound as one Array on ClickHouse.
//
// How values are bound and scanned follows the profile of the driver behind database/sql:
// the first of the dialect's drivers the emitter knows, or the one [WithDriver] names.
// Unless [WithPrepared] turns it off, the package also gets prepared.go, a PreparedDBTX
// that prepares every static query once and reuses the statements; [PreparesStatements]
// says which drivers can have it. The sgl command writes it only when go.prepared is set.
//
// # Thread safety
//
// The emitter New returns keeps only the options it was given and never changes them, so
// it is safe for concurrent use by multiple goroutines.
//
// # Usage
//
//	emitter := emit_driver_databasesql.New(emit_driver_databasesql.WithPrepared(false))
//	generator, err := seagull.New(engine, seagull.WithEmitter(emitter))
//	if err != nil {
//	    return err
//	}
package emit_driver_databasesql
