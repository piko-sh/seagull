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

package databasesql

import (
	"fmt"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	_ emit.CheckerPort = (*Emitter)(nil)

	_ emit.NullElementBinderPort = (*Emitter)(nil)
)

// BindsNullElements reports whether the driver behind database/sql binds a NULL element
// of an array or a list parameter, and a NULL value of a map parameter: every driver but
// DuckDB's, which panics on one.
//
// Takes info (*dialect.Facts) which names the dialect's drivers. BindsNullElements does
// not change it.
//
// Returns bool which is true when the driver binds them.
func (emitter *Emitter) BindsNullElements(info *dialect.Facts) bool {
	profile := profileFor(info.Drivers, emitter.settings.driver)
	return !profile.refusesNullElements
}

// Check reports requests unsupported by the selected database/sql driver.
//
// Checks cover preparation, command results, batch shapes, enum conversions, composite
// decoding failures, arrays of composite types the driver cannot bind, elements of a map
// or struct parameter it cannot build, and values whose type nests deeper than generated
// code follows.
//
// Takes request (*emit.Request) which is the request about to be emitted. Check does not
// change it.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func (emitter *Emitter) Check(request *emit.Request) []emit.Problem {
	profile := profileFor(request.Dialect.Drivers, emitter.settings.driver)
	problems := emitkit.CheckTypeDepth(request)
	if emitter.settings.prepared != nil && *emitter.settings.prepared && profile.preparesOnlyInserts {
		problems = append(problems, emit.Problem{
			Query: nil,
			Code:  diag.CodeEmitterUnsupported,
			Message: fmt.Sprintf("WithPrepared(true) asks for prepared.go, but the %s driver behind database/sql prepares "+
				"only INSERT statements, so it cannot prepare the package's queries; leave WithPrepared out or pass false",
				profile.module),
			Severity: diag.SeverityError,
		})
	}
	for _, query := range request.Queries {
		if problem, refused := unreportedResult(&profile, query); refused {
			problems = append(problems, problem)
		}
	}
	for _, query := range request.Queries {
		if query.Command == sqlmodel.QueryCommandBatch || query.Command == sqlmodel.QueryCommandCopyFrom {
			problems = append(problems, emitkit.CheckBatch(query, emitkit.BatchRows, emitkit.BindLimit{
				Form: request.Dialect.Placeholders.Execution, Limit: batchLimit(&request.Dialect, &profile),
			})...)
		}
	}
	for _, query := range request.Queries {
		problems = append(problems, profile.enums.Problems(request, query, sqlLists, "the "+profile.module+" driver behind database/sql")...)
	}
	for _, query := range request.Queries {
		problems = append(problems, unreadableTupleElements(&profile, query)...)
		problems = append(problems, unreadableDocumentMaps(&profile, query)...)
		problems = append(problems, unbindableCompositeArrays(&profile, request, query)...)
		problems = append(problems, unbindableNullElements(&profile, query)...)
		problems = append(problems, unbindableHeldElements(&profile, query)...)
	}
	return problems
}

// PreparesStatements reports whether the database/sql emitter can generate the
// prepared-statement cache, prepared.go, for a dialect's driver: it cannot for a driver
// that prepares only some statements, whose cache could not prepare the generated
// queries.
//
// Takes info (*dialect.Facts) which describes the dialect, whose drivers choose the
// profile unless driver names one. PreparesStatements does not change it.
// Takes driver (string) which is the module of the driver behind database/sql, as
// WithDriver takes it, or empty for the first of the dialect's drivers the emitter knows.
//
// Returns bool which is true when WithPrepared(true) works for the dialect and driver.
func PreparesStatements(info *dialect.Facts, driver string) bool {
	profile := profileFor(info.Drivers, driver)
	return !profile.preparesOnlyInserts
}

// unbindableHeldElements reports parameters containing unsupported elements inside maps
// or structs at any depth. These parameters fail at runtime because the driver uses the
// statement's element types rather than inferring them from Go values.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []emit.Problem which holds a problem for each such parameter.
func unbindableHeldElements(profile *driverProfile, query *sqlmodel.AnalysedQuery) []emit.Problem {
	if profile.buildsElement == nil {
		return nil
	}
	var problems []emit.Problem
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		if parameter.IsSlice {
			continue
		}
		element := unbuiltHeldElement(profile, &parameter.SQLType, false, 0)
		if element == nil {
			continue
		}
		problems = append(problems, emit.Problem{
			Query: query,
			Code:  diag.CodeEmitterUnsupported,
			Message: fmt.Sprintf("parameter %s holds a value of type %s inside a map or a struct, which the %s driver behind "+
				"database/sql cannot bind; build the struct in the SQL from a parameter for each field, as {'name': $1, 'kind': $2}, "+
				"or the map from a list of keys and a list of values, as map($1, $2)", parameter.Name, element.EngineName, profile.module),
			Severity: diag.SeverityError,
		})
	}
	return problems
}

// unbuiltHeldElement returns the first element a type holds inside a map or a struct, at
// any depth and through lists, that the driver cannot build.
//
// Takes profile (*driverProfile) which is the driver's profile, whose buildsElement is
// set.
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
// Takes held (bool) which reports that the type sits inside a map or a struct.
// Takes depth (int) which bounds the walk.
//
// Returns *sqlmodel.SQLType which is the element's type, or nil when there is none.
func unbuiltHeldElement(profile *driverProfile, sqlType *sqlmodel.SQLType, held bool, depth int) *sqlmodel.SQLType {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return nil
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryArray:
		return unbuiltHeldElement(profile, sqlType.ElementType, held, depth+1)
	case sqlmodel.TypeCategoryMap:
		if key := unbuiltHeldElement(profile, sqlType.KeyType, true, depth+1); key != nil {
			return key
		}
		return unbuiltHeldElement(profile, sqlType.ElementType, true, depth+1)
	case sqlmodel.TypeCategoryStruct:
		for index := range sqlType.StructFields {
			if field := unbuiltHeldElement(profile, &sqlType.StructFields[index].SQLType, true, depth+1); field != nil {
				return field
			}
		}
		return nil
	default:
		if held && !profile.buildsElement(sqlType) {
			return sqlType
		}
		return nil
	}
}

// unbindableNullElements reports the parameters of a query whose elements or map values
// are declared nullable, which a driver that cannot bind a NULL element panics on.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []emit.Problem which holds a problem for each such parameter.
func unbindableNullElements(profile *driverProfile, query *sqlmodel.AnalysedQuery) []emit.Problem {
	if !profile.refusesNullElements {
		return nil
	}
	var problems []emit.Problem
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		if parameter.IsSlice || !holdsNullableElements(&parameter.SQLType, 0) {
			continue
		}
		problems = append(problems, emit.Problem{
			Query: query,
			Code:  diag.CodeEmitterUnsupported,
			Message: fmt.Sprintf("parameter %s declares elements that may be NULL, which the %s driver behind database/sql "+
				"cannot bind; declare them non-null with non_null_elements: true, or write the NULL elements in the SQL",
				parameter.Name, profile.module),
			Severity: diag.SeverityError,
		})
	}
	return problems
}

// holdsNullableElements reports whether a type holds an element of an array or a list, or
// a value of a map, marked nullable, at any depth.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, or nil.
// Takes depth (int) which bounds the walk.
//
// Returns bool which is true when it holds one.
func holdsNullableElements(sqlType *sqlmodel.SQLType, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryArray, sqlmodel.TypeCategoryMap:
		return sqlType.ElementType != nil && (sqlType.ElementType.Nullable || holdsNullableElements(sqlType.ElementType, depth+1))
	case sqlmodel.TypeCategoryStruct:
		for index := range sqlType.StructFields {
			if holdsNullableElements(&sqlType.StructFields[index].SQLType, depth+1) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// unbindableCompositeArrays reports the parameters of a query that are arrays of a
// composite type or of a table's row type, which a driver that cannot bind them fails on
// at run time.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes request (*emit.Request) which supplies the catalogue and the type mapping.
// unbindableCompositeArrays does not change it.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []emit.Problem which holds a problem for each such parameter.
func unbindableCompositeArrays(profile *driverProfile, request *emit.Request, query *sqlmodel.AnalysedQuery) []emit.Problem {
	if !profile.refusesCompositeArrays {
		return nil
	}
	var problems []emit.Problem
	for _, parameter := range gogen.CompositeArrayParameters(request.Types, request.Catalogue, query) {
		problems = append(problems, emit.Problem{
			Query: query,
			Code:  diag.CodeEmitterUnsupported,
			Message: fmt.Sprintf("parameter %s is an array of the composite type %s, which the %s driver behind database/sql "+
				"cannot bind; pass each field as an array of its own and build the rows with unnest(...), or override the "+
				"parameter's Go type with a type whose value the driver can encode", parameter.Name, parameter.TypeName,
				profile.module),
			Severity: diag.SeverityError,
		})
	}
	return problems
}

// unreadableDocumentMaps reports Map-of-JSON columns that make the driver panic.
//
// The failure happens while constructing the row, before a scan adapter can handle it
// (clickhouse-go v2.48).
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []emit.Problem which holds a problem for each such column.
func unreadableDocumentMaps(profile *driverProfile, query *sqlmodel.AnalysedQuery) []emit.Problem {
	if !profile.panicsOnDocumentMaps {
		return nil
	}
	var problems []emit.Problem
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		if !chvalues.MapsJSON(&column.SQLType) && !chvalues.NestsJSONMap(&column.SQLType) {
			continue
		}
		problems = append(problems, emit.Problem{
			Query: query,
			Code:  diag.CodeEmitterUnsupported,
			Message: fmt.Sprintf("column %q holds a Map whose values are JSON documents, which the %s driver behind database/sql "+
				"panics reading; cast it to a Map of Nullable(JSON), or use the native ClickHouse emitter, which reads it",
				column.Name, profile.module),
			Severity: diag.SeverityError,
		})
	}
	return problems
}

// unreadableTupleElements returns the problems with a query's columns that hold a tuple
// whose elements have no names inside another tuple, which a driver reading tuple
// elements as interface values reads with the inner tuple's elements lost.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []emit.Problem which holds a problem for each such column.
func unreadableTupleElements(profile *driverProfile, query *sqlmodel.AnalysedQuery) []emit.Problem {
	if !profile.readsTupleElementsAsInterfaces {
		return nil
	}
	var problems []emit.Problem
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		if !holdsUnnamedTupleInTuple(&column.SQLType, false, 0) {
			continue
		}
		problems = append(problems, emit.Problem{
			Query: query,
			Code:  diag.CodeEmitterUnsupported,
			Message: fmt.Sprintf("column %q holds a tuple whose elements have no names inside another tuple, which the %s "+
				"driver behind database/sql reads with the inner tuple's elements lost; name the inner tuple's elements",
				column.Name, profile.module),
			Severity: diag.SeverityError,
		})
	}
	return problems
}

// holdsUnnamedTupleInTuple reports whether a type holds a tuple whose elements have no
// names inside another tuple, through arrays; a map's values start the walk afresh.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type.
// Takes insideTuple (bool) which reports that the type sits inside a tuple.
// Takes depth (int) which bounds the walk.
//
// Returns bool which is true when it holds one.
func holdsUnnamedTupleInTuple(sqlType *sqlmodel.SQLType, insideTuple bool, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryStruct:
		if insideTuple && hasUnnamedElement(sqlType) {
			return true
		}
		for index := range sqlType.StructFields {
			if holdsUnnamedTupleInTuple(&sqlType.StructFields[index].SQLType, true, depth+1) {
				return true
			}
		}
		return false
	case sqlmodel.TypeCategoryArray:
		return holdsUnnamedTupleInTuple(sqlType.ElementType, insideTuple, depth+1)
	case sqlmodel.TypeCategoryMap:
		return holdsUnnamedTupleInTuple(sqlType.ElementType, false, depth+1)
	default:
		return false
	}
}

// hasUnnamedElement reports whether a tuple has an element without a name.
//
// Takes tuple (*sqlmodel.SQLType) which is the tuple's type.
//
// Returns bool which is true when an element has no name.
func hasUnnamedElement(tuple *sqlmodel.SQLType) bool {
	for index := range tuple.StructFields {
		if tuple.StructFields[index].Positional || tuple.StructFields[index].Name == "" {
			return true
		}
	}
	return false
}

// unreportedResult returns the problem with a query whose command returns what the driver
// does not report: the number of rows a statement changed, for execrows, or the
// sql.Result holding it, for execresult.
//
// Takes profile (*driverProfile) which is the driver's profile.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns emit.Problem which describes the refusal.
// Returns bool which is true when the query is refused.
func unreportedResult(profile *driverProfile, query *sqlmodel.AnalysedQuery) (emit.Problem, bool) {
	if profile.reportsRowsAffected {
		return emit.Problem{}, false
	}
	var returns string
	switch query.Command {
	case sqlmodel.QueryCommandExecRows:
		returns = "returns the number of rows the statement changed"
	case sqlmodel.QueryCommandExecResult:
		returns = "returns the sql.Result counting the rows the statement changed"
	default:
		return emit.Problem{}, false
	}
	return emit.Problem{
		Query: query,
		Code:  diag.CodeEmitterUnsupported,
		Message: fmt.Sprintf("command: %s %s, which the %s driver behind database/sql does not report; use command: exec",
			query.Command, returns, profile.module),
		Severity: diag.SeverityError,
	}, true
}
