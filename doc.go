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

// Package seagull turns plain SQL into type-safe Go.
//
// Seagull reads a directory of SQL migrations and a directory of annotated queries,
// replays the migrations into a schema catalogue, analyses every query against it with
// full type and nullability inference, and emits Go: typed row structs, one method per
// query, enum types and transaction helpers. Each SQL dialect is a pure-Go analyser, so
// nothing needs cgo or a running database to generate code.
//
// Most projects drive Seagull through the sgl command (seagull.sh/seagull/cmd/sgl). The
// seagull package is the library behind it, for tools and frameworks that embed
// generation.
//
// # Usage
//
//	generator, err := seagull.New(dialect_engine_sqlite.New())
//	if err != nil {
//	    return err
//	}
//	result, err := generator.Generate(ctx, seagull.Input{
//	    Migrations:   os.DirFS("db"),
//	    MigrationDir: "migrations",
//	    QueryDir:     "queries",
//	    Package:      "store",
//	})
//	if err != nil {
//	    return err
//	}
//	if err := result.Err(); err != nil {
//	    return err
//	}
//	err = seagull.WriteFiles("store", result)
//
// # Layout
//
// The public surface is seagull and the areas under seagull.sh/seagull/sdk:
//
//   - seagull: [New], the [Generator] and its options, [WriteFiles], the diagnostics.
//   - sdk/dialect: the dialect contract, dialect_engine_* engines and dialect_registry.
//   - sdk/emit: the code emitter contract and the emit_driver_* emitters.
//   - sdk/migrate: runs migrations and seed files, with migrate_backend_* backends.
//   - sdk/schema: reads the schema from a live database, with schema_source_* sources.
//   - sdk/observe: the observer and clock that trace and time each stage of a run.
package seagull
