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

package pgx

import (
	"fmt"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// pgxDriverName names pgx in the problems it has with a request.
	pgxDriverName = "pgx"
)

var (
	_ emit.CheckerPort = (*Emitter)(nil)
)

// Check reports request shapes that pgx cannot read, bind or copy.
//
// CopyFrom requires one INSERT row of lone placeholders with no clause after it, and
// excludes enum arrays and types pgx writes in binary only once registered. Statement
// batches can queue any statement. A parameter that is an array of a composite type is
// refused, since pgx encodes it only once the connection registers the type, and so is a
// value whose type nests deeper than generated code follows.
//
// Takes request (*emit.Request) which is the request about to be emitted. Check does not
// change it.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func (*Emitter) Check(request *emit.Request) []emit.Problem {
	problems := emitkit.CheckTypeDepth(request)
	for _, query := range request.Queries {
		if query.Command == sqlmodel.QueryCommandCopyFrom {
			problems = append(problems, copyFromProblems(request, query)...)
			continue
		}
		problems = append(problems, pgxEnums.Problems(request, query, pgxLists, pgxDriverName)...)
		problems = append(problems, compositeArrayProblems(request, query)...)
	}
	return problems
}

// compositeArrayProblems reports arrays of composite or table row types whose Go types
// have no override. pgx can encode them only after the connection registers their types,
// which generated code cannot assume.
//
// Takes request (*emit.Request) which supplies the catalogue the elements' type names
// resolve in and the type mapping, which says which Go types an override chose.
// compositeArrayProblems does not change it.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func compositeArrayProblems(request *emit.Request, query *sqlmodel.AnalysedQuery) []emit.Problem {
	parameters := gogen.CompositeArrayParameters(request.Types, request.Catalogue, query)
	problems := make([]emit.Problem, 0, len(parameters))
	for _, parameter := range parameters {
		problems = append(problems, emit.Problem{
			Query: query,
			Code:  diag.CodeEmitterUnsupported,
			Message: fmt.Sprintf("parameter %s is an array of the composite type %s, which pgx encodes only on a connection "+
				"that has loaded and registered the type (LoadType and RegisterType), and the generated code cannot rely on "+
				"that; pass each field as an array of its own and build the rows with unnest(...), or override the "+
				"parameter's Go type with a type whose value pgx can encode", parameter.Name, parameter.TypeName),
			Severity: diag.SeverityError,
		})
	}
	return problems
}

// copyFromProblems reports copyfrom queries with unsupported INSERT shapes, trailing
// clauses, enum arrays or columns needing registered binary codecs. The copy protocol
// cannot apply clauses after VALUES.
//
// Takes request (*emit.Request) which holds the dialect's lexical rules. copyFromProblems
// does not change it.
// Takes query (*sqlmodel.AnalysedQuery) which is the copyfrom query.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func copyFromProblems(request *emit.Request, query *sqlmodel.AnalysedQuery) []emit.Problem {
	if shape := emitkit.CheckBatch(query, emitkit.BatchColumns); len(shape) > 0 {
		return shape
	}
	if emitkit.ClausesAfterValues(query, &request.Dialect.Lexical) {
		return []emit.Problem{{
			Query: query,
			Code:  diag.CodeBatchShape,
			Message: "command: copyfrom sends the rows through the copy protocol, which applies nothing the statement " +
				"writes after its VALUES row, such as ON CONFLICT or RETURNING, so the clause would be dropped; use " +
				"command: batch, which runs the statement for each row, or leave the clause out",
			Severity: diag.SeverityError,
		}}
	}
	columns, _ := emitkit.RowColumns(query)
	if len(query.InsertColumns) != len(columns) || query.InsertTarget.Name == "" {
		return []emit.Problem{{
			Query: query,
			Code:  diag.CodeBatchShape,
			Message: fmt.Sprintf("command: copyfrom sends each row to the columns the INSERT names, so it needs an INSERT "+
				"into a named table that lists its columns, one for each of the %d items of its VALUES row", len(columns)),
			Severity: diag.SeverityError,
		}}
	}
	var problems []emit.Problem
	for position, parameter := range columns {
		if problem, refused := unencodedCopyColumn(request, query, query.InsertColumns[position], &query.Parameters[parameter]); refused {
			problems = append(problems, problem)
			continue
		}
		if !enumArray(query.Parameters[parameter].SQLType) {
			continue
		}
		problems = append(problems, emit.Problem{
			Query: query,
			Code:  diag.CodeEmitterUnsupported,
			Message: fmt.Sprintf("command: copyfrom cannot send column %s, an array of an enum: pgx's CopyFrom sends every "+
				"value in binary, which pgx writes for an enum array only on a connection that has loaded the enum's array "+
				"type (LoadType and RegisterType for the enum and its array type), and the generated code cannot rely on "+
				"that; use command: batch, which binds the array as text the server parses, or insert the column with "+
				"another query", query.InsertColumns[position]),
			Severity: diag.SeverityError,
		})
	}
	return problems
}

// unencodedCopyColumn reports copyfrom columns whose types need registration for pgx
// binary encoding, including unknown named types, composites and arrays of either. It
// leaves overridden Go types and unknown tables to the caller, who may supply codecs.
//
// Takes request (*emit.Request) which holds the type mapping, which says which Go types
// an override chose. unencodedCopyColumn does not change it.
// Takes query (*sqlmodel.AnalysedQuery) which is the copyfrom query.
// Takes column (string) which names the column.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter sent to it.
//
// Returns emit.Problem which describes the refusal.
// Returns bool which is true when the column is refused.
func unencodedCopyColumn(request *emit.Request, query *sqlmodel.AnalysedQuery, column string, parameter *sqlmodel.QueryParameter) (emit.Problem, bool) {
	if gogen.OverrideChosen(request.Types, parameter.GoTypeOverride, parameter.SQLType) {
		return emit.Problem{}, false
	}
	element := parameter.SQLType
	for depth := 0; depth < sqlmodel.MaxTypeDepth && element.Category == sqlmodel.TypeCategoryArray && element.ElementType != nil; depth++ {
		element = *element.ElementType
	}
	namedUnknown := element.Category == sqlmodel.TypeCategoryUnknown && element.EngineName != ""
	if !namedUnknown && element.Category != sqlmodel.TypeCategoryStruct {
		return emit.Problem{}, false
	}
	return emit.Problem{
		Query: query,
		Code:  diag.CodeEmitterUnsupported,
		Message: fmt.Sprintf("command: copyfrom cannot send column %s, whose type pgx has no binary encoding for unless the "+
			"connection registers it (an extension's type such as ltree, or a composite type): pgx's CopyFrom sends every "+
			"value in binary, so the value would reach the server as text it rejects; use command: batch, which binds "+
			"the value as text the server parses, or insert the column with another query", column),
		Severity: diag.SeverityError,
	}, true
}

// enumArray reports whether a SQL type is an array whose elements, through any number of
// dimensions, are of an enum type.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns bool which is true for an array of an enum.
func enumArray(sqlType sqlmodel.SQLType) bool {
	for depth := 0; depth < sqlmodel.MaxTypeDepth && sqlType.Category == sqlmodel.TypeCategoryArray && sqlType.ElementType != nil; depth++ {
		sqlType = *sqlType.ElementType
		if sqlType.Category == sqlmodel.TypeCategoryEnum {
			return true
		}
	}
	return false
}
