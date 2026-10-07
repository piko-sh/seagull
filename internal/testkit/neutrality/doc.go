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

// Package neutrality holds a test that keeps engine facts out of Seagull's core, so no
// engine becomes the reference the others are measured against.
//
// The core is internal/app, internal/sqlmodel, internal/resolve, internal/analyse,
// internal/check, internal/catalogue, internal/gotypes, internal/gogen,
// internal/pipeline, internal/sqltemplate, internal/directive, internal/sqlfile,
// internal/emit and every package under internal/emit. The test parses each non-test Go
// file there, outside testdata, and reports these facts:
//
//   - Engine type names. A string literal whose trimmed value equals, ignoring case, a
//     spelling only an engine uses, such as int4, float8, timestamptz, bpchar, jsonb,
//     bytea, varchar2, serial, tinyint, mediumtext, hugeint, inet or money. ClickHouse
//     names whose lower-case forms are Go spellings are matched with their exact
//     capitalisation instead: Nullable, LowCardinality, FixedString, DateTime64, Int128,
//     Int256 and UInt8 to UInt256. The narrow ClickHouse names Int16, Int32, Int64,
//     Float32 and Float64 are not matched at all, because Go spells sql.NullInt64's field
//     and reflect's kinds the same way.
//   - The Go spellings int8 and Int8 are also the PostgreSQL bigint and ClickHouse Int8.
//     They count only where the literal decides something: a map key, a switch case, an
//     operand of == or !=, or the value of a const or var declaration. Elsewhere, such as
//     a call argument naming a Go type, they are taken as Go.
//   - Map keys. A string-literal key of a map composite literal counts when its
//     lower-case form is any listed type name, so a lookup table keyed by engine
//     spellings is caught whatever the capitalisation.
//   - The default schema: the literal public.
//   - Engine families: a literal naming an engine, such as postgres, mysql, sqlite,
//     duckdb or clickhouse, and any selector whose name begins with Family, such as a
//     dialect's Family constant or dialect.Info's Family field, since nothing in the core
//     may switch on a family.
//   - Placeholder decisions: a literal that is wholly a placeholder ($, $1, ?, @p1 or a
//     ClickHouse {name:Type}). The core writes placeholders from the dialect's
//     placeholder description instead. Lone braces are not matched; the code generator
//     writes them for Go.
//   - Engine operators and pseudo-columns: ||, ->, ->>, #>, #>>, @>, <@, ?|, ?&, ::,
//     ILIKE, GLOB, REGEXP, RLIKE, SIMILAR TO, and rowid, _rowid_, ctid, xmin and xmax.
//
// Struct tags, import paths and arguments to the generator's Go-name builders (Ident,
// NewIdent, CachedIdent, SelectorExpr, SelectorExprFrom, KeyValueIdent and AddImport) are
// never matched, because they name Go, not SQL.
//
// The test fails on any fact it finds, naming its file, line and kind. A fact that
// belongs to an engine reaches the core through the dialect contract, as data in
// dialect.Info or the type catalogue, or as a dialect hook.
package neutrality
