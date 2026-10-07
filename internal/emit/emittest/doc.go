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

// Package emittest validates an emitter: a conformance suite of behaviours every emitter
// must share, whatever driver API it writes Go for and whatever dialect the queries come
// from.
//
// Run drives an emitter through the generator with every dialect the caller passes, and
// with the martian dialect, an invented engine that shares no convention with a real
// database, so every emitter is shown to assume no real engine. It runs over a neutral
// corpus the suite writes in each dialect's own syntax: its placeholders, directive
// comments and identifier quotes come from the dialect's Info, and its tables are typed
// with the dialect's core types through a schema source, so the suite never names an
// engine. Each case of the corpus is one generated package. The checks hold the output to
// the guarantees that keep emitters interchangeable: generation fails only with
// diagnostics, files are named safely and carry the header, the output type-checks, every
// parameter is bound and every row field filled, generated names never collide with what
// the emitter declares, SQL constants use the dialect's execution placeholders and keep
// every byte of the query, declarations are unique, and the output is the same however
// the queries are ordered.
//
// A Harness adds the runtime lane: the suite builds the generated packages with a
// recording stand-in for the driver, calls every method of the runtime cases, and checks
// what the driver receives: as many arguments as the SQL has placeholders, every value
// passed, slices of every length, multi-row batches, a batch whose clause after its
// VALUES row may change one row twice sending each item alone, sorted and runtime-built
// queries, counts, enum values of the types the plan declares, alone and in nested
// shapes, values of the defined types a column override names, JSON documents written as
// text and read into a destination the driver fills, and the query-name lookup.
// DatabaseSQL, Pgx and ClickHouseNative are the harnesses for code written against
// database/sql, pgx and the native ClickHouse driver, and refuse what those drivers
// refuse; a Harness of an emitter's own stands in for any other driver API.
//
// Every emitter generates the corpus's cases: each command, named and repeated
// parameters, optional queries and parameters, slices, batches, sortable queries, runtime
// builders, and grouped rows with embeds. Only a query the driver cannot run as written
// may be refused, with an error diagnostic from the emitter's emit.CheckerPort rather
// than an error from Emit: execrows and execresult on a driver that reports no affected
// rows, a batch that binds a parameter outside its VALUES row, and, under Q059, a column
// or parameter holding an enum in a nested shape the driver cannot take: an array of
// nullable enums, a map of enums or an array of arrays of enums. A corpus case an emitter
// refuses is left out of the corpus checks.
//
// Corpora add golden-style case directories written in one dialect's SQL, such as a
// dialect's own testdata, whose output is held to the checks that need no knowledge of
// the queries.
//
// A failing check can be recorded in Suite.KnownFailures with its reason, which every
// entry must give. If it passes, the suite fails until the entry is removed. The checks
// that need the go command, the type checks and the runtime lane, are skipped under
// -short. A generated package that does not build with the harness fails the runtime
// checks of its case with the compiler's report. A check, an emitter or a dialect that
// panics fails the check with the panic's value, and the stack is logged once in the
// subtest's output.
//
// The package uses only the standard library's testing package, so importing it adds no
// test dependencies.
package emittest
