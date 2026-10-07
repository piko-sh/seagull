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
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func TestGeneratedColumnPass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		catalogue           *sqlmodel.Catalogue
		parameterReferences []ast.RawParameterReference
		expectedCount       int
	}{
		{
			name: "assignment to generated column produces Q013",
			catalogue: func() *sqlmodel.Catalogue {
				catalogue := fakedialect.NewCatalogue("main")
				catalogue.Schemas["main"].Tables["users"] = &sqlmodel.Table{
					Name: "users",
					Columns: []sqlmodel.Column{
						{Name: "full_name", IsGenerated: true},
					},
				}
				return catalogue
			}(),
			parameterReferences: []ast.RawParameterReference{
				{
					Number:  1,
					Name:    "full_name",
					Context: sqlmodel.ParameterContextAssignment,
					ColumnReference: &ast.ColumnReference{
						TableAlias: "users",
						ColumnName: "full_name",
					},
				},
			},
			expectedCount: 1,
		},
		{
			name: "assignment to non-generated column produces no diagnostic",
			catalogue: func() *sqlmodel.Catalogue {
				catalogue := fakedialect.NewCatalogue("main")
				catalogue.Schemas["main"].Tables["users"] = &sqlmodel.Table{
					Name: "users",
					Columns: []sqlmodel.Column{
						{Name: "email", IsGenerated: false},
					},
				}
				return catalogue
			}(),
			parameterReferences: []ast.RawParameterReference{
				{
					Number:  1,
					Name:    "email",
					Context: sqlmodel.ParameterContextAssignment,
					ColumnReference: &ast.ColumnReference{
						TableAlias: "users",
						ColumnName: "email",
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "non-assignment context is skipped",
			catalogue: func() *sqlmodel.Catalogue {
				catalogue := fakedialect.NewCatalogue("main")
				catalogue.Schemas["main"].Tables["users"] = &sqlmodel.Table{
					Name: "users",
					Columns: []sqlmodel.Column{
						{Name: "full_name", IsGenerated: true},
					},
				}
				return catalogue
			}(),
			parameterReferences: []ast.RawParameterReference{
				{
					Number:  1,
					Context: sqlmodel.ParameterContextComparison,
					ColumnReference: &ast.ColumnReference{
						TableAlias: "users",
						ColumnName: "full_name",
					},
				},
			},
			expectedCount: 0,
		},
		{
			name: "assignment without column reference is skipped",
			catalogue: func() *sqlmodel.Catalogue {
				catalogue := fakedialect.NewCatalogue("main")
				return catalogue
			}(),
			parameterReferences: []ast.RawParameterReference{
				{
					Number:          1,
					Context:         sqlmodel.ParameterContextAssignment,
					ColumnReference: nil,
				},
			},
			expectedCount: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pass := &generatedColumnPass{catalogue: testCase.catalogue}
			subject := &DiagnosticContext{
				Filename: "test.sql",
				Query: &sqlmodel.AnalysedQuery{
					Name: "TestQuery",
					Line: 1,
				},
				RawAnalysis: &ast.RawQueryAnalysis{
					ParameterReferences: testCase.parameterReferences,
				},
			}

			diagnostics := pass.Analyse(t.Context(), subject)

			require.Len(t, diagnostics, testCase.expectedCount)

			if testCase.expectedCount > 0 {
				assert.Equal(t, "Q013", diagnostics[0].Code)
				assert.Equal(t, diag.SeverityWarning, diagnostics[0].Severity)
				assert.Contains(t, diagnostics[0].Message, "generated column")
			}
		})
	}
}

func twoSchemaGeneratedCatalogue() *sqlmodel.Catalogue {
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = &sqlmodel.Table{
		Name: "users", Schema: "public",
		Columns: []sqlmodel.Column{{Name: "total", IsGenerated: false}},
	}
	catalogue.Schemas["audit"] = &sqlmodel.Schema{
		Name: "audit",
		Tables: map[string]*sqlmodel.Table{
			"users": {Name: "users", Schema: "audit", Columns: []sqlmodel.Column{{Name: "total", IsGenerated: true}}},
		},
	}
	return catalogue
}

func TestGeneratedColumnPassLooksTheTableUpInItsSchema(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		reference     ast.TableReference
		expectedCount int
	}{
		{name: "an unqualified table follows the search path", reference: ast.TableReference{Name: "users", Alias: "u"}, expectedCount: 0},
		{name: "a qualified table names its schema", reference: ast.TableReference{Schema: "audit", Name: "users", Alias: "u"}, expectedCount: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := twoSchemaGeneratedCatalogue()
			scope := resolve.NewScopeChain(resolve.ScopeKindQuery, nil)
			scope.AddTable(testCase.reference, ast.JoinInner, catalogue.Schemas[catalogue.Key(schemaOf(testCase.reference))].Tables["users"])

			pass := &generatedColumnPass{catalogue: catalogue}
			diagnostics := pass.Analyse(t.Context(), &DiagnosticContext{
				Filename: "test.sql",
				Query:    &sqlmodel.AnalysedQuery{Name: "SetTotal", Line: 3},
				Scope:    scope,
				RawAnalysis: &ast.RawQueryAnalysis{ParameterReferences: []ast.RawParameterReference{{
					Number:          1,
					Context:         sqlmodel.ParameterContextAssignment,
					ColumnReference: &ast.ColumnReference{TableAlias: "u", ColumnName: "total"},
				}}},
			})

			require.Len(t, diagnostics, testCase.expectedCount)
			for _, diagnostic := range diagnostics {
				require.Equal(t, diag.CodeGeneratedColumn, diagnostic.Code)
				require.Equal(t, 3, diagnostic.Line)
			}
		})
	}
}

func schemaOf(reference ast.TableReference) string {
	if reference.Schema == "" {
		return "public"
	}
	return reference.Schema
}
