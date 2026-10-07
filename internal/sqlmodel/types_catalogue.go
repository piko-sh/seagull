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
	// MaxArrayDimensions caps the array dimensions Seagull follows on a type: the dimensions
	// Column.ValueType wraps, and the [] suffixes of a type override. It bounds the
	// recursion over array types so a hostile schema or override cannot exhaust the stack or
	// memory.
	MaxArrayDimensions = 6
)

// GeneratedKind indicates how a generated column is stored.
type GeneratedKind uint8

const (
	// GeneratedKindNone indicates the column is not generated.
	GeneratedKindNone GeneratedKind = iota

	// GeneratedKindVirtual indicates a VIRTUAL generated column (computed on read, not
	// physically stored). This is the default in SQLite.
	GeneratedKindVirtual

	// GeneratedKindStored indicates a STORED generated column (computed on write and
	// physically stored).
	GeneratedKindStored
)

// FunctionOption sets an optional attribute of a function built by NewFunction.
type FunctionOption func(*Function)

// FunctionDataAccess describes whether a function may modify database state.
type FunctionDataAccess uint8

const (
	// DataAccessUnknown means the function's data access is not declared. Treated
	// conservatively as potentially modifying data.
	DataAccessUnknown FunctionDataAccess = iota

	// DataAccessReadOnly means the function does not modify data (PostgreSQL: IMMUTABLE or
	// STABLE; MySQL: NO SQL or READS SQL DATA).
	DataAccessReadOnly

	// DataAccessModifiesData means the function may modify data (PostgreSQL: VOLATILE
	// (default); MySQL: MODIFIES SQL DATA).
	DataAccessModifiesData
)

// FunctionArgument describes a single function parameter.
type FunctionArgument struct {
	// Name is the parameter name, if named.
	Name string

	// Type is the parameter type.
	Type SQLType

	// IsOptional indicates this argument has a default value and can be omitted. Optional
	// arguments must come after all required arguments.
	IsOptional bool

	// NotNull indicates the argument must not be NULL even when the called function accepts
	// NULL as a value at its other arguments, so a parameter bound to it is not nullable.
	NotNull bool
}

// FunctionNullableBehaviour describes how a function handles NULL inputs.
type FunctionNullableBehaviour uint8

const (
	// FunctionNullableCalledOnNull means the function is called even when arguments are
	// NULL. The result nullability depends on the function.
	FunctionNullableCalledOnNull FunctionNullableBehaviour = iota

	// FunctionNullableReturnsNullOnNull means NULL is returned when any argument is NULL
	// (SQL STRICT / RETURNS NULL ON NULL INPUT).
	FunctionNullableReturnsNullOnNull

	// FunctionNullableNeverNull means the function never returns NULL regardless of input
	// (e.g. COUNT).
	FunctionNullableNeverNull

	// FunctionNullableNullWhenArgumentNull means the called function returns NULL only when
	// the argument at Function.NullingArgument is NULL, and never because of any other
	// argument, as with a separator argument that decides the whole result.
	FunctionNullableNullWhenArgumentNull
)

// MigrationOrigin records which migration file introduced or last modified a catalogue
// object, enabling precise error messages that point back to the DDL source.
type MigrationOrigin struct {
	// Filename is the migration file that introduced or last modified this object (e.g.
	// "001_create_users.up.sql").
	Filename string

	// Index is the zero-based position among the up migrations replayed in numeric version
	// order.
	Index int

	// Statement is the zero-based position, among the migration file's statements, of the
	// statement that introduced or last modified the object, so objects can be ordered as
	// the migrations declare them.
	Statement int
}

// Catalogue represents the full schema state of a database, built from replaying
// migration files.
type Catalogue struct {
	// Schemas maps schema names to their contents.
	Schemas map[string]*Schema

	// Extensions tracks loaded extensions.
	Extensions map[string]struct{}

	// DefaultSchema is the schema an unqualified object is created in: the first entry of
	// SearchPath, or empty when the dialect has no named schemas.
	DefaultSchema string

	// SearchPath lists, in order, the schemas an unqualified name is looked up in. The
	// dialect supplies it; an empty entry stands for the schema of an engine whose current
	// schema has no fixed name.
	SearchPath []string

	// Identifiers is how the dialect compares identifiers. Schemas, tables, views, enums,
	// composite types, sequences, domains and functions are keyed by Identifiers.Key of
	// their names.
	Identifiers IdentifierCase
}

// Schema represents a single database schema containing tables, views, enums, functions,
// and types.
type Schema struct {
	// Tables maps table names to their definitions.
	Tables map[string]*Table

	// Views maps view names to their definitions.
	Views map[string]*View

	// Enums maps enum type names to their definitions.
	Enums map[string]*Enum

	// Functions maps function names, keyed as the catalogue's Identifiers.Key gives them, to
	// their overloaded signatures.
	Functions map[string][]*Function

	// CompositeTypes maps composite type names to their definitions.
	CompositeTypes map[string]*CompositeType

	// Sequences maps sequence names to their definitions.
	Sequences map[string]*Sequence

	// Domains maps domain names to their definitions.
	Domains map[string]*Domain

	// Name is the schema name.
	Name string
}

// Table represents a database table.
type Table struct {
	// Name is the table name.
	Name string

	// Schema is the schema the table belongs to.
	Schema string

	// Comment is the table comment, if any.
	Comment string

	// VirtualModuleName is the module name from CREATE VIRTUAL TABLE ... USING module(...),
	// or empty for non-virtual tables.
	VirtualModuleName string

	// Columns holds the table's columns in declaration order.
	Columns []Column

	// PrimaryKey holds the primary key columns, if defined.
	PrimaryKey []string

	// Indexes holds the table's indexes.
	Indexes []Index

	// Constraints holds the table's constraints.
	Constraints []Constraint

	// PartitionOf names the partitioned table this table is a partition of, by its schema
	// and name, or is nil for a table that is no partition. A partition is queried like any
	// table and holds its parent's columns; generated code models only the parent.
	PartitionOf *TableName

	// Origin records which migration introduced this table.
	Origin MigrationOrigin

	// IsVirtual indicates the table was created with CREATE VIRTUAL TABLE.
	IsVirtual bool

	// IsWithoutRowID indicates the table was created with WITHOUT ROWID.
	IsWithoutRowID bool
}

// View represents a database view.
type View struct {
	// Name is the view name.
	Name string

	// Schema is the schema the view belongs to.
	Schema string

	// Columns holds the view's output columns.
	Columns []Column

	// Definition is the SQL SELECT statement defining the view.
	Definition string

	// Comment is the view comment, if any.
	Comment string

	// Origin records which migration introduced this view.
	Origin MigrationOrigin
}

// Enum represents a user-defined enum type.
type Enum struct {
	// Name is the enum type name.
	Name string

	// Schema is the schema the enum belongs to.
	Schema string

	// Values holds the enum values in declaration order.
	Values []string

	// Comment is the enum type comment, if any.
	Comment string

	// Origin records which migration introduced this enum.
	Origin MigrationOrigin
}

// CompositeType represents a user-defined composite type.
type CompositeType struct {
	// Name is the composite type name.
	Name string

	// Schema is the schema the type belongs to.
	Schema string

	// Fields holds the type's fields.
	Fields []Column

	// Origin records which migration introduced this composite type.
	Origin MigrationOrigin
}

// Domain is a named type declared over a base type, such as CREATE DOMAIN email AS text
// NOT NULL. A column of a domain's type takes the base type, and cannot hold NULL when
// the domain is NOT NULL, as a live database describes it.
type Domain struct {
	// Name is the domain name.
	Name string

	// Schema is the schema the domain belongs to.
	Schema string

	// Origin records which migration introduced this domain.
	Origin MigrationOrigin

	// BaseType is the type the domain is declared over; for an array base type, the type of
	// its elements.
	BaseType SQLType

	// ArrayDimensions is the number of array dimensions of the base type.
	ArrayDimensions int

	// NotNull indicates the domain is declared NOT NULL, directly or through a domain it is
	// declared over.
	NotNull bool
}

// Sequence represents a database sequence.
type Sequence struct {
	// Name is the sequence name.
	Name string

	// Schema is the schema the sequence belongs to.
	Schema string

	// OwnedByTable is the table that owns this sequence (for serial columns).
	OwnedByTable string

	// OwnedByColumn is the column that owns this sequence.
	OwnedByColumn string

	// Columns holds the columns a query reading the sequence as a relation sees, such as
	// PostgreSQL's last_value, log_cnt and is_called, in order. It is empty when the engine
	// does not let a query read a sequence.
	Columns []Column

	// Origin records which migration introduced this sequence.
	Origin MigrationOrigin
}

// Index represents a database index.
type Index struct {
	// Name is the index name.
	Name string

	// Columns holds the indexed column names.
	Columns []string

	// Origin records which migration introduced this index.
	Origin MigrationOrigin

	// IsUnique indicates whether the index enforces uniqueness.
	IsUnique bool

	// IsPrimary indicates whether this is the primary key index.
	IsPrimary bool
}

// Constraint represents a database constraint.
type Constraint struct {
	// Name is the constraint name.
	Name string

	// ForeignTable is the referenced table for foreign key constraints.
	ForeignTable string

	// Columns holds the constrained column names.
	Columns []string

	// ForeignColumns holds the referenced columns for foreign key constraints.
	ForeignColumns []string

	// Origin records which migration introduced this constraint.
	Origin MigrationOrigin

	// Kind identifies the constraint type.
	Kind ConstraintKind
}

// ConstraintKind identifies the type of constraint.
type ConstraintKind uint8

const (
	// ConstraintPrimaryKey is a PRIMARY KEY constraint.
	ConstraintPrimaryKey ConstraintKind = iota

	// ConstraintForeignKey is a FOREIGN KEY constraint.
	ConstraintForeignKey

	// ConstraintUnique is a UNIQUE constraint.
	ConstraintUnique

	// ConstraintCheck is a CHECK constraint.
	ConstraintCheck

	// ConstraintNotNull is a NOT NULL constraint.
	ConstraintNotNull
)

// Column represents a column definition within a table.
type Column struct {
	// Name is the column name.
	Name string

	// Comment is the column comment, if any.
	Comment string

	// SQLTypeOverride, when non-empty, carries the engine-specific SQL type name declared
	// via a migration-level sgl.column(table.col, type: ...) directive. The query analyser
	// uses this to replace the engine-inferred SQLType on any output column that traces back
	// to this column via direct projection.
	SQLTypeOverride string

	// Origin records which migration introduced this column.
	Origin MigrationOrigin

	// GoTypeOverride, when non-nil, carries the import-path-qualified custom Go destination
	// type declared via a migration-level sgl.column(table.col, go_type: ...) directive.
	// Propagates to output columns that project this column unchanged.
	GoTypeOverride *GoType

	// NullableOverride is non-nil when a migration-level sgl.column directive declared an
	// explicit nullability for this column, overriding what the engine inferred from the
	// CREATE TABLE.
	NullableOverride *bool

	// NonNullElements controls whether this column's collection elements and map values can
	// be NULL. Nil defers to a type override or the preset.
	NonNullElements *bool

	// StructTags lists, by key, the struct tags every generated field holding the column
	// carries, from a migration-level sgl.column(table.col, struct_tags: ...) directive over
	// a column type override's, key by key. Propagates to output columns that read the
	// column.
	StructTags map[string]string

	// SQLType is the structured type of the column.
	SQLType SQLType

	// ArrayDimensions is the number of array dimensions. ValueType wraps at most
	// MaxArrayDimensions of them.
	ArrayDimensions int

	// Nullable indicates whether the column permits NULL values.
	Nullable bool

	// HasDefault indicates whether the column has a DEFAULT expression.
	HasDefault bool

	// IsGenerated indicates whether the column is a generated column.
	IsGenerated bool

	// GeneratedKind distinguishes VIRTUAL from STORED generated columns. Only meaningful
	// when IsGenerated is true.
	GeneratedKind GeneratedKind

	// IsArray indicates whether the column is an array type.
	IsArray bool

	// Hidden indicates the column can be named but is not part of the table's row: SELECT *
	// leaves it out, as it leaves out an FTS5 table's rank column.
	Hidden bool
}

// NewColumn returns a column of the given name, type, and nullability, with every other
// attribute unset.
//
// Takes name (string) which is the column's name.
// Takes sqlType (SQLType) which is the column's type.
// Takes nullable (bool) which reports whether the column permits NULL values.
//
// Returns Column which carries only the name, type, and nullability.
func NewColumn(name string, sqlType SQLType, nullable bool) Column {
	column := Column{}
	column.Name = name
	column.SQLType = sqlType
	column.Nullable = nullable
	return column
}

// ValueType returns a column's SQL type with its array dimensions applied.
//
// Type mapping uses this complete value type rather than the catalogue's separate element
// type and dimensions.
//
// Returns SQLType which is the value type.
func (c *Column) ValueType() SQLType {
	if c.ArrayDimensions <= 0 {
		return c.SQLType
	}
	wrapped := c.SQLType
	for range min(c.ArrayDimensions, MaxArrayDimensions) {
		inner := wrapped
		wrapped = SQLType{}
		wrapped.Category = TypeCategoryArray
		wrapped.EngineName = inner.EngineName + "[]"
		wrapped.ElementType = &inner
	}
	return wrapped
}

// Function describes a SQL function as the catalogue records it: its name, arguments,
// return type and behaviour. A built-in function and one a migration creates share this
// form; a migration's parsed function body stays with the dialect's definition of it and
// never reaches the catalogue.
type Function struct {
	// Name is the function name.
	Name string

	// Schema is the schema the function belongs to.
	Schema string

	// Language is the declared function language (e.g. "sql", "plpgsql", "c"), or empty if
	// not declared or not applicable.
	Language string

	// BodySQL holds the function's body as source text, as the migration wrote it.
	//
	// The catalogue builder analyses it to infer the return type of a LANGUAGE sql function
	// and whether any function writes data. Empty when the dialect does not capture the
	// body.
	BodySQL string

	// ResultName names the one column a call of the function as a FROM item gives when the
	// function names its one result itself, as a PostgreSQL function whose only output
	// parameter names it does. Empty names the column after the function.
	ResultName string

	// Arguments describes the function's parameters.
	Arguments []FunctionArgument

	// Origin records which migration introduced the function.
	Origin MigrationOrigin

	// ReturnType is the function's return type.
	ReturnType SQLType

	// NullingArgument is the zero-based position of the argument whose NULL makes the result
	// NULL, read only when NullableBehaviour is FunctionNullableNullWhenArgumentNull.
	NullingArgument int

	// ReturnTypeFromArgument is the one-based argument position supplying the result type.
	// Zero or an unknown argument type uses ReturnType.
	ReturnTypeFromArgument int

	// MinArguments is the minimum number of arguments a caller must supply; arguments beyond
	// this index are optional (they carry defaults).
	//
	// When left zero on a non-variadic signature, RequiredArguments derives the true minimum
	// from the arguments' IsOptional flags, so a genuinely-zero minimum (a leading optional
	// argument) is not conflated with an unpopulated field. Set it explicitly only to
	// override that derivation. On a variadic signature the value is always authoritative,
	// so zero means a call may pass no arguments at all.
	MinArguments int

	// ReturnsSet indicates whether a set of rows is returned.
	ReturnsSet bool

	// IsAggregate indicates whether the function is an aggregate.
	IsAggregate bool

	// IsStrict indicates that NULL is returned on any NULL argument. PostgreSQL: STRICT or
	// RETURNS NULL ON NULL INPUT.
	IsStrict bool

	// NullableBehaviour describes how the function handles NULL arguments.
	NullableBehaviour FunctionNullableBehaviour

	// DataAccess describes whether the function may modify data.
	//
	// The dialect supplies it for built-ins; declarations and body analysis determine it for
	// user-defined functions. DataAccessUnknown is treated as potentially modifying.
	DataAccess FunctionDataAccess

	// IsVariadic indicates the last argument can repeat zero or more times. When true, the
	// resolver matches any arity >= MinArguments.
	IsVariadic bool
}

// NewFunction returns a function taking arguments and returning returnType, with every
// other attribute unset unless an option sets it.
//
// Takes arguments ([]FunctionArgument) which are the declared arguments, or nil for none.
// Takes returnType (SQLType) which specifies the result type.
// Takes nullableBehaviour (FunctionNullableBehaviour) which says when the result is NULL.
// Takes options (...FunctionOption) which set any further attributes.
//
// Returns *Function which is the configured function.
func NewFunction(
	arguments []FunctionArgument,
	returnType SQLType,
	nullableBehaviour FunctionNullableBehaviour,
	options ...FunctionOption,
) *Function {
	function := &Function{}
	function.Arguments = arguments
	function.ReturnType = returnType
	function.NullableBehaviour = nullableBehaviour
	for _, option := range options {
		option(function)
	}
	return function
}

// RequiredArguments returns the fewest arguments a call to the function must pass.
//
// MinArguments is authoritative when it is set or the function is variadic. Otherwise the
// minimum is the count of leading arguments without a default: optional arguments must
// trail the required ones, so the first optional argument marks it, and a list with no
// optional arguments requires all of them.
//
// Returns int which is the fewest arguments a call must pass.
func (f *Function) RequiredArguments() int {
	if f.MinArguments != 0 || f.IsVariadic {
		return f.MinArguments
	}
	for index := range f.Arguments {
		if f.Arguments[index].IsOptional {
			return index
		}
	}
	return len(f.Arguments)
}

// WithFunctionName names a function.
//
// Takes name (string) which is the function's name.
//
// Returns FunctionOption which sets the name.
func WithFunctionName(name string) FunctionOption {
	return func(function *Function) {
		function.Name = name
	}
}

// WithReturnTypeFromArgument makes a function's result take the type of one of its
// arguments, as a polymorphic function's does.
//
// Takes position (int) which is the one-based position of the argument.
//
// Returns FunctionOption which sets ReturnTypeFromArgument.
func WithReturnTypeFromArgument(position int) FunctionOption {
	return func(function *Function) {
		function.ReturnTypeFromArgument = position
	}
}

// WithAggregate marks a function as an aggregate function.
//
// Returns FunctionOption which sets IsAggregate.
func WithAggregate() FunctionOption {
	return func(function *Function) {
		function.IsAggregate = true
	}
}

// WithVariadic marks a function as variadic, accepting at least minArguments arguments.
//
// Takes minArguments (int) which is the fewest arguments a call may pass.
//
// Returns FunctionOption which sets IsVariadic and MinArguments.
func WithVariadic(minArguments int) FunctionOption {
	return func(function *Function) {
		function.IsVariadic = true
		function.MinArguments = minArguments
	}
}

// WithNullingArgument makes a function NULL only when the argument at a position is NULL,
// whatever its other arguments: its NullableBehaviour becomes
// FunctionNullableNullWhenArgumentNull.
//
// Takes position (int) which is the zero-based position of the deciding argument.
//
// Returns FunctionOption which sets NullableBehaviour and NullingArgument.
func WithNullingArgument(position int) FunctionOption {
	return func(function *Function) {
		function.NullableBehaviour = FunctionNullableNullWhenArgumentNull
		function.NullingArgument = position
	}
}

// WithMinArguments sets the fewest arguments a call to a non-variadic function may pass,
// for functions whose trailing arguments are optional.
//
// Takes minArguments (int) which is the fewest arguments a call may pass.
//
// Returns FunctionOption which sets MinArguments.
func WithMinArguments(minArguments int) FunctionOption {
	return func(function *Function) {
		function.MinArguments = minArguments
	}
}

// WithReturnsSet marks a function as returning a set of rows.
//
// Returns FunctionOption which sets ReturnsSet.
func WithReturnsSet() FunctionOption {
	return func(function *Function) {
		function.ReturnsSet = true
	}
}
