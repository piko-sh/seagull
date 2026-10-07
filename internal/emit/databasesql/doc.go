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

// Package databasesql implements emit.EmitterPort for the database/sql runtime target. It
// generates Go source code that uses the standard database/sql interfaces (ExecContext,
// QueryContext, QueryRowContext) and, for drivers that support it, includes a
// PreparedDBTX wrapper that eagerly caches static queries and runs other SQL directly.
//
// This is the built-in default emitter. For pgx-native code generation, use
// emit_driver_pgx instead.
//
// # Enums
//
// database/sql stores a string into a generated enum type, a named type over string, but
// a slice or a map only into one of exactly the type the driver returns, and binds a
// named string type as the string it holds. Where a driver needs more, its profile says
// so and the generated code converts through sglConvertStrings, sglConvertStringPointers
// and sglConvertStringMap: clickhouse-go returns an enum array as a []string, an array of
// nullable enums as a []*string and a map of enum values as a map of strings, so those
// columns are read through them; and pgx's stdlib has no encoding for an enum's array
// type the connection has not loaded, so a slice of an enum bound whole, an enum array
// parameter, is bound as a []string, or a []*string where its elements may be NULL, which
// pgx sends as text the server parses as the enum's array type. Check refuses, under
// Q059, a ClickHouse column holding an enum deeper than those shapes, which database/sql
// cannot store, and a pgx parameter holding one deeper than a slice, such as a
// multidimensional enum array, which pgx's stdlib cannot send; a slice parameter
// database/sql binds element by element is judged by its elements.
//
// # NULL elements
//
// A parameter of an array, list or map type binds its elements, or its map's values, as
// pointers where they may be NULL, so a nil one binds NULL. DuckDB's driver panics on a
// NULL element of a list parameter, or a NULL value of a map parameter, and on a pointer
// to one, so the emitter reports through emit.NullElementBinderPort that it cannot bind
// them: the parameters of a DuckDB package keep plain elements and values, and Check
// refuses, under Q059, a parameter that declares them nullable.
package databasesql
