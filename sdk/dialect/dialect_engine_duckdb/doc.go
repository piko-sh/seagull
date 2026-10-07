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

// Package dialect_engine_duckdb is the dialect for DuckDB.
//
// It parses DuckDB's DDL with a hand-written recursive-descent parser, replays it into
// the schema catalogue, and types queries with DuckDB's rules: its nested STRUCT, MAP,
// LIST, fixed-size array and UNION types, struct and list literals, list comprehensions
// and lambdas, PIVOT and UNPIVOT, scalar and table macros, and DuckDB's functions.
// docs/reference/dialects/duckdb.md describes what it covers in detail.
//
// # Family
//
// DuckDB is a family of its own, whose name is [Family], and no flavour builds on it. Its
// SQL is largely PostgreSQL-compatible, and its parser follows PostgreSQL's, but its
// types, functions and lexical rules are its own, so it is not a PostgreSQL flavour.
//
// # Usage
//
// [New] returns the dialect as a [dialect.Dialect], ready for seagull.New. Options adjust
// it:
//
//	engine := dialect_engine_duckdb.New(
//	    dialect_engine_duckdb.WithMaxParseDepth(128),
//	    dialect_engine_duckdb.WithMaxTokensPerStatement(50_000),
//	)
//	generator, err := seagull.New(engine)
//
// # Options
//
// [WithMaxParseDepth] and [WithMaxTokensPerStatement] bound the work a hostile statement
// can cost, and [WithName] reports another name in the same family. A function an
// extension adds is declared to the generator with seagull.WithCustomFunctions.
//
// # Lexical rules
//
// The rules its statements are split under are the Lexical field of its facts,
// dialect_engine_duckdb.New().Info().Lexical, for a migration backend built with
// migrate.CustomBackend. They mark a leading VACUUM, which DuckDB cannot run inside a
// transaction.
//
// # Related providers
//
// Migrations run through migrate_backend_duckdb, which a custom CLI or the application
// links together with its cgo driver; the stock sgl command does not. Generated code uses
// database/sql. dialect_registry builds this dialect by the name duckdb, as the sgl
// command does.
package dialect_engine_duckdb
