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

package thirdparty

import (
	"fmt"
	"strings"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_kit"
)

const (
	// bytesType is the slice type this emitter accepts as a single value.
	bytesType = "[]byte"
)

// Check reports requests the third-party emitter cannot implement.
//
// Takes request (*emit.Request) which is the request about to be emitted. Check does not
// change it.
//
// Returns []emit.Problem which holds the problems, or nil when there are none.
func (*Emitter) Check(request *emit.Request) []emit.Problem {
	if request.Dialect.Placeholders.BindsByName() {
		return []emit.Problem{unsupported(nil, "the dialect binds parameters by name, and this emitter binds them by position")}
	}
	var problems []emit.Problem
	for _, query := range request.Queries {
		problems = append(problems, checkCommand(&request.Dialect, query)...)
		problems = append(problems, checkValues(request, query)...)
		problems = append(problems, checkShape(request, query)...)
	}
	return problems
}

// checkCommand refuses the commands the emitter has no method for, and checks a batch's
// statement.
//
// Takes info (*dialect.Facts) which describes the dialect.
// Takes query (*emit.AnalysedQuery) which is the query.
//
// Returns []emit.Problem which holds the problems.
func checkCommand(info *dialect.Facts, query *emit.AnalysedQuery) []emit.Problem {
	switch {
	case query.Command == emit.QueryCommandCopyFrom || query.Command == emit.QueryCommandAsyncExec:
		return []emit.Problem{unsupported(query, fmt.Sprintf("command: %s has no method in this emitter; use command: batch or exec", query.Command))}
	case query.Command == emit.QueryCommandBatch && (len(query.OutputColumns) > 0 || len(query.Parameters) == 0):
		return []emit.Problem{unsupported(query, "command: batch with RETURNING, or with no parameters, has no method in this emitter")}
	case query.Command == emit.QueryCommandBatch:
		return emit_kit.CheckBatch(query, emit_kit.BatchRows)
	case query.DynamicRuntime && emit_kit.ExecutionForm(info, query).Kind != dialect.PlaceholderNumbered:
		return []emit.Problem{unsupported(query, "dynamic: runtime numbers the placeholders it appends, which the dialect does not")}
	default:
		return nil
	}
}

// checkValues refuses parameter and column types this emitter does not bind or scan,
// outer embeds, and a LIMIT parameter's default or cap, which the emitter does not apply.
//
// Takes request (*emit.Request) which carries the type mapping.
// Takes query (*emit.AnalysedQuery) which is the query.
//
// Returns []emit.Problem which holds the problems.
func checkValues(request *emit.Request, query *emit.AnalysedQuery) []emit.Problem {
	var problems []emit.Problem
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		if parameter.DefaultLimit != nil || parameter.MaxLimit != nil {
			problems = append(problems, unsupported(query, "parameter "+parameter.Name+" sets default: or max:, which this emitter does not apply"))
		}
		if parameter.Kind != emit.ParameterDirectiveSortable && !scalar(parameterType(request, parameter)) {
			problems = append(problems, unsupported(query, "parameter "+parameter.Name+" has a Go type this emitter cannot bind"))
		}
	}
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		if !scalar(columnType(request, column)) {
			problems = append(problems, unsupported(query, "column "+column.Name+" has a Go type this emitter cannot scan"))
		}
		if column.EmbedIsOuter {
			problems = append(problems, unsupported(query, "column "+column.Name+" belongs to an outer embed, which this emitter does not read"))
		}
	}
	return problems
}

// checkShape refuses the query shapes the emitter does not generate: a grouped query
// other than command: many or keyed by a nullable column, and a runtime builder with a
// slice parameter or grouped output.
//
// Takes request (*emit.Request) which carries the type mapping.
// Takes query (*emit.AnalysedQuery) which is the query.
//
// Returns []emit.Problem which holds the problems.
func checkShape(request *emit.Request, query *emit.AnalysedQuery) []emit.Problem {
	switch {
	case query.DynamicRuntime && (hasSlice(query) || grouped(query)):
		return []emit.Problem{unsupported(query, "dynamic: runtime with a slice parameter or group_by has no builder in this emitter")}
	case !grouped(query):
		return nil
	case query.Command != emit.QueryCommandMany:
		return []emit.Problem{unsupported(query, "group_by needs command: many in this emitter")}
	}
	key := groupKeyColumn(query)
	if key >= 0 && strings.HasPrefix(columnType(request, &query.OutputColumns[key]).Name, "*") {
		return []emit.Problem{unsupported(query, "group_by keys the rows by a nullable column, which this emitter cannot group by")}
	}
	return nil
}

// scalar reports whether the emitter accepts a Go type as a single value: a type with no
// composite shape, slice syntax other than []byte, or inline JSON type, which the emitter
// does not declare. Named types are left to the driver's binding and scanning.
//
// Takes goType (emit.GoType) which is the type.
//
// Returns bool which is true for a scalar type.
func scalar(goType emit.GoType) bool {
	name := strings.TrimLeft(goType.Name, "*")
	switch {
	case goType.Composite != nil:
		return false
	case goType.Package == "" && name == emit.InlineJSONTypeName:
		return false
	default:
		return !strings.HasPrefix(name, "[]") || name == bytesType
	}
}

// unsupported returns a CodeEmitterUnsupported error at a query, or at the request when
// the query is nil.
//
// Takes query (*emit.AnalysedQuery) which is the query, or nil.
// Takes message (string) which says what the emitter does not support.
//
// Returns emit.Problem which is the problem.
func unsupported(query *emit.AnalysedQuery, message string) emit.Problem {
	return emit.Problem{Query: query, Code: emit.CodeEmitterUnsupported, Message: message, Severity: emit.SeverityError}
}
