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

package mysql

import (
	_ "embed"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// anyTypeSpelling is the type spelling builtin_functions.spec gives the wildcard type
	// that accepts every argument type.
	anyTypeSpelling = "any"

	// unsignedTypeSuffix ends the spelling builtin_functions.spec gives an unsigned integer
	// type, such as int_unsigned for INT UNSIGNED.
	unsignedTypeSuffix = "_unsigned"
)

var (
	// builtinFunctionSpecSource is the built-in function catalogue, one signature per line
	// in the format dialectkit.FunctionSpec describes.
	//
	//go:embed builtin_functions.spec
	builtinFunctionSpecSource string

	// builtinFunctionSpec decodes builtinFunctionSpecSource the first time a catalogue is
	// built and keeps the decode error, which every dialect built from it reports; the
	// signatures that decoded are registered regardless.
	builtinFunctionSpec = dialectkit.EmbeddedFunctionSpec(builtinFunctionSpecSource)
)

// FunctionCatalogueBuilder builds the MySQL function catalogue. A flavour such as MariaDB
// receives it through WithExtraFunctions to register its own functions.
//
// It embeds the shared dialectkit.CatalogueBuilder, so it registers signatures with Add,
// Args, NullOnNull, NeverNull, CalledOnNull and ModifiesData and names any of the
// dialect's types, extra types included, with Type. The methods below are shortcuts for
// the types built-in signatures use most, plus MySQL's aggregate, window and variadic
// registrations.
type FunctionCatalogueBuilder struct {
	*catalogueBuilder
}

// Integer returns the MySQL INT type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Integer() sqlmodel.SQLType { return b.Type("int") }

// Bigint returns the MySQL BIGINT type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Bigint() sqlmodel.SQLType { return b.Type("bigint") }

// Float returns the MySQL FLOAT type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Float() sqlmodel.SQLType { return b.Type("float") }

// Double returns the MySQL DOUBLE type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Double() sqlmodel.SQLType { return b.Type("double") }

// Decimal returns the MySQL DECIMAL type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Decimal() sqlmodel.SQLType { return b.Type("decimal") }

// Text returns the MySQL TEXT type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Text() sqlmodel.SQLType { return b.Type("text") }

// Varchar returns the MySQL VARCHAR type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Varchar() sqlmodel.SQLType { return b.Type("varchar") }

// Varbinary returns the MySQL VARBINARY type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Varbinary() sqlmodel.SQLType { return b.Type("varbinary") }

// Boolean returns the MySQL BOOLEAN type, stored as TINYINT.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Boolean() sqlmodel.SQLType { return b.Type("boolean") }

// Bytea returns the MySQL BLOB type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Bytea() sqlmodel.SQLType { return b.Type("blob") }

// Date returns the MySQL DATE type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Date() sqlmodel.SQLType { return b.Type("date") }

// Time returns the MySQL TIME type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Time() sqlmodel.SQLType { return b.Type("time") }

// Datetime returns the MySQL DATETIME type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Datetime() sqlmodel.SQLType { return b.Type("datetime") }

// Timestamp returns the MySQL TIMESTAMP type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Timestamp() sqlmodel.SQLType { return b.Type("timestamp") }

// JSON returns the MySQL JSON type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) JSON() sqlmodel.SQLType { return b.Type("json") }

// Geometry returns the MySQL GEOMETRY type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Geometry() sqlmodel.SQLType { return b.Type("geometry") }

// Any returns the wildcard type used for polymorphic arguments.
//
// Returns sqlmodel.SQLType which is the type.
func (*FunctionCatalogueBuilder) Any() sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
}

// Aggregate registers an aggregate function.
//
// Takes name (string) which is the function name.
// Takes arguments ([]sqlmodel.FunctionArgument) which is the argument list.
// Takes returnType (sqlmodel.SQLType) which is the function's return type.
func (b *FunctionCatalogueBuilder) Aggregate(name string, arguments []sqlmodel.FunctionArgument, returnType sqlmodel.SQLType) {
	b.Add(name, sqlmodel.NewFunction(
		arguments,
		returnType,
		sqlmodel.FunctionNullableCalledOnNull,
		sqlmodel.WithAggregate(),
	))
}

// Window registers a window function.
//
// Takes name (string) which is the function name.
// Takes arguments ([]sqlmodel.FunctionArgument) which is the argument list.
// Takes returnType (sqlmodel.SQLType) which is the function's return type.
func (b *FunctionCatalogueBuilder) Window(name string, arguments []sqlmodel.FunctionArgument, returnType sqlmodel.SQLType) {
	b.Add(name, sqlmodel.NewFunction(arguments, returnType, sqlmodel.FunctionNullableNeverNull))
}

// Variadic registers a variadic function.
//
// Takes name (string) which is the function name.
// Takes arguments ([]sqlmodel.FunctionArgument) which is the argument list, with the
// trailing slot acting as the variadic template.
// Takes minArguments (int) which is the minimum number of arguments required.
// Takes returnType (sqlmodel.SQLType) which is the function's return type.
func (b *FunctionCatalogueBuilder) Variadic(name string, arguments []sqlmodel.FunctionArgument, minArguments int, returnType sqlmodel.SQLType) {
	b.Add(name, sqlmodel.NewFunction(
		arguments,
		returnType,
		sqlmodel.FunctionNullableCalledOnNull,
		sqlmodel.WithVariadic(minArguments),
	))
}

// registerBuiltinFunctions registers the built-in signatures of builtin_functions.spec.
// Their types resolve through the builder's type table, extra types included, and a
// signature whose line names no NULL behaviour returns NULL when any argument is NULL.
//
// Takes loadSpec (func() (*dialectkit.FunctionSpec, error)) which gives the decoded
// signatures.
//
// Returns error when the signatures did not all decode; those that did are registered.
func (b *FunctionCatalogueBuilder) registerBuiltinFunctions(loadSpec func() (*dialectkit.FunctionSpec, error)) error {
	spec, err := loadSpec()
	spec.Register(dialectkit.FunctionSpecHooks{
		ResolveType:        b.specType,
		DefaultNullability: nullOnNull,
		Add:                b.Add,
	})
	if err != nil {
		return fmt.Errorf("registering the built-in functions: %w", err)
	}
	return nil
}

// specType resolves a type spelling of builtin_functions.spec: the wildcard spelling to
// the wildcard type, a spelling ending in _unsigned, such as int_unsigned, to the
// unsigned integer type a spec line cannot spell with its space, and every other spelling
// through the builder's type table.
//
// Takes spelling (string) which is the type spelling.
//
// Returns sqlmodel.SQLType which is the type it names.
func (b *FunctionCatalogueBuilder) specType(spelling string) sqlmodel.SQLType {
	if spelling == anyTypeSpelling {
		return b.Any()
	}
	return b.Type(specTypeName(spelling))
}

// Arg names a single function argument and its SQL type. It aliases the shared toolkit's
// Arg so call sites keep using the bare name.
type Arg = dialectkit.Arg

// catalogueBuilder names the shared builder so FunctionCatalogueBuilder can embed it
// without exporting the embedded field.
type catalogueBuilder = dialectkit.CatalogueBuilder

// buildFunctionCatalogue assembles the MySQL function catalogue: the built-in signatures
// of builtin_functions.spec less the withheld ones, then any extra functions.
//
// Takes loadSpec (func() (*dialectkit.FunctionSpec, error)) which gives the decoded
// built-in signatures.
// Takes extraFunctions ([]func(*FunctionCatalogueBuilder)) which add extra signatures, in
// order, after the built-ins have been registered.
// Takes withheld ([]string) which names, lower-cased, the built-in functions to leave out
// before the extra functions register.
// Takes types (*ast.TypeCatalogue) which is the dialect's merged type catalogue, so the
// builder's Type method and shortcuts resolve the dialect's own types.
//
// Returns *ast.FunctionCatalogue which lists every registered function signature.
// Returns error when the built-in signatures did not all decode; the catalogue still
// holds those that did.
func buildFunctionCatalogue(
	loadSpec func() (*dialectkit.FunctionSpec, error),
	extraFunctions []func(*FunctionCatalogueBuilder),
	withheld []string,
	types *ast.TypeCatalogue,
) (*ast.FunctionCatalogue, error) {
	builder := &FunctionCatalogueBuilder{catalogueBuilder: dialectkit.NewTypedCatalogueBuilder(types.Types)}
	err := builder.registerBuiltinFunctions(loadSpec)
	for _, name := range withheld {
		delete(builder.Catalogue.Functions, name)
	}
	for _, register := range extraFunctions {
		register(builder)
	}
	return builder.Catalogue, err
}

// nullOnNull is the NULL behaviour of a built-in signature whose spec line names none:
// NULL when any argument is NULL.
//
// Returns sqlmodel.FunctionNullableBehaviour which is FunctionNullableReturnsNullOnNull.
func nullOnNull(_ string, _ bool) sqlmodel.FunctionNullableBehaviour {
	return sqlmodel.FunctionNullableReturnsNullOnNull
}

// specTypeName returns the type table name a spelling of builtin_functions.spec stands
// for: the spelling itself, or for one ending in _unsigned, such as int_unsigned, the
// unsigned type's name, int unsigned.
//
// Takes spelling (string) which is the type spelling.
//
// Returns string which is the type table name.
func specTypeName(spelling string) string {
	if base, unsigned := strings.CutSuffix(spelling, unsignedTypeSuffix); unsigned {
		return base + " unsigned"
	}
	return spelling
}
