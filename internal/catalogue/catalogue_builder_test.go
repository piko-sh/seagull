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
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/internal/testkit/scaling"
)

type mockExtensionLoaderEngine struct {
	fakedialect.Engine
	loadExtensionFunctionsHook func(name string) []*sqlmodel.Function
}

func (m *mockExtensionLoaderEngine) LoadExtensionFunctions(name string) []*sqlmodel.Function {
	if m.loadExtensionFunctionsHook != nil {
		return m.loadExtensionFunctionsHook(name)
	}
	return nil
}

func setupBuilder() (*catalogueBuilder, *fakedialect.Engine) {
	engine := &fakedialect.Engine{
		SchemaSearchPathFunc: func() []string { return []string{"public"} },
	}
	builder := newCatalogueBuilder(engine, namespace.Default())
	return builder, engine
}

func applyMigration(t *testing.T, builder *catalogueBuilder, filename, migration string, index int) []diag.SourceError {
	t.Helper()

	diagnostics, err := builder.ApplyMigration(t.Context(), filename, []byte(migration), index)
	require.NoError(t, err)
	return diagnostics
}

func setupBuilderWithTable(tableName string, columns ...sqlmodel.Column) (*catalogueBuilder, *fakedialect.Engine) {
	builder, engine := setupBuilder()
	builder.catalogue.Schemas["public"].Tables[tableName] = &sqlmodel.Table{
		Name:    tableName,
		Schema:  "public",
		Columns: columns,
	}
	return builder, engine
}

func TestNewCatalogueBuilder(t *testing.T) {
	t.Parallel()

	t.Run("creates default schema when non-empty", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		catalogue := builder.Catalogue()

		assert.Equal(t, "public", catalogue.DefaultSchema)
		require.Contains(t, catalogue.Schemas, "public")
		assert.Equal(t, "public", catalogue.Schemas["public"].Name)
		assert.NotNil(t, catalogue.Schemas["public"].Tables)
		assert.NotNil(t, catalogue.Schemas["public"].Views)
		assert.NotNil(t, catalogue.Schemas["public"].Enums)
		assert.NotNil(t, catalogue.Schemas["public"].Functions)
		assert.NotNil(t, catalogue.Schemas["public"].CompositeTypes)
		assert.NotNil(t, catalogue.Schemas["public"].Sequences)
		assert.NotNil(t, catalogue.Extensions)
	})

	t.Run("empty default schema creates no initial schema entry", func(t *testing.T) {
		t.Parallel()

		engine := &fakedialect.Engine{
			SchemaSearchPathFunc: func() []string { return []string{""} },
		}
		builder := newCatalogueBuilder(engine, namespace.Default())
		catalogue := builder.Catalogue()

		assert.Equal(t, "", catalogue.DefaultSchema)
		assert.Empty(t, catalogue.Schemas)
	})
}

func TestCatalogueBuilder_ApplyCreateTable(t *testing.T) {
	t.Parallel()

	t.Run("simple table with columns", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateTable,
			SchemaName: "public",
			TableName:  "users",
			Columns: []sqlmodel.Column{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "integer"}},
				{Name: "email", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}, Nullable: true},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["users"]
		require.NotNil(t, table)
		assert.Equal(t, "users", table.Name)
		assert.Equal(t, "public", table.Schema)
		require.Len(t, table.Columns, 2)
		assert.Equal(t, "id", table.Columns[0].Name)
		assert.Equal(t, "email", table.Columns[1].Name)
		assert.True(t, table.Columns[1].Nullable)
	})

	t.Run("duplicate table returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateTable,
			SchemaName: "public",
			TableName:  "users",
			Columns: []sqlmodel.Column{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("table with primary key", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateTable,
			SchemaName: "public",
			TableName:  "accounts",
			PrimaryKey: []string{"id"},
			Columns: []sqlmodel.Column{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "integer"}},
				{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["accounts"]
		require.NotNil(t, table)
		assert.Equal(t, []string{"id"}, table.PrimaryKey)
	})

	t.Run("table with NOT NULL columns", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateTable,
			SchemaName: "public",
			TableName:  "items",
			Columns: []sqlmodel.Column{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}, Nullable: false},
				{Name: "description", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, Nullable: true},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["items"]
		require.Len(t, table.Columns, 2)
		assert.False(t, table.Columns[0].Nullable)
		assert.True(t, table.Columns[1].Nullable)
	})

	t.Run("table with constraints", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateTable,
			SchemaName: "public",
			TableName:  "orders",
			Columns: []sqlmodel.Column{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
				{Name: "user_id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			},
			Constraints: []sqlmodel.Constraint{
				{Name: "pk_orders", Kind: sqlmodel.ConstraintPrimaryKey, Columns: []string{"id"}},
				{Name: "fk_orders_user", Kind: sqlmodel.ConstraintForeignKey, Columns: []string{"user_id"}, ForeignTable: "users", ForeignColumns: []string{"id"}},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["orders"]
		require.Len(t, table.Constraints, 2)
		assert.Equal(t, "pk_orders", table.Constraints[0].Name)
		assert.Equal(t, sqlmodel.ConstraintForeignKey, table.Constraints[1].Kind)
	})

	t.Run("virtual table records module name", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:              ast.MutationCreateTable,
			SchemaName:        "public",
			TableName:         "search_index",
			IsVirtual:         true,
			VirtualModuleName: "fts5",
			Columns: []sqlmodel.Column{
				{Name: "content", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["search_index"]
		assert.True(t, table.IsVirtual)
		assert.Equal(t, "fts5", table.VirtualModuleName)
	})

	t.Run("table in implicit schema creates schema automatically", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateTable,
			SchemaName: "analytics",
			TableName:  "events",
			Columns: []sqlmodel.Column{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			},
		})

		require.NoError(t, err)
		require.Contains(t, builder.Catalogue().Schemas, "analytics")
		assert.NotNil(t, builder.Catalogue().Schemas["analytics"].Tables["events"])
	})

	t.Run("column with enum type is resolved", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		_ = builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateEnum,
			SchemaName: "public",
			EnumName:   "status",
			EnumValues: []string{"active", "inactive"},
		})

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateTable,
			SchemaName: "public",
			TableName:  "accounts",
			Columns: []sqlmodel.Column{
				{Name: "state", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: "status"}},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["accounts"]
		assert.Equal(t, sqlmodel.TypeCategoryEnum, table.Columns[0].SQLType.Category)
		assert.Equal(t, []string{"active", "inactive"}, table.Columns[0].SQLType.EnumValues)
	})
}

func TestCatalogueBuilder_ApplyDropTable(t *testing.T) {
	t.Parallel()

	t.Run("removes existing table", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "id"},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropTable,
			SchemaName: "public",
			TableName:  "users",
		})

		require.NoError(t, err)
		assert.NotContains(t, builder.Catalogue().Schemas["public"].Tables, "users")
	})

	t.Run("dropping non-existent table is refused", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropTable,
			SchemaName: "public",
			TableName:  "ghost",
		})

		require.ErrorIs(t, err, errUnknownObject)
	})

	t.Run("dropping non-existent table IF EXISTS is a no-op", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropTable,
			SchemaName: "public",
			TableName:  "ghost",
			IfExists:   true,
		})

		require.NoError(t, err)
	})
}

func TestCatalogueBuilder_ApplyAlterTableAddColumn(t *testing.T) {
	t.Parallel()

	t.Run("adds column to existing table", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableAddColumn,
			SchemaName: "public",
			TableName:  "users",
			Columns: []sqlmodel.Column{
				{Name: "email", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["users"]
		require.Len(t, table.Columns, 2)
		assert.Equal(t, "email", table.Columns[1].Name)
	})

	t.Run("table not found returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableAddColumn,
			SchemaName: "public",
			TableName:  "missing",
			Columns: []sqlmodel.Column{
				{Name: "col"},
			},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("empty columns slice is a no-op", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "id"},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableAddColumn,
			SchemaName: "public",
			TableName:  "users",
			Columns:    nil,
		})

		require.NoError(t, err)
		assert.Len(t, builder.Catalogue().Schemas["public"].Tables["users"].Columns, 1)
	})
}

func TestCatalogueBuilder_ApplyAlterTableDropColumn(t *testing.T) {
	t.Parallel()

	t.Run("removes column from table", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "id"},
			sqlmodel.Column{Name: "email"},
			sqlmodel.Column{Name: "name"},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableDropColumn,
			SchemaName: "public",
			TableName:  "users",
			ColumnName: "email",
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["users"]
		require.Len(t, table.Columns, 2)
		assert.Equal(t, "id", table.Columns[0].Name)
		assert.Equal(t, "name", table.Columns[1].Name)
	})

	t.Run("table not found returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableDropColumn,
			SchemaName: "public",
			TableName:  "missing",
			ColumnName: "col",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestCatalogueBuilder_ApplyAlterTableDropColumnTakesColumnsUnderAPrefix(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		column      string
		prefix      string
		wantColumns []string
	}{
		{name: "a prefix drops every column under it", column: "legs", prefix: "legs.", wantColumns: []string{"id", "legsx", "note"}},
		{name: "no prefix drops the column alone", column: "legs", prefix: "", wantColumns: []string{"id", "legs.colony", "legs.minutes", "legsx", "note"}},
		{name: "a prefix with no column of that name", column: "note", prefix: "note.", wantColumns: []string{"id", "legs", "legs.colony", "legs.minutes", "legsx"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilderWithTable("flights",
				sqlmodel.Column{Name: "id"},
				sqlmodel.Column{Name: "legs", Hidden: true},
				sqlmodel.Column{Name: "legs.colony"},
				sqlmodel.Column{Name: "legs.minutes"},
				sqlmodel.Column{Name: "legsx"},
				sqlmodel.Column{Name: "note"},
			)

			err := builder.applyMutation(t.Context(), ast.NewCatalogueMutation(ast.MutationAlterTableDropColumn, "public", "flights",
				ast.WithColumnName(testCase.column), ast.WithDroppedColumnPrefix(testCase.prefix)))

			require.NoError(t, err)
			require.Equal(t, testCase.wantColumns, columnNamesOf(builder.Catalogue().Schemas["public"].Tables["flights"].Columns))
		})
	}
}

func TestCatalogueBuilder_ApplyAlterTableAlterColumn(t *testing.T) {
	t.Parallel()

	t.Run("replaces column definition", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "email", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"}, Nullable: false},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableAlterColumn,
			SchemaName: "public",
			TableName:  "users",
			ColumnName: "email",
			Columns: []sqlmodel.Column{
				{Name: "email", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}, Nullable: true},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["users"]
		assert.Equal(t, "text", table.Columns[0].SQLType.EngineName)
		assert.True(t, table.Columns[0].Nullable)
	})

	t.Run("table not found returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableAlterColumn,
			SchemaName: "public",
			TableName:  "missing",
			ColumnName: "col",
			Columns: []sqlmodel.Column{
				{Name: "col"},
			},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("column not found returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "id"},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableAlterColumn,
			SchemaName: "public",
			TableName:  "users",
			ColumnName: "ghost",
			Columns: []sqlmodel.Column{
				{Name: "ghost"},
			},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "column ghost not found")
	})

	t.Run("empty columns slice is a no-op", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "id", SQLType: sqlmodel.SQLType{EngineName: "integer"}},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableAlterColumn,
			SchemaName: "public",
			TableName:  "users",
			ColumnName: "id",
			Columns:    nil,
		})

		require.NoError(t, err)

		assert.Equal(t, "integer", builder.Catalogue().Schemas["public"].Tables["users"].Columns[0].SQLType.EngineName)
	})
}

func TestCatalogueBuilder_ApplyAlterTableRenameColumn(t *testing.T) {
	t.Parallel()

	t.Run("renames existing column", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "email"},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableRenameColumn,
			SchemaName: "public",
			TableName:  "users",
			ColumnName: "email",
			NewName:    "email_address",
		})

		require.NoError(t, err)
		assert.Equal(t, "email_address", builder.Catalogue().Schemas["public"].Tables["users"].Columns[0].Name)
	})

	t.Run("column not found returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "id"},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableRenameColumn,
			SchemaName: "public",
			TableName:  "users",
			ColumnName: "ghost",
			NewName:    "spectre",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "column ghost not found")
	})
}

func TestCatalogueBuilder_ApplyAlterTableRenameTable(t *testing.T) {
	t.Parallel()

	t.Run("renames existing table", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "id"},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableRenameTable,
			SchemaName: "public",
			TableName:  "users",
			NewName:    "accounts",
		})

		require.NoError(t, err)
		schema := builder.Catalogue().Schemas["public"]
		assert.NotContains(t, schema.Tables, "users")
		require.Contains(t, schema.Tables, "accounts")
		assert.Equal(t, "accounts", schema.Tables["accounts"].Name)
	})

	t.Run("table not found returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableRenameTable,
			SchemaName: "public",
			TableName:  "missing",
			NewName:    "renamed",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestCatalogueBuilder_ApplyAlterTableSetSchema(t *testing.T) {
	t.Parallel()

	t.Run("moves table between schemas", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "id"},
		)

		builder.catalogue.Schemas["archive"] = newEmptySchema("archive")
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableSetSchema,
			SchemaName: "public",
			TableName:  "users",
			NewName:    "archive",
		})

		require.NoError(t, err)
		assert.NotContains(t, builder.Catalogue().Schemas["public"].Tables, "users")
		require.Contains(t, builder.Catalogue().Schemas["archive"].Tables, "users")
		assert.Equal(t, "archive", builder.Catalogue().Schemas["archive"].Tables["users"].Schema)
	})

	t.Run("table not found returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableSetSchema,
			SchemaName: "public",
			TableName:  "missing",
			NewName:    "other",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("target schema is created automatically if absent", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("logs",
			sqlmodel.Column{Name: "id"},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableSetSchema,
			SchemaName: "public",
			TableName:  "logs",
			NewName:    "reporting",
		})

		require.NoError(t, err)
		require.Contains(t, builder.Catalogue().Schemas, "reporting")
		assert.NotNil(t, builder.Catalogue().Schemas["reporting"].Tables["logs"])
	})
}

func TestCatalogueBuilder_ApplyCreateEnum(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilder()
	ctx := t.Context()

	err := builder.applyMutation(ctx, &ast.CatalogueMutation{
		Kind:       ast.MutationCreateEnum,
		SchemaName: "public",
		EnumName:   "mood",
		EnumValues: []string{"happy", "sad", "neutral"},
	})

	require.NoError(t, err)
	enum := builder.Catalogue().Schemas["public"].Enums["mood"]
	require.NotNil(t, enum)
	assert.Equal(t, "mood", enum.Name)
	assert.Equal(t, "public", enum.Schema)
	assert.Equal(t, []string{"happy", "sad", "neutral"}, enum.Values)
}

func TestCatalogueBuilder_ApplyAlterEnumAddValue(t *testing.T) {
	t.Parallel()

	t.Run("adds value to existing enum", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].Enums["mood"] = &sqlmodel.Enum{
			Name:   "mood",
			Schema: "public",
			Values: []string{"happy", "sad"},
		}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterEnumAddValue,
			SchemaName: "public",
			EnumName:   "mood",
			EnumValues: []string{"neutral"},
		})

		require.NoError(t, err)
		assert.Equal(t, []string{"happy", "sad", "neutral"}, builder.Catalogue().Schemas["public"].Enums["mood"].Values)
	})

	t.Run("enum not found returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterEnumAddValue,
			SchemaName: "public",
			EnumName:   "ghost",
			EnumValues: []string{"value"},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "enum ghost not found")
	})
}

func TestCatalogueBuilder_ApplyAlterEnumRenameValue(t *testing.T) {
	t.Parallel()

	t.Run("renames existing enum value", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].Enums["colour"] = &sqlmodel.Enum{
			Name:   "colour",
			Schema: "public",
			Values: []string{"red", "green", "blue"},
		}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterEnumRenameValue,
			SchemaName: "public",
			EnumName:   "colour",
			EnumValues: []string{"green", "lime"},
		})

		require.NoError(t, err)
		assert.Equal(t, []string{"red", "lime", "blue"}, builder.Catalogue().Schemas["public"].Enums["colour"].Values)
	})

	t.Run("enum not found returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterEnumRenameValue,
			SchemaName: "public",
			EnumName:   "ghost",
			EnumValues: []string{"old", "new"},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "enum ghost not found")
	})

	t.Run("fewer than two values is a no-op", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].Enums["colour"] = &sqlmodel.Enum{
			Name:   "colour",
			Schema: "public",
			Values: []string{"red"},
		}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterEnumRenameValue,
			SchemaName: "public",
			EnumName:   "colour",
			EnumValues: []string{"red"},
		})

		require.NoError(t, err)
		assert.Equal(t, []string{"red"}, builder.Catalogue().Schemas["public"].Enums["colour"].Values)
	})
}

func TestCatalogueBuilder_ApplyDropEnum(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilder()
	ctx := t.Context()
	builder.catalogue.Schemas["public"].Enums["mood"] = &sqlmodel.Enum{
		Name: "mood",
	}

	err := builder.applyMutation(ctx, &ast.CatalogueMutation{
		Kind:       ast.MutationDropEnum,
		SchemaName: "public",
		EnumName:   "mood",
	})

	require.NoError(t, err)
	assert.NotContains(t, builder.Catalogue().Schemas["public"].Enums, "mood")
}

func TestCatalogueBuilder_ApplyCreateCompositeType(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilder()
	ctx := t.Context()

	err := builder.applyMutation(ctx, &ast.CatalogueMutation{
		Kind:       ast.MutationCreateCompositeType,
		SchemaName: "public",
		EnumName:   "address",
		Columns: []sqlmodel.Column{
			{Name: "street", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}},
			{Name: "city", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}},
		},
	})

	require.NoError(t, err)
	composite := builder.Catalogue().Schemas["public"].CompositeTypes["address"]
	require.NotNil(t, composite)
	assert.Equal(t, "address", composite.Name)
	assert.Equal(t, "public", composite.Schema)
	require.Len(t, composite.Fields, 2)
	assert.Equal(t, "street", composite.Fields[0].Name)
	assert.Equal(t, "city", composite.Fields[1].Name)
}

func TestCatalogueBuilder_ApplyDropType(t *testing.T) {
	t.Parallel()

	t.Run("removes composite type", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].CompositeTypes["address"] = &sqlmodel.CompositeType{
			Name: "address",
		}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropType,
			SchemaName: "public",
			EnumName:   "address",
		})

		require.NoError(t, err)
		assert.NotContains(t, builder.Catalogue().Schemas["public"].CompositeTypes, "address")
	})

	t.Run("removes enum type", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].Enums["status"] = &sqlmodel.Enum{
			Name: "status",
		}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropType,
			SchemaName: "public",
			EnumName:   "status",
		})

		require.NoError(t, err)
		assert.NotContains(t, builder.Catalogue().Schemas["public"].Enums, "status")
	})

	t.Run("removes both enum and composite with same name", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].Enums["dual"] = &sqlmodel.Enum{Name: "dual"}
		builder.catalogue.Schemas["public"].CompositeTypes["dual"] = &sqlmodel.CompositeType{Name: "dual"}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropType,
			SchemaName: "public",
			EnumName:   "dual",
		})

		require.NoError(t, err)
		assert.NotContains(t, builder.Catalogue().Schemas["public"].Enums, "dual")
		assert.NotContains(t, builder.Catalogue().Schemas["public"].CompositeTypes, "dual")
	})
}

func TestCatalogueBuilder_ApplyCreateFunction(t *testing.T) {
	t.Parallel()

	t.Run("creates new function", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateFunction,
			SchemaName: "public",
			Function: ast.NewFunctionDefinition(&sqlmodel.Function{
				Name:       "greet",
				Language:   "plpgsql",
				ReturnType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"},
				Arguments: []sqlmodel.FunctionArgument{
					{Name: "name", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}},
				},
			}),
		})

		require.NoError(t, err)
		overloads := builder.Catalogue().Schemas["public"].Functions["greet"]
		require.Len(t, overloads, 1)
		assert.Equal(t, "greet", overloads[0].Name)
		assert.Equal(t, sqlmodel.TypeCategoryText, overloads[0].ReturnType.Category)
	})

	t.Run("replaces existing function with matching signature", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		original := &sqlmodel.Function{
			Name:       "greet",
			Language:   "plpgsql",
			ReturnType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"},
			Arguments: []sqlmodel.FunctionArgument{
				{Name: "name", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}},
			},
		}
		builder.catalogue.Schemas["public"].Functions["greet"] = []*sqlmodel.Function{original}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateFunction,
			SchemaName: "public",
			Function: ast.NewFunctionDefinition(&sqlmodel.Function{
				Name:       "greet",
				Language:   "plpgsql",
				ReturnType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"},
				Arguments: []sqlmodel.FunctionArgument{
					{Name: "name", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}},
				},
			}),
		})

		require.NoError(t, err)
		overloads := builder.Catalogue().Schemas["public"].Functions["greet"]
		require.Len(t, overloads, 1)

		assert.Equal(t, "varchar", overloads[0].ReturnType.EngineName)
	})

	t.Run("adds overload with different argument types", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		existing := &sqlmodel.Function{
			Name: "process",
			Arguments: []sqlmodel.FunctionArgument{
				{Name: "val", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "integer"}},
			},
		}
		builder.catalogue.Schemas["public"].Functions["process"] = []*sqlmodel.Function{existing}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateFunction,
			SchemaName: "public",
			Function: ast.NewFunctionDefinition(&sqlmodel.Function{
				Name: "process",
				Arguments: []sqlmodel.FunctionArgument{
					{Name: "val", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}},
				},
			}),
		})

		require.NoError(t, err)
		assert.Len(t, builder.Catalogue().Schemas["public"].Functions["process"], 2)
	})

	t.Run("nil function signature returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateFunction,
			SchemaName: "public",
			Function:   nil,
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing function signature")
	})

	t.Run("strict function sets nullable behaviour", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateFunction,
			SchemaName: "public",
			Function: ast.NewFunctionDefinition(&sqlmodel.Function{
				Name:     "strict_fn",
				Language: "plpgsql",
				IsStrict: true,
			}),
		})

		require.NoError(t, err)
		function := builder.Catalogue().Schemas["public"].Functions["strict_fn"][0]
		assert.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, function.NullableBehaviour)
	})
}

func TestCatalogueBuilder_ApplyDropFunction(t *testing.T) {
	t.Parallel()

	t.Run("returns error when mutation is missing function signature", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].Functions["greet"] = []*sqlmodel.Function{
			{Name: "greet"},
		}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropFunction,
			SchemaName: "public",
			TableName:  "greet",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "DROP FUNCTION mutation missing function signature")
		assert.Contains(t, builder.Catalogue().Schemas["public"].Functions, "greet")
	})

	t.Run("drops all overloads when signature has no arguments", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].Functions["process"] = []*sqlmodel.Function{
			{Name: "process", Arguments: []sqlmodel.FunctionArgument{{Name: "a", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}}}},
			{Name: "process", Arguments: []sqlmodel.FunctionArgument{{Name: "a", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}}},
		}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropFunction,
			SchemaName: "public",
			Function: ast.NewFunctionDefinition(&sqlmodel.Function{
				Name: "process",
			}),
		})

		require.NoError(t, err)
		assert.NotContains(t, builder.Catalogue().Schemas["public"].Functions, "process")
	})

	t.Run("drops specific overload by argument types", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].Functions["process"] = []*sqlmodel.Function{
			{Name: "process", Arguments: []sqlmodel.FunctionArgument{{Name: "a", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "integer"}}}},
			{Name: "process", Arguments: []sqlmodel.FunctionArgument{{Name: "a", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}}}},
		}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropFunction,
			SchemaName: "public",
			Function: ast.NewFunctionDefinition(&sqlmodel.Function{
				Name: "process",
				Arguments: []sqlmodel.FunctionArgument{
					{Name: "a", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "integer"}},
				},
			}),
		})

		require.NoError(t, err)
		remaining := builder.Catalogue().Schemas["public"].Functions["process"]
		require.Len(t, remaining, 1)
		assert.Equal(t, sqlmodel.TypeCategoryText, remaining[0].Arguments[0].Type.Category)
	})
}

func TestCatalogueBuilder_ApplyCreateSchema(t *testing.T) {
	t.Parallel()

	t.Run("creates new schema", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateSchema,
			SchemaName: "analytics",
		})

		require.NoError(t, err)
		require.Contains(t, builder.Catalogue().Schemas, "analytics")
		assert.Equal(t, "analytics", builder.Catalogue().Schemas["analytics"].Name)
		assert.NotNil(t, builder.Catalogue().Schemas["analytics"].Tables)
	})

	t.Run("idempotent for existing schema", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateSchema,
			SchemaName: "public",
		})

		require.NoError(t, err)

		assert.Contains(t, builder.Catalogue().Schemas, "public")
	})
}

func TestCatalogueBuilder_ApplyDropSchema(t *testing.T) {
	t.Parallel()

	t.Run("removes existing schema", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["temp"] = newEmptySchema("temp")

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropSchema,
			SchemaName: "temp",
		})

		require.NoError(t, err)
		assert.NotContains(t, builder.Catalogue().Schemas, "temp")
	})

	t.Run("dropping non-existent schema is a no-op", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropSchema,
			SchemaName: "ghost",
		})

		require.NoError(t, err)
	})
}

func TestCatalogueBuilder_ApplyCreateView(t *testing.T) {
	t.Parallel()

	t.Run("creates view with explicit columns", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateView,
			SchemaName: "public",
			TableName:  "active_users",
			Columns: []sqlmodel.Column{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
				{Name: "email", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
		})

		require.NoError(t, err)
		view := builder.Catalogue().Schemas["public"].Views["active_users"]
		require.NotNil(t, view)
		assert.Equal(t, "active_users", view.Name)
		assert.Equal(t, "public", view.Schema)
		require.Len(t, view.Columns, 2)
		assert.Equal(t, "id", view.Columns[0].Name)
	})
}

func TestCatalogueBuilder_ApplyDropView(t *testing.T) {
	t.Parallel()

	t.Run("removes existing view", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].Views["active_users"] = &sqlmodel.View{
			Name: "active_users",
		}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropView,
			SchemaName: "public",
			TableName:  "active_users",
		})

		require.NoError(t, err)
		assert.NotContains(t, builder.Catalogue().Schemas["public"].Views, "active_users")
	})

	t.Run("dropping non-existent view is refused", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationDropView,
			SchemaName: "public",
			TableName:  "ghost",
		})

		require.ErrorIs(t, err, errUnknownObject)
	})
}

func TestCatalogueBuilder_ApplyCreateIndex(t *testing.T) {
	t.Parallel()

	t.Run("adds index to existing table", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "email"},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateIndex,
			SchemaName: "public",
			TableName:  "users",
			NewName:    "idx_users_email",
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["users"]
		require.Len(t, table.Indexes, 1)
		assert.Equal(t, "idx_users_email", table.Indexes[0].Name)
	})

	t.Run("missing relation is an unknown object", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateIndex,
			SchemaName: "public",
			TableName:  "missing",
			NewName:    "idx_test",
		})

		require.ErrorIs(t, err, errUnknownObject)
		assert.Equal(t, diag.CodeUnknownObject, mutationErrorCode(err))
	})

	t.Run("index on a view is accepted without a record", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].Views["order_totals"] = &sqlmodel.View{
			Name: "order_totals",
		}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateIndex,
			SchemaName: "public",
			TableName:  "order_totals",
			NewName:    "idx_order_totals",
		})

		require.NoError(t, err)
	})
}

func TestCatalogueBuilder_ApplyCreateExtension(t *testing.T) {
	t.Parallel()

	t.Run("records extension in catalogue", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:      ast.MutationCreateExtension,
			TableName: "pgcrypto",
		})

		require.NoError(t, err)
		assert.Contains(t, builder.Catalogue().Extensions, "pgcrypto")
	})

	t.Run("loads extension functions when engine supports it", func(t *testing.T) {
		t.Parallel()

		engine := &mockExtensionLoaderEngine{
			SchemaSearchPathFunc: func() []string { return []string{"public"} },
			loadExtensionFunctionsHook: func(name string) []*sqlmodel.Function {
				if name == "uuid-ossp" {
					return []*sqlmodel.Function{
						{Name: "uuid_generate_v4", ReturnType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUUID, EngineName: "uuid"}},
					}
				}
				return nil
			},
		}
		builder := newCatalogueBuilder(engine, namespace.Default())
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:      ast.MutationCreateExtension,
			TableName: "uuid-ossp",
		})

		require.NoError(t, err)
		assert.Contains(t, builder.Catalogue().Extensions, "uuid-ossp")
		functions := builder.Catalogue().Schemas["public"].Functions["uuid_generate_v4"]
		require.Len(t, functions, 1)
		assert.Equal(t, sqlmodel.TypeCategoryUUID, functions[0].ReturnType.Category)
	})
}

func TestCatalogueBuilder_ApplyCreateSequence(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilder()
	ctx := t.Context()

	err := builder.applyMutation(ctx, &ast.CatalogueMutation{
		Kind:          ast.MutationCreateSequence,
		SchemaName:    "public",
		SequenceName:  "users_id_seq",
		OwnedByTable:  "users",
		OwnedByColumn: "id",
	})

	require.NoError(t, err)
	sequence := builder.Catalogue().Schemas["public"].Sequences["users_id_seq"]
	require.NotNil(t, sequence)
	assert.Equal(t, "users_id_seq", sequence.Name)
	assert.Equal(t, "public", sequence.Schema)
	assert.Equal(t, "users", sequence.OwnedByTable)
	assert.Equal(t, "id", sequence.OwnedByColumn)
}

func TestCatalogueBuilder_ApplyDropSequence(t *testing.T) {
	t.Parallel()

	t.Run("removes existing sequence", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].Sequences["users_id_seq"] = &sqlmodel.Sequence{
			Name: "users_id_seq",
		}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:         ast.MutationDropSequence,
			SchemaName:   "public",
			SequenceName: "users_id_seq",
		})

		require.NoError(t, err)
		assert.NotContains(t, builder.Catalogue().Schemas["public"].Sequences, "users_id_seq")
	})

	t.Run("dropping non-existent sequence IF EXISTS is a no-op", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:         ast.MutationDropSequence,
			SchemaName:   "public",
			SequenceName: "ghost",
			IfExists:     true,
		})

		require.NoError(t, err)
	})
}

func TestCatalogueBuilder_ApplyAddConstraint(t *testing.T) {
	t.Parallel()

	t.Run("adds primary key constraint", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "id"},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableAddConstraint,
			SchemaName: "public",
			TableName:  "users",
			Constraints: []sqlmodel.Constraint{
				{Name: "pk_users", Kind: sqlmodel.ConstraintPrimaryKey, Columns: []string{"id"}},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["users"]
		require.Len(t, table.Constraints, 1)
		assert.Equal(t, "pk_users", table.Constraints[0].Name)
		assert.Equal(t, sqlmodel.ConstraintPrimaryKey, table.Constraints[0].Kind)
	})

	t.Run("adds unique constraint", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "email"},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableAddConstraint,
			SchemaName: "public",
			TableName:  "users",
			Constraints: []sqlmodel.Constraint{
				{Name: "uq_users_email", Kind: sqlmodel.ConstraintUnique, Columns: []string{"email"}},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["users"]
		require.Len(t, table.Constraints, 1)
		assert.Equal(t, sqlmodel.ConstraintUnique, table.Constraints[0].Kind)
	})

	t.Run("adds foreign key constraint", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("orders",
			sqlmodel.Column{Name: "user_id"},
		)
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableAddConstraint,
			SchemaName: "public",
			TableName:  "orders",
			Constraints: []sqlmodel.Constraint{
				{
					Name:           "fk_orders_user",
					Kind:           sqlmodel.ConstraintForeignKey,
					Columns:        []string{"user_id"},
					ForeignTable:   "users",
					ForeignColumns: []string{"id"},
				},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["orders"]
		require.Len(t, table.Constraints, 1)
		assert.Equal(t, "users", table.Constraints[0].ForeignTable)
	})

	t.Run("table not found returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableAddConstraint,
			SchemaName: "public",
			TableName:  "missing",
			Constraints: []sqlmodel.Constraint{
				{Name: "pk_test", Kind: sqlmodel.ConstraintPrimaryKey},
			},
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestCatalogueBuilder_ApplyDropConstraint(t *testing.T) {
	t.Parallel()

	t.Run("removes constraint by name", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()
		builder.catalogue.Schemas["public"].Tables["users"] = &sqlmodel.Table{
			Name:   "users",
			Schema: "public",
			Columns: []sqlmodel.Column{
				{Name: "id"},
				{Name: "email"},
			},
			Constraints: []sqlmodel.Constraint{
				{Name: "pk_users", Kind: sqlmodel.ConstraintPrimaryKey},
				{Name: "uq_users_email", Kind: sqlmodel.ConstraintUnique},
			},
		}

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:           ast.MutationAlterTableDropConstraint,
			SchemaName:     "public",
			TableName:      "users",
			ConstraintName: "uq_users_email",
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["users"]
		require.Len(t, table.Constraints, 1)
		assert.Equal(t, "pk_users", table.Constraints[0].Name)
	})

	t.Run("table not found returns error", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:           ast.MutationAlterTableDropConstraint,
			SchemaName:     "public",
			TableName:      "missing",
			ConstraintName: "pk_test",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestCatalogueBuilder_ApplyComment(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilder()
	ctx := t.Context()

	err := builder.applyMutation(ctx, &ast.CatalogueMutation{
		Kind: ast.MutationComment,
	})

	require.NoError(t, err)
}

func TestCatalogueBuilder_ApplyMigration(t *testing.T) {
	t.Parallel()

	t.Run("parse error produces diagnostics", func(t *testing.T) {
		t.Parallel()

		engine := &fakedialect.Engine{
			SchemaSearchPathFunc: func() []string { return []string{"public"} },
			ParseStatementsFunc: func(_ context.Context, _ string) ([]ast.ParsedStatement, error) {
				return nil, errors.New("syntax error at position 42")
			},
		}
		builder := newCatalogueBuilder(engine, namespace.Default())

		diagnostics := applyMigration(t, builder, "001_bad.sql", "CREATE TABL users (id int);", 0)

		require.Len(t, diagnostics, 1)
		assert.Equal(t, "001_bad.sql", diagnostics[0].Filename)
		assert.Contains(t, diagnostics[0].Message, "syntax error")
		assert.Equal(t, diag.SeverityError, diagnostics[0].Severity)
		assert.Equal(t, "Q010", diagnostics[0].Code)
	})

	t.Run("successful migration applies DDL", func(t *testing.T) {
		t.Parallel()

		engine := &fakedialect.Engine{
			SchemaSearchPathFunc: func() []string { return []string{"public"} },
			ParseStatementsFunc: func(_ context.Context, _ string) ([]ast.ParsedStatement, error) {
				return []ast.ParsedStatement{
					{Location: 0, Length: 50},
				}, nil
			},
			ApplyDDLFunc: func(_ context.Context, _ ast.ParsedStatement) (*ast.CatalogueMutation, error) {
				return &ast.CatalogueMutation{
					Kind:       ast.MutationCreateTable,
					SchemaName: "public",
					TableName:  "users",
					Columns: []sqlmodel.Column{
						{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "integer"}},
					},
				}, nil
			},
		}
		builder := newCatalogueBuilder(engine, namespace.Default())

		diagnostics := applyMigration(t, builder, "001_create_users.sql", "CREATE TABLE users (id integer);", 0)

		assert.Empty(t, diagnostics)
		table := builder.Catalogue().Schemas["public"].Tables["users"]
		require.NotNil(t, table)
		assert.Equal(t, "users", table.Name)

		assert.Equal(t, "001_create_users.sql", table.Origin.Filename)
		assert.Equal(t, 0, table.Origin.Index)
	})

	t.Run("DDL error produces diagnostics but continues", func(t *testing.T) {
		t.Parallel()

		callCount := 0
		engine := &fakedialect.Engine{
			SchemaSearchPathFunc: func() []string { return []string{"public"} },
			ParseStatementsFunc: func(_ context.Context, _ string) ([]ast.ParsedStatement, error) {
				return []ast.ParsedStatement{
					{Location: 0, Length: 20},
					{Location: 21, Length: 30},
				}, nil
			},
			ApplyDDLFunc: func(_ context.Context, _ ast.ParsedStatement) (*ast.CatalogueMutation, error) {
				callCount++
				if callCount == 1 {
					return nil, errors.New("unsupported DDL")
				}
				return &ast.CatalogueMutation{
					Kind:       ast.MutationCreateTable,
					SchemaName: "public",
					TableName:  "items",
					Columns: []sqlmodel.Column{
						{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
					},
				}, nil
			},
		}
		builder := newCatalogueBuilder(engine, namespace.Default())

		diagnostics := applyMigration(t, builder, "002_mixed.sql", "BADSTMT; CREATE TABLE items (id int);", 1)

		require.Len(t, diagnostics, 1)
		assert.Contains(t, diagnostics[0].Message, "unsupported DDL")
		assert.NotNil(t, builder.Catalogue().Schemas["public"].Tables["items"])
	})

	t.Run("nil mutation from engine is silently skipped", func(t *testing.T) {
		t.Parallel()

		engine := &fakedialect.Engine{
			SchemaSearchPathFunc: func() []string { return []string{"public"} },
			ParseStatementsFunc: func(_ context.Context, _ string) ([]ast.ParsedStatement, error) {
				return []ast.ParsedStatement{
					{Location: 0, Length: 10},
				}, nil
			},
			ApplyDDLFunc: func(_ context.Context, _ ast.ParsedStatement) (*ast.CatalogueMutation, error) {
				return nil, nil
			},
		}
		builder := newCatalogueBuilder(engine, namespace.Default())

		diagnostics := applyMigration(t, builder, "003_noop.sql", "-- comment", 2)

		assert.Empty(t, diagnostics)
	})
}

func TestCatalogueBuilder_PlpgsqlBodyCallingWriterIsModifying(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilder()
	ctx := t.Context()

	writer := &sqlmodel.Function{
		Name:       "write_audit",
		Language:   "plpgsql",
		DataAccess: sqlmodel.DataAccessModifiesData,
		Arguments: []sqlmodel.FunctionArgument{
			{Name: "id", Type: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "int8"}},
		},
	}
	builder.catalogue.Schemas["public"].Functions["write_audit"] = []*sqlmodel.Function{writer}

	err := builder.applyMutation(ctx, &ast.CatalogueMutation{
		Kind:       ast.MutationCreateFunction,
		SchemaName: "public",
		Function: ast.NewFunctionDefinition(&sqlmodel.Function{
			Name:     "on_insert_trigger",
			Language: "plpgsql",
			BodySQL:  "BEGIN PERFORM write_audit(NEW.id); RETURN NEW; END;",
		}),
	})
	require.NoError(t, err)

	caller := builder.Catalogue().Schemas["public"].Functions["on_insert_trigger"][0]
	assert.Contains(t, builder.calledFunctions[caller], "write_audit",
		"the indirect callee must be recorded so propagation can reach it")
	assert.Equal(t, sqlmodel.DataAccessReadOnly, caller.DataAccess,
		"the keyword scan alone cannot see the write before propagation")

	propagateDataAccess(builder.Catalogue(), builder.calledFunctions, builder.calledTableFunctions, nil)

	caller = builder.Catalogue().Schemas["public"].Functions["on_insert_trigger"][0]
	assert.Equal(t, sqlmodel.DataAccessModifiesData, caller.DataAccess,
		"propagation must promote a plpgsql body that calls a writing function")
}

func TestCatalogueBuilder_ApplyMutation_UnknownKind(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilder()
	ctx := t.Context()

	err := builder.applyMutation(ctx, &ast.CatalogueMutation{
		Kind: ast.MutationKind(math.MaxUint8),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown mutation kind")
}

func TestCatalogueBuilder_ApplyTriggerMutations(t *testing.T) {
	t.Parallel()

	t.Run("create trigger is a no-op", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:        ast.MutationCreateTrigger,
			TriggerName: "trg_audit",
		})

		require.NoError(t, err)
	})

	t.Run("drop trigger is a no-op", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:        ast.MutationDropTrigger,
			TriggerName: "trg_audit",
		})

		require.NoError(t, err)
	})
}

func TestCatalogueBuilder_ApplyDropIndex(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilder()
	ctx := t.Context()

	err := builder.applyMutation(ctx, &ast.CatalogueMutation{
		Kind:    ast.MutationDropIndex,
		NewName: "idx_some_index",
	})

	require.NoError(t, err)
}

func TestCatalogueBuilder_OriginPropagation(t *testing.T) {
	t.Parallel()

	t.Run("create table sets origin on columns and constraints", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		origin := sqlmodel.MigrationOrigin{Filename: "001_init.sql", Index: 0}
		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationCreateTable,
			SchemaName: "public",
			TableName:  "users",
			Origin:     origin,
			Columns: []sqlmodel.Column{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			},
			Constraints: []sqlmodel.Constraint{
				{Name: "pk_users", Kind: sqlmodel.ConstraintPrimaryKey, Columns: []string{"id"}},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["users"]
		assert.Equal(t, origin, table.Origin)
		assert.Equal(t, origin, table.Columns[0].Origin)
		assert.Equal(t, origin, table.Constraints[0].Origin)
	})

	t.Run("add column sets origin on new columns", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilderWithTable("users",
			sqlmodel.Column{Name: "id"},
		)
		ctx := t.Context()

		origin := sqlmodel.MigrationOrigin{Filename: "002_add_email.sql", Index: 1}
		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAlterTableAddColumn,
			SchemaName: "public",
			TableName:  "users",
			Origin:     origin,
			Columns: []sqlmodel.Column{
				{Name: "email", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
		})

		require.NoError(t, err)
		table := builder.Catalogue().Schemas["public"].Tables["users"]
		assert.Equal(t, origin, table.Columns[1].Origin)
	})
}

func TestCatalogueBuilder_ApplyAsyncDataMutations(t *testing.T) {
	t.Parallel()

	t.Run("async update", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAsyncDataUpdate,
			SchemaName: "public",
			TableName:  "users",
			EngineSpecific: map[string]string{
				"ASYNC_BODY": "UPDATE name = 'x' WHERE id = 1",
			},
		})
		require.NoError(t, err)
	})

	t.Run("async delete", func(t *testing.T) {
		t.Parallel()

		builder, _ := setupBuilder()
		ctx := t.Context()

		err := builder.applyMutation(ctx, &ast.CatalogueMutation{
			Kind:       ast.MutationAsyncDataDelete,
			SchemaName: "public",
			TableName:  "users",
			EngineSpecific: map[string]string{
				"ASYNC_BODY": "DELETE WHERE id = 1",
			},
		})
		require.NoError(t, err)
	})
}

func TestCatalogueBuilder_MutationHandlersComplete(t *testing.T) {
	t.Parallel()

	explicitSwitchKinds := map[ast.MutationKind]struct{}{
		ast.MutationCreateTable:    {},
		ast.MutationCreateFunction: {},
		ast.MutationCreateView:     {},
	}

	for kind := ast.MutationKind(0); kind.IsValid(); kind++ {
		if _, isExplicit := explicitSwitchKinds[kind]; isExplicit {
			continue
		}
		assert.NotNil(t, mutationHandlers[kind], "mutation kind %d has no handler and is not in the explicit switch", kind)
	}
}

func TestCatalogueBuilder_ApplyMigrationColumnOverridesGoType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		goType       string
		wantCode     string
		wantSeverity diag.ErrorSeverity
		wantApplied  bool
	}{
		{
			name:        "the standard library's UUID is applied",
			goType:      "uuid.UUID",
			wantApplied: true,
		},
		{
			name:         "malformed qualified type is reported as unusable",
			goType:       "pkg.",
			wantCode:     diag.CodeUnusableGoTypeOverride,
			wantSeverity: diag.SeverityError,
			wantApplied:  false,
		},
		{
			name:         "spaced name is reported as unusable",
			goType:       "bad type",
			wantCode:     diag.CodeUnusableGoTypeOverride,
			wantSeverity: diag.SeverityError,
			wantApplied:  false,
		},
		{
			name:         "map type is reported as unusable",
			goType:       "map[string]int",
			wantCode:     diag.CodeUnusableGoTypeOverride,
			wantSeverity: diag.SeverityError,
			wantApplied:  false,
		},
		{
			name:         "numeric type name is reported as unusable",
			goType:       "example.com/menu.123",
			wantCode:     diag.CodeUnusableGoTypeOverride,
			wantSeverity: diag.SeverityError,
			wantApplied:  false,
		},
		{
			name:        "valid qualified type is applied",
			goType:      "github.com/google/uuid.UUID",
			wantApplied: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilderWithTable("users", sqlmodel.NewColumn("id", sqlmodel.SQLType{}, false))
			span := diagbuild.TextSpan{Line: 3, Column: 30, EndLine: 3, EndColumn: 40}
			overrides := []syntax.MigrationColumnOverride{{Table: "users", Column: "id", GoType: testCase.goType, GoTypeSpan: span}}

			diagnostics := builder.applyMigrationColumnOverrides(builder.readColumnDirectives(overrides), "001_init.up.sql")

			column := builder.Catalogue().FindColumn("", "users", "id")
			require.NotNil(t, column)
			if testCase.wantApplied {
				assert.Empty(t, diagnostics)
				assert.NotNil(t, column.GoTypeOverride)
				return
			}
			require.Len(t, diagnostics, 1)
			assert.Equal(t, testCase.wantCode, diagnostics[0].Code)
			assert.Equal(t, testCase.wantSeverity, diagnostics[0].Severity)
			assert.Equal(t, "001_init.up.sql", diagnostics[0].Filename)
			assert.Equal(t, []int{3, 30, 3, 40}, []int{diagnostics[0].Line, diagnostics[0].Column, diagnostics[0].EndLine, diagnostics[0].EndColumn})
			assert.Contains(t, diagnostics[0].Message, testCase.goType)
			assert.Nil(t, column.GoTypeOverride)
		})
	}
}

func TestApplyMigrationReadOnlyOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutation   *ast.CatalogueMutation
		readOnly   *bool
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "nil mutation does not panic",
			mutation: &ast.CatalogueMutation{
				Kind: ast.MutationCreateTable,
			},
			readOnly: nil,
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()

				assert.Nil(t, mutation.Function)
			},
		},
		{
			name: "non-CreateFunction mutation kind is a no-op",
			mutation: &ast.CatalogueMutation{
				Kind: ast.MutationCreateTable,
				Function: ast.NewFunctionDefinition(&sqlmodel.Function{
					Name:       "irrelevant",
					DataAccess: sqlmodel.DataAccessUnknown,
				}),
			},
			readOnly: new(true),
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()

				assert.Equal(t, sqlmodel.DataAccessUnknown, mutation.Function.DataAccess)
			},
		},
		{
			name: "CreateFunction with override true sets DataAccessReadOnly",
			mutation: &ast.CatalogueMutation{
				Kind: ast.MutationCreateFunction,
				Function: ast.NewFunctionDefinition(&sqlmodel.Function{
					Name:       "my_reader",
					DataAccess: sqlmodel.DataAccessUnknown,
				}),
			},
			readOnly: new(true),
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()

				assert.Equal(t, sqlmodel.DataAccessReadOnly, mutation.Function.DataAccess)
			},
		},
		{
			name: "CreateFunction with override false sets DataAccessModifiesData",
			mutation: &ast.CatalogueMutation{
				Kind: ast.MutationCreateFunction,
				Function: ast.NewFunctionDefinition(&sqlmodel.Function{
					Name:       "my_writer",
					DataAccess: sqlmodel.DataAccessUnknown,
				}),
			},
			readOnly: new(false),
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()

				assert.Equal(t, sqlmodel.DataAccessModifiesData, mutation.Function.DataAccess)
			},
		},
		{
			name: "CreateFunction with no override remains unchanged",
			mutation: &ast.CatalogueMutation{
				Kind: ast.MutationCreateFunction,
				Function: ast.NewFunctionDefinition(&sqlmodel.Function{
					Name:       "no_override",
					DataAccess: sqlmodel.DataAccessUnknown,
				}),
			},
			readOnly: nil,
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()

				assert.Equal(t, sqlmodel.DataAccessUnknown, mutation.Function.DataAccess)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			applyMigrationReadOnlyOverride(testCase.mutation, testCase.readOnly)
			testCase.assertions(t, testCase.mutation)
		})
	}
}

func TestApplyAlterTableAlterColumnChangesOnlyNullability(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		option       ast.CatalogueMutationOption
		nullable     bool
		wantNullable bool
	}{
		{name: "set not null", option: ast.WithColumnNullability(false), nullable: true, wantNullable: false},
		{name: "drop not null", option: ast.WithColumnNullability(true), nullable: false, wantNullable: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilderWithTable("boats",
				sqlmodel.Column{Name: "name", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"), Nullable: testCase.nullable, HasDefault: true},
			)

			err := builder.applyMutation(t.Context(), ast.NewCatalogueMutation(ast.MutationAlterTableAlterColumn, "public", "boats",
				ast.WithColumnName("NAME"), testCase.option))

			require.NoError(t, err)
			column := builder.Catalogue().Schemas["public"].Tables["boats"].Columns[0]
			require.Equal(t, testCase.wantNullable, column.Nullable)
			require.Equal(t, "varchar", column.SQLType.EngineName)
			require.True(t, column.HasDefault)
		})
	}
}

func TestCatalogueBuilder_ApplyMigrationColumnOverridesHonoursTheSchema(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		table         string
		wantSchema    string
		wantUntouched string
	}{
		{name: "an unqualified table follows the search path", table: "users", wantSchema: "public", wantUntouched: "audit"},
		{name: "a qualified table names its schema", table: "audit.users", wantSchema: "audit", wantUntouched: "public"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilderWithTable("users", sqlmodel.NewColumn("code", sqlmodel.SQLType{}, false))
			builder.catalogue.Schemas["audit"] = &sqlmodel.Schema{
				Name: "audit",
				Tables: map[string]*sqlmodel.Table{
					"users": {Name: "users", Schema: "audit", Columns: []sqlmodel.Column{sqlmodel.NewColumn("code", sqlmodel.SQLType{}, false)}},
				},
			}

			diagnostics := builder.applyMigrationColumnOverrides(builder.readColumnDirectives(
				[]syntax.MigrationColumnOverride{{Table: testCase.table, Column: "code", SQLType: "uuid"}}), "001_init.up.sql")

			require.Empty(t, diagnostics)
			assert.Equal(t, "uuid", builder.catalogue.Schemas[testCase.wantSchema].Tables["users"].Columns[0].SQLTypeOverride)
			assert.Empty(t, builder.catalogue.Schemas[testCase.wantUntouched].Tables["users"].Columns[0].SQLTypeOverride)
		})
	}
}

func TestApplyAlterTableAlterColumnAnnotatesAView(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilder()
	ctx := t.Context()
	require.NoError(t, builder.applyMutation(ctx, &ast.CatalogueMutation{
		Kind:       ast.MutationCreateView,
		SchemaName: "public",
		TableName:  "hourly",
		Columns:    []sqlmodel.Column{{Name: "bucket", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryTemporal}}},
	}))

	annotation := ast.NewCatalogueMutation(ast.MutationAlterTableAlterColumn, "", "hourly",
		ast.WithEngineSpecific(map[string]string{"POLICY": "refresh"}))
	require.NoError(t, builder.applyMutation(ctx, annotation))

	nullability := ast.NewCatalogueMutation(ast.MutationAlterTableAlterColumn, "", "hourly",
		ast.WithColumnName("bucket"), ast.WithColumnNullability(false))
	require.Error(t, builder.applyMutation(ctx, nullability))

	missing := ast.NewCatalogueMutation(ast.MutationAlterTableAlterColumn, "", "nowhere",
		ast.WithEngineSpecific(map[string]string{"POLICY": "refresh"}))
	require.Error(t, builder.applyMutation(ctx, missing))
}

func TestApplyMigrationReportsAMutationErrorOnItsStatement(t *testing.T) {
	t.Parallel()

	migration := "CREATE TABLE users (id integer);\n\nALTER TABLE nowhere ADD COLUMN x integer;\n"
	engine := &fakedialect.Engine{
		SchemaSearchPathFunc: func() []string { return []string{"public"} },
		ParseStatementsFunc: func(_ context.Context, _ string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Location: 0, Length: 31}, {Location: 34, Length: 40}}, nil
		},
		ApplyDDLFunc: func(_ context.Context, statement ast.ParsedStatement) (*ast.CatalogueMutation, error) {
			if statement.Location == 0 {
				return &ast.CatalogueMutation{Kind: ast.MutationCreateTable, SchemaName: "public", TableName: "users",
					Columns: []sqlmodel.Column{{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}}}}, nil
			}
			return &ast.CatalogueMutation{Kind: ast.MutationAlterTableAddColumn, SchemaName: "public", TableName: "nowhere",
				Columns: []sqlmodel.Column{{Name: "x", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}}}}, nil
		},
	}
	builder := newCatalogueBuilder(engine, namespace.Default())

	diagnostics := applyMigration(t, builder, "002_alter.up.sql", migration, 0)

	require.Len(t, diagnostics, 1)
	require.Equal(t, 3, diagnostics[0].Line)
	require.Equal(t, diag.CodeUnknownObject, diagnostics[0].Code)
	require.Contains(t, diagnostics[0].Message, "nowhere")
}

func TestApplyMigrationRecordsTheStatementThatDeclaresATable(t *testing.T) {
	t.Parallel()

	catalogue, diagnostics := replayPostgresMigrations(t,
		"CREATE TABLE users (id int);\nCREATE TABLE orders (id int);",
		"SELECT 1;\nCREATE TABLE posts (id int);",
	)

	require.Empty(t, diagnostics)
	tables := catalogue.Schemas["public"].Tables
	require.Equal(t, sqlmodel.MigrationOrigin{Filename: "migration.sql", Index: 0, Statement: 0}, tables["users"].Origin)
	require.Equal(t, sqlmodel.MigrationOrigin{Filename: "migration.sql", Index: 0, Statement: 1}, tables["orders"].Origin)
	require.Equal(t, sqlmodel.MigrationOrigin{Filename: "migration.sql", Index: 1, Statement: 1}, tables["posts"].Origin)
}

func TestEnumsNamedInsideAColumnTypeAreResolved(t *testing.T) {
	t.Parallel()

	named := func(nullable bool) *sqlmodel.SQLType {
		return &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: "mood", Nullable: nullable}
	}
	mapOf := func(key, value *sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, EngineName: "map", KeyType: key, ElementType: value}
	}
	listOf := func(element *sqlmodel.SQLType) *sqlmodel.SQLType {
		return &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "list", ElementType: element}
	}
	structOf := func(fields ...sqlmodel.StructField) sqlmodel.SQLType {
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, EngineName: "struct", StructFields: fields}
	}
	text := &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "varchar"}

	testCases := []struct {
		name     string
		column   sqlmodel.SQLType
		enumAt   func(resolved *sqlmodel.SQLType) *sqlmodel.SQLType
		nullable bool
	}{
		{
			name:     "a map's values",
			column:   mapOf(text, named(true)),
			enumAt:   func(resolved *sqlmodel.SQLType) *sqlmodel.SQLType { return resolved.ElementType },
			nullable: true,
		},
		{
			name:   "a map's keys",
			column: mapOf(named(false), text),
			enumAt: func(resolved *sqlmodel.SQLType) *sqlmodel.SQLType { return resolved.KeyType },
		},
		{
			name:     "a struct's field",
			column:   structOf(sqlmodel.StructField{Name: "feeling", SQLType: *named(true)}),
			enumAt:   func(resolved *sqlmodel.SQLType) *sqlmodel.SQLType { return &resolved.StructFields[0].SQLType },
			nullable: true,
		},
		{
			name:   "a list inside a struct's field",
			column: structOf(sqlmodel.StructField{Name: "feelings", SQLType: *listOf(named(false))}),
			enumAt: func(resolved *sqlmodel.SQLType) *sqlmodel.SQLType {
				return resolved.StructFields[0].SQLType.ElementType
			},
		},
		{
			name:   "a union's member",
			column: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnion, EngineName: "union", UnionMembers: []sqlmodel.UnionMember{{Tag: "feeling", SQLType: *named(false)}}},
			enumAt: func(resolved *sqlmodel.SQLType) *sqlmodel.SQLType { return &resolved.UnionMembers[0].SQLType },
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilder()
			ctx := t.Context()
			require.NoError(t, builder.applyMutation(ctx, &ast.CatalogueMutation{
				Kind:       ast.MutationCreateEnum,
				SchemaName: "public",
				EnumName:   "mood",
				EnumValues: []string{"happy", "sad"},
			}))
			declared := testCase.column

			require.NoError(t, builder.applyMutation(ctx, &ast.CatalogueMutation{
				Kind:       ast.MutationCreateTable,
				SchemaName: "public",
				TableName:  "people",
				Columns:    []sqlmodel.Column{{Name: "feelings", SQLType: declared, Nullable: true}},
			}))

			column := builder.Catalogue().Schemas["public"].Tables["people"].Columns[0]
			enum := testCase.enumAt(&column.SQLType)
			require.NotNil(t, enum)
			require.Equal(t, sqlmodel.TypeCategoryEnum, enum.Category)
			require.Equal(t, "mood", enum.EngineName)
			require.Equal(t, []string{"happy", "sad"}, enum.EnumValues)
			require.Equal(t, testCase.nullable, enum.Nullable)
			require.Equal(t, sqlmodel.TypeCategoryUnknown, testCase.enumAt(&declared).Category)
		})
	}
}

func emptyMigrationEngine() *fakedialect.Engine {
	return &fakedialect.Engine{
		SchemaSearchPathFunc: func() []string { return []string{"public"} },
		ParseStatementsFunc: func(_ context.Context, _ string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Location: 0, Length: 1}, {Location: 1, Length: 1}}, nil
		},
		ApplyDDLFunc: func(_ context.Context, _ ast.ParsedStatement) (*ast.CatalogueMutation, error) {
			return nil, nil
		},
	}
}

func codesOf(diagnostics []diag.SourceError) []string {
	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	return codes
}

func TestMigrationWarningsRenderCanonicalPrefix(t *testing.T) {
	t.Parallel()

	prefixes := namespace.Set{Canonical: "acme", Aliases: []string{"acmedb"}, Foreign: nil}
	builder := newCatalogueBuilder(emptyMigrationEngine(), prefixes)

	diagnostics := applyMigration(t, builder, "001_init.up.sql", "-- acmedb.query(GetUser, one)\n-- acmedb.migration(no_transaction: true)\nCREATE TABLE a (id int);\nCREATE TABLE b (id int);", 0)

	require.ElementsMatch(t, []string{diag.CodeDirectiveWrongContext, diag.CodeMigrationMixedTransaction}, codesOf(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeDirectiveWrongContext {
			assert.Equal(t, "acme.query is a query directive and is ignored in a migration file; use acme.migration for migration directives", diagnostic.Message)
		}
		if diagnostic.Code == diag.CodeMigrationMixedTransaction {
			assert.Contains(t, diagnostic.Message, "acme.migration(no_transaction: true)")
		}
	}
}

func TestMigrationForeignDirectivesAreReported(t *testing.T) {
	t.Parallel()

	prefixes := namespace.Set{Canonical: "acme", Aliases: nil, Foreign: []string{"other"}}
	builder := newCatalogueBuilder(emptyMigrationEngine(), prefixes)

	diagnostics := applyMigration(t, builder, "001_init.up.sql", "-- other.migration(no_transaction: true)\nCREATE TABLE a (id int);", 0)

	require.Equal(t, []string{diag.CodeForeignPrefix}, codesOf(diagnostics))
	assert.Equal(t, 1, diagnostics[0].Line)
	assert.Equal(t, diag.SeverityWarning, diagnostics[0].Severity)
	assert.Contains(t, diagnostics[0].Message, "-- other.migration is not enabled here")
}

func TestApplyMigrationReportsADialectFailureOnItsStatement(t *testing.T) {
	t.Parallel()

	migration := "CREATE TABLE users (id integer);\n\nBROKEN STATEMENT;\nCREATE TABLE items (id integer);\n"
	testCases := []struct {
		name     string
		failure  error
		wantLine int
		wantCode string
	}{
		{name: "an error that locates nothing", failure: errors.New("unsupported DDL"), wantLine: 3, wantCode: diag.CodeParseError},
		{name: "a panic the dialect recovered", failure: fmt.Errorf("apply DDL: %w", fault.ErrPanic), wantLine: 3,
			wantCode: diag.CodeRecoveredPanic},
		{name: "an error that locates its token", failure: &dialectkit.ParseError{Message: "unexpected token", Token: "BROKEN",
			Position: 34}, wantLine: 3, wantCode: diag.CodeParseError},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.Engine{
				SchemaSearchPathFunc: func() []string { return []string{"public"} },
				ParseStatementsFunc: func(_ context.Context, _ string) ([]ast.ParsedStatement, error) {
					return []ast.ParsedStatement{{Location: 0, Length: 32}, {Location: 34, Length: 17}, {Location: 52, Length: 32}}, nil
				},
				ApplyDDLFunc: func(_ context.Context, statement ast.ParsedStatement) (*ast.CatalogueMutation, error) {
					if statement.Location == 34 {
						return nil, testCase.failure
					}
					name := map[int]string{0: "users", 52: "items"}[statement.Location]
					return ast.NewCatalogueMutation(ast.MutationCreateTable, "public", name), nil
				},
			}
			builder := newCatalogueBuilder(engine, namespace.Default())

			diagnostics := applyMigration(t, builder, "002_broken.up.sql", migration, 0)

			require.Len(t, diagnostics, 1)
			assert.Equal(t, testCase.wantLine, diagnostics[0].Line)
			assert.Equal(t, testCase.wantCode, diagnostics[0].Code)
			assert.Equal(t, []string{"items", "users"}, schemaTableNames(builder.Catalogue(), "public"))
		})
	}
}

func TestApplyMigrationRecoversAPanicInTheDialect(t *testing.T) {
	t.Parallel()

	migration := "CREATE TABLE users (id integer);\n\nCREATE TABLE broken (id integer);\nCREATE TABLE items (id integer);\n"
	statements := []ast.ParsedStatement{{Location: 0, Length: 32}, {Location: 34, Length: 33}, {Location: 68, Length: 32}}
	names := map[int]string{0: "users", 34: "broken", 68: "items"}
	testCases := []struct {
		name       string
		parse      func(context.Context, string) ([]ast.ParsedStatement, error)
		apply      func(context.Context, ast.ParsedStatement) (*ast.CatalogueMutation, error)
		wantLine   int
		wantTables []string
	}{
		{
			name:       "parsing the migration",
			parse:      func(context.Context, string) ([]ast.ParsedStatement, error) { panic("parse") },
			apply:      nil,
			wantLine:   1,
			wantTables: nil,
		},
		{
			name:  "interpreting a statement",
			parse: nil,
			apply: func(_ context.Context, statement ast.ParsedStatement) (*ast.CatalogueMutation, error) {
				if statement.Location == 34 {
					panic("apply")
				}
				return ast.NewCatalogueMutation(ast.MutationCreateTable, "public", names[statement.Location]), nil
			},
			wantLine:   3,
			wantTables: []string{"items", "users"},
		},
		{
			name:  "an additional mutation that is nil",
			parse: nil,
			apply: func(_ context.Context, statement ast.ParsedStatement) (*ast.CatalogueMutation, error) {
				if statement.Location == 34 {
					return &ast.CatalogueMutation{Kind: ast.MutationCreateTable, SchemaName: "public", TableName: "broken",
						AdditionalMutations: []*ast.CatalogueMutation{nil}}, nil
				}
				return ast.NewCatalogueMutation(ast.MutationCreateTable, "public", names[statement.Location]), nil
			},
			wantLine:   0,
			wantTables: []string{"broken", "items", "users"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.Engine{
				SchemaSearchPathFunc: func() []string { return []string{"public"} },
				ParseStatementsFunc: func(ctx context.Context, sql string) ([]ast.ParsedStatement, error) {
					if testCase.parse != nil {
						return testCase.parse(ctx, sql)
					}
					return statements, nil
				},
				ApplyDDLFunc: testCase.apply,
			}
			builder := newCatalogueBuilder(engine, namespace.Default())

			var diagnostics []diag.SourceError
			require.NotPanics(t, func() {
				diagnostics = applyMigration(t, builder, "003_panic.up.sql", migration, 0)
			})

			if testCase.wantLine == 0 {
				require.Empty(t, diagnostics)
			} else {
				require.Len(t, diagnostics, 1)
				assert.Equal(t, diag.CodeRecoveredPanic, diagnostics[0].Code)
				assert.Equal(t, diag.SeverityError, diagnostics[0].Severity)
				assert.Equal(t, testCase.wantLine, diagnostics[0].Line)
			}
			assert.Equal(t, testCase.wantTables, schemaTableNames(builder.Catalogue(), "public"))
		})
	}
}

func TestApplyMigrationReturnsCancellationAsAnError(t *testing.T) {
	t.Parallel()

	cause := errors.New("the run was stopped")
	testCases := []struct {
		name      string
		cancelsAt int
	}{
		{name: "while parsing", cancelsAt: -1},
		{name: "while applying the last statement", cancelsAt: 34},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			engine := &fakedialect.Engine{
				SchemaSearchPathFunc: func() []string { return []string{"public"} },
				ParseStatementsFunc: func(ctx context.Context, _ string) ([]ast.ParsedStatement, error) {
					if testCase.cancelsAt < 0 {
						cancel(cause)
						return nil, context.Cause(ctx)
					}
					return []ast.ParsedStatement{{Location: 0, Length: 32}, {Location: 34, Length: 32}}, nil
				},
				ApplyDDLFunc: func(_ context.Context, statement ast.ParsedStatement) (*ast.CatalogueMutation, error) {
					if statement.Location == testCase.cancelsAt {
						cancel(cause)
					}
					return ast.NewCatalogueMutation(ast.MutationCreateTable, "public", fmt.Sprintf("t%d", statement.Location)), nil
				},
			}
			builder := newCatalogueBuilder(engine, namespace.Default())

			diagnostics, err := builder.ApplyMigration(ctx, "004.up.sql",
				[]byte("CREATE TABLE t0 (id integer);\n\nCREATE TABLE t34 (id integer);"), 0)

			require.ErrorIs(t, err, cause)
			require.Contains(t, err.Error(), "004.up.sql")
			require.Empty(t, diagnostics)
		})
	}
}

func TestApplyMigrationReportsThePartsTheDialectCouldNotAnalyse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		part       ast.Warning
		wantLine   int
		wantColumn int
	}{
		{name: "a located part", part: ast.Warning{Message: "the view body was not analysed", Span: ast.Span{Start: 45, End: 53}},
			wantLine: 3, wantColumn: 12},
		{name: "a part with no place", part: ast.Warning{Message: "the view body was not analysed", Span: ast.Span{Start: 0, End: 0}},
			wantLine: 3, wantColumn: 1},
		{name: "a part placed past the text", part: ast.Warning{Message: "the view body was not analysed",
			Span: ast.Span{Start: 400, End: 410}}, wantLine: 3, wantColumn: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakedialect.Engine{
				SchemaSearchPathFunc: func() []string { return []string{"public"} },
				ParseStatementsFunc: func(_ context.Context, _ string) ([]ast.ParsedStatement, error) {
					return []ast.ParsedStatement{{Location: 0, Length: 32}, {Location: 34, Length: 20}}, nil
				},
				ApplyDDLFunc: func(_ context.Context, statement ast.ParsedStatement) (*ast.CatalogueMutation, error) {
					if statement.Location == 0 {
						return ast.NewCatalogueMutation(ast.MutationCreateTable, "public", "users"), nil
					}
					return ast.NewCatalogueMutation(ast.MutationCreateView, "public", "v", ast.WithUnanalysedPart(testCase.part)), nil
				},
			}
			builder := newCatalogueBuilder(engine, namespace.Default())

			diagnostics := applyMigration(t, builder, "005.up.sql", "CREATE TABLE users (id integer);\n\nCREATE VIEW v AS weird body;", 0)

			require.Len(t, diagnostics, 1)
			assert.Equal(t, diag.CodePartlyAnalysed, diagnostics[0].Code)
			assert.Equal(t, diag.SeverityWarning, diagnostics[0].Severity)
			assert.Equal(t, testCase.part.Message, diagnostics[0].Message)
			assert.Equal(t, testCase.wantLine, diagnostics[0].Line)
			assert.Equal(t, testCase.wantColumn, diagnostics[0].Column)
			assert.NotNil(t, builder.Catalogue().LookupView("", "v"))
		})
	}
}

func TestApplyMigrationReportsAViewBodyPostgresCannotAnalyse(t *testing.T) {
	t.Parallel()

	_, diagnostics := replayPostgresMigrations(t, "CREATE TABLE t (id int);\n\nCREATE TABLE t2 AS EXECUTE fetch_rows (1);")

	require.Len(t, diagnostics, 1)
	assert.Equal(t, diag.CodePartlyAnalysed, diagnostics[0].Code)
	assert.Equal(t, 3, diagnostics[0].Line)
	assert.Equal(t, 20, diagnostics[0].Column)
}

func TestAnUnknownMigrationColumnDirectiveIsReportedOnItsLine(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		span     diagbuild.TextSpan
		wantLine int
	}{
		{name: "a directive on line seven", span: diagbuild.TextSpan{Line: 7, Column: 1, EndLine: 7, EndColumn: 1}, wantLine: 7},
		{name: "a directive with no line", span: diagbuild.TextSpan{Line: 0, Column: 0, EndLine: 0, EndColumn: 0}, wantLine: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilderWithTable("users", sqlmodel.NewColumn("id", sqlmodel.SQLType{}, false))
			overrides := []syntax.MigrationColumnOverride{{Table: "users", Column: "missing", GoTypeSpan: testCase.span}}

			diagnostics := builder.applyMigrationColumnOverrides(builder.readColumnDirectives(overrides), "001_init.up.sql")

			require.Len(t, diagnostics, 1)
			assert.Equal(t, diag.CodeUnknownOverrideMigrationColumn, diagnostics[0].Code)
			assert.Equal(t, testCase.wantLine, diagnostics[0].Line)
		})
	}
}

func TestAReadOnlyDirectiveAppliesOnlyToTheFunctionItPrecedes(t *testing.T) {
	t.Parallel()

	migration := "CREATE TABLE items (id integer);\n" +
		"CREATE SCHEMA other;\n" +
		"-- sgl.migration(readonly: true)\n" +
		"CREATE FUNCTION touch(integer) RETURNS void AS $$ DELETE FROM items $$ LANGUAGE sql;\n" +
		"CREATE FUNCTION touch(text) RETURNS void AS $$ DELETE FROM items $$ LANGUAGE sql;\n" +
		"CREATE FUNCTION other.touch() RETURNS void AS $$ DELETE FROM items $$ LANGUAGE sql;\n" +
		"-- sgl.migration(readonly: false)\n" +
		"CREATE FUNCTION touch(boolean) RETURNS void AS $$ SELECT 1 $$ LANGUAGE sql;\n"
	testCases := []struct {
		name       string
		schema     string
		argument   string
		wantAccess sqlmodel.FunctionDataAccess
	}{
		{name: "the overload the directive precedes", schema: "public", argument: "int4", wantAccess: sqlmodel.DataAccessReadOnly},
		{name: "an overload of the same name after it", schema: "public", argument: "text", wantAccess: sqlmodel.DataAccessModifiesData},
		{name: "a function of the same name in another schema", schema: "other", argument: "", wantAccess: sqlmodel.DataAccessModifiesData},
		{name: "a later directive's own overload", schema: "public", argument: "bool", wantAccess: sqlmodel.DataAccessModifiesData},
	}

	builder := newCatalogueBuilder(postgres.New(), namespace.Default())
	diagnostics := applyMigration(t, builder, "001_touch.up.sql", migration, 0)
	require.Empty(t, diagnostics)

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var found *sqlmodel.Function
			for _, overload := range builder.catalogue.Schemas[testCase.schema].Functions["touch"] {
				if (testCase.argument == "" && len(overload.Arguments) == 0) ||
					(len(overload.Arguments) == 1 && overload.Arguments[0].Type.EngineName == testCase.argument) {
					found = overload
				}
			}
			require.NotNil(t, found)
			assert.Equal(t, testCase.wantAccess, found.DataAccess)
		})
	}
}

func TestApplyMigrationReportsMalformedMigrationDirectivesOnTheirLines(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		directive string
		wantCode  string
	}{
		{name: "a misspelt no_transaction", directive: "-- sgl.migration(no_transacton: true)", wantCode: diag.CodeUnknownKeywordArgument},
		{name: "a read-only value that is not a boolean", directive: "-- sgl.migration(readonly: maybe)", wantCode: diag.CodeInvalidKeywordArgumentValue},
		{name: "a column without its table", directive: "-- sgl.column(email, nullable: true)", wantCode: diag.CodeInvalidKeywordArgumentValue},
		{name: "a misspelt go_type", directive: "-- sgl.column(users.email, go_typ: string)", wantCode: diag.CodeUnknownKeywordArgument},
		{name: "a call left unclosed", directive: "-- sgl.migration(readonly: true", wantCode: diag.CodeUnclosedDirective},
		{name: "a misspelt role", directive: "-- sgl.migraton(readonly: true)", wantCode: diag.CodeUnknownDirective},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder := newCatalogueBuilder(postgres.New(), namespace.Default())
			migration := "CREATE TABLE users (email text);\n\n" + testCase.directive + "\nCREATE TABLE items (id integer);\n"

			diagnostics := applyMigration(t, builder, "001_init.up.sql", migration, 0)

			require.Len(t, diagnostics, 1, "%v", diagnostics)
			assert.Equal(t, testCase.wantCode, diagnostics[0].Code)
			assert.Equal(t, 3, diagnostics[0].Line)
			assert.Equal(t, "001_init.up.sql", diagnostics[0].Filename)
			assert.NotNil(t, builder.catalogue.LookupTable("", "items"))
		})
	}
}

func TestApplyMigrationReportsDirectiveProblemsWhenTheStatementsDoNotParse(t *testing.T) {
	t.Parallel()

	builder := newCatalogueBuilder(postgres.New(), namespace.Default())

	diagnostics := applyMigration(t, builder, "001_init.up.sql", "-- sgl.migration(no_transacton: true)\nCREATE TABLE 'broken;\n", 0)

	require.Len(t, diagnostics, 2)
	assert.Equal(t, diag.CodeUnknownKeywordArgument, diagnostics[0].Code)
	assert.Equal(t, 1, diagnostics[0].Line)
}

func TestApplyMigrationChecksTheTransactionFromTheTokensItReadOnce(t *testing.T) {
	t.Parallel()

	migration := "CREATE TABLE a (id int);\nBEGIN WORK;\n-- note\nCREATE INDEX CONCURRENTLY i ON a (id);\n"
	var seen [][]string
	proceduralBlockCalls := 0
	rules := sqllex.Rules{
		ProceduralBlock: func([]string) bool {
			proceduralBlockCalls++
			return false
		},
		RequiresNoTransaction: func(tokens []sqllex.Token) bool {
			words := make([]string, 0, len(tokens))
			for _, token := range tokens {
				words = append(words, token.Raw)
			}
			seen = append(seen, words)
			return slices.Contains(words, "CONCURRENTLY")
		},
	}
	engine := &fakedialect.Engine{
		SchemaSearchPathFunc: func() []string { return []string{"public"} },
		LexicalFunc:          func() sqllex.Rules { return rules },
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			texts := []string{"CREATE TABLE a (id int)", "BEGIN WORK", "CREATE INDEX CONCURRENTLY i ON a (id)"}
			statements := make([]ast.ParsedStatement, 0, len(texts))
			for _, text := range texts {
				start := strings.Index(sql, text)
				statements = append(statements, ast.ParsedStatement{Raw: nil, Location: start, Length: len(text)})
			}
			return statements, nil
		},
	}
	builder := newCatalogueBuilder(engine, namespace.Default())

	diagnostics := applyMigration(t, builder, "001_init.up.sql", migration, 0)

	require.Equal(t, []string{diag.CodeMigrationMixedTransaction}, codesOf(diagnostics))
	assert.Zero(t, proceduralBlockCalls)
	assert.Equal(t, [][]string{
		{"CREATE", "TABLE", "a", "(", "id", "int", ")"},
		{"BEGIN", "WORK"},
		{"CREATE", "INDEX", "CONCURRENTLY", "i", "ON", "a", "(", "id", ")"},
	}, seen)
}

func TestApplyMigrationReportsAPanickingTransactionPredicate(t *testing.T) {
	t.Parallel()

	engine := emptyMigrationEngine()
	engine.LexicalFunc = func() sqllex.Rules {
		return sqllex.Rules{RequiresNoTransaction: func([]sqllex.Token) bool { panic("predicate broke") }}
	}
	builder := newCatalogueBuilder(engine, namespace.Default())

	diagnostics := applyMigration(t, builder, "001_init.up.sql", "AB", 0)

	require.Equal(t, []string{diag.CodeRecoveredPanic}, codesOf(diagnostics))
	assert.Contains(t, diagnostics[0].Message, "predicate broke")
}

func TestStatementsRequireNoTransactionReadsEachStatementsSignificantTokens(t *testing.T) {
	t.Parallel()

	text := "VACUUM; /* VACUUM */ SELECT 'VACUUM';\n-- VACUUM\nSELECT 1"
	tokens := syntax.Tokens(sqllex.Rules{}, text)
	statement := func(written string) ast.ParsedStatement {
		start := strings.Index(text, written)
		return ast.ParsedStatement{Raw: nil, Location: start, Length: len(written)}
	}
	isVacuum := func(tokens []sqllex.Token) bool { return len(tokens) > 0 && tokens[0].IsWord("VACUUM") }

	testCases := []struct {
		name       string
		predicate  sqllex.StatementPredicate
		statements []ast.ParsedStatement
		want       bool
	}{
		{name: "no predicate", predicate: nil, statements: []ast.ParsedStatement{statement("VACUUM")}, want: false},
		{name: "a statement that matches", predicate: isVacuum, statements: []ast.ParsedStatement{statement("SELECT 'VACUUM'"), statement("VACUUM")}, want: true},
		{name: "words in comments and strings do not count", predicate: isVacuum,
			statements: []ast.ParsedStatement{statement("SELECT 'VACUUM'"), statement("SELECT 1")}, want: false},
		{name: "no statements", predicate: isVacuum, statements: nil, want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statements := slices.Clone(testCase.statements)
			slices.SortFunc(statements, func(left, right ast.ParsedStatement) int { return cmp.Compare(left.Location, right.Location) })

			assert.Equal(t, testCase.want, statementsRequireNoTransaction(testCase.predicate, tokens, statements))
		})
	}
}

func TestColumnDirectivesAreReadOnceForTheirMigration(t *testing.T) {
	t.Parallel()

	builder, _ := setupBuilderWithTable("users",
		sqlmodel.NewColumn("id", sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "integer"), false),
		sqlmodel.NewColumn("email", sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), false),
	)
	overrides := []syntax.MigrationColumnOverride{
		{Table: "users", Column: "id", GoType: "github.com/google/uuid.UUID", StructTags: `json:"id"`},
		{Table: "users", Column: "email", GoType: "not a type"},
		{Table: "users", Column: "missing", GoType: "string"},
	}

	directives := builder.readColumnDirectives(overrides)

	require.Len(t, directives, 3)
	require.NotNil(t, directives[0].goType)
	assert.Equal(t, "UUID", directives[0].goType.Name)
	assert.Equal(t, map[string]string{"json": "id"}, directives[0].tags)
	assert.Nil(t, directives[1].goType)
	assert.NotEmpty(t, directives[1].goTypeProblem)

	builder.pendingColumnDirectives = directives
	builder.applyPendingColumnOverrides()
	builder.applyPendingColumnOverrides()

	id := builder.catalogue.FindColumn("", "users", "id")
	require.NotNil(t, id.GoTypeOverride)
	assert.NotSame(t, directives[0].goType, id.GoTypeOverride)
	assert.Nil(t, builder.catalogue.FindColumn("", "users", "email").GoTypeOverride)

	diagnostics := builder.applyMigrationColumnOverrides(directives, "001_init.up.sql")
	assert.Equal(t, []string{diag.CodeUnusableGoTypeOverride, diag.CodeUnknownOverrideMigrationColumn}, codesOf(diagnostics))
}

func TestAColumnDirectiveNamesAColumnByTheDialectsIdentifierRules(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		identifiers    sqlmodel.IdentifierCase
		table          string
		column         string
		wantColumn     string
		wantSuggestion string
	}{
		{
			name:        "a quoted mixed-case column where unquoted names fold to lower case",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false},
			table:       "events", column: `"UserID"`, wantColumn: "UserID",
		},
		{
			name:        "an unquoted mixed-case column where names keep their case",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: false},
			table:       "events", column: "UserID", wantColumn: "UserID",
		},
		{
			name:        "an unquoted column folded to lower case",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false},
			table:       "Events", column: "Created_At", wantColumn: "created_at",
		},
		{
			name:        "an unquoted mixed-case column that folds away from it is suggested quoted",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false},
			table:       "events", column: "UserID", wantSuggestion: `events."UserID"`,
		},
		{
			name:        "a misspelt column where names keep their case is suggested as named",
			identifiers: sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: false},
			table:       "events", column: "UserId", wantSuggestion: "events.UserID",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			builder, _ := setupBuilder()
			builder.catalogue.Identifiers = testCase.identifiers
			builder.catalogue.Schemas["public"].Tables["events"] = &sqlmodel.Table{
				Name:   "events",
				Schema: "public",
				Columns: []sqlmodel.Column{
					sqlmodel.NewColumn("UserID", sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), false),
					sqlmodel.NewColumn("created_at", sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), false),
				},
			}
			overrides := []syntax.MigrationColumnOverride{{Table: testCase.table, Column: testCase.column, SQLType: "uuid"}}

			diagnostics := builder.applyMigrationColumnOverrides(builder.readColumnDirectives(overrides), "001_init.up.sql")

			if testCase.wantSuggestion != "" {
				require.Len(t, diagnostics, 1)
				assert.Equal(t, diag.CodeUnknownOverrideMigrationColumn, diagnostics[0].Code)
				assert.Equal(t, testCase.wantSuggestion, diagnostics[0].Suggestion)
				return
			}
			require.Empty(t, diagnostics)
			for _, column := range builder.catalogue.Schemas["public"].Tables["events"].Columns {
				want := ""
				if column.Name == testCase.wantColumn {
					want = "uuid"
				}
				assert.Equal(t, want, column.SQLTypeOverride, column.Name)
			}
		})
	}
}

func TestADeeplyNestedMigrationGoTypeIsRefusedOnceAndQuickly(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		depth int
	}{
		{name: "nested past the type-argument limit", depth: 20},
		{name: "nested four thousand levels deep", depth: 4_000},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			goType := strings.Repeat("example.com/o.Option[", testCase.depth) + "int" + strings.Repeat("]", testCase.depth)
			var migration strings.Builder
			migration.WriteString("CREATE TABLE items (id integer);\n-- sgl.column(items.id, go_type: '" + goType + "')\n")
			for index := range 200 {
				fmt.Fprintf(&migration, "CREATE VIEW items_%d AS SELECT id FROM items;\n", index)
			}

			var diagnostics []diag.SourceError
			scaling.Within(t, 250*time.Millisecond, func(ctx context.Context) {
				builder := newCatalogueBuilder(postgres.New(), namespace.Default())
				var err error
				diagnostics, err = builder.ApplyMigration(ctx, "001_items.up.sql", []byte(migration.String()), 0)
				require.NoError(t, err)
			})

			require.Equal(t, []string{diag.CodeUnusableGoTypeOverride}, codesOf(diagnostics))
			assert.Equal(t, 2, diagnostics[0].Line)
		})
	}
}
