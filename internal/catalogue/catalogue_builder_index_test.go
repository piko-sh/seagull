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

package catalogue

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestCreateIndexOwnsKeysAndPreservesExistingDefinition(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilderWithTable("items", sqlmodel.Column{Name: "id"}, sqlmodel.Column{Name: "category"})
	mutation := ast.NewCatalogueMutation(ast.MutationCreateIndex, "public", "items", ast.WithIndexDefinition("item_key", []string{"category", "id"}, true))
	mutation.Origin = sqlmodel.MigrationOrigin{Filename: "001_items.up.sql", Statement: 2}
	require.NoError(t, builder.applyCreateIndex(mutation))
	table := builder.Catalogue().LookupTable("public", "items")
	require.Len(t, table.Indexes, 1)
	require.Equal(t, mutation.Origin, table.Indexes[0].Origin)
	mutation.Index.Columns[0] = "id"
	require.Equal(t, []string{"category", "id"}, table.Indexes[0].Columns)
	repeated := ast.NewCatalogueMutation(ast.MutationCreateIndex, "public", "items", ast.WithIndexDefinition("item_key", []string{"id"}, false), ast.WithIfNotExists(true))
	require.NoError(t, builder.applyCreateIndex(repeated))
	require.Len(t, table.Indexes, 1)
	require.Equal(t, []string{"category", "id"}, table.Indexes[0].Columns)
	require.True(t, table.Indexes[0].IsUnique)
}

func TestDropIndexRespectsSchemaSearchOrderAndTableOwnership(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		schemaName string
		tableName  string
		wantFirst  []sqlmodel.Index
		wantSecond int
	}{
		{
			name:       "unqualified",
			wantFirst:  []sqlmodel.Index{{Name: "keep_key", Columns: []string{"id"}}},
			wantSecond: 1,
		},
		{
			name:       "schema",
			schemaName: "later",
			wantFirst:  []sqlmodel.Index{{Name: "shared_key", Columns: []string{"id"}}, {Name: "keep_key", Columns: []string{"id"}}},
			wantSecond: 0,
		},
		{
			name:       "table",
			schemaName: "later",
			tableName:  "other",
			wantFirst:  []sqlmodel.Index{{Name: "shared_key", Columns: []string{"id"}}, {Name: "keep_key", Columns: []string{"id"}}},
			wantSecond: 0,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilderWithTable("items", sqlmodel.Column{Name: "id"})
			first := builder.Catalogue().LookupTable("public", "items")
			first.Indexes = []sqlmodel.Index{{Name: "shared_key", Columns: []string{"id"}}, {Name: "keep_key", Columns: []string{"id"}}}
			second := &sqlmodel.Table{Name: "other", Schema: "later", Indexes: []sqlmodel.Index{{Name: "shared_key", Columns: []string{"id"}}}}
			builder.catalogue.Schemas["later"] = &sqlmodel.Schema{Name: "later", Tables: map[string]*sqlmodel.Table{"other": second}}
			builder.catalogue.SearchPath = []string{"public", "later"}
			mutation := ast.NewCatalogueMutation(ast.MutationDropIndex, testCase.schemaName, testCase.tableName, ast.WithNewName("shared_key"))

			require.NoError(t, builder.applyDropIndex(mutation))

			require.Equal(t, testCase.wantFirst, first.Indexes)
			require.Len(t, second.Indexes, testCase.wantSecond)
		})
	}
}

func TestDroppingAColumnFollowsTheIndexPolicy(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		policy      ast.ColumnIndexPolicy
		indexes     []sqlmodel.Index
		wantErr     bool
		wantIndexes []sqlmodel.Index
	}{
		{
			name:        "restrict refuses a drop an index depends on",
			policy:      ast.ColumnIndexesRestrict,
			indexes:     []sqlmodel.Index{{Name: "category_key", Columns: []string{"category"}}},
			wantErr:     true,
			wantIndexes: []sqlmodel.Index{{Name: "category_key", Columns: []string{"category"}}},
		},
		{
			name:        "trim keeps the other columns of an index",
			policy:      ast.ColumnIndexesTrim,
			indexes:     []sqlmodel.Index{{Name: "pair_key", Columns: []string{"category", "id"}}, {Name: "category_key", Columns: []string{"category"}}},
			wantIndexes: []sqlmodel.Index{{Name: "pair_key", Columns: []string{"id"}}},
		},
		{
			name:        "preserve unique refuses to narrow a unique index",
			policy:      ast.ColumnIndexesPreserveUnique,
			indexes:     []sqlmodel.Index{{Name: "pair_key", Columns: []string{"category", "id"}, IsUnique: true}},
			wantErr:     true,
			wantIndexes: []sqlmodel.Index{{Name: "pair_key", Columns: []string{"category", "id"}, IsUnique: true}},
		},
		{
			name:        "preserve unique drops a unique index on the column alone",
			policy:      ast.ColumnIndexesPreserveUnique,
			indexes:     []sqlmodel.Index{{Name: "category_key", Columns: []string{"category"}, IsUnique: true}},
			wantIndexes: []sqlmodel.Index{},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilderWithTable("items", sqlmodel.Column{Name: "id"}, sqlmodel.Column{Name: "category"})
			table := builder.Catalogue().LookupTable("public", "items")
			table.Indexes = testCase.indexes
			shared := slices.Clone(testCase.indexes[0].Columns)
			mutation := ast.NewCatalogueMutation(ast.MutationAlterTableDropColumn, "public", "items",
				ast.WithColumnName("category"), ast.WithDroppedColumnIndexes(testCase.policy))

			err := builder.applyAlterTableDropColumn(mutation)

			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.wantIndexes, table.Indexes)
			require.Equal(t, shared, testCase.indexes[0].Columns, "the index columns the table held before are not changed in place")
		})
	}
}

func TestColumnRenameUpdatesInheritedIndexKeys(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilderWithTable("items", sqlmodel.Column{Name: "id"})
	parent := builder.Catalogue().LookupTable("public", "items")
	parent.Indexes = []sqlmodel.Index{{Name: "parent_key", Columns: []string{"id"}}}
	child := &sqlmodel.Table{Name: "child", Schema: "public", Columns: []sqlmodel.Column{{Name: "id"}}, Indexes: []sqlmodel.Index{{Name: "child_key", Columns: []string{"id"}}}}
	builder.catalogue.Schemas["public"].Tables["child"] = child
	builder.recordInheritance(child, []*sqlmodel.Table{parent}, map[string]struct{}{})
	mutation := ast.NewCatalogueMutation(ast.MutationAlterTableRenameColumn, "public", "items", ast.WithColumnName("id"), ast.WithNewName("renamed"))
	require.NoError(t, builder.applyAlterTableRenameColumn(mutation))
	require.Equal(t, []string{"renamed"}, parent.Indexes[0].Columns)
	require.Equal(t, []string{"renamed"}, child.Indexes[0].Columns)
}

func TestColumnDropRemovesInheritedIndexesWithoutTouchingIndependentKeys(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilderWithTable("items", sqlmodel.Column{Name: "id"}, sqlmodel.Column{Name: "category"})
	parent := builder.Catalogue().LookupTable("public", "items")
	parent.Indexes = []sqlmodel.Index{{Name: "parent_key", Columns: []string{"category", "id"}}}
	child := &sqlmodel.Table{Name: "child", Schema: "public", Columns: []sqlmodel.Column{{Name: "id"}, {Name: "category"}}, Indexes: []sqlmodel.Index{{Name: "child_key", Columns: []string{"category"}}, {Name: "independent_key", Columns: []string{"id"}}}}
	builder.catalogue.Schemas["public"].Tables["child"] = child
	builder.recordInheritance(child, []*sqlmodel.Table{parent}, map[string]struct{}{})
	mutation := ast.NewCatalogueMutation(ast.MutationAlterTableDropColumn, "public", "items", ast.WithColumnName("category"), ast.WithDroppedColumnIndexes(ast.ColumnIndexesDrop))
	require.NoError(t, builder.applyAlterTableDropColumn(mutation))
	require.Empty(t, parent.Indexes)
	require.Equal(t, []sqlmodel.Index{{Name: "independent_key", Columns: []string{"id"}}}, child.Indexes)
}

func TestIndexNamesRemainIndependentAcrossSchemas(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilderWithTable("items", sqlmodel.Column{Name: "id"})
	parent := builder.Catalogue().LookupTable("public", "items")
	parent.Indexes = []sqlmodel.Index{{Name: "shared_key", Columns: []string{"id"}}}
	other := &sqlmodel.Table{Name: "other", Schema: "later", Columns: []sqlmodel.Column{{Name: "id"}}}
	builder.catalogue.Schemas["later"] = &sqlmodel.Schema{Name: "later", Tables: map[string]*sqlmodel.Table{"other": other}}
	mutation := ast.NewCatalogueMutation(ast.MutationCreateIndex, "later", "other", ast.WithIndexDefinition("shared_key", []string{"id"}, true), ast.WithIfNotExists(true))
	require.NoError(t, builder.applyCreateIndex(mutation))
	require.Len(t, other.Indexes, 1)
	require.True(t, other.Indexes[0].IsUnique)
	require.False(t, parent.Indexes[0].IsUnique)
}

func TestCreateIndexIfNotExistsSharesNamesWithRelations(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilderWithTable("items", sqlmodel.Column{Name: "id"})
	table := builder.Catalogue().LookupTable("public", "items")
	mutation := ast.NewCatalogueMutation(ast.MutationCreateIndex, "public", "items",
		ast.WithIndexDefinition("items", []string{"id"}, false), ast.WithIfNotExists(true))
	mutation.IndexScope = ast.IndexNamesInRelations

	require.NoError(t, builder.applyCreateIndex(mutation))

	require.Empty(t, table.Indexes, "a table of the index's name makes IF NOT EXISTS skip it")
}

func TestDroppingAnIndexDropsTheUniqueConstraintItBacks(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilderWithTable("items", sqlmodel.Column{Name: "id"})
	table := builder.Catalogue().LookupTable("public", "items")
	table.Constraints = []sqlmodel.Constraint{{Name: "items_id_key", Kind: sqlmodel.ConstraintUnique, Columns: []string{"id"}}}

	require.NoError(t, builder.applyDropIndex(ast.NewCatalogueMutation(ast.MutationDropIndex, "", "", ast.WithNewName("items_id_key"))))

	require.Empty(t, table.Constraints)
}
