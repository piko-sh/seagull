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

package dialectkit

import (
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Arg names a single function argument and its SQL type.
//
// It is the typed unit accepted by Args, so a malformed argument is a compile error
// rather than a value silently dropped at registration time. Engines pass their own
// dialect SQLType values, keeping their type vocabulary engine-local while the
// registration mechanism is shared.
type Arg struct {
	// Name is the argument name.
	Name string

	// Type is the argument SQL type.
	Type sqlmodel.SQLType
}

// CatalogueBuilder accumulates FunctionSignatures into a FunctionCatalogue.
//
// It carries no dialect-specific type vocabulary of its own: a dialect embeds it in its
// function catalogue builder and hands it the dialect's type table, so Type resolves the
// dialect's own type names, and the dialect's type shortcuts (such as Integer or Text)
// are thin methods over Type. Every built-in dialect exposes the same builder shape this
// way, so a flavour author learns one registration API.
type CatalogueBuilder struct {
	// Catalogue is the function catalogue being assembled.
	Catalogue *ast.FunctionCatalogue

	// types is the dialect's type table Type resolves names against, keyed by lower-cased
	// name.
	types map[string]sqlmodel.SQLType
}

// NewCatalogueBuilder returns a CatalogueBuilder with an empty catalogue ready to
// register function signatures into.
//
// Returns *CatalogueBuilder which is ready for registration calls.
func NewCatalogueBuilder() *CatalogueBuilder {
	return &CatalogueBuilder{
		Catalogue: &ast.FunctionCatalogue{
			Functions: make(map[string][]*sqlmodel.Function),
		},
		types: nil,
	}
}

// NewTypedCatalogueBuilder returns a CatalogueBuilder whose Type method resolves names
// against the dialect's type table, such as its built-in types merged with any extra
// types, so registrations can name a flavour's own types too.
//
// Takes types (map[string]sqlmodel.SQLType) which is the dialect's type table; keys are
// matched ignoring case.
//
// Returns *CatalogueBuilder which is ready for registration calls.
func NewTypedCatalogueBuilder(types map[string]sqlmodel.SQLType) *CatalogueBuilder {
	builder := NewCatalogueBuilder()
	builder.types = make(map[string]sqlmodel.SQLType, len(types))
	for name := range types {
		builder.types[strings.ToLower(name)] = types[name]
	}
	return builder
}

// Type returns the dialect type registered under name, for use in a signature. A name the
// type table does not hold resolves to an unknown type carrying the name, so a typo shows
// up as an unknown type in the generated code rather than a panic.
//
// Takes name (string) which is the type name, matched ignoring case.
//
// Returns sqlmodel.SQLType which is the registered type.
func (b *CatalogueBuilder) Type(name string) sqlmodel.SQLType {
	lowered := strings.ToLower(name)
	if sqlType, found := b.types[lowered]; found {
		return sqlType
	}
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, lowered)
}

// Args builds a slice of FunctionArgument from the supplied typed arguments, preserving
// declaration order.
//
// Takes arguments (...Arg) which names each positional argument together with its SQL
// type.
//
// Returns []sqlmodel.FunctionArgument which lists the arguments in order.
func (*CatalogueBuilder) Args(arguments ...Arg) []sqlmodel.FunctionArgument {
	result := make([]sqlmodel.FunctionArgument, 0, len(arguments))
	for index := range arguments {
		result = append(result, sqlmodel.FunctionArgument{
			Name:       arguments[index].Name,
			Type:       arguments[index].Type,
			IsOptional: false,
			NotNull:    false,
		})
	}
	return result
}

// Add registers a function signature under the given name and returns it so the caller
// can refine fields (for example MinArguments) on the registered value. The signature is
// marked read-only by default; callers can override DataAccess for functions that write.
//
// Takes name (string) which is the function name to register under.
// Takes signature (*sqlmodel.Function) which describes the function.
//
// Returns *sqlmodel.Function which is the registered signature.
func (b *CatalogueBuilder) Add(name string, signature *sqlmodel.Function) *sqlmodel.Function {
	signature.Name = name
	signature.DataAccess = sqlmodel.DataAccessReadOnly
	b.Catalogue.Functions[name] = append(b.Catalogue.Functions[name], signature)
	return signature
}

// NullOnNull registers a function that returns NULL when any argument is NULL.
//
// Takes name (string) which is the function name to register.
// Takes arguments ([]sqlmodel.FunctionArgument) which describes the positional arguments.
// Takes returnType (sqlmodel.SQLType) which is the result type when all arguments are
// non-null.
//
// Returns *sqlmodel.Function which is the registered signature.
func (b *CatalogueBuilder) NullOnNull(name string, arguments []sqlmodel.FunctionArgument, returnType sqlmodel.SQLType) *sqlmodel.Function {
	return b.Add(name, sqlmodel.NewFunction(
		arguments,
		returnType,
		sqlmodel.FunctionNullableReturnsNullOnNull,
	))
}

// NeverNull registers a function that never returns NULL.
//
// Takes name (string) which is the function name to register.
// Takes arguments ([]sqlmodel.FunctionArgument) which describes the positional arguments.
// Takes returnType (sqlmodel.SQLType) which is the result type.
//
// Returns *sqlmodel.Function which is the registered signature.
func (b *CatalogueBuilder) NeverNull(name string, arguments []sqlmodel.FunctionArgument, returnType sqlmodel.SQLType) *sqlmodel.Function {
	return b.Add(name, sqlmodel.NewFunction(arguments, returnType, sqlmodel.FunctionNullableNeverNull))
}

// CalledOnNull registers a function that is invoked even with NULL arguments; the result
// may or may not be NULL depending on the function.
//
// Takes name (string) which is the function name to register.
// Takes arguments ([]sqlmodel.FunctionArgument) which describes the positional arguments.
// Takes returnType (sqlmodel.SQLType) which is the result type.
//
// Returns *sqlmodel.Function which is the registered signature.
func (b *CatalogueBuilder) CalledOnNull(name string, arguments []sqlmodel.FunctionArgument, returnType sqlmodel.SQLType) *sqlmodel.Function {
	return b.Add(name, sqlmodel.NewFunction(
		arguments,
		returnType,
		sqlmodel.FunctionNullableCalledOnNull,
	))
}

// ModifiesData registers a built-in that may modify database state.
//
// Such functions include a sequence advance like nextval or setval. It is registered as
// never-null, and its data access is set to DataAccessModifiesData, overriding the
// read-only default that Add applies, so a query projecting it is classified as
// data-modifying and routed to the writer connection rather than a read replica.
//
// Takes name (string) which is the function name to register.
// Takes arguments ([]sqlmodel.FunctionArgument) which describes the positional arguments.
// Takes returnType (sqlmodel.SQLType) which is the result type.
//
// Returns *sqlmodel.Function which is the registered signature.
func (b *CatalogueBuilder) ModifiesData(name string, arguments []sqlmodel.FunctionArgument, returnType sqlmodel.SQLType) *sqlmodel.Function {
	signature := b.Add(name, sqlmodel.NewFunction(
		arguments,
		returnType,
		sqlmodel.FunctionNullableNeverNull,
	))
	signature.DataAccess = sqlmodel.DataAccessModifiesData
	return signature
}
