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

package check

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestDynamicSafetyPass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		outputColumns       []sqlmodel.OutputColumn
		parameterDirectives []*directive.ParameterDirective
		expectedCount       int
	}{
		{
			name: "sortable references existing column produces no diagnostic",
			outputColumns: []sqlmodel.OutputColumn{
				{Name: "created_at"},
				{Name: "name"},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{
					Number:  1,
					Name:    "sort",
					Kind:    sqlmodel.ParameterDirectiveSortable,
					Columns: []string{"created_at", "name"},
				},
			},
			expectedCount: 0,
		},
		{
			name: "sortable references non-existent column produces Q011 diagnostic",
			outputColumns: []sqlmodel.OutputColumn{
				{Name: "id"},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{
					Number:  1,
					Name:    "sort",
					Kind:    sqlmodel.ParameterDirectiveSortable,
					Columns: []string{"missing_column"},
				},
			},
			expectedCount: 1,
		},
		{
			name: "non-sortable directive is skipped",
			outputColumns: []sqlmodel.OutputColumn{
				{Name: "id"},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{
					Number: 1,
					Name:   "email",
					Kind:   sqlmodel.ParameterDirectiveParam,
				},
			},
			expectedCount: 0,
		},
		{
			name: "sortable with case-insensitive match produces no diagnostic",
			outputColumns: []sqlmodel.OutputColumn{
				{Name: "Created_At"},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{
					Number:  1,
					Name:    "sort",
					Kind:    sqlmodel.ParameterDirectiveSortable,
					Columns: []string{"created_at"},
				},
			},
			expectedCount: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pass := &dynamicSafetyPass{}
			subject := &DiagnosticContext{
				Filename: "test.sql",
				Query: &sqlmodel.AnalysedQuery{
					Name:          "TestQuery",
					OutputColumns: testCase.outputColumns,
					Line:          1,
				},
				RawAnalysis:         &ast.RawQueryAnalysis{},
				ParameterDirectives: testCase.parameterDirectives,
			}

			diagnostics := pass.Analyse(t.Context(), subject)

			require.Len(t, diagnostics, testCase.expectedCount)

			if testCase.expectedCount > 0 {
				assert.Equal(t, "Q011", diagnostics[0].Code)
				assert.Equal(t, diag.SeverityWarning, diagnostics[0].Severity)
				assert.Contains(t, diagnostics[0].Message, "sortable")
				assert.Contains(t, diagnostics[0].Message, "not in the query output")
			}
		})
	}
}

func filterTemplate(present bool) sqlmodel.SQLTemplate {
	filter := sqlmodel.SQLSegment{Kind: sqlmodel.SegmentFilter, Present: present}
	if present {
		filter.Children = []sqlmodel.SQLSegment{{Kind: sqlmodel.SegmentText, Text: "id = 1"}}
	}
	return sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{{Kind: sqlmodel.SegmentText, Text: "SELECT 1 FROM users WHERE "}, filter}}
}

func TestBaseQueryShapeDiagnostics(t *testing.T) {
	t.Parallel()

	type expected struct {
		code     string
		severity diag.ErrorSeverity
	}

	tests := []struct {
		name         string
		query        *sqlmodel.AnalysedQuery
		want         []expected
		countWrapped bool
	}{
		{
			name: "no_where_no_terminator",
			query: &sqlmodel.AnalysedQuery{
				SQL:  "SELECT 1 FROM users",
				Line: 1,
			},
		},
		{
			name: "has_where_emits_hint",
			query: &sqlmodel.AnalysedQuery{
				SQL:      "SELECT 1 FROM users WHERE id = 1",
				Template: filterTemplate(true),
				Line:     1,
			},
			want: []expected{{diag.CodeRuntimeBuilderBaseHasWhere, diag.SeverityHint}},
		},
		{
			name: "the place of a where clause emits no hint",
			query: &sqlmodel.AnalysedQuery{
				SQL:      "SELECT 1 FROM users",
				Template: filterTemplate(false),
				Line:     1,
			},
		},
		{
			name: "a runtime query without a count template emits a warning",
			query: &sqlmodel.AnalysedQuery{
				SQL:            "SELECT 1",
				DynamicRuntime: true,
				Line:           1,
			},
			want: []expected{{diag.CodeCountRewriteUnavailable, diag.SeverityWarning}},
		},
		{
			name: "a statement terminator draws no diagnostic, since the builder writes before it",
			query: &sqlmodel.AnalysedQuery{
				SQL:  "SELECT 1 FROM users;\r\n",
				Line: 1,
			},
		},
		{
			name: "count_wrapped_emits_hint",
			query: &sqlmodel.AnalysedQuery{
				SQL:  "SELECT DISTINCT category FROM posts",
				Line: 1,
			},
			countWrapped: true,
			want:         []expected{{diag.CodeCountSemanticsWrapped, diag.SeverityHint}},
		},
		{
			name: "both hints in order",
			query: &sqlmodel.AnalysedQuery{
				SQL:      "SELECT DISTINCT category FROM posts WHERE id = 1;",
				Template: filterTemplate(true),
				Line:     1,
			},
			countWrapped: true,
			want: []expected{
				{diag.CodeRuntimeBuilderBaseHasWhere, diag.SeverityHint},
				{diag.CodeCountSemanticsWrapped, diag.SeverityHint},
			},
		},
		{
			name: "trailing_whitespace_only_does_not_emit_terminator",
			query: &sqlmodel.AnalysedQuery{
				SQL:  "SELECT 1 FROM users  \n\t",
				Line: 1,
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			subject := &DiagnosticContext{
				Filename:     "test.sql",
				Query:        testCase.query,
				CountWrapped: testCase.countWrapped,
			}
			diagnostics := baseQueryShapeDiagnostics(subject)
			require.Len(t, diagnostics, len(testCase.want))
			for index, expect := range testCase.want {
				assert.Equal(t, expect.code, diagnostics[index].Code, "diagnostic %d code", index)
				assert.Equal(t, expect.severity, diagnostics[index].Severity, "diagnostic %d severity", index)
				assert.Equal(t, "test.sql", diagnostics[index].Filename, "diagnostic %d filename", index)
				assert.Equal(t, 1, diagnostics[index].Line, "diagnostic %d line", index)
				assert.Equal(t, 1, diagnostics[index].Column, "diagnostic %d column", index)
			}
		})
	}
}
