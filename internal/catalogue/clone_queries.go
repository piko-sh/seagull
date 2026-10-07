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

package catalogue

import (
	"maps"
	"slices"

	"seagull.sh/seagull/internal/sqlmodel"
)

// CloneQueries copies analysed queries, including their types, overrides and templates.
// The result shares no mutable data with its input.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries to copy.
//
// Returns []*sqlmodel.AnalysedQuery which holds the independent copies.
func CloneQueries(queries []*sqlmodel.AnalysedQuery) []*sqlmodel.AnalysedQuery {
	return cloneEach(queries, func(query *sqlmodel.AnalysedQuery) *sqlmodel.AnalysedQuery {
		copied := *query
		copied.GroupByKey = slices.Clone(query.GroupByKey)
		copied.InsertColumns = slices.Clone(query.InsertColumns)
		copied.Template = cloneTemplate(query.Template)
		if query.CountTemplate != nil {
			copied.CountTemplate = new(cloneTemplate(*query.CountTemplate))
		}
		copied.AllowedColumns = cloneValues(query.AllowedColumns, func(column sqlmodel.AllowedColumn) sqlmodel.AllowedColumn {
			column.SQLType = cloneSQLType(column.SQLType)
			return column
		})
		copied.Parameters = cloneValues(query.Parameters, func(parameter sqlmodel.QueryParameter) sqlmodel.QueryParameter {
			parameter.DefaultLimit = cloneInteger(parameter.DefaultLimit)
			parameter.MaxLimit = cloneInteger(parameter.MaxLimit)
			parameter.GoTypeOverride = cloneGoType(parameter.GoTypeOverride)
			parameter.SQLType = cloneSQLType(parameter.SQLType)
			parameter.SortableColumns = slices.Clone(parameter.SortableColumns)
			if parameter.NonNullElements != nil {
				parameter.NonNullElements = new(*parameter.NonNullElements)
			}
			return parameter
		})
		copied.OutputColumns = cloneValues(query.OutputColumns, func(column sqlmodel.OutputColumn) sqlmodel.OutputColumn {
			column.SQLType = cloneSQLType(column.SQLType)
			column.GoTypeOverride = cloneGoType(column.GoTypeOverride)
			column.StructTags = maps.Clone(column.StructTags)
			return column
		})
		return &copied
	})
}

// cloneTemplate copies the template's bind order and nested segments.
//
// Takes template (sqlmodel.SQLTemplate) which is the template whose mutable slices must
// be copied.
//
// Returns sqlmodel.SQLTemplate which owns independent segment and argument slices.
func cloneTemplate(template sqlmodel.SQLTemplate) sqlmodel.SQLTemplate {
	template.Arguments = slices.Clone(template.Arguments)
	template.Segments = cloneValues(template.Segments, cloneSegment)
	return template
}

// cloneSegment copies nested segments and expanded star columns.
//
// Takes segment (sqlmodel.SQLSegment) which is the segment whose children must be copied.
//
// Returns sqlmodel.SQLSegment which owns independent nested segments.
func cloneSegment(segment sqlmodel.SQLSegment) sqlmodel.SQLSegment {
	segment.Children = cloneValues(segment.Children, cloneSegment)
	segment.Star = slices.Clone(segment.Star)
	return segment
}
