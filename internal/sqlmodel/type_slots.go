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

package sqlmodel

// TypeSlot names the Go types one category of SQL values maps to. The zero value keeps
// the default for the slot.
type TypeSlot struct {
	// NotNull is the Go type used when the value cannot be NULL.
	NotNull GoType

	// Nullable is the Go type used when the value may be NULL. When it is zero and NotNull
	// is set, a pointer to NotNull is used.
	Nullable GoType
}

// IsZero reports whether the slot keeps its default.
//
// Returns bool which is true when neither Go type is set.
func (slot TypeSlot) IsZero() bool {
	return slot.NotNull == GoType{} && slot.Nullable == GoType{}
}

// Resolved fills an unset Nullable type with a pointer to NotNull when NotNull is set.
//
// Returns TypeSlot with the derived nullable type, or the unchanged slot when NotNull is
// unset or Nullable is already set.
func (slot TypeSlot) Resolved() TypeSlot {
	if slot.NotNull == (GoType{}) {
		return slot
	}
	if slot.Nullable == (GoType{}) {
		slot.Nullable = GoType{Composite: slot.NotNull.Composite, Package: slot.NotNull.Package, Name: "*" + slot.NotNull.Name}
	}
	return slot
}

// TypeSlots names the Go types for the SQL values that have no single natural Go type, or
// whose natural type a driver cannot read: exact decimals, money, integers wider than 64
// bits, JSON, intervals, IP addresses, enums, maps and structs. A zero slot keeps the
// default.
type TypeSlots struct {
	// Decimal maps exact numeric types such as NUMERIC and DECIMAL.
	Decimal TypeSlot

	// Money maps money types. The default is string: an engine writes money in its locale's
	// format, with a currency symbol and group separators, which no decimal type reads.
	Money TypeSlot

	// BigInt maps integer types wider than 64 bits, such as Int128 and HUGEINT.
	BigInt TypeSlot

	// JSON maps JSON documents and tagged unions.
	JSON TypeSlot

	// Interval maps interval types, lengths of time. The default is string, the form a
	// driver behind database/sql returns them in.
	Interval TypeSlot

	// TimeOfDay maps times of day with no date. The default is time.Time, and string for a
	// zone-aware time of day, which a driver may return only as its text; a driver that
	// returns a time of day as text maps it to string.
	TimeOfDay TypeSlot

	// IPAddress maps types holding a single IP address. The default is netip.Addr.
	IPAddress TypeSlot

	// Enum configures Go types for SQL enums. When unset, enums use generated named types
	// where available and strings otherwise.
	Enum TypeSlot

	// Map maps key-value map types. When it is zero a map becomes a Go map of its key's and
	// value's Go types.
	Map TypeSlot

	// Struct maps struct and named tuple types. When it is zero a type whose fields are all
	// named becomes the named Go struct the package's naming plan declares for it, or an
	// anonymous Go struct of its fields' Go types when no plan names it.
	Struct TypeSlot

	// JSONScanInto names the generic function that adapts a typed destination into a
	// JSON-decoding sql.Scanner; only NotNull is used. When it is zero the inline helper
	// emitted with the inline JSON type applies.
	JSONScanInto TypeSlot
}
