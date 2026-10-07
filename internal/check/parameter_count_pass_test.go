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
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestParameterCountPass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		parameterReferences []ast.RawParameterReference
		parameterDirectives []*directive.ParameterDirective
		expectedCount       int
	}{
		{
			name: "all parameters referenced produces no diagnostics",
			parameterReferences: []ast.RawParameterReference{
				{Number: 1, Name: ""},
				{Number: 2, Name: ""},
			},
			parameterDirectives: []*directive.ParameterDirective{
				{Number: 1, Name: "email", Kind: sqlmodel.ParameterDirectiveParam},
				{Number: 2, Name: "name", Kind: sqlmodel.ParameterDirectiveParam},
			},
			expectedCount: 0,
		},
		{
			name:                "unreferenced numbered parameter produces an unreferenced-parameter error",
			parameterReferences: []ast.RawParameterReference{},
			parameterDirectives: []*directive.ParameterDirective{
				{Number: 1, Name: "email", Kind: sqlmodel.ParameterDirectiveParam},
			},
			expectedCount: 1,
		},
		{
			name:                "sortable parameters excluded from unreferenced check",
			parameterReferences: []ast.RawParameterReference{},
			parameterDirectives: []*directive.ParameterDirective{
				{Number: 1, Name: "order", Kind: sqlmodel.ParameterDirectiveSortable},
			},
			expectedCount: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pass := &parameterCountPass{}
			subject := &DiagnosticContext{
				Filename: "test.sql",
				Query: &sqlmodel.AnalysedQuery{
					Name: "TestQuery",
					Line: 5,
				},
				RawAnalysis: &ast.RawQueryAnalysis{
					ParameterReferences: testCase.parameterReferences,
				},
				ParameterDirectives: testCase.parameterDirectives,
			}

			diagnostics := pass.Analyse(t.Context(), subject)

			require.Len(t, diagnostics, testCase.expectedCount)

			if testCase.expectedCount > 0 {
				assert.Equal(t, diag.CodeUnreferencedParameter, diagnostics[0].Code)
				assert.Equal(t, diag.SeverityError, diagnostics[0].Severity)
				assert.Equal(t, "test.sql", diagnostics[0].Filename)
				assert.Equal(t, 5, diagnostics[0].Line)
				assert.Equal(t, 1, diagnostics[0].Column)
			}
		})
	}
}

func TestParameterCountPass_NamedParameter(t *testing.T) {
	t.Parallel()

	pass := &parameterCountPass{}

	t.Run("named parameter referenced by name produces no diagnostic", func(t *testing.T) {
		t.Parallel()

		subject := &DiagnosticContext{
			Filename: "test.sql",
			Query: &sqlmodel.AnalysedQuery{
				Name: "TestQuery",
				Line: 1,
			},
			RawAnalysis: &ast.RawQueryAnalysis{
				ParameterReferences: []ast.RawParameterReference{
					{Number: 0, Name: "email"},
				},
			},
			ParameterDirectives: []*directive.ParameterDirective{
				{
					Number:        0,
					Name:          "email",
					DirectiveName: "param",
					IsNamed:       true,
					Kind:          sqlmodel.ParameterDirectiveParam,
				},
			},
		}

		diagnostics := pass.Analyse(t.Context(), subject)
		assert.Empty(t, diagnostics)
	})

	t.Run("unreferenced named parameter produces warning with quoted name", func(t *testing.T) {
		t.Parallel()

		subject := &DiagnosticContext{
			Filename: "test.sql",
			Query: &sqlmodel.AnalysedQuery{
				Name: "TestQuery",
				Line: 1,
			},
			RawAnalysis: &ast.RawQueryAnalysis{
				ParameterReferences: []ast.RawParameterReference{},
			},
			ParameterDirectives: []*directive.ParameterDirective{
				{
					Number:        0,
					Name:          "email",
					DirectiveName: "param",
					IsNamed:       true,
					Kind:          sqlmodel.ParameterDirectiveParam,
				},
			},
		}

		diagnostics := pass.Analyse(t.Context(), subject)
		require.Len(t, diagnostics, 1)
		assert.Contains(t, diagnostics[0].Message, `"email"`)
		assert.Contains(t, diagnostics[0].Message, "declared but not referenced")
	})

	t.Run("named parameter not matched by a positional reference sharing its number", func(t *testing.T) {
		t.Parallel()

		subject := &DiagnosticContext{
			Filename: "test.sql",
			Query: &sqlmodel.AnalysedQuery{
				Name: "TestQuery",
				Line: 1,
			},
			RawAnalysis: &ast.RawQueryAnalysis{
				ParameterReferences: []ast.RawParameterReference{
					{Number: 0, Name: "other"},
				},
			},
			ParameterDirectives: []*directive.ParameterDirective{
				{
					Number:        0,
					Name:          "email",
					DirectiveName: "param",
					IsNamed:       true,
					Kind:          sqlmodel.ParameterDirectiveParam,
				},
			},
		}

		diagnostics := pass.Analyse(t.Context(), subject)
		require.Len(t, diagnostics, 1)
		assert.Contains(t, diagnostics[0].Message, `"email"`)
		assert.Contains(t, diagnostics[0].Message, "declared but not referenced")
	})
}

func TestParameterCountPass_RenamedNamedParameter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		anchor        string
		expectedCount int
	}{
		{name: "the anchor the statement uses", anchor: "customer", expectedCount: 0},
		{name: "an anchor the statement never uses", anchor: "custmer", expectedCount: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			subject := &DiagnosticContext{
				Filename: "test.sql",
				Query:    &sqlmodel.AnalysedQuery{Name: "TestQuery", Line: 1},
				RawAnalysis: &ast.RawQueryAnalysis{
					ParameterReferences: []ast.RawParameterReference{{Number: 1, Name: "customer"}},
				},
				ParameterDirectives: []*directive.ParameterDirective{{
					Number:        1,
					Name:          "buyer",
					Anchor:        testCase.anchor,
					DirectiveName: "param",
					IsNamed:       true,
					Kind:          sqlmodel.ParameterDirectiveParam,
				}},
			}

			diagnostics := (&parameterCountPass{}).Analyse(t.Context(), subject)

			require.Len(t, diagnostics, testCase.expectedCount)
		})
	}
}

func TestAnUnreferencedParameterIsReportedAtItsDirective(t *testing.T) {
	t.Parallel()

	directiveSpan := diagbuild.TextSpan{Line: 2, Column: 4, EndLine: 2, EndColumn: 26}
	testCases := []struct {
		name        string
		anchor      string
		number      int
		named       bool
		wantMessage string
	}{
		{name: "a named anchor spelling a number", anchor: "1", number: 0, named: true,
			wantMessage: `parameter "who" declared but not referenced in query; a placeholder numbered 1 is written ?1`},
		{name: "a named anchor", anchor: "person", number: 0, named: true,
			wantMessage: `parameter "who" declared but not referenced in query`},
		{name: "a numbered anchor", anchor: "", number: 3, named: false,
			wantMessage: `parameter 3 declared as "who" but not referenced in query`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			subject := &DiagnosticContext{
				Filename:    "people.sql",
				Query:       &sqlmodel.AnalysedQuery{Name: "ByName", Line: 1},
				RawAnalysis: &ast.RawQueryAnalysis{ParameterReferences: []ast.RawParameterReference{{Number: 1}}},
				ParameterDirectives: []*directive.ParameterDirective{{
					Number: testCase.number, Name: "who", Anchor: testCase.anchor, DirectiveName: "param", IsNamed: testCase.named,
					Kind: sqlmodel.ParameterDirectiveParam, Span: directiveSpan,
				}},
			}

			diagnostics := (&parameterCountPass{dialect: newDialectFacts(sqlite.New())}).Analyse(t.Context(), subject)

			require.Len(t, diagnostics, 1)
			require.Equal(t, testCase.wantMessage, diagnostics[0].Message)
			require.Equal(t, diag.CodeUnreferencedParameter, diagnostics[0].Code)
			require.Equal(t, directiveSpan.Line, diagnostics[0].Line)
			require.Equal(t, directiveSpan.Column, diagnostics[0].Column)
		})
	}
}
