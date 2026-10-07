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

// Package dialect_engine_clickhouse is the dialect for ClickHouse.
//
// It parses ClickHouse's DDL with a hand-written recursive-descent parser, replays it
// into the schema catalogue, and types queries with ClickHouse's rules: typed {name:Type}
// parameters, the Nullable, LowCardinality, Array, Tuple, Map, Nested and JSON types,
// MergeTree engine clauses, ARRAY JOIN, the SETTINGS that change a result's types, and
// ClickHouse's functions and aggregate combinators. docs/reference/dialects/clickhouse.md
// describes what it covers in detail.
//
// # Family
//
// ClickHouse is a family of its own, whose name is [Family], and no flavour builds on it.
// It differs from the PostgreSQL and MySQL families in its parameters, its nullability,
// which a type wrapper carries, and its asynchronous mutations.
//
// # Usage
//
// [New] returns the dialect as a [dialect.Dialect], ready for seagull.New. Options adjust
// it:
//
//	engine := dialect_engine_clickhouse.New(
//	    dialect_engine_clickhouse.WithJoinUseNulls(true),
//	    dialect_engine_clickhouse.WithGroupByUseNulls(true),
//	)
//	generator, err := seagull.New(engine)
//
// # Options
//
// [WithJoinUseNulls] and [WithGroupByUseNulls] match the server settings of those names,
// so set them as the server runs; a query's own SETTINGS clause takes precedence.
// [WithMaxParseDepth], [WithMaxTokensPerStatement] and [WithMaxTypeParseDepth] bound the
// work a hostile statement can cost, and [WithName] reports another name in the same
// family. A user-defined function the server loads is declared to the generator with
// seagull.WithCustomFunctions.
//
// # Lexical rules
//
// The rules its statements are split under are the Lexical field of its facts,
// dialect_engine_clickhouse.New().Info().Lexical, for a migration backend built with
// migrate.CustomBackend. ClickHouse has no transactions, so no statement is marked to run
// outside one.
//
// # Related providers
//
// Migrations run through migrate_backend_clickhouse, which takes no lock, so concurrent
// runs need coordinating outside the migrator. Generated code uses database/sql by
// default; emit_driver_clickhouse writes code for the native clickhouse-go/v2 interface
// instead, with its columnar batch path. dialect_registry builds this dialect by the name
// clickhouse, as the sgl command does.
package dialect_engine_clickhouse
