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

package resolve

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func setupTypeResolverScope() *ScopeChain {
	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	scope.AddTable(
		ast.TableReference{Name: "users", Schema: "public"},
		ast.JoinInner,
		&sqlmodel.Table{
			Name: "users",
			Columns: []sqlmodel.Column{
				{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
				{Name: "name", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}},
				{Name: "email", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}, Nullable: true},
			},
		},
	)
	return scope
}

func setupMultiTableScope() *ScopeChain {
	scope := setupTypeResolverScope()
	scope.AddTable(
		ast.TableReference{Name: "orders", Schema: "public"},
		ast.JoinInner,
		&sqlmodel.Table{
			Name: "orders",
			Columns: []sqlmodel.Column{
				{Name: "order_id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
				{Name: "total", SQLType: sqlmodel.SQLType{EngineName: "numeric", Category: sqlmodel.TypeCategoryDecimal}},
			},
		},
	)
	return scope
}

func newTestTypeResolver() *TypeResolver {
	engine := &fakedialect.Engine{}
	catalogue := fakedialect.NewCatalogue("public")
	builtins := &ast.FunctionCatalogue{
		Functions: make(map[string][]*sqlmodel.Function),
	}
	functionResolver := NewFunctionResolver(builtins, catalogue, engine)

	return NewTypeResolver(catalogue, functionResolver, engine)
}

func newTestTypeResolverWithEngine(engine *fakedialect.Engine) *TypeResolver {
	catalogue := fakedialect.NewCatalogue("public")
	builtins := engine.BuiltinFunctions()
	functionResolver := NewFunctionResolver(builtins, catalogue, engine)

	return NewTypeResolver(catalogue, functionResolver, engine)
}

func TestTypeResolver_ResolveOutputColumns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		rawColumns      []ast.RawOutputColumn
		scope           func() *ScopeChain
		wantColumns     []sqlmodel.OutputColumn
		wantDiagnostics int
		wantModifying   bool
	}{
		{
			name: "column reference resolved with type from scope",
			rawColumns: []ast.RawOutputColumn{
				{ColumnName: "id", TableAlias: "users"},
			},
			scope: setupTypeResolverScope,
			wantColumns: []sqlmodel.OutputColumn{
				{
					Name:         "id",
					SQLType:      sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
					Nullable:     false,
					SourceTable:  "users",
					SourceColumn: "id",
				},
			},
			wantDiagnostics: 0,
		},
		{
			name: "column reference uses alias when provided",
			rawColumns: []ast.RawOutputColumn{
				{ColumnName: "id", TableAlias: "users", Name: "user_id"},
			},
			scope: setupTypeResolverScope,
			wantColumns: []sqlmodel.OutputColumn{
				{
					Name:         "user_id",
					SQLType:      sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
					Nullable:     false,
					SourceTable:  "users",
					SourceColumn: "id",
				},
			},
			wantDiagnostics: 0,
		},
		{
			name: "nullable column preserves nullability",
			rawColumns: []ast.RawOutputColumn{
				{ColumnName: "email", TableAlias: "users"},
			},
			scope: setupTypeResolverScope,
			wantColumns: []sqlmodel.OutputColumn{
				{
					Name:         "email",
					SQLType:      sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
					Nullable:     true,
					SourceTable:  "users",
					SourceColumn: "email",
				},
			},
			wantDiagnostics: 0,
		},
		{
			name: "star expansion returns all scope columns",
			rawColumns: []ast.RawOutputColumn{
				{IsStar: true},
			},
			scope: setupTypeResolverScope,
			wantColumns: []sqlmodel.OutputColumn{
				{
					Name:         "id",
					SQLType:      sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
					Nullable:     false,
					SourceTable:  "users",
					SourceColumn: "id",
				},
				{
					Name:         "name",
					SQLType:      sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
					Nullable:     false,
					SourceTable:  "users",
					SourceColumn: "name",
				},
				{
					Name:         "email",
					SQLType:      sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
					Nullable:     true,
					SourceTable:  "users",
					SourceColumn: "email",
				},
			},
			wantDiagnostics: 0,
		},
		{
			name: "qualified star expansion returns only that table",
			rawColumns: []ast.RawOutputColumn{
				{IsStar: true, TableAlias: "users"},
			},
			scope: setupMultiTableScope,
			wantColumns: []sqlmodel.OutputColumn{
				{
					Name:         "id",
					SQLType:      sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
					Nullable:     false,
					SourceTable:  "users",
					SourceColumn: "id",
				},
				{
					Name:         "name",
					SQLType:      sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
					Nullable:     false,
					SourceTable:  "users",
					SourceColumn: "name",
				},
				{
					Name:         "email",
					SQLType:      sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
					Nullable:     true,
					SourceTable:  "users",
					SourceColumn: "email",
				},
			},
			wantDiagnostics: 0,
		},
		{
			name: "expression column with cast resolves to cast type",
			rawColumns: []ast.RawOutputColumn{
				{
					Name: "total_text",
					Expression: &ast.CastExpression{
						Inner:    &ast.ColumnRefExpression{TableAlias: "users", ColumnName: "id"},
						TypeName: "text",
					},
				},
			},
			scope: setupTypeResolverScope,
			wantColumns: []sqlmodel.OutputColumn{
				{
					Name:         "total_text",
					SQLType:      sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryUnknown},
					Nullable:     false,
					SourceTable:  "users",
					SourceColumn: "id",
				},
			},
			wantDiagnostics: 0,
		},
		{
			name: "expression column without name defaults to question mark column",
			rawColumns: []ast.RawOutputColumn{
				{
					Expression: &ast.LiteralExpression{TypeName: "int4"},
				},
			},
			scope: setupTypeResolverScope,
			wantColumns: []sqlmodel.OutputColumn{
				{
					Name:     "?column?",
					SQLType:  sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryUnknown},
					Nullable: false,
				},
			},
			wantDiagnostics: 0,
		},
		{
			name: "unknown column keeps an unknown placeholder and produces a diagnostic",
			rawColumns: []ast.RawOutputColumn{
				{ColumnName: "id", TableAlias: "users"},
				{ColumnName: "nonexistent", TableAlias: "users"},
				{ColumnName: "email", TableAlias: "users"},
			},
			scope: setupTypeResolverScope,
			wantColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}, SourceTable: "users", SourceColumn: "id"},
				{Name: "nonexistent", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown}, Nullable: true},
				{Name: "email", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}, Nullable: true, SourceTable: "users", SourceColumn: "email"},
			},
			wantDiagnostics: 1,
		},
		{
			name: "aliased unknown column keeps its alias",
			rawColumns: []ast.RawOutputColumn{
				{Name: "fish", ColumnName: "fish", TableAlias: "basket"},
			},
			scope: setupTypeResolverScope,
			wantColumns: []sqlmodel.OutputColumn{
				{Name: "fish", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown}, Nullable: true},
			},
			wantDiagnostics: 1,
		},
		{
			name: "unknown table in star produces diagnostic",
			rawColumns: []ast.RawOutputColumn{
				{IsStar: true, TableAlias: "nonexistent"},
			},
			scope:           setupTypeResolverScope,
			wantColumns:     nil,
			wantDiagnostics: 1,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := newTestTypeResolver()
			scope := testCase.scope()
			ctx := t.Context()

			columns, dataModifying, diagnostics := resolver.ResolveOutputColumns(ctx, testCase.rawColumns, scope)

			assert.Equal(t, testCase.wantModifying, dataModifying, "data modifying flag mismatch")
			assert.Len(t, diagnostics, testCase.wantDiagnostics, "unexpected number of diagnostics")

			if testCase.wantColumns != nil {
				require.Len(t, columns, len(testCase.wantColumns), "unexpected number of output columns")
				for index, want := range testCase.wantColumns {
					assert.Equal(t, want.Name, columns[index].Name, "column %d name", index)
					assert.Equal(t, want.SQLType.EngineName, columns[index].SQLType.EngineName, "column %d engine name", index)
					assert.Equal(t, want.SQLType.Category, columns[index].SQLType.Category, "column %d category", index)
					assert.Equal(t, want.Nullable, columns[index].Nullable, "column %d nullable", index)
					assert.Equal(t, want.SourceTable, columns[index].SourceTable, "column %d source table", index)
					assert.Equal(t, want.SourceColumn, columns[index].SourceColumn, "column %d source column", index)
				}
			} else {
				assert.Empty(t, columns, "expected no resolved columns")
			}
		})
	}
}

func TestTypeResolver_ResolveOutputColumns_StopsOnCancelledContext(t *testing.T) {
	t.Parallel()

	resolver := newTestTypeResolver()
	scope := setupTypeResolverScope()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	rawColumns := []ast.RawOutputColumn{
		{ColumnName: "id", TableAlias: "users"},
		{ColumnName: "name", TableAlias: "users"},
	}

	columns, dataModifying, diagnostics := resolver.ResolveOutputColumns(ctx, rawColumns, scope)

	assert.Empty(t, columns, "expected no columns resolved when context is already cancelled")
	assert.False(t, dataModifying, "expected data modifying flag to remain false")
	assert.Empty(t, diagnostics, "cancellation is the caller's to report, not a diagnostic")
}

func TestTypeResolver_ResolveParameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		rawParameters   []ast.RawParameterReference
		directives      []*directive.ParameterDirective
		wantParameters  []sqlmodel.QueryParameter
		wantDiagnostics int
	}{
		{
			name: "column reference parameter infers type from scope and uses column name",
			rawParameters: []ast.RawParameterReference{
				{
					Number: 1,
					ColumnReference: &ast.ColumnReference{
						TableAlias: "users",
						ColumnName: "id",
					},
					Context: sqlmodel.ParameterContextComparison,
				},
			},
			wantParameters: []sqlmodel.QueryParameter{
				{
					Number:  1,
					Name:    "id",
					SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
				},
			},
		},
		{
			name: "cast type parameter uses cast type",
			rawParameters: []ast.RawParameterReference{
				{
					Number: 1,
					CastType: &sqlmodel.SQLType{
						EngineName: "text",
						Category:   sqlmodel.TypeCategoryText,
					},
					Context: sqlmodel.ParameterContextCast,
				},
			},
			wantParameters: []sqlmodel.QueryParameter{
				{
					Number:  1,
					Name:    "p1",
					SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
				},
			},
		},
		{
			name: "duplicate parameter references merged into single param",
			rawParameters: []ast.RawParameterReference{
				{
					Number: 1,
					ColumnReference: &ast.ColumnReference{
						TableAlias: "users",
						ColumnName: "id",
					},
					Context: sqlmodel.ParameterContextComparison,
				},
				{
					Number: 1,
					ColumnReference: &ast.ColumnReference{
						TableAlias: "users",
						ColumnName: "id",
					},
					Context: sqlmodel.ParameterContextComparison,
				},
			},
			wantParameters: []sqlmodel.QueryParameter{
				{
					Number:  1,
					Name:    "id",
					SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
				},
			},
		},
		{
			name: "type hint from directive overrides inferred type",
			rawParameters: []ast.RawParameterReference{
				{
					Number: 1,
					ColumnReference: &ast.ColumnReference{
						TableAlias: "users",
						ColumnName: "id",
					},
					Context: sqlmodel.ParameterContextComparison,
				},
			},
			directives: []*directive.ParameterDirective{
				{
					Number:   1,
					Name:     "user_id",
					Kind:     sqlmodel.ParameterDirectiveParam,
					TypeHint: new("text"),
				},
			},
			wantParameters: []sqlmodel.QueryParameter{
				{
					Number: 1,
					Name:   "user_id",

					SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryUnknown},
					Kind:    sqlmodel.ParameterDirectiveParam,
				},
			},
		},
		{
			name: "name from directive overrides default naming",
			rawParameters: []ast.RawParameterReference{
				{
					Number:  1,
					Context: sqlmodel.ParameterContextUnknown,
				},
			},
			directives: []*directive.ParameterDirective{
				{
					Number: 1,
					Name:   "user_email",
					Kind:   sqlmodel.ParameterDirectiveParam,
				},
			},
			wantParameters: []sqlmodel.QueryParameter{
				{
					Number:  1,
					Name:    "user_email",
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown},
					Kind:    sqlmodel.ParameterDirectiveParam,
				},
			},
		},
		{
			name: "limit context parameter infers integer type",
			rawParameters: []ast.RawParameterReference{
				{
					Number:  1,
					Context: sqlmodel.ParameterContextLimit,
				},
			},
			wantParameters: []sqlmodel.QueryParameter{
				{
					Number:  1,
					Name:    "limit",
					SQLType: fakedialect.DefaultCoreTypes().RowCount,
				},
			},
		},
		{
			name: "offset context parameter infers integer type",
			rawParameters: []ast.RawParameterReference{
				{
					Number:  1,
					Context: sqlmodel.ParameterContextOffset,
				},
			},
			wantParameters: []sqlmodel.QueryParameter{
				{
					Number:  1,
					Name:    "offset",
					SQLType: fakedialect.DefaultCoreTypes().RowCount,
				},
			},
		},
		{
			name: "nullable column reference makes parameter nullable and uses column name",
			rawParameters: []ast.RawParameterReference{
				{
					Number: 1,
					ColumnReference: &ast.ColumnReference{
						TableAlias: "users",
						ColumnName: "email",
					},
					Context: sqlmodel.ParameterContextComparison,
				},
			},
			wantParameters: []sqlmodel.QueryParameter{
				{
					Number:   1,
					Name:     "email",
					SQLType:  sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
					Nullable: true,
				},
			},
		},
		{
			name: "unresolvable column reference still uses column name and produces diagnostic",
			rawParameters: []ast.RawParameterReference{
				{
					Number: 1,
					ColumnReference: &ast.ColumnReference{
						TableAlias: "nonexistent",
						ColumnName: "nonexistent_column",
					},
					Context: sqlmodel.ParameterContextComparison,
				},
			},
			wantParameters: []sqlmodel.QueryParameter{
				{
					Number:  1,
					Name:    "nonexistent_column",
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown},
				},
			},
			wantDiagnostics: 1,
		},
		{

			name: "unknown alias with resolvable bare column types from the bare column",
			rawParameters: []ast.RawParameterReference{
				{
					Number: 1,
					ColumnReference: &ast.ColumnReference{
						TableAlias: "nonexistent",
						ColumnName: "id",
					},
					Context: sqlmodel.ParameterContextComparison,
				},
			},
			wantParameters: []sqlmodel.QueryParameter{
				{
					Number:  1,
					Name:    "id",
					SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
				},
			},
			wantDiagnostics: 0,
		},
		{
			name:          "a directive for a placeholder the query lacks adds no parameter",
			rawParameters: []ast.RawParameterReference{},
			directives: []*directive.ParameterDirective{
				{
					Number: 1,
					Name:   "page_size",
				},
			},
			wantParameters: nil,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := newTestTypeResolver()
			scope := setupTypeResolverScope()
			ctx := t.Context()

			directives := testCase.directives
			if directives == nil {
				directives = []*directive.ParameterDirective{}
			}

			rawAnalysis := &ast.RawQueryAnalysis{ParameterReferences: testCase.rawParameters}
			parameters, diagnostics := resolver.ResolveParameters(ctx, rawAnalysis, scope, directives)

			assert.Len(t, diagnostics, testCase.wantDiagnostics, "unexpected number of diagnostics")
			require.Len(t, parameters, len(testCase.wantParameters), "unexpected number of parameters")

			for index, want := range testCase.wantParameters {
				assert.Equal(t, want.Number, parameters[index].Number, "param %d number", index)
				assert.Equal(t, want.Name, parameters[index].Name, "param %d name", index)
				assert.Equal(t, want.SQLType.EngineName, parameters[index].SQLType.EngineName, "param %d engine name", index)
				assert.Equal(t, want.SQLType.Category, parameters[index].SQLType.Category, "param %d category", index)
				assert.Equal(t, want.Nullable, parameters[index].Nullable, "param %d nullable", index)
				assert.Equal(t, want.Kind, parameters[index].Kind, "param %d kind", index)
			}
		})
	}
}

func TestTypeResolver_ResolveParameters_LimitContextDoesNotMaskComparison(t *testing.T) {
	t.Parallel()

	resolver := newTestTypeResolver()
	scope := setupMultiTableScope()
	ctx := t.Context()

	rawAnalysis := &ast.RawQueryAnalysis{
		ParameterReferences: []ast.RawParameterReference{
			{
				Number: 1,
				ColumnReference: &ast.ColumnReference{
					TableAlias: "orders",
					ColumnName: "total",
				},
				Context: sqlmodel.ParameterContextComparison,
			},
			{
				Number:  1,
				Context: sqlmodel.ParameterContextLimit,
			},
		},
	}

	parameters, _ := resolver.ResolveParameters(ctx, rawAnalysis, scope, []*directive.ParameterDirective{})

	require.Len(t, parameters, 1)
	assert.False(t, parameters[0].IsPaginationBound(),
		"comparison context must not be overwritten by a later LIMIT reference")
	assert.Equal(t, sqlmodel.TypeCategoryDecimal, parameters[0].SQLType.Category,
		"the real comparison type must survive reuse in a LIMIT clause")
}

func TestTypeResolver_ResolveParameters_LimitContextKeptWhenComparisonUnresolved(t *testing.T) {
	t.Parallel()

	resolver := newTestTypeResolver()
	scope := setupTypeResolverScope()
	ctx := t.Context()

	rawAnalysis := &ast.RawQueryAnalysis{
		ParameterReferences: []ast.RawParameterReference{
			{Number: 1, Context: sqlmodel.ParameterContextLimit},
		},
	}

	parameters, _ := resolver.ResolveParameters(ctx, rawAnalysis, scope, []*directive.ParameterDirective{})

	require.Len(t, parameters, 1)
	assert.True(t, parameters[0].IsPaginationBound(),
		"a standalone LIMIT parameter must remain pagination-bound")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, parameters[0].SQLType.Category)
}

func newTypeResolverWithCatalogue(catalogue *sqlmodel.Catalogue) *TypeResolver {
	engine := &fakedialect.Engine{}
	builtins := &ast.FunctionCatalogue{
		Functions: make(map[string][]*sqlmodel.Function),
	}
	functionResolver := NewFunctionResolver(builtins, catalogue, engine)
	return NewTypeResolver(catalogue, functionResolver, engine)
}

func TestTypeResolver_ResolveParameters_SubqueryScope(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	intType := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}

	buildCatalogue := func() *sqlmodel.Catalogue {
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["orchestrator_tasks"] = fakedialect.NewTable("orchestrator_tasks",
			sqlmodel.Column{Name: "workflow_id", SQLType: textType},
			sqlmodel.Column{Name: "status", SQLType: textType},
		)
		catalogue.Schemas["public"].Tables["orchestrator_workflow_receipts"] = fakedialect.NewTable("orchestrator_workflow_receipts",
			sqlmodel.Column{Name: "workflow_id", SQLType: textType},
		)
		catalogue.Schemas["public"].Tables["workflows"] = fakedialect.NewTable("workflows",
			sqlmodel.Column{Name: "id", SQLType: intType},
		)
		return catalogue
	}

	workflowIDParameter := ast.RawParameterReference{
		Number:          1,
		ColumnReference: &ast.ColumnReference{ColumnName: "workflow_id"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	correlatedParameter := ast.RawParameterReference{
		Number:          1,
		ColumnReference: &ast.ColumnReference{TableAlias: "workflows", ColumnName: "id"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	tasksSubquery := func(parameter ast.RawParameterReference) *ast.RawQueryAnalysis {
		return &ast.RawQueryAnalysis{
			FromTables:          []ast.TableReference{{Name: "orchestrator_tasks", Schema: "public"}},
			ParameterReferences: []ast.RawParameterReference{parameter},
		}
	}

	tests := []struct {
		name          string
		outerScope    func() *ScopeChain
		expression    ast.Expression
		flatParameter ast.RawParameterReference
		wantName      string
		wantEngine    string
		wantCategory  sqlmodel.SQLTypeCategory
	}{
		{
			name:          "exists subquery types param from its own from scope",
			outerScope:    func() *ScopeChain { return NewRootScopeChain(ScopeKindQuery, testScopeRules) },
			expression:    &ast.ExistsExpression{InnerQuery: tasksSubquery(workflowIDParameter)},
			flatParameter: workflowIDParameter,
			wantName:      "workflow_id",
			wantEngine:    "text",
			wantCategory:  sqlmodel.TypeCategoryText,
		},
		{
			name:          "scalar subquery types param from its own from scope",
			outerScope:    func() *ScopeChain { return NewRootScopeChain(ScopeKindQuery, testScopeRules) },
			expression:    &ast.ScalarSubqueryExpression{InnerQuery: tasksSubquery(workflowIDParameter)},
			flatParameter: workflowIDParameter,
			wantName:      "workflow_id",
			wantEngine:    "text",
			wantCategory:  sqlmodel.TypeCategoryText,
		},
		{
			name: "correlated subquery types param against outer scope",
			outerScope: func() *ScopeChain {
				scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				scope.AddTable(
					ast.TableReference{Name: "workflows", Schema: "public"},
					ast.JoinInner,
					fakedialect.NewTable("workflows", sqlmodel.Column{Name: "id", SQLType: intType}),
				)
				return scope
			},
			expression:    &ast.ExistsExpression{InnerQuery: tasksSubquery(correlatedParameter)},
			flatParameter: correlatedParameter,
			wantName:      "id",
			wantEngine:    "int4",
			wantCategory:  sqlmodel.TypeCategoryInteger,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := newTypeResolverWithCatalogue(buildCatalogue())
			rawAnalysis := &ast.RawQueryAnalysis{
				OutputColumns: []ast.RawOutputColumn{
					{Name: "has_incomplete", Expression: testCase.expression},
				},
				ParameterReferences: []ast.RawParameterReference{testCase.flatParameter},
			}

			parameters, diagnostics := resolver.ResolveParameters(
				t.Context(), rawAnalysis, testCase.outerScope(), []*directive.ParameterDirective{},
			)

			assert.Empty(t, diagnostics, "no unknown-column diagnostic expected")
			require.Len(t, parameters, 1)
			assert.Equal(t, 1, parameters[0].Number)
			assert.Equal(t, testCase.wantName, parameters[0].Name)
			assert.Equal(t, testCase.wantEngine, parameters[0].SQLType.EngineName)
			assert.Equal(t, testCase.wantCategory, parameters[0].SQLType.Category)
		})
	}
}

func TestTypeResolver_ResolveParameters_DerivedTableScope(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["orchestrator_tasks"] = fakedialect.NewTable("orchestrator_tasks",
		sqlmodel.Column{Name: "workflow_id", SQLType: textType},
	)
	catalogue.Schemas["public"].Tables["orchestrator_workflow_receipts"] = fakedialect.NewTable("orchestrator_workflow_receipts",
		sqlmodel.Column{Name: "workflow_id", SQLType: textType},
	)
	resolver := newTypeResolverWithCatalogue(catalogue)

	parameter := ast.RawParameterReference{
		Number:          1,
		ColumnReference: &ast.ColumnReference{TableAlias: "ot", ColumnName: "workflow_id"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	innerQuery := &ast.RawQueryAnalysis{
		FromTables:          []ast.TableReference{{Name: "orchestrator_tasks", Schema: "public", Alias: "ot"}},
		ParameterReferences: []ast.RawParameterReference{parameter},
	}
	rawAnalysis := &ast.RawQueryAnalysis{
		RawDerivedTables:    []ast.RawDerivedTableReference{{InnerQuery: innerQuery, Alias: "d"}},
		ParameterReferences: []ast.RawParameterReference{parameter},
	}

	outerScope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	parameters, diagnostics := resolver.ResolveParameters(
		t.Context(), rawAnalysis, outerScope, []*directive.ParameterDirective{},
	)

	assert.Empty(t, diagnostics)
	require.Len(t, parameters, 1)
	assert.Equal(t, "workflow_id", parameters[0].Name)
	assert.Equal(t, sqlmodel.TypeCategoryText, parameters[0].SQLType.Category)
}

func TestTypeResolver_ResolveParameters_InsertSelectScope(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	catalogue := fakedialect.NewCatalogue("public")

	catalogue.Schemas["public"].Tables["sessions"] = fakedialect.NewTable("sessions",
		sqlmodel.Column{Name: "id", SQLType: textType},
	)
	catalogue.Schemas["public"].Tables["accounts"] = fakedialect.NewTable("accounts",
		sqlmodel.Column{Name: "account_id", SQLType: textType},
	)
	catalogue.Schemas["public"].Tables["account_audit"] = fakedialect.NewTable("account_audit",
		sqlmodel.Column{Name: "account_id", SQLType: textType},
	)
	resolver := newTypeResolverWithCatalogue(catalogue)

	parameter := ast.RawParameterReference{
		Number:          1,
		ColumnReference: &ast.ColumnReference{TableAlias: "a", ColumnName: "account_id"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	insertSelect := &ast.RawQueryAnalysis{
		FromTables:          []ast.TableReference{{Name: "accounts", Schema: "public", Alias: "a"}},
		ParameterReferences: []ast.RawParameterReference{parameter},
	}
	rawAnalysis := &ast.RawQueryAnalysis{
		InsertTable:         "sessions",
		FromTables:          []ast.TableReference{{Name: "sessions", Schema: "public"}},
		InsertSelect:        insertSelect,
		ParameterReferences: []ast.RawParameterReference{parameter},
	}

	outerScope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	outerScope.AddTable(
		ast.TableReference{Name: "sessions", Schema: "public"},
		ast.JoinInner,
		fakedialect.NewTable("sessions", sqlmodel.Column{Name: "id", SQLType: textType}),
	)

	parameters, diagnostics := resolver.ResolveParameters(
		t.Context(), rawAnalysis, outerScope, []*directive.ParameterDirective{},
	)

	assert.Empty(t, diagnostics, "no unknown-alias/ambiguous diagnostic expected")
	require.Len(t, parameters, 1)
	assert.Equal(t, "account_id", parameters[0].Name)
	assert.Equal(t, sqlmodel.TypeCategoryText, parameters[0].SQLType.Category)
}

func TestTypeResolver_ResolveParameters_CTEBodyScope(t *testing.T) {
	t.Parallel()

	intType := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["content_media_folders"] = fakedialect.NewTable("content_media_folders",
		sqlmodel.Column{Name: "id", SQLType: intType},
	)
	catalogue.Schemas["public"].Tables["content_media_folder_versions"] = fakedialect.NewTable("content_media_folder_versions",
		sqlmodel.Column{Name: "id", SQLType: intType},
		sqlmodel.Column{Name: "media_folder_id", SQLType: intType},
		sqlmodel.Column{Name: "status", SQLType: intType},
	)
	resolver := newTypeResolverWithCatalogue(catalogue)

	cteMediaFolderParameter := ast.RawParameterReference{
		Number:          2,
		ColumnReference: &ast.ColumnReference{ColumnName: "media_folder_id"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	cteIDParameter := ast.RawParameterReference{
		Number:          1,
		ColumnReference: &ast.ColumnReference{ColumnName: "id"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	mainParameter := ast.RawParameterReference{
		Number:          2,
		ColumnReference: &ast.ColumnReference{TableAlias: "m", ColumnName: "id"},
		Context:         sqlmodel.ParameterContextComparison,
	}

	rawAnalysis := &ast.RawQueryAnalysis{
		CTEDefinitions: []ast.RawCTEDefinition{
			{
				Name:                "latest",
				FromTables:          []ast.TableReference{{Name: "content_media_folder_versions", Schema: "public"}},
				ParameterReferences: []ast.RawParameterReference{cteMediaFolderParameter, cteIDParameter},
			},
		},
		ParameterReferences: []ast.RawParameterReference{cteMediaFolderParameter, cteIDParameter, mainParameter},
	}

	mainScope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	mainScope.AddTable(
		ast.TableReference{Name: "content_media_folders", Schema: "public", Alias: "m"},
		ast.JoinInner,
		fakedialect.NewTable("content_media_folders", sqlmodel.Column{Name: "id", SQLType: intType}),
	)
	mainScope.AddTable(
		ast.TableReference{Name: "content_media_folder_versions", Schema: "public", Alias: "v"},
		ast.JoinInner,
		fakedialect.NewTable("content_media_folder_versions",
			sqlmodel.Column{Name: "id", SQLType: intType},
			sqlmodel.Column{Name: "media_folder_id", SQLType: intType},
		),
	)
	mainScope.AddTable(
		ast.TableReference{Name: "latest", Schema: "public", Alias: "l"},
		ast.JoinInner,
		fakedialect.NewTable("latest", sqlmodel.Column{Name: "id", SQLType: intType}),
	)

	parameters, diagnostics := resolver.ResolveParameters(
		t.Context(), rawAnalysis, mainScope, []*directive.ParameterDirective{},
	)

	assert.Empty(t, diagnostics, "CTE-body id must resolve in the CTE scope, not raise Q002 in the outer scope")
	require.Len(t, parameters, 2)
	byNumber := make(map[int]sqlmodel.QueryParameter, len(parameters))
	for _, parameter := range parameters {
		byNumber[parameter.Number] = parameter
	}
	require.Contains(t, byNumber, 1)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, byNumber[1].SQLType.Category)
	require.Contains(t, byNumber, 2)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, byNumber[2].SQLType.Category)
}

func TestTypeResolver_ResolveParameters_CTEParameterUnresolvedDefersToFlatPass(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["versions"] = fakedialect.NewTable("versions",
		sqlmodel.Column{Name: "boundary", SQLType: textType},
	)
	resolver := newTypeResolverWithCatalogue(catalogue)

	cteReference := ast.RawParameterReference{
		Number:  1,
		Context: sqlmodel.ParameterContextComparison,
	}

	mainReference := ast.RawParameterReference{
		Number:          1,
		ColumnReference: &ast.ColumnReference{TableAlias: "v", ColumnName: "boundary"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	rawAnalysis := &ast.RawQueryAnalysis{
		CTEDefinitions: []ast.RawCTEDefinition{
			{
				Name:                "c",
				ParameterReferences: []ast.RawParameterReference{cteReference},
			},
		},
		ParameterReferences: []ast.RawParameterReference{cteReference, mainReference},
	}

	mainScope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	mainScope.AddTable(
		ast.TableReference{Name: "versions", Schema: "public", Alias: "v"},
		ast.JoinInner,
		fakedialect.NewTable("versions", sqlmodel.Column{Name: "boundary", SQLType: textType}),
	)

	parameters, diagnostics := resolver.ResolveParameters(
		t.Context(), rawAnalysis, mainScope, []*directive.ParameterDirective{},
	)

	assert.Empty(t, diagnostics)
	require.Len(t, parameters, 1)
	assert.Equal(t, sqlmodel.TypeCategoryText, parameters[0].SQLType.Category,
		"parameter must be typed from the main-query occurrence, not collapsed to unknown by the CTE capture")
}

func TestTypeResolver_ResolveParameters_PredicateSubqueryScope(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	intType := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["accounts"] = fakedialect.NewTable("accounts",
		sqlmodel.Column{Name: "id", SQLType: intType},
	)
	catalogue.Schemas["public"].Tables["account_versions"] = fakedialect.NewTable("account_versions",
		sqlmodel.Column{Name: "id", SQLType: intType},
		sqlmodel.Column{Name: "account_id", SQLType: intType},
		sqlmodel.Column{Name: "email", SQLType: textType},
	)
	resolver := newTypeResolverWithCatalogue(catalogue)

	emailParameter := ast.RawParameterReference{
		Number:          1,
		ColumnReference: &ast.ColumnReference{TableAlias: "av", ColumnName: "email"},
		Context:         sqlmodel.ParameterContextComparison,
	}

	subqueryParameter := ast.RawParameterReference{
		Number:          2,
		ColumnReference: &ast.ColumnReference{TableAlias: "av2", ColumnName: "id"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	predicateSubquery := &ast.RawQueryAnalysis{
		FromTables:          []ast.TableReference{{Name: "account_versions", Schema: "public", Alias: "av2"}},
		ParameterReferences: []ast.RawParameterReference{subqueryParameter},
	}
	rawAnalysis := &ast.RawQueryAnalysis{
		PredicateSubqueries: []*ast.RawQueryAnalysis{predicateSubquery},
		ParameterReferences: []ast.RawParameterReference{emailParameter, subqueryParameter},
	}

	outerScope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	outerScope.AddTable(
		ast.TableReference{Name: "accounts", Schema: "public", Alias: "a"},
		ast.JoinInner,
		fakedialect.NewTable("accounts", sqlmodel.Column{Name: "id", SQLType: intType}),
	)
	outerScope.AddTable(
		ast.TableReference{Name: "account_versions", Schema: "public", Alias: "av"},
		ast.JoinInner,
		fakedialect.NewTable("account_versions",
			sqlmodel.Column{Name: "id", SQLType: intType},
			sqlmodel.Column{Name: "email", SQLType: textType},
		),
	)

	parameters, diagnostics := resolver.ResolveParameters(
		t.Context(), rawAnalysis, outerScope, []*directive.ParameterDirective{},
	)

	assert.Empty(t, diagnostics, "subquery-local av2 must resolve in the subquery scope, no false Q001")
	require.Len(t, parameters, 2)
	byNumber := make(map[int]sqlmodel.QueryParameter, len(parameters))
	for _, parameter := range parameters {
		byNumber[parameter.Number] = parameter
	}
	require.Contains(t, byNumber, 2)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, byNumber[2].SQLType.Category, "?2 typed from av2.id")
	require.Contains(t, byNumber, 1)
	assert.Equal(t, sqlmodel.TypeCategoryText, byNumber[1].SQLType.Category, "?1 typed from av.email")
}

func TestTypeResolver_ResolveParameters_PredicateSubqueryUnknownAliasStillErrors(t *testing.T) {
	t.Parallel()

	intType := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["account_versions"] = fakedialect.NewTable("account_versions",
		sqlmodel.Column{Name: "id", SQLType: intType},
	)
	resolver := newTypeResolverWithCatalogue(catalogue)

	badParameter := ast.RawParameterReference{
		Number:          1,
		ColumnReference: &ast.ColumnReference{TableAlias: "bogus", ColumnName: "nonexistent"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	predicateSubquery := &ast.RawQueryAnalysis{
		FromTables:          []ast.TableReference{{Name: "account_versions", Schema: "public", Alias: "av2"}},
		ParameterReferences: []ast.RawParameterReference{badParameter},
	}
	rawAnalysis := &ast.RawQueryAnalysis{
		PredicateSubqueries: []*ast.RawQueryAnalysis{predicateSubquery},
		ParameterReferences: []ast.RawParameterReference{badParameter},
	}

	outerScope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	outerScope.AddTable(
		ast.TableReference{Name: "account_versions", Schema: "public", Alias: "av"},
		ast.JoinInner,
		fakedialect.NewTable("account_versions", sqlmodel.Column{Name: "id", SQLType: intType}),
	)

	_, diagnostics := resolver.ResolveParameters(
		t.Context(), rawAnalysis, outerScope, []*directive.ParameterDirective{},
	)

	require.NotEmpty(t, diagnostics, "an unknown alias inside a predicate subquery must still raise a diagnostic")
}

func TestTypeResolver_ResolveParameters_FunctionArgument(t *testing.T) {
	t.Parallel()

	uuidV4 := sqlmodel.SQLType{EngineName: "uuid_v4", Category: sqlmodel.TypeCategoryText}
	catalogue := fakedialect.NewCatalogue("content")
	catalogue.Schemas["content"].Functions["get_pages_with_latest_version"] = []*sqlmodel.Function{
		{
			Name:       "get_pages_with_latest_version",
			Schema:     "content",
			ReturnType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger},
			ReturnsSet: true,
			Arguments: []sqlmodel.FunctionArgument{
				{Name: "_environment_id", Type: uuidV4},
				{Name: "_published", Type: sqlmodel.SQLType{EngineName: "boolean", Category: sqlmodel.TypeCategoryBoolean}},
			},
		},
	}
	resolver := newTypeResolverWithCatalogue(catalogue)

	envParameter := ast.RawParameterReference{
		Number:                1,
		Context:               sqlmodel.ParameterContextFunctionArgument,
		EnclosingFunctionName: "content.get_pages_with_latest_version",
		ArgumentOrdinal:       0,
	}
	publishedParameter := ast.RawParameterReference{
		Number:                2,
		Context:               sqlmodel.ParameterContextFunctionArgument,
		EnclosingFunctionName: "content.get_pages_with_latest_version",
		ArgumentOrdinal:       1,
	}
	rawAnalysis := &ast.RawQueryAnalysis{
		ParameterReferences: []ast.RawParameterReference{envParameter, publishedParameter},
	}
	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)

	parameters, diagnostics := resolver.ResolveParameters(t.Context(), rawAnalysis, scope, []*directive.ParameterDirective{})

	assert.Empty(t, diagnostics)
	require.Len(t, parameters, 2)
	byNumber := make(map[int]sqlmodel.QueryParameter, len(parameters))
	for _, parameter := range parameters {
		byNumber[parameter.Number] = parameter
	}
	require.Contains(t, byNumber, 1)
	assert.Equal(t, "uuid_v4", byNumber[1].SQLType.EngineName, "arg 0 typed from _environment_id")
	require.Contains(t, byNumber, 2)
	assert.Equal(t, sqlmodel.TypeCategoryBoolean, byNumber[2].SQLType.Category, "arg 1 typed from _published")
}

func TestTypeResolver_ResolveParameters_SubqueryOverDerivedTable(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["identity_account_versions"] = fakedialect.NewTable("identity_account_versions",
		sqlmodel.Column{Name: "account_id", SQLType: textType},
		sqlmodel.Column{Name: "email", SQLType: textType},
	)
	resolver := newTypeResolverWithCatalogue(catalogue)

	innerMost := &ast.RawQueryAnalysis{
		FromTables: []ast.TableReference{{Name: "identity_account_versions", Schema: "public"}},
		OutputColumns: []ast.RawOutputColumn{
			{Name: "account_id", ColumnName: "account_id"},
			{Name: "email", ColumnName: "email"},
		},
	}
	parameter := ast.RawParameterReference{
		Number:          1,
		ColumnReference: &ast.ColumnReference{ColumnName: "email"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	predicateSubquery := &ast.RawQueryAnalysis{
		RawDerivedTables:    []ast.RawDerivedTableReference{{InnerQuery: innerMost, Alias: ""}},
		ParameterReferences: []ast.RawParameterReference{parameter},
	}
	rawAnalysis := &ast.RawQueryAnalysis{
		PredicateSubqueries: []*ast.RawQueryAnalysis{predicateSubquery},
		ParameterReferences: []ast.RawParameterReference{parameter},
	}
	outerScope := NewRootScopeChain(ScopeKindQuery, testScopeRules)

	parameters, diagnostics := resolver.ResolveParameters(t.Context(), rawAnalysis, outerScope, []*directive.ParameterDirective{})

	assert.Empty(t, diagnostics, "email must resolve against the derived table's projected columns")
	require.Len(t, parameters, 1)
	assert.Equal(t, sqlmodel.TypeCategoryText, parameters[0].SQLType.Category)
}

func TestTypeResolver_ResolveParameters_IndependentNestedNumberingNotSuppressed(t *testing.T) {
	t.Parallel()

	intType := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["orchestrator_tasks"] = fakedialect.NewTable("orchestrator_tasks",
		sqlmodel.Column{Name: "workflow_id", SQLType: textType},
	)
	resolver := newTypeResolverWithCatalogue(catalogue)

	outerParameter := ast.RawParameterReference{
		Number:          1,
		Name:            "q",
		ColumnReference: &ast.ColumnReference{TableAlias: "workflows", ColumnName: "id"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	innerParameter := ast.RawParameterReference{
		Number:          1,
		Name:            "p",
		ColumnReference: &ast.ColumnReference{TableAlias: "ot", ColumnName: "workflow_id"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	innerQuery := &ast.RawQueryAnalysis{
		FromTables:          []ast.TableReference{{Name: "orchestrator_tasks", Schema: "public", Alias: "ot"}},
		ParameterReferences: []ast.RawParameterReference{innerParameter},
	}
	rawAnalysis := &ast.RawQueryAnalysis{
		RawDerivedTables: []ast.RawDerivedTableReference{{InnerQuery: innerQuery, Alias: "d"}},

		ParameterReferences: []ast.RawParameterReference{outerParameter},
	}

	outerScope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	outerScope.AddTable(
		ast.TableReference{Name: "workflows", Schema: "public"},
		ast.JoinInner,
		fakedialect.NewTable("workflows", sqlmodel.Column{Name: "id", SQLType: intType}),
	)

	parameters, diagnostics := resolver.ResolveParameters(
		t.Context(), rawAnalysis, outerScope, []*directive.ParameterDirective{},
	)

	assert.Empty(t, diagnostics, "the outer parameter resolves; no false diagnostic")
	require.Len(t, parameters, 1)
	assert.Equal(t, 1, parameters[0].Number)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, parameters[0].SQLType.Category,
		"outer parameter must keep its own type, not be overwritten by the independent inner parameter")
}

func TestTypeResolver_ResolveParameters_SubqueryOverView(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Views["active_sessions"] = &sqlmodel.View{
		Name:    "active_sessions",
		Schema:  "public",
		Columns: []sqlmodel.Column{{Name: "session_account_id", SQLType: textType}},
	}
	resolver := newTypeResolverWithCatalogue(catalogue)

	parameter := ast.RawParameterReference{
		Number:          1,
		ColumnReference: &ast.ColumnReference{TableAlias: "s", ColumnName: "session_account_id"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	innerQuery := &ast.RawQueryAnalysis{
		FromTables:          []ast.TableReference{{Name: "active_sessions", Schema: "public", Alias: "s"}},
		ParameterReferences: []ast.RawParameterReference{parameter},
	}
	rawAnalysis := &ast.RawQueryAnalysis{
		OutputColumns: []ast.RawOutputColumn{
			{Name: "has_session", Expression: &ast.ExistsExpression{InnerQuery: innerQuery}},
		},
		ParameterReferences: []ast.RawParameterReference{parameter},
	}

	outerScope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	parameters, diagnostics := resolver.ResolveParameters(
		t.Context(), rawAnalysis, outerScope, []*directive.ParameterDirective{},
	)

	assert.Empty(t, diagnostics, "a subquery over a view must resolve, not raise Q001")
	require.Len(t, parameters, 1)
	assert.Equal(t, "session_account_id", parameters[0].Name)
	assert.Equal(t, sqlmodel.TypeCategoryText, parameters[0].SQLType.Category)
}

func TestTypeResolver_ResolveParameters_SubqueryOverTableValuedFunction(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	catalogue := fakedialect.NewCatalogue("public")

	engine := &fakedialect.Engine{
		TableValuedFunctionColumnsFunc: func(functionName string) []ast.ScopedColumn {
			if functionName != "json_each" {
				return nil
			}
			return []ast.ScopedColumn{
				{Name: "key", SQLType: textType, Nullable: true},
				{Name: "value", SQLType: textType, Nullable: true},
			}
		},
	}
	builtins := &ast.FunctionCatalogue{Functions: make(map[string][]*sqlmodel.Function)}
	resolver := NewTypeResolver(catalogue, NewFunctionResolver(builtins, catalogue, engine), engine)

	parameter := ast.RawParameterReference{
		Number:          1,
		ColumnReference: &ast.ColumnReference{TableAlias: "je", ColumnName: "value"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	existsSubquery := &ast.RawQueryAnalysis{
		RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{
			{FunctionName: "json_each", Alias: "je"},
		},
		ParameterReferences: []ast.RawParameterReference{parameter},
	}
	rawAnalysis := &ast.RawQueryAnalysis{
		PredicateSubqueries: []*ast.RawQueryAnalysis{existsSubquery},
		ParameterReferences: []ast.RawParameterReference{parameter},
	}

	outerScope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	parameters, diagnostics := resolver.ResolveParameters(
		t.Context(), rawAnalysis, outerScope, []*directive.ParameterDirective{},
	)

	assert.Empty(t, diagnostics, "je.value from a json_each table-valued function in an EXISTS subquery must resolve in the subquery scope, no false Q001")
	require.Len(t, parameters, 1)
	assert.Equal(t, sqlmodel.TypeCategoryText, parameters[0].SQLType.Category,
		"the parameter takes the json_each value column type")
}

func TestTypeResolver_ResolveParameters_SubqueryOverUnknownTableValuedFunctionStillErrors(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")

	engine := &fakedialect.Engine{
		TableValuedFunctionColumnsFunc: func(string) []ast.ScopedColumn { return nil },
	}
	builtins := &ast.FunctionCatalogue{Functions: make(map[string][]*sqlmodel.Function)}
	resolver := NewTypeResolver(catalogue, NewFunctionResolver(builtins, catalogue, engine), engine)

	parameter := ast.RawParameterReference{
		Number:          1,
		ColumnReference: &ast.ColumnReference{TableAlias: "je", ColumnName: "value"},
		Context:         sqlmodel.ParameterContextComparison,
	}
	existsSubquery := &ast.RawQueryAnalysis{
		RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{
			{FunctionName: "totally_unknown_function", Alias: "je"},
		},
		ParameterReferences: []ast.RawParameterReference{parameter},
	}
	rawAnalysis := &ast.RawQueryAnalysis{
		PredicateSubqueries: []*ast.RawQueryAnalysis{existsSubquery},
		ParameterReferences: []ast.RawParameterReference{parameter},
	}

	outerScope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	_, diagnostics := resolver.ResolveParameters(
		t.Context(), rawAnalysis, outerScope, []*directive.ParameterDirective{},
	)

	require.NotEmpty(t, diagnostics, "an unknown table-valued function must not silently resolve; je stays unresolved and a diagnostic is raised")
	assert.Equal(t, diag.CodeUnknownColumn, diagnostics[0].Code)
}

func TestCollectParameters(t *testing.T) {
	t.Parallel()

	t.Run("ordered by number ascending", func(t *testing.T) {
		t.Parallel()

		parameterTypes := map[int]*sqlmodel.QueryParameter{
			3: {Number: 3, Name: "p3"},
			1: {Number: 1, Name: "p1"},
			2: {Number: 2, Name: "p2"},
		}

		result := collectParameters(parameterTypes)

		require.Len(t, result, 3)
		assert.Equal(t, 1, result[0].Number)
		assert.Equal(t, 2, result[1].Number)
		assert.Equal(t, 3, result[2].Number)
	})

	t.Run("gaps in numbering skip missing entries", func(t *testing.T) {
		t.Parallel()

		parameterTypes := map[int]*sqlmodel.QueryParameter{
			1: {Number: 1, Name: "p1"},
			3: {Number: 3, Name: "p3"},
		}

		result := collectParameters(parameterTypes)

		require.Len(t, result, 2)
		assert.Equal(t, 1, result[0].Number)
		assert.Equal(t, "p1", result[0].Name)
		assert.Equal(t, 3, result[1].Number)
		assert.Equal(t, "p3", result[1].Name)
	})

	t.Run("empty map returns empty slice", func(t *testing.T) {
		t.Parallel()

		result := collectParameters(map[int]*sqlmodel.QueryParameter{})

		assert.Empty(t, result)
	})

	t.Run("single parameter", func(t *testing.T) {
		t.Parallel()

		parameterTypes := map[int]*sqlmodel.QueryParameter{
			1: {Number: 1, Name: "only"},
		}

		result := collectParameters(parameterTypes)

		require.Len(t, result, 1)
		assert.Equal(t, "only", result[0].Name)
	})
}

func TestResolveParameterName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		raw                ast.RawParameterReference
		directiveNumberMap map[int]*directive.ParameterDirective
		directiveNameMap   map[string]*directive.ParameterDirective
		want               string
	}{
		{
			name: "raw name used when no directive matches",
			raw: ast.RawParameterReference{
				Number: 1,
				Name:   "user_email",
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{},
			directiveNameMap:   map[string]*directive.ParameterDirective{},
			want:               "user_email",
		},
		{
			name: "directive name map overrides raw name",
			raw: ast.RawParameterReference{
				Number: 1,
				Name:   "email",
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{},
			directiveNameMap: map[string]*directive.ParameterDirective{
				"email": {Number: 1, Name: "user_email", DirectiveName: "email"},
			},
			want: "user_email",
		},
		{
			name: "directive number map provides name when raw has none",
			raw: ast.RawParameterReference{
				Number: 1,
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{
				1: {Number: 1, Name: "page_size"},
			},
			directiveNameMap: map[string]*directive.ParameterDirective{},
			want:             "page_size",
		},
		{
			name: "default p{N} fallback when no name or directive",
			raw: ast.RawParameterReference{
				Number: 5,
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{},
			directiveNameMap:   map[string]*directive.ParameterDirective{},
			want:               "p5",
		},
		{
			name: "raw name takes precedence over number directive",
			raw: ast.RawParameterReference{
				Number: 1,
				Name:   "from_sql",
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{
				1: {Number: 1, Name: "from_directive"},
			},
			directiveNameMap: map[string]*directive.ParameterDirective{},
			want:             "from_sql",
		},
		{
			name: "column reference name used when no explicit name or directive",
			raw: ast.RawParameterReference{
				Number: 1,
				ColumnReference: &ast.ColumnReference{
					ColumnName: "user_id",
				},
				Context: sqlmodel.ParameterContextComparison,
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{},
			directiveNameMap:   map[string]*directive.ParameterDirective{},
			want:               "user_id",
		},
		{
			name: "directive number overrides column reference name",
			raw: ast.RawParameterReference{
				Number: 1,
				ColumnReference: &ast.ColumnReference{
					ColumnName: "user_id",
				},
				Context: sqlmodel.ParameterContextComparison,
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{
				1: {Number: 1, Name: "actor"},
			},
			directiveNameMap: map[string]*directive.ParameterDirective{},
			want:             "actor",
		},
		{
			name: "limit context defaults to limit",
			raw: ast.RawParameterReference{
				Number:  1,
				Context: sqlmodel.ParameterContextLimit,
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{},
			directiveNameMap:   map[string]*directive.ParameterDirective{},
			want:               "limit",
		},
		{
			name: "offset context defaults to offset",
			raw: ast.RawParameterReference{
				Number:  1,
				Context: sqlmodel.ParameterContextOffset,
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{},
			directiveNameMap:   map[string]*directive.ParameterDirective{},
			want:               "offset",
		},
		{
			name: "function argument context falls through to pN when no column reference",
			raw: ast.RawParameterReference{
				Number:  3,
				Context: sqlmodel.ParameterContextFunctionArgument,
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{},
			directiveNameMap:   map[string]*directive.ParameterDirective{},
			want:               "p3",
		},
		{
			name: "function argument takes the signature's argument name",
			raw: ast.RawParameterReference{
				Number:                1,
				Context:               sqlmodel.ParameterContextFunctionArgument,
				EnclosingFunctionName: "scale_price",
				ArgumentOrdinal:       1,
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{},
			directiveNameMap:   map[string]*directive.ParameterDirective{},
			want:               "factor",
		},
		{
			name: "a schema-qualified call takes its schema's argument name",
			raw: ast.RawParameterReference{
				Number:                1,
				Context:               sqlmodel.ParameterContextFunctionArgument,
				EnclosingFunctionName: "public.scale_price",
				ArgumentOrdinal:       0,
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{},
			directiveNameMap:   map[string]*directive.ParameterDirective{},
			want:               "amount",
		},
		{
			name: "overloads that disagree on the name fall back to pN",
			raw: ast.RawParameterReference{
				Number:                2,
				Context:               sqlmodel.ParameterContextFunctionArgument,
				EnclosingFunctionName: "describe",
				ArgumentOrdinal:       0,
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{},
			directiveNameMap:   map[string]*directive.ParameterDirective{},
			want:               "p2",
		},
		{
			name: "an unnamed signature argument falls back to pN",
			raw: ast.RawParameterReference{
				Number:                4,
				Context:               sqlmodel.ParameterContextFunctionArgument,
				EnclosingFunctionName: "p1",
				ArgumentOrdinal:       0,
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{},
			directiveNameMap:   map[string]*directive.ParameterDirective{},
			want:               "p4",
		},
		{
			name: "a directive outranks the signature's argument name",
			raw: ast.RawParameterReference{
				Number:                1,
				Context:               sqlmodel.ParameterContextFunctionArgument,
				EnclosingFunctionName: "scale_price",
				ArgumentOrdinal:       1,
			},
			directiveNumberMap: map[int]*directive.ParameterDirective{1: {Number: 1, Name: "multiplier"}},
			directiveNameMap:   map[string]*directive.ParameterDirective{},
			want:               "multiplier",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := signatureNamingResolver().parameterName(testCase.raw, testCase.directiveNumberMap, testCase.directiveNameMap)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func signatureNamingResolver() *TypeResolver {
	catalogue := fakedialect.NewCatalogue("public")
	integer := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
	text := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	scale := sqlmodel.NewFunction([]sqlmodel.FunctionArgument{
		{Name: "amount", Type: integer},
		{Name: "factor", Type: integer},
	}, integer, sqlmodel.FunctionNullableReturnsNullOnNull)
	scale.Name = "scale_price"
	scale.Schema = "public"
	describeInteger := sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "value", Type: integer}}, text, sqlmodel.FunctionNullableReturnsNullOnNull)
	describeInteger.Name = "describe"
	describeText := sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "label", Type: text}}, text, sqlmodel.FunctionNullableReturnsNullOnNull)
	describeText.Name = "describe"
	unnamed := sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "", Type: integer}}, integer, sqlmodel.FunctionNullableReturnsNullOnNull)
	unnamed.Name = "p1"
	catalogue.Schemas["public"].Functions["scale_price"] = []*sqlmodel.Function{scale}
	catalogue.Schemas["public"].Functions["describe"] = []*sqlmodel.Function{describeInteger, describeText}
	catalogue.Schemas["public"].Functions["p1"] = []*sqlmodel.Function{unnamed}
	engine := &fakedialect.Engine{}
	return NewTypeResolver(catalogue, NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, engine), engine)
}

func TestDisambiguateParameterNamesKeepsDerivedNamesOverPositionalFallbacks(t *testing.T) {
	t.Parallel()

	parameters := []sqlmodel.QueryParameter{
		{Number: 1, Name: "p2"},
		{Number: 2, Name: "p2"},
		{Number: 3, Name: "id"},
	}
	disambiguateParameterNames(parameters)

	require.Equal(t, "p2", parameters[0].Name, "a name the query or a signature gives keeps its bare form")
	require.Equal(t, "p2_2", parameters[1].Name, "the positional fallback yields")
	require.Equal(t, "id", parameters[2].Name)
}

func TestDisambiguateParameterNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []sqlmodel.QueryParameter
		expected []string
	}{
		{
			name:     "empty slice",
			input:    nil,
			expected: []string{},
		},
		{
			name: "single parameter is left untouched",
			input: []sqlmodel.QueryParameter{
				{Number: 1, Name: "id"},
			},
			expected: []string{"id"},
		},
		{
			name: "distinct names are left untouched",
			input: []sqlmodel.QueryParameter{
				{Number: 1, Name: "id"},
				{Number: 2, Name: "email"},
				{Number: 3, Name: "limit"},
			},
			expected: []string{"id", "email", "limit"},
		},
		{
			name: "two duplicates: first keeps bare name, second gets _2 suffix",
			input: []sqlmodel.QueryParameter{
				{Number: 1, Name: "id"},
				{Number: 2, Name: "id"},
			},
			expected: []string{"id", "id_2"},
		},
		{
			name: "three duplicates produce _2 and _3 suffixes",
			input: []sqlmodel.QueryParameter{
				{Number: 1, Name: "id"},
				{Number: 2, Name: "id"},
				{Number: 3, Name: "id"},
			},
			expected: []string{"id", "id_2", "id_3"},
		},
		{
			name: "interleaved duplicates suffix only the colliders",
			input: []sqlmodel.QueryParameter{
				{Number: 1, Name: "id"},
				{Number: 2, Name: "email"},
				{Number: 3, Name: "id"},
				{Number: 4, Name: "email"},
				{Number: 5, Name: "id"},
			},
			expected: []string{"id", "email", "id_2", "email_2", "id_3"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			parameters := append([]sqlmodel.QueryParameter(nil), testCase.input...)
			disambiguateParameterNames(parameters)
			actual := make([]string, len(parameters))
			for index := range parameters {
				actual[index] = parameters[index].Name
			}
			assert.Equal(t, testCase.expected, actual)
		})
	}
}

func TestFindColumnInCatalogue(t *testing.T) {
	t.Parallel()

	intColumn := func(name string, nullable bool) sqlmodel.Column {
		return sqlmodel.Column{
			Name:     name,
			SQLType:  sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
			Nullable: nullable,
		}
	}
	textColumn := func(name string, nullable bool) sqlmodel.Column {
		return sqlmodel.Column{
			Name:     name,
			SQLType:  sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
			Nullable: nullable,
		}
	}

	t.Run("nil catalogue returns false", func(t *testing.T) {
		t.Parallel()
		resolver := &TypeResolver{}
		_, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{ColumnName: "id"})
		assert.False(t, ok)
	})

	t.Run("nil reference returns false", func(t *testing.T) {
		t.Parallel()
		resolver := &TypeResolver{catalogue: fakedialect.NewCatalogue("public")}
		_, ok := resolver.findColumnInCatalogue(nil)
		assert.False(t, ok)
	})

	t.Run("empty column name returns false", func(t *testing.T) {
		t.Parallel()
		resolver := &TypeResolver{catalogue: fakedialect.NewCatalogue("public")}
		_, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{})
		assert.False(t, ok)
	})

	t.Run("empty schemas return false", func(t *testing.T) {
		t.Parallel()
		resolver := &TypeResolver{catalogue: fakedialect.NewCatalogue("public")}
		_, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{ColumnName: "id"})
		assert.False(t, ok)
	})

	t.Run("single match returns column type and nullability", func(t *testing.T) {
		t.Parallel()
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users", intColumn("id", false), textColumn("name", false))
		resolver := &TypeResolver{catalogue: catalogue}
		match, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{ColumnName: "name"})
		require.True(t, ok)
		assert.Equal(t, "text", match.sqlType.EngineName)
		assert.False(t, match.nullable)
	})

	t.Run("array column is wrapped to the Array category", func(t *testing.T) {
		t.Parallel()
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["posts"] = fakedialect.NewTable("posts", sqlmodel.Column{
			Name:            "tags",
			SQLType:         sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
			IsArray:         true,
			ArrayDimensions: 1,
		})
		resolver := &TypeResolver{catalogue: catalogue}
		match, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{ColumnName: "tags"})
		require.True(t, ok)

		assert.Equal(t, sqlmodel.TypeCategoryArray, match.sqlType.Category)
		assert.Equal(t, "text[]", match.sqlType.EngineName)
		require.NotNil(t, match.sqlType.ElementType)
		assert.Equal(t, sqlmodel.TypeCategoryText, match.sqlType.ElementType.Category)
	})

	t.Run("nullable column propagates the flag", func(t *testing.T) {
		t.Parallel()
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users", textColumn("email", true))
		resolver := &TypeResolver{catalogue: catalogue}
		match, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{ColumnName: "email"})
		require.True(t, ok)
		assert.True(t, match.nullable)
	})

	t.Run("ambiguous match across tables returns false", func(t *testing.T) {
		t.Parallel()
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users", intColumn("id", false))
		catalogue.Schemas["public"].Tables["orders"] = fakedialect.NewTable("orders", intColumn("id", false))
		resolver := &TypeResolver{catalogue: catalogue}
		_, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{ColumnName: "id"})
		assert.False(t, ok)
	})

	t.Run("table alias narrows the search", func(t *testing.T) {
		t.Parallel()
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users", textColumn("email", false))
		catalogue.Schemas["public"].Tables["accounts"] = fakedialect.NewTable("accounts", textColumn("email", true))
		resolver := &TypeResolver{catalogue: catalogue}
		match, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{
			TableAlias: "users",
			ColumnName: "email",
		})
		require.True(t, ok)
		assert.False(t, match.nullable, "should pick users.email, not accounts.email")
	})

	t.Run("table alias not matching any table returns false", func(t *testing.T) {
		t.Parallel()
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users", textColumn("email", false))
		resolver := &TypeResolver{catalogue: catalogue}
		_, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{
			TableAlias: "ghost",
			ColumnName: "email",
		})
		assert.False(t, ok)
	})

	t.Run("column name match is case insensitive", func(t *testing.T) {
		t.Parallel()
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users", textColumn("Email", false))
		resolver := &TypeResolver{catalogue: catalogue}
		_, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{ColumnName: "EMAIL"})
		assert.True(t, ok)
	})

	t.Run("table alias match is case insensitive", func(t *testing.T) {
		t.Parallel()
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["Users"] = fakedialect.NewTable("Users", textColumn("email", false))
		resolver := &TypeResolver{catalogue: catalogue}
		_, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{
			TableAlias: "USERS",
			ColumnName: "email",
		})
		assert.True(t, ok)
	})

	t.Run("ambiguity short-circuits on the second match", func(t *testing.T) {
		t.Parallel()
		catalogue := fakedialect.NewCatalogue("public")
		for index, name := range []string{"a", "b", "c", "d", "e"} {
			catalogue.Schemas["public"].Tables[name] = fakedialect.NewTable(name, intColumn("id", index%2 == 0))
		}
		resolver := &TypeResolver{catalogue: catalogue}
		_, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{ColumnName: "id"})
		assert.False(t, ok, "five tables share id; lookup must reject as ambiguous")
	})

	t.Run("nil schema entry is skipped", func(t *testing.T) {
		t.Parallel()
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["empty"] = nil
		catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users", intColumn("id", false))
		resolver := &TypeResolver{catalogue: catalogue}
		_, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{ColumnName: "id"})
		assert.True(t, ok)
	})

	t.Run("nil table entry is skipped", func(t *testing.T) {
		t.Parallel()
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["ghost"] = nil
		catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users", intColumn("id", false))
		resolver := &TypeResolver{catalogue: catalogue}
		_, ok := resolver.findColumnInCatalogue(&ast.ColumnReference{ColumnName: "id"})
		assert.True(t, ok)
	})
}

func TestFindColumnInTableFollowsIdentifierRules(t *testing.T) {
	t.Parallel()

	insensitive := sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true}
	sensitive := sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false}
	tests := []struct {
		name        string
		identifiers sqlmodel.IdentifierCase
		column      string
		wantFound   bool
	}{
		{name: "exact match", identifiers: sensitive, column: "Email", wantFound: true},
		{name: "another case in a case-insensitive dialect", identifiers: insensitive, column: "EMAIL", wantFound: true},
		{name: "another case in a case-sensitive dialect", identifiers: sensitive, column: "email", wantFound: false},
		{name: "a different name", identifiers: insensitive, column: "name", wantFound: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			table := &sqlmodel.Table{Name: "users", Columns: []sqlmodel.Column{{Name: "Email"}}}
			_, found := findColumnInTable(table, testCase.identifiers, testCase.column)
			assert.Equal(t, testCase.wantFound, found)
		})
	}
}

func TestResolveLikeParameterType(t *testing.T) {
	t.Parallel()

	wantText := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}

	t.Run("nil column reference returns text without error", func(t *testing.T) {
		t.Parallel()
		resolver := newTestTypeResolver()
		raw := ast.RawParameterReference{
			Number:  1,
			Context: sqlmodel.ParameterContextLike,
		}
		sqlType, nullable, err := resolver.resolveLikeParameterType(raw, setupTypeResolverScope())
		require.NoError(t, err)
		assert.Equal(t, wantText, sqlType)
		assert.False(t, nullable)
	})

	t.Run("empty column name returns text without error", func(t *testing.T) {
		t.Parallel()
		resolver := newTestTypeResolver()
		raw := ast.RawParameterReference{
			Number:          1,
			Context:         sqlmodel.ParameterContextLike,
			ColumnReference: &ast.ColumnReference{},
		}
		sqlType, nullable, err := resolver.resolveLikeParameterType(raw, setupTypeResolverScope())
		require.NoError(t, err)
		assert.Equal(t, wantText, sqlType)
		assert.False(t, nullable)
	})

	t.Run("scope hit returns text and clears the resolution error", func(t *testing.T) {
		t.Parallel()
		resolver := newTestTypeResolver()
		raw := ast.RawParameterReference{
			Number:          1,
			Context:         sqlmodel.ParameterContextLike,
			ColumnReference: &ast.ColumnReference{TableAlias: "users", ColumnName: "name"},
		}
		sqlType, nullable, err := resolver.resolveLikeParameterType(raw, setupTypeResolverScope())
		require.NoError(t, err)
		assert.Equal(t, wantText, sqlType)
		assert.False(t, nullable)
	})

	t.Run("catalogue fallback resolves a column missing from scope", func(t *testing.T) {
		t.Parallel()
		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["accounts"] = fakedialect.NewTable(
			"accounts",
			sqlmodel.Column{
				Name:     "label",
				SQLType:  sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
				Nullable: true,
			},
		)
		engine := &fakedialect.Engine{}
		resolver := NewTypeResolver(catalogue, NewFunctionResolver(&ast.FunctionCatalogue{
			Functions: make(map[string][]*sqlmodel.Function),
		}, catalogue, engine), engine)

		raw := ast.RawParameterReference{
			Number:          1,
			Context:         sqlmodel.ParameterContextLike,
			ColumnReference: &ast.ColumnReference{ColumnName: "label"},
		}
		sqlType, nullable, err := resolver.resolveLikeParameterType(raw, setupTypeResolverScope())
		require.NoError(t, err)
		assert.Equal(t, wantText, sqlType)
		assert.False(t, nullable, "LIKE parameter nullability is not column-derived")
	})

	t.Run("unresolved column surfaces the underlying error", func(t *testing.T) {
		t.Parallel()
		resolver := newTestTypeResolver()
		raw := ast.RawParameterReference{
			Number:          1,
			Context:         sqlmodel.ParameterContextLike,
			ColumnReference: &ast.ColumnReference{TableAlias: "ghost", ColumnName: "missing"},
		}
		sqlType, nullable, err := resolver.resolveLikeParameterType(raw, setupTypeResolverScope())
		require.Error(t, err)
		assert.Equal(t, wantText, sqlType, "type stays text even when the column cannot be resolved")
		assert.False(t, nullable)
	})
}

func TestApplyDirectiveKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		directive       *directive.ParameterDirective
		assertParameter func(t *testing.T, parameter *sqlmodel.QueryParameter)
	}{
		{
			name: "optional sets nullable and isOptional",
			directive: &directive.ParameterDirective{
				Number:     1,
				Name:       "filter",
				IsOptional: true,
			},
			assertParameter: func(t *testing.T, parameter *sqlmodel.QueryParameter) {
				t.Helper()
				assert.True(t, parameter.IsOptional, "expected IsOptional to be true")
				assert.True(t, parameter.Nullable, "expected Nullable to be true")
			},
		},
		{
			name: "slice sets isSlice",
			directive: &directive.ParameterDirective{
				Number:  1,
				Name:    "ids",
				IsSlice: true,
			},
			assertParameter: func(t *testing.T, parameter *sqlmodel.QueryParameter) {
				t.Helper()
				assert.True(t, parameter.IsSlice, "expected IsSlice to be true")
			},
		},
		{
			name: "sortable sets columns from directive",
			directive: &directive.ParameterDirective{
				Number:  1,
				Name:    "sort",
				Kind:    sqlmodel.ParameterDirectiveSortable,
				Columns: []string{"name", "created_at", "email"},
			},
			assertParameter: func(t *testing.T, parameter *sqlmodel.QueryParameter) {
				t.Helper()
				assert.Equal(t, []string{"name", "created_at", "email"}, parameter.SortableColumns)
				assert.False(t, parameter.Nullable, "sortable should force nullable to false")
			},
		},
		{
			name: "default and max populate limit bounds",
			directive: &directive.ParameterDirective{
				Number:       1,
				Name:         "page_size",
				DefaultValue: new(20),
				MaxValue:     new(100),
			},
			assertParameter: func(t *testing.T, parameter *sqlmodel.QueryParameter) {
				t.Helper()
				require.NotNil(t, parameter.DefaultLimit, "expected DefaultLimit to be set")
				assert.Equal(t, 20, *parameter.DefaultLimit)
				require.NotNil(t, parameter.MaxLimit, "expected MaxLimit to be set")
				assert.Equal(t, 100, *parameter.MaxLimit)
			},
		},
		{
			name: "param kind makes no special modification",
			directive: &directive.ParameterDirective{
				Number: 1,
				Name:   "value",
				Kind:   sqlmodel.ParameterDirectiveParam,
			},
			assertParameter: func(t *testing.T, parameter *sqlmodel.QueryParameter) {
				t.Helper()

				assert.False(t, parameter.IsOptional, "param should not set IsOptional")
				assert.False(t, parameter.IsSlice, "param should not set IsSlice")
				assert.Nil(t, parameter.SortableColumns, "param should not set SortableColumns")
				assert.Nil(t, parameter.DefaultLimit, "param should not set DefaultLimit")
				assert.Nil(t, parameter.MaxLimit, "param should not set MaxLimit")
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := newTestTypeResolver()
			parameterTypes := map[int]*sqlmodel.QueryParameter{}
			if testCase.directive.Kind != sqlmodel.ParameterDirectiveSortable {
				parameterTypes[testCase.directive.Number] = &sqlmodel.QueryParameter{Number: testCase.directive.Number}
			}
			resolver.applyParameterDirectives(parameterTypes, []*directive.ParameterDirective{testCase.directive})

			parameter, ok := parameterTypes[testCase.directive.Number]
			require.True(t, ok, "expected a parameter for number %d", testCase.directive.Number)
			testCase.assertParameter(t, parameter)
		})
	}
}

func TestExpandStar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tableAlias string
		scope      func() *ScopeChain
		wantCount  int
		wantErr    bool

		wantTable string
	}{
		{
			name:       "qualified table returns only that table columns",
			tableAlias: "users",
			scope:      setupMultiTableScope,
			wantCount:  3,
			wantTable:  "users",
		},
		{
			name:       "qualified CTE returns CTE columns",
			tableAlias: "recent_users",
			scope: func() *ScopeChain {
				scope := setupTypeResolverScope()
				scope.AddCTE("recent_users", []ast.ScopedColumn{
					{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
					{Name: "name", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}},
				})
				return scope
			},
			wantCount: 2,
			wantTable: "recent_users",
		},
		{
			name:       "unknown table returns error",
			tableAlias: "nonexistent",
			scope:      setupTypeResolverScope,
			wantErr:    true,
		},
		{
			name:       "unqualified returns all columns from all tables",
			tableAlias: "",
			scope:      setupMultiTableScope,

			wantCount: 5,
		},
		{
			name:       "unqualified with single table returns all its columns",
			tableAlias: "",
			scope:      setupTypeResolverScope,
			wantCount:  3,
			wantTable:  "users",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := newTestTypeResolver()
			scope := testCase.scope()

			columns, err := resolver.expandStar(testCase.tableAlias, scope)

			if testCase.wantErr {
				require.NotNil(t, err)
				return
			}

			require.Nil(t, err)
			assert.Len(t, columns, testCase.wantCount, "unexpected number of expanded columns")

			if testCase.wantTable != "" && len(columns) > 0 {
				assert.Equal(t, testCase.wantTable, columns[0].SourceTable,
					"first expanded column should come from %s", testCase.wantTable)
			}
		})
	}
}

func TestExpandStarFollowsFromOrder(t *testing.T) {
	t.Parallel()

	setup := func() *ScopeChain {
		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)

		scope.AddTable(
			ast.TableReference{Name: "zebra", Schema: "public"},
			ast.JoinInner,
			&sqlmodel.Table{
				Name: "zebra",
				Columns: []sqlmodel.Column{
					{Name: "z_first", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
					{Name: "z_second", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}},
				},
			},
		)
		scope.AddTable(
			ast.TableReference{Name: "apple", Schema: "public"},
			ast.JoinInner,
			&sqlmodel.Table{
				Name: "apple",
				Columns: []sqlmodel.Column{
					{Name: "a_first", SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
				},
			},
		)
		return scope
	}

	resolver := newTestTypeResolver()

	wantNames := []string{"z_first", "z_second", "a_first"}

	for attempt := range 8 {
		scope := setup()
		columns, err := resolver.expandStar("", scope)
		require.Nil(t, err)

		gotNames := make([]string, len(columns))
		for index := range columns {
			gotNames[index] = columns[index].Name
		}
		require.Equal(t, wantNames, gotNames, "expansion must follow FROM order on attempt %d", attempt)
	}
}

func TestMergeExistingParameterType(t *testing.T) {
	t.Parallel()

	t.Run("cast type overrides existing type", func(t *testing.T) {
		t.Parallel()

		resolver := newTestTypeResolver()
		existing := &sqlmodel.QueryParameter{
			Number:  1,
			Name:    "p1",
			SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
		}

		resolver.mergeExistingParameterType(
			existing,
			sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
			false,
			true,
		)

		assert.Equal(t, sqlmodel.TypeCategoryText, existing.SQLType.Category)
		assert.Equal(t, "text", existing.SQLType.EngineName)
	})

	t.Run("known type replaces unknown type", func(t *testing.T) {
		t.Parallel()

		resolver := newTestTypeResolver()
		existing := &sqlmodel.QueryParameter{
			Number:  1,
			Name:    "p1",
			SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown},
		}

		resolver.mergeExistingParameterType(
			existing,
			sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
			false,
			false,
		)

		assert.Equal(t, sqlmodel.TypeCategoryInteger, existing.SQLType.Category)
	})

	t.Run("both known triggers type promotion", func(t *testing.T) {
		t.Parallel()

		engine := &fakedialect.Engine{
			PromoteTypeFunc: func(left sqlmodel.SQLType, right sqlmodel.SQLType) sqlmodel.SQLType {

				return sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger}
			},
		}
		resolver := newTestTypeResolverWithEngine(engine)
		existing := &sqlmodel.QueryParameter{
			Number:  1,
			Name:    "p1",
			SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
		}

		resolver.mergeExistingParameterType(
			existing,
			sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger},
			false,
			false,
		)

		assert.Equal(t, "int8", existing.SQLType.EngineName)
	})

	t.Run("nullable becomes true if new reference is nullable", func(t *testing.T) {
		t.Parallel()

		resolver := newTestTypeResolver()
		existing := &sqlmodel.QueryParameter{
			Number:   1,
			Name:     "p1",
			SQLType:  sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
			Nullable: false,
		}

		resolver.mergeExistingParameterType(
			existing,
			sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
			true,
			false,
		)

		assert.True(t, existing.Nullable, "merged parameter should become nullable")
	})
}

func TestApplyParameterDirectives(t *testing.T) {
	t.Parallel()

	t.Run("directive nullable override", func(t *testing.T) {
		t.Parallel()

		resolver := newTestTypeResolver()
		parameterTypes := map[int]*sqlmodel.QueryParameter{
			1: {
				Number:   1,
				Name:     "p1",
				SQLType:  sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
				Nullable: false,
			},
		}

		resolver.applyParameterDirectives(parameterTypes, []*directive.ParameterDirective{
			{
				Number:   1,
				Name:     "user_id",
				Kind:     sqlmodel.ParameterDirectiveParam,
				Nullable: new(true),
			},
		})

		require.Contains(t, parameterTypes, 1)
		assert.Equal(t, "user_id", parameterTypes[1].Name)
		assert.True(t, parameterTypes[1].Nullable, "nullable should be overridden by directive")
	})

	t.Run("a directive for a number no placeholder has creates no parameter", func(t *testing.T) {
		t.Parallel()

		resolver := newTestTypeResolver()
		parameterTypes := map[int]*sqlmodel.QueryParameter{}

		resolver.applyParameterDirectives(parameterTypes, []*directive.ParameterDirective{
			{
				Number: 2,
				Name:   "skip",
			},
		})

		assert.NotContains(t, parameterTypes, 2)
	})

	t.Run("a sortable directive creates its own parameter", func(t *testing.T) {
		t.Parallel()

		resolver := newTestTypeResolver()
		parameterTypes := map[int]*sqlmodel.QueryParameter{}

		resolver.applyParameterDirectives(parameterTypes, []*directive.ParameterDirective{
			{
				Name:    "sort",
				Kind:    sqlmodel.ParameterDirectiveSortable,
				Columns: []string{"name"},
			},
		})

		require.Contains(t, parameterTypes, 1)
		assert.Equal(t, "sort", parameterTypes[1].Name)
	})

	t.Run("type hint overrides inferred type", func(t *testing.T) {
		t.Parallel()

		engine := &fakedialect.Engine{
			NormaliseTypeNameFunc: func(name string, modifiers ...int) sqlmodel.SQLType {
				if name == "uuid" {
					return sqlmodel.SQLType{EngineName: "uuid", Category: sqlmodel.TypeCategoryUUID}
				}
				return sqlmodel.SQLType{EngineName: name, Category: sqlmodel.TypeCategoryUnknown}
			},
		}
		resolver := newTestTypeResolverWithEngine(engine)
		parameterTypes := map[int]*sqlmodel.QueryParameter{
			1: {
				Number:  1,
				Name:    "p1",
				SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger},
			},
		}

		resolver.applyParameterDirectives(parameterTypes, []*directive.ParameterDirective{
			{
				Number:   1,
				Name:     "user_id",
				Kind:     sqlmodel.ParameterDirectiveParam,
				TypeHint: new("uuid"),
			},
		})

		assert.Equal(t, sqlmodel.TypeCategoryUUID, parameterTypes[1].SQLType.Category)
		assert.Equal(t, "uuid", parameterTypes[1].SQLType.EngineName)
	})
}

func TestResolveParameterType(t *testing.T) {
	t.Parallel()

	t.Run("cast type takes precedence", func(t *testing.T) {
		t.Parallel()

		resolver := newTestTypeResolver()
		scope := setupTypeResolverScope()

		raw := ast.RawParameterReference{
			Number: 1,
			CastType: &sqlmodel.SQLType{
				EngineName: "text",
				Category:   sqlmodel.TypeCategoryText,
			},
			ColumnReference: &ast.ColumnReference{
				TableAlias: "users",
				ColumnName: "id",
			},
		}

		sqlType, nullable, err := resolver.resolveParameterType(t.Context(), raw, scope)

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryText, sqlType.Category)
		assert.False(t, nullable, "cast parameters are not nullable")
	})

	t.Run("nullable cast type makes the parameter nullable", func(t *testing.T) {
		t.Parallel()

		resolver := newTestTypeResolver()
		scope := setupTypeResolverScope()

		raw := ast.RawParameterReference{
			Number: 1,
			CastType: &sqlmodel.SQLType{
				EngineName: "String",
				Category:   sqlmodel.TypeCategoryText,
				Nullable:   true,
			},
		}

		sqlType, nullable, err := resolver.resolveParameterType(t.Context(), raw, scope)

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryText, sqlType.Category)
		assert.True(t, nullable)
	})

	t.Run("column reference used when no cast", func(t *testing.T) {
		t.Parallel()

		resolver := newTestTypeResolver()
		scope := setupTypeResolverScope()

		raw := ast.RawParameterReference{
			Number: 1,
			ColumnReference: &ast.ColumnReference{
				TableAlias: "users",
				ColumnName: "email",
			},
		}

		sqlType, nullable, err := resolver.resolveParameterType(t.Context(), raw, scope)

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryText, sqlType.Category)
		assert.True(t, nullable, "email column is nullable")
	})

	t.Run("unknown context returns unknown type", func(t *testing.T) {
		t.Parallel()

		resolver := newTestTypeResolver()
		scope := setupTypeResolverScope()

		raw := ast.RawParameterReference{
			Number:  1,
			Context: sqlmodel.ParameterContextUnknown,
		}

		sqlType, nullable, err := resolver.resolveParameterType(t.Context(), raw, scope)

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category)
		assert.False(t, nullable)
	})

	t.Run("unresolvable column reference returns error", func(t *testing.T) {
		t.Parallel()

		resolver := newTestTypeResolver()
		scope := setupTypeResolverScope()

		raw := ast.RawParameterReference{
			Number: 1,
			ColumnReference: &ast.ColumnReference{
				TableAlias: "nonexistent",
				ColumnName: "nonexistent_column",
			},
		}

		sqlType, _, err := resolver.resolveParameterType(t.Context(), raw, scope)

		require.Error(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryUnknown, sqlType.Category)
	})

	t.Run("unknown alias falls back to the bare column when it resolves", func(t *testing.T) {
		t.Parallel()

		resolver := newTestTypeResolver()
		scope := setupTypeResolverScope()

		raw := ast.RawParameterReference{
			Number: 1,
			ColumnReference: &ast.ColumnReference{
				TableAlias: "nonexistent",
				ColumnName: "email",
			},
		}

		sqlType, nullable, err := resolver.resolveParameterType(t.Context(), raw, scope)

		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryText, sqlType.Category)
		assert.True(t, nullable, "email column is nullable")
	})
}

func TestNewTypeResolver(t *testing.T) {
	t.Parallel()

	t.Run("constructs with all fields populated", func(t *testing.T) {
		t.Parallel()

		engine := &fakedialect.Engine{}
		catalogue := fakedialect.NewCatalogue("public")
		builtins := engine.BuiltinFunctions()
		functionResolver := NewFunctionResolver(builtins, catalogue, engine)

		resolver := NewTypeResolver(catalogue, functionResolver, engine)

		require.NotNil(t, resolver)
		assert.Equal(t, catalogue, resolver.catalogue)
		assert.Equal(t, functionResolver, resolver.functionResolver)

		assert.Equal(t, engine, resolver.engine.(*fakedialect.Engine))
	})
}

func TestInferExpressionName_HandlesNil(t *testing.T) {
	t.Parallel()

	t.Run("untyped nil", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "", inferExpressionName(nil))
	})

	t.Run("typed nil column ref", func(t *testing.T) {
		t.Parallel()
		var expression ast.Expression = (*ast.ColumnRefExpression)(nil)
		assert.Equal(t, "", inferExpressionName(expression))
	})

	t.Run("typed nil cast", func(t *testing.T) {
		t.Parallel()
		var expression ast.Expression = (*ast.CastExpression)(nil)
		assert.Equal(t, "", inferExpressionName(expression))
	})

	t.Run("typed nil coalesce", func(t *testing.T) {
		t.Parallel()
		var expression ast.Expression = (*ast.CoalesceExpression)(nil)
		assert.Equal(t, "", inferExpressionName(expression))
	})

	t.Run("typed nil function call", func(t *testing.T) {
		t.Parallel()
		var expression ast.Expression = (*ast.FunctionCallExpression)(nil)
		assert.Equal(t, "", inferExpressionName(expression))
	})
}

func TestInferExpressionName_UnwrapsCastAndCoalesce(t *testing.T) {
	t.Parallel()

	columnReference := &ast.ColumnRefExpression{ColumnName: "email"}

	t.Run("cast wraps column ref", func(t *testing.T) {
		t.Parallel()
		cast := &ast.CastExpression{Inner: columnReference}
		assert.Equal(t, "email", inferExpressionName(cast))
	})

	t.Run("coalesce picks first non-empty argument", func(t *testing.T) {
		t.Parallel()
		coalesce := &ast.CoalesceExpression{
			Arguments: []ast.Expression{
				&ast.FunctionCallExpression{},
				columnReference,
			},
		}
		assert.Equal(t, "email", inferExpressionName(coalesce))
	})

	t.Run("function call returns its name", func(t *testing.T) {
		t.Parallel()
		call := &ast.FunctionCallExpression{FunctionName: "count"}
		assert.Equal(t, "count", inferExpressionName(call))
	})
}

func TestUnwrapCastToColumnRef(t *testing.T) {
	t.Parallel()

	column := &ast.ColumnRefExpression{ColumnName: "page_id"}
	tests := []struct {
		name       string
		expression ast.Expression
		want       *ast.ColumnRefExpression
	}{
		{name: "bare column", expression: column, want: column},
		{name: "single cast", expression: &ast.CastExpression{Inner: column}, want: column},
		{
			name:       "nested casts",
			expression: &ast.CastExpression{Inner: &ast.CastExpression{Inner: column}},
			want:       column,
		},
		{
			name:       "cast over function call is not a column",
			expression: &ast.CastExpression{Inner: &ast.FunctionCallExpression{FunctionName: "now"}},
			want:       nil,
		},
		{name: "literal is not a column", expression: &ast.LiteralExpression{}, want: nil},
		{name: "nil expression", expression: nil, want: nil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, testCase.want, unwrapCastToColumnRef(testCase.expression))
		})
	}
}

func TestApplyCastColumnSource(t *testing.T) {
	t.Parallel()

	t.Run("cast over a resolvable column records the source", func(t *testing.T) {
		t.Parallel()
		output := &sqlmodel.OutputColumn{}

		applyCastColumnSource(output, &ast.CastExpression{Inner: resolverColumnRef("wm", "version_role_id")}, resolverMemberScope())

		assert.Equal(t, "version_role_id", output.SourceColumn)
		assert.Equal(t, "workspace_members_with_latest_version", output.SourceTable)
		assert.Equal(t, "wm", output.SourceQualifier)
	})

	t.Run("cast over a function call leaves the source empty", func(t *testing.T) {
		t.Parallel()
		output := &sqlmodel.OutputColumn{}

		applyCastColumnSource(output, &ast.CastExpression{Inner: &ast.FunctionCallExpression{FunctionName: "now"}}, resolverMemberScope())

		assert.Empty(t, output.SourceColumn)
		assert.Empty(t, output.SourceTable)
	})

	t.Run("cast over an unresolvable column leaves the source empty", func(t *testing.T) {
		t.Parallel()
		output := &sqlmodel.OutputColumn{}

		applyCastColumnSource(output, &ast.CastExpression{Inner: resolverColumnRef("wm", "nonexistent")}, resolverMemberScope())

		assert.Empty(t, output.SourceColumn)
	})
}

func resolverMemberScope() *ScopeChain {
	return &ScopeChain{
		Tables: map[string]*ScopedTable{
			"wm": {
				Name:  "workspace_members_with_latest_version",
				Alias: "wm",
				Columns: []ast.ScopedColumn{
					{Name: "version_role_id", SQLType: sqlmodel.SQLType{EngineName: "uuid"}},
				},
			},
		},
		CTEs: make(map[string]*ResolvedCTE),
	}
}

func resolverColumnRef(alias, column string) *ast.ColumnRefExpression {
	return &ast.ColumnRefExpression{TableAlias: alias, ColumnName: column}
}

func namedArgumentResolver() *TypeResolver {
	catalogue := fakedialect.NewCatalogue("public")
	integer := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
	float := sqlmodel.SQLType{EngineName: "float8", Category: sqlmodel.TypeCategoryFloat}
	scale := sqlmodel.NewFunction([]sqlmodel.FunctionArgument{
		{Name: "amount", Type: integer},
		{Name: "factor", Type: float},
	}, float, sqlmodel.FunctionNullableReturnsNullOnNull)
	scale.Name = "scale_price"
	catalogue.Schemas["public"].Functions["scale_price"] = []*sqlmodel.Function{scale}
	engine := &fakedialect.Engine{}
	return NewTypeResolver(catalogue, NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, engine), engine)
}

func TestResolveParametersMatchesNamedArguments(t *testing.T) {
	t.Parallel()

	resolver := namedArgumentResolver()
	parameters, problems := resolver.ResolveParameters(t.Context(), &ast.RawQueryAnalysis{
		ParameterReferences: []ast.RawParameterReference{
			{Number: 1, Context: sqlmodel.ParameterContextFunctionArgument, EnclosingFunctionName: "scale_price", ArgumentOrdinal: 0},
			{Number: 2, Context: sqlmodel.ParameterContextFunctionArgument, EnclosingFunctionName: "scale_price", ArgumentOrdinal: 1},
		},
		ArgumentNames: map[int]string{1: "factor", 2: "amount"},
	}, NewRootScopeChain(ScopeKindQuery, testScopeRules), nil)

	require.Empty(t, problems)
	require.Len(t, parameters, 2)
	require.Equal(t, "factor", parameters[0].Name)
	require.Equal(t, sqlmodel.TypeCategoryFloat, parameters[0].SQLType.Category)
	require.Equal(t, "amount", parameters[1].Name)
	require.Equal(t, sqlmodel.TypeCategoryInteger, parameters[1].SQLType.Category)
}

func TestResolveExpressionTypeMatchesNamedArguments(t *testing.T) {
	t.Parallel()

	resolver := namedArgumentResolver()
	call := &ast.FunctionCallExpression{
		FunctionName: "scale_price",
		Arguments: []ast.Expression{
			&ast.NamedArgumentExpression{Name: "factor", Value: &ast.LiteralExpression{TypeName: ""}},
			&ast.NamedArgumentExpression{Name: "amount", Value: &ast.LiteralExpression{TypeName: ""}},
		},
	}

	sqlType, _, err := resolver.ResolveExpressionType(t.Context(), call, NewRootScopeChain(ScopeKindQuery, testScopeRules), new(false))
	require.NoError(t, err)
	require.Equal(t, sqlmodel.TypeCategoryFloat, sqlType.Category)

	call.Arguments = append(call.Arguments, &ast.NamedArgumentExpression{Name: "colour", Value: &ast.LiteralExpression{TypeName: ""}})
	_, _, err = resolver.ResolveExpressionType(t.Context(), call, NewRootScopeChain(ScopeKindQuery, testScopeRules), new(false))
	require.Error(t, err)
	require.Equal(t, diag.CodeUnknownFunction, ExtractErrorCode(err))
}

func castChain(depth int, inner ast.Expression) ast.Expression {
	expression := inner
	for range depth {
		expression = &ast.CastExpression{Inner: expression, TypeName: "text"}
	}
	return expression
}

func TestDepthLimitsAreReported(t *testing.T) {
	t.Parallel()

	column := &ast.ColumnRefExpression{ColumnName: "name"}
	tests := []struct {
		name   string
		column ast.RawOutputColumn
		want   bool
	}{
		{name: "a short cast chain", column: ast.RawOutputColumn{Expression: castChain(3, column)}, want: false},
		{name: "a cast chain past the limit", column: ast.RawOutputColumn{Name: "deep", Expression: castChain(MaxExpressionResolveDepth+5, column)}, want: true},
		{name: "an unnamed cast chain past the name limit", column: ast.RawOutputColumn{Expression: castChain(maxInferExpressionNameDepth+5, column)}, want: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver := newTestTypeResolver()
			_, _, problems := resolver.ResolveOutputColumns(t.Context(), []ast.RawOutputColumn{testCase.column}, setupTypeResolverScope())
			reported := 0
			for _, problem := range problems {
				if problem.Code == diag.CodeDepthLimit {
					reported++
				}
			}
			if testCase.want {
				assert.Positive(t, reported)
				return
			}
			assert.Zero(t, reported)
		})
	}
}

func TestColumnSpanFindsTheNamedReference(t *testing.T) {
	t.Parallel()

	placed := &ast.ColumnRefExpression{TableAlias: "t", ColumnName: "Title", Span: ast.Span{Start: 30, End: 35}}
	unplaced := &ast.ColumnRefExpression{TableAlias: "", ColumnName: "title", Span: ast.Span{Start: 0, End: 0}}
	testCases := []struct {
		name       string
		expression ast.Expression
		reference  *ast.ColumnReference
		wantSpan   ast.Span
		wantFound  bool
	}{
		{
			name:       "direct reference ignoring case",
			expression: placed,
			reference:  &ast.ColumnReference{TableAlias: "", ColumnName: "title"},
			wantSpan:   placed.Span,
			wantFound:  true,
		},
		{
			name:       "inside a comparison",
			expression: &ast.ComparisonExpression{Left: unplaced, Right: placed, Operator: "="},
			reference:  &ast.ColumnReference{TableAlias: "T", ColumnName: "title"},
			wantSpan:   placed.Span,
			wantFound:  true,
		},
		{
			name: "inside a function argument",
			expression: &ast.FunctionCallExpression{
				FilterExpression: nil, FunctionName: "lower", Schema: "", Arguments: []ast.Expression{placed},
			},
			reference: &ast.ColumnReference{TableAlias: "", ColumnName: "title"},
			wantSpan:  placed.Span,
			wantFound: true,
		},
		{
			name:       "another qualifier",
			expression: placed,
			reference:  &ast.ColumnReference{TableAlias: "other", ColumnName: "title"},
			wantSpan:   ast.Span{},
			wantFound:  false,
		},
		{
			name:       "no recorded position",
			expression: unplaced,
			reference:  &ast.ColumnReference{TableAlias: "", ColumnName: "title"},
			wantSpan:   ast.Span{},
			wantFound:  false,
		},
		{
			name:       "nil expression",
			expression: nil,
			reference:  &ast.ColumnReference{TableAlias: "", ColumnName: "title"},
			wantSpan:   ast.Span{},
			wantFound:  false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			span, found := columnSpan(testCase.expression, testCase.reference)
			assert.Equal(t, testCase.wantFound, found)
			assert.Equal(t, testCase.wantSpan, span)
		})
	}
}

func TestLocateColumnProblemPlacesOnlyUnplacedProblems(t *testing.T) {
	t.Parallel()

	resolver := &TypeResolver{clauseExpressions: []ast.ClauseExpression{{
		Expression: &ast.ColumnRefExpression{TableAlias: "", ColumnName: "titel", Span: ast.Span{Start: 12, End: 17}},
		Clause:     ast.ClauseWhere,
	}}}
	reference := &ast.ColumnReference{TableAlias: "", ColumnName: "titel"}

	problem := newError(diag.CodeUnknownColumn, "unknown column %q", "titel")
	located := resolver.locateColumnProblem(problem, reference)
	assert.Equal(t, Span{Start: 12, End: 17}, located.Span)
	assert.False(t, problem.Span.Known())

	alreadyPlaced := newError(diag.CodeUnknownColumn, "unknown column %q", "titel")
	alreadyPlaced.Span = Span{Start: 1, End: 2}
	assert.Same(t, alreadyPlaced, resolver.locateColumnProblem(alreadyPlaced, reference))

	assert.Nil(t, resolver.locateColumnProblem(nil, reference))
	assert.Same(t, problem, resolver.locateColumnProblem(problem, nil))
}

func TestTypeResolver_ResolveParameters_ReportsAnUnknownInsertTargetOnce(t *testing.T) {
	t.Parallel()

	resolver := newTypeResolverWithCatalogue(fakedialect.NewCatalogue("public"))
	references := make([]ast.RawParameterReference, 0, 3)
	for number, column := range []string{"title", "body", "published_at"} {
		references = append(references, ast.RawParameterReference{
			Number:          number + 1,
			ColumnReference: &ast.ColumnReference{TableAlias: "articles", ColumnName: column},
			Context:         sqlmodel.ParameterContextAssignment,
		})
	}
	rawAnalysis := &ast.RawQueryAnalysis{
		InsertTarget:        ast.TableReference{Name: "articles"},
		ParameterReferences: references,
	}

	_, diagnostics := resolver.ResolveParameters(
		t.Context(), rawAnalysis, NewRootScopeChain(ScopeKindQuery, testScopeRules), []*directive.ParameterDirective{},
	)

	require.Len(t, diagnostics, 1, "one unknown qualifier is one problem: %v", diagnostics)
	assert.Contains(t, diagnostics[0].Message, `"articles"`)
}

func newOverrideTestResolver(t *testing.T) (*TypeResolver, *ScopeChain) {
	t.Helper()

	address := sqlmodel.NewNetworkType("inet", sqlmodel.NetworkKindAddress)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	table := fakedialect.NewTable("hosts",
		sqlmodel.Column{Name: "id", SQLType: sqlmodel.NewIntegerType("int4", 32, false)},
		sqlmodel.Column{Name: "addr", SQLType: address, GoTypeOverride: &sqlmodel.GoType{Package: "example.com/net", Name: "Addr"}},
		sqlmodel.Column{Name: "label", SQLType: text, GoTypeOverride: &sqlmodel.GoType{Package: "example.com/net", Name: "Label"}},
		sqlmodel.Column{Name: "crew", SQLType: listOf(text)},
	)
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["hosts"] = table
	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	scope.AddTable(ast.TableReference{Name: "hosts"}, ast.JoinInner, table)
	engine := &fakedialect.Engine{InfoFunc: func() dialect.Info {
		info := (&fakedialect.Engine{}).Info()
		info.OperatorFeatures = map[string]ast.ExpressionFeature{"LIKE": ast.SQLFeaturePatternMatch, "~": ast.SQLFeaturePatternMatch}
		return info
	}}
	resolver := NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine)
	return resolver, scope
}

func castAs(inner ast.Expression, typeName string) *ast.CastExpression {
	return &ast.CastExpression{Inner: inner, TypeName: typeName}
}

func columnNamed(name string) *ast.ColumnReference {
	return &ast.ColumnReference{ColumnName: name}
}

func TestAColumnOverrideReachesAParameterDeclaringTheColumnsType(t *testing.T) {
	t.Parallel()

	address := sqlmodel.NewNetworkType("inet", sqlmodel.NetworkKindAddress)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	testCases := []struct {
		name       string
		clauses    []ast.Expression
		references []ast.RawParameterReference
		want       []string
	}{
		{
			name:       "an undeclared parameter compared with the column",
			clauses:    []ast.Expression{&ast.ComparisonExpression{Operator: "=", Left: column("addr"), Right: parameter(1)}},
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison, ColumnReference: columnNamed("addr")}},
			want:       []string{"Addr"},
		},
		{
			name:       "a parameter cast to the column's type",
			clauses:    []ast.Expression{&ast.ComparisonExpression{Operator: "=", Left: column("addr"), Right: castAs(parameter(1), "inet")}},
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextCast, CastType: new(address)}},
			want:       []string{"Addr"},
		},
		{
			name:       "a parameter cast to the column's type on the left of the comparison",
			clauses:    []ast.Expression{&ast.ComparisonExpression{Operator: "<>", Left: castAs(parameter(1), "inet"), Right: column("addr")}},
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextUnknown, CastType: new(address)}},
			want:       []string{"Addr"},
		},
		{
			name:       "a parameter cast to another type",
			clauses:    []ast.Expression{&ast.ComparisonExpression{Operator: "=", Left: column("addr"), Right: castAs(parameter(1), "text")}},
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextCast, CastType: new(text)}},
			want:       []string{""},
		},
		{
			name: "bounds cast to the column's type",
			clauses: []ast.Expression{&ast.BetweenExpression{Inner: column("addr"), Low: castAs(parameter(1), "inet"),
				High: castAs(parameter(2), "inet")}},
			references: []ast.RawParameterReference{
				{Number: 1, Context: sqlmodel.ParameterContextCast, CastType: new(address)},
				{Number: 2, Context: sqlmodel.ParameterContextCast, CastType: new(address)},
			},
			want: []string{"Addr", "Addr"},
		},
		{
			name:       "an IN list member cast to the column's type",
			clauses:    []ast.Expression{&ast.InListExpression{Inner: column("addr"), Values: []ast.Expression{castAs(parameter(1), "inet")}}},
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextCast, CastType: new(address)}},
			want:       []string{"Addr"},
		},
		{
			name: "a typed placeholder modelled as its parameter with a literal default",
			clauses: []ast.Expression{&ast.ComparisonExpression{Operator: "=", Left: column("addr"), Right: castAs(
				&ast.CoalesceExpression{Arguments: []ast.Expression{parameter(1), &ast.LiteralExpression{TypeName: "inet"}}}, "inet")}},
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison, CastType: new(address)}},
			want:       []string{"Addr"},
		},
		{
			name: "a parameter compared with a call over the column",
			clauses: []ast.Expression{&ast.ComparisonExpression{Operator: "=", Right: parameter(1),
				Left: &ast.FunctionCallExpression{FunctionName: "lower", Arguments: []ast.Expression{column("label")}}}},
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison,
				ColumnReference: columnNamed("label"), CastType: new(text)}},
			want: []string{""},
		},
		{
			name:       "a pattern the dialect places as one",
			clauses:    []ast.Expression{&ast.ComparisonExpression{Operator: "LIKE", Left: column("label"), Right: parameter(1)}},
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextLike, ColumnReference: columnNamed("label")}},
			want:       []string{""},
		},
		{
			name:       "a typed pattern the dialect places only as compared",
			clauses:    []ast.Expression{&ast.ComparisonExpression{Operator: "LIKE", Left: column("label"), Right: castAs(parameter(1), "text")}},
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison, CastType: new(text)}},
			want:       []string{""},
		},
		{
			name:       "an undeclared pattern the dialect places as compared with the column",
			clauses:    []ast.Expression{&ast.ComparisonExpression{Operator: "~", Left: column("label"), Right: parameter(1)}},
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison, ColumnReference: columnNamed("label")}},
			want:       []string{""},
		},
		{
			name:       "a parameter assigned to the column declaring its type",
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextAssignment, ColumnReference: columnNamed("addr"), CastType: new(address)}},
			want:       []string{"Addr"},
		},
		{
			name:       "a parameter assigned to the column declaring another type",
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextAssignment, ColumnReference: columnNamed("addr"), CastType: new(text)}},
			want:       []string{""},
		},
		{
			name:       "a declared parameter the dialect compares with a column no predicate shows",
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison, ColumnReference: columnNamed("addr"), CastType: new(address)}},
			want:       []string{""},
		},
		{
			name: "a parameter compared with a lambda parameter named as a column",
			clauses: []ast.Expression{&ast.FunctionCallExpression{FunctionName: "apply", Arguments: []ast.Expression{column("crew"),
				lambda(&ast.ComparisonExpression{Operator: "=", Left: column("addr"), Right: castAs(parameter(1), "inet")}, "addr")}}},
			references: []ast.RawParameterReference{{Number: 1, Context: sqlmodel.ParameterContextComparison, CastType: new(address)}},
			want:       []string{""},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, scope := newOverrideTestResolver(t)
			analysis := &ast.RawQueryAnalysis{ParameterReferences: testCase.references}
			for _, clause := range testCase.clauses {
				analysis.ClauseExpressions = append(analysis.ClauseExpressions, ast.ClauseExpression{Expression: clause, Clause: ast.ClauseWhere})
			}

			parameters, _ := resolver.ResolveParameters(t.Context(), analysis, scope, nil)

			got := make([]string, 0, len(parameters))
			for index := range parameters {
				override := ""
				if parameters[index].GoTypeOverride != nil {
					override = parameters[index].GoTypeOverride.Name
				}
				got = append(got, override)
			}
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestAColumnOverrideReachesADeclaredParameterInACTEBody(t *testing.T) {
	t.Parallel()

	resolver, scope := newOverrideTestResolver(t)
	address := sqlmodel.NewNetworkType("inet", sqlmodel.NetworkKindAddress)
	reference := ast.RawParameterReference{Number: 1, Context: sqlmodel.ParameterContextCast, CastType: new(address)}
	analysis := &ast.RawQueryAnalysis{
		ParameterReferences: []ast.RawParameterReference{reference},
		CTEDefinitions: []ast.RawCTEDefinition{{
			Name:                "found",
			FromTables:          []ast.TableReference{{Name: "hosts"}},
			ParameterReferences: []ast.RawParameterReference{reference},
			Query: &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{{
				Expression: &ast.ComparisonExpression{Operator: "=", Left: column("addr"), Right: castAs(parameter(1), "inet")},
				Clause:     ast.ClauseWhere,
			}}},
		}},
	}

	parameters, _ := resolver.ResolveParameters(t.Context(), analysis, scope, nil)

	require.Len(t, parameters, 1)
	require.NotNil(t, parameters[0].GoTypeOverride)
	require.Equal(t, "Addr", parameters[0].GoTypeOverride.Name)
}
