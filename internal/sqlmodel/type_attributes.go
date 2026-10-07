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

const (
	// TemporalKindNone marks a type that is not temporal.
	TemporalKindNone TemporalKind = iota

	// TemporalKindDate is a calendar date with no time of day.
	TemporalKindDate

	// TemporalKindTime is a time of day with no date.
	TemporalKindTime

	// TemporalKindTimestamp is a date and a time of day together.
	TemporalKindTimestamp

	// TemporalKindInterval is a length of time rather than a point in it.
	TemporalKindInterval
)

const (
	// NetworkKindNone marks a type that is not a network value.
	NetworkKindNone NetworkKind = iota

	// NetworkKindAddress is a single IP address with no prefix length. SQLType.Width says
	// which IP version the SQL type contains: 32 for IPv4 only, 128 for IPv6, IPv4 included
	// as IPv4-mapped, and zero for a type that does not say.
	NetworkKindAddress

	// NetworkKindPrefix is an IP address or network together with a prefix length.
	NetworkKindPrefix

	// NetworkKindHardware is a hardware (MAC) address.
	NetworkKindHardware
)

var (
	// temporalKindNames names each TemporalKind, indexed by the kind's value.
	temporalKindNames = [...]string{
		TemporalKindNone:      "none",
		TemporalKindDate:      "date",
		TemporalKindTime:      "time",
		TemporalKindTimestamp: "timestamp",
		TemporalKindInterval:  "interval",
	}

	// networkKindNames names each NetworkKind, indexed by the kind's value.
	networkKindNames = [...]string{
		NetworkKindNone:     "none",
		NetworkKindAddress:  "address",
		NetworkKindPrefix:   "prefix",
		NetworkKindHardware: "hardware",
	}
)

// TemporalKind says which kind of temporal value a type in the temporal category holds.
// The zero value, TemporalKindNone, marks a type outside the category.
type TemporalKind uint8

// String returns the kind's name, such as date or interval.
//
// Returns string which is the name, or TemporalKind(n) for an undeclared value.
func (kind TemporalKind) String() string {
	return enumName(kind, temporalKindNames[:], "TemporalKind")
}

// IsZero reports whether the kind is TemporalKindNone, the kind of every type outside the
// temporal category.
//
// Returns bool which is true for TemporalKindNone.
func (kind TemporalKind) IsZero() bool {
	return kind == TemporalKindNone
}

// NetworkKind says which kind of network value a type in the network category holds. The
// zero value, NetworkKindNone, marks a type outside the category.
type NetworkKind uint8

// String returns the kind's name, such as address or prefix.
//
// Returns string which is the name, or NetworkKind(n) for an undeclared value.
func (kind NetworkKind) String() string {
	return enumName(kind, networkKindNames[:], "NetworkKind")
}

// IsZero reports whether the kind is NetworkKindNone, the kind of every type outside the
// network category.
//
// Returns bool which is true for NetworkKindNone.
func (kind NetworkKind) IsZero() bool {
	return kind == NetworkKindNone
}

// HasStructuralAttributes reports whether a type supplies the category-specific
// attributes needed for structural analysis and Go type mapping.
//
// Returns bool which is true when the type's category-specific attributes are set.
func (t SQLType) HasStructuralAttributes() bool {
	switch t.Category {
	case TypeCategoryInteger, TypeCategoryFloat:
		return t.Width > 0
	case TypeCategoryTemporal:
		return !t.Temporal.IsZero()
	case TypeCategoryNetwork:
		return !t.Network.IsZero()
	default:
		return true
	}
}

// NewIntegerType returns an integer type of the given width and signedness, named as the
// engine spells it.
//
// Takes engineName (string) which is the engine's name for the type.
// Takes width (int) which is the size of the type in bits.
// Takes unsigned (bool) which is true for a type that holds no negative values.
//
// Returns SQLType which is the integer type.
func NewIntegerType(engineName string, width int, unsigned bool) SQLType {
	sqlType := NewSQLType(TypeCategoryInteger, engineName)
	sqlType.Width = width
	sqlType.Unsigned = unsigned
	return sqlType
}

// NewFloatType returns a floating-point type of the given width, named as the engine
// spells it.
//
// Takes engineName (string) which is the engine's name for the type.
// Takes width (int) which is the size of the type in bits, such as 32 or 64.
//
// Returns SQLType which is the floating-point type.
func NewFloatType(engineName string, width int) SQLType {
	sqlType := NewSQLType(TypeCategoryFloat, engineName)
	sqlType.Width = width
	return sqlType
}

// NewTemporalType returns a temporal type of the given kind, named as the engine spells
// it.
//
// Takes engineName (string) which is the engine's name for the type.
// Takes kind (TemporalKind) which says whether it is a date, a time, a timestamp or an
// interval.
// Takes timeZone (bool) which is true for a zone-aware time or timestamp.
//
// Returns SQLType which is the temporal type.
func NewTemporalType(engineName string, kind TemporalKind, timeZone bool) SQLType {
	sqlType := NewSQLType(TypeCategoryTemporal, engineName)
	sqlType.Temporal = kind
	sqlType.TimeZone = timeZone
	return sqlType
}

// NewNetworkType returns a network type of the given kind, named as the engine spells it.
//
// Takes engineName (string) which is the engine's name for the type.
// Takes kind (NetworkKind) which says whether it is an address, a prefix or a hardware
// address.
//
// Returns SQLType which is the network type.
func NewNetworkType(engineName string, kind NetworkKind) SQLType {
	sqlType := NewSQLType(TypeCategoryNetwork, engineName)
	sqlType.Network = kind
	return sqlType
}
