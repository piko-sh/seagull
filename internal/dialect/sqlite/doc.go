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

// Package sqlite is the dialect for SQLite, using a hand-written recursive-descent
// parser. It converts SQLite DDL into catalogue mutations and analyses DML queries to
// produce the raw query analysis that the analyser resolves into fully typed results.
//
// # Integer widths
//
// SQLite stores every integer as a variable-length 1-8 byte value, so a declared width is
// a preference, not a storage limit: every integer spelling is a signed 64-bit integer
// and maps to int64. An INTEGER PRIMARY KEY (the rowid alias), every integer column of a
// STRICT table, the id of an R*Tree table and the results of counts and sums are named
// BIGINT, so a type override on INTEGER can narrow ordinary columns without narrowing
// them.
//
// # STRICT tables
//
// A STRICT table (CREATE TABLE ... (...) STRICT, alone or with WITHOUT ROWID in either
// order, the two separated by a comma) enforces its declared types and permits only INT,
// INTEGER, REAL, TEXT, BLOB, and ANY; the engine rejects any other spelling at generation
// time (validateStrictColumnTypes) instead of letting it fail at migration time. STRICT
// cannot express BIGINT, so widenStrictIntegers gives its integer columns that name in
// the catalogue to keep them separate from an INTEGER type override. Both ordinary and
// STRICT integer columns map to int64 by default. An ANY column resolves to Go any.
package sqlite
