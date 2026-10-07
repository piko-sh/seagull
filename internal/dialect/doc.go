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

// Package dialect defines the interface a SQL dialect implements so Seagull can analyse
// its schema and queries.
//
// DialectPort is the required set: parsing statements, replaying DDL into the catalogue,
// analysing queries, the dialect's type system and built-ins, and Info, which describes
// the dialect's identity, syntax and capabilities. The narrower interfaces beside it are
// optional capabilities the analyser detects with a type assertion, such as resolving
// function calls or table-valued functions from the catalogue, typing predicates and row
// values, and loading extension functions.
//
// Dialect holds a DialectPort for the callers that run the generator rather than the
// analyser: its Info gives the dialect's Facts, the part of Info that generated code and
// tools read, and an emitter reads the same Facts from its request.
//
// A dialect's methods should not panic. Those that take a context recover a panic of
// their own at entry and return it as an error; Seagull recovers one again where it runs
// analysis, replays migrations and runs its pipeline, as DialectPort describes. The Info
// a dialect returns belongs to its caller. A dialect handed a statement another dialect
// parsed returns an error wrapping ErrForeignStatement.
//
// The built-in dialects live in the subpackages, one per database;
// internal/dialect/dialects constructs them by name, internal/dialect/dialectkit holds
// helpers for writing a new one, and internal/dialect/dialecttest validates it.
package dialect
