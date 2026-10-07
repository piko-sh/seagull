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

// Package dialecttest validates a dialect: a conformance suite of behaviours every
// dialect must share, and a golden-file suite that records what a dialect makes of a
// directory of migrations and queries.
//
// RunConformance drives a dialect through the generator and checks the invariants that
// keep dialects interchangeable: statement splitting, parameter tracking, delimited and
// reserved-word identifiers, declared columns, placeholders, trailing tokens, byte order
// marks, unknown references, comment handling, unterminated-comment diagnostics, bounded
// recursion on deeply nested input, a complete Info, additive options, and RETURNING
// clauses that behave as Info declares. It catches the drift where a fix lands in one
// dialect but not its siblings, which per-dialect golden files, being re-recordable,
// cannot. A check a dialect fails today is recorded in Fixture.KnownFailures with its
// reason, and must keep failing until the entry is removed, so a fix cannot go unnoticed.
// A check that panics, or whose dialect panics, fails alone, its stack logged, and the
// other checks still run.
//
// RunGolden runs every case directory under a testdata directory through the generator
// and compares a curated projection of the catalogue, analysed queries and diagnostics
// with the golden files beside them, re-recording them when asked. A case's testspec.json
// may hold only the keys the kit knows, so a misspelt expectation fails the case instead
// of checking nothing.
//
// FuzzGenerate feeds fuzzed query files through the whole generator, seeded from a
// dialect's golden cases, and checks that it never panics, finishes before its context's
// deadline, fails only with a limit or malformed-SQL error, and places every diagnostic
// on a line.
//
// Each entry point checks its arguments first and stops the test before running anything
// when they are unusable, such as a nil dialect constructor.
//
// The package uses only the standard library's testing package, so importing it adds no
// test dependencies.
package dialecttest
