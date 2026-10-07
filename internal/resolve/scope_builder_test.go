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
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestOutputColumnsToScoped(t *testing.T) {
	t.Parallel()

	intType := sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}
	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}

	tests := []struct {
		name     string
		columns  []sqlmodel.OutputColumn
		expected []ast.ScopedColumn
	}{
		{
			name:     "empty input returns empty slice",
			columns:  nil,
			expected: []ast.ScopedColumn{},
		},
		{
			name: "converts output columns to scoped columns",
			columns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: intType, Nullable: false},
				{Name: "email", SQLType: textType, Nullable: true},
			},
			expected: []ast.ScopedColumn{
				{Name: "id", SQLType: intType, Nullable: false},
				{Name: "email", SQLType: textType, Nullable: true},
			},
		},
		{
			name: "extra output column fields are not carried over",
			columns: []sqlmodel.OutputColumn{
				{
					Name:         "name",
					SQLType:      textType,
					Nullable:     false,
					SourceTable:  "users",
					SourceColumn: "name",
					IsEmbedded:   true,
					EmbedTable:   "users",
				},
			},
			expected: []ast.ScopedColumn{
				{Name: "name", SQLType: textType, Nullable: false},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := outputColumnsToScoped(testCase.columns)

			assert.Equal(t, testCase.expected, got)
		})
	}
}

func TestResolveTableReference(t *testing.T) {
	t.Parallel()

	intType := sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}
	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = &sqlmodel.Table{
		Name:   "users",
		Schema: "public",
		Columns: []sqlmodel.Column{
			{Name: "id", SQLType: intType},
			{Name: "email", SQLType: textType},
		},
	}
	catalogue.Schemas["public"].Views["active_users"] = &sqlmodel.View{
		Name:   "active_users",
		Schema: "public",
		Columns: []sqlmodel.Column{
			{Name: "id", SQLType: intType},
		},
	}

	builder := &ScopeBuilder{
		engine:    &fakedialect.Engine{},
		catalogue: catalogue,
	}

	tests := []struct {
		name      string
		reference ast.TableReference
		wantName  string
		wantErr   string
	}{
		{
			name:      "known table resolves successfully",
			reference: ast.TableReference{Name: "users"},
			wantName:  "users",
		},
		{
			name:      "known table in explicit schema resolves",
			reference: ast.TableReference{Schema: "public", Name: "users"},
			wantName:  "users",
		},
		{
			name:      "view resolves as table",
			reference: ast.TableReference{Name: "active_users"},
			wantName:  "active_users",
		},
		{
			name:      "unknown table returns error",
			reference: ast.TableReference{Name: "nonexistent"},
			wantErr:   `unknown table or view "nonexistent"`,
		},
		{
			name:      "unknown schema returns error",
			reference: ast.TableReference{Schema: "private", Name: "users"},
			wantErr:   `unknown schema "private"`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := builder.resolveTableReference(testCase.reference)

			if testCase.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErr)
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.NotNil(t, got)
				assert.Equal(t, testCase.wantName, got.Name)
			}
		})
	}
}

func TestResolveTableReferenceNamesAnUnqualifiedUnknownTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		catalogue *sqlmodel.Catalogue
		reference ast.TableReference
		wantErr   string
	}{
		{
			name:      "no schema is on the search path yet",
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{}},
			reference: ast.TableReference{Name: "articles"},
			wantErr:   `unknown table or view "articles"`,
		},
		{
			name: "the tables sit in the unnamed schema",
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{
				"": {Name: "", Tables: map[string]*sqlmodel.Table{"other": {Name: "other"}}},
			}},
			reference: ast.TableReference{Name: "articles"},
			wantErr:   `unknown table or view "articles"`,
		},
		{
			name:      "the search path names a schema the catalogue lacks",
			catalogue: &sqlmodel.Catalogue{DefaultSchema: "app", Schemas: map[string]*sqlmodel.Schema{}},
			reference: ast.TableReference{Name: "articles"},
			wantErr:   `unknown table or view "articles" in schema "app"`,
		},
		{
			name:      "a qualified reference to a missing schema names the schema",
			catalogue: &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{}},
			reference: ast.TableReference{Schema: "archive", Name: "articles"},
			wantErr:   `unknown schema "archive"`,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := &ScopeBuilder{engine: &fakedialect.Engine{}, catalogue: testCase.catalogue}
			table, err := builder.resolveTableReference(testCase.reference)

			require.EqualError(t, err, testCase.wantErr)
			assert.Nil(t, table)
		})
	}
}

func TestASequenceTheEngineLetsAQueryReadResolvesAsARelation(t *testing.T) {
	t.Parallel()

	lastValue := sqlmodel.NewColumn("last_value", sqlmodel.NewIntegerType("int8", 64, false), false)
	catalogue := &sqlmodel.Catalogue{DefaultSchema: "public", SearchPath: []string{"public"}, Schemas: map[string]*sqlmodel.Schema{
		"public": {Name: "public", Sequences: map[string]*sqlmodel.Sequence{
			"counter": {Name: "counter", Schema: "public", Columns: []sqlmodel.Column{lastValue}},
			"opaque":  {Name: "opaque", Schema: "public"},
		}},
	}}
	builder := &ScopeBuilder{engine: &fakedialect.Engine{}, catalogue: catalogue}

	testCases := []struct {
		name        string
		reference   ast.TableReference
		wantColumns []string
		wantErr     string
	}{
		{name: "an unqualified sequence", reference: ast.TableReference{Name: "counter"}, wantColumns: []string{"last_value"}},
		{name: "a qualified sequence", reference: ast.TableReference{Schema: "public", Name: "counter"}, wantColumns: []string{"last_value"}},
		{name: "a sequence with no columns", reference: ast.TableReference{Name: "opaque"},
			wantErr: `unknown table or view "opaque" in schema "public"`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			table, err := builder.resolveTableReference(testCase.reference)

			if testCase.wantErr != "" {
				require.EqualError(t, err, testCase.wantErr)
				return
			}
			require.NoError(t, err)
			names := make([]string, 0, len(table.Columns))
			for index := range table.Columns {
				names = append(names, table.Columns[index].Name)
			}
			require.Equal(t, testCase.wantColumns, names)
		})
	}
}

func TestBuildScopeChain(t *testing.T) {
	t.Parallel()

	intType := sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}
	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}

	t.Run("single FROM table populates scope", func(t *testing.T) {
		t.Parallel()

		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["users"] = &sqlmodel.Table{
			Name: "users",
			Columns: []sqlmodel.Column{
				{Name: "id", SQLType: intType},
				{Name: "name", SQLType: textType},
			},
		}

		builder := &ScopeBuilder{
			engine:    &fakedialect.Engine{},
			catalogue: catalogue,
		}

		raw := &ast.RawQueryAnalysis{
			FromTables: []ast.TableReference{
				{Name: "users"},
			},
		}

		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		diagnostics := builder.buildScopeChain(raw, scope)

		assert.Empty(t, diagnostics)
		assert.Contains(t, scope.Tables, "users")
		assert.Len(t, scope.Tables["users"].Columns, 2)
	})

	t.Run("join adds both tables to scope", func(t *testing.T) {
		t.Parallel()

		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["users"] = &sqlmodel.Table{
			Name: "users",
			Columns: []sqlmodel.Column{
				{Name: "id", SQLType: intType},
			},
		}
		catalogue.Schemas["public"].Tables["orders"] = &sqlmodel.Table{
			Name: "orders",
			Columns: []sqlmodel.Column{
				{Name: "order_id", SQLType: intType},
			},
		}

		builder := &ScopeBuilder{
			engine:    &fakedialect.Engine{},
			catalogue: catalogue,
		}

		raw := &ast.RawQueryAnalysis{
			FromTables: []ast.TableReference{
				{Name: "users"},
			},
			JoinClauses: []ast.JoinClause{
				{
					Table: ast.TableReference{Name: "orders"},
					Kind:  ast.JoinLeft,
				},
			},
		}

		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		diagnostics := builder.buildScopeChain(raw, scope)

		assert.Empty(t, diagnostics)
		assert.Contains(t, scope.Tables, "users")
		assert.Contains(t, scope.Tables, "orders")
	})

	t.Run("unknown FROM table produces warning diagnostic", func(t *testing.T) {
		t.Parallel()

		catalogue := fakedialect.NewCatalogue("public")

		builder := &ScopeBuilder{
			engine:    &fakedialect.Engine{},
			catalogue: catalogue,
		}

		raw := &ast.RawQueryAnalysis{
			FromTables: []ast.TableReference{
				{Name: "nonexistent"},
			},
		}

		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		diagnostics := builder.buildScopeChain(raw, scope)

		require.Len(t, diagnostics, 1)
		assert.Equal(t, diag.SeverityWarning, diagnostics[0].Severity)
		assert.Equal(t, "Q003", diagnostics[0].Code)
		assert.Contains(t, diagnostics[0].Message, "nonexistent")
	})

	t.Run("FROM table matching CTE reuses CTE instead of catalogue lookup", func(t *testing.T) {
		t.Parallel()

		catalogue := fakedialect.NewCatalogue("public")

		builder := &ScopeBuilder{
			engine:    &fakedialect.Engine{},
			catalogue: catalogue,
		}

		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		scope.AddCTE("recent_users", []ast.ScopedColumn{
			{Name: "id", SQLType: intType, Nullable: false},
		})

		raw := &ast.RawQueryAnalysis{
			FromTables: []ast.TableReference{
				{Name: "recent_users"},
			},
		}

		diagnostics := builder.buildScopeChain(raw, scope)

		assert.Empty(t, diagnostics)

		assert.Contains(t, scope.CTEs, "recent_users")
		require.Contains(t, scope.Tables, "recent_users",
			"FROM-referenced CTE should be promoted into scope.tables so unqualified column resolution is deterministic")
		promoted := scope.Tables["recent_users"]
		require.Len(t, promoted.Columns, 1)
		assert.Equal(t, "id", promoted.Columns[0].Name)
	})

	t.Run("FROM table matching CTE with different alias registers alias", func(t *testing.T) {
		t.Parallel()

		catalogue := fakedialect.NewCatalogue("public")

		builder := &ScopeBuilder{
			engine:    &fakedialect.Engine{},
			catalogue: catalogue,
		}

		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		scope.AddCTE("recent_users", []ast.ScopedColumn{
			{Name: "id", SQLType: intType, Nullable: false},
		})

		raw := &ast.RawQueryAnalysis{
			FromTables: []ast.TableReference{
				{Name: "recent_users", Alias: "ru"},
			},
		}

		diagnostics := builder.buildScopeChain(raw, scope)

		assert.Empty(t, diagnostics)

		assert.Contains(t, scope.CTEs, "ru")
	})

}

func TestResolveJoinClauses(t *testing.T) {
	t.Parallel()

	intType := sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}

	t.Run("known join table is added to scope", func(t *testing.T) {
		t.Parallel()

		catalogue := fakedialect.NewCatalogue("public")
		catalogue.Schemas["public"].Tables["orders"] = &sqlmodel.Table{
			Name:    "orders",
			Columns: []sqlmodel.Column{{Name: "order_id", SQLType: intType}},
		}

		builder := &ScopeBuilder{
			engine:    &fakedialect.Engine{},
			catalogue: catalogue,
		}

		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)

		diagnostics := builder.resolveJoinClauses(
			[]ast.JoinClause{
				{
					Table: ast.TableReference{Name: "orders"},
					Kind:  ast.JoinInner,
				},
			},
			scope,
		)

		assert.Empty(t, diagnostics)
		assert.Contains(t, scope.Tables, "orders")
	})

	t.Run("unknown join table produces warning", func(t *testing.T) {
		t.Parallel()

		catalogue := fakedialect.NewCatalogue("public")

		builder := &ScopeBuilder{
			engine:    &fakedialect.Engine{},
			catalogue: catalogue,
		}

		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)

		diagnostics := builder.resolveJoinClauses(
			[]ast.JoinClause{
				{
					Table: ast.TableReference{Name: "missing"},
					Kind:  ast.JoinLeft,
				},
			},
			scope,
		)

		require.Len(t, diagnostics, 1)
		assert.Equal(t, "Q003", diagnostics[0].Code)
		assert.Contains(t, diagnostics[0].Message, "missing")
	})

	t.Run("join referencing CTE uses CTE columns", func(t *testing.T) {
		t.Parallel()

		catalogue := fakedialect.NewCatalogue("public")

		builder := &ScopeBuilder{
			engine:    &fakedialect.Engine{},
			catalogue: catalogue,
		}

		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		scope.AddCTE("my_cte", []ast.ScopedColumn{
			{Name: "val", SQLType: intType},
		})

		diagnostics := builder.resolveJoinClauses(
			[]ast.JoinClause{
				{
					Table: ast.TableReference{Name: "my_cte", Alias: "mc"},
					Kind:  ast.JoinInner,
				},
			},
			scope,
		)

		assert.Empty(t, diagnostics)

		assert.Contains(t, scope.CTEs, "mc")
	})
}

func TestCTEBodiesResolveTheirRelations(t *testing.T) {
	t.Parallel()

	intType := sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}
	earlier := ast.RawCTEDefinition{
		Name:          "parent_cte",
		FromTables:    []ast.TableReference{{Name: "users"}},
		OutputColumns: []ast.RawOutputColumn{{Name: "id", ColumnName: "id"}},
	}

	testCases := []struct {
		name            string
		definition      ast.RawCTEDefinition
		expectedColumns []string
		expectedCode    string
	}{
		{
			name: "a FROM reference to an earlier CTE reads its columns",
			definition: ast.RawCTEDefinition{
				Name:          "child",
				FromTables:    []ast.TableReference{{Name: "parent_cte"}},
				OutputColumns: []ast.RawOutputColumn{{IsStar: true}},
			},
			expectedColumns: []string{"id"},
			expectedCode:    "",
		},
		{
			name: "an aliased reference to an earlier CTE reads its columns under the alias",
			definition: ast.RawCTEDefinition{
				Name:          "child",
				FromTables:    []ast.TableReference{{Name: "parent_cte", Alias: "p"}},
				OutputColumns: []ast.RawOutputColumn{{Name: "id", TableAlias: "p", ColumnName: "id"}},
			},
			expectedColumns: []string{"id"},
			expectedCode:    "",
		},
		{
			name: "a join onto an earlier CTE reads its columns",
			definition: ast.RawCTEDefinition{
				Name:          "child",
				FromTables:    []ast.TableReference{{Name: "users", Alias: "u"}},
				JoinClauses:   []ast.JoinClause{{Table: ast.TableReference{Name: "parent_cte", Alias: "p"}, Kind: ast.JoinInner}},
				OutputColumns: []ast.RawOutputColumn{{Name: "id", TableAlias: "p", ColumnName: "id"}},
			},
			expectedColumns: []string{"id"},
			expectedCode:    "",
		},
		{
			name: "an unknown table in a CTE body is reported",
			definition: ast.RawCTEDefinition{
				Name:          "child",
				FromTables:    []ast.TableReference{{Name: "no_such_table"}},
				OutputColumns: []ast.RawOutputColumn{{IsStar: true}},
			},
			expectedColumns: nil,
			expectedCode:    diag.CodeUnknownTable,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := fakedialect.NewCatalogue("public")
			catalogue.Schemas["public"].Tables["users"] = &sqlmodel.Table{
				Name:    "users",
				Schema:  "public",
				Columns: []sqlmodel.Column{{Name: "id", SQLType: intType}},
			}
			builder := NewCatalogueScopeBuilder(&fakedialect.Engine{}, catalogue)
			scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)

			diagnostics := builder.resolveCTEs(t.Context(), []ast.RawCTEDefinition{earlier, testCase.definition}, scope)

			if testCase.expectedCode != "" {
				require.Len(t, diagnostics, 1)
				require.Equal(t, testCase.expectedCode, diagnostics[0].Code)
				return
			}
			require.Empty(t, diagnostics)
			cte, exists := scope.CTE("child")
			require.True(t, exists)
			require.Equal(t, testCase.expectedColumns, scopedColumnNames(cte.columns))
			require.Equal(t, sqlmodel.TypeCategoryInteger, cte.columns[0].SQLType.Category)
		})
	}
}

func TestResolveCompoundBranches_UnresolvedColumnDoesNotRaiseArityError(t *testing.T) {
	t.Parallel()

	intType := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
		sqlmodel.Column{Name: "id", SQLType: intType},
		sqlmodel.Column{Name: "name", SQLType: textType},
	)
	builder := NewCatalogueScopeBuilder(&fakedialect.Engine{}, catalogue)

	primaryColumns := []sqlmodel.OutputColumn{
		{Name: "id", SQLType: intType},
		{Name: "name", SQLType: textType},
	}

	branches := []ast.RawCompoundBranch{
		{
			Operator: ast.CompoundUnion,
			Query: &ast.RawQueryAnalysis{
				FromTables: []ast.TableReference{{Name: "users"}},
				OutputColumns: []ast.RawOutputColumn{
					{ColumnName: "id"},
					{ColumnName: "no_such_col"},
				},
			},
		},
	}

	diagnostics := builder.ResolveCompoundBranches(t.Context(), branches, primaryColumns, nil)

	for _, diagnostic := range diagnostics {
		assert.NotEqual(t, diag.CodeCompoundColumnCount, diagnostic.Code,
			"a dropped unresolved branch column must not raise the arity-mismatch error")
		assert.NotEqual(t, diag.SeverityError, diagnostic.Severity,
			"only the unknown-column warning should surface, never a generation-blocking error")
	}
	require.NotEmpty(t, diagnostics, "the unknown column must still produce its own diagnostic")
}

func TestResolveCompoundBranches_GenuineArityMismatchStillReported(t *testing.T) {
	t.Parallel()

	intType := sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}
	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["users"] = fakedialect.NewTable("users",
		sqlmodel.Column{Name: "id", SQLType: intType},
		sqlmodel.Column{Name: "name", SQLType: textType},
	)
	builder := NewCatalogueScopeBuilder(&fakedialect.Engine{}, catalogue)

	primaryColumns := []sqlmodel.OutputColumn{
		{Name: "id", SQLType: intType},
		{Name: "name", SQLType: textType},
	}

	branches := []ast.RawCompoundBranch{
		{
			Operator: ast.CompoundUnion,
			Query: &ast.RawQueryAnalysis{
				FromTables: []ast.TableReference{{Name: "users"}},
				OutputColumns: []ast.RawOutputColumn{
					{ColumnName: "id"},
				},
			},
		},
	}

	diagnostics := builder.ResolveCompoundBranches(t.Context(), branches, primaryColumns, nil)

	foundArityError := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeCompoundColumnCount {
			foundArityError = true
			assert.Equal(t, diag.SeverityError, diagnostic.Severity)
		}
	}
	assert.True(t, foundArityError, "a genuine arity mismatch must still be reported")
}

func TestQueryAnalyser_ResolveTableValuedFunctions(t *testing.T) {
	t.Parallel()

	textType := sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}
	engine := &fakedialect.Engine{
		TableValuedFunctionColumnsFunc: func(functionName string) []ast.ScopedColumn {
			if functionName != "json_each" {
				return nil
			}
			return []ast.ScopedColumn{{Name: "value", SQLType: textType, Nullable: true}}
		},
	}
	builder := &ScopeBuilder{engine: engine, catalogue: fakedialect.NewCatalogue("public")}

	t.Run("known function registers as a derived table without diagnostics", func(t *testing.T) {
		t.Parallel()

		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		diagnostics := builder.resolveTableValuedFunctions(
			t.Context(),
			[]ast.RawTableValuedFunctionReference{{FunctionName: "json_each", Alias: "je"}},
			scope,
		)

		assert.Empty(t, diagnostics)
		column, _, err := scope.ResolveColumn("je", "value")
		require.NoError(t, err)
		assert.Equal(t, sqlmodel.TypeCategoryText, column.SQLType.Category)
	})

	t.Run("unknown function reports an unknown-table diagnostic", func(t *testing.T) {
		t.Parallel()

		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		diagnostics := builder.resolveTableValuedFunctions(
			t.Context(),
			[]ast.RawTableValuedFunctionReference{{FunctionName: "missing", Alias: "m"}},
			scope,
		)

		require.Len(t, diagnostics, 1)
		assert.Equal(t, diag.CodeUnknownTable, diagnostics[0].Code)
	})
}

func nestedQueryBuilder() *ScopeBuilder {
	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Tables["t"] = &sqlmodel.Table{
		Name:   "t",
		Schema: "public",
		Columns: []sqlmodel.Column{
			{Name: "x", SQLType: sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger}},
		},
	}
	engine := &fakedialect.Engine{}
	return NewScopeBuilder(engine, catalogue, NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine))
}

func projectX() []ast.RawOutputColumn {
	return []ast.RawOutputColumn{{Name: "x", ColumnName: "x", Expression: &ast.ColumnRefExpression{ColumnName: "x"}}}
}

func TestBuildQueryScopeResolvesNestedDerivedTables(t *testing.T) {
	t.Parallel()

	innermost := &ast.RawQueryAnalysis{OutputColumns: projectX(), FromTables: []ast.TableReference{{Name: "t"}}}
	middle := &ast.RawQueryAnalysis{
		OutputColumns:    projectX(),
		RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "a", InnerQuery: innermost, JoinKind: ast.JoinInner}},
	}
	outer := &ast.RawQueryAnalysis{
		OutputColumns:    projectX(),
		RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "b", InnerQuery: middle, JoinKind: ast.JoinInner}},
	}

	builder := nestedQueryBuilder()
	scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	diagnostics := builder.BuildQueryScope(t.Context(), outer, scope)
	require.Empty(t, diagnostics)

	columns, _, columnDiagnostics := builder.TypeResolver.ResolveOutputColumns(t.Context(), outer.OutputColumns, scope)
	require.Empty(t, columnDiagnostics)
	require.Len(t, columns, 1)
	require.Equal(t, sqlmodel.TypeCategoryInteger, columns[0].SQLType.Category)
}

func TestBuildQueryScopeResolvesDerivedTablesInCompoundBranches(t *testing.T) {
	t.Parallel()

	derived := &ast.RawQueryAnalysis{OutputColumns: projectX(), FromTables: []ast.TableReference{{Name: "t"}}}
	branch := &ast.RawQueryAnalysis{
		OutputColumns:    projectX(),
		RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "d", InnerQuery: derived, JoinKind: ast.JoinInner}},
	}
	outer := &ast.RawQueryAnalysis{
		OutputColumns:    projectX(),
		FromTables:       []ast.TableReference{{Name: "t"}},
		CompoundBranches: []ast.RawCompoundBranch{{Operator: ast.CompoundUnionAll, Query: branch}},
	}

	builder := nestedQueryBuilder()
	scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	require.Empty(t, builder.BuildQueryScope(t.Context(), outer, scope))
	columns, _, columnDiagnostics := builder.TypeResolver.ResolveOutputColumns(t.Context(), outer.OutputColumns, scope)
	require.Empty(t, columnDiagnostics)
	require.Empty(t, builder.ResolveCompoundBranches(t.Context(), outer.CompoundBranches, columns, scope))
}

func recursiveCTEBuilder() *ScopeBuilder {
	catalogue := fakedialect.NewCatalogue("public")
	engine := &fakedialect.Engine{
		NormaliseTypeNameFunc: func(name string, _ ...int) sqlmodel.SQLType {
			if name == "wide" {
				return sqlmodel.NewIntegerType("wide", 64, false)
			}
			return sqlmodel.NewIntegerType("narrow", 8, false)
		},
		PromoteTypeFunc: func(_, right sqlmodel.SQLType) sqlmodel.SQLType {
			return right
		},
		BinaryResultFunc: func(string, sqlmodel.SQLType, sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
			return sqlmodel.NewIntegerType("wide", 64, false), sqlmodel.FunctionNullableReturnsNullOnNull, true
		},
	}
	return NewScopeBuilder(engine, catalogue, NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine))
}

func TestRecursiveCTETakesItsTypesFromTheAnchor(t *testing.T) {
	t.Parallel()

	generation := func(expression ast.Expression) []ast.RawOutputColumn {
		return []ast.RawOutputColumn{{Name: "generation", Expression: expression}}
	}
	recursiveBranch := &ast.RawQueryAnalysis{
		OutputColumns: generation(&ast.BinaryOpExpression{
			Operator: "+",
			Left:     &ast.ColumnRefExpression{TableAlias: "family", ColumnName: "generation"},
			Right:    &ast.LiteralExpression{TypeName: "narrow"},
		}),
		FromTables: []ast.TableReference{{Name: "family"}},
	}
	definition := ast.RawCTEDefinition{
		Name:             "family",
		IsRecursive:      true,
		OutputColumns:    generation(&ast.LiteralExpression{TypeName: "narrow"}),
		CompoundBranches: []ast.RawCompoundBranch{{Operator: ast.CompoundUnionAll, Query: recursiveBranch}},
	}

	builder := recursiveCTEBuilder()
	scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	require.Empty(t, builder.resolveCTEs(t.Context(), []ast.RawCTEDefinition{definition}, scope))

	cte, exists := scope.CTE("family")
	require.True(t, exists)
	require.Len(t, cte.columns, 1)
	require.Equal(t, "narrow", cte.columns[0].SQLType.EngineName)
	require.False(t, cte.columns[0].Nullable)
}

func TestRecursiveCTEStillPromotesAnotherAnchor(t *testing.T) {
	t.Parallel()

	generation := func(typeName string) []ast.RawOutputColumn {
		return []ast.RawOutputColumn{{Name: "generation", Expression: &ast.LiteralExpression{TypeName: typeName}}}
	}
	definition := ast.RawCTEDefinition{
		Name:          "family",
		IsRecursive:   true,
		OutputColumns: generation("narrow"),
		CompoundBranches: []ast.RawCompoundBranch{{
			Operator: ast.CompoundUnionAll,
			Query:    &ast.RawQueryAnalysis{OutputColumns: generation("wide")},
		}},
	}

	builder := recursiveCTEBuilder()
	scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	require.Empty(t, builder.resolveCTEs(t.Context(), []ast.RawCTEDefinition{definition}, scope))

	cte, exists := scope.CTE("family")
	require.True(t, exists)
	require.Equal(t, "wide", cte.columns[0].SQLType.EngineName)
}

func TestCompoundBranchesMergeNullabilityIntoThePrimaryColumns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		primaryNullable bool
		branchNullable  bool
		wantNullable    bool
	}{
		{name: "a nullable branch makes a non-null primary nullable", primaryNullable: false, branchNullable: true, wantNullable: true},
		{name: "a non-null branch leaves a nullable primary nullable", primaryNullable: true, branchNullable: false, wantNullable: true},
		{name: "two non-null columns stay non-null", primaryNullable: false, branchNullable: false, wantNullable: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			primary := []sqlmodel.OutputColumn{{Name: "x", Nullable: testCase.primaryNullable}}
			branch := []sqlmodel.OutputColumn{{Name: "x", Nullable: testCase.branchNullable}}

			mergeBranchNullability(primary, branch)

			assert.Equal(t, testCase.wantNullable, primary[0].Nullable)
		})
	}
}

func TestCompoundBranchesPromoteThePrimaryColumnTypes(t *testing.T) {
	t.Parallel()

	narrow := sqlmodel.NewIntegerType("narrow", 8, false)
	wide := sqlmodel.NewIntegerType("wide", 64, false)

	testCases := []struct {
		name        string
		primaryType sqlmodel.SQLType
		branchType  sqlmodel.SQLType
		wantType    string
	}{
		{name: "a known primary type is promoted with the branch's", primaryType: narrow, branchType: wide, wantType: "wide"},
		{name: "an unknown primary type takes the branch's", primaryType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown}, branchType: narrow, wantType: "narrow"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			primary := []sqlmodel.OutputColumn{{Name: "x", SQLType: testCase.primaryType, Nullable: false}}
			branch := []sqlmodel.OutputColumn{{Name: "x", SQLType: testCase.branchType, Nullable: true}}

			recursiveCTEBuilder().promoteBranchTypes(primary, branch)

			assert.Equal(t, testCase.wantType, primary[0].SQLType.EngineName)
			assert.False(t, primary[0].Nullable)
		})
	}
}

func TestResolveSilentColumnsKeepsUnresolvedColumns(t *testing.T) {
	t.Parallel()

	builder := nestedQueryBuilder()
	scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	builder.addRawTablesToScope(t.Context(), &ast.RawQueryAnalysis{FromTables: []ast.TableReference{{Name: "t"}}}, scope)

	columns := builder.resolveSilentColumns(t.Context(), []ast.RawOutputColumn{
		{Name: "x", ColumnName: "x"},
		{Name: "missing", ColumnName: "missing"},
	}, scope)

	require.Len(t, columns, 2)
	require.Equal(t, "missing", columns[1].Name)
	require.Equal(t, sqlmodel.TypeCategoryUnknown, columns[1].SQLType.Category)
	require.True(t, columns[1].Nullable)
}

func fromOrderBuilder() *ScopeBuilder {
	catalogue := fakedialect.NewCatalogue("public")
	integer := sqlmodel.NewIntegerType("int4", 32, false)
	catalogue.Schemas["public"].Tables["zebra"] = &sqlmodel.Table{Name: "zebra", Schema: "public",
		Columns: []sqlmodel.Column{sqlmodel.NewColumn("z_first", integer, false), sqlmodel.NewColumn("z_second", integer, false)}}
	catalogue.Schemas["public"].Tables["apple"] = &sqlmodel.Table{Name: "apple", Schema: "public",
		Columns: []sqlmodel.Column{sqlmodel.NewColumn("a_first", integer, false)}}
	return NewCatalogueScopeBuilder(&fakedialect.Engine{}, catalogue)
}

func TestStarInsideACTEBodyFollowsItsFromOrder(t *testing.T) {
	t.Parallel()

	cte := ast.RawCTEDefinition{
		Name:          "pairs",
		FromTables:    []ast.TableReference{{Name: "apple"}},
		JoinClauses:   []ast.JoinClause{{Table: ast.TableReference{Name: "zebra"}, Kind: ast.JoinInner}},
		FromOrder:     []string{"zebra", "apple"},
		OutputColumns: []ast.RawOutputColumn{{IsStar: true}},
	}
	query := &ast.RawQueryAnalysis{
		CTEDefinitions: []ast.RawCTEDefinition{cte},
		FromTables:     []ast.TableReference{{Name: "pairs"}},
		OutputColumns:  []ast.RawOutputColumn{{IsStar: true}},
	}

	builder := fromOrderBuilder()
	scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	require.Empty(t, builder.BuildQueryScope(t.Context(), query, scope))
	columns, _, problems := builder.TypeResolver.ResolveOutputColumns(t.Context(), query.OutputColumns, scope)
	require.Empty(t, problems)

	names := make([]string, len(columns))
	for index := range columns {
		names[index] = columns[index].Name
	}
	assert.Equal(t, []string{"z_first", "z_second", "a_first"}, names)
}

func TestARuntimeDerivedTableFunctionTakesTheDeclaredColumns(t *testing.T) {
	t.Parallel()

	engine := &fakedialect.Engine{
		TableValuedFunctionColumnsFunc: func(name string) []ast.ScopedColumn {
			if name == "read_file" {
				return []ast.ScopedColumn{}
			}
			return nil
		},
	}
	catalogue := fakedialect.NewCatalogue("public")
	builder := NewCatalogueScopeBuilder(engine, catalogue)
	query := &ast.RawQueryAnalysis{
		RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{
			{FunctionName: "read_file", Alias: "first"},
			{FunctionName: "read_file", Alias: "second"},
		},
		OutputColumns: []ast.RawOutputColumn{{IsStar: true}},
	}
	declared := []ast.ScopedColumn{
		{Name: "id", SQLType: sqlmodel.NewIntegerType("int4", 32, false), Nullable: false},
		{Name: "label", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), Nullable: true},
	}

	scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	scope.DeclareRuntimeColumns(declared)
	require.Empty(t, builder.BuildQueryScope(t.Context(), query, scope))

	first, found := scope.Table("first")
	require.True(t, found)
	assert.Equal(t, declared, first.Columns)
	second, found := scope.Table("second")
	require.True(t, found)
	assert.Empty(t, second.Columns, "only the first runtime-derived source takes the declared columns")

	column, _, err := scope.ResolveColumn("first", "id")
	require.NoError(t, err)
	assert.Equal(t, "int4", column.SQLType.EngineName)
}

func columnX(qualifier string) []ast.RawOutputColumn {
	return []ast.RawOutputColumn{{Name: "x", TableAlias: qualifier, ColumnName: "x"}}
}

func TestNestedQueriesSeeTheRelationsTheyMayName(t *testing.T) {
	t.Parallel()

	readT := &ast.RawQueryAnalysis{OutputColumns: columnX(""), FromTables: []ast.TableReference{{Name: "t"}}}
	cteOverT := ast.RawCTEDefinition{Name: "a", OutputColumns: columnX(""), FromTables: []ast.TableReference{{Name: "t"}}, Query: readT}
	testCases := []struct {
		name  string
		query *ast.RawQueryAnalysis
	}{
		{
			name: "a CTE body reads its own derived table",
			query: &ast.RawQueryAnalysis{
				CTEDefinitions: []ast.RawCTEDefinition{{Name: "a", OutputColumns: columnX("d"), Query: &ast.RawQueryAnalysis{
					OutputColumns:    columnX("d"),
					RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "d", InnerQuery: readT, JoinKind: ast.JoinInner}},
				}}},
				FromTables:    []ast.TableReference{{Name: "a"}},
				OutputColumns: columnX(""),
			},
		},
		{
			name: "a CTE body reads the CTE of its own WITH clause",
			query: &ast.RawQueryAnalysis{
				CTEDefinitions: []ast.RawCTEDefinition{{Name: "a", OutputColumns: columnX(""), Query: &ast.RawQueryAnalysis{
					CTEDefinitions: []ast.RawCTEDefinition{cteOverT},
					FromTables:     []ast.TableReference{{Name: "a"}},
					OutputColumns:  columnX(""),
				}}},
				FromTables:    []ast.TableReference{{Name: "a"}},
				OutputColumns: columnX(""),
			},
		},
		{
			name: "a CTE body joins a CTE declared before it",
			query: &ast.RawQueryAnalysis{
				CTEDefinitions: []ast.RawCTEDefinition{cteOverT, {Name: "b", OutputColumns: columnX("y"), Query: &ast.RawQueryAnalysis{
					FromTables:    []ast.TableReference{{Name: "t", Alias: "z"}},
					JoinClauses:   []ast.JoinClause{{Table: ast.TableReference{Name: "a", Alias: "y"}, Kind: ast.JoinInner}},
					OutputColumns: columnX("y"),
				}}},
				FromTables:    []ast.TableReference{{Name: "b"}},
				OutputColumns: columnX(""),
			},
		},
		{
			name: "a derived table reads a CTE of the enclosing WITH clause",
			query: &ast.RawQueryAnalysis{
				CTEDefinitions: []ast.RawCTEDefinition{cteOverT},
				RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "d", JoinKind: ast.JoinInner, InnerQuery: &ast.RawQueryAnalysis{
					OutputColumns: columnX(""), FromTables: []ast.TableReference{{Name: "a"}},
				}}},
				OutputColumns: columnX("d"),
			},
		},
		{
			name: "a set operation branch in a derived table reads a CTE of the enclosing WITH clause",
			query: &ast.RawQueryAnalysis{
				CTEDefinitions: []ast.RawCTEDefinition{cteOverT},
				RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "u", JoinKind: ast.JoinInner, InnerQuery: &ast.RawQueryAnalysis{
					OutputColumns: columnX(""), FromTables: []ast.TableReference{{Name: "a"}},
					CompoundBranches: []ast.RawCompoundBranch{{Operator: ast.CompoundUnionAll, Query: &ast.RawQueryAnalysis{
						OutputColumns: columnX(""), FromTables: []ast.TableReference{{Name: "a"}},
					}}},
				}}},
				OutputColumns: columnX("u"),
			},
		},
		{
			name: "a derived table reads a relation named before it, as a LATERAL subquery does",
			query: &ast.RawQueryAnalysis{
				FromTables: []ast.TableReference{{Name: "t", Alias: "c"}},
				RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "l", JoinKind: ast.JoinCross, InnerQuery: &ast.RawQueryAnalysis{
					OutputColumns: columnX("c"), FromTables: []ast.TableReference{{Name: "t", Alias: "inner_t"}},
				}}},
				OutputColumns: columnX("l"),
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := nestedQueryBuilder()
			scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
			require.Empty(t, builder.BuildQueryScope(t.Context(), testCase.query, scope))
			columns, _, problems := builder.TypeResolver.ResolveOutputColumns(t.Context(), testCase.query.OutputColumns, scope)
			require.Empty(t, problems)
			require.Len(t, columns, 1)
			require.Equal(t, sqlmodel.TypeCategoryInteger, columns[0].SQLType.Category)
		})
	}
}

func TestColumnListsRenameTheColumnsOfARelation(t *testing.T) {
	t.Parallel()

	twoColumns := &ast.RawQueryAnalysis{
		OutputColumns: []ast.RawOutputColumn{{Name: "x", ColumnName: "x"}, {Name: "y", Expression: &ast.LiteralExpression{TypeName: "text"}}},
		FromTables:    []ast.TableReference{{Name: "t"}},
	}
	testCases := []struct {
		name          string
		columnNames   []string
		expectedNames []string
		expectedCode  string
	}{
		{name: "a full list renames every column", columnNames: []string{"a", "b"}, expectedNames: []string{"a", "b"}},
		{name: "a shorter list renames the leading columns and keeps the rest", columnNames: []string{"a"},
			expectedNames: []string{"a", "y"}},
		{name: "a longer list is reported and its surplus names ignored", columnNames: []string{"a", "b", "c"},
			expectedNames: []string{"a", "b"}, expectedCode: diag.CodeCompoundColumnCount},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			for _, query := range []*ast.RawQueryAnalysis{
				{
					CTEDefinitions: []ast.RawCTEDefinition{{Name: "c", OutputColumns: twoColumns.OutputColumns, ColumnNames: testCase.columnNames, Query: twoColumns}},
					FromTables:     []ast.TableReference{{Name: "c"}},
					OutputColumns:  []ast.RawOutputColumn{{IsStar: true}},
				},
				{
					RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "c", ColumnNames: testCase.columnNames, InnerQuery: twoColumns, JoinKind: ast.JoinInner}},
					OutputColumns:    []ast.RawOutputColumn{{IsStar: true}},
				},
			} {
				builder := nestedQueryBuilder()
				scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
				diagnostics := builder.BuildQueryScope(t.Context(), query, scope)
				columns, _, problems := builder.TypeResolver.ResolveOutputColumns(t.Context(), query.OutputColumns, scope)
				require.Empty(t, problems)
				names := make([]string, len(columns))
				for index := range columns {
					names[index] = columns[index].Name
				}
				require.Equal(t, testCase.expectedNames, names)
				require.Equal(t, sqlmodel.TypeCategoryInteger, columns[0].SQLType.Category)
				if testCase.expectedCode == "" {
					require.Empty(t, diagnostics)
					continue
				}
				require.Len(t, diagnostics, 1)
				require.Equal(t, testCase.expectedCode, diagnostics[0].Code)
			}
		})
	}
}

func TestAColumnOfNoKnownTypeTakesTheTypeOfTheLaterBranches(t *testing.T) {
	t.Parallel()

	nullFirst := &ast.RawQueryAnalysis{
		OutputColumns: []ast.RawOutputColumn{{Name: "x", Expression: &ast.LiteralExpression{TypeName: ""}}},
		CompoundBranches: []ast.RawCompoundBranch{{Operator: ast.CompoundUnionAll, Query: &ast.RawQueryAnalysis{
			OutputColumns: columnX(""), FromTables: []ast.TableReference{{Name: "t"}},
		}}},
	}

	builder := nestedQueryBuilder()
	scope := builder.TypeResolver.NewRootScope(ScopeKindQuery)
	require.Empty(t, builder.BuildQueryScope(t.Context(), nullFirst, scope))
	columns, _, problems := builder.TypeResolver.ResolveOutputColumns(t.Context(), nullFirst.OutputColumns, scope)
	require.Empty(t, problems)
	require.Equal(t, sqlmodel.TypeCategoryUnknown, columns[0].SQLType.Category)
	require.Empty(t, builder.ResolveCompoundBranches(t.Context(), nullFirst.CompoundBranches, columns, scope))

	require.Equal(t, sqlmodel.TypeCategoryInteger, columns[0].SQLType.Category)
}

func catalogueFunctionBuilder(fromCatalogue func(ctx context.Context, catalogue *sqlmodel.Catalogue, functionName string) ([]ast.ScopedColumn, error)) *ScopeBuilder {
	engine := &fakedialect.CatalogueResolverEngine{Engine: &fakedialect.Engine{}, FromCatalogueFunc: fromCatalogue}
	catalogue := fakedialect.NewCatalogue("public")
	return NewScopeBuilder(engine, catalogue, NewTypeResolver(catalogue, NewFunctionResolver(&ast.FunctionCatalogue{}, catalogue, engine), engine))
}

func TestTableValuedFunctionsReportWhatTheDialectFailedOn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		failure   error
		name      string
		wantCodes []string
	}{
		{
			name:      "a panic the dialect recovered is reported in place of an unknown function",
			failure:   fmt.Errorf("table function columns: %w: index out of range", fault.ErrPanic),
			wantCodes: []string{diag.CodeRecoveredPanic, diag.CodeRecoveredPanic},
		},
		{
			name:      "an interruption is no problem with the query and ends the walk",
			failure:   fmt.Errorf("walk: %w", context.DeadlineExceeded),
			wantCodes: nil,
		},
		{
			name:      "any other failure leaves the function unknown",
			failure:   errors.New("not resolvable"),
			wantCodes: []string{diag.CodeUnknownTable, diag.CodeUnknownTable},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := catalogueFunctionBuilder(func(context.Context, *sqlmodel.Catalogue, string) ([]ast.ScopedColumn, error) {
				return nil, testCase.failure
			})
			raw := &ast.RawQueryAnalysis{RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{
				{FunctionName: "my_udf", Alias: "u"},
				{FunctionName: "other_udf", Alias: "o"},
			}}

			problems := builder.BuildQueryScope(t.Context(), raw, NewRootScopeChain(ScopeKindQuery, testScopeRules))

			var codes []string
			for _, problem := range problems {
				codes = append(codes, problem.Code)
			}
			require.Equal(t, testCase.wantCodes, codes)
		})
	}
}

func TestScopeBuildingHandsTheDialectTheCallersContext(t *testing.T) {
	t.Parallel()

	type marker struct{}
	reference := ast.RawTableValuedFunctionReference{FunctionName: "my_udf", Alias: "u"}
	testCases := []struct {
		resolve func(ctx context.Context, builder *ScopeBuilder)
		name    string
	}{
		{
			name: "a FROM item of the query",
			resolve: func(ctx context.Context, builder *ScopeBuilder) {
				raw := &ast.RawQueryAnalysis{RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{reference}}
				_ = builder.BuildQueryScope(ctx, raw, NewRootScopeChain(ScopeKindQuery, testScopeRules))
			},
		},
		{
			name: "a FROM item of a subquery the select list holds",
			resolve: func(ctx context.Context, builder *ScopeBuilder) {
				inner := &ast.RawQueryAnalysis{
					RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{reference},
					OutputColumns:           []ast.RawOutputColumn{{ColumnName: "out", TableAlias: "u"}},
				}
				raw := []ast.RawOutputColumn{{Name: "value", Expression: &ast.ScalarSubqueryExpression{InnerQuery: inner}}}
				_ = builder.TypeResolver.ResolveProjection(ctx, raw, nil, NewRootScopeChain(ScopeKindQuery, testScopeRules))
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var seen []any
			builder := catalogueFunctionBuilder(func(ctx context.Context, _ *sqlmodel.Catalogue, _ string) ([]ast.ScopedColumn, error) {
				seen = append(seen, ctx.Value(marker{}))
				return []ast.ScopedColumn{{Name: "out", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")}}, nil
			})

			testCase.resolve(context.WithValue(t.Context(), marker{}, "caller"), builder)

			require.NotEmpty(t, seen)
			for _, value := range seen {
				require.Equal(t, "caller", value)
			}
		})
	}
}

func TestSilentScopeBuildingRecordsTheDepthLimitItReaches(t *testing.T) {
	t.Parallel()

	inner := &ast.RawQueryAnalysis{FromTables: []ast.TableReference{{Name: "t"}}, OutputColumns: []ast.RawOutputColumn{{Name: "x", ColumnName: "x"}}}
	testCases := []struct {
		raw       *ast.RawQueryAnalysis
		name      string
		depth     int
		wantLimit bool
	}{
		{name: "derived tables within the limit", depth: 0, wantLimit: false,
			raw: &ast.RawQueryAnalysis{RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "d", InnerQuery: inner}}}},
		{name: "derived tables past the limit", depth: MaxExpressionResolveDepth, wantLimit: true,
			raw: &ast.RawQueryAnalysis{RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "d", InnerQuery: inner}}}},
		{name: "CTEs past the limit", depth: MaxExpressionResolveDepth, wantLimit: true,
			raw: &ast.RawQueryAnalysis{CTEDefinitions: []ast.RawCTEDefinition{{Name: "c", Query: inner}}}},
		{name: "plain tables past the limit", depth: MaxExpressionResolveDepth, wantLimit: false,
			raw: &ast.RawQueryAnalysis{FromTables: []ast.TableReference{{Name: "t"}}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := nestedQueryBuilder()
			scope := scopeAtDepth(builder.TypeResolver.NewRootScope(ScopeKindQuery), testCase.depth)

			builder.addRawTablesToScope(t.Context(), testCase.raw, scope)
			builder.addRawTablesToScope(t.Context(), testCase.raw, scope)

			limits := builder.TypeResolver.TakeDepthLimits()
			if !testCase.wantLimit {
				require.Empty(t, limits)
				return
			}
			require.Len(t, limits, 1, "the limit is recorded once")
			require.Equal(t, diag.CodeDepthLimit, limits[0].Code)
			require.Contains(t, limits[0].Message, "were not followed")
		})
	}
}

func TestASubqueryReadingAFromCallKeepsTheProblemsAroundIt(t *testing.T) {
	t.Parallel()

	integer := &ast.LiteralExpression{TypeName: "integer"}
	text := &ast.LiteralExpression{TypeName: "text"}
	testCases := []struct {
		name      string
		reference ast.RawTableValuedFunctionReference
	}{
		{name: "a table-valued call typed by its arguments", reference: ast.RawTableValuedFunctionReference{
			FunctionName: "series", Alias: "g", Arguments: []ast.Expression{integer, integer}}},
		{name: "a call of a function returning one value", reference: ast.RawTableValuedFunctionReference{
			FunctionName: "lower", Alias: "g", Arguments: []ast.Expression{text}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := scalarFunctionEngine(true)
			engine.TableValuedFunctionColumnsFunc = func(functionName string) []ast.ScopedColumn {
				if functionName != "series" {
					return nil
				}
				return []ast.ScopedColumn{{Name: "g", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int4"), Nullable: false}}
			}
			catalogue := fakedialect.NewCatalogue("public")
			resolver := NewTypeResolver(catalogue, NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine), engine)
			subquery := &ast.ScalarSubqueryExpression{InnerQuery: &ast.RawQueryAnalysis{
				RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{testCase.reference},
				OutputColumns:           []ast.RawOutputColumn{{Name: "g", ColumnName: "g"}},
			}}
			item := []ast.RawOutputColumn{{Name: "v", Expression: &ast.CoalesceExpression{Arguments: []ast.Expression{
				&ast.ColumnRefExpression{ColumnName: "missing_col"}, subquery,
			}}}}

			projection := resolver.ResolveProjection(t.Context(), item, nil, setupTypeResolverScope())

			messages := make([]string, 0, len(projection.Problems))
			for _, problem := range projection.Problems {
				messages = append(messages, problem.Message)
			}
			require.Contains(t, messages, `unknown column "missing_col"`)
		})
	}
}

func scopeAtDepth(root *ScopeChain, depth int) *ScopeChain {
	scope := root
	for range depth {
		scope = scope.CreateChildScope(ScopeKindSubquery)
	}
	return scope
}

func TestEveryWayIntoANestedQueryContinuesTheEnclosingDepth(t *testing.T) {
	t.Parallel()

	derived := func() *ast.RawQueryAnalysis {
		return &ast.RawQueryAnalysis{RawDerivedTables: []ast.RawDerivedTableReference{{
			Alias: "d", JoinKind: ast.JoinInner,
			InnerQuery: &ast.RawQueryAnalysis{OutputColumns: projectX(), FromTables: []ast.TableReference{{Name: "t"}}},
		}}}
	}
	testCases := []struct {
		enter     func(ctx context.Context, builder *ScopeBuilder, scope *ScopeChain) []*Error
		name      string
		depth     int
		wantLimit bool
	}{
		{name: "a scalar subquery within the budget", depth: MaxExpressionResolveDepth - 1, wantLimit: false, enter: typeScalarSubquery},
		{name: "a scalar subquery past the budget", depth: MaxExpressionResolveDepth, wantLimit: true, enter: typeScalarSubquery},
		{name: "the arms of a compound query within the budget", depth: MaxExpressionResolveDepth - 1, wantLimit: false, enter: typeCompoundArms},
		{name: "the arms of a compound query past the budget", depth: MaxExpressionResolveDepth, wantLimit: true, enter: typeCompoundArms},
		{name: "a derived table the analyser reports on within the budget", depth: MaxExpressionResolveDepth - 1, wantLimit: false,
			enter: func(ctx context.Context, builder *ScopeBuilder, scope *ScopeChain) []*Error {
				return builder.BuildQueryScope(ctx, derived(), scope)
			}},
		{name: "a derived table the analyser reports on past the budget", depth: MaxExpressionResolveDepth, wantLimit: true,
			enter: func(ctx context.Context, builder *ScopeBuilder, scope *ScopeChain) []*Error {
				return builder.BuildQueryScope(ctx, derived(), scope)
			}},
		{name: "a derived table a subquery reads past the budget", depth: MaxExpressionResolveDepth, wantLimit: true,
			enter: func(ctx context.Context, builder *ScopeBuilder, scope *ScopeChain) []*Error {
				builder.addRawTablesToScope(ctx, derived(), scope)
				return nil
			}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := nestedQueryBuilder()
			scope := scopeAtDepth(builder.TypeResolver.NewRootScope(ScopeKindQuery), testCase.depth)

			problems := testCase.enter(t.Context(), builder, scope)
			problems = append(problems, builder.TypeResolver.TakeDepthLimits()...)

			limited := slices.ContainsFunc(problems, func(problem *Error) bool { return problem.Code == diag.CodeDepthLimit })
			require.Equal(t, testCase.wantLimit, limited, "%v", problems)
		})
	}
}

func TestScalarSubqueriesAndDerivedTablesShareOneDepthBudget(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		levels    int
		wantLimit bool
	}{
		{name: "nesting within the budget", levels: 40, wantLimit: false},
		{name: "nesting past the budget, though no one kind of nesting is", levels: 200, wantLimit: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := nestedQueryBuilder()
			subquery := alternatingSubqueries(testCase.levels)

			var sqlType sqlmodel.SQLType
			scaling.Within(t, time.Second, func(ctx context.Context) {
				sqlType, _, _ = builder.TypeResolver.ResolveExpressionType(ctx, subquery, builder.TypeResolver.NewRootScope(ScopeKindQuery), nil)
			})

			limits := builder.TypeResolver.TakeDepthLimits()
			require.Equal(t, testCase.wantLimit, len(limits) > 0, "%v", limits)
			if !testCase.wantLimit {
				require.Equal(t, sqlmodel.TypeCategoryInteger, sqlType.Category)
			}
		})
	}
}

func typeScalarSubquery(ctx context.Context, builder *ScopeBuilder, scope *ScopeChain) []*Error {
	subquery := &ast.ScalarSubqueryExpression{InnerQuery: &ast.RawQueryAnalysis{OutputColumns: projectX(), FromTables: []ast.TableReference{{Name: "t"}}}}
	_, _, _ = builder.TypeResolver.ResolveExpressionType(ctx, subquery, scope, nil)
	return nil
}

func typeCompoundArms(ctx context.Context, builder *ScopeBuilder, scope *ScopeChain) []*Error {
	raw := &ast.RawQueryAnalysis{
		OutputColumns:    projectX(),
		CompoundBranches: []ast.RawCompoundBranch{{Query: &ast.RawQueryAnalysis{OutputColumns: projectX(), FromTables: []ast.TableReference{{Name: "t"}}}}},
	}
	_, problems := builder.TypeResolver.ResolveParameters(ctx, raw, scope, nil)
	return problems
}

func alternatingSubqueries(levels int) *ast.ScalarSubqueryExpression {
	inner := &ast.RawQueryAnalysis{OutputColumns: projectX(), FromTables: []ast.TableReference{{Name: "t"}}}
	var subquery *ast.ScalarSubqueryExpression
	for range levels {
		subquery = &ast.ScalarSubqueryExpression{InnerQuery: &ast.RawQueryAnalysis{
			OutputColumns:    []ast.RawOutputColumn{{Name: "x", ColumnName: "x"}},
			RawDerivedTables: []ast.RawDerivedTableReference{{Alias: "d", JoinKind: ast.JoinInner, InnerQuery: inner}},
		}}
		inner = &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Name: "x", Expression: subquery}}}
	}
	return subquery
}
