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

// Package dialect_engine_timescaledb is the dialect for TimescaleDB.
//
// It reads TimescaleDB's DDL and queries: hypertables in the CREATE HYPERTABLE, the
// create_hypertable() and the CREATE TABLE ... WITH (tsdb.hypertable) forms, continuous
// aggregates, compression settings, the policy, job and hypertable management calls and
// CALL refresh_continuous_aggregate, together with the TimescaleDB and toolkit functions,
// their aggregate-state types and the toolkit's accessor arrows, such as stats_agg(x) ->
// average(). docs/reference/dialects/timescaledb.md describes what it covers in detail.
//
// # Family
//
// TimescaleDB is a PostgreSQL flavour: [New] is the PostgreSQL dialect with TimescaleDB's
// settings made ahead of the caller's, its own statements read alongside PostgreSQL's.
// The dialect is named timescaledb and reports the postgres family,
// dialect_engine_postgres.Family, so the PostgreSQL type mappings and drivers apply to
// it.
//
// # Usage
//
// [New] returns the dialect as a [dialect.Dialect], ready for seagull.New. Options adjust
// it:
//
//	engine := dialect_engine_timescaledb.New(
//	    dialect_engine_timescaledb.WithSearchPath("metrics", "public"),
//	    dialect_engine_timescaledb.WithMaxParseDepth(128),
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
// The rules its statements are split under are the Lexical field of its facts,
// dialect_engine_timescaledb.New().Info().Lexical, for a migration backend built with
// migrate.CustomBackend. They are PostgreSQL's, with the continuous aggregate statements
// TimescaleDB cannot run inside a transaction added.
//
// # Related providers
//
// Migrations run through migrate_backend_timescaledb, which runs those statements outside
// a transaction. Generated code uses database/sql by default; emit_driver_pgx writes
// pgx-native code instead. schema_source_postgres reads the catalogue from a live
// TimescaleDB database, and dialect_registry builds this dialect by the name timescaledb,
// as the sgl command does.
package dialect_engine_timescaledb
