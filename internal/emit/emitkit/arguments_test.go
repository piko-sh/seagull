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

package emitkit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestArgumentOrder(t *testing.T) {
	t.Parallel()

	mixed := template(
		text("WHERE b = "), parameter(1),
		text(" AND a "), list(3, "IN ", false),
		text(" AND c = "), parameter(0),
		text(" AND d "), list(2, "NOT IN ", true),
		text(" AND b2 = "), parameter(1),
	)
	listedAndAlone := template(list(2, "IN ", false), text(" OR id = "), parameter(2), text(" AND a = "), parameter(0))
	inClauses := template(
		clause(sqlmodel.SegmentFilter, true, parameter(1)),
		clause(sqlmodel.SegmentOrderBy, true, parameter(0)),
		clause(sqlmodel.SegmentPagination, true, parameter(4)),
	)

	testCases := []struct {
		name     string
		template sqlmodel.SQLTemplate
		form     dialect.PlaceholderSigil
		expected []int
	}{
		{
			name:     "a numbered form binds single parameters in index order, then slices in index order",
			template: mixed,
			form:     postgresPlaceholders.Execution,
			expected: []int{0, 1, 2, 3},
		},
		{
			name:     "a named form binds in the same canonical order",
			template: mixed,
			form:     clickhousePlaceholders.Execution,
			expected: []int{0, 1, 2, 3},
		},
		{
			name:     "an anonymous form binds every occurrence in the order written",
			template: mixed,
			form:     mysqlPlaceholders.Execution,
			expected: []int{1, 3, 0, 2, 1},
		},
		{
			name:     "a parameter written alone and as a list counts as a slice",
			template: listedAndAlone,
			form:     postgresPlaceholders.Execution,
			expected: []int{0, 2},
		},
		{
			name:     "an anonymous form binds a parameter written alone and as a list at each occurrence",
			template: listedAndAlone,
			form:     mysqlPlaceholders.Execution,
			expected: []int{2, 2, 0},
		},
		{
			name:     "parameters inside clauses are bound",
			template: inClauses,
			form:     martianPlaceholders.Execution,
			expected: []int{0, 1, 4},
		},
		{
			name:     "a template without parameters binds nothing",
			template: template(text("SELECT 1")),
			form:     postgresPlaceholders.Execution,
		},
		{
			name:     "an anonymous template without parameters binds nothing",
			template: sqlmodel.SQLTemplate{},
			form:     mysqlPlaceholders.Execution,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, emitkit.ArgumentOrder(testCase.template, testCase.form))
		})
	}
}

func TestExecutionForm(t *testing.T) {
	t.Parallel()

	withoutPositional := clickhousePlaceholders
	withoutPositional.Positional = dialect.PlaceholderSigil{}

	testCases := []struct {
		name         string
		placeholders dialect.Placeholders
		expected     dialect.PlaceholderSigil
		runtime      bool
	}{
		{name: "a positional dialect executes its own form", placeholders: postgresPlaceholders, expected: postgresPlaceholders.Execution},
		{name: "a runtime query of a positional dialect too", placeholders: mysqlPlaceholders, runtime: true, expected: mysqlPlaceholders.Execution},
		{name: "a named dialect executes its named form", placeholders: clickhousePlaceholders, expected: clickhouseBraced},
		{name: "a runtime query of a named dialect binds by position", placeholders: clickhousePlaceholders, runtime: true, expected: dollarNumbered},
		{name: "a named dialect without a positional form keeps its own", placeholders: withoutPositional, runtime: true, expected: clickhouseBraced},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{DynamicRuntime: testCase.runtime}

			require.Equal(t, testCase.expected, emitkit.ExecutionForm(&dialect.Facts{Placeholders: testCase.placeholders}, query))
		})
	}
}
