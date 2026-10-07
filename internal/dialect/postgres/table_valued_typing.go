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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// seriesFunction counts from its first argument to its second, by its third, giving
	// values of the type the bounds meet in.
	seriesFunction = "generate_series"

	// catalogueSchema is the schema PostgreSQL's built-in functions live in, which a call
	// may name.
	catalogueSchema = "pg_catalog"
)

// TypedTableValuedFunctionColumns resolves argument-dependent columns for
// generate_series, unnest, record-population functions and overloaded set-returning
// functions. Calls whose arguments do not determine their columns are left to
// TableValuedFunctionColumns.
//
// Takes catalogue (*sqlmodel.Catalogue) which defines the composite types and tables an
// argument's type may name; nil defines none.
// Takes functionName (string) which is the function's name as the reference writes it.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of its arguments.
//
// Returns []ast.ScopedColumn which holds the typed columns, or nil.
func (engine *Dialect) TypedTableValuedFunctionColumns(
	catalogue *sqlmodel.Catalogue,
	functionName string,
	argumentTypes []sqlmodel.SQLType,
) []ast.ScopedColumn {
	schemaName, bareName := splitQualifiedName(strings.ToLower(functionName))
	if schemaName != "" && schemaName != catalogueSchema {
		return nil
	}
	switch bareName {
	case seriesFunction:
		if seriesType, typed := engine.seriesType(argumentTypes); typed {
			return []ast.ScopedColumn{{Name: seriesFunction, SQLType: seriesType, Nullable: false}}
		}
	case multipleArrayUnnest:
		return engine.unnestColumns(argumentTypes)
	case "json_populate_record", "jsonb_populate_record", "json_populate_recordset", "jsonb_populate_recordset":
		if len(argumentTypes) > 0 {
			return rowColumns(catalogue, argumentTypes[0])
		}
	default:
		if _, listed := tableValuedFunctionColumns[bareName]; !listed && engine.functions != nil &&
			setReturningShape(engine.functions.Functions[bareName], bareName) == nil {
			return selectedOverloadShape(engine.functions, bareName, argumentTypes)
		}
	}
	return nil
}

// unnestColumns types the column of an unnest of one array by the array's element type,
// and the columns of an unnest of a value that is no array, such as the TimescaleDB
// toolkit's timevector, by the overload its type selects.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of the call's arguments.
//
// Returns []ast.ScopedColumn which holds the typed columns, or nil.
func (engine *Dialect) unnestColumns(argumentTypes []sqlmodel.SQLType) []ast.ScopedColumn {
	if len(argumentTypes) != 1 {
		return nil
	}
	if argumentTypes[0].Category != sqlmodel.TypeCategoryArray {
		return selectedOverloadShape(engine.functions, multipleArrayUnnest, argumentTypes)
	}
	if argumentTypes[0].ElementType == nil {
		return nil
	}
	return []ast.ScopedColumn{{Name: multipleArrayUnnest, SQLType: *argumentTypes[0].ElementType, Nullable: true}}
}

// seriesType types generate_series values from their bounds and interval.
//
// Temporal bounds yield timestamps, while numeric bounds use their common type. An
// interval step disambiguates unknown temporal bounds.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of the call's arguments:
// the bounds, then the step and any time zone.
//
// Returns sqlmodel.SQLType which is the type of the values.
// Returns bool which is false when the bounds do not decide it.
func (engine *Dialect) seriesType(argumentTypes []sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	if len(argumentTypes) < 2 {
		return sqlmodel.SQLType{}, false
	}
	start, stop := argumentTypes[0], argumentTypes[1]
	switch {
	case isPointInTime(start) || isPointInTime(stop):
		return engine.temporalSeriesType(start, stop), true
	case start.Category == sqlmodel.TypeCategoryUnknown && stop.Category == sqlmodel.TypeCategoryUnknown:
		if len(argumentTypes) > 2 && isInterval(argumentTypes[2]) {
			return engine.NormaliseTypeName("timestamptz"), true
		}
		return sqlmodel.SQLType{}, false
	default:
		return engine.numericSeriesType(start, stop)
	}
}

// temporalSeriesType returns the type of the values a series between points in time
// gives: a timestamp when every bound of known type is one without a time zone, and a
// timestamp with time zone otherwise, as a date bound is stepped as one.
//
// Takes start (sqlmodel.SQLType) which is the first bound's type.
// Takes stop (sqlmodel.SQLType) which is the second bound's type.
//
// Returns sqlmodel.SQLType which is the type of the values.
func (engine *Dialect) temporalSeriesType(start, stop sqlmodel.SQLType) sqlmodel.SQLType {
	plainTimestamp := func(bound sqlmodel.SQLType) bool {
		return bound.Category == sqlmodel.TypeCategoryUnknown || bound.Temporal == sqlmodel.TemporalKindTimestamp && !bound.TimeZone
	}
	if plainTimestamp(start) && plainTimestamp(stop) {
		return engine.NormaliseTypeName("timestamp")
	}
	return engine.NormaliseTypeName("timestamptz")
}

// numericSeriesType returns the type of the values a series between numbers gives: the
// type its bounds meet in, a bound of unknown type taking the other's.
//
// Takes start (sqlmodel.SQLType) which is the first bound's type.
// Takes stop (sqlmodel.SQLType) which is the second bound's type.
//
// Returns sqlmodel.SQLType which is the type of the values.
// Returns bool which is false when no bound is a number.
func (*Dialect) numericSeriesType(start, stop sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	switch {
	case start.Category == sqlmodel.TypeCategoryUnknown && isNumber(stop):
		return stop, true
	case stop.Category == sqlmodel.TypeCategoryUnknown && isNumber(start):
		return start, true
	case isNumber(start) && isNumber(stop):
		return meetingType(start, stop)
	default:
		return sqlmodel.SQLType{}, false
	}
}

// rowColumns returns the columns of a row type an argument names: the fields of a
// composite type, or the columns of a table, each of which may be NULL, as a row built
// from a JSON document that lacks a key holds NULL there.
//
// Takes catalogue (*sqlmodel.Catalogue) which defines the composite types and tables; nil
// defines none.
// Takes rowType (sqlmodel.SQLType) which is the argument's type.
//
// Returns []ast.ScopedColumn which holds the columns, or nil when the type names no row
// type the catalogue defines.
func rowColumns(catalogue *sqlmodel.Catalogue, rowType sqlmodel.SQLType) []ast.ScopedColumn {
	if catalogue == nil {
		return nil
	}
	resolved := catalogue.ResolveNamedType(rowType)
	if resolved.Category == sqlmodel.TypeCategoryComposite {
		if schema := catalogue.LookupSchema(resolved.Schema); schema != nil {
			if columns := resolveCompositeColumns(catalogue, schema, resolved); columns != nil {
				for index := range columns {
					columns[index].Nullable = true
				}
				return columns
			}
		}
	}
	return rowTypeColumns(catalogue, rowType)
}

// selectedOverloadShape resolves columns from set-returning overloads selected by
// argument types, preferring concrete matches over any-type arguments. If a selected
// overload has no known shape, resolution falls back to the function name alone.
//
// Takes functions (*ast.FunctionCatalogue) which holds the dialect's functions; nil holds
// none.
// Takes functionName (string) which is the function's bare name.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of the call's arguments.
//
// Returns []ast.ScopedColumn which holds the columns the selected overloads agree on, or
// nil when the arguments select none.
func selectedOverloadShape(functions *ast.FunctionCatalogue, functionName string, argumentTypes []sqlmodel.SQLType) []ast.ScopedColumn {
	if functions == nil || !slices.ContainsFunc(argumentTypes, knownType) {
		return nil
	}
	signatures := functions.Functions[functionName]
	selected := acceptingOverloads(signatures, argumentTypes, true)
	if len(selected) == 0 {
		selected = acceptingOverloads(signatures, argumentTypes, false)
	}
	for _, signature := range selected {
		if resultColumns(functionName, signature) == nil {
			return nil
		}
	}
	return setReturningShape(selected, functionName)
}

// acceptingOverloads returns the set-returning overloads that take arguments of the given
// types.
//
// Takes signatures ([]*sqlmodel.Function) which are the function's overloads.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of the call's arguments.
// Takes exact (bool) which is true to keep only the overloads that declare the type of
// every argument of known type, rather than the wildcard.
//
// Returns []*sqlmodel.Function which holds the overloads, in order.
func acceptingOverloads(signatures []*sqlmodel.Function, argumentTypes []sqlmodel.SQLType, exact bool) []*sqlmodel.Function {
	var accepting []*sqlmodel.Function
	for _, signature := range signatures {
		if signature != nil && signature.ReturnsSet && overloadAccepts(signature, argumentTypes, exact) {
			accepting = append(accepting, signature)
		}
	}
	return accepting
}

// knownType reports whether a type is known.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns bool which is true unless the type is of the unknown category and names no
// type.
func knownType(sqlType sqlmodel.SQLType) bool {
	return sqlType.Category != sqlmodel.TypeCategoryUnknown || sqlType.EngineName != ""
}

// overloadAccepts reports whether a signature takes arguments of the given types: no more
// than it declares unless it is variadic, each of the declared type where both types are
// known, a declared type of no name being the wildcard, which an exact match refuses for
// an argument of known type.
//
// Takes signature (*sqlmodel.Function) which is the overload.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of the call's arguments.
// Takes exact (bool) which is true to refuse the wildcard for an argument of known type.
//
// Returns bool which is true when the overload accepts the arguments.
func overloadAccepts(signature *sqlmodel.Function, argumentTypes []sqlmodel.SQLType, exact bool) bool {
	if len(argumentTypes) > len(signature.Arguments) && !signature.IsVariadic {
		return false
	}
	for index := range argumentTypes {
		if index >= len(signature.Arguments) || !knownType(argumentTypes[index]) {
			continue
		}
		declared := signature.Arguments[index].Type
		if !knownType(declared) {
			if exact {
				return false
			}
			continue
		}
		if !strings.EqualFold(declared.EngineName, argumentTypes[index].EngineName) {
			return false
		}
	}
	return true
}
