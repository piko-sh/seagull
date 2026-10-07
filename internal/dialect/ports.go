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

package dialect

import (
	"context"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// ParserPort parses SQL into statements, replays DDL into catalogue mutations and
// analyses queries into the analyser's neutral intermediate form.
type ParserPort interface {
	// ParseStatements parses a SQL string into a sequence of parsed statements using the
	// dialect's native parser.
	//
	// Takes sql (string) which is the raw SQL text to parse.
	//
	// Returns []ast.ParsedStatement which contains the parsed AST nodes.
	// Returns error when the SQL contains syntax errors, or when the context was cancelled
	// before parsing.
	ParseStatements(ctx context.Context, sql string) ([]ast.ParsedStatement, error)

	// ApplyDDL interprets a parsed DDL statement and returns the mutation it applies to the
	// schema catalogue. Non-DDL statements (DML) are ignored and return a nil mutation
	// without error.
	//
	// Takes statement (ast.ParsedStatement) which is a parsed SQL statement from
	// ParseStatements.
	//
	// Returns *ast.CatalogueMutation which describes the schema change, or nil if the
	// statement is not DDL.
	// Returns error when the DDL is malformed or references non-existent objects, or when
	// the context was cancelled before dispatch.
	ApplyDDL(ctx context.Context, statement ast.ParsedStatement) (*ast.CatalogueMutation, error)

	// AnalyseQuery analyses a DML query against the catalogue, producing a raw analysis
	// result with output columns, parameter references, and scope information. The analyser
	// performs further type resolution and nullability propagation on this result.
	//
	// Takes catalogue (*sqlmodel.Catalogue) which is the current schema state.
	// Takes statement (ast.ParsedStatement) which is the query to analyse.
	//
	// Returns *ast.RawQueryAnalysis which contains unresolved column and parameter
	// references for the analyser to type-check.
	// Returns error when the query references non-existent tables or has structural errors,
	// or when the context was cancelled before analysis.
	AnalyseQuery(ctx context.Context, catalogue *sqlmodel.Catalogue, statement ast.ParsedStatement) (*ast.RawQueryAnalysis, error)
}

// TypeNormaliserPort turns a dialect's type names into structured SQL types. It is the
// part of a dialect that live-database introspection needs.
type TypeNormaliserPort interface {
	// NormaliseTypeName converts a dialect-specific type name and optional modifiers
	// (precision, scale, length) into a structured SQLType.
	//
	// The result carries the structural attributes of its category, which the analyser and
	// the Go mapping read instead of the name: Width, and Unsigned for an integer; Width for
	// a float; Temporal, and TimeZone where it applies, for a temporal type; Network for a
	// network type; Monetary for a money type. EngineName is kept for display and as the key
	// of a per-engine type override.
	//
	// Takes name (string) which is the dialect-native type name (e.g. "varchar", "numeric",
	// "int8").
	// Takes modifiers (...int) which are optional type parameters such as precision, scale,
	// or length.
	//
	// Returns sqlmodel.SQLType which is the structured type representation.
	NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType
}

// OperatorTyperPort types the results of a dialect's operators.
//
// Each method reports ok as false for an operator it leaves to the analyser's structural
// default: a binary operator over two values of one category promotes them through
// PromoteType and CanImplicitCast, a unary operator keeps its operand's type, and either
// is NULL when an operand is NULL. A dialect answers for every operator whose result
// differs, such as a division that always yields an exact decimal or a JSON extraction
// whose type depends on the value it reads.
//
// The nullability result uses the function vocabulary: FunctionNullableReturnsNullOnNull
// for a result that is NULL when an operand is NULL, FunctionNullableNeverNull for one
// that is never NULL, and FunctionNullableCalledOnNull for one that may be NULL whatever
// its operands, such as an extraction that finds nothing.
type OperatorTyperPort interface {
	// BinaryResult types a binary operator applied to two operands.
	//
	// Takes operator (string) which is the operator as the dialect's parser spells it.
	// Takes left (sqlmodel.SQLType) which is the left operand's type.
	// Takes right (sqlmodel.SQLType) which is the right operand's type.
	//
	// Returns sqlmodel.SQLType which is the result's type.
	// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
	// Returns bool which is false to leave the operator to the structural default.
	BinaryResult(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool)

	// UnaryResult types a prefix operator applied to one operand.
	//
	// Takes operator (string) which is the operator as the dialect's parser spells it.
	// Takes operand (sqlmodel.SQLType) which is the operand's type.
	//
	// Returns sqlmodel.SQLType which is the result's type.
	// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
	// Returns bool which is false to leave the operator to the structural default.
	UnaryResult(operator string, operand sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool)
}

// TypeSystemPort provides type normalisation, promotion, implicit casting, operator
// typing and the dialect's core types. The type and function resolvers depend on this
// narrow interface rather than the whole DialectPort.
type TypeSystemPort interface {
	TypeNormaliserPort

	OperatorTyperPort

	// CoreTypes returns the dialect's types for the values the analyser types itself.
	//
	// Returns CoreTypes which holds the types, each built with the dialect's normaliser.
	CoreTypes() CoreTypes

	// PromoteType selects the wider type when both operands share the same category but
	// differ in precision or width (e.g. a 16-bit and a 64-bit integer promote to the 64-bit
	// integer).
	//
	// Takes left (sqlmodel.SQLType) which is the first operand type.
	// Takes right (sqlmodel.SQLType) which is the second operand type.
	//
	// Returns sqlmodel.SQLType which is the promoted type.
	PromoteType(left sqlmodel.SQLType, right sqlmodel.SQLType) sqlmodel.SQLType

	// CanImplicitCast reports whether a value of the source type can be implicitly cast to
	// the target type according to the dialect's casting rules. Both types are whole, so a
	// dialect can rule on width and signedness as well as on category.
	//
	// Takes from (sqlmodel.SQLType) which is the source type.
	// Takes to (sqlmodel.SQLType) which is the target type.
	//
	// Returns bool which is true if the implicit cast is allowed.
	CanImplicitCast(from sqlmodel.SQLType, to sqlmodel.SQLType) bool
}

// BuiltinsPort provides the dialect's built-in functions, types and table-valued
// functions.
type BuiltinsPort interface {
	// BuiltinFunctions returns the dialect's catalogue of built-in functions with their
	// signatures, overloads, return types, and nullability behaviour.
	//
	// Returns *ast.FunctionCatalogue which maps function names to their overloaded
	// signatures.
	BuiltinFunctions() *ast.FunctionCatalogue

	// BuiltinTypes returns the dialect's catalogue of built-in SQL types, mapping
	// dialect-specific type names to structured SQLType values.
	//
	// Returns *ast.TypeCatalogue which maps type names to SQLType.
	BuiltinTypes() *ast.TypeCatalogue

	// TableValuedFunctionColumns returns the output column schema for a known table-valued
	// function (e.g. json_each, generate_series), or nil if the function is not recognised.
	//
	// Takes functionName (string) which is the table-valued function to look up.
	//
	// Returns []ast.ScopedColumn which holds the output columns, or nil for unknown
	// functions.
	TableValuedFunctionColumns(functionName string) []ast.ScopedColumn
}

// DialectPort is the interface a SQL dialect implements so Seagull can replay its
// migrations and analyse its queries.
//
// Optional capabilities are separate interfaces that the analyser detects with a type
// assertion: FunctionResolverPort, CatalogueFunctionResolverPort,
// TableValuedFunctionTyperPort, ElementNullabilityPort, ExtensionLoaderPort,
// PredicateTyperPort, RowValueTyperPort, ArrayTyperPort, AliasPrecedencePort,
// ArrayJoinTyperPort, ResultCheckerPort and ArgumentCoercerPort.
//
// The methods of a DialectPort, and of its optional capabilities, should not panic. A
// method that takes a context recovers a panic of its own at entry, through
// dialectkit.Recover, and returns it as an error that matches seagull.ErrPanic. Seagull
// also recovers a panic at the boundaries where it runs analysis, replays migrations and
// runs its pipeline, reports it as diagnostic Q070 and logs its stack once. A panic in a
// method an embedder calls directly, outside those boundaries, is not recovered.
type DialectPort interface {
	ParserPort

	TypeSystemPort

	BuiltinsPort

	// Info describes the dialect.
	//
	// Returns Info which holds the dialect's identity, syntax and capabilities.
	Info() Info
}

// CatalogueFunctionResolverPort is an optional interface for resolving user-defined
// functions returning composite or set-of types from the catalogue.
type CatalogueFunctionResolverPort interface {
	// TableValuedFunctionColumnsFromCatalogue resolves the output columns of a user-defined
	// table-valued function by looking up its signature and return type in the catalogue.
	//
	// Takes catalogue (*sqlmodel.Catalogue) which is the schema to look the function up in.
	// Takes functionName (string) which is the table-valued function to resolve.
	//
	// Returns []ast.ScopedColumn which are the output columns, and nil when the function is
	// not found or its return type cannot be expanded into columns.
	// Returns error when resolving the function failed, such as when the context was
	// cancelled or the dialect recovered a panic; the caller treats the function as giving
	// no columns.
	TableValuedFunctionColumnsFromCatalogue(
		ctx context.Context,
		catalogue *sqlmodel.Catalogue,
		functionName string,
	) ([]ast.ScopedColumn, error)
}

// TableValuedFunctionTyperPort types table-valued functions whose columns depend on their
// arguments, such as a function returning a row of the composite type its first argument
// has, whose fields the catalogue defines.
//
// The analyser consults the interface before the name-only built-in column catalogue.
type TableValuedFunctionTyperPort interface {
	// TypedTableValuedFunctionColumns returns the columns a call of a table-valued function
	// gives, typed from its arguments.
	//
	// Takes catalogue (*sqlmodel.Catalogue) which holds the schema the query runs against,
	// such as the composite types and tables an argument's type may name; it may be nil.
	// Takes functionName (string) which is the function's name as the reference writes it,
	// lower-cased and optionally schema-qualified.
	// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of the call's arguments
	// in order; an argument whose type is not known, such as a parameter nothing types, has
	// the unknown category.
	//
	// Returns []ast.ScopedColumn which holds the columns, or nil to leave the call to
	// BuiltinsPort.TableValuedFunctionColumns.
	TypedTableValuedFunctionColumns(
		catalogue *sqlmodel.Catalogue,
		functionName string,
		argumentTypes []sqlmodel.SQLType,
	) []ast.ScopedColumn
}

// ElementNullabilityPort describes dialects that allow NULL inside collections regardless
// of the declared element type.
//
// The generator uses pointers for those elements unless configuration declares them
// non-null.
type ElementNullabilityPort interface {
	// HoldsNullElements reports whether a value of a type can hold NULL among its elements
	// or its map values.
	//
	// Takes sqlType (sqlmodel.SQLType) which is an array, list or map type, or any other
	// type, for which it reports false.
	//
	// Returns bool which is true when the engine can hold NULL there.
	HoldsNullElements(sqlType sqlmodel.SQLType) bool
}

// FunctionResolverPort is an optional interface that dialects implement to handle
// context-dependent or polymorphic function resolution that the standard overload
// resolution cannot match, such as ARRAY_AGG, whose return type depends on the argument
// types.
type FunctionResolverPort interface {
	// ResolveFunctionCall types calls the catalogue cannot resolve because the name,
	// matching overload or return type is unknown. The dialect may use argument types and
	// catalogue context; calls with a known return type are never passed here.
	//
	// Takes catalogue (*sqlmodel.Catalogue) which is the schema context.
	// Takes name (string) which is the function name.
	// Takes schema (string) which is the function schema, if specified.
	// Takes argumentTypes ([]sqlmodel.SQLType) which are the resolved argument types.
	//
	// Returns *ast.FunctionResolution which is the resolved function call.
	// Returns error when the function cannot be resolved, the context was cancelled or the
	// dialect recovered a panic.
	ResolveFunctionCall(
		ctx context.Context,
		catalogue *sqlmodel.Catalogue,
		name string,
		schema string,
		argumentTypes []sqlmodel.SQLType,
	) (*ast.FunctionResolution, error)
}

// PredicateTyperPort is an optional interface for a dialect whose predicates do not all
// return CoreTypes.Boolean: a comparison or pattern match, an IS NULL, IN, BETWEEN or
// EXISTS test, and the logical operators AND and OR.
//
// The analyser asks it for every predicate it types, and keeps the core boolean where it
// declines, NULL when an operand the predicate reads may be, except for an IS NULL or
// EXISTS test and a comparison Info.OperatorFeatures lists as a NULL test.
type PredicateTyperPort interface {
	// PredicateResult types a predicate from the types of its operands.
	//
	// Takes predicate (ast.Expression) which is the predicate: an *ast.ComparisonExpression,
	// *ast.IsNullExpression, *ast.InListExpression, *ast.BetweenExpression,
	// *ast.ExistsExpression or *ast.LogicalOpExpression.
	// Takes operands ([]sqlmodel.SQLType) which holds the types of the predicate's operands
	// in order: Left and Right of a comparison; the Inner of an IS NULL test; the Inner and
	// then each of the Values of an IN list; the Inner, Low and High of a BETWEEN; none for
	// EXISTS; and the Operands of a logical operator.
	//
	// Returns sqlmodel.SQLType which is the result's type.
	// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL, read
	// over the operands in the same order, so FunctionNullableNullWhenArgumentNull makes it
	// NULL when the first operand is.
	// Returns bool which is false to leave the predicate to the core boolean.
	PredicateResult(predicate ast.Expression, operands []sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool)
}

// RowValueTyperPort lets a dialect type a relation referenced as a value.
//
// The analyser uses it when the name resolves to a relation rather than a visible column.
type RowValueTyperPort interface {
	// RowValueType types the current row of a relation a query names as a value.
	//
	// Takes relation (RowRelation) which describes the relation.
	//
	// Returns sqlmodel.SQLType which is the row's type; its Nullable flag says whether the
	// row may be NULL, as it may on the nullable side of an outer join where the engine
	// gives no row there, or where a single value a function produces may be.
	RowValueType(relation RowRelation) sqlmodel.SQLType
}

// ArrayTyperPort names the array type a dialect gives values of one type held in an
// array, as the array a parameter of id = ANY($1) binds, whose elements the comparison
// compares with id.
//
// The analyser uses it to type a parameter a comparison quantified with ANY, ALL or SOME
// reads the elements of; a dialect without it leaves such a parameter untyped.
type ArrayTyperPort interface {
	// ArrayOf returns the array type whose elements have a type.
	//
	// Takes element (sqlmodel.SQLType) which is the elements' type.
	//
	// Returns sqlmodel.SQLType which is the array type.
	ArrayOf(element sqlmodel.SQLType) sqlmodel.SQLType
}

// AliasPrecedencePort describes a dialect where select-list aliases shadow columns of the
// same name in clauses listed by Info.SelectAliases. Qualified references and references
// within the item defining an alias still resolve to columns; visible columns otherwise
// take precedence.
type AliasPrecedencePort interface {
	// SelectAliasesShadowColumns reports whether a select-list alias takes precedence over a
	// column of the same name.
	//
	// Returns bool which is true when the alias wins.
	SelectAliasesShadowColumns() bool
}

// ArrayJoinTyperPort types the element an ARRAY JOIN unfolds a value into where the value
// is not a plain array, such as a ClickHouse Map, which an ARRAY JOIN unfolds into one
// Tuple(keys K, values V) per entry.
//
// The analyser uses it for every ARRAY JOIN source, the entry's alias and a column
// unfolded in place alike; without it, or where it reports false, a typed array gives its
// element and any other value its own type.
type ArrayJoinTyperPort interface {
	// ArrayJoinElement types each element an ARRAY JOIN unfolds a value of a type into.
	//
	// Takes source (sqlmodel.SQLType) which is the type of the value unfolded.
	//
	// Returns sqlmodel.SQLType which is the element's type; its Nullable flag says whether
	// the element may be NULL.
	// Returns bool which is false to leave the source to the analyser's default.
	ArrayJoinElement(source sqlmodel.SQLType) (sqlmodel.SQLType, bool)
}

// ResultCheckerPort identifies result types that a dialect's drivers cannot read.
//
// The analyser reports refused columns before generating code, placing each diagnostic at
// its output item.
type ResultCheckerPort interface {
	// UnreadableResult reports why the dialect's drivers cannot read a value of a type and
	// how to write the query so they can.
	//
	// Takes sqlType (sqlmodel.SQLType) which is the type of a column a query returns.
	//
	// Returns ResultProblem which says why and how.
	// Returns bool which is false for a type the drivers read.
	UnreadableResult(sqlType sqlmodel.SQLType) (ResultProblem, bool)
}

// CoalesceTyperPort is implemented by an engine that types COALESCE only by its arguments
// up to the first that cannot be NULL, as ClickHouse does.
type CoalesceTyperPort interface {
	// CoalesceTypedUpToNonNullable reports whether arguments after the first non-nullable
	// one take no part in the type of a COALESCE.
	//
	// Returns bool which is true when they take no part.
	CoalesceTypedUpToNonNullable() bool
}

// UntypedArgumentPort is implemented by an engine that chooses a placeholder's type when
// a function's overloads take different types at its position.
type UntypedArgumentPort interface {
	// UntypedArgumentType chooses the type of an untyped argument from the types the
	// applicable overloads declare at its position.
	//
	// Takes candidates ([]sqlmodel.SQLType) which holds the declared types, in overload
	// order, more than one of them different.
	//
	// Returns sqlmodel.SQLType which is the chosen type.
	// Returns bool which is false when the engine cannot choose, leaving the argument
	// untyped.
	UntypedArgumentType(candidates []sqlmodel.SQLType) (sqlmodel.SQLType, bool)
}

// ArgumentCoercerPort describes conversions allowed specifically for function arguments.
//
// It is consulted after CanImplicitCast rejects an argument, without changing the common
// type of expressions. The analyser also consults it for an explicit conversion, such as
// CAST(x AS DATE), of a value CanImplicitCast does not convert: where the coercion of an
// argument of the value's type to a parameter of the target type may be NULL, so may the
// conversion.
type ArgumentCoercerPort interface {
	// CoerceArgument reports whether the engine converts an argument to a function parameter
	// of another type, and how.
	//
	// Takes argument (CallArgument) which is the value the call passes: its type and whether
	// the query writes it as a literal.
	// Takes parameter (sqlmodel.SQLType) which is the type the function's parameter
	// declares.
	//
	// Returns ArgumentCoercion which says how the engine converts the argument.
	// Returns bool which is false when the engine does not convert it.
	CoerceArgument(argument CallArgument, parameter sqlmodel.SQLType) (ArgumentCoercion, bool)
}

// ExtensionLoaderPort is an optional interface for providing function definitions for SQL
// extensions (e.g. CREATE EXTENSION pgcrypto).
type ExtensionLoaderPort interface {
	// LoadExtensionFunctions returns function signatures provided by the named extension.
	//
	// Takes name (string) which is the extension to load functions from.
	//
	// Returns []*sqlmodel.Function which are the extension's function signatures, and nil
	// when the extension is unknown.
	LoadExtensionFunctions(name string) []*sqlmodel.Function
}
