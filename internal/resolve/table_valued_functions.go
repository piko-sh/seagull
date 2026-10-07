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

package resolve

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// argumentTyper types the arguments of a table-valued function call, giving the unknown
// category to an argument it cannot type.
type argumentTyper func(ctx context.Context, arguments []ast.Expression) []sqlmodel.SQLType

// scalarCallTyper types a call of a function that returns one value, written as a FROM
// item, as the one column it gives, named after the function, as
// dialect.Info.ScalarFunctionsInFrom describes.
//
// Unknown, set-returning and aggregate functions return false. An interrupted resolver
// returns the interruption error; a recovered panic is returned as an *Error.
type scalarCallTyper func(ctx context.Context, functionName string, arguments []ast.Expression) (ast.ScopedColumn, bool, error)

// tableFunctionCall is one function call a FROM item reads columns from.
type tableFunctionCall struct {
	// functionName is the function's name as the reference writes it, optionally
	// schema-qualified.
	functionName string

	// columnName names the column a call of a function returning one value gives: the item's
	// alias for an item of one call, or empty to name it after the function.
	columnName string

	// arguments holds the call's arguments.
	arguments []ast.Expression
}

// callTypers types the calls of a FROM clause.
type callTypers struct {
	// arguments types the arguments of a call; nil leaves them untyped.
	arguments argumentTyper

	// scalarCall types a call of a function that returns one value; nil types none.
	scalarCall scalarCallTyper
}

// argumentTypes types the arguments of a call.
//
// Takes arguments ([]ast.Expression) which are the call's arguments.
//
// Returns []sqlmodel.SQLType which holds their types, or nil when no typer is set.
func (typers callTypers) argumentTypes(ctx context.Context, arguments []ast.Expression) []sqlmodel.SQLType {
	if typers.arguments == nil {
		return nil
	}
	return typers.arguments(ctx, arguments)
}

// resolveScalarCallColumn types a call of a function that returns one value, written as a
// FROM item, as the one column it gives: named after the function, and typed and as
// nullable as the call is in a select list.
//
// Takes functionName (string) which is the function's name as the reference writes it,
// optionally schema-qualified.
// Takes arguments ([]ast.Expression) which are the call's arguments.
// Takes scope (*ScopeChain) which resolves the arguments' names.
//
// Returns ast.ScopedColumn which is the column.
// Returns bool which is false for a function the resolver does not know, a set-returning
// function and an aggregate, and when the function's resolution failed.
// Returns error which is the interruption that stopped the dialect's resolver, the *Error
// a panic the dialect recovered is reported as, or nil.
func (r *TypeResolver) resolveScalarCallColumn(
	ctx context.Context,
	functionName string,
	arguments []ast.Expression,
	scope *ScopeChain,
) (ast.ScopedColumn, bool, error) {
	schema, name := splitFunctionName(functionName)
	callArguments := r.resolveCallArguments(ctx, arguments, scope, new(false))
	match, resolveError, err := r.functionResolver.resolveCall(ctx, name, schema, callArguments)
	switch {
	case err != nil:
		return ast.ScopedColumn{}, false, err
	case resolveError != nil && resolveError.Code == diag.CodeRecoveredPanic:
		return ast.ScopedColumn{}, false, resolveError
	case resolveError != nil || match == nil || match.returnsSet || match.isAggregate:
		return ast.ScopedColumn{}, false, nil
	}
	return ast.ScopedColumn{
		Name:     name,
		SQLType:  match.returnType,
		Nullable: resultNullable(match.nullableBehaviour, match.nullingArgument, argumentNullability(callArguments)),
	}, true, nil
}

// resolveTableValuedFunctionColumns resolves the output columns of a table-valued
// function reference, independent of any analyser receiver so both the top-level query
// path and the inner-subquery scope builder share one implementation.
//
// Explicit column definitions win; otherwise the engine's built-in table-valued function
// schema is consulted, then (for engines that implement
// dialect.CatalogueFunctionResolverPort) the catalogue is searched for a user-defined
// table-valued function, and then, for an engine whose Info lists ScalarFunctionsInFrom,
// a call of a function returning one value gives one column named after the function. The
// columns are nil when none of these resolve the function. A reference written WITH
// ORDINALITY has the ordinality column the dialect states appended after the columns the
// function gives, before the alias column list renames them by position; explicit column
// definitions are the whole list. The engine's columns are copied, never changed. A
// function whose columns are known only when it runs resolves to no columns, without the
// ordinality column. Explicit column definitions win for a reference of several function
// calls too; otherwise its columns are resolved by resolveFunctionCallsColumns.
//
// Takes engine (dialect.DialectPort) which supplies the type normaliser and built-in
// table-valued function schema.
// Takes catalogue (*sqlmodel.Catalogue) which holds user-defined functions for the
// optional catalogue fallback.
// Takes reference (ast.RawTableValuedFunctionReference) which holds the function
// reference with optional column definitions.
// Takes typers (callTypers) which type the arguments of each call and, for an engine
// whose Info lists ScalarFunctionsInFrom, a call of a function returning one value.
//
// Returns []ast.ScopedColumn which holds the resolved columns, or nil if the function
// cannot be resolved.
// Returns *Error which is non-nil when the alias count exceeds the resolved column count,
// when a call of a reference of several calls is unknown, or when the engine panicked
// resolving a call's columns.
// Returns error which is the interruption that stopped the engine, or nil.
func resolveTableValuedFunctionColumns(
	ctx context.Context,
	engine dialect.DialectPort,
	catalogue *sqlmodel.Catalogue,
	reference ast.RawTableValuedFunctionReference,
	typers callTypers,
) ([]ast.ScopedColumn, *Error, error) {
	if len(reference.ColumnDefinitions) > 0 && reference.ColumnDefinitions[0].TypeName != "" {
		return definedColumns(engine, catalogue, reference.ColumnDefinitions), nil, nil
	}
	if len(reference.Functions) > 0 {
		return resolveFunctionCallsColumns(ctx, engine, catalogue, reference, typers)
	}

	columns, problem, err := functionColumns(ctx, engine, catalogue, tableFunctionCall{
		functionName: reference.FunctionName,
		arguments:    reference.Arguments,
		columnName:   reference.Alias,
	}, typers)
	if err != nil || problem != nil || columns == nil {
		return nil, problem, err
	}
	if len(columns) == 0 {
		return columns, nil, nil
	}
	columns = slices.Clone(columns)
	if reference.Ordinality != nil {
		columns = append(columns, *reference.Ordinality)
	}
	return columns, applyTableValuedFunctionAliases(reference, columns), nil
}

// resolveFunctionCallsColumns combines function-call columns, nullable padding and
// ordinality.
//
// Each call's column definition list takes precedence. Columns are copied before alias
// renaming, preserving the engine's catalogue.
//
// Takes engine (dialect.DialectPort) which supplies the type normaliser and built-in
// table-valued function schema.
// Takes catalogue (*sqlmodel.Catalogue) which holds user-defined functions.
// Takes reference (ast.RawTableValuedFunctionReference) which holds the calls.
// Takes typers (callTypers) which type the calls.
//
// Returns []ast.ScopedColumn which holds the resolved columns, or nil when a call's
// columns are not known.
// Returns *Error which names the first call whose columns are not known or whose columns
// the engine panicked resolving, reports surplus column aliases, or is nil.
// Returns error which is the interruption that stopped the engine, or nil.
func resolveFunctionCallsColumns(
	ctx context.Context,
	engine dialect.DialectPort,
	catalogue *sqlmodel.Catalogue,
	reference ast.RawTableValuedFunctionReference,
	typers callTypers,
) ([]ast.ScopedColumn, *Error, error) {
	var columns []ast.ScopedColumn
	for _, call := range reference.Functions {
		callColumns, problem, err := functionColumns(ctx, engine, catalogue, tableFunctionCall{
			functionName: call.FunctionName,
			arguments:    call.Arguments,
			columnName:   "",
		}, typers)
		if err != nil {
			return nil, nil, err
		}
		if len(call.ColumnDefinitions) > 0 && call.ColumnDefinitions[0].TypeName != "" {
			callColumns, problem = definedColumns(engine, catalogue, call.ColumnDefinitions), nil
		}
		if problem != nil {
			return nil, problem, nil
		}
		if len(callColumns) == 0 {
			return nil, newError(diag.CodeUnknownTable, "unknown table-valued function %q", call.FunctionName), nil
		}
		for index := range callColumns {
			columns = append(columns, callColumns[index])
			columns[len(columns)-1].Nullable = true
		}
	}
	if reference.Ordinality != nil {
		columns = append(columns, *reference.Ordinality)
	}
	return columns, applyTableValuedFunctionAliases(reference, columns), nil
}

// definedColumns types the columns a column definition list names, each of which may be
// NULL. A type the list names is resolved against the catalogue's domains, enums and
// composite types, as a column's type is.
//
// Takes engine (dialect.DialectPort) which normalises the definitions' type names.
// Takes catalogue (*sqlmodel.Catalogue) which holds the named types, or nil.
// Takes definitions ([]ast.TVFColumnDefinition) which name and type the columns.
//
// Returns []ast.ScopedColumn which holds the columns in order.
func definedColumns(engine dialect.DialectPort, catalogue *sqlmodel.Catalogue, definitions []ast.TVFColumnDefinition) []ast.ScopedColumn {
	columns := make([]ast.ScopedColumn, len(definitions))
	for index, definition := range definitions {
		columns[index] = ast.ScopedColumn{
			Name:     definition.Name,
			SQLType:  resolveNamedTypes(catalogue, engine.NormaliseTypeName(definition.TypeName), 0),
			Nullable: true,
		}
	}
	return columns
}

// functionColumns resolves columns of functions in FROM through dialect typing, built-in
// definitions and catalogue functions, in that order. It also supports scalar functions
// in FROM when the dialect allows them.
//
// Takes engine (dialect.DialectPort) which supplies the built-in table-valued functions.
// Takes catalogue (*sqlmodel.Catalogue) which holds user-defined functions.
// Takes call (tableFunctionCall) which is the call.
// Takes typers (callTypers) which type the call.
//
// Returns []ast.ScopedColumn which holds the columns, shared with the engine, or nil when
// the function is not known.
// Returns *Error which is the Q070 error when the engine panicked resolving the
// function's columns from the catalogue, which leaves the function without columns.
// Returns error which is the interruption that stopped the engine, or nil.
func functionColumns(
	ctx context.Context,
	engine dialect.DialectPort,
	catalogue *sqlmodel.Catalogue,
	call tableFunctionCall,
	typers callTypers,
) ([]ast.ScopedColumn, *Error, error) {
	argumentTypes := typers.argumentTypes(ctx, call.arguments)
	if typer, ok := engine.(dialect.TableValuedFunctionTyperPort); ok && argumentTypes != nil {
		if columns := typer.TypedTableValuedFunctionColumns(catalogue, call.functionName, argumentTypes); columns != nil {
			return columns, nil, nil
		}
	}
	columns := engine.TableValuedFunctionColumns(call.functionName)
	if columns == nil {
		resolved, problem, err := catalogueFunctionColumns(ctx, engine, catalogue, call.functionName)
		if problem != nil || err != nil {
			return nil, problem, err
		}
		columns = resolved
	}
	if columns == nil && typers.scalarCall != nil && engine.Info().ScalarFunctionsInFrom {
		return scalarCallColumns(ctx, call, typers.scalarCall)
	}
	return columns, nil, nil
}

// scalarCallColumns types a call of a function that returns one value, written as a FROM
// item, as the one column it gives: named after the item's alias for an item of one call,
// and otherwise after the function.
//
// Takes call (tableFunctionCall) which is the call.
// Takes scalarCall (scalarCallTyper) which types it.
//
// Returns []ast.ScopedColumn which holds the column, or nil when the call is not typed.
// Returns *Error which is the Q070 error when the dialect's resolver panicked, or nil.
// Returns error which is the interruption that stopped the dialect's resolver, or nil.
func scalarCallColumns(ctx context.Context, call tableFunctionCall, scalarCall scalarCallTyper) ([]ast.ScopedColumn, *Error, error) {
	column, typed, err := scalarCall(ctx, call.functionName, call.arguments)
	if problem, isProblem := errors.AsType[*Error](err); isProblem {
		return nil, problem, nil
	}
	if err != nil || !typed {
		return nil, nil, err
	}
	column.Name = cmp.Or(call.columnName, column.Name)
	return []ast.ScopedColumn{column}, nil, nil
}

// catalogueFunctionColumns asks an engine that implements
// dialect.CatalogueFunctionResolverPort for the columns of a table-valued function the
// catalogue defines.
//
// An error the engine returns is sorted by engineFailure: a recovered panic is a Q070
// error, an interruption is returned, and any other error leaves the function without
// columns.
//
// Takes engine (dialect.DialectPort) which may resolve catalogue functions.
// Takes catalogue (*sqlmodel.Catalogue) which holds user-defined functions.
// Takes functionName (string) which is the function's name as the reference writes it.
//
// Returns []ast.ScopedColumn which holds the columns, or nil when the engine gives none.
// Returns *Error which is the Q070 error when the engine panicked, or nil.
// Returns error which is the interruption that stopped the engine, or nil.
func catalogueFunctionColumns(
	ctx context.Context,
	engine dialect.DialectPort,
	catalogue *sqlmodel.Catalogue,
	functionName string,
) ([]ast.ScopedColumn, *Error, error) {
	resolver, ok := engine.(dialect.CatalogueFunctionResolverPort)
	if !ok {
		return nil, nil, nil
	}
	columns, err := resolver.TableValuedFunctionColumnsFromCatalogue(ctx, catalogue, functionName)
	if err != nil {
		problem, interruption := engineFailure(ctx, err, fmt.Sprintf("table-valued function %q", functionName))
		return nil, problem, interruption
	}
	return columns, nil, nil
}

// applyTableValuedFunctionAliases renames the resolved columns of a table-valued function
// in place from the call's alias-only column definitions. Aliases beyond the resolved
// column count cannot be applied; in that case the in-range aliases are still applied and
// a warning diagnostic is returned describing the count mismatch.
//
// Takes reference (ast.RawTableValuedFunctionReference) which holds the alias
// definitions.
// Takes columns ([]ast.ScopedColumn) which holds the resolved columns to rename.
//
// Returns *Error which is non-nil only when aliases outnumber columns.
func applyTableValuedFunctionAliases(
	reference ast.RawTableValuedFunctionReference,
	columns []ast.ScopedColumn,
) *Error {
	if len(reference.ColumnDefinitions) == 0 {
		return nil
	}
	applied := min(len(reference.ColumnDefinitions), len(columns))
	for index := range applied {
		columns[index].Name = reference.ColumnDefinitions[index].Name
	}
	if len(reference.ColumnDefinitions) <= len(columns) {
		return nil
	}
	return newError(diag.CodeCompoundColumnCount,
		"table-valued function %q exposes %d columns but %d column aliases were supplied; surplus aliases are ignored",
		reference.FunctionName, len(columns), len(reference.ColumnDefinitions))
}
