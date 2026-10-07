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

// Package clickhouse implements emit.EmitterPort for the native clickhouse-go/v2 driver.
//
// Generates Go source that uses the ClickHouse native interface
// (github.com/ClickHouse/clickhouse-go/v2/lib/driver) rather than database/sql. Each
// `{name:Type}` placeholder keeps its brace form with the name given a `p_` wire prefix,
// and its value binds as clickhouse.Named("p_name", sglClickHouseValue(value, "shape")),
// the form the driver requires for a statement holding such placeholders. The generated
// sglClickHouseValue renders each value as the text the server parses for the parameter's
// type, which the shape describes. Bulk writes (batch / copyfrom) whose VALUES row is one
// placeholder per column compile to the native columnar path conn.PrepareBatch ->
// batch.Append -> batch.Send, which appends typed values.
//
// # Enums
//
// The native driver scans an Enum8 or Enum16 value only into a string or a pointer to
// one, and a Map only into a map of its own types, and its batch columns append an enum
// value, a pointer, a slice, a slice of pointers or a map's values only as Go's own
// string types, so the generated code converts a generated enum type: a NOT NULL value
// scans through (*string)(&row.Field), a nullable one through a *string and a map of enum
// values through a map of strings, each converted after the scan, and a batch appends
// string(value), (*string)(value), sglConvertStrings[string](value),
// sglConvertStringPointers[string](value) and sglConvertStringMap[string](value). An
// Array of an enum, of nullable ones or of such arrays, and a Tuple with an enum field,
// scan into the generated types as they are. Check refuses, under Q059, a column holding
// an enum in a map deeper than as its values, which the driver cannot read, and a value a
// columnar batch appends holding an enum deeper than the shapes above, which it cannot
// append; a parameter bound by name is written as text, whatever its shape.
//
// # Addresses and maps in a columnar batch
//
// The native driver appends an address to an IPv4 column through netip.Addr.As4, which
// panics on the zero netip.Addr and on an IPv6 address, and stores the zero address in an
// IPv6 column as ::. Its batch columns append a Map only from a map of exactly the Go
// types the driver reads the column as, though an Array or a Tuple takes the generated
// netip.Addr, *big.Int and structs element by element. A columnar batch whose row holds
// an IP address, at any depth, or a Map whose values hold a tuple therefore appends each
// row through the generated sglAppendNativeRow (clickhouse_appending.go), passing the
// row's shape: it refuses the zero address and an IPv6 address for an IPv4 value with an
// error naming the parameter, before the row is appended, and converts a value holding a
// Map whose values hold an address or a tuple, each address into a net.IP and each named
// tuple into a map[string]any keyed by element name. It reads the shape with the value
// helper's parser, so the package also holds clickhouse_values.go. Check refuses, under
// Q059, a value a columnar batch appends holding a Map keyed by a type the driver cannot
// decode: an IP address, an Array, a Tuple, a Map or a Decimal. A parameter bound by name
// is checked the same way by sglClickHouseValue, so every path refuses the same
// addresses.
//
// This is the high-throughput counterpart to the generic database/sql emitter, as the pgx
// emitter is for PostgreSQL. It is opt-in: select it with seagull.WithEmitter(New()), or
// go.driver: clickhouse in sgl.yaml.
//
// # Dependencies
//
// The emitter itself does not import clickhouse-go at build time; it only produces source
// text that references the native driver types. The clickhouse-go/v2 dependency therefore
// lands only in the module that consumes the generated code, never in Seagull itself.
//
// # Constraints
//
// ClickHouse's native Exec returns only an error (no sql.Result), so exec compiles to a
// single-value `return db.Exec(...)` while execrows and execresult are rejected at
// generation time. The native connection has no interactive transactions, so the
// generated querier omits WithTx/RunInTx.
//
// # Thread safety
//
// Emitter values carry no mutable state and are safe for concurrent use by multiple
// goroutines.
package clickhouse
