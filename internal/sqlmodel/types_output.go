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

// GoCompositeKind says whether a composite Go type is a map, a struct or an instance of a
// generic type.
type GoCompositeKind uint8

const (
	// GoCompositeMap is a map type, map[Key]Value.
	GoCompositeMap GoCompositeKind = iota

	// GoCompositeStruct is an anonymous struct type with Fields.
	GoCompositeStruct

	// GoCompositeInstance is an instance of a generic type, Value[Arguments...], such as
	// opt.Option[uuid.UUID]: Value names the generic type and Arguments its type arguments,
	// in order.
	GoCompositeInstance
)

// GoType identifies a Go type by its import path and name, or describes a map, struct or
// generic instance built from other Go types.
type GoType struct {
	// Composite describes the map, struct or generic instance this Go type is built from, or
	// is nil for a named type. When it is set, Name holds any pointer and slice prefixes
	// wrapping the composite type, or is empty for the type itself, and Package is empty.
	Composite *GoComposite

	// Package is the Go import path (e.g. "github.com/google/uuid"), or empty for built-in
	// types (string, int64, etc.).
	Package string

	// Name is the Go type name (e.g. "Decimal", "string", "*int64").
	Name string
}

// NewGoMap returns the Go map type with the given key and value types.
//
// Takes key (GoType) which is the map's key type.
// Takes value (GoType) which is the map's value type.
//
// Returns GoType which is the map type.
func NewGoMap(key GoType, value GoType) GoType {
	return GoType{
		Composite: &GoComposite{Key: &key, Value: &value, Fields: nil, Arguments: nil, Kind: GoCompositeMap},
		Package:   "",
		Name:      "",
	}
}

// NewGoStruct returns the anonymous Go struct type with the given fields.
//
// Takes fields ([]GoField) which are the struct's fields, in order.
//
// Returns GoType which is the struct type.
func NewGoStruct(fields []GoField) GoType {
	return GoType{
		Composite: &GoComposite{Key: nil, Value: nil, Fields: fields, Arguments: nil, Kind: GoCompositeStruct},
		Package:   "",
		Name:      "",
	}
}

// NewGoInstance returns the instance of a generic Go type with the given type arguments,
// such as opt.Option[uuid.UUID].
//
// Takes generic (GoType) which is the generic type, by its package and name.
// Takes arguments (...GoType) which are the type arguments, in order.
//
// Returns GoType which is the instance.
func NewGoInstance(generic GoType, arguments ...GoType) GoType {
	return GoType{
		Composite: &GoComposite{Key: nil, Value: &generic, Fields: nil, Arguments: arguments, Kind: GoCompositeInstance},
		Package:   "",
		Name:      "",
	}
}

// GoComposite describes a Go map or struct type, or an instance of a generic type, whose
// parts are themselves Go types.
type GoComposite struct {
	// Key is the key type of a map.
	Key *GoType

	// Value is the value type of a map, or the generic type of an instance.
	Value *GoType

	// Fields are the fields of a struct, in order.
	Fields []GoField

	// Arguments are the type arguments of an instance of a generic type, in order.
	Arguments []GoType

	// Kind says whether the type is a map, a struct or an instance.
	Kind GoCompositeKind
}

// GoField is one field of a composite struct type.
type GoField struct {
	// Name is the field's exported Go name.
	Name string

	// Tag is the field's struct tag, without the back quotes, or empty for none.
	Tag string

	// Type is the field's Go type.
	Type GoType
}

// AnalysedQuery is the fully resolved result of query analysis, with all types and
// nullability propagated.
type AnalysedQuery struct {
	// Filename is the source SQL file path.
	Filename string

	// SQL is the original SQL text.
	SQL string

	// Name is the query name from the sgl.query directive.
	Name string

	// RowType is the name the query header's row: keyword gives the struct the query's rows
	// are read into, as written, which every query giving the same name shares, or empty
	// when the header gives none.
	RowType string

	// Description is the query's description: the prose comment lines of its header, above
	// or among its directive lines and before its SQL, without their comment markers, one
	// line per comment line and an empty line between paragraphs. It is empty when the
	// header holds no prose.
	Description string

	// CountTemplate is the SQL that counts the rows the query returns, for the runtime
	// builder's Count terminal, as segments with its own argument order. It is nil when
	// counting does not apply to the query.
	CountTemplate *SQLTemplate

	// InsertTarget is the table an INSERT writes, with the schema as the query writes it,
	// used by bulk-insert generation. It is zero for non-INSERT statements.
	InsertTarget TableName

	// RowModel is the table whose model the query's rows are read into, as the query
	// header's model: keyword names it, with the schema and name the catalogue holds it
	// under, or zero when the header names none.
	RowModel TableName

	// Template is the query's executable SQL as segments, which code generation renders for
	// its driver with emit_kit.Render. It is the zero template when the query has none.
	Template SQLTemplate

	// Parameters holds the fully typed query parameters.
	Parameters []QueryParameter

	// OutputColumns holds the fully typed output columns.
	OutputColumns []OutputColumn

	// GroupByKey holds the column(s) used for one-to-many grouping, if any.
	GroupByKey []string

	// InsertColumns holds the INSERT target column names recorded by the dialect.
	InsertColumns []string

	// AllowedColumns holds the columns that can be used in runtime WHERE and ORDER BY
	// clauses.
	AllowedColumns []AllowedColumn

	// Line is the line number of the query in the source file.
	Line int

	// Command is the execution pattern declared by the sgl.query directive.
	Command QueryCommand

	// IsDynamic indicates the query's method prepares its arguments at call time: it has a
	// sortable input, an optional parameter or a clamped LIMIT/OFFSET parameter.
	IsDynamic bool

	// DynamicRuntime indicates this query uses dynamic: runtime to generate a fluent runtime
	// builder instead of a standard method.
	DynamicRuntime bool

	// ReadOnly indicates the query does not modify data. Downstream consumers can use this
	// to route read-only queries to read replicas.
	ReadOnly bool

	// Optional reports that the query header set optional: true. For a command:one query it
	// makes a zero-row result return (row, false, nil) instead of the driver no-rows
	// sentinel; the emitter widens the method signature to (row, bool, error).
	Optional bool
}

// AllowedColumn represents a column available for runtime query building.
type AllowedColumn struct {
	// Name is the column's output (result-set) name, which is what a caller passes to the
	// runtime builder's Where and OrderBy methods.
	Name string

	// SourceExpression is the qualified source reference the builder emits into the SQL in
	// place of the caller's text (for example "users.email"). It is unambiguous across joins
	// and references the real column behind an aliased projection.
	SourceExpression string

	// SQLType is the column's resolved SQL type.
	SQLType SQLType
}

// OutputColumn is a fully typed output column.
type OutputColumn struct {
	// GoTypeOverride holds the explicit Go mapping, or is nil for the default mapping.
	//
	// GoTypeOverride, when non-nil, carries the custom Go destination type declared via a
	// sgl.column(name, go_type: ...) directive (either in the query header for a per-query
	// override or propagated from the catalogue for a migration-level override). When set,
	// the emitter uses this directly instead of mapping SQLType through the engine type
	// registry.
	GoTypeOverride *GoType

	// StructTags lists, by key, the struct tags the column's generated field carries besides
	// the json tag the style shapes: those of the catalogue column it reads, with a
	// query-level sgl.column(name, struct_tags: ...) directive's taking precedence key by
	// key. Nil gives none.
	StructTags map[string]string

	// Name is the column name or alias.
	Name string

	// SourceTable is the source table of a direct or cast-only column projection.
	SourceTable string

	// SourceSchema is the schema of SourceTable, when known, so a lookup against the
	// catalogue is unambiguous even when two schemas hold a table of the same name. Empty
	// for CTE/derived sources or when the schema could not be attributed.
	SourceSchema string

	// SourceColumn is the source column name of a direct or cast-only projection.
	SourceColumn string

	// SourceQualifier is the table reference that qualifies SourceColumn in the FROM clause.
	//
	// Holds the alias when the query aliased the table, otherwise the table or CTE name. The
	// dynamic runtime builder emits "<SourceQualifier>.<SourceColumn>" so a filter or
	// ordering on a projected column is unambiguous across joins and references the real
	// column rather than the caller's text. Empty when no qualifier is known.
	SourceQualifier string

	// EmbedTable is the table or view named by the embed directive that holds the column,
	// for embedded columns.
	EmbedTable string

	// EmbedName names the embed group that holds the column: the directive's as: value, or
	// EmbedTable when as: is absent.
	//
	// The emitter names the nested field and its struct type after it and groups an embed's
	// columns by it, so two embeds of one table (the two sides of a self-join) stay apart.
	// Empty for a column that is not embedded; an emitter reading an embedded column with an
	// empty EmbedName uses EmbedTable.
	EmbedName string

	// SQLType is the resolved SQL type.
	SQLType SQLType

	// Nullable indicates whether the column can be NULL, accounting for JOIN nullability,
	// expression nullability, and aggregate behaviour.
	Nullable bool

	// IsEmbedded indicates this column is part of a sgl.embed group.
	IsEmbedded bool

	// EmbedIsOuter indicates the embedded table was introduced via LEFT, RIGHT, or FULL
	// JOIN. When true, the emitter generates a pointer type for the embedded struct (nil
	// when no matching row exists).
	EmbedIsOuter bool
}

// QueryParameter is a fully typed query parameter.
type QueryParameter struct {
	// DefaultLimit holds the default value applied when the caller omits a numeric parameter
	// (e.g. a LIMIT page size), from sgl.param's default: quality.
	DefaultLimit *int

	// MaxLimit holds the inclusive maximum enforced at call time for a numeric parameter
	// (e.g. a LIMIT cap), from sgl.param's max: quality.
	MaxLimit *int

	// GoTypeOverride holds the explicit Go mapping, or is nil for the default mapping.
	//
	// GoTypeOverride, when non-nil, is the Go type a go_type override gives the column the
	// parameter is compared with or assigned to, so the parameter takes the same Go type as
	// the column. A cast to a different SQL type, a LIKE pattern or the parameter's own
	// type: directive leaves it nil.
	GoTypeOverride *GoType

	// NonNullElements records element nullability: true means non-null, false means
	// nullable, and nil uses the generation default. Parameter directives override column
	// settings; quantified comparisons require non-null elements.
	NonNullElements *bool

	// Name is the parameter name from sgl.param directive, or inferred from the column name.
	Name string

	// Placeholder identifies a named placeholder that a directive renamed. Empty means the
	// parameter uses its Name or binds by position.
	Placeholder string

	// SortableColumns holds the allowed ORDER BY column names from a sgl.sortable
	// directive's columns: option.
	SortableColumns []string

	// SQLType is the resolved SQL type.
	SQLType SQLType

	// Number is the positional parameter number.
	Number int

	// Nullable indicates whether the parameter accepts NULL values.
	Nullable bool

	// IsSlice indicates the parameter expands to multiple values (sgl.param kind: slice).
	IsSlice bool

	// IsOptional indicates the parameter accepts an omitted value, which binds NULL without
	// changing the SQL (sgl.param optional: true).
	IsOptional bool

	// Kind identifies the directive kind that declared this parameter.
	Kind ParameterDirectiveKind

	// Context records the SQL clause the parameter was found in. LIMIT/OFFSET contexts drive
	// integer typing, the non-negative clamp, and the generated `int` field type.
	Context ParameterContext
}

// IsPaginationBound reports whether the parameter binds a LIMIT or OFFSET clause, which
// makes it a non-negative integer with a generated `int` field and optional default/max
// clamping.
//
// Returns bool which is true when the parameter binds a LIMIT or OFFSET clause.
func (p *QueryParameter) IsPaginationBound() bool {
	return p.Context == ParameterContextLimit || p.Context == ParameterContextOffset
}

// GeneratedFile represents a single generated source file.
type GeneratedFile struct {
	// Name is the filename (e.g. "models.go", "users.sql.go").
	Name string

	// Content is the formatted source code.
	Content []byte
}
