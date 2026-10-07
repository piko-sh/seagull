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

package syntax

const (
	// keywordName is the key that fills a directive's name slot by name.
	keywordName = "name"

	// keywordType is the column directive keyword that overrides the column's SQL type.
	keywordType = "type"

	// keywordGoType is the column directive keyword that overrides the column's Go type.
	keywordGoType = "go_type"

	// keywordNullable is the column directive keyword that overrides the column's
	// nullability.
	keywordNullable = "nullable"

	// keywordReadOnly is the migration directive keyword that marks the function the next
	// statement creates read-only or modifying.
	keywordReadOnly = "readonly"

	// keywordNoTransaction is the migration directive keyword that runs the migration
	// outside a transaction.
	keywordNoTransaction = "no_transaction"

	// literalTrue is the value that sets a boolean argument.
	literalTrue = "true"

	// literalFalse is the value that clears a boolean argument.
	literalFalse = "false"
)

const (
	// KeywordArgumentString accepts any bare word or quoted string literal.
	KeywordArgumentString KeywordArgumentKind = iota

	// KeywordArgumentBool accepts true or false.
	KeywordArgumentBool

	// KeywordArgumentInt accepts an integer literal.
	KeywordArgumentInt

	// KeywordArgumentIdentifier accepts a bare identifier; when AllowedValues is set the
	// identifier must match one of the listed values (case-insensitive).
	KeywordArgumentIdentifier

	// KeywordArgumentList accepts a bracketed list of bare identifiers (e.g. columns: [name,
	// price, created_at]).
	KeywordArgumentList

	// KeywordArgumentQualifiedIdentifier accepts a dotted identifier such as `orders.id`.
	KeywordArgumentQualifiedIdentifier

	// KeywordArgumentGoName accepts a name the generated code can export: a letter with an
	// upper-case form, then letters, digits and underscores, such as GetUser or get_user.
	KeywordArgumentGoName
)

var (
	// booleanValues are the values a KeywordArgumentBool argument accepts.
	booleanValues = []string{literalTrue, literalFalse}

	// migrationCallSpec declares the arguments of the per-block migration directive, which
	// takes keyword arguments only.
	migrationCallSpec = CallSpec{
		Positionals: nil,
		KeywordArguments: []KeywordArgumentSpec{
			{
				// Mark the following CREATE FUNCTION read-only (true) or modifying (false) in the
				// catalogue.
				Name:          keywordReadOnly,
				Kind:          KeywordArgumentBool,
				AllowedValues: booleanValues,
				Required:      false,
			},
			{
				// Run the migration outside a transaction (e.g. CREATE INDEX CONCURRENTLY).
				Name:          keywordNoTransaction,
				Kind:          KeywordArgumentBool,
				AllowedValues: booleanValues,
				Required:      false,
			},
		},
	}

	// columnCallSpec declares the arguments of the column override directive, in a query
	// header and in a migration alike.
	columnCallSpec = CallSpec{
		Positionals: []PositionalSpec{
			{
				// Output column name (in a query header) or qualified table.column name (in a
				// migration file).
				Name:          keywordName,
				Required:      true,
				Kind:          KeywordArgumentQualifiedIdentifier,
				AllowedValues: nil,
			},
		},
		KeywordArguments: []KeywordArgumentSpec{
			{
				// SQL type that overrides the analyser's inference. Mapped to Go through the engine's
				// type registry. Mutually exclusive with go_type.
				Name:          keywordType,
				Kind:          KeywordArgumentIdentifier,
				AllowedValues: nil,
				Required:      false,
			},
			{
				// Explicit Go destination type with import path (e.g. github.com/google/uuid.UUID).
				// The package is imported automatically. Mutually exclusive with type.
				Name:          keywordGoType,
				Kind:          KeywordArgumentString,
				AllowedValues: nil,
				Required:      false,
			},
			{
				// Override the inferred nullability of this column.
				Name:          keywordNullable,
				Kind:          KeywordArgumentBool,
				AllowedValues: booleanValues,
				Required:      false,
			},
			{
				// Declare the column's array or list elements and map values non-null (true), so they
				// are not pointers, or nullable (false), where the engine can hold NULL there.
				Name:          keywordNonNullElements,
				Kind:          KeywordArgumentBool,
				AllowedValues: booleanValues,
				Required:      false,
			},
			{
				// Struct tags for the column's generated field, written as Go writes them (e.g.
				// 'validate:\"required\" db:\"email\"'); a json tag among them replaces the generated
				// one.
				Name:          keywordStructTags,
				Kind:          KeywordArgumentString,
				AllowedValues: nil,
				Required:      false,
			},
		},
	}
)

// KeywordArgumentKind classifies the value shape a directive argument accepts.
type KeywordArgumentKind uint8

// PositionalSpec describes one positional argument a directive accepts. Each positional
// has a name and may be passed positionally or by name; validation routes a keyword
// argument whose key matches a positional name into the corresponding slot.
type PositionalSpec struct {
	// Name is the name shown in errors, also accepted as a keyword argument key for keyword
	// argument-as-positional usage.
	Name string

	// AllowedValues, when non-empty, restricts KeywordArgumentIdentifier values to a closed
	// set checked case-insensitively.
	AllowedValues []string

	// Required marks whether the positional argument must be present.
	Required bool

	// Kind classifies the positional's value shape, used by the validator to reject inputs
	// of the wrong shape.
	Kind KeywordArgumentKind
}

// KeywordArgumentSpec describes one accepted keyword argument on a directive.
type KeywordArgumentSpec struct {
	// Name is the keyword argument key as it appears on the directive line.
	Name string

	// AllowedValues, when non-empty, restricts KeywordArgumentIdentifier values to a closed
	// set checked case-insensitively.
	AllowedValues []string

	// Kind classifies the value shape.
	Kind KeywordArgumentKind

	// Required marks the keyword argument as mandatory.
	Required bool
}

// CallSpec declares the arguments one directive call accepts: its positional slots, each
// of which may also be passed by name, and its keyword arguments.
type CallSpec struct {
	// Positionals lists the positional arguments in declaration order.
	Positionals []PositionalSpec

	// KeywordArguments lists the arguments that can only be passed by key, in display order.
	KeywordArguments []KeywordArgumentSpec
}

// Positional resolves a positional slot by name.
//
// Takes name (string) which is the positional name to resolve.
//
// Returns *PositionalSpec which is the matching positional, or nil when none matches.
// Returns int which is the positional's index in Positionals.
// Returns bool which is true when a positional was found.
func (spec *CallSpec) Positional(name string) (*PositionalSpec, int, bool) {
	for index := range spec.Positionals {
		if spec.Positionals[index].Name == name {
			return &spec.Positionals[index], index, true
		}
	}
	return nil, 0, false
}

// KeywordArgument resolves a keyword argument by key.
//
// Takes key (string) which is the keyword argument key to resolve.
//
// Returns *KeywordArgumentSpec which is the matching keyword argument, or nil when none
// matches.
// Returns bool which is true when a keyword argument was found.
func (spec *CallSpec) KeywordArgument(key string) (*KeywordArgumentSpec, bool) {
	for index := range spec.KeywordArguments {
		if spec.KeywordArguments[index].Name == key {
			return &spec.KeywordArguments[index], true
		}
	}
	return nil, false
}

// ArgumentNames returns the name of every argument the call accepts: the positional slots
// in declaration order, then the keyword arguments in display order.
//
// Returns []string which holds the names.
func (spec *CallSpec) ArgumentNames() []string {
	names := make([]string, 0, len(spec.Positionals)+len(spec.KeywordArguments))
	for index := range spec.Positionals {
		names = append(names, spec.Positionals[index].Name)
	}
	for index := range spec.KeywordArguments {
		names = append(names, spec.KeywordArguments[index].Name)
	}
	return names
}

// MigrationCallSpec returns the arguments the per-block migration directive accepts.
//
// Returns CallSpec which shares its argument lists with every other caller, so it must
// not be changed.
func MigrationCallSpec() CallSpec {
	return migrationCallSpec
}

// ColumnCallSpec returns the arguments the column override directive accepts.
//
// Returns CallSpec which shares its argument lists with every other caller, so it must
// not be changed.
func ColumnCallSpec() CallSpec {
	return columnCallSpec
}
