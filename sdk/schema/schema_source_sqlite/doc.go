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

// Package schema_source_sqlite reads the schema catalogue from a live SQLite database, so
// the generator can analyse queries against an existing database instead of replaying
// migration files.
//
// The source [New] returns implements [schema.SchemaSourcePort] through PRAGMA commands.
// It covers the tables, virtual tables, views and indexes of the main schema, leaves out
// SQLite's internal tables and the shadow tables a virtual table keeps its data in, and
// marks a column SQLite hides from SELECT * as hidden, as migration replay does for the
// same DDL. Pass dialect_engine_sqlite as the type normaliser.
//
// # Usage
//
//	engine := dialect_engine_sqlite.New()
//	generator, err := seagull.New(engine,
//	    seagull.WithSchemaSource(schema_source_sqlite.New(database, engine)),
//	)
//
// The caller opens and closes the database; the source only reads from it.
package schema_source_sqlite
