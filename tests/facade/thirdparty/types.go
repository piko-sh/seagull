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
	"strings"

	"seagull.sh/seagull/sdk/emit"
)

const (
	// pointerPrefix starts the name of a pointer type.
	pointerPrefix = "*"

	// keySeparator separates the table from the column in a group_by key.
	keySeparator = "."
)

// parameterType returns the Go type a parameter takes, or, for a slice parameter, the
// type of each element: the type a go_type override gives it, made a pointer when the
// parameter is nullable, or the type the mapping gives its SQL type.
//
// Takes request (*emit.Request) which carries the type mapping.
// Takes parameter (*emit.QueryParameter) which is the parameter.
//
// Returns emit.GoType which is the type.
func parameterType(request *emit.Request, parameter *emit.QueryParameter) emit.GoType {
	if parameter.GoTypeOverride != nil {
		return nullableOverride(*parameter.GoTypeOverride, parameter.Nullable)
	}
	return request.Types.Resolve(parameter.SQLType, parameter.Nullable)
}

// columnType returns the Go type an output column is read into: the type a go_type
// override gives it, made a pointer when the column is nullable, or the type the mapping
// gives its SQL type.
//
// Takes request (*emit.Request) which carries the type mapping.
// Takes column (*emit.OutputColumn) which is the column.
//
// Returns emit.GoType which is the type.
func columnType(request *emit.Request, column *emit.OutputColumn) emit.GoType {
	if column.GoTypeOverride != nil {
		return nullableOverride(*column.GoTypeOverride, column.Nullable)
	}
	return request.Types.Resolve(column.SQLType, column.Nullable)
}

// nullableOverride returns a go_type override for a value that may be NULL as a pointer
// to the type, unless it is one already.
//
// Takes override (emit.GoType) which is the override.
// Takes nullable (bool) which is true when the value may be NULL.
//
// Returns emit.GoType which is the type to use.
func nullableOverride(override emit.GoType, nullable bool) emit.GoType {
	if nullable && !strings.HasPrefix(override.Name, pointerPrefix) {
		override.Name = pointerPrefix + override.Name
	}
	return override
}

// hasSlice reports whether a query has a slice parameter.
//
// Takes query (*emit.AnalysedQuery) which is the query.
//
// Returns bool which is true when one parameter is a slice.
func hasSlice(query *emit.AnalysedQuery) bool {
	for index := range query.Parameters {
		if query.Parameters[index].IsSlice {
			return true
		}
	}
	return false
}

// sortableParameter returns the index of a query's sortable parameter, or -1 when it has
// none.
//
// Takes query (*emit.AnalysedQuery) which is the query.
//
// Returns int which is the index into AnalysedQuery.Parameters.
func sortableParameter(query *emit.AnalysedQuery) int {
	for index := range query.Parameters {
		if query.Parameters[index].Kind == emit.ParameterDirectiveSortable {
			return index
		}
	}
	return -1
}

// grouped reports whether a query folds its rows into groups: one with a group_by key and
// embedded columns.
//
// Takes query (*emit.AnalysedQuery) which is the query.
//
// Returns bool which is true for a grouped query.
func grouped(query *emit.AnalysedQuery) bool {
	if len(query.GroupByKey) == 0 {
		return false
	}
	for index := range query.OutputColumns {
		if query.OutputColumns[index].IsEmbedded {
			return true
		}
	}
	return false
}

// groupKey splits a query's group_by key into its table, which is empty when the key
// names none, and its column.
//
// Takes query (*emit.AnalysedQuery) which is the query.
//
// Returns table (string) which is the key's table.
// Returns column (string) which is the key's column.
func groupKey(query *emit.AnalysedQuery) (table, column string) {
	if len(query.GroupByKey) == 0 {
		return "", ""
	}
	table, column, qualified := strings.Cut(query.GroupByKey[0], keySeparator)
	if !qualified {
		return "", table
	}
	return table, column
}

// groupKeyColumn returns the index of the output column a grouped query is keyed by: the
// column named like the key's column from the key's table, or failing that the first
// column of that name, or -1 when there is none.
//
// Takes query (*emit.AnalysedQuery) which is the query.
//
// Returns int which is the index into AnalysedQuery.OutputColumns.
func groupKeyColumn(query *emit.AnalysedQuery) int {
	table, column := groupKey(query)
	fallback := -1
	for index := range query.OutputColumns {
		current := &query.OutputColumns[index]
		if !strings.EqualFold(current.Name, column) {
			continue
		}
		if table == "" || current.IsEmbedded && strings.EqualFold(current.EmbedTable, table) ||
			current.SourceTable != "" && strings.EqualFold(current.SourceTable, table) {
			return index
		}
		if fallback < 0 {
			fallback = index
		}
	}
	return fallback
}

// embedGroup returns the embed group an embedded column belongs to: its EmbedName, or its
// EmbedTable when the name is empty.
//
// Takes column (*emit.OutputColumn) which is the embedded column.
//
// Returns string which is the group.
func embedGroup(column *emit.OutputColumn) string {
	if column.EmbedName != "" {
		return column.EmbedName
	}
	return column.EmbedTable
}

// detailEmbed reports whether a grouped query collects an embed group's rows into a
// slice: an embed of a table other than the key's.
//
// Takes query (*emit.AnalysedQuery) which is the query.
// Takes group (string) which is the embed group.
//
// Returns bool which is true when the group's rows are collected.
func detailEmbed(query *emit.AnalysedQuery, group string) bool {
	table, _ := groupKey(query)
	if !grouped(query) || table == "" {
		return false
	}
	for index := range query.OutputColumns {
		column := &query.OutputColumns[index]
		if column.IsEmbedded && embedGroup(column) == group {
			return !strings.EqualFold(column.EmbedTable, table)
		}
	}
	return false
}
