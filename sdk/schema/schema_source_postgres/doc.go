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

// Package schema_source_postgres reads the schema catalogue from a live
// PostgreSQL-compatible database, so the generator can analyse queries against an
// existing database instead of replaying migration files.
//
// The source [New] returns implements [schema.SchemaSourcePort] for PostgreSQL,
// CockroachDB and TimescaleDB through pg_catalog and information_schema. It covers the
// tables, views, indexes, enums, composite types, functions and extensions in every
// schema the role may use, and leaves out the system schemas, TimescaleDB's internal
// schemas and the schemas and relations an extension owns. Pass the dialect the generator
// uses as the type normaliser: dialect_engine_postgres, dialect_engine_cockroachdb or
// dialect_engine_timescaledb.
//
// # Usage
//
//	engine := dialect_engine_postgres.New()
//	generator, err := seagull.New(engine,
//	    seagull.WithSchemaSource(schema_source_postgres.New(database, engine)),
//	)
//
// The caller opens and closes the database; the source only reads from it, so a role that
// can read the schemas the queries use is enough.
package schema_source_postgres
