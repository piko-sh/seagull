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

// Package dialect_engine_cockroachdb is the dialect for CockroachDB.
//
// It reads CockroachDB's DDL and queries with CockroachDB's types, such as STRING, BYTES
// and its 64-bit INT, its built-in functions, UPSERT, hidden rowid and hash-shard
// columns, NOT VISIBLE and computed columns, AS OF SYSTEM TIME, its integer division and
// its parameter typing. It leaves out the PostgreSQL functions CockroachDB lacks and
// rejects the PostgreSQL syntax it does not support, such as PARTITION OF, so such a
// query fails at generation rather than on the server.
// docs/reference/dialects/cockroachdb.md describes what it covers in detail.
//
// # Family
//
// CockroachDB is a PostgreSQL flavour: [New] is the PostgreSQL dialect with CockroachDB's
// settings made ahead of the caller's. The dialect is named cockroachdb and reports the
// postgres family, dialect_engine_postgres.Family, so the PostgreSQL type mappings and
// drivers apply to it.
//
// # Usage
//
// [New] returns the dialect as a [dialect.Dialect], ready for seagull.New. Options adjust
// it:
//
//	engine := dialect_engine_cockroachdb.New(
//	    dialect_engine_cockroachdb.WithSearchPath("app", "public"),
//	    dialect_engine_cockroachdb.WithMaxTokensPerStatement(50_000),
//	)
//	generator, err := seagull.New(engine)
//
// # Options
//
// [Option] is dialect_engine_postgres.Option, so an option from either package applies.
// [WithSearchPath] sets the schemas unqualified names resolve in, and [WithMaxParseDepth]
// and [WithMaxTokensPerStatement] bound the work a hostile statement can cost. [WithName]
// replaces the name New gives it.
//
// # Lexical rules
//
// CockroachDB's statements split under PostgreSQL's rules, the Lexical field of its
// facts, for a migration backend built with migrate.CustomBackend.
//
// # Related providers
//
// Migrations run through migrate_backend_cockroachdb, which serialises runs with a lock
// table and runs migrations outside transactions. Generated code uses database/sql by
// default; emit_driver_pgx writes pgx-native code instead. schema_source_postgres reads
// the catalogue from a live CockroachDB database, and dialect_registry builds this
// dialect by the name cockroachdb, as the sgl command does.
package dialect_engine_cockroachdb
