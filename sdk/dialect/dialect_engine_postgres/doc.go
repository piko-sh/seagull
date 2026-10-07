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

// Package dialect_engine_postgres is the dialect for PostgreSQL.
//
// It parses PostgreSQL's DDL with a hand-written recursive-descent parser, replays it
// into the schema catalogue, and types queries with PostgreSQL's rules: its built-in
// types and functions, enums, domains and composite types, arrays and row values, the
// JSON operators, set-returning functions and the schema search path.
// docs/reference/dialects/postgres.md describes what it covers in detail.
//
// # Family
//
// PostgreSQL is the base of the postgres family, whose name is [Family]. The flavours
// dialect_engine_cockroachdb and dialect_engine_timescaledb are this dialect with their
// own settings: they share its [Option] type, so every option here applies to them too,
// and they report the same family.
//
// # Usage
//
// [New] returns the dialect as a [dialect.Dialect], ready for seagull.New. Options adjust
// it:
//
//	engine := dialect_engine_postgres.New(
//	    dialect_engine_postgres.WithSearchPath("app", "public"),
//	    dialect_engine_postgres.WithMaxParseDepth(128),
//	)
//	generator, err := seagull.New(engine)
//
// # Options
//
// [WithSearchPath] sets the schemas unqualified names resolve in, and [WithMaxParseDepth]
// and [WithMaxTokensPerStatement] bound the work a hostile statement can cost. [WithName]
// reports another name in the same family.
//
// # Lexical rules
//
// The rules its statements are split under are the Lexical field of its facts,
// dialect_engine_postgres.New().Info().Lexical, for a migration backend built with
// migrate.CustomBackend. They mark the statements PostgreSQL cannot run inside a
// transaction, such as CREATE INDEX CONCURRENTLY.
//
// # Related providers
//
// Migrations run through migrate_backend_postgres, or its NewPgBouncer behind PgBouncer
// in transaction mode. Generated code uses database/sql by default; emit_driver_pgx
// writes pgx-native code instead. schema_source_postgres reads the catalogue from a live
// database instead of replaying migrations, and dialect_registry builds this dialect by
// the name postgres, as the sgl command does.
package dialect_engine_postgres
