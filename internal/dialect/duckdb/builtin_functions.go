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

package duckdb

import (
	_ "embed"
	"strings"

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
	// built. The spec is fixed at build time and a test asserts it decodes; were it not to,
	// the signatures that decoded are registered regardless and the error is kept for
	// ApplyDDL and AnalyseQuery to return.
	builtinFunctionSpec = dialectkit.EmbeddedFunctionSpec(builtinFunctionSpecSource)
)

// FunctionCatalogueBuilder builds the DuckDB function catalogue. A flavour receives it
// through WithExtraFunctions to register its own functions.
//
// It embeds the shared dialectkit.CatalogueBuilder, so it registers signatures with Add,
// Args, NullOnNull, NeverNull, CalledOnNull and ModifiesData and names any of the
// dialect's types, extra types included, with Type. The methods below are shortcuts for
// the types built-in signatures use most.
type FunctionCatalogueBuilder struct {
	*catalogueBuilder
}

// Integer returns the 32-bit signed integer type, INTEGER.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Integer() sqlmodel.SQLType { return b.Type("integer") }

// Bigint returns the 64-bit signed integer type, BIGINT.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Bigint() sqlmodel.SQLType { return b.Type("bigint") }

// UBigint returns the 64-bit unsigned integer type, UBIGINT.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) UBigint() sqlmodel.SQLType { return b.Type("ubigint") }

// Smallint returns the 16-bit signed integer type, SMALLINT.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Smallint() sqlmodel.SQLType { return b.Type("smallint") }

// Hugeint returns the 128-bit signed integer type, HUGEINT.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Hugeint() sqlmodel.SQLType { return b.Type("hugeint") }

// Float4 returns the single-precision floating point type, FLOAT.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Float4() sqlmodel.SQLType { return b.Type("real") }

// Float8 returns the double-precision floating point type, DOUBLE.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Float8() sqlmodel.SQLType { return b.Type("double") }

// Numeric returns the exact decimal type, DECIMAL.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Numeric() sqlmodel.SQLType { return b.Type("numeric") }

// Boolean returns the boolean type, BOOLEAN.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Boolean() sqlmodel.SQLType { return b.Type("boolean") }

// Text returns the variable-length text type, VARCHAR.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Text() sqlmodel.SQLType { return b.Type("varchar") }

// Bytea returns the binary type, BLOB.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Bytea() sqlmodel.SQLType { return b.Type("blob") }

// Timestamp returns the timestamp without time zone type, TIMESTAMP.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Timestamp() sqlmodel.SQLType { return b.Type("timestamp") }

// Timestamptz returns the timestamp with time zone type, TIMESTAMPTZ.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Timestamptz() sqlmodel.SQLType { return b.Type("timestamptz") }

// Date returns the calendar date type, DATE.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Date() sqlmodel.SQLType { return b.Type("date") }

// Time returns the time-of-day type, TIME.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Time() sqlmodel.SQLType { return b.Type("time") }

// Interval returns the time interval type, INTERVAL.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Interval() sqlmodel.SQLType { return b.Type("interval") }

// JSON returns the JSON document type, JSON.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) JSON() sqlmodel.SQLType { return b.Type("json") }

// UUID returns the universally unique identifier type, UUID.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) UUID() sqlmodel.SQLType { return b.Type("uuid") }

// Any returns the wildcard type a polymorphic argument or result takes.
//
// Returns sqlmodel.SQLType which is the type.
func (*FunctionCatalogueBuilder) Any() sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
}

// registerBuiltinFunctions registers the built-in signatures of builtin_functions.spec.
// Their types resolve through the builder's type table, extra types included, and a
// signature whose line names no NULL behaviour returns NULL when any argument is NULL.
//
// Returns error when the spec does not decode; the signatures that decoded are registered
// regardless.
func (b *FunctionCatalogueBuilder) registerBuiltinFunctions() error {
	specification, err := builtinFunctionSpec()
	specification.Register(dialectkit.FunctionSpecHooks{
		ResolveType:        b.specType,
		DefaultNullability: nullOnNull,
		Add:                b.Add,
	})
	return err
}

// specType resolves a type spelling of builtin_functions.spec: the wildcard spelling to
// the wildcard type, a spelling ending in [] to the list of the type the rest of it
// names, and every other spelling through the builder's type table.
//
// Takes spelling (string) which is the type spelling.
//
// Returns sqlmodel.SQLType which is the type it names.
func (b *FunctionCatalogueBuilder) specType(spelling string) sqlmodel.SQLType {
	if spelling == anyTypeSpelling {
		return b.Any()
	}
	if element, isList := strings.CutSuffix(spelling, arraySubscriptSuffix); isList {
		return newArrayType(b.specType(element))
	}
	return b.Type(spelling)
}

// Arg names a single function argument and its SQL type. It aliases the shared toolkit's
// Arg so registrations keep using the bare name.
type Arg = dialectkit.Arg

// catalogueBuilder names the shared builder so FunctionCatalogueBuilder can embed it
// without exporting the embedded field.
type catalogueBuilder = dialectkit.CatalogueBuilder

// buildFunctionCatalogue assembles the DuckDB function catalogue: the built-in signatures
// of builtin_functions.spec, then any extra functions.
//
// Takes extraFunctions ([]func(*FunctionCatalogueBuilder)) which add extra signatures, in
// order, after the built-ins have been registered.
// Takes types (*ast.TypeCatalogue) which is the dialect's merged type catalogue, so the
// builder's Type method and shortcuts resolve the dialect's own types.
//
// Returns *ast.FunctionCatalogue which lists every registered function signature.
// Returns error when the built-in catalogue does not decode.
func buildFunctionCatalogue(
	extraFunctions []func(*FunctionCatalogueBuilder),
	types *ast.TypeCatalogue,
) (*ast.FunctionCatalogue, error) {
	builder := &FunctionCatalogueBuilder{catalogueBuilder: dialectkit.NewTypedCatalogueBuilder(types.Types)}
	err := builder.registerBuiltinFunctions()
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
