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

package directive

import (
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/sqlmodel"
)

// DirectiveBlock is the root AST node for all directives in a query header. It holds the
// fully parsed representation of the -- sgl.query(...) comment lines that precede a SQL
// statement.
type DirectiveBlock struct {
	// Name holds the positional or keyword name argument from sgl.query, or nil when absent.
	Name *NameDirective

	// Command holds the positional or keyword command argument from sgl.query, or nil when
	// absent or invalid.
	Command *CommandDirective

	// Description holds header prose without comment markers or trailing spaces, preserving
	// line breaks and paragraph separators. Directive lines and their continuations are
	// excluded; headers without prose yield an empty string.
	Description string

	// Parameters holds placeholder-binding directives and standalone sgl.sortable inputs.
	Parameters []*ParameterDirective

	// Metadata holds the list of parsed metadata directives produced from query-level
	// keyword arguments (group_by, dynamic, readonly, nullable).
	Metadata []*MetadataDirective

	// Embeds holds the parsed -- sgl.embed(table, from: alias) header directives.
	Embeds []*EmbedDirective

	// ColumnOverrides holds the parsed -- sgl.column(name, ...) header directives. Each
	// entry overrides the inferred SQL type, custom Go destination type, or nullability of
	// one output column.
	ColumnOverrides []*ColumnOverride

	// Span holds the source range covering all directives in this block.
	Span diagbuild.TextSpan
}

// ColumnOverride represents one -- sgl.column(name, type:, go_type:, nullable:,
// non_null_elements:, struct_tags:) directive. Populated by applyHeaderCall in the
// directive parser; consumed by the query analyser after output-column inference and by
// the catalogue builder when the directive is parsed from a migration file.
//
// Name is either an output column name (when the directive appears in a query header) or
// a qualified table.column name (when it appears in a migration file above a CREATE
// TABLE). The consumer distinguishes by the directive's position relative to surrounding
// SQL.
type ColumnOverride struct {
	// Nullable is non-nil when the `nullable:` keyword argument was declared.
	Nullable *bool

	// NonNullElements is non-nil when the `non_null_elements:` keyword argument was
	// declared.
	NonNullElements *bool

	// ParsedGoType holds GoType as ParseGoTypeOverride reads it, so a consumer need not
	// parse it again, or is nil when there is no go_type or the generator cannot use it.
	ParsedGoType *sqlmodel.GoType

	// StructTags holds the struct tags the `struct_tags:` keyword argument declares, by key,
	// or is nil when it was not declared or could not be read.
	StructTags map[string]string

	// Name is the output column or table.column reference targeted by the override.
	Name string

	// SQLType carries the `type:` keyword argument value when set. When empty, no SQL-type
	// override was declared.
	SQLType string

	// GoType carries the `go_type:` keyword argument value when set.
	//
	// ParseGoTypeOverride reads the type name, optional import path, pointer or slice
	// prefixes and generic arguments. When empty, no Go-type override was declared.
	GoType string

	// Span covers the column call's parentheses and their contents.
	Span diagbuild.TextSpan

	// NameSpan holds the source range of the positional or keyword name value.
	NameSpan diagbuild.TextSpan

	// TypeSpan holds the source range of the `type:` keyword-argument value when present.
	TypeSpan diagbuild.TextSpan

	// GoTypeSpan holds the source range of the `go_type:` keyword- argument value when
	// present.
	GoTypeSpan diagbuild.TextSpan
}

// EmbedDirective describes a request to group a table's projected columns into a nested
// Go struct.
//
// From selects the table reference, and As sets the generated field and type name.
type EmbedDirective struct {
	// Table is the source table or view whose columns are embedded.
	Table string

	// From is the FROM-clause alias whose columns belong to this embed group. Empty when the
	// table is referenced once and its alias is taken from the FROM clause.
	From string

	// As overrides the Go field and type name; empty defaults to the table.
	As string

	// Span covers the embed call's parentheses and their contents.
	Span diagbuild.TextSpan

	// TableSpan holds the source range of the positional or keyword table value.
	TableSpan diagbuild.TextSpan

	// FromSpan holds the source range of the from: value, or is zero when From is empty.
	FromSpan diagbuild.TextSpan

	// AsSpan holds the source range of the as: value, or is zero when As is empty.
	AsSpan diagbuild.TextSpan
}

// NameDirective records the name argument of a sgl.query directive.
type NameDirective struct {
	// Value holds the name string specified in the directive.
	Value string

	// Span holds the source range of the positional or keyword argument.
	Span diagbuild.TextSpan

	// KeySpan locates the name keyword, or the value when it is positional.
	KeySpan diagbuild.TextSpan

	// ValueSpan holds the source range of the name value.
	ValueSpan diagbuild.TextSpan
}

// CommandDirective records the command argument of a sgl.query directive.
type CommandDirective struct {
	// Value holds the raw command string specified in the directive.
	Value string

	// Span holds the source range of the positional or keyword argument.
	Span diagbuild.TextSpan

	// KeySpan locates the command keyword, or the value when it is positional.
	KeySpan diagbuild.TextSpan

	// ValueSpan holds the source range of the command value.
	ValueSpan diagbuild.TextSpan

	// Command holds the parsed query command enumeration value.
	Command sqlmodel.QueryCommand
}

// ParameterDirective represents a parameter directive such as `-- $1 as
// sgl.param(page_size)` or `-- :email as sgl.param`.
type ParameterDirective struct {
	// TypeHint holds the optional SQL type hint for this parameter, or nil if unspecified.
	TypeHint *string

	// Nullable holds the optional nullability override, or nil if unspecified.
	Nullable *bool

	// DefaultValue holds the optional default value applied when the caller omits a numeric
	// parameter (e.g. a LIMIT page size), or nil if unspecified.
	DefaultValue *int

	// MaxValue holds the optional inclusive maximum enforced at call time for a numeric
	// parameter (e.g. a LIMIT cap), or nil if unspecified.
	MaxValue *int

	// NonNullElements holds what the `non_null_elements:` keyword argument declares of the
	// parameter's array elements and map values: true for non-null, false for nullable, or
	// nil when it was not declared.
	NonNullElements *bool

	// Name holds the declared parameter name.
	Name string

	// Anchor identifies the placeholder bound by a named parameter directive, even when Name
	// renames it. Empty marks a positional anchor.
	Anchor string

	// DirectiveName holds the raw directive role, such as "param" or "sortable".
	DirectiveName string

	// Columns holds the list of column names for sortable parameters.
	Columns []string

	// Number holds the positional parameter number (e.g. 1 for ?1).
	Number int

	// Span holds the source range of the entire parameter directive.
	Span diagbuild.TextSpan

	// NumberSpan locates the positional or named placeholder anchor, or is zero for a
	// sortable input.
	NumberSpan diagbuild.TextSpan

	// KindSpan locates the qualified directive role (e.g. "sgl.param"), or the call's
	// parentheses and contents for a sortable input.
	KindSpan diagbuild.TextSpan

	// NameSpan holds the source range of the parameter name token.
	NameSpan diagbuild.TextSpan

	// Kind holds the parsed parameter directive kind enumeration value.
	Kind sqlmodel.ParameterDirectiveKind

	// IsNamed indicates whether this parameter uses named (:name) rather than positional
	// (?N) syntax.
	IsNamed bool

	// IsOptional indicates the parameter may be omitted (the `optional: true` quality);
	// supplying nil binds NULL without removing its predicate.
	IsOptional bool

	// IsSlice indicates the parameter expands to multiple placeholders at call time (the
	// `kind: slice` quality).
	IsSlice bool
}

// MetadataDirective records a query-header keyword argument, such as group_by or
// nullable, or an embed declaration mirrored into the metadata list.
type MetadataDirective struct {
	// Directive holds the directive name string (e.g. "group_by", "nullable").
	Directive string

	// Value holds the directive value string.
	Value string

	// Span holds the source range of the keyword argument or embed call's parentheses.
	Span diagbuild.TextSpan

	// KeySpan holds the keyword's source range, or is zero for an embed declaration.
	KeySpan diagbuild.TextSpan

	// ValueSpan holds the value's source range, or is zero for an embed declaration.
	ValueSpan diagbuild.TextSpan
}
