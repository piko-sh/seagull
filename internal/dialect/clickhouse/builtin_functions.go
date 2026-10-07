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

package clickhouse

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"sync"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// builtinTableCount is how many embedded tables a function catalogue is built from.
const builtinTableCount = 2

// Arg names a single function argument and its SQL type. It aliases the shared toolkit's
// Arg so call sites keep using the bare name.
type Arg = dialectkit.Arg

// FunctionCatalogueBuilder builds the ClickHouse function catalogue. A caller receives it
// through WithExtraFunctions to register its own functions.
//
// It embeds the shared dialectkit.CatalogueBuilder, so it registers signatures with Add,
// Args, NullOnNull, NeverNull, CalledOnNull and ModifiesData and names any of the
// dialect's types, extra types included, with Type, the same shape every built-in
// dialect's builder has. Register and its variants are ClickHouse additions that take
// positional argument types and file the signature under its lower-cased name for the
// analyser's case-insensitive catalogue lookup.
type FunctionCatalogueBuilder struct {
	*catalogueBuilder
}

// Register adds a non-aggregate function signature.
//
// Names are lower-cased before insertion so the analyser's catalogue lookup is
// case-insensitive. The result's NULL behaviour follows the rule for the function's name
// in scalarNullBehaviour.
//
// Takes name (string) which is the function name to register under.
// Takes returnType (sqlmodel.SQLType) which is the function's result type.
// Takes argumentTypes (...sqlmodel.SQLType) which are the argument types in order.
func (builder *FunctionCatalogueBuilder) Register(name string, returnType sqlmodel.SQLType, argumentTypes ...sqlmodel.SQLType) {
	builder.add(name, sqlmodel.NewFunction(
		builtinArguments(argumentTypes),
		returnType,
		scalarNullBehaviour(name),
	))
}

// RegisterVariadic adds a function whose last argument may repeat.
//
// Names are lower-cased to match Register's case-insensitive lookup, and the result's
// NULL behaviour follows Register's rule.
//
// Takes name (string) which is the function name to register under.
// Takes returnType (sqlmodel.SQLType) which is the function's result type.
// Takes minimumArguments (int) which is the minimum number of arguments accepted.
// Takes argumentTypes (...sqlmodel.SQLType) which are the argument types in order.
func (builder *FunctionCatalogueBuilder) RegisterVariadic(name string, returnType sqlmodel.SQLType, minimumArguments int, argumentTypes ...sqlmodel.SQLType) {
	builder.add(name, sqlmodel.NewFunction(
		builtinArguments(argumentTypes),
		returnType,
		scalarNullBehaviour(name),
		sqlmodel.WithVariadic(minimumArguments),
	))
}

// RegisterNullPropagating adds a scalar function whose result is NULL exactly when an
// argument is NULL - the standard cast/conversion behaviour (toInt64, toString, ...). It
// mirrors Register but stamps FunctionNullableReturnsNullOnNull, so converting a non-null
// value (for example toInt64(count())) is typed non-nullable rather than the default
// always-nullable.
//
// Takes name (string) which is the function name to register under.
// Takes returnType (sqlmodel.SQLType) which is the function's result type.
// Takes argumentTypes (...sqlmodel.SQLType) which are the argument types in order.
func (builder *FunctionCatalogueBuilder) RegisterNullPropagating(name string, returnType sqlmodel.SQLType, argumentTypes ...sqlmodel.SQLType) {
	builder.add(name, sqlmodel.NewFunction(
		builtinArguments(argumentTypes),
		returnType,
		sqlmodel.FunctionNullableReturnsNullOnNull,
	))
}

// RegisterAggregate adds an aggregate function signature.
//
// Names are lower-cased to match Register's case-insensitive lookup. The result's NULL
// behaviour follows ClickHouse's default for the aggregate's name; see
// aggregateNullBehaviour.
//
// Takes name (string) which is the function name to register under.
// Takes returnType (sqlmodel.SQLType) which is the function's result type.
// Takes argumentTypes (...sqlmodel.SQLType) which are the argument types in order.
func (builder *FunctionCatalogueBuilder) RegisterAggregate(name string, returnType sqlmodel.SQLType, argumentTypes ...sqlmodel.SQLType) {
	builder.add(name, sqlmodel.NewFunction(
		builtinArguments(argumentTypes),
		returnType,
		aggregateNullBehaviour(name),
		sqlmodel.WithAggregate(),
	))
}

// RegisterAggregateNoArgs adds a no-argument aggregate such as count().
//
// Names are lower-cased to match the other Register* helpers.
//
// Takes name (string) which is the function name to register under.
// Takes returnType (sqlmodel.SQLType) which is the function's result type.
func (builder *FunctionCatalogueBuilder) RegisterAggregateNoArgs(name string, returnType sqlmodel.SQLType) {
	builder.add(name, sqlmodel.NewFunction(
		nil,
		returnType,
		sqlmodel.FunctionNullableNeverNull,
		sqlmodel.WithAggregate(),
	))
}

// RegisterVariadicAggregate adds an aggregate function whose last argument may repeat.
//
// It is used by the categorical-information-value and matrix correlation aggregates which
// take a variable number of columns. Names are lower-cased to match the other Register*
// helpers.
//
// Takes name (string) which is the function name to register under.
// Takes returnType (sqlmodel.SQLType) which is the function's result type.
// Takes minimumArguments (int) which is the minimum number of arguments accepted.
// Takes argumentTypes (...sqlmodel.SQLType) which are the argument types in order.
func (builder *FunctionCatalogueBuilder) RegisterVariadicAggregate(name string, returnType sqlmodel.SQLType, minimumArguments int, argumentTypes ...sqlmodel.SQLType) {
	builder.add(name, sqlmodel.NewFunction(
		builtinArguments(argumentTypes),
		returnType,
		sqlmodel.FunctionNullableNeverNull,
		sqlmodel.WithVariadic(minimumArguments),
		sqlmodel.WithAggregate(),
	))
}

// add files a signature under its lower-cased name through the shared builder, which
// marks it read-only, and keeps the name's own spelling on the signature.
//
// Takes name (string) which is the function name to register under.
// Takes signature (*sqlmodel.Function) which is the signature to register.
//
// Returns *sqlmodel.Function which is the registered signature.
func (builder *FunctionCatalogueBuilder) add(name string, signature *sqlmodel.Function) *sqlmodel.Function {
	registered := builder.Add(strings.ToLower(name), signature)
	registered.Name = name
	return registered
}

// registerBuiltinFunctions registers the built-in signatures of a function spec. Their
// types resolve through the ClickHouse type parser, whatever extra types the dialect has,
// and a signature whose line names no NULL behaviour takes ClickHouse's default for its
// name, as Register and RegisterAggregate do.
//
// Takes decode (func() (*dialectkit.FunctionSpec, error)) which decodes the spec.
//
// Returns error which names each spec line that did not decode, whose signature is left
// out, or nil when every line decoded.
func (builder *FunctionCatalogueBuilder) registerBuiltinFunctions(decode func() (*dialectkit.FunctionSpec, error)) error {
	specification, err := decode()
	specification.Register(dialectkit.FunctionSpecHooks{
		ResolveType:        specType,
		DefaultNullability: defaultNullBehaviour,
		Add:                builder.add,
	})
	return err
}

// catalogueBuilder names the shared builder so FunctionCatalogueBuilder can embed it
// without exporting the embedded field.
type catalogueBuilder = dialectkit.CatalogueBuilder

// builtinTables decode the embedded tables a function catalogue starts from.
type builtinTables struct {
	// signatures decodes the built-in function signatures.
	signatures func() (*dialectkit.FunctionSpec, error)

	// aliases decodes the function alias table.
	aliases func() (map[string]string, error)
}

var (
	// mayReturnNullFunctions are the ordinary functions, lower-cased, that can return NULL
	// from arguments that are not NULL.
	mayReturnNullFunctions = map[string]bool{
		"nullif": true, "tonullable": true, "nullable": true, "variantelement": true, "dynamicelement": true,
	}

	// neverNullFunctions are the ordinary functions, lower-cased, that never return NULL,
	// even from a NULL argument.
	neverNullFunctions = map[string]bool{
		"isnull": true, "isnotnull": true, "iszeroornull": true, "assumenotnull": true, "totypename": true,
		"isnullable": true,
	}

	// neverNullAggregates are the aggregates, lower-cased, that never return NULL besides
	// the uniq, groupArray and count families.
	neverNullAggregates = map[string]bool{
		"groupuniqarray": true, "groupbitmap": true, "topk": true, "topkweighted": true, "histogram": true,
		"approx_top_k": true, "approx_top_sum": true, "groupconcat": true,
	}

	// builtinFunctionSpecSource is the built-in function catalogue, one signature per line
	// in the format dialectkit.FunctionSpec describes.
	//
	//go:embed builtin_functions.spec
	builtinFunctionSpecSource string

	// builtinFunctionSpec decodes builtinFunctionSpecSource the first time a catalogue is
	// built. The signatures that decoded are registered regardless, and the decode error of
	// any line that did not is kept on the dialect, which ApplyDDL and AnalyseQuery return.
	builtinFunctionSpec = dialectkit.EmbeddedFunctionSpec(builtinFunctionSpecSource)

	// specTypeResolver resolves the type spellings of builtin_functions.spec through the
	// ClickHouse type parser, knowing besides the spellings the spec uses that a column type
	// does not: the wildcard any, and the families DateTime64 and Decimal32 to Decimal256
	// written bare, without the precision or scale a column type gives them.
	specTypeResolver = newTypeResolver(nil, map[string]sqlmodel.SQLType{
		"any":        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		"datetime64": sqlmodel.NewTemporalType("DateTime64", sqlmodel.TemporalKindTimestamp, true),
		"decimal32":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "Decimal32"),
		"decimal64":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "Decimal64"),
		"decimal128": sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "Decimal128"),
		"decimal256": sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "Decimal256"),
	}, defaultMaxTypeParseDepth)

	// specTypes memoises specType, keyed by spelling.
	specTypes sync.Map

	// embeddedTables decode the built-in signatures of builtin_functions.spec and the
	// aliases of builtin_function_aliases.spec that every dialect New builds starts from.
	embeddedTables = builtinTables{signatures: builtinFunctionSpec, aliases: functionAliasTable}
)

// buildFunctionCatalogue constructs the ClickHouse function catalogue: the built-in
// signatures, then any extra functions, then the aliases of the functions it holds.
//
// A registration that panics is recovered, so the registrations after it still run, and
// its panic is logged once with its stack and returned among the errors. A line of an
// embedded table that does not decode leaves out only its own signature or alias.
//
// Takes tables (builtinTables) which decode the built-in signatures and the alias table.
// Takes extras ([]func(*FunctionCatalogueBuilder)) which register extra signatures, in
// order, after the built-in signatures.
// Takes types (*ast.TypeCatalogue) which is the dialect's merged type catalogue, so the
// builder's Type method resolves the dialect's own types, extra types included.
//
// Returns *ast.FunctionCatalogue which is the finalised catalogue.
// Returns error which joins the decode errors of the built-in spec and the alias table
// and a dialectkit.PanicError for each registration that panicked, or nil when nothing
// went wrong.
func buildFunctionCatalogue(tables builtinTables, extras []func(*FunctionCatalogueBuilder), types *ast.TypeCatalogue) (*ast.FunctionCatalogue, error) {
	builder := &FunctionCatalogueBuilder{catalogueBuilder: dialectkit.NewTypedCatalogueBuilder(types.Types)}
	problems := make([]error, 0, len(extras)+builtinTableCount)
	problems = append(problems, builder.registerBuiltinFunctions(tables.signatures))
	for _, register := range extras {
		problems = append(problems, runRegistration(builder, register))
	}
	problems = append(problems, builder.registerFunctionAliases(tables.aliases))
	return builder.Catalogue, errors.Join(problems...)
}

// specType resolves a built-in type spelling through the ClickHouse type parser.
//
// Invalid spellings produce unknown types. Results are cached by spelling and shared
// across catalogues.
//
// Takes spelling (string) which is the type spelling.
//
// Returns sqlmodel.SQLType which is the type it names.
func specType(spelling string) sqlmodel.SQLType {
	if cached, found := specTypes.Load(spelling); found {
		if sqlType, isType := cached.(sqlmodel.SQLType); isType {
			return sqlType
		}
	}
	sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, spelling)
	if parsed, err := specTypeResolver.parse(spelling); err == nil {
		sqlType = parsed.SQLType
		markTupleFieldsPositional(&sqlType)
	}
	specTypes.Store(spelling, sqlType)
	return sqlType
}

// markTupleFieldsPositional marks every field of every tuple within a type positional,
// keeping the fields' labels as their names.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type to mark in place.
func markTupleFieldsPositional(sqlType *sqlmodel.SQLType) {
	for index := range sqlType.StructFields {
		sqlType.StructFields[index].Positional = true
		markTupleFieldsPositional(&sqlType.StructFields[index].SQLType)
	}
	if sqlType.ElementType != nil {
		markTupleFieldsPositional(sqlType.ElementType)
	}
	if sqlType.KeyType != nil {
		markTupleFieldsPositional(sqlType.KeyType)
	}
}

// defaultNullBehaviour returns ClickHouse's default NULL behaviour for a signature: the
// aggregate rule for an aggregate and the scalar rule for any other function.
//
// Takes name (string) which is the function's name.
// Takes aggregate (bool) which is true for an aggregate.
//
// Returns sqlmodel.FunctionNullableBehaviour which is the function's NULL behaviour.
func defaultNullBehaviour(name string, aggregate bool) sqlmodel.FunctionNullableBehaviour {
	if aggregate {
		return aggregateNullBehaviour(name)
	}
	return scalarNullBehaviour(name)
}

// scalarNullBehaviour returns ClickHouse's NULL behaviour for an ordinary function.
//
// Most ClickHouse functions use the server's default implementation for NULLs: the result
// is NULL exactly when an argument is, so a function of values that are never NULL never
// returns NULL. The exceptions are the functions that can return NULL from values that
// are not, the OrNull family, nullIf, toNullable and the Variant and Dynamic element
// readers, which may always return NULL, and the NULL tests and assumeNotNull and
// toTypeName, which never return NULL. coalesce and ifNull are typed from their arguments
// by the parser, and if and multiIf by the resolver.
//
// Takes name (string) which is the function's name.
//
// Returns sqlmodel.FunctionNullableBehaviour which is the function's NULL behaviour.
func scalarNullBehaviour(name string) sqlmodel.FunctionNullableBehaviour {
	lowered := strings.ToLower(name)
	switch {
	case neverNullFunctions[lowered]:
		return sqlmodel.FunctionNullableNeverNull
	case strings.HasSuffix(name, "OrNull") || mayReturnNullFunctions[lowered]:
		return sqlmodel.FunctionNullableCalledOnNull
	default:
		return sqlmodel.FunctionNullableReturnsNullOnNull
	}
}

// aggregateNullBehaviour returns the catalogue's nullability rule for an aggregate.
//
// Counting, collecting and sketching aggregates are marked never nullable. The OrNull
// family is marked potentially nullable regardless of its arguments; other aggregates
// conservatively inherit their arguments' nullability.
//
// Takes name (string) which is the aggregate's name.
//
// Returns sqlmodel.FunctionNullableBehaviour which is the aggregate's NULL behaviour.
func aggregateNullBehaviour(name string) sqlmodel.FunctionNullableBehaviour {
	lowered := strings.ToLower(name)
	switch {
	case strings.HasSuffix(name, "OrNull"):
		return sqlmodel.FunctionNullableCalledOnNull
	case neverNullAggregates[lowered] || strings.HasPrefix(lowered, "uniq") || strings.HasPrefix(lowered, "grouparray") ||
		strings.HasPrefix(lowered, "count"):
		return sqlmodel.FunctionNullableNeverNull
	default:
		return sqlmodel.FunctionNullableReturnsNullOnNull
	}
}

// builtinArguments converts positional argument types into required, unnamed function
// arguments.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the argument types in order.
//
// Returns []sqlmodel.FunctionArgument which holds one required argument per type.
func builtinArguments(argumentTypes []sqlmodel.SQLType) []sqlmodel.FunctionArgument {
	arguments := make([]sqlmodel.FunctionArgument, len(argumentTypes))
	for index := range argumentTypes {
		arguments[index] = sqlmodel.FunctionArgument{Type: argumentTypes[index], Name: "", IsOptional: false, NotNull: false}
	}
	return arguments
}

// runRegistration runs one WithExtraFunctions registration, turning a panic in it into a
// returned error.
//
// New takes no logger, so the panic's stack is logged once through the default logger.
//
// Takes builder (*FunctionCatalogueBuilder) which receives the registration's signatures.
// Takes register (func(*FunctionCatalogueBuilder)) which is the registration.
//
// Returns error which is a dialectkit.PanicError when the registration panicked, or nil.
func runRegistration(builder *FunctionCatalogueBuilder, register func(*FunctionCatalogueBuilder)) (err error) {
	defer func() {
		dialectkit.Recover[struct{}](context.Background(), "clickhouse: register extra functions", recover(), nil, &err)
	}()
	register(builder)
	return nil
}
