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

package sqlite

import (
	_ "embed"
	"fmt"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// anyTypeSpelling is the type spelling builtin_functions.spec gives the wildcard type
// that accepts every argument type.
const anyTypeSpelling = "any"

var (
	// builtinFunctionSpecSource is the built-in function catalogue, one signature per line
	// in the format dialectkit.FunctionSpec describes.
	//
	//go:embed builtin_functions.spec
	builtinFunctionSpecSource string

	// builtinFunctionSpec decodes builtinFunctionSpecSource the first time a catalogue is
	// built and gives every later caller the same result.
	//
	// The specification is fixed at build time and checked by a test. If decoding fails,
	// successfully decoded signatures are still registered, and ApplyDDL and AnalyseQuery
	// report the error.
	builtinFunctionSpec = dialectkit.EmbeddedFunctionSpec(builtinFunctionSpecSource)
)

// FunctionCatalogueBuilder builds the SQLite function catalogue. A caller receives it
// through WithExtraFunctions to register its own functions, such as the
// application-defined functions a program registers with its SQLite driver.
//
// It embeds the shared dialectkit.CatalogueBuilder, so it registers signatures with Add,
// Args, NullOnNull, NeverNull, CalledOnNull and ModifiesData and names any of the
// dialect's types, extra types included, with Type. The methods below are shortcuts for
// the types built-in signatures use most.
type FunctionCatalogueBuilder struct {
	*catalogueBuilder
}

// Integer returns the SQL integer type built-in signatures use for ordinary integer
// results: INTEGER, a signed 64-bit integer like every SQLite integer.
//
// Returns sqlmodel.SQLType which is the type.
func (*FunctionCatalogueBuilder) Integer() sqlmodel.SQLType {
	return sqlmodel.NewIntegerType(typeNameInteger, widthInteger, false)
}

// Bigint returns the BIGINT spelling of the 64-bit integer type.
//
// It names the results that are genuinely large: count and the window-ranking functions,
// the connection change counters, last_insert_rowid and unixepoch. The distinct name lets
// a type override narrow ordinary INTEGER results without narrowing these.
//
// Returns sqlmodel.SQLType which is the type.
func (*FunctionCatalogueBuilder) Bigint() sqlmodel.SQLType {
	return sqlmodel.NewIntegerType(typeNameBigint, widthInteger, false)
}

// Text returns the SQL text type.
//
// Returns sqlmodel.SQLType which is the type.
func (*FunctionCatalogueBuilder) Text() sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
}

// Real returns the SQL real (8-byte floating point) type.
//
// Returns sqlmodel.SQLType which is the type.
func (*FunctionCatalogueBuilder) Real() sqlmodel.SQLType {
	return sqlmodel.NewFloatType(typeNameReal, widthReal)
}

// Blob returns the SQL blob type, which also carries SQLite's binary JSONB form.
//
// Returns sqlmodel.SQLType which is the type.
func (*FunctionCatalogueBuilder) Blob() sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "blob")
}

// Any returns the wildcard type polymorphic signatures use, which accepts an argument of
// any type.
//
// Returns sqlmodel.SQLType which is the type.
func (*FunctionCatalogueBuilder) Any() sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
}

// registerBuiltinFunctions registers the built-in signatures of builtin_functions.spec.
// Their types resolve through SQLite's built-in type normaliser, as the type shortcuts
// do, and a signature whose line names no NULL behaviour returns NULL when any argument
// is NULL.
//
// Takes spec (*dialectkit.FunctionSpec) which holds the decoded signatures; nil registers
// none.
func (b *FunctionCatalogueBuilder) registerBuiltinFunctions(spec *dialectkit.FunctionSpec) {
	spec.Register(dialectkit.FunctionSpecHooks{
		ResolveType:        specType,
		DefaultNullability: nullOnNull,
		Add:                b.Add,
	})
}

// Arg names a single function argument and its SQL type. It aliases the shared toolkit's
// Arg so call sites keep using the bare name; the registration methods (Add, Args,
// NullOnNull, NeverNull, CalledOnNull) are promoted from the embedded shared builder.
type Arg = dialectkit.Arg

// catalogueBuilder names the shared builder so FunctionCatalogueBuilder can embed it
// without exporting the embedded field.
type catalogueBuilder = dialectkit.CatalogueBuilder

// buildFunctionCatalogue assembles the SQLite function catalogue: the built-in signatures
// loadSpec decodes, then any extra functions.
//
// Takes loadSpec (func() (*dialectkit.FunctionSpec, error)) which decodes the built-in
// signatures.
// Takes extraFunctions ([]func(*FunctionCatalogueBuilder)) which add extra signatures, in
// order, after the built-ins have been registered.
// Takes types (*ast.TypeCatalogue) which is the dialect's merged type catalogue, so the
// builder's Type method and shortcuts resolve the dialect's own types.
//
// Returns *ast.FunctionCatalogue which lists every registered function signature,
// including the built-ins that decoded when loadSpec fails.
// Returns error when loadSpec fails.
func buildFunctionCatalogue(
	loadSpec func() (*dialectkit.FunctionSpec, error),
	extraFunctions []func(*FunctionCatalogueBuilder),
	types *ast.TypeCatalogue,
) (*ast.FunctionCatalogue, error) {
	builder := &FunctionCatalogueBuilder{catalogueBuilder: dialectkit.NewTypedCatalogueBuilder(types.Types)}
	spec, err := loadSpec()
	builder.registerBuiltinFunctions(spec)
	for _, register := range extraFunctions {
		register(builder)
	}
	if err != nil {
		return builder.Catalogue, fmt.Errorf("sqlite: building the built-in function catalogue: %w", err)
	}
	return builder.Catalogue, nil
}

// nullOnNull is the NULL behaviour of a built-in signature whose spec line names none:
// NULL when any argument is NULL.
//
// Returns sqlmodel.FunctionNullableBehaviour which is FunctionNullableReturnsNullOnNull.
func nullOnNull(_ string, _ bool) sqlmodel.FunctionNullableBehaviour {
	return sqlmodel.FunctionNullableReturnsNullOnNull
}

// specType resolves a type spelling of builtin_functions.spec: the wildcard spelling to
// the wildcard type, and every other spelling through SQLite's built-in type normaliser.
//
// Takes spelling (string) which is the type spelling.
//
// Returns sqlmodel.SQLType which is the type it names.
func specType(spelling string) sqlmodel.SQLType {
	if spelling == anyTypeSpelling {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	}
	return normaliseTypeName(spelling)
}
