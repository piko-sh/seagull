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

package gotypes

import (
	"strconv"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// goPackageTime is the import path of the time package.
	goPackageTime = "time"

	// goPackageDecimal is the import path of the arbitrary-precision decimal package exact
	// numeric types map to by default.
	goPackageDecimal = "github.com/cockroachdb/apd/v3"

	// goPackageUUID is the import path of the UUID package.
	goPackageUUID = "github.com/google/uuid"

	// goPackageNetIP is the import path of the IP address package.
	goPackageNetIP = "net/netip"

	// goTypeAny is the Go type a value of no known shape maps to.
	goTypeAny = "any"

	// goTypeString is the Go string type.
	goTypeString = "string"

	// goTypeByteSlice is the Go type a binary value maps to, nullable or not.
	goTypeByteSlice = "[]byte"

	// bitsInByte is the width of the narrowest Go integer, int8.
	bitsInByte = 8

	// bitsInShort is the width of int16.
	bitsInShort = 16

	// bitsInWord is the width of int32 and float32.
	bitsInWord = 32

	// bitsInLong is the width of int64, the widest fixed-width Go integer.
	bitsInLong = 64

	// jsonTagPrefix opens the struct tag naming a field's SQL name, the tag drivers that
	// read a struct by field name honour.
	jsonTagPrefix = "json:"
)

// structuralMapper maps a SQL type to a Go type by its category and attributes.
type structuralMapper struct {
	// table resolves the parts of a composite type, so an override applies to them too.
	table *MappingTable

	// namer names the Go type the generated package declares for an enum or a struct, or is
	// nil when no type is named.
	namer func(sqlType sqlmodel.SQLType) (string, bool)

	// slots holds the type slots in force; a zero slot keeps the built-in default.
	slots sqlmodel.TypeSlots

	// depth is the recursion depth through composite types.
	depth int
}

// goType maps a SQL type to its Go type by structure.
//
// Takes sqlType (sqlmodel.SQLType) which is the SQL type to map.
// Takes nullable (bool) which selects the nullable Go type.
//
// Returns sqlmodel.GoType which is the Go type, or any for a type of no known shape.
func (m *structuralMapper) goType(sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	switch sqlType.Category {
	case sqlmodel.TypeCategoryInteger:
		return m.integer(sqlType, nullable)
	case sqlmodel.TypeCategoryFloat:
		return named(floatName(sqlType.Width), nullable)
	case sqlmodel.TypeCategoryDecimal:
		return m.decimal(sqlType, nullable)
	case sqlmodel.TypeCategoryBoolean:
		return named("bool", nullable)
	case sqlmodel.TypeCategoryText:
		return named(goTypeString, nullable)
	case sqlmodel.TypeCategoryEnum:
		return m.enum(sqlType, nullable)
	case sqlmodel.TypeCategoryBinary, sqlmodel.TypeCategoryAggregateState:
		return sqlmodel.GoType{Composite: nil, Package: "", Name: goTypeByteSlice}
	case sqlmodel.TypeCategoryTemporal:
		return m.temporal(sqlType, nullable)
	case sqlmodel.TypeCategoryJSON, sqlmodel.TypeCategoryUnion:
		document := sqlmodel.GoType{Composite: nil, Package: "", Name: emit.InlineJSONTypeName}
		return fromSlot(m.slots.JSON, sqlmodel.TypeSlot{NotNull: document, Nullable: document}, nullable)
	case sqlmodel.TypeCategoryUUID:
		return qualified(goPackageUUID, "UUID", nullable)
	case sqlmodel.TypeCategoryNetwork:
		return m.network(sqlType, nullable)
	case sqlmodel.TypeCategoryArray:
		return m.array(sqlType)
	case sqlmodel.TypeCategoryMap:
		return m.mapType(sqlType, nullable)
	case sqlmodel.TypeCategoryStruct:
		return m.structType(sqlType, nullable)
	default:
		return named(goTypeAny, false)
	}
}

// integer maps an integer by its width and signedness. An integer wider than 64 bits goes
// through the BigInt slot, whose default is a decimal string, and an integer whose width
// the dialect did not give maps to a 64-bit Go integer.
//
// Takes sqlType (sqlmodel.SQLType) which is the integer type.
// Takes nullable (bool) which selects the nullable Go type.
//
// Returns sqlmodel.GoType which is the Go integer type.
func (m *structuralMapper) integer(sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	if sqlType.Width > bitsInLong {
		return fromSlot(m.slots.BigInt, namedSlot(goTypeString), nullable)
	}
	width := bitsInLong
	switch {
	case sqlType.Width <= 0:
	case sqlType.Width <= bitsInByte:
		width = bitsInByte
	case sqlType.Width <= bitsInShort:
		width = bitsInShort
	case sqlType.Width <= bitsInWord:
		width = bitsInWord
	}
	name := "int" + strconv.Itoa(width)
	if sqlType.Unsigned {
		name = "u" + name
	}
	return named(name, nullable)
}

// enum maps an enum through the Enum slot, or, when it is zero, to the named type the
// namer gives it, or to string when the namer gives it none.
//
// Takes sqlType (sqlmodel.SQLType) which is the enum type.
// Takes nullable (bool) which selects the nullable Go type.
//
// Returns sqlmodel.GoType which is the Go type.
func (m *structuralMapper) enum(sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	if resolved := m.slots.Enum.Resolved(); !resolved.IsZero() {
		return fromSlot(resolved, sqlmodel.TypeSlot{}, nullable)
	}
	if name, ok := m.declaredName(sqlType); ok {
		return named(name, nullable)
	}
	return named(goTypeString, nullable)
}

// declaredName returns the name of the Go type the generated package declares for a SQL
// type, when the namer gives it one.
//
// Takes sqlType (sqlmodel.SQLType) which is the enum or struct type.
//
// Returns string which is the declared type's name.
// Returns bool which is true when the namer names the type.
func (m *structuralMapper) declaredName(sqlType sqlmodel.SQLType) (string, bool) {
	if m.namer == nil {
		return "", false
	}
	return m.namer(sqlType)
}

// decimal maps an exact decimal through the Decimal slot, and an amount of money through
// the Money slot, whose default is a string so currency symbols and locale-specific
// formatting do not need to be parsed as a decimal.
//
// Takes sqlType (sqlmodel.SQLType) which is the decimal type.
// Takes nullable (bool) which selects the nullable Go type.
//
// Returns sqlmodel.GoType which is the Go type.
func (m *structuralMapper) decimal(sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	if sqlType.Monetary {
		return fromSlot(m.slots.Money, namedSlot(goTypeString), nullable)
	}
	decimal := sqlmodel.TypeSlot{
		NotNull:  sqlmodel.GoType{Composite: nil, Package: goPackageDecimal, Name: "Decimal"},
		Nullable: sqlmodel.GoType{Composite: nil, Package: goPackageDecimal, Name: "*Decimal"},
	}
	if resolved := m.slots.Decimal.Resolved(); !resolved.IsZero() {
		decimal = resolved
	}
	return fromSlot(sqlmodel.TypeSlot{}, decimal, nullable)
}

// temporal maps SQL temporal types to Go types using time.Time for dates and timestamps
// and configured slots for times and intervals. Zone-aware times default to strings
// because drivers may lack a suitable Go type.
//
// Takes sqlType (sqlmodel.SQLType) which is the temporal type.
// Takes nullable (bool) which selects the nullable Go type.
//
// Returns sqlmodel.GoType which is the Go type.
func (m *structuralMapper) temporal(sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	timestamp := sqlmodel.TypeSlot{NotNull: qualified(goPackageTime, "Time", false), Nullable: qualified(goPackageTime, "Time", true)}
	switch sqlType.Temporal {
	case sqlmodel.TemporalKindInterval:
		return fromSlot(m.slots.Interval, namedSlot(goTypeString), nullable)
	case sqlmodel.TemporalKindTime:
		if sqlType.TimeZone {
			return fromSlot(m.slots.TimeOfDay, namedSlot(goTypeString), nullable)
		}
		return fromSlot(m.slots.TimeOfDay, timestamp, nullable)
	default:
		return fromSlot(sqlmodel.TypeSlot{}, timestamp, nullable)
	}
}

// network maps a single IP address through the IPAddress slot, whose default is
// netip.Addr. Other network values have no single Go type and map to any.
//
// Takes sqlType (sqlmodel.SQLType) which is the network type.
// Takes nullable (bool) which selects the nullable Go type.
//
// Returns sqlmodel.GoType which is the Go type.
func (m *structuralMapper) network(sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	if sqlType.Network != sqlmodel.NetworkKindAddress {
		return named(goTypeAny, false)
	}
	address := sqlmodel.TypeSlot{
		NotNull:  sqlmodel.GoType{Composite: nil, Package: goPackageNetIP, Name: "Addr"},
		Nullable: sqlmodel.GoType{Composite: nil, Package: goPackageNetIP, Name: "*Addr"},
	}
	return fromSlot(m.slots.IPAddress, address, nullable)
}

// array maps an array to a slice of its element's Go type, using the nullable mapping
// when the element type permits NULL. A NULL array is a nil slice, so the array's own
// nullability changes nothing.
//
// Takes sqlType (sqlmodel.SQLType) which is the array type.
//
// Returns sqlmodel.GoType which is the slice type, nullable or not.
func (m *structuralMapper) array(sqlType sqlmodel.SQLType) sqlmodel.GoType {
	if sqlType.ElementType == nil || m.depth >= sqlmodel.MaxTypeDepth {
		return named("[]"+goTypeAny, false)
	}
	element := m.part(*sqlType.ElementType, sqlType.ElementType.Nullable)
	element.Name = "[]" + element.Name
	return element
}

// mapType maps a key-value map through the Map slot, or, when it is zero, to a Go map of
// its key's and value's Go types. A key rejected by comparableGoType makes the whole type
// any.
//
// Takes sqlType (sqlmodel.SQLType) which is the map type.
// Takes nullable (bool) which selects the nullable Go type.
//
// Returns sqlmodel.GoType which is the Go type.
func (m *structuralMapper) mapType(sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	if resolved := m.slots.Map.Resolved(); !resolved.IsZero() {
		return fromSlot(resolved, sqlmodel.TypeSlot{}, nullable)
	}
	if sqlType.KeyType == nil || sqlType.ElementType == nil || m.depth >= sqlmodel.MaxTypeDepth {
		return named(goTypeAny, false)
	}
	key := m.part(*sqlType.KeyType, false)
	if !comparableGoType(key) {
		return named(goTypeAny, false)
	}
	return sqlmodel.NewGoMap(key, m.part(*sqlType.ElementType, sqlType.ElementType.Nullable))
}

// structType chooses the Go representation of a SQL struct.
//
// Named fields use the configured struct mapping or a generated struct. Positional fields
// use []any, and an empty struct uses any.
//
// Takes sqlType (sqlmodel.SQLType) which is the struct type.
// Takes nullable (bool) which selects a pointer to the struct.
//
// Returns sqlmodel.GoType which is the Go type.
func (m *structuralMapper) structType(sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	if len(sqlType.StructFields) == 0 || m.depth >= sqlmodel.MaxTypeDepth {
		return named(goTypeAny, false)
	}
	if !hasNamedFields(&sqlType) {
		return named("[]"+goTypeAny, false)
	}
	if resolved := m.slots.Struct.Resolved(); !resolved.IsZero() {
		return fromSlot(resolved, sqlmodel.TypeSlot{}, nullable)
	}
	if name, ok := m.declaredName(sqlType); ok {
		return named(name, nullable)
	}
	fields := make([]sqlmodel.GoField, 0, len(sqlType.StructFields))
	taken := make(map[string]struct{}, len(sqlType.StructFields))
	for index := range sqlType.StructFields {
		field := &sqlType.StructFields[index]
		fields = append(fields, sqlmodel.GoField{
			Name: goastutil.ReserveIdentifier(goast.SnakeToPascalCase(field.Name), taken),
			Tag:  jsonTagPrefix + strconv.Quote(field.Name),
			Type: m.part(field.SQLType, field.SQLType.Nullable),
		})
	}
	goType := sqlmodel.NewGoStruct(fields)
	if nullable {
		goType.Name = "*"
	}
	return goType
}

// part maps one part of a composite type, an element, a key, a value or a field, through
// the table, so overrides apply to it too.
//
// Takes sqlType (sqlmodel.SQLType) which is the part's type.
// Takes nullable (bool) which selects the nullable Go type.
//
// Returns sqlmodel.GoType which is the part's Go type.
func (m *structuralMapper) part(sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	if m.table != nil {
		return m.table.resolveDepth(sqlType, nullable, m.depth+1)
	}
	child := &structuralMapper{table: nil, namer: m.namer, slots: m.slots, depth: m.depth + 1}
	return child.goType(sqlType, nullable)
}

// floatName returns the Go float type of a width: float32 for 32 bits or fewer, and
// float64 otherwise, including when the dialect did not give a width.
//
// Takes width (int) which is the float's width in bits.
//
// Returns string which is the Go type name.
func floatName(width int) string {
	if width > 0 && width <= bitsInWord {
		return "float32"
	}
	return "float64"
}

// hasNamedFields reports whether a struct type has fields and every one of them is named.
//
// Takes sqlType (*sqlmodel.SQLType) which is the struct type.
//
// Returns bool which is true when every field has a name it is addressed by.
func hasNamedFields(sqlType *sqlmodel.SQLType) bool {
	if len(sqlType.StructFields) == 0 {
		return false
	}
	for index := range sqlType.StructFields {
		if sqlType.StructFields[index].Positional || sqlType.StructFields[index].Name == "" {
			return false
		}
	}
	return true
}

// comparableGoType reports whether the mapper accepts a Go type as a map key. It rejects
// composite types, any and slice spellings, and assumes other named types are comparable.
//
// Takes goType (sqlmodel.GoType) which is the candidate key type.
//
// Returns bool which is true when the mapper accepts the key type.
func comparableGoType(goType sqlmodel.GoType) bool {
	if goType.Composite != nil || goType.Name == goTypeAny {
		return false
	}
	return len(goType.Name) < 2 || goType.Name[:2] != "[]"
}

// named returns a built-in Go type, or a pointer to it when nullable.
//
// Takes name (string) which is the Go type name.
// Takes nullable (bool) which selects the pointer form.
//
// Returns sqlmodel.GoType which is the Go type.
func named(name string, nullable bool) sqlmodel.GoType {
	if nullable {
		name = "*" + name
	}
	return sqlmodel.GoType{Composite: nil, Package: "", Name: name}
}

// qualified returns a Go type from an imported package, or a pointer to it when nullable.
//
// Takes importPath (string) which is the package's import path.
// Takes name (string) which is the Go type name.
// Takes nullable (bool) which selects the pointer form.
//
// Returns sqlmodel.GoType which is the Go type.
func qualified(importPath string, name string, nullable bool) sqlmodel.GoType {
	if nullable {
		name = "*" + name
	}
	return sqlmodel.GoType{Composite: nil, Package: importPath, Name: name}
}

// namedSlot returns the slot of a built-in Go type: the type itself, and a pointer to it
// when nullable.
//
// Takes name (string) which is the Go type name.
//
// Returns sqlmodel.TypeSlot which holds both forms.
func namedSlot(name string) sqlmodel.TypeSlot {
	return sqlmodel.TypeSlot{NotNull: named(name, false), Nullable: named(name, true)}
}

// fromSlot picks a Go type from a slot, falling back to a default slot when the slot is
// zero.
//
// Takes slot (sqlmodel.TypeSlot) which is the configured slot, possibly zero.
// Takes fallback (sqlmodel.TypeSlot) which is the built-in default.
// Takes nullable (bool) which selects the nullable Go type.
//
// Returns sqlmodel.GoType which is the chosen Go type.
func fromSlot(slot sqlmodel.TypeSlot, fallback sqlmodel.TypeSlot, nullable bool) sqlmodel.GoType {
	chosen := slot.Resolved()
	if chosen.IsZero() {
		chosen = fallback
	}
	if nullable {
		return chosen.Nullable
	}
	return chosen.NotNull
}
