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
	"cmp"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

var testScopeRules = ScopeRules{
	RowIDType:    sqlmodel.NewIntegerType("integer", 64, false),
	RowIDAliases: []string{"rowid", "_rowid_", "oid"},
	Identifiers:  sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
}

type rowValueTyper struct{}

func (rowValueTyper) RowValueType(relation dialect.RowRelation) sqlmodel.SQLType {
	row := sqlmodel.NewSQLType(sqlmodel.TypeCategoryComposite, cmp.Or(relation.Name, "record"))
	row.Schema = relation.Schema
	row.Nullable = relation.OuterJoined
	for _, column := range relation.Columns {
		row.StructFields = append(row.StructFields, sqlmodel.StructField{Name: column.Name, SQLType: column.SQLType})
	}
	return row
}

func catalogueColumn(name string, category sqlmodel.SQLTypeCategory, nullable bool) sqlmodel.Column {
	return sqlmodel.Column{
		Name:     name,
		SQLType:  sqlmodel.SQLType{EngineName: strings.ToLower(name), Category: category},
		Nullable: nullable,
	}
}

func catalogueTable(name string, columns ...sqlmodel.Column) *sqlmodel.Table {
	return &sqlmodel.Table{
		Name:    name,
		Columns: columns,
	}
}

func scopedColumn(name string, category sqlmodel.SQLTypeCategory, nullable bool) ast.ScopedColumn {
	return ast.ScopedColumn{
		Name:     name,
		SQLType:  sqlmodel.SQLType{EngineName: strings.ToLower(name), Category: category},
		Nullable: nullable,
	}
}

func TestScopeChain_AddTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		joinKind         ast.JoinKind
		catalogueColumns []sqlmodel.Column
		tableReference   ast.TableReference

		wantNullable []bool

		existingBecomeNullable bool
	}{
		{
			name:     "inner join preserves nullability",
			joinKind: ast.JoinInner,
			catalogueColumns: []sqlmodel.Column{
				catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
				catalogueColumn("email", sqlmodel.TypeCategoryText, true),
			},
			tableReference:         ast.TableReference{Name: "users"},
			wantNullable:           []bool{false, true},
			existingBecomeNullable: false,
		},
		{
			name:     "left join forces all added columns nullable",
			joinKind: ast.JoinLeft,
			catalogueColumns: []sqlmodel.Column{
				catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
				catalogueColumn("name", sqlmodel.TypeCategoryText, false),
			},
			tableReference:         ast.TableReference{Name: "orders"},
			wantNullable:           []bool{true, true},
			existingBecomeNullable: false,
		},
		{
			name:     "right join makes existing tables nullable",
			joinKind: ast.JoinRight,
			catalogueColumns: []sqlmodel.Column{
				catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
			},
			tableReference:         ast.TableReference{Name: "orders"},
			wantNullable:           []bool{false},
			existingBecomeNullable: true,
		},
		{
			name:     "full join makes all columns nullable",
			joinKind: ast.JoinFull,
			catalogueColumns: []sqlmodel.Column{
				catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
				catalogueColumn("amount", sqlmodel.TypeCategoryDecimal, false),
			},
			tableReference:         ast.TableReference{Name: "orders"},
			wantNullable:           []bool{true, true},
			existingBecomeNullable: true,
		},
		{
			name:     "a join only its engine has forces both directions nullable by its sides",
			joinKind: ast.JoinKind{Keyword: "POSITIONAL", Nullable: ast.JoinSideLeft | ast.JoinSideRight, Filters: false},
			catalogueColumns: []sqlmodel.Column{
				catalogueColumn("val", sqlmodel.TypeCategoryInteger, false),
			},
			tableReference:         ast.TableReference{Name: "series"},
			wantNullable:           []bool{true},
			existingBecomeNullable: true,
		},
		{
			name:     "a filtering join only its engine has keeps nullability",
			joinKind: ast.JoinKind{Keyword: "ASOF", Nullable: 0, Filters: true},
			catalogueColumns: []sqlmodel.Column{
				catalogueColumn("val", sqlmodel.TypeCategoryInteger, false),
			},
			tableReference:         ast.TableReference{Name: "quotes"},
			wantNullable:           []bool{false},
			existingBecomeNullable: false,
		},
		{
			name:     "alias used when present",
			joinKind: ast.JoinInner,
			catalogueColumns: []sqlmodel.Column{
				catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
			},
			tableReference: ast.TableReference{
				Name:  "users",
				Alias: "u",
			},
			wantNullable:           []bool{false},
			existingBecomeNullable: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)

			existingCat := catalogueTable("existing",
				catalogueColumn("pk", sqlmodel.TypeCategoryInteger, false),
			)
			scope.AddTable(ast.TableReference{Name: "existing"}, ast.JoinInner, existingCat)

			table := catalogueTable(testCase.tableReference.Name, testCase.catalogueColumns...)
			scope.AddTable(testCase.tableReference, testCase.joinKind, table)

			lookupKey := testCase.tableReference.Alias
			if lookupKey == "" {
				lookupKey = testCase.tableReference.Name
			}

			scopedTable, exists := scope.Tables[lookupKey]
			require.True(t, exists, "table should be registered under %q", lookupKey)

			require.Len(t, scopedTable.Columns, len(testCase.wantNullable))
			for index, wantNull := range testCase.wantNullable {
				assert.Equal(t, wantNull, scopedTable.Columns[index].Nullable,
					"column %q nullability mismatch", scopedTable.Columns[index].Name)
			}

			existingScoped := scope.Tables["existing"]
			require.NotNil(t, existingScoped)
			assert.Equal(t, testCase.existingBecomeNullable, existingScoped.Columns[0].Nullable,
				"existing table column nullability mismatch")
		})
	}
}

func TestScopeChain_AddCTE(t *testing.T) {
	t.Parallel()

	t.Run("registers CTE by lowercase name with correct columns", func(t *testing.T) {
		t.Parallel()

		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		columns := []ast.ScopedColumn{
			scopedColumn("id", sqlmodel.TypeCategoryInteger, false),
			scopedColumn("label", sqlmodel.TypeCategoryText, true),
		}

		scope.AddCTE("ActiveUsers", columns)

		cte, exists := scope.CTEs["activeusers"]
		require.True(t, exists)
		assert.Equal(t, "ActiveUsers", cte.name, "original casing should be preserved in name field")
		assert.Equal(t, columns, cte.columns)
	})

	t.Run("case-insensitive lookup works", func(t *testing.T) {
		t.Parallel()

		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		columns := []ast.ScopedColumn{
			scopedColumn("x", sqlmodel.TypeCategoryInteger, false),
		}

		scope.AddCTE("MyCTE", columns)

		for _, alias := range []string{"MyCTE", "mycte", "MYCTE", "myCTE"} {
			cte, found := scope.CTE(alias)
			require.True(t, found, "alias %q should resolve", alias)
			assert.Equal(t, "MyCTE", cte.name, "CTE name should preserve original casing")
			require.Len(t, cte.columns, 1)
			assert.Equal(t, "x", cte.columns[0].Name)
		}
	})
}

func TestScopeChain_AddDerivedTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		joinKind     ast.JoinKind
		inputColumns []ast.ScopedColumn
		wantNullable []bool
	}{
		{
			name:     "left join derived table forces nullable",
			joinKind: ast.JoinLeft,
			inputColumns: []ast.ScopedColumn{
				scopedColumn("val", sqlmodel.TypeCategoryInteger, false),
				scopedColumn("tag", sqlmodel.TypeCategoryText, false),
			},
			wantNullable: []bool{true, true},
		},
		{
			name:     "inner join derived table preserves nullability",
			joinKind: ast.JoinInner,
			inputColumns: []ast.ScopedColumn{
				scopedColumn("val", sqlmodel.TypeCategoryInteger, false),
				scopedColumn("tag", sqlmodel.TypeCategoryText, true),
			},
			wantNullable: []bool{false, true},
		},
		{
			name:     "full join derived table forces nullable",
			joinKind: ast.JoinFull,
			inputColumns: []ast.ScopedColumn{
				scopedColumn("val", sqlmodel.TypeCategoryInteger, false),
			},
			wantNullable: []bool{true},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			reference := DerivedTableReference{
				Alias:    "derived",
				Columns:  testCase.inputColumns,
				JoinKind: testCase.joinKind,
			}

			scope.AddDerivedTable(reference)

			scopedTable, exists := scope.Tables["derived"]
			require.True(t, exists)
			require.Len(t, scopedTable.Columns, len(testCase.wantNullable))
			for index, wantNull := range testCase.wantNullable {
				assert.Equal(t, wantNull, scopedTable.Columns[index].Nullable,
					"column %q nullability mismatch", scopedTable.Columns[index].Name)
			}
		})
	}
}

func TestScopeChain_ResolveColumn_Qualified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setup         func() *ScopeChain
		tableAlias    string
		columnName    string
		wantColumn    string
		wantTable     string
		wantErrorCode string
	}{
		{
			name: "table exists and column found",
			setup: func() *ScopeChain {
				scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				table := catalogueTable("users",
					catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
					catalogueColumn("email", sqlmodel.TypeCategoryText, true),
				)
				scope.AddTable(ast.TableReference{Name: "users"}, ast.JoinInner, table)
				return scope
			},
			tableAlias: "users",
			columnName: "email",
			wantColumn: "email",
			wantTable:  "users",
		},
		{
			name: "table exists but column not found",
			setup: func() *ScopeChain {
				scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				table := catalogueTable("users",
					catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
				)
				scope.AddTable(ast.TableReference{Name: "users"}, ast.JoinInner, table)
				return scope
			},
			tableAlias:    "users",
			columnName:    "nonexistent",
			wantErrorCode: "Q001",
		},
		{
			name: "a CTE the FROM clause does not read",
			setup: func() *ScopeChain {
				scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				scope.AddCTE("recent_orders", []ast.ScopedColumn{
					scopedColumn("order_id", sqlmodel.TypeCategoryInteger, false),
				})
				return scope
			},
			tableAlias:    "recent_orders",
			columnName:    "order_id",
			wantErrorCode: "Q001",
		},
		{
			name: "parent scope traversal with subquery kind",
			setup: func() *ScopeChain {
				parent := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				table := catalogueTable("accounts",
					catalogueColumn("balance", sqlmodel.TypeCategoryDecimal, false),
				)
				parent.AddTable(ast.TableReference{Name: "accounts"}, ast.JoinInner, table)
				child := parent.CreateChildScope(ScopeKindSubquery)
				return child
			},
			tableAlias: "accounts",
			columnName: "balance",
			wantColumn: "balance",
			wantTable:  "accounts",
		},
		{
			name: "unknown table returns Q001",
			setup: func() *ScopeChain {
				return NewRootScopeChain(ScopeKindQuery, testScopeRules)
			},
			tableAlias:    "ghost",
			columnName:    "id",
			wantErrorCode: "Q001",
		},
		{
			name: "implicit rowid on qualified table",
			setup: func() *ScopeChain {
				scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				table := catalogueTable("items",
					catalogueColumn("name", sqlmodel.TypeCategoryText, false),
				)
				scope.AddTable(ast.TableReference{Name: "items"}, ast.JoinInner, table)
				return scope
			},
			tableAlias: "items",
			columnName: "rowid",
			wantColumn: "rowid",
			wantTable:  "items",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope := testCase.setup()
			column, table, err := scope.ResolveColumn(testCase.tableAlias, testCase.columnName)

			if testCase.wantErrorCode != "" {
				require.Error(t, err)
				assert.Equal(t, testCase.wantErrorCode, ExtractErrorCode(err))
				assert.NotContains(t, err.Error(), testCase.wantErrorCode)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, column)
			require.NotNil(t, table)
			assert.Equal(t, testCase.wantColumn, column.Name)
			assert.Equal(t, testCase.wantTable, table.Name)
		})
	}
}

func TestScopeChain_ResolveColumn_Unqualified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setup         func() *ScopeChain
		columnName    string
		wantColumn    string
		wantTable     string
		wantErrorCode string
	}{
		{
			name: "single match returns the column",
			setup: func() *ScopeChain {
				scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				table := catalogueTable("users",
					catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
					catalogueColumn("email", sqlmodel.TypeCategoryText, true),
				)
				scope.AddTable(ast.TableReference{Name: "users"}, ast.JoinInner, table)
				return scope
			},
			columnName: "email",
			wantColumn: "email",
			wantTable:  "users",
		},
		{
			name: "ambiguous column produces Q002",
			setup: func() *ScopeChain {
				scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				cat1 := catalogueTable("users",
					catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
				)
				cat2 := catalogueTable("orders",
					catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
				)
				scope.AddTable(ast.TableReference{Name: "users"}, ast.JoinInner, cat1)
				scope.AddTable(ast.TableReference{Name: "orders"}, ast.JoinInner, cat2)
				return scope
			},
			columnName:    "id",
			wantErrorCode: "Q002",
		},
		{
			name: "CTE match when not in tables",
			setup: func() *ScopeChain {
				scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				scope.AddCTE("totals", []ast.ScopedColumn{
					scopedColumn("sum_amount", sqlmodel.TypeCategoryDecimal, false),
				})
				return scope
			},
			columnName: "sum_amount",
			wantColumn: "sum_amount",
			wantTable:  "totals",
		},
		{
			name: "parent scope traversal",
			setup: func() *ScopeChain {
				parent := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				table := catalogueTable("config",
					catalogueColumn("key", sqlmodel.TypeCategoryText, false),
				)
				parent.AddTable(ast.TableReference{Name: "config"}, ast.JoinInner, table)
				child := parent.CreateChildScope(ScopeKindSubquery)
				return child
			},
			columnName: "key",
			wantColumn: "key",
			wantTable:  "config",
		},
		{
			name: "implicit rowid with single table",
			setup: func() *ScopeChain {
				scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				table := catalogueTable("items",
					catalogueColumn("name", sqlmodel.TypeCategoryText, false),
				)
				scope.AddTable(ast.TableReference{Name: "items"}, ast.JoinInner, table)
				return scope
			},
			columnName: "rowid",
			wantColumn: "rowid",
			wantTable:  "items",
		},
		{
			name: "implicit rowid ambiguous with multiple tables",
			setup: func() *ScopeChain {
				scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				cat1 := catalogueTable("t1",
					catalogueColumn("a", sqlmodel.TypeCategoryText, false),
				)
				cat2 := catalogueTable("t2",
					catalogueColumn("b", sqlmodel.TypeCategoryText, false),
				)
				scope.AddTable(ast.TableReference{Name: "t1"}, ast.JoinInner, cat1)
				scope.AddTable(ast.TableReference{Name: "t2"}, ast.JoinInner, cat2)
				return scope
			},
			columnName:    "rowid",
			wantErrorCode: "Q002",
		},
		{
			name: "implicit rowid not resolved for WithoutRowID table",
			setup: func() *ScopeChain {
				scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				table := &sqlmodel.Table{
					Name:           "strict_t",
					Columns:        []sqlmodel.Column{catalogueColumn("pk", sqlmodel.TypeCategoryInteger, false)},
					IsWithoutRowID: true,
				}
				scope.AddTable(ast.TableReference{Name: "strict_t"}, ast.JoinInner, table)
				return scope
			},
			columnName:    "rowid",
			wantErrorCode: "Q001",
		},
		{
			name: "column not found anywhere returns Q001",
			setup: func() *ScopeChain {
				scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
				table := catalogueTable("users",
					catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
				)
				scope.AddTable(ast.TableReference{Name: "users"}, ast.JoinInner, table)
				return scope
			},
			columnName:    "phantom",
			wantErrorCode: "Q001",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope := testCase.setup()
			column, table, err := scope.ResolveColumn("", testCase.columnName)

			if testCase.wantErrorCode != "" {
				require.Error(t, err)
				assert.Equal(t, testCase.wantErrorCode, ExtractErrorCode(err))
				assert.NotContains(t, err.Error(), testCase.wantErrorCode)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, column)
			require.NotNil(t, table)
			assert.Equal(t, testCase.wantColumn, column.Name)
			assert.Equal(t, testCase.wantTable, table.Name)
		})
	}
}

func TestScopeChain_CreateChildScope(t *testing.T) {
	t.Parallel()

	t.Run("parent is linked correctly and kind is set", func(t *testing.T) {
		t.Parallel()

		parent := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		child := parent.CreateChildScope(ScopeKindSubquery)

		assert.Equal(t, parent, child.parent, "child's parent should reference the parent scope")
		assert.Equal(t, ScopeKindSubquery, child.kind, "child kind should be ScopeKindSubquery")
		assert.NotNil(t, child.Tables, "tables map should be initialised")
		assert.NotNil(t, child.CTEs, "ctes map should be initialised")
	})

	t.Run("grandchild scope traverses two levels", func(t *testing.T) {
		t.Parallel()

		root := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		rootTable := catalogueTable("root_table",
			catalogueColumn("val", sqlmodel.TypeCategoryInteger, false),
		)
		root.AddTable(ast.TableReference{Name: "root_table"}, ast.JoinInner, rootTable)

		child := root.CreateChildScope(ScopeKindSubquery)
		grandchild := child.CreateChildScope(ScopeKindSubquery)

		column, table, err := grandchild.ResolveColumn("root_table", "val")
		require.NoError(t, err)
		assert.Equal(t, "val", column.Name)
		assert.Equal(t, "root_table", table.Name)
	})
}

func TestArrayWrappedSQLType_ClampsToMax(t *testing.T) {
	t.Parallel()

	wrapped := arrayWrappedSQLType(&sqlmodel.Column{
		Name:            "values",
		IsArray:         true,
		ArrayDimensions: 100,
		SQLType:         sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText},
	})

	depth := 0
	current := wrapped
	for current.Category == sqlmodel.TypeCategoryArray && current.ElementType != nil {
		depth++
		current = *current.ElementType
	}

	assert.Equal(t, sqlmodel.MaxArrayDimensions, depth, "array wraps must be clamped to maxArrayDimensions")
}

func TestArrayWrappedSQLType_RespectsNonArrayColumn(t *testing.T) {
	t.Parallel()

	column := sqlmodel.Column{
		Name:    "id",
		IsArray: false,
		SQLType: sqlmodel.SQLType{EngineName: "integer", Category: sqlmodel.TypeCategoryInteger},
	}

	assert.Equal(t, column.SQLType, arrayWrappedSQLType(&column))
}

func fullTextTable() *sqlmodel.Table {
	rank := catalogueColumn("rank", sqlmodel.TypeCategoryFloat, true)
	rank.Hidden = true
	tableNamed := catalogueColumn("docs", sqlmodel.TypeCategoryText, true)
	tableNamed.Hidden = true
	table := catalogueTable("docs",
		catalogueColumn("title", sqlmodel.TypeCategoryText, true),
		catalogueColumn("body", sqlmodel.TypeCategoryText, true),
		rank,
		tableNamed,
	)
	table.IsVirtual = true
	return table
}

func TestScopeChainHiddenColumnsAreNamedButNotStarred(t *testing.T) {
	t.Parallel()

	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	scope.AddTable(ast.TableReference{Name: "docs"}, ast.JoinInner, fullTextTable())

	require.Equal(t, []string{"title", "body"}, starNames(t, scope))

	qualified, problem := expandQualifiedStar("docs", scope)
	require.Nil(t, problem)
	qualifiedNames := make([]string, len(qualified))
	for index := range qualified {
		qualifiedNames[index] = qualified[index].Name
	}
	require.Equal(t, []string{"title", "body"}, qualifiedNames)

	for _, name := range []string{"rank", "docs"} {
		column, _, resolveError := scope.ResolveColumn("", name)
		require.NoError(t, resolveError, name)
		require.Equal(t, name, column.Name)

		column, _, resolveError = scope.ResolveColumn("docs", name)
		require.NoError(t, resolveError, name)
		require.Equal(t, name, column.Name)
	}
}

func TestScopeChainVirtualTablesKeepTheirRowID(t *testing.T) {
	t.Parallel()

	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	scope.AddTable(ast.TableReference{Name: "docs"}, ast.JoinInner, fullTextTable())

	column, _, err := scope.ResolveColumn("", "rowid")
	require.NoError(t, err)
	require.Equal(t, sqlmodel.TypeCategoryInteger, column.SQLType.Category)

	_, _, err = scope.ResolveColumn("docs", "rowid")
	require.NoError(t, err)
}

func TestScopeChainResolvesTheColumnsEveryTableHas(t *testing.T) {
	t.Parallel()

	rules := testScopeRules
	rules.SystemColumns = []string{"_part", "xmin"}
	declared := catalogueColumn("xmin", sqlmodel.TypeCategoryText, true)
	testCases := []struct {
		name         string
		tables       []*sqlmodel.Table
		qualifier    string
		column       string
		wantCode     string
		wantCategory sqlmodel.SQLTypeCategory
		wantNullable bool
	}{
		{
			name:         "a system column of the only table",
			tables:       []*sqlmodel.Table{catalogueTable("readings", catalogueColumn("id", sqlmodel.TypeCategoryInteger, false))},
			column:       "_PART",
			wantCategory: sqlmodel.TypeCategoryUnknown,
		},
		{
			name:         "a system column qualified by its table",
			tables:       []*sqlmodel.Table{catalogueTable("readings"), catalogueTable("stations")},
			qualifier:    "stations",
			column:       "_part",
			wantCategory: sqlmodel.TypeCategoryUnknown,
		},
		{
			name:     "a system column two tables have",
			tables:   []*sqlmodel.Table{catalogueTable("readings"), catalogueTable("stations")},
			column:   "_part",
			wantCode: "Q002",
		},
		{
			name:         "a declared column of a system column's name",
			tables:       []*sqlmodel.Table{catalogueTable("readings", declared)},
			column:       "xmin",
			wantCategory: sqlmodel.TypeCategoryText,
			wantNullable: true,
		},
		{
			name:     "a name no table has",
			tables:   []*sqlmodel.Table{catalogueTable("readings")},
			column:   "_parts",
			wantCode: "Q001",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope := NewRootScopeChain(ScopeKindQuery, rules)
			for _, table := range testCase.tables {
				scope.AddTable(ast.TableReference{Name: table.Name}, ast.JoinInner, table)
			}

			column, _, err := scope.ResolveColumn(testCase.qualifier, testCase.column)

			if testCase.wantCode != "" {
				require.Error(t, err)
				require.Equal(t, testCase.wantCode, ExtractErrorCode(err))
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.wantCategory, column.SQLType.Category)
			require.Equal(t, testCase.wantNullable, column.Nullable)
		})
	}
}

func TestScopeChainResolvesARelationNamedAsAValueAsItsRow(t *testing.T) {
	t.Parallel()

	readings := catalogueTable("readings", catalogueColumn("id", sqlmodel.TypeCategoryInteger, false), catalogueColumn("note", sqlmodel.TypeCategoryText, true))
	readings.Schema = "public"
	testCases := []struct {
		name       string
		build      func(scope *ScopeChain)
		value      string
		wantFound  bool
		wantEngine string
		wantFields []string
		nullable   bool
	}{
		{
			name: "a table read in the scope",
			build: func(scope *ScopeChain) {
				scope.AddTable(ast.TableReference{Name: "readings"}, ast.JoinInner, readings)
			},
			value:      "readings",
			wantFound:  true,
			wantEngine: "readings",
			wantFields: []string{"id", "note"},
		},
		{
			name: "a table by its alias",
			build: func(scope *ScopeChain) {
				scope.AddTable(ast.TableReference{Name: "readings", Alias: "r"}, ast.JoinInner, readings)
			},
			value:      "R",
			wantFound:  true,
			wantEngine: "readings",
			wantFields: []string{"id", "note"},
		},
		{
			name: "a table on the nullable side of an outer join",
			build: func(scope *ScopeChain) {
				scope.AddTable(ast.TableReference{Name: "stations"}, ast.JoinInner, catalogueTable("stations"))
				scope.AddTable(ast.TableReference{Name: "readings"}, ast.JoinLeft, readings)
			},
			value:      "readings",
			wantFound:  true,
			wantEngine: "readings",
			wantFields: []string{"id", "note"},
			nullable:   true,
		},
		{
			name: "a derived table has no name of its own",
			build: func(scope *ScopeChain) {
				scope.AddDerivedTable(DerivedTableReference{Alias: "d", Columns: []ast.ScopedColumn{scopedColumn("total", sqlmodel.TypeCategoryInteger, false)}, JoinKind: ast.JoinInner})
			},
			value:      "d",
			wantFound:  true,
			wantEngine: "record",
			wantFields: []string{"total"},
		},
		{
			name: "a name no relation has",
			build: func(scope *ScopeChain) {
				scope.AddTable(ast.TableReference{Name: "readings"}, ast.JoinInner, readings)
			},
			value: "stations",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			rules := testScopeRules
			rules.RowValues = rowValueTyper{}
			scope := NewRootScopeChain(ScopeKindQuery, rules)
			testCase.build(scope)

			row, _, found := scope.ResolveRowValue(testCase.value)

			require.Equal(t, testCase.wantFound, found)
			if !found {
				return
			}
			require.Equal(t, testCase.wantEngine, row.SQLType.EngineName)
			var fields []string
			for _, field := range row.SQLType.StructFields {
				fields = append(fields, field.Name)
			}
			require.Equal(t, testCase.wantFields, fields)
			require.Equal(t, testCase.nullable, row.Nullable)
		})
	}
}

func TestScopeChainResolvesARowOfAnEnclosingScope(t *testing.T) {
	t.Parallel()

	rules := testScopeRules
	rules.RowValues = rowValueTyper{}
	outer := NewRootScopeChain(ScopeKindQuery, rules)
	outer.AddTable(ast.TableReference{Name: "readings"}, ast.JoinInner, catalogueTable("readings"))
	inner := outer.CreateChildScope(ScopeKindSubquery)

	row, table, found := inner.ResolveRowValue("readings")

	require.True(t, found)
	require.Equal(t, "readings", table.Alias)
	require.Equal(t, "readings", row.Name)
}

func TestScopeChainHasNoRowValuesWhereTheDialectHasNone(t *testing.T) {
	t.Parallel()

	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	scope.AddTable(ast.TableReference{Name: "readings"}, ast.JoinInner, catalogueTable("readings"))

	_, _, found := scope.ResolveRowValue("readings")

	require.False(t, found)
}

func manyRelationsQuery(t *testing.T, count int) func() {
	t.Helper()

	aliases := make([]string, 0, count)
	tables := make([]*sqlmodel.Table, 0, count)
	for index := range count {
		alias := fmt.Sprintf("r%d", index)
		aliases = append(aliases, alias)
		tables = append(tables, &sqlmodel.Table{Name: alias, Columns: []sqlmodel.Column{
			{Name: "c" + alias, SQLType: sqlmodel.SQLType{EngineName: "int4", Category: sqlmodel.TypeCategoryInteger}},
		}})
	}
	return func() {
		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		scope.SetFromOrder(aliases)
		for index, alias := range aliases {
			joinKind := ast.JoinInner
			if index == len(aliases)-1 {
				joinKind = ast.JoinRight
			}
			scope.AddTable(ast.TableReference{Name: alias}, joinKind, tables[index])
		}
		for _, alias := range aliases {
			_, _, err := scope.ResolveColumn("", "c"+alias)
			require.NoError(t, err)
		}
	}
}

func TestRegisteringAndResolvingManyRelationsTakesLinearTime(t *testing.T) {
	t.Parallel()

	scaling.Linear(t, manyRelationsQuery(t, 200), manyRelationsQuery(t, 800))
}

func TestRelationsKeepFromOrderAcrossRegistrations(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		fromOrder  []string
		registered []string
		want       []string
	}{
		{name: "registration order without a recorded order", fromOrder: nil, registered: []string{"b", "a", "c"},
			want: []string{"b", "a", "c"}},
		{name: "the recorded order over registration order", fromOrder: []string{"a", "b", "c"}, registered: []string{"c", "a", "b"},
			want: []string{"a", "b", "c"}},
		{name: "unlisted relations after listed ones", fromOrder: []string{"b"}, registered: []string{"c", "b", "a"},
			want: []string{"b", "c", "a"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			scope.SetFromOrder(testCase.fromOrder)
			for _, alias := range testCase.registered {
				scope.AddTable(ast.TableReference{Name: alias}, ast.JoinInner, &sqlmodel.Table{Name: alias})
				require.Len(t, scope.OrderedTables(), len(scope.Tables), "the order is kept up to date as relations are registered")
			}

			ordered := scope.OrderedTables()
			got := make([]string, 0, len(ordered))
			for _, table := range ordered {
				got = append(got, table.Alias)
			}
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestScopeChainLooksUpThroughAsManyScopesAsQueriesNest(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		wantErr error
		name    string
		depth   int
	}{
		{name: "a query nested to the limit", depth: MaxExpressionResolveDepth, wantErr: nil},
		{name: "a query nested past the limit", depth: MaxExpressionResolveDepth + 10, wantErr: ErrScopeChainDepthExceeded},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			root.AddTable(ast.TableReference{Name: "users"}, ast.JoinInner, catalogueTable("users",
				catalogueColumn("email", sqlmodel.TypeCategoryText, false)))
			scope := root
			for range testCase.depth {
				scope = scope.CreateChildScope(ScopeKindSubquery)
			}
			require.Equal(t, testCase.depth, scope.Depth())

			for _, qualifier := range []string{"", "users"} {
				column, _, err := scope.ResolveColumn(qualifier, "email")
				if testCase.wantErr != nil {
					require.ErrorIs(t, err, testCase.wantErr)
					continue
				}
				require.NoError(t, err)
				require.Equal(t, "email", column.Name)
			}
		})
	}
}

func TestOnlyASubqueryScopeSeesTheNamesOfTheQueryItIsIn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		kind     ScopeKind
		wantSeen bool
	}{
		{name: "a subquery, as a correlated subquery or a LATERAL join is", kind: ScopeKindSubquery, wantSeen: true},
		{name: "a CTE body", kind: ScopeKindCTE, wantSeen: false},
		{name: "a compound branch", kind: ScopeKindQuery, wantSeen: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parent := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			parent.AddTable(ast.TableReference{Name: "accounts", Alias: "a"}, ast.JoinInner, catalogueTable("accounts",
				catalogueColumn("balance", sqlmodel.TypeCategoryDecimal, false)))
			child := parent.CreateChildScope(testCase.kind)

			for _, qualifier := range []string{"", "a"} {
				_, table, err := child.ResolveColumn(qualifier, "balance")
				if !testCase.wantSeen {
					require.Error(t, err, qualifier)
					continue
				}
				require.NoError(t, err, qualifier)
				require.Equal(t, "accounts", table.Name)
			}
			_, _, isRow := child.ResolveRowValue("a")
			require.False(t, isRow, "the test rules type no row values")
		})
	}
}

func TestUnknownNameSuggestionsStopAtTheChainsBudget(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		lookups         int
		wantSuggestions int
	}{
		{name: "fewer unknown names than the budget", lookups: 10, wantSuggestions: 10},
		{name: "more unknown names than the budget", lookups: maxSuggestionsPerChain + 50, wantSuggestions: maxSuggestionsPerChain},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			root.AddTable(ast.TableReference{Name: "people"}, ast.JoinInner, catalogueTable("people",
				catalogueColumn("surname", sqlmodel.TypeCategoryText, false)))
			child := root.CreateChildScope(ScopeKindSubquery)

			suggestions := 0
			for index := range testCase.lookups {
				scope := root
				if index%2 == 1 {
					scope = child
				}
				_, _, err := scope.ResolveColumn("", "surnam")
				var problem *Error
				require.ErrorAs(t, err, &problem)
				if problem.Suggestion == "surname" {
					suggestions++
				}
			}
			require.Equal(t, testCase.wantSuggestions, suggestions, "every scope of the chain shares the root's budget")
		})
	}
}

func TestUnknownNamesInAWideRelationAreReportedInLinearTime(t *testing.T) {
	t.Parallel()

	lookUp := func(size int) func() {
		columns := make([]sqlmodel.Column, size)
		for index := range columns {
			columns[index] = catalogueColumn(fmt.Sprintf("column_%06d", index), sqlmodel.TypeCategoryText, false)
		}
		table := catalogueTable("wide", columns...)
		return func() {
			scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			scope.AddTable(ast.TableReference{Name: "wide"}, ast.JoinInner, table)
			for index := range size {
				_, _, err := scope.ResolveColumn("", fmt.Sprintf("q%d", index))
				require.Error(t, err)
			}
		}
	}

	scaling.Linear(t, lookUp(1_200), lookUp(4_800))
}

func TestScopeChainDepthIsReportedAsADepthLimit(t *testing.T) {
	t.Parallel()

	scope := setupTypeResolverScope()
	for range maxScopeChainDepth + 2 {
		scope = scope.CreateChildScope(ScopeKindSubquery)
	}
	_, _, err := scope.ResolveColumn("", "name")
	require.ErrorIs(t, err, ErrScopeChainDepthExceeded)
	assert.Equal(t, diag.CodeDepthLimit, ExtractErrorCode(err))
}
