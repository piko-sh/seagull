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
	"strconv"

	"seagull.sh/seagull/sdk/emit"
)

const (
	// directionType is the type of a sortable query's direction field.
	directionType = "OrderDirection"

	// slicePrefix starts the Go source of a slice type.
	slicePrefix = "[]"

	// closeBlock ends a type, a function or a block.
	closeBlock = "}\n"

	// fieldLine declares a field of a struct.
	fieldLine = "%s %s\n"
)

// writeQuery writes everything one query declares: its SQL constants, its Params and Row
// structs, its sortable column type, and the method or builder that runs it.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
func (current *generation) writeQuery(file *source, rendered *query) {
	naming := rendered.naming
	file.printf("const %s = %s\n\n", naming.SQLConstant, strconv.Quote(rendered.rendered.SQL))
	if rendered.count != nil {
		file.printf("const %s = %s\n\n", naming.CountConstant, strconv.Quote(rendered.count.SQL))
	}
	current.writeParams(file, rendered)
	writeSortable(file, rendered)
	current.writeRow(file, rendered)
	switch {
	case rendered.analysed.DynamicRuntime:
		current.writeBuilder(file, rendered)
	case rendered.analysed.Command == emit.QueryCommandBatch:
		current.writeBatch(file, rendered)
	default:
		current.writeMethod(file, rendered)
	}
}

// writeParams writes the struct a query's parameters are passed in, when the plan gives
// it one: a field per parameter, and the direction field after a sortable one.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
func (current *generation) writeParams(file *source, rendered *query) {
	naming := rendered.naming
	if naming.Params == "" {
		return
	}
	sortable := sortableParameter(rendered.analysed)
	file.printf("type %s struct {\n", naming.Params)
	for index := range rendered.analysed.Parameters {
		file.printf(fieldLine, naming.ParameterFields[index], current.parameterSource(file, rendered, index))
		if index == sortable {
			file.printf(fieldLine, naming.DirectionField, directionType)
		}
	}
	file.printf(closeBlock + "\n")
}

// parameterSource writes the Go type a parameter is passed as: the sortable column type
// for a sortable parameter, a slice of the element type for a slice parameter, and the
// parameter's type otherwise.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
// Takes index (int) which is the parameter's index.
//
// Returns string which is the type's Go source.
func (current *generation) parameterSource(file *source, rendered *query, index int) string {
	parameter := &rendered.analysed.Parameters[index]
	switch {
	case parameter.Kind == emit.ParameterDirectiveSortable:
		return rendered.naming.OrderBy
	case parameter.IsSlice:
		return slicePrefix + file.typeOf(parameterType(current.request, parameter))
	default:
		return file.typeOf(parameterType(current.request, parameter))
	}
}

// writeRow writes a query's row struct and its embed group structs, with grouped queries
// collecting non-key embeds into slices. It skips table models and shared row types that
// another declaration provides.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
func (current *generation) writeRow(file *source, rendered *query) {
	naming := rendered.naming
	if naming.Row == "" || naming.SharedRow {
		return
	}
	for _, embed := range naming.Embeds {
		file.printf("type %s struct {\n", embed.Type)
		current.writeColumnFields(file, rendered, func(column *emit.OutputColumn) bool {
			return column.IsEmbedded && embedGroup(column) == embed.Group
		})
		file.printf(closeBlock + "\n")
	}
	file.printf("type %s struct {\n", naming.Row)
	current.writeColumnFields(file, rendered, func(column *emit.OutputColumn) bool { return !column.IsEmbedded })
	for _, embed := range naming.Embeds {
		if detailEmbed(rendered.analysed, embed.Group) {
			file.printf("%s %s%s\n", embed.Field, slicePrefix, embed.Type)
			continue
		}
		file.printf(fieldLine, embed.Field, embed.Type)
	}
	file.printf(closeBlock + "\n")
}

// writeColumnFields writes the fields of a struct holding some of a query's output
// columns, each with the struct tags fieldTags gives it.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
// Takes holds (func(*emit.OutputColumn) bool) which selects the columns included in the
// struct.
func (current *generation) writeColumnFields(file *source, rendered *query, holds func(*emit.OutputColumn) bool) {
	columns := rendered.analysed.OutputColumns
	var held []int
	var names []string
	var tags []map[string]string
	for index := range columns {
		if holds(&columns[index]) {
			held = append(held, index)
			names = append(names, columns[index].Name)
			tags = append(tags, columns[index].StructTags)
		}
	}
	written := fieldTags(&current.request.Style, names, tags)
	for position, index := range held {
		writeField(file, rendered.naming.ColumnFields[index], file.typeOf(columnType(current.request, &columns[index])), written[position])
	}
}

// writeSortable writes a sortable query's column type and a constant per column it may be
// sorted by.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
func writeSortable(file *source, rendered *query) {
	sortable := sortableParameter(rendered.analysed)
	if sortable < 0 {
		return
	}
	naming := rendered.naming
	file.printf("type %s string\n\nconst (\n", naming.OrderBy)
	for index, column := range rendered.analysed.Parameters[sortable].SortableColumns {
		file.printf("%s %s = %s\n", naming.OrderByConstants[index], naming.OrderBy, strconv.Quote(column))
	}
	file.printf(")\n\n")
}

// writeField writes one field of a struct, with its struct tag when it has one.
//
// Takes file (*source) which is the file.
// Takes name (string) which is the field's name.
// Takes fieldType (string) which is the field's type as Go source.
// Takes tag (string) which is the field's struct tag without back quotes, or empty.
func writeField(file *source, name, fieldType, tag string) {
	if tag == "" {
		file.printf(fieldLine, name, fieldType)
		return
	}
	file.printf("%s %s `%s`\n", name, fieldType, tag)
}

// embedField returns the Row field holding an embed group.
//
// Takes rendered (*query) which is the query.
// Takes group (string) which is the embed group.
//
// Returns string which is the field.
func embedField(rendered *query, group string) string {
	for _, embed := range rendered.naming.Embeds {
		if embed.Group == group {
			return embed.Field
		}
	}
	return ""
}
