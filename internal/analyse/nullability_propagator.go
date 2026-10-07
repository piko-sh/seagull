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

package analyse

import (
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/sqlmodel"
)

// propagateOutputNullability applies the sgl.query nullable: override to output columns,
// once type resolution has typed them.
//
// GROUP BY leaves nullability alone: grouping on a column of the side an outer join can
// leave without a row still yields the NULL group, and a column functionally dependent on
// such a grouping key is NULL in that group too.
//
// Takes columns ([]sqlmodel.OutputColumn) which holds the output columns to adjust.
// Takes queryDirectives (*directive.QueryDirectives) which holds any nullable override
// directive.
//
// Returns []sqlmodel.OutputColumn which holds the adjusted output columns.
func propagateOutputNullability(
	columns []sqlmodel.OutputColumn,
	queryDirectives *directive.QueryDirectives,
) []sqlmodel.OutputColumn {
	result := make([]sqlmodel.OutputColumn, len(columns))
	copy(result, columns)

	if queryDirectives != nil && queryDirectives.NullableOverride != nil {
		for index := range result {
			result[index].Nullable = *queryDirectives.NullableOverride
		}
	}

	return result
}

// propagateParameterNullability applies final nullability adjustments to parameters based
// on their parameter kind declarations, once type resolution has typed them.
//
// An optional: true parameter is always nullable. A LIMIT or OFFSET parameter is always a
// NOT NULL integer. A kind: slice parameter is always NOT NULL. A sgl.param parameter
// takes the nullable: true or nullable: false override when one is supplied.
//
// Takes parameters ([]sqlmodel.QueryParameter) which holds the parameters to adjust.
// Takes parameterDirectives ([]*directive.ParameterDirective) which holds the directive
// declarations.
//
// Returns []sqlmodel.QueryParameter which holds the adjusted parameters.
func propagateParameterNullability(
	parameters []sqlmodel.QueryParameter,
	parameterDirectives []*directive.ParameterDirective,
) []sqlmodel.QueryParameter {
	result := make([]sqlmodel.QueryParameter, len(parameters))
	copy(result, parameters)

	directiveMap := make(map[int]*directive.ParameterDirective, len(parameterDirectives))
	for _, parameterDirective := range parameterDirectives {
		directiveMap[parameterDirective.Number] = parameterDirective
	}

	for index := range result {
		parameterDirective, exists := directiveMap[result[index].Number]
		if !exists {
			continue
		}

		result[index].Kind = parameterDirective.Kind
		result[index].IsOptional = parameterDirective.IsOptional
		result[index].IsSlice = parameterDirective.IsSlice
		switch {
		case parameterDirective.Kind == sqlmodel.ParameterDirectiveSortable:
			result[index].Nullable = false
			result[index].SortableColumns = parameterDirective.Columns
		case parameterDirective.IsOptional:

			result[index].Nullable = true
		case parameterDirective.IsSlice:
			result[index].Nullable = false
		case result[index].IsPaginationBound():

			result[index].Nullable = false
		default:
			if parameterDirective.Nullable != nil {
				result[index].Nullable = *parameterDirective.Nullable
			}
		}
	}

	return result
}
