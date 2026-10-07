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

// Package dialect_engine_mariadb is the dialect for MariaDB.
//
// It reads everything the MySQL dialect reads except where MariaDB differs: RETURNING on
// INSERT and DELETE, sequences, OFFSET ... FETCH, OR REPLACE on tables and other objects,
// MariaDB's UUID, INET4, INET6 and VECTOR types and its own functions. It rejects the
// MySQL syntax MariaDB lacks, such as the -> and ->> JSON operators and the INSERT row
// alias, so such a query fails at generation rather than on the server.
// docs/reference/dialects/mariadb.md describes what it covers in detail.
//
// # Family
//
// MariaDB is a MySQL flavour: [New] is the MySQL dialect with MariaDB's settings made
// ahead of the caller's. The dialect is named mariadb and reports the mysql family,
// dialect_engine_mysql.Family, so the MySQL type mappings and drivers apply to it.
//
// # Usage
//
// [New] returns the dialect as a [dialect.Dialect], ready for seagull.New. Options adjust
// it:
//
//	engine := dialect_engine_mariadb.New(
//	    dialect_engine_mariadb.WithANSIQuotes(true),
//	    dialect_engine_mariadb.WithMaxParseDepth(128),
//	)
//	generator, err := seagull.New(engine)
//
// # Options
//
// [Option] is dialect_engine_mysql.Option, so an option from either package applies. New
// already sets [WithReturning] for INSERT and DELETE. [WithANSIQuotes] matches the
// server's sql_mode, so set it as the server runs; [WithMaxParseDepth] and
// [WithMaxTokensPerStatement] bound the work a hostile statement can cost, and [WithName]
// replaces the name New gives it.
//
// # Lexical rules
//
// MariaDB's statements split under MySQL's rules, the Lexical field of its facts, for a
// migration backend built with migrate.CustomBackend. Under [WithANSIQuotes] they read
// "..." as an identifier.
//
// # Related providers
//
// Migrations run through migrate_backend_mysql, which serves MySQL and MariaDB alike and
// whose own WithANSIQuotes must match the dialect's. Generated code uses database/sql,
// through go-sql-driver/mysql with parseTime=true in its DSN. dialect_registry builds
// this dialect by the name mariadb, as the sgl command does.
package dialect_engine_mariadb
