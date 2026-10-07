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
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

type registeredRelation struct {
	joinKind ast.JoinKind
	alias    string
	kind     string
}

func joinTestScope(placement ast.JoinColumnPlacement, joinKind ast.JoinKind, using *ast.JoinUsing) *ScopeChain {
	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	scope.AddTable(ast.TableReference{Name: "a"}, ast.JoinInner, catalogueTable("a",
		catalogueColumn("name", sqlmodel.TypeCategoryText, false),
		catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
		catalogueColumn("code", sqlmodel.TypeCategoryText, false),
	))
	scope.AddTable(ast.TableReference{Name: "b"}, joinKind, catalogueTable("b",
		catalogueColumn("extra", sqlmodel.TypeCategoryText, false),
		catalogueColumn("code", sqlmodel.TypeCategoryText, false),
		catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
	))
	if using != nil {
		using.Placement = placement
		scope.joinUsing("b", using)
	}
	return scope
}

func starNames(t *testing.T, scope *ScopeChain) []string {
	t.Helper()
	columns := scope.StarColumns()
	names := make([]string, len(columns))
	for index := range columns {
		names[index] = columns[index].Column.Name
	}
	return names
}

func TestScopeChainUsingMergesColumns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		using     *ast.JoinUsing
		placement ast.JoinColumnPlacement
		want      []string
	}{
		{
			name:      "using leads",
			using:     &ast.JoinUsing{Columns: []string{"id"}},
			placement: ast.JoinColumnsLead,
			want:      []string{"id", "name", "code", "extra", "code"},
		},
		{
			name:      "using keeps the left place",
			using:     &ast.JoinUsing{Columns: []string{"id"}},
			placement: ast.JoinColumnsInPlace,
			want:      []string{"name", "id", "code", "extra", "code"},
		},
		{
			name:      "using several columns leads in list order",
			using:     &ast.JoinUsing{Columns: []string{"code", "id"}},
			placement: ast.JoinColumnsLead,
			want:      []string{"code", "id", "name", "extra"},
		},
		{
			name:      "natural leads in left order",
			using:     &ast.JoinUsing{Natural: true},
			placement: ast.JoinColumnsLead,
			want:      []string{"id", "code", "name", "extra"},
		},
		{
			name:      "natural keeps the left place",
			using:     &ast.JoinUsing{Natural: true},
			placement: ast.JoinColumnsInPlace,
			want:      []string{"name", "id", "code", "extra"},
		},
		{
			name:      "an on join merges nothing",
			using:     nil,
			placement: ast.JoinColumnsLead,
			want:      []string{"name", "id", "code", "extra", "code", "id"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope := joinTestScope(testCase.placement, ast.JoinInner, testCase.using)
			require.Equal(t, testCase.want, starNames(t, scope))
		})
	}
}

func TestScopeChainUsingColumnResolvesUnqualified(t *testing.T) {
	t.Parallel()

	scope := joinTestScope(ast.JoinColumnsLead, ast.JoinInner, &ast.JoinUsing{Columns: []string{"id"}})

	column, _, err := scope.ResolveColumn("", "id")
	require.NoError(t, err)
	require.Equal(t, "id", column.Name)

	_, _, err = scope.ResolveColumn("", "code")
	require.Error(t, err)

	qualified, table, err := scope.ResolveColumn("b", "id")
	require.NoError(t, err)
	require.Equal(t, "id", qualified.Name)
	require.Equal(t, "b", table.Alias)
}

func TestScopeChainUsingColumnNullability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		joinKind     ast.JoinKind
		wantNullable bool
		wantSource   string
	}{
		{name: "inner join takes the left column", joinKind: ast.JoinInner, wantNullable: false, wantSource: "a"},
		{name: "left join keeps the left column", joinKind: ast.JoinLeft, wantNullable: false, wantSource: "a"},
		{name: "right join takes the right column", joinKind: ast.JoinRight, wantNullable: false, wantSource: "b"},
		{name: "full join coalesces both nullable sides", joinKind: ast.JoinFull, wantNullable: true, wantSource: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scope := joinTestScope(ast.JoinColumnsLead, testCase.joinKind, &ast.JoinUsing{Columns: []string{"id"}})

			column, table, err := scope.ResolveColumn("", "id")
			require.NoError(t, err)
			require.Equal(t, testCase.wantNullable, column.Nullable)
			source := ""
			if table != nil {
				source = table.Alias
			}
			require.Equal(t, testCase.wantSource, source)

			star := scope.StarColumns()
			require.Equal(t, "id", star[0].Column.Name)
			require.Equal(t, testCase.wantNullable, star[0].Column.Nullable)
		})
	}
}

func TestScopeChainUsingChainMergesOnce(t *testing.T) {
	t.Parallel()

	scope := joinTestScope(ast.JoinColumnsLead, ast.JoinInner, &ast.JoinUsing{Columns: []string{"id"}})
	scope.AddTable(ast.TableReference{Name: "c"}, ast.JoinInner, catalogueTable("c",
		catalogueColumn("id", sqlmodel.TypeCategoryInteger, false),
		catalogueColumn("note", sqlmodel.TypeCategoryText, true),
	))
	scope.joinUsing("c", &ast.JoinUsing{Columns: []string{"id"}, Placement: ast.JoinColumnsLead})

	require.Equal(t, []string{"id", "name", "code", "extra", "code", "note"}, starNames(t, scope))
}

func TestScopeChainStarFollowsRecordedFromOrder(t *testing.T) {
	t.Parallel()

	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	scope.AddTable(ast.TableReference{Name: "t"}, ast.JoinInner, catalogueTable("t",
		catalogueColumn("t_id", sqlmodel.TypeCategoryInteger, false),
	))
	scope.AddDerivedTable(DerivedTableReference{
		Alias:    "d",
		Columns:  []ast.ScopedColumn{scopedColumn("d_id", sqlmodel.TypeCategoryInteger, false)},
		JoinKind: ast.JoinInner,
		Source:   DerivedSourceSubquery,
		Using:    nil,
	})
	scope.SetFromOrder([]string{"d", "t"})

	require.Equal(t, []string{"d_id", "t_id"}, starNames(t, scope))
}

func TestScopeChainStarReferableNamesOnlyColumnsTheCatalogueHolds(t *testing.T) {
	t.Parallel()

	derived := func() *ScopeChain {
		scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
		scope.AddTable(ast.TableReference{Name: "t"}, ast.JoinInner, catalogueTable("t",
			catalogueColumn("t_id", sqlmodel.TypeCategoryInteger, false),
		))
		scope.AddDerivedTable(DerivedTableReference{
			Alias:    "d",
			Columns:  []ast.ScopedColumn{scopedColumn("count", sqlmodel.TypeCategoryInteger, false)},
			JoinKind: ast.JoinInner,
			Source:   DerivedSourceSubquery,
			Using:    nil,
		})
		scope.SetFromOrder([]string{"t", "d"})
		return scope
	}
	testCases := []struct {
		name      string
		scope     *ScopeChain
		qualifier string
		want      []bool
	}{
		{name: "an unqualified star over a table and a derived table", scope: derived(), qualifier: "", want: []bool{true, false}},
		{name: "a star qualified by a table", scope: derived(), qualifier: "t", want: []bool{true}},
		{name: "a star qualified by a derived table", scope: derived(), qualifier: "d", want: []bool{false}},
		{name: "a star qualified by nothing in scope", scope: derived(), qualifier: "missing", want: nil},
		{
			name:  "a FULL join merging a column",
			scope: joinTestScope(ast.JoinColumnsLead, ast.JoinFull, &ast.JoinUsing{Columns: []string{"id"}}),
			want:  []bool{true, true, true, true, true},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, testCase.scope.StarReferable(testCase.qualifier))
		})
	}
}

func registerRelation(scope *ScopeChain, relation registeredRelation) {
	columns := []ast.ScopedColumn{scopedColumn("id", sqlmodel.TypeCategoryInteger, false)}
	switch relation.kind {
	case "cte":
		scope.AddCTEAsTable(relation.alias, columns, relation.joinKind)
	case "derived":
		scope.AddDerivedTable(DerivedTableReference{Alias: relation.alias, Columns: columns, JoinKind: relation.joinKind, Source: DerivedSourceSubquery})
	default:
		scope.AddTable(ast.TableReference{Name: relation.alias}, relation.joinKind, catalogueTable(relation.alias,
			catalogueColumn("id", sqlmodel.TypeCategoryInteger, false)))
	}
}

func TestRelationsBeforeAJoinThatMayLeaveThemWithoutARowAreNullable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		fromOrder    []string
		registered   []registeredRelation
		wantNullable map[string]bool
	}{
		{
			name:      "a CTE on the left of a RIGHT join",
			fromOrder: []string{"x", "y"},
			registered: []registeredRelation{
				{alias: "x", kind: "cte", joinKind: ast.JoinInner},
				{alias: "y", kind: "cte", joinKind: ast.JoinRight},
			},
			wantNullable: map[string]bool{"x": true, "y": false},
		},
		{
			name:      "both CTEs of a FULL join",
			fromOrder: []string{"x", "y"},
			registered: []registeredRelation{
				{alias: "x", kind: "cte", joinKind: ast.JoinInner},
				{alias: "y", kind: "cte", joinKind: ast.JoinFull},
			},
			wantNullable: map[string]bool{"x": true, "y": true},
		},
		{
			name:      "a derived table registered after the RIGHT join it comes before",
			fromOrder: []string{"d", "t"},
			registered: []registeredRelation{
				{alias: "t", kind: "table", joinKind: ast.JoinRight},
				{alias: "d", kind: "derived", joinKind: ast.JoinInner},
			},
			wantNullable: map[string]bool{"d": true, "t": false},
		},
		{
			name:      "a relation joined after the RIGHT join keeps its nullability",
			fromOrder: []string{"a", "d", "b"},
			registered: []registeredRelation{
				{alias: "a", kind: "table", joinKind: ast.JoinInner},
				{alias: "b", kind: "table", joinKind: ast.JoinInner},
				{alias: "d", kind: "derived", joinKind: ast.JoinRight},
			},
			wantNullable: map[string]bool{"a": true, "d": false, "b": false},
		},
		{
			name:      "a table on the left of a RIGHT join without a recorded order",
			fromOrder: nil,
			registered: []registeredRelation{
				{alias: "a", kind: "table", joinKind: ast.JoinInner},
				{alias: "b", kind: "table", joinKind: ast.JoinRight},
			},
			wantNullable: map[string]bool{"a": true, "b": false},
		},
		{
			name:      "a table registered after a RIGHT join without a recorded order",
			fromOrder: nil,
			registered: []registeredRelation{
				{alias: "a", kind: "table", joinKind: ast.JoinInner},
				{alias: "b", kind: "table", joinKind: ast.JoinRight},
				{alias: "c", kind: "table", joinKind: ast.JoinInner},
			},
			wantNullable: map[string]bool{"a": true, "b": false, "c": false},
		},
		{
			name:      "relations registered on both sides of two RIGHT joins out of FROM order",
			fromOrder: []string{"a", "b", "c", "d", "e"},
			registered: []registeredRelation{
				{alias: "d", kind: "table", joinKind: ast.JoinRight},
				{alias: "e", kind: "table", joinKind: ast.JoinInner},
				{alias: "a", kind: "derived", joinKind: ast.JoinInner},
				{alias: "b", kind: "table", joinKind: ast.JoinRight},
				{alias: "c", kind: "derived", joinKind: ast.JoinInner},
			},
			wantNullable: map[string]bool{"a": true, "b": true, "c": true, "d": false, "e": false},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			scope.SetFromOrder(test.fromOrder)
			for _, relation := range test.registered {
				registerRelation(scope, relation)
			}

			for alias, want := range test.wantNullable {
				table, exists := scope.Table(alias)
				require.True(t, exists, alias)
				require.Equal(t, want, table.Columns[0].Nullable, alias)
			}
		})
	}
}

func TestNullExtendingARelationLeavesTheColumnsItSharesAlone(t *testing.T) {
	t.Parallel()

	shared := []ast.ScopedColumn{scopedColumn("id", sqlmodel.TypeCategoryInteger, false)}
	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	scope.SetFromOrder([]string{"d", "t"})
	scope.AddDerivedTable(DerivedTableReference{Alias: "d", Columns: shared, JoinKind: ast.JoinInner, Source: DerivedSourceSubquery})
	registerRelation(scope, registeredRelation{alias: "t", kind: "table", joinKind: ast.JoinRight})

	table, exists := scope.Table("d")
	require.True(t, exists)
	require.True(t, table.Columns[0].Nullable)
	require.False(t, shared[0].Nullable)
}
