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

// Package emit_driver_pgx is the emitter for pgx v5's native interfaces.
//
// The code it generates calls pgx directly rather than through database/sql: Exec, Query
// and QueryRow, with SendBatch for batches and CopyFrom for bulk loads. Seagull does not
// import pgx itself; the generated source refers to pgx's types, so the application's
// module requires github.com/jackc/pgx/v5. [New] returns the emitter as an
// emit.EmitterPort for seagull.WithEmitter.
//
// # Choosing it
//
// Choose it when the application talks to pgx directly, for a dialect that runs on pgx,
// such as PostgreSQL, CockroachDB or TimescaleDB. For the standard database/sql
// interfaces, which work with every dialect, choose emit_driver_databasesql, and for
// clickhouse-go v2's native interface choose emit_driver_clickhouse. In sgl.yaml it is
// go.driver: pgx.
//
// # Generated code
//
// The generated DBTX is satisfied by a *pgxpool.Pool, a *pgx.Conn or a pgx.Tx. The
// querier offers New, WithTx and RunInTx; it has no NewWithReplica and no prepared.go,
// since pgx keeps its own statement cache. A batch query sends one statement per item in
// a pgx.Batch, and a copyfrom query uses COPY. A slice parameter binds as one array
// argument, its IN list written as = ANY($1), so pgx caches one prepared statement per
// query whatever the slice's length. An execresult method returns a pgconn.CommandTag.
//
// # Thread safety
//
// The emitter New returns holds no state, so it is safe for concurrent use by multiple
// goroutines.
//
// # Usage
//
//	emitter := emit_driver_pgx.New()
//	generator, err := seagull.New(engine, seagull.WithEmitter(emitter))
//	if err != nil {
//	    return err
//	}
package emit_driver_pgx
