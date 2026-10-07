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

// Package dialect_engine_mysql is the dialect for MySQL.
//
// It parses MySQL's DDL with a hand-written recursive-descent parser, replays it into the
// schema catalogue, and types queries with MySQL's rules: backtick identifiers,
// AUTO_INCREMENT, unsigned integers, ENUM and SET columns, generated columns, INSERT ...
// ON DUPLICATE KEY UPDATE, INSERT IGNORE and REPLACE, JSON columns and the -> and ->>
// operators, window functions, stored routines with or without DELIMITER, CALL and the
// SHOW statements whose columns MySQL fixes. docs/reference/dialects/mysql.md describes
// what it covers in detail.
//
// # Family
//
// MySQL is the base of the mysql family, whose name is [Family]. The flavour
// dialect_engine_mariadb is this dialect with MariaDB's settings: it shares its [Option]
// type, so every option here applies to it too, and it reports the same family.
//
// # Usage
//
// [New] returns the dialect as a [dialect.Dialect], ready for seagull.New. Options adjust
// it:
//
//	engine := dialect_engine_mysql.New(
//	    dialect_engine_mysql.WithANSIQuotes(true),
//	    dialect_engine_mysql.WithMaxParseDepth(128),
//	)
//	generator, err := seagull.New(engine)
//
// # Options
//
// [WithANSIQuotes] matches the server's sql_mode, so set it as the server runs.
// [WithReturning] names the statements that accept a RETURNING clause, which MySQL has on
// none. [WithMaxParseDepth] and [WithMaxTokensPerStatement] bound the work a hostile
// statement can cost, and [WithName] reports another name in the same family.
//
// # Lexical rules
//
// The rules its statements are split under are the Lexical field of its facts,
// dialect_engine_mysql.New().Info().Lexical, for a migration backend built with
// migrate.CustomBackend. Under [WithANSIQuotes] they read "..." as an identifier.
//
// # Related providers
//
// Migrations run through migrate_backend_mysql, whose own WithANSIQuotes must match the
// dialect's. Generated code uses database/sql, through go-sql-driver/mysql with
// parseTime=true in its DSN. dialect_registry builds this dialect by the name mysql, as
// the sgl command does.
package dialect_engine_mysql
