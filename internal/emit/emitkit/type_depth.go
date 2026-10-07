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

package emitkit

import (
	"fmt"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// CheckTypeDepth reports every parameter and output column of a request's queries, and
// every column of a table it models, whose SQL type nests element, key, field or member
// types to sqlmodel.MaxTypeDepth or deeper.
//
// Every walk an emitter makes through a type stops at that depth, so code generated for
// such a type could not read or bind its innermost parts; each is reported as a
// diag.CodeEmitterUnsupported error, at its query, or against the request for a table's
// column.
//
// Takes request (*emit.Request) which holds the queries and the catalogue. CheckTypeDepth
// does not change it.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func CheckTypeDepth(request *emit.Request) []emit.Problem {
	tables := catalogueTables(request.Catalogue)
	found := make([][]emit.Problem, 0, len(tables)+len(request.Queries))
	for _, table := range tables {
		found = append(found, tableDepthProblems(table))
	}
	for _, query := range request.Queries {
		found = append(found, queryDepthProblems(query))
	}
	return slices.Concat(found...)
}

// tableDepthProblems reports every column of a table whose type nests too deeply.
//
// Takes table (*sqlmodel.Table) which is the table.
//
// Returns []emit.Problem which holds a problem for each such column, against the request.
func tableDepthProblems(table *sqlmodel.Table) []emit.Problem {
	var problems []emit.Problem
	for index := range table.Columns {
		column := &table.Columns[index]
		if sqlmodel.TypeTooDeep(new(column.ValueType())) {
			problems = append(problems, typeDepthProblem(nil, fmt.Sprintf("column %s of table %s", column.Name, qualifiedTable(table))))
		}
	}
	return problems
}

// queryDepthProblems reports every parameter and output column of a query whose type
// nests too deeply.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []emit.Problem which holds a problem for each such value, at the query.
func queryDepthProblems(query *sqlmodel.AnalysedQuery) []emit.Problem {
	var problems []emit.Problem
	for index := range query.Parameters {
		if sqlmodel.TypeTooDeep(&query.Parameters[index].SQLType) {
			problems = append(problems, typeDepthProblem(query, "parameter "+parameterName(query, index)))
		}
	}
	for index := range query.OutputColumns {
		if sqlmodel.TypeTooDeep(&query.OutputColumns[index].SQLType) {
			problems = append(problems, typeDepthProblem(query, "column "+query.OutputColumns[index].Name))
		}
	}
	return problems
}

// typeDepthProblem returns the error of a value whose type nests too deeply.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query holding the value, or nil for
// a table's column.
// Takes subject (string) which names the value, such as parameter id.
//
// Returns emit.Problem which is the problem.
func typeDepthProblem(query *sqlmodel.AnalysedQuery, subject string) emit.Problem {
	return emit.Problem{
		Query: query,
		Code:  diag.CodeEmitterUnsupported,
		Message: fmt.Sprintf("%s has a type nesting %d or more levels of elements, keys, fields or members, deeper than "+
			"generated code follows; flatten the type", subject, sqlmodel.MaxTypeDepth),
		Severity: diag.SeverityError,
	}
}

// qualifiedTable returns a table's name after its schema's, when it has one.
//
// Takes table (*sqlmodel.Table) which is the table.
//
// Returns string which is the name, such as audit.events.
func qualifiedTable(table *sqlmodel.Table) string {
	if table.Schema == "" {
		return table.Name
	}
	return table.Schema + "." + table.Name
}
