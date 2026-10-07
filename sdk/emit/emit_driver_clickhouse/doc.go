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

// Package emit_driver_clickhouse is the emitter for clickhouse-go v2's native interface.
//
// The code it generates calls the native driver's driver.Conn rather than database/sql:
// Exec, Query and QueryRow, with PrepareBatch for bulk writes. Seagull does not import
// clickhouse-go itself; the generated source refers to its types, so the application's
// module requires github.com/ClickHouse/clickhouse-go/v2. [New] returns the emitter as an
// emit.EmitterPort for seagull.WithEmitter.
//
// # Choosing it
//
// Choose it when the application uses ClickHouse's native driver, the high-throughput
// counterpart to its database/sql driver. For the standard database/sql interfaces, which
// work with every dialect, ClickHouse included, choose emit_driver_databasesql, and for
// pgx v5's native interfaces choose emit_driver_pgx. In sgl.yaml it is go.driver:
// clickhouse.
//
// # Generated code
//
// The generated DBTX is satisfied by a driver.Conn. The querier offers New alone: the
// native connection has no interactive transactions, so there is no WithTx or RunInTx,
// and there is no NewWithReplica and no prepared.go. Each {name:Type} placeholder binds
// by name, in the form the driver requires. A copyfrom query, and a batch query whose
// VALUES row is one placeholder per column, go through the native columnar path,
// PrepareBatch, Append and Send; any other batch is sent as a multi-row INSERT. A slice
// parameter binds as one Array. The native Exec returns only an error, so execrows and
// execresult are refused at generation time.
//
// # Thread safety
//
// The emitter New returns holds no state, so it is safe for concurrent use by multiple
// goroutines.
//
// # Usage
//
//	emitter := emit_driver_clickhouse.New()
//	generator, err := seagull.New(engine, seagull.WithEmitter(emitter))
//	if err != nil {
//	    return err
//	}
package emit_driver_clickhouse
