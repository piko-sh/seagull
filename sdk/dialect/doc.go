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

// Package dialect names the SQL dialect the generator works with, the facts generated
// code reads about it, and the schema model a dialect builds its catalogue in.
//
// A dialect is everything Seagull knows about one database engine: how it parses SQL,
// which types and built-in functions it has, and how it writes identifiers, parameters
// and the clauses the generated code adds. The analyser and the code generator hold no
// engine facts of their own; they read them from the dialect.
//
// # Choosing a dialect
//
// A [Dialect] comes from one of the built-in engines or from the registry:
//
//   - dialect_engine_*: one New for each built-in engine: dialect_engine_postgres,
//     dialect_engine_mysql, dialect_engine_sqlite, dialect_engine_duckdb and
//     dialect_engine_clickhouse, and the flavours dialect_engine_cockroachdb and
//     dialect_engine_timescaledb over PostgreSQL and dialect_engine_mariadb over MySQL.
//     Their options set the name, the parser limits and each engine's own settings.
//   - dialect_registry: builds a built-in engine from a name and plain-data options, for
//     a dialect chosen by configuration, as the sgl command does.
//
// The zero Dialect holds none, and the generator refuses it.
//
// # Facts
//
// A Dialect's Info returns its [Facts]: its name and family, how it writes parameters
// ([Placeholders] and [PlaceholderSigil]), identifiers, lists and row limits, the drivers
// it runs on, the lexical rules its scripts split under and the statements that return
// rows ([DataChange]). An emitter reads the same facts from its request, and a tool such
// as the sgl command reads them to choose a migration backend or a driver.
//
// # The model
//
// [Catalogue] is the schema the migrations replay into: its [Schema] values with their
// tables, views, columns, functions and user-defined types. [SQLType] describes a SQL
// type by its [SQLTypeCategory] and the engine's own name; [NewSQLType] and [NewColumn]
// build the values a schema source fills a catalogue with.
//
// # Usage
//
// Pass a dialect to the generator:
//
//	generator, err := seagull.New(dialect_engine_postgres.New())
//	if err != nil {
//	    return err
//	}
//
// Or build one from a name read from configuration:
//
//	engine, err := dialect_registry.New("sqlite", dialect_registry.Options{})
//	if err != nil {
//	    return err
//	}
//	generator, err := seagull.New(engine)
package dialect
