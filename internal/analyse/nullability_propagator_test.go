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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestPropagateOutputNullability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		columns          []sqlmodel.OutputColumn
		queryDirectives  *directive.QueryDirectives
		expectedNullable []bool
	}{
		{
			name: "nullable override forces all columns nullable",
			columns: []sqlmodel.OutputColumn{
				{Name: "id", Nullable: false, SourceTable: "users", SourceColumn: "id"},
				{Name: "email", Nullable: false, SourceTable: "users", SourceColumn: "email"},
			},
			queryDirectives: &directive.QueryDirectives{
				NullableOverride: new(true),
			},
			expectedNullable: []bool{true, true},
		},
		{
			name: "outer join nullability is kept",
			columns: []sqlmodel.OutputColumn{
				{Name: "id", Nullable: true, SourceTable: "users", SourceColumn: "id"},
				{Name: "bio", Nullable: true},
			},
			queryDirectives:  nil,
			expectedNullable: []bool{true, true},
		},
		{
			name: "no directives returns columns unchanged",
			columns: []sqlmodel.OutputColumn{
				{Name: "id", Nullable: false},
				{Name: "name", Nullable: true},
			},
			queryDirectives:  nil,
			expectedNullable: []bool{false, true},
		},
		{
			name: "nullable override false forces all columns not nullable",
			columns: []sqlmodel.OutputColumn{
				{Name: "id", Nullable: true},
				{Name: "bio", Nullable: true},
			},
			queryDirectives: &directive.QueryDirectives{
				NullableOverride: new(false),
			},
			expectedNullable: []bool{false, false},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := propagateOutputNullability(testCase.columns, testCase.queryDirectives)

			require.Len(t, result, len(testCase.expectedNullable))
			for index, expected := range testCase.expectedNullable {
				assert.Equal(t, expected, result[index].Nullable,
					"column %q at index %d: expected Nullable=%v, got %v",
					result[index].Name, index, expected, result[index].Nullable)
			}
		})
	}
}

func TestPropagateOutputNullability_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	original := []sqlmodel.OutputColumn{
		{Name: "id", Nullable: false},
		{Name: "name", Nullable: false},
	}

	result := propagateOutputNullability(original, &directive.QueryDirectives{NullableOverride: new(true)})

	assert.True(t, result[0].Nullable)
	assert.True(t, result[1].Nullable)
	assert.False(t, original[0].Nullable, "original slice should not be mutated")
	assert.False(t, original[1].Nullable, "original slice should not be mutated")
}

func TestPropagateParameterNullability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		parameters          []sqlmodel.QueryParameter
		parameterDirectives []*directive.ParameterDirective
		expectedNullable    []bool
		checkFunction       func(t *testing.T, result []sqlmodel.QueryParameter)
	}{
		{
			name: "optional parameter forces nullable",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "email", Nullable: false},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{Number: 1, IsOptional: true},
			},
			expectedNullable: []bool{true},
			checkFunction: func(t *testing.T, result []sqlmodel.QueryParameter) {
				assert.True(t, result[0].IsOptional,
					"optional parameter should have IsOptional set")
			},
		},
		{
			name: "slice parameter forces not nullable",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "ids", Nullable: true},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{Number: 1, IsSlice: true},
			},
			expectedNullable: []bool{false},
			checkFunction: func(t *testing.T, result []sqlmodel.QueryParameter) {
				assert.True(t, result[0].IsSlice,
					"slice parameter should have IsSlice set")
			},
		},
		{
			name: "sortable parameter forces not nullable and preserves columns",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "sort", Nullable: true},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{
					Number:  1,
					Kind:    sqlmodel.ParameterDirectiveSortable,
					Columns: []string{"created_at", "name"},
				},
			},
			expectedNullable: []bool{false},
			checkFunction: func(t *testing.T, result []sqlmodel.QueryParameter) {
				assert.Equal(t, []string{"created_at", "name"}, result[0].SortableColumns)
			},
		},
		{
			name: "limit parameter forces not nullable",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "page_size", Nullable: true, Context: sqlmodel.ParameterContextLimit},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{Number: 1, Name: "page_size"},
			},
			expectedNullable: []bool{false},
			checkFunction: func(t *testing.T, result []sqlmodel.QueryParameter) {
				assert.True(t, result[0].IsPaginationBound())
			},
		},
		{
			name: "offset parameter forces not nullable",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "page_offset", Nullable: true, Context: sqlmodel.ParameterContextOffset},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{Number: 1, Name: "page_offset"},
			},
			expectedNullable: []bool{false},
			checkFunction: func(t *testing.T, result []sqlmodel.QueryParameter) {
				assert.True(t, result[0].IsPaginationBound())
			},
		},
		{
			name: "param with explicit nullable true forces nullable",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "email", Nullable: false},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{Number: 1, Kind: sqlmodel.ParameterDirectiveParam, Nullable: new(true)},
			},
			expectedNullable: []bool{true},
			checkFunction:    nil,
		},
		{
			name: "param with explicit nullable false forces not nullable",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "email", Nullable: true},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{Number: 1, Kind: sqlmodel.ParameterDirectiveParam, Nullable: new(false)},
			},
			expectedNullable: []bool{false},
			checkFunction:    nil,
		},
		{
			name: "param with no nullable override preserves original",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "email", Nullable: true},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{Number: 1, Kind: sqlmodel.ParameterDirectiveParam, Nullable: nil},
			},
			expectedNullable: []bool{true},
			checkFunction:    nil,
		},
		{
			name: "parameter without matching directive is unchanged",
			parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "email", Nullable: true},
				{Number: 2, Name: "name", Nullable: false},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{Number: 1, IsOptional: true},
			},
			expectedNullable: []bool{true, false},
			checkFunction: func(t *testing.T, result []sqlmodel.QueryParameter) {

				assert.True(t, result[0].IsOptional)
				assert.False(t, result[1].IsOptional)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := propagateParameterNullability(
				testCase.parameters,
				testCase.parameterDirectives,
			)

			require.Len(t, result, len(testCase.expectedNullable))
			for index, expected := range testCase.expectedNullable {
				assert.Equal(t, expected, result[index].Nullable,
					"parameter %q at index %d: expected Nullable=%v, got %v",
					result[index].Name, index, expected, result[index].Nullable)
			}

			if testCase.checkFunction != nil {
				testCase.checkFunction(t, result)
			}
		})
	}
}

func TestPropagateParameterNullability_DoesNotMutateInput(t *testing.T) {
	t.Parallel()

	original := []sqlmodel.QueryParameter{
		{Number: 1, Name: "email", Nullable: false},
	}

	result := propagateParameterNullability(
		original,
		[]*directive.ParameterDirective{
			{Number: 1, IsOptional: true},
		},
	)

	assert.True(t, result[0].Nullable, "result should be nullable")
	assert.True(t, result[0].IsOptional, "result should be optional")
	assert.False(t, original[0].Nullable, "original should not be mutated")
	assert.False(t, original[0].IsOptional, "original should not be mutated")
}
