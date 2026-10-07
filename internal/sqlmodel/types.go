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

// SQLTypeCategory classifies SQL types into broad categories for structured type
// resolution.
type SQLTypeCategory uint8

const (
	// TypeCategoryInteger covers whole numbers of a fixed width, signed or unsigned; Width
	// and Unsigned describe them.
	TypeCategoryInteger SQLTypeCategory = iota

	// TypeCategoryFloat covers binary floating-point numbers; Width describes them.
	TypeCategoryFloat

	// TypeCategoryDecimal covers exact decimal numbers, with an optional precision and
	// scale; Monetary marks an amount of money.
	TypeCategoryDecimal

	// TypeCategoryBoolean covers truth values.
	TypeCategoryBoolean

	// TypeCategoryText covers character strings, fixed or varying in length.
	TypeCategoryText

	// TypeCategoryBinary covers byte strings, fixed or varying in length.
	TypeCategoryBinary

	// TypeCategoryTemporal covers dates, times of day, timestamps and intervals; Temporal
	// and TimeZone describe them.
	TypeCategoryTemporal

	// TypeCategoryJSON covers JSON documents, stored as text or in a binary form.
	TypeCategoryJSON

	// TypeCategoryUUID covers universally unique identifiers.
	TypeCategoryUUID

	// TypeCategoryNetwork covers IP addresses, networks and hardware addresses; Network
	// describes them, and Width the size of an address type that holds one IP version.
	TypeCategoryNetwork

	// TypeCategoryGeometric covers point, line, polygon, and similar types.
	TypeCategoryGeometric

	// TypeCategoryEnum covers user-defined enum types.
	TypeCategoryEnum

	// TypeCategoryComposite covers user-defined composite types.
	TypeCategoryComposite

	// TypeCategoryArray covers array types wrapping another element type.
	TypeCategoryArray

	// TypeCategoryRange covers ranges of another type's values.
	TypeCategoryRange

	// TypeCategoryStruct covers records of typed fields, named or positional.
	TypeCategoryStruct

	// TypeCategoryMap covers maps from typed keys to typed values.
	TypeCategoryMap

	// TypeCategoryUnion covers tagged unions, a value of one of several typed members.
	TypeCategoryUnion

	// TypeCategoryUnknown is the fallback for unrecognised types.
	TypeCategoryUnknown

	// TypeCategoryAggregateState covers the intermediate state of an aggregate function,
	// stored in place of a finalised value; reading the value needs the engine's finalising
	// function. It is placed after TypeCategoryUnknown so earlier values stay stable in
	// serialised golden files.
	TypeCategoryAggregateState
)

// StructField describes a single named field within a STRUCT type.
type StructField struct {
	// Name is the field name.
	Name string

	// SQLType is the field's type. The SQLType.Nullable flag records inner nullability for
	// the field, since individual struct fields can be independently nullable in ClickHouse
	// (`Tuple(a Nullable(T), b T)`).
	SQLType SQLType

	// Positional reports a field the type addresses only by position, such as an element of
	// an unnamed tuple. Name then holds the dialect's synthesised positional name.
	Positional bool
}

// UnionMember describes a single tagged member within a UNION type.
type UnionMember struct {
	// Tag is the member's tag name.
	Tag string

	// SQLType is the member's type.
	SQLType SQLType
}

// QueryCommand specifies the execution pattern for a query.
type QueryCommand uint8

const (
	// QueryCommandOne reads the first row. A zero-row result is an error unless the query
	// sets Optional, which adds a boolean result indicating whether a row was found.
	QueryCommandOne QueryCommand = iota

	// QueryCommandMany returns a slice of rows.
	QueryCommandMany

	// QueryCommandExec executes without returning rows.
	QueryCommandExec

	// QueryCommandExecResult returns the driver's execution result.
	QueryCommandExecResult

	// QueryCommandExecRows returns the affected row count.
	QueryCommandExecRows

	// QueryCommandBatch executes using the driver's batch strategy.
	QueryCommandBatch

	// QueryCommandStream returns an iterator over rows.
	QueryCommandStream

	// QueryCommandCopyFrom uses the driver's bulk-insert strategy, such as PostgreSQL COPY.
	QueryCommandCopyFrom

	// QueryCommandAsyncExec marks a fire-and-forget statement whose completion semantics are
	// server-side asynchronous.
	//
	// ClickHouse ALTER UPDATE and ALTER DELETE are the canonical case: the server accepts
	// the statement, queues a background mutation, and the client receives no rows-affected
	// count. The emitted method returns (error) only and carries a doc comment explaining
	// the asynchronous lifecycle so callers do not expect synchronous completion. Dialects
	// that report Info.AsyncMutations as false reject this command at analysis time.
	QueryCommandAsyncExec
)

// SQLType is a structured representation of a SQL type, carrying category,
// engine-specific name, and optional parameters.
type SQLType struct {
	// Precision is the numeric precision for decimal types, or nil if unspecified.
	Precision *int

	// Scale is the numeric scale for decimal types, or nil if unspecified.
	Scale *int

	// Length is the character length for text types, or nil if unspecified.
	Length *int

	// ElementType is the element type for array and range types, or the value type for maps.
	ElementType *SQLType

	// EngineName is the engine-native type name (e.g. "numeric", "varchar", "int8"), used
	// for display and engine-specific code paths.
	EngineName string

	// Schema is the schema for user-defined types (enums, composites). Empty for built-in
	// types.
	Schema string

	// EnumValues holds the valid values for enum types.
	EnumValues []string

	// StructFields holds the fields of structured types, such as DuckDB STRUCT and
	// ClickHouse Tuple.
	StructFields []StructField

	// KeyType is the key type for map types. The value type is stored in ElementType.
	KeyType *SQLType

	// UnionMembers holds the tagged members for UNION types (DuckDB).
	UnionMembers []UnionMember

	// Width is the size in bits of a numeric or IP address type. Zero means the width is
	// unspecified or variable.
	Width int

	// Category classifies the type for structured resolution.
	Category SQLTypeCategory

	// Temporal says which kind of temporal value the SQL type contains: a date, a time of
	// day, a timestamp or an interval. It is TemporalKindNone outside the temporal category.
	Temporal TemporalKind

	// Network says which kind of network value the SQL type contains: a single IP address,
	// an address with a prefix length, or a hardware address. It is NetworkKindNone outside
	// the network category.
	Network NetworkKind

	// Nullable reports whether values can be NULL at the type level.
	//
	// ClickHouse uses `Nullable(T)` as a type wrapper, so `Array(Nullable(String))` must
	// carry the inner nullability on the element type. The Column and Parameter consumers
	// also carry a separate Nullable flag for outer-column nullability; the type-level flag
	// covers inner wrappers where consumer-level nullability cannot reach, such as array
	// elements, map values, and tuple fields.
	Nullable bool

	// Unsigned reports an integer type that holds no negative values.
	Unsigned bool

	// TimeZone reports a time or timestamp that is zone-aware: it carries a zone offset, or
	// it stores an instant shown in the session's time zone.
	TimeZone bool

	// Monetary reports an exact decimal type that stores an amount of money in a fixed
	// currency scale, which the Money type slot maps.
	Monetary bool

	// BinaryForm marks text values sent in a different binary-protocol representation.
	// Generated code casts them to the dialect's text type when the driver cannot decode
	// them into a string.
	BinaryForm bool
}

// NewSQLType returns a SQLType of the given category, named as the engine spells it, with
// every other attribute unset.
//
// Takes category (SQLTypeCategory) which classifies the type.
// Takes engineName (string) which is the engine's name for the type, or "" when it has
// none.
//
// Returns SQLType which carries only the category and the engine name.
func NewSQLType(category SQLTypeCategory, engineName string) SQLType {
	sqlType := SQLType{}
	sqlType.EngineName = engineName
	sqlType.Category = category
	return sqlType
}

// TypeOverride customises a SQL type's or column's Go type, collection element
// nullability or field tags. Exactly one of SQLTypeName and Column is set.
type TypeOverride struct {
	// NonNullElements controls whether collection elements and map values can be NULL where
	// the engine permits them. Nil keeps the preset's choice.
	NonNullElements *bool

	// StructTags adds tags to fields using a column override, with {name} replaced by the
	// field's JSON name. A json key replaces that tag; sgl.column directives take
	// precedence.
	StructTags map[string]string

	// SQLTypeName is the SQL type to override (e.g. "public.custom_type").
	SQLTypeName string

	// Column is the column to override, as "table.column" or "schema.table.column". A column
	// override changes the Go type of that column wherever a query returns it, as a
	// migration go_type column directive does, and ranks below such a directive.
	Column string

	// GoPackage is the Go import path for the target type.
	GoPackage string

	// GoName selects the override's Go type. Empty preserves the mapped type when only
	// NonNullElements or StructTags is configured.
	GoName string
}

// CustomFunctionConfig declares a function the dialect does not know, such as one a
// loadable extension provides, so that queries and views calling it can be analysed.
type CustomFunctionConfig struct {
	// Name is the SQL function name.
	Name string

	// ReturnType is the SQL type name of the return value.
	ReturnType string

	// Nullable specifies the null-handling behaviour. Valid values are "null_on_null"
	// (default), "never_null", and "called_on_null".
	Nullable string

	// DataAccess says whether a call can change data, which decides whether a query that
	// calls the function is routed to the reader or the writer. Valid values are "read_only"
	// (default) and "modifies_data".
	DataAccess string

	// Arguments lists the SQL type names for each parameter (e.g. "integer", "real", "text",
	// "blob", "any").
	Arguments []string

	// MinArguments is the minimum argument count when IsVariadic is true or when arguments
	// have defaults. Defaults to len(Arguments).
	MinArguments int

	// IsAggregate indicates whether the function is an aggregate.
	IsAggregate bool

	// IsVariadic indicates the last argument can repeat.
	IsVariadic bool
}
