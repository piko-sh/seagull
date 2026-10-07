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

package postgres

import (
	_ "embed"
	"errors"
	"fmt"
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
	// built and keeps the decode error, which every dialect built from it reports; the
	// signatures that decoded are registered regardless.
	builtinFunctionSpec = dialectkit.EmbeddedFunctionSpec(builtinFunctionSpecSource)
)

// Arg names a single function argument and its SQL type. It aliases the shared toolkit
// type so call sites keep using the bare Arg name while the registration mechanism stays
// shared.
type Arg = dialectkit.Arg

// FunctionCatalogueBuilder builds the PostgreSQL function catalogue. A flavour receives
// it through WithExtraFunctions to register its own functions.
//
// It embeds the shared dialectkit.CatalogueBuilder, so it registers signatures with Add,
// Args, NullOnNull, NeverNull, CalledOnNull and ModifiesData and names any of the
// dialect's types, extra types included, with Type. The methods below are shortcuts for
// the types built-in signatures use most.
type FunctionCatalogueBuilder struct {
	*catalogueBuilder

	// problems holds the problems the registrations reported, in order.
	problems []error
}

// ReportError records a problem with the functions a registration adds, such as an
// embedded catalogue that did not decode, so the dialect returns it from every ApplyDDL
// and AnalyseQuery rather than working quietly from a partial catalogue.
//
// Takes err (error) which is the problem; nil is ignored.
func (b *FunctionCatalogueBuilder) ReportError(err error) {
	if err != nil {
		b.problems = append(b.problems, err)
	}
}

// Integer returns the 4-byte integer type, int4.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Integer() sqlmodel.SQLType { return b.Type("int4") }

// Bigint returns the 8-byte integer type, int8.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Bigint() sqlmodel.SQLType { return b.Type("int8") }

// Smallint returns the 2-byte integer type, int2.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Smallint() sqlmodel.SQLType { return b.Type("int2") }

// Float4 returns the single-precision floating point type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Float4() sqlmodel.SQLType { return b.Type("float4") }

// Float8 returns the double-precision floating point type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Float8() sqlmodel.SQLType { return b.Type("float8") }

// Numeric returns the arbitrary-precision decimal type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Numeric() sqlmodel.SQLType { return b.Type("numeric") }

// Boolean returns the boolean type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Boolean() sqlmodel.SQLType { return b.Type("bool") }

// Text returns the variable-length text type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Text() sqlmodel.SQLType { return b.Type("text") }

// Bytea returns the raw byte string type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Bytea() sqlmodel.SQLType { return b.Type("bytea") }

// Timestamp returns the timestamp without time zone type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Timestamp() sqlmodel.SQLType { return b.Type("timestamp") }

// Timestamptz returns the timestamp with time zone type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Timestamptz() sqlmodel.SQLType { return b.Type("timestamptz") }

// Date returns the calendar date type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Date() sqlmodel.SQLType { return b.Type("date") }

// Time returns the time-of-day type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Time() sqlmodel.SQLType { return b.Type("time") }

// Interval returns the interval type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) Interval() sqlmodel.SQLType { return b.Type("interval") }

// JSON returns the textual JSON type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) JSON() sqlmodel.SQLType { return b.Type("json") }

// JSONB returns the binary JSON type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) JSONB() sqlmodel.SQLType { return b.Type("jsonb") }

// UUID returns the universally unique identifier type.
//
// Returns sqlmodel.SQLType which is the type.
func (b *FunctionCatalogueBuilder) UUID() sqlmodel.SQLType { return b.Type("uuid") }

// Any returns the wildcard type used for polymorphic arguments.
//
// Returns sqlmodel.SQLType which is the type.
func (*FunctionCatalogueBuilder) Any() sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
}

// registerBuiltinFunctions registers the built-in signatures of builtin_functions.spec.
// Their types resolve through the builder's type table, extra types included, and a
// signature whose line names no NULL behaviour returns NULL when any argument is NULL.
//
// Takes loadSpecification (func() (*dialectkit.FunctionSpec, error)) which gives the
// decoded signatures.
//
// Returns error when the signatures did not all decode; those that did are registered.
func (b *FunctionCatalogueBuilder) registerBuiltinFunctions(loadSpecification func() (*dialectkit.FunctionSpec, error)) error {
	specification, err := loadSpecification()
	specification.Register(dialectkit.FunctionSpecHooks{
		ResolveType:        b.specType,
		DefaultNullability: nullOnNull,
		Add:                b.Add,
	})
	return err
}

// specType resolves a type spelling of builtin_functions.spec: the wildcard spelling to
// the wildcard type, an array spelling such as text[] to the array of its element type,
// and every other spelling through the builder's type table.
//
// Takes spelling (string) which is the type spelling.
//
// Returns sqlmodel.SQLType which is the type it names.
func (b *FunctionCatalogueBuilder) specType(spelling string) sqlmodel.SQLType {
	if spelling == anyTypeSpelling {
		return b.Any()
	}
	if element, isArray := strings.CutSuffix(spelling, arraySubscriptSuffix); isArray {
		return arrayTypeOf(b.specType(element), 1)
	}
	return b.Type(spelling)
}

// catalogueBuilder names the shared builder so FunctionCatalogueBuilder can embed it
// without exporting the embedded field.
type catalogueBuilder = dialectkit.CatalogueBuilder

// buildFunctionCatalogue assembles the PostgreSQL function catalogue: the built-in
// signatures of builtin_functions.spec less the withheld ones, then any extra functions.
//
// Takes loadSpecification (func() (*dialectkit.FunctionSpec, error)) which gives the
// decoded built-in signatures.
// Takes extraFunctions ([]func(*FunctionCatalogueBuilder)) which add extra signatures, in
// order, after the built-ins have been registered.
// Takes withheld (map[string]struct{}) which holds, lower-cased, the names of the
// built-in functions left out.
// Takes types (*ast.TypeCatalogue) which is the dialect's merged type catalogue, so the
// builder's Type method and shortcuts resolve the dialect's own types.
//
// Returns *ast.FunctionCatalogue which lists every registered function signature.
// Returns error when the built-in signatures did not all decode or an extra registration
// reported a problem; the catalogue still holds what was registered.
func buildFunctionCatalogue(
	loadSpecification func() (*dialectkit.FunctionSpec, error),
	extraFunctions []func(*FunctionCatalogueBuilder),
	withheld map[string]struct{},
	types *ast.TypeCatalogue,
) (*ast.FunctionCatalogue, error) {
	builder := &FunctionCatalogueBuilder{catalogueBuilder: dialectkit.NewTypedCatalogueBuilder(types.Types), problems: nil}
	if err := builder.registerBuiltinFunctions(loadSpecification); err != nil {
		builder.ReportError(fmt.Errorf("registering the built-in functions: %w", err))
	}
	for name := range withheld {
		delete(builder.Catalogue.Functions, name)
	}
	for _, register := range extraFunctions {
		register(builder)
	}
	return builder.Catalogue, errors.Join(builder.problems...)
}

// nullOnNull is the NULL behaviour of a built-in signature whose spec line names none:
// NULL when any argument is NULL.
//
// Returns sqlmodel.FunctionNullableBehaviour which is FunctionNullableReturnsNullOnNull.
func nullOnNull(_ string, _ bool) sqlmodel.FunctionNullableBehaviour {
	return sqlmodel.FunctionNullableReturnsNullOnNull
}
