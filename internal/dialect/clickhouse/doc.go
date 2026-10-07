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

// Package clickhouse is the dialect for ClickHouse. It implements dialect.DialectPort:
// SQL parsing, the type system, the built-in function and type catalogues, and Info.
//
// ClickHouse diverges from the SQL-standard family that postgres and mysql inhabit in
// three notable ways the dialect must handle.
//
// Parameter binding uses the brace form `{name:Type}`, which carries both the placeholder
// identifier and its ClickHouse-native type tag. The dialect describes the form in
// Info.Placeholders, as a named form with a type, executed as written, so the emitters
// bind each value to its placeholder by name.
//
// ClickHouse type names encode nullability as `Nullable(T)`. The type parser returns the
// unwrapped type and a separate nullability flag, which column and placeholder parsing
// apply to the catalogue column or parameter type.
//
// `CREATE TABLE ... ENGINE = MergeTree() PARTITION BY ... ORDER BY ... SETTINGS ...`
// carries query-planning metadata that codegen does not interpret. The parser captures
// reconstructed clause text into CatalogueMutation.EngineSpecific for inspection by
// callers. The stored text does not preserve the original formatting or quoting.
//
// Migrations run through migrate_backend_clickhouse.New, which has no lock because
// ClickHouse has no advisory locks; concurrent migrators rely on external coordination.
package clickhouse
