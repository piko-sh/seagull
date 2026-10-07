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
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// RoleQuery is the role name of the top-level query header.
	RoleQuery = "query"

	// RoleEmbed is the role name of the embed header.
	RoleEmbed = "embed"

	// RoleColumn is the role name of the column override header.
	RoleColumn = "column"

	// RoleMigration is the role name of the per-block migration directive.
	RoleMigration = "migration"

	// KeywordArgumentString accepts any bare word or quoted string literal.
	KeywordArgumentString = syntax.KeywordArgumentString

	// KeywordArgumentBool accepts true or false.
	KeywordArgumentBool = syntax.KeywordArgumentBool

	// KeywordArgumentInt accepts an integer literal.
	KeywordArgumentInt = syntax.KeywordArgumentInt

	// KeywordArgumentIdentifier accepts a bare identifier; when AllowedValues is set the
	// identifier must match one of the listed values (case-insensitive).
	KeywordArgumentIdentifier = syntax.KeywordArgumentIdentifier

	// KeywordArgumentList accepts a bracketed list of bare identifiers (e.g. columns: [name,
	// price, created_at]).
	KeywordArgumentList = syntax.KeywordArgumentList

	// KeywordArgumentQualifiedIdentifier accepts a dotted identifier such as `orders.id`.
	KeywordArgumentQualifiedIdentifier = syntax.KeywordArgumentQualifiedIdentifier

	// KeywordArgumentGoName accepts a name the generated code can export: a letter with an
	// upper-case form, then letters, digits and underscores, such as GetUser or get_user.
	KeywordArgumentGoName = syntax.KeywordArgumentGoName

	// roleParam is the role name of a parameter binding.
	roleParam = "param"

	// roleSortable is the role name of the standalone sortable header.
	roleSortable = "sortable"
)

const (
	// DirectiveRoleTop is the top-level query header `-- sgl.query(...)`.
	DirectiveRoleTop DirectiveRole = iota

	// DirectiveRoleParam is a parameter binding line such as `-- $1 as sgl.param(name)`.
	DirectiveRoleParam

	// DirectiveRoleHeader is a header-level directive such as `-- sgl.embed(table, from: o)`
	// that is not a parameter binding.
	DirectiveRoleHeader

	// DirectiveRoleMigration is a directive recognised inside .up.sql migration files
	// preceding CREATE FUNCTION statements.
	DirectiveRoleMigration
)

var (
	// commandEnumValues holds the literals accepted by the top-level `command:` keyword
	// argument.
	//
	// They are kept in declaration order so suggestions list them predictably. The asyncexec
	// command marks fire-and-forget mutations whose completion the server reports
	// asynchronously (for example ClickHouse ALTER UPDATE and DELETE).
	commandEnumValues = []string{
		"one", "many", "exec", "execresult", "execrows",
		"batch", "stream", "copyfrom", "asyncexec",
	}

	// dynamicEnumValues are the accepted values for the `dynamic:` keyword argument.
	dynamicEnumValues = []string{"runtime"}

	// boolEnumValues are the accepted values for KeywordArgumentBool keyword arguments.
	boolEnumValues = []string{"true", "false"}

	// kindEnumValues are the accepted values for the param `kind:` keyword argument (the
	// parameter's cardinality). The default when omitted is scalar.
	kindEnumValues = []string{"scalar", "slice"}

	// directiveSpecs is the registry of every directive the parser recognises. Lookups go
	// through lookupDirective; the slice form is kept so suggestions list the directives in
	// declaration order.
	directiveSpecs = []DirectiveSpec{
		{
			// Top-level query declaration. Required on every query header.
			Name: RoleQuery,
			Role: DirectiveRoleTop,
			Positionals: []PositionalSpec{
				{
					// Go method name for the generated query: a letter, then letters, digits and
					// underscores.
					Name:          "name",
					Required:      true,
					Kind:          KeywordArgumentGoName,
					AllowedValues: nil,
				},
				{
					// Execution pattern
					// (one/many/exec/execresult/execrows/batch/stream/copyfrom/asyncexec).
					Name:          "command",
					Required:      true,
					Kind:          KeywordArgumentIdentifier,
					AllowedValues: commandEnumValues,
				},
			},
			KeywordArguments: []KeywordArgumentSpec{
				{
					// Emit a fluent runtime query builder instead of a static method.
					Name:          "dynamic",
					Kind:          KeywordArgumentIdentifier,
					AllowedValues: dynamicEnumValues,
					Required:      false,
				},
				{
					// Override automatic read-only detection.
					Name:          "readonly",
					Kind:          KeywordArgumentBool,
					AllowedValues: boolEnumValues,
					Required:      false,
				},
				{
					// Override automatic nullability propagation across the output columns.
					Name:          "nullable",
					Kind:          KeywordArgumentBool,
					AllowedValues: boolEnumValues,
					Required:      false,
				},
				{
					// For command:one, return (row, false, nil) instead of the no-rows sentinel.
					Name:          "optional",
					Kind:          KeywordArgumentBool,
					AllowedValues: boolEnumValues,
					Required:      false,
				},
				{
					// Declare the grouping column for one-to-many embed joins.
					Name:          "group_by",
					Kind:          KeywordArgumentQualifiedIdentifier,
					AllowedValues: nil,
					Required:      false,
				},
				{
					// Read the rows into the model of this table (or schema.table) rather than a Row
					// struct of the query's own; the query's columns must be the model's fields.
					// Excludes row.
					Name:          KeywordModel,
					Kind:          KeywordArgumentQualifiedIdentifier,
					AllowedValues: nil,
					Required:      false,
				},
				{
					// Name the struct the rows are read into; queries naming the same row share one
					// struct, so their columns must match. Excludes model.
					Name:          KeywordRow,
					Kind:          KeywordArgumentGoName,
					AllowedValues: nil,
					Required:      false,
				},
			},
			ParamKind: sqlmodel.ParameterDirectiveNone,
		},
		{
			// Bind a positional or named placeholder to a Go parameter.
			Name:      roleParam,
			Role:      DirectiveRoleParam,
			ParamKind: sqlmodel.ParameterDirectiveParam,
			Positionals: []PositionalSpec{
				{
					// The Go identifier for the parameter.
					Name:          "name",
					Required:      true,
					Kind:          KeywordArgumentIdentifier,
					AllowedValues: nil,
				},
			},
			KeywordArguments: []KeywordArgumentSpec{
				{
					// Explicit SQL type that overrides inference.
					Name:          "type",
					Kind:          KeywordArgumentIdentifier,
					AllowedValues: nil,
					Required:      false,
				},
				{
					// Override the inferred nullability for this parameter.
					Name:          "nullable",
					Kind:          KeywordArgumentBool,
					AllowedValues: boolEnumValues,
					Required:      false,
				},
				{
					// Default integer for an omitted numeric (pagination) parameter, e.g. a LIMIT
					// default. Excludes optional.
					Name:          "default",
					Kind:          KeywordArgumentInt,
					AllowedValues: nil,
					Required:      false,
				},
				{
					// Make the Go value a pointer the caller may pass as nil, which binds NULL; the SQL
					// says what NULL means, as in ($1 IS NULL OR col = $1); excludes default.
					Name:          "optional",
					Kind:          KeywordArgumentBool,
					AllowedValues: boolEnumValues,
					Required:      false,
				},
				{
					// Cardinality: scalar (default) or slice (expands to IN (...) at call time).
					Name:          "kind",
					Kind:          KeywordArgumentIdentifier,
					AllowedValues: kindEnumValues,
					Required:      false,
				},
				{
					// Inclusive upper bound enforced at call time for a numeric parameter (e.g. a LIMIT
					// cap).
					Name:          "max",
					Kind:          KeywordArgumentInt,
					AllowedValues: nil,
					Required:      false,
				},
				{
					// Declare the parameter's array or list elements and map values non- null (true), so
					// they are not pointers, or nullable (false), where the engine can hold NULL there.
					Name:          keywordNonNullElements,
					Kind:          KeywordArgumentBool,
					AllowedValues: boolEnumValues,
					Required:      false,
				},
			},
		},
		{
			// Validated dynamic ORDER BY with a closed column allow-list. Standalone: it does not
			// bind a placeholder, so it is not written with the $N as prefix.
			Name:      roleSortable,
			Role:      DirectiveRoleHeader,
			ParamKind: sqlmodel.ParameterDirectiveSortable,
			Positionals: []PositionalSpec{
				{
					// The Go identifier for the sortable input.
					Name:          "name",
					Required:      true,
					Kind:          KeywordArgumentIdentifier,
					AllowedValues: nil,
				},
				{
					// Closed list of column names the caller may sort by.
					Name:          "columns",
					Required:      true,
					Kind:          KeywordArgumentList,
					AllowedValues: nil,
				},
			},
			KeywordArguments: nil,
		},
		{
			// Group projection columns into a nested Go struct.
			Name: RoleEmbed,
			Role: DirectiveRoleHeader,
			Positionals: []PositionalSpec{
				{
					// Source table or view whose columns are embedded.
					Name:          "table",
					Required:      true,
					Kind:          KeywordArgumentIdentifier,
					AllowedValues: nil,
				},
			},
			KeywordArguments: []KeywordArgumentSpec{
				{
					// FROM-clause alias whose columns belong to this embed group; required when the
					// table appears more than once.
					Name:          "from",
					Kind:          KeywordArgumentIdentifier,
					AllowedValues: nil,
					Required:      false,
				},
				{
					// Name of the generated field and struct type for the embed; defaults to the table.
					Name:          "as",
					Kind:          KeywordArgumentIdentifier,
					AllowedValues: nil,
					Required:      false,
				},
			},
			ParamKind: sqlmodel.ParameterDirectiveNone,
		},
		{
			// Override the inferred SQL type, custom Go destination type, nullability or struct
			// tags of one column. Valid above a SELECT (where the positional is an output column
			// name) or above a CREATE TABLE in a migration file (where the positional is a
			// qualified table.column name).
			Name:             RoleColumn,
			Role:             DirectiveRoleHeader,
			Positionals:      syntax.ColumnCallSpec().Positionals,
			KeywordArguments: syntax.ColumnCallSpec().KeywordArguments,
			ParamKind:        sqlmodel.ParameterDirectiveNone,
		},
		{
			// Per-block migration directive. Placed above the statement it configures: readonly:
			// above a CREATE FUNCTION marks the function read-only in the catalogue;
			// no_transaction: above any statement runs the migration outside a transaction.
			Name:             RoleMigration,
			Role:             DirectiveRoleMigration,
			KeywordArguments: syntax.MigrationCallSpec().KeywordArguments,
			Positionals:      nil,
			ParamKind:        sqlmodel.ParameterDirectiveNone,
		},
	}

	// directiveSpecsByName indexes directiveSpecs by role name.
	//
	// It gives O(1) lookup, and lookupDirective resolves through it. It is built once at
	// package initialisation; Go's variable-initialisation order guarantees directiveSpecs
	// is populated first because this initialiser depends on it. Each value is a pointer
	// into the directiveSpecs backing array so callers observe the same spec instance the
	// slice form exposes.
	directiveSpecsByName = buildDirectiveSpecsByName()
)

// DirectiveRole identifies where a directive appears in a SQL file and what shape its
// grammar takes.
type DirectiveRole uint8

// KeywordArgumentKind classifies the value shape a keyword argument accepts.
type KeywordArgumentKind = syntax.KeywordArgumentKind

// PositionalSpec describes one positional argument a directive accepts. Each positional
// has a name and may be passed positionally or by name; the parser routes a keyword
// argument whose key matches a positional name into the corresponding slot.
type PositionalSpec = syntax.PositionalSpec

// KeywordArgumentSpec describes one accepted keyword argument on a directive.
type KeywordArgumentSpec = syntax.KeywordArgumentSpec

// DirectiveSpec is the single source of truth for everything the parser and the validator
// know about one directive shape.
type DirectiveSpec struct {
	// Name is the directive's role without a prefix, such as query or param. It is the
	// registry key.
	Name string

	// Positionals lists the positional arguments in declaration order.
	//
	// Each may also be passed by name. Nil or empty means the directive accepts no
	// positionals.
	Positionals []PositionalSpec

	// KeywordArguments lists the keyword argument-only arguments in display order. These
	// cannot be passed positionally.
	KeywordArguments []KeywordArgumentSpec

	// Role categorises where the directive may appear.
	Role DirectiveRole

	// ParamKind is the parameter kind the directive declares: the standard kind for a param
	// binding, the sortable kind for sortable, or ParameterDirectiveNone for a directive
	// that declares no parameter.
	ParamKind sqlmodel.ParameterDirectiveKind
}

// LookupKeywordArgument resolves a keyword argument key against a directive spec.
//
// Takes spec (*DirectiveSpec) which holds the keyword arguments to search.
// Takes key (string) which is the keyword argument key to resolve.
//
// Returns *KeywordArgumentSpec which is the matching keyword argument, or nil when none
// matches.
// Returns bool which is true when a keyword argument was found.
func LookupKeywordArgument(spec *DirectiveSpec, key string) (*KeywordArgumentSpec, bool) {
	if spec == nil {
		return nil, false
	}
	for index := range spec.KeywordArguments {
		if spec.KeywordArguments[index].Name == key {
			return &spec.KeywordArguments[index], true
		}
	}
	return nil, false
}

// Roles returns the role name of every directive, without a prefix, such as query and
// param.
//
// Returns []string which holds the role names in declaration order.
func Roles() []string {
	roles := make([]string, len(directiveSpecs))
	for index := range directiveSpecs {
		roles[index] = directiveSpecs[index].Name
	}
	return roles
}

// buildDirectiveSpecsByName indexes the directiveSpecs registry by role name.
//
// Returns map[string]*DirectiveSpec which maps each directive's role Name to its spec
// pointer.
func buildDirectiveSpecsByName() map[string]*DirectiveSpec {
	index := make(map[string]*DirectiveSpec, len(directiveSpecs))
	for position := range directiveSpecs {
		index[directiveSpecs[position].Name] = &directiveSpecs[position]
	}
	return index
}

// lookupDirective resolves a directive role name (for example "query" or "embed") to its
// spec.
//
// Lookups are case-sensitive because the grammar only accepts the lower-case form.
// Resolution is an O(1) map lookup over the directiveSpecsByName index.
//
// Takes name (string) which is the role name to resolve, without any prefix.
//
// Returns *DirectiveSpec which is the matching spec, or nil when none matches.
// Returns bool which is true when a spec was found.
func lookupDirective(name string) (*DirectiveSpec, bool) {
	spec, found := directiveSpecsByName[name]
	return spec, found
}

// lookupPositional resolves a positional by name.
//
// It is used when a keyword argument key matches one of the directive's positional slots
// (keyword argument-as-positional).
//
// Takes spec (*DirectiveSpec) which holds the positionals to search.
// Takes name (string) which is the positional name to resolve.
//
// Returns *PositionalSpec which is the matching positional, or nil when none matches.
// Returns int which is the positional's index in the Positionals slice.
// Returns bool which is true when a positional was found.
func lookupPositional(spec *DirectiveSpec, name string) (*PositionalSpec, int, bool) {
	if spec == nil {
		return nil, 0, false
	}
	for index := range spec.Positionals {
		if spec.Positionals[index].Name == name {
			return &spec.Positionals[index], index, true
		}
	}
	return nil, 0, false
}

// positionalNames returns the names of each positional in declaration order.
//
// It is used by the validator's "did you mean" suggestion helper for keyword
// argument-as-positional name typos.
//
// Takes spec (*DirectiveSpec) which holds the positionals to name.
//
// Returns []string which are the positional names in declaration order, or nil when spec
// is nil.
func positionalNames(spec *DirectiveSpec) []string {
	if spec == nil {
		return nil
	}
	names := make([]string, len(spec.Positionals))
	for index := range spec.Positionals {
		names[index] = spec.Positionals[index].Name
	}
	return names
}

// directiveNames returns the name of every directive in the registry, qualified with the
// canonical prefix of prefixes.
//
// It is used by the validator's "did you mean" suggestion helper.
//
// Takes prefixes (namespace.Set) whose canonical prefix qualifies each name.
//
// Returns []string which are the qualified directive names in declaration order.
func directiveNames(prefixes namespace.Set) []string {
	names := make([]string, len(directiveSpecs))
	for index := range directiveSpecs {
		names[index] = prefixes.Qualify(directiveSpecs[index].Name)
	}
	return names
}

// keywordArgumentNames returns the keyword argument keys accepted by the directive spec.
//
// It is used by the validator's "did you mean" suggestion helper.
//
// Takes spec (*DirectiveSpec) which holds the keyword arguments to name.
//
// Returns []string which are the keyword argument keys in display order, or nil when spec
// is nil.
func keywordArgumentNames(spec *DirectiveSpec) []string {
	if spec == nil {
		return nil
	}
	names := make([]string, len(spec.KeywordArguments))
	for index := range spec.KeywordArguments {
		names[index] = spec.KeywordArguments[index].Name
	}
	return names
}
