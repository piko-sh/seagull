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

// Package dialect_engine_sqlite is the dialect for SQLite.
//
// It parses SQLite's DDL with a hand-written recursive-descent parser, replays it into
// the schema catalogue, and types queries with SQLite's affinity rules: STRICT and
// WITHOUT ROWID tables, the rowid alias, generated columns, views and triggers, FTS5 and
// R*Tree virtual tables, RETURNING, ON CONFLICT, UPDATE ... FROM and table-valued
// functions such as json_each. Every integer spelling is a signed 64-bit integer.
// docs/reference/dialects/sqlite.md describes what it covers in detail.
//
// # Family
//
// SQLite is a family of its own, whose name is [Family], and no flavour builds on it.
//
// # Usage
//
// [New] returns the dialect as a [dialect.Dialect], ready for seagull.New. Options adjust
// it:
//
//	engine := dialect_engine_sqlite.New(
//	    dialect_engine_sqlite.WithMaxParseDepth(128),
//	    dialect_engine_sqlite.WithMaxTokensPerStatement(50_000),
//	)
//	generator, err := seagull.New(engine)
//
// # Options
//
// [WithMaxParseDepth] and [WithMaxTokensPerStatement] bound the work a hostile statement
// can cost, and [WithName] reports another name in the same family. A function the
// program registers with its SQLite driver is declared to the generator with
// seagull.WithCustomFunctions.
//
// # Lexical rules
//
// The rules its statements are split under are the Lexical field of its facts,
// dialect_engine_sqlite.New().Info().Lexical, for a migration backend built with
// migrate.CustomBackend. They mark a leading VACUUM, which SQLite cannot run inside a
// transaction.
//
// # Related providers
//
// Migrations run through migrate_backend_sqlite. Generated code uses database/sql.
// schema_source_sqlite reads the catalogue from a live database instead of replaying
// migrations, and dialect_registry builds this dialect by the name sqlite, as the sgl
// command does.
package dialect_engine_sqlite
