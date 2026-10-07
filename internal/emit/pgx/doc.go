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

// Package pgx implements emit.EmitterPort for pgx/v5.
//
// Generates Go source code that uses the pgx native interfaces rather than database/sql.
// The emitter itself does not import pgx at build time; it only produces source text that
// references pgx types.
//
// # Slice parameters
//
// pgx encodes a Go slice as a PostgreSQL array, so a `kind: slice` parameter binds as one
// argument: its `IN ($N)` list is generated as `= ANY($N)` and its `NOT IN ($N)` list as
// `<> ALL($N)`. The statement text does not depend on the slice length, so pgx caches one
// prepared statement per query. An empty slice is an empty array, which `= ANY` never
// matches and `<> ALL` always matches. For NOT IN, generated methods replace a nil slice
// with an empty slice so it also matches every row. For IN, a nil slice binds NULL and
// matches no rows. The database/sql emitter uses the dialect's empty-set tests when
// expanding nil or empty slices, with the same matching behaviour.
//
// # Enums
//
// pgx binds and scans a value of a generated enum type, and a pointer to one, through the
// string it holds. It has no encoding for an array of a type the connection has not
// registered, such as an enum's array type, so a slice of a generated enum type, or of
// pointers to one, whether a slice parameter or an enum array parameter or column, is
// bound and scanned as Go's own []string or []*string through the generated
// sglConvertStrings or sglConvertStringPointers: pgx sends and reads the array as text,
// which the server parses as the enum's array type, and the connection needs no LoadType.
// A map of an enum's values, which a dialect with map types may hold, is bound and
// scanned as a map of strings through sglConvertStringMap likewise. pgx knows no Go type
// for a multidimensional text array, so Check refuses, under Q059, a column or parameter
// holding an enum array of more than one dimension, or an enum nested in a map's key or
// deeper in its value. CopyFrom sends every value in binary, which pgx writes for an enum
// array only on a connection that has loaded its type, so Check refuses a copyfrom into
// an enum array column; an enum value itself copies as the label it holds.
//
// # Thread safety
//
// Emitter values carry no mutable state and are safe for concurrent use by multiple
// goroutines.
package pgx
