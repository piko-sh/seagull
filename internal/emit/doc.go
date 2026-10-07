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

// Package emit defines the interface a code emitter implements to turn analysed queries
// into Go source for one database driver API, and the request it receives.
//
// An EmitterPort has one method, Emit, so the request can grow without breaking
// implementations. Optional interfaces add to it: TypeDefaulterPort supplies the driver's
// natural Go types, ReserverPort lists the names the emitter's generated code uses so the
// naming plan never chooses them, and CheckerPort refuses, as diagnostics at the query,
// what the driver cannot serve.
//
// The Request carries the analysed queries, whose executable SQL is each query's
// sqlmodel.SQLTemplate, the schema, the type mapping, the dialect's facts, the style of
// the generated code and the package's naming plan, Naming. The emitkit package renders
// templates, reads batch rows and writes the querier from it; the emittest package holds
// the conformance suite every emitter passes.
//
// The built-in implementations live in the subpackages: database/sql, pgx and the native
// ClickHouse driver.
package emit
