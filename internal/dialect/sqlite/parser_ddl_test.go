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

package sqlite

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
)

func applyDDL(t *testing.T, sql string) *ast.CatalogueMutation {
	t.Helper()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.NotEmpty(t, statements)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)

	return mutation
}

func TestApplyDDL_StrictTableRejectsIllegalDatatype(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, parseError := engine.ParseStatements(t.Context(),
		"CREATE TABLE t (id INTEGER PRIMARY KEY, n SMALLINT) STRICT",
	)
	require.NoError(t, parseError)
	require.NotEmpty(t, statements)

	_, applyError := engine.ApplyDDL(t.Context(), statements[0])
	require.Error(t, applyError)
	assert.ErrorContains(t, applyError, "STRICT table column")
	assert.ErrorContains(t, applyError, "n")
}

func TestApplyDDL_CreateTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple table with columns",
			sql:  "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				require.Len(t, mutation.Columns, 3)

				assert.Equal(t, "id", mutation.Columns[0].Name)
				assert.Equal(t, sqlmodel.TypeCategoryInteger, mutation.Columns[0].SQLType.Category)
				assert.False(t, mutation.Columns[0].Nullable, "primary key column should not be nullable")
				assert.True(t, mutation.Columns[0].HasDefault, "primary key column should have default flag set")

				assert.Equal(t, "name", mutation.Columns[1].Name)
				assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[1].SQLType.Category)
				assert.False(t, mutation.Columns[1].Nullable, "NOT NULL column should not be nullable")

				assert.Equal(t, "email", mutation.Columns[2].Name)
				assert.True(t, mutation.Columns[2].Nullable, "column without NOT NULL should be nullable")

				assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
			},
		},
		{
			name: "WITHOUT ROWID table",
			sql:  "CREATE TABLE kv (key TEXT PRIMARY KEY, value BLOB) WITHOUT ROWID",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "kv", mutation.TableName)
				assert.True(t, mutation.IsWithoutRowID, "should detect WITHOUT ROWID")
				require.Len(t, mutation.Columns, 2)
				assert.Equal(t, "key", mutation.Columns[0].Name)
				assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[0].SQLType.Category)
				assert.Equal(t, []string{"key"}, mutation.PrimaryKey)
			},
		},
		{
			name: "IF NOT EXISTS is accepted",
			sql:  "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				require.Len(t, mutation.Columns, 1)
			},
		},
		{
			name: "type affinity columns",
			sql:  "CREATE TABLE data (a VARCHAR(255), b BIGINT, c DOUBLE, d BOOLEAN, e BLOB)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 5)

				assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[0].SQLType.Category,
					"VARCHAR should have text affinity")

				assert.Equal(t, sqlmodel.TypeCategoryInteger, mutation.Columns[1].SQLType.Category,
					"BIGINT should have integer affinity")

				assert.Equal(t, sqlmodel.TypeCategoryFloat, mutation.Columns[2].SQLType.Category,
					"DOUBLE should have real affinity")

				assert.Equal(t, sqlmodel.TypeCategoryBoolean, mutation.Columns[3].SQLType.Category,
					"BOOLEAN should have boolean category")

				assert.Equal(t, sqlmodel.TypeCategoryBinary, mutation.Columns[4].SQLType.Category,
					"BLOB should have bytea category")
			},
		},
		{
			name: "composite PRIMARY KEY constraint",
			sql:  "CREATE TABLE membership (user_id INTEGER, group_id INTEGER, PRIMARY KEY (user_id, group_id))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
				assert.Equal(t, []string{"user_id", "group_id"}, mutation.PrimaryKey)
			},
		},
		{
			name: "AUTOINCREMENT column",
			sql:  "CREATE TABLE events (id INTEGER PRIMARY KEY AUTOINCREMENT, payload TEXT)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
				assert.Equal(t, "id", mutation.Columns[0].Name)
				assert.False(t, mutation.Columns[0].Nullable, "AUTOINCREMENT column should not be nullable")
				assert.True(t, mutation.Columns[0].HasDefault, "AUTOINCREMENT column should have default flag set")
				assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
			},
		},
		{
			name: "CREATE TABLE AS returns mutation without columns",
			sql:  "CREATE TABLE archive AS SELECT id, name FROM users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "archive", mutation.TableName)

				assert.Empty(t, mutation.Columns, "CREATE TABLE AS should have no explicit columns")
			},
		},
		{
			name: "column with DEFAULT expression",
			sql:  "CREATE TABLE t (x INTEGER DEFAULT 0, y TEXT DEFAULT 'hello')",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
				assert.True(t, mutation.Columns[0].HasDefault, "column with DEFAULT should have HasDefault set")
				assert.True(t, mutation.Columns[1].HasDefault, "column with DEFAULT should have HasDefault set")
			},
		},
		{
			name: "GENERATED ALWAYS AS stored column",
			sql:  "CREATE TABLE products (price REAL, tax REAL, total REAL GENERATED ALWAYS AS (price + tax) STORED)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 3)
				assert.True(t, mutation.Columns[2].IsGenerated, "generated column should be marked generated")
				assert.Equal(t, sqlmodel.GeneratedKindStored, mutation.Columns[2].GeneratedKind)
			},
		},
		{
			name: "GENERATED ALWAYS AS virtual column",
			sql:  "CREATE TABLE items (a INTEGER, b INTEGER, c AS (a * b) VIRTUAL)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 3)
				assert.True(t, mutation.Columns[2].IsGenerated, "virtual generated column should be marked generated")
				assert.Equal(t, sqlmodel.GeneratedKindVirtual, mutation.Columns[2].GeneratedKind)
			},
		},
		{
			name: "CHECK constraint on table",
			sql:  "CREATE TABLE orders (id INTEGER PRIMARY KEY, amount REAL, CHECK (amount > 0))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintCheck, mutation.Constraints[0].Kind)
			},
		},
		{
			name: "multiple UNIQUE constraints",
			sql:  "CREATE TABLE accounts (id INTEGER PRIMARY KEY, email TEXT, username TEXT, UNIQUE (email), UNIQUE (username))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Constraints, 2)
				assert.Equal(t, sqlmodel.ConstraintUnique, mutation.Constraints[0].Kind)
				assert.Equal(t, []string{"email"}, mutation.Constraints[0].Columns)
				assert.Equal(t, sqlmodel.ConstraintUnique, mutation.Constraints[1].Kind)
				assert.Equal(t, []string{"username"}, mutation.Constraints[1].Columns)
			},
		},
		{
			name: "FOREIGN KEY table constraint",
			sql:  "CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER, FOREIGN KEY (user_id) REFERENCES users (id))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[0].Kind)
				assert.Equal(t, "users", mutation.Constraints[0].ForeignTable)
				assert.Equal(t, []string{"user_id"}, mutation.Constraints[0].Columns)
				assert.Equal(t, []string{"id"}, mutation.Constraints[0].ForeignColumns)
			},
		},
		{
			name: "column with COLLATE constraint",
			sql:  "CREATE TABLE names (id INTEGER PRIMARY KEY, name TEXT COLLATE NOCASE)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
				assert.Equal(t, "name", mutation.Columns[1].Name)
			},
		},
		{
			name: "column with REFERENCES constraint",
			sql:  "CREATE TABLE comments (id INTEGER PRIMARY KEY, post_id INTEGER REFERENCES posts (id))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
				assert.Equal(t, "post_id", mutation.Columns[1].Name)
			},
		},
		{
			name: "STRICT table mode widens integers to 64-bit",
			sql:  "CREATE TABLE strict_tbl (id INTEGER PRIMARY KEY, n INTEGER NOT NULL, val TEXT) STRICT",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "strict_tbl", mutation.TableName)
				require.Len(t, mutation.Columns, 3)

				assert.Equal(t, "bigint", mutation.Columns[0].SQLType.EngineName, "STRICT rowid alias is 64-bit")
				assert.Equal(t, "bigint", mutation.Columns[1].SQLType.EngineName, "STRICT INTEGER column is 64-bit")
				assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[2].SQLType.Category)
			},
		},
		{
			name: "STRICT table allows an ANY column",
			sql:  "CREATE TABLE strict_any (id INTEGER PRIMARY KEY, payload ANY) STRICT",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)

				assert.Equal(t, "payload", mutation.Columns[1].Name)
				assert.Equal(t, sqlmodel.TypeCategoryUnknown, mutation.Columns[1].SQLType.Category)
			},
		},
		{
			name: "named CONSTRAINT on table",
			sql:  "CREATE TABLE t (x INTEGER, CONSTRAINT pk_x PRIMARY KEY (x))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, []string{"x"}, mutation.PrimaryKey)
			},
		},
		{
			name: "DEFAULT with parenthesised expression",
			sql:  "CREATE TABLE t (created_at TEXT DEFAULT (datetime('now')))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.True(t, mutation.Columns[0].HasDefault, "DEFAULT with parenthesised expression should set HasDefault")
			},
		},
		{
			name: "column with UNIQUE keyword constraint",
			sql:  "CREATE TABLE t (code TEXT UNIQUE NOT NULL)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.False(t, mutation.Columns[0].Nullable, "NOT NULL after UNIQUE should set Nullable=false")
			},
		},
		{
			name: "column with CHECK constraint",
			sql:  "CREATE TABLE t (age INTEGER CHECK (age >= 0 AND age <= 200))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "age", mutation.Columns[0].Name)
			},
		},
		{
			name: "DEFAULT with negative numeric literal",
			sql:  "CREATE TABLE t (balance REAL DEFAULT -0.01)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.True(t, mutation.Columns[0].HasDefault, "DEFAULT with negative literal should set HasDefault")
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_CreateTableSchemaQualified(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE TABLE main.events (id INTEGER PRIMARY KEY, name TEXT)")
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	assert.Equal(t, "events", mutation.TableName)
	require.Len(t, mutation.Columns, 2)
}

func TestApplyDDL_CreateVirtualTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "FTS5 virtual table extracts content columns and rank",
			sql:  "CREATE VIRTUAL TABLE documents USING fts5(title, body, tokenize='porter')",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "documents", mutation.TableName)
				assert.True(t, mutation.IsVirtual, "should be marked as virtual")
				assert.Equal(t, "fts5", mutation.VirtualModuleName)

				require.Len(t, mutation.Columns, 4)
				assert.Equal(t, "title", mutation.Columns[0].Name)
				assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[0].SQLType.Category)
				assert.False(t, mutation.Columns[0].Hidden)
				assert.Equal(t, "body", mutation.Columns[1].Name)
				assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[1].SQLType.Category)
				assert.Equal(t, "rank", mutation.Columns[2].Name)
				assert.True(t, mutation.Columns[2].IsGenerated, "rank column should be generated")
				assert.Equal(t, sqlmodel.GeneratedKindVirtual, mutation.Columns[2].GeneratedKind)
				assert.True(t, mutation.Columns[2].Hidden, "rank is hidden from SELECT *")
				assert.Equal(t, "documents", mutation.Columns[3].Name)
				assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[3].SQLType.Category)
				assert.True(t, mutation.Columns[3].Hidden, "the table-named column is hidden from SELECT *")
			},
		},
		{
			name: "FTS5 with IF NOT EXISTS",
			sql:  "CREATE VIRTUAL TABLE IF NOT EXISTS search USING fts5(content)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "search", mutation.TableName)
				assert.True(t, mutation.IsVirtual)
				assert.Equal(t, "fts5", mutation.VirtualModuleName)

				require.Len(t, mutation.Columns, 3)
				assert.Equal(t, "content", mutation.Columns[0].Name)
			},
		},
		{
			name: "RTree virtual table",
			sql:  "CREATE VIRTUAL TABLE spatial USING rtree(id, min_x, max_x, min_y, max_y)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "spatial", mutation.TableName)
				assert.True(t, mutation.IsVirtual)
				assert.Equal(t, "rtree", mutation.VirtualModuleName)
				require.Len(t, mutation.Columns, 5)
				assert.Equal(t, "id", mutation.Columns[0].Name)
				assert.False(t, mutation.Columns[0].Nullable, "rtree id column should not be nullable")
				assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
			},
		},
		{
			name: "generic virtual table module",
			sql:  "CREATE VIRTUAL TABLE my_table USING custom_module(col1, col2)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "my_table", mutation.TableName)
				assert.True(t, mutation.IsVirtual)
				assert.Equal(t, "custom_module", mutation.VirtualModuleName)
				require.Len(t, mutation.Columns, 2)
				assert.Equal(t, "col1", mutation.Columns[0].Name)
				assert.Equal(t, "col2", mutation.Columns[1].Name)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_AlterTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "ADD COLUMN",
			sql:  "ALTER TABLE users ADD COLUMN email TEXT",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "email", mutation.Columns[0].Name)
				assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[0].SQLType.Category)
			},
		},
		{
			name: "ADD COLUMN without COLUMN keyword",
			sql:  "ALTER TABLE users ADD age INTEGER",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "age", mutation.Columns[0].Name)
				assert.Equal(t, sqlmodel.TypeCategoryInteger, mutation.Columns[0].SQLType.Category)
			},
		},
		{
			name: "RENAME TO",
			sql:  "ALTER TABLE users RENAME TO accounts",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableRenameTable, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				assert.Equal(t, "accounts", mutation.NewName)
			},
		},
		{
			name: "RENAME COLUMN",
			sql:  "ALTER TABLE users RENAME COLUMN name TO full_name",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableRenameColumn, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				assert.Equal(t, "name", mutation.ColumnName)
				assert.Equal(t, "full_name", mutation.NewName)
			},
		},
		{
			name: "DROP COLUMN",
			sql:  "ALTER TABLE users DROP COLUMN email",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableDropColumn, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				assert.Equal(t, "email", mutation.ColumnName)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_DropTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple DROP TABLE",
			sql:  "DROP TABLE users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropTable, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "DROP TABLE IF EXISTS",
			sql:  "DROP TABLE IF EXISTS users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropTable, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_CreateView(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple view",
			sql:  "CREATE VIEW active_users AS SELECT id, name FROM users WHERE active = 1",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateView, mutation.Kind)
				assert.Equal(t, "active_users", mutation.TableName)
			},
		},
		{
			name: "view with explicit column list",
			sql:  "CREATE VIEW user_names (user_id, user_name) AS SELECT id, name FROM users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateView, mutation.Kind)
				assert.Equal(t, "user_names", mutation.TableName)
				require.Len(t, mutation.Columns, 2)
				assert.Equal(t, "user_id", mutation.Columns[0].Name)
				assert.Equal(t, "user_name", mutation.Columns[1].Name)
			},
		},
		{
			name: "temporary view with IF NOT EXISTS",
			sql:  "CREATE TEMP VIEW IF NOT EXISTS tmp_view AS SELECT 1",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateView, mutation.Kind)
				assert.Equal(t, "tmp_view", mutation.TableName)
			},
		},
		{
			name: "view with complex SELECT including JOIN",
			sql:  "CREATE VIEW user_posts AS SELECT u.name, p.title FROM users u JOIN posts p ON p.user_id = u.id",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateView, mutation.Kind)
				assert.Equal(t, "user_posts", mutation.TableName)
			},
		},
		{
			name: "view body using VALUES is analysed",
			sql:  "CREATE VIEW v AS VALUES (1, 'a')",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateView, mutation.Kind)
				assert.Equal(t, "v", mutation.TableName)
				require.NotNil(t, mutation.ViewDefinition)
				assert.Len(t, mutation.ViewDefinition.OutputColumns, 2)
				assert.Empty(t, mutation.UnanalysedParts)
			},
		},
		{
			name: "view declared column list longer than projection does not panic",
			sql:  "CREATE VIEW v (a, b, c) AS SELECT 1",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateView, mutation.Kind)
				assert.Equal(t, "v", mutation.TableName)
				require.Len(t, mutation.Columns, 3)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_DropView(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple DROP VIEW",
			sql:  "DROP VIEW active_users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropView, mutation.Kind)
				assert.Equal(t, "active_users", mutation.TableName)
			},
		},
		{
			name: "DROP VIEW IF EXISTS",
			sql:  "DROP VIEW IF EXISTS active_users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropView, mutation.Kind)
				assert.Equal(t, "active_users", mutation.TableName)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_CreateIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple index",
			sql:  "CREATE INDEX idx_users_name ON users (name)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateIndex, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "unique index",
			sql:  "CREATE UNIQUE INDEX idx_users_email ON users (email)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateIndex, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "index with IF NOT EXISTS",
			sql:  "CREATE INDEX IF NOT EXISTS idx_users_name ON users (name)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateIndex, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "index on multiple columns",
			sql:  "CREATE INDEX idx_users_name_email ON users (name, email)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateIndex, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_DropIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple DROP INDEX",
			sql:  "DROP INDEX idx_users_name",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropIndex, mutation.Kind)
			},
		},
		{
			name: "DROP INDEX IF EXISTS",
			sql:  "DROP INDEX IF EXISTS idx_users_name",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropIndex, mutation.Kind)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_CreateTrigger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "BEFORE INSERT trigger",
			sql:  "CREATE TRIGGER trg_before_insert BEFORE INSERT ON users BEGIN SELECT 1; END",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTrigger, mutation.Kind)
				assert.Equal(t, "trg_before_insert", mutation.TriggerName)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "AFTER UPDATE trigger",
			sql:  "CREATE TRIGGER trg_after_update AFTER UPDATE ON posts BEGIN SELECT 1; END",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTrigger, mutation.Kind)
				assert.Equal(t, "trg_after_update", mutation.TriggerName)
				assert.Equal(t, "posts", mutation.TableName)
			},
		},
		{
			name: "trigger with IF NOT EXISTS",
			sql:  "CREATE TRIGGER IF NOT EXISTS trg_audit AFTER DELETE ON users BEGIN SELECT 1; END",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTrigger, mutation.Kind)
				assert.Equal(t, "trg_audit", mutation.TriggerName)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_DropTrigger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple DROP TRIGGER",
			sql:  "DROP TRIGGER trg_before_insert",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropTrigger, mutation.Kind)
				assert.Equal(t, "trg_before_insert", mutation.TriggerName)
			},
		},
		{
			name: "DROP TRIGGER IF EXISTS",
			sql:  "DROP TRIGGER IF EXISTS trg_before_insert",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropTrigger, mutation.Kind)
				assert.Equal(t, "trg_before_insert", mutation.TriggerName)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_CreateTrigger_Variants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "INSTEAD OF trigger on view",
			sql:  "CREATE TRIGGER trg_instead INSTEAD OF INSERT ON users BEGIN SELECT 1; END",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTrigger, mutation.Kind)
				assert.Equal(t, "trg_instead", mutation.TriggerName)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "UPDATE OF columns trigger",
			sql:  "CREATE TRIGGER trg_update_of AFTER UPDATE OF name, email ON users BEGIN SELECT 1; END",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTrigger, mutation.Kind)
				assert.Equal(t, "trg_update_of", mutation.TriggerName)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "TEMP TRIGGER",
			sql:  "CREATE TEMP TRIGGER trg_temp BEFORE DELETE ON users BEGIN SELECT 1; END",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTrigger, mutation.Kind)
				assert.Equal(t, "trg_temp", mutation.TriggerName)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestATableConstraintTakesAConflictClause(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		sql             string
		wantPrimaryKey  []string
		wantConstraints []sqlmodel.ConstraintKind
	}{
		{
			name:            "a unique constraint over two columns replacing on conflict",
			sql:             "CREATE TABLE t (a TEXT NOT NULL, b TEXT NOT NULL, UNIQUE (a, b) ON CONFLICT REPLACE)",
			wantPrimaryKey:  nil,
			wantConstraints: []sqlmodel.ConstraintKind{sqlmodel.ConstraintUnique},
		},
		{
			name: "a named unique constraint ignoring conflicts, then a primary key aborting on them",
			sql: "CREATE TABLE t (a TEXT, b TEXT, CONSTRAINT pair UNIQUE (a, b) ON CONFLICT IGNORE, " +
				"PRIMARY KEY (a) ON CONFLICT ABORT)",
			wantPrimaryKey:  []string{"a"},
			wantConstraints: []sqlmodel.ConstraintKind{sqlmodel.ConstraintUnique},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			require.Equal(t, []string{"a", "b"}, []string{mutation.Columns[0].Name, mutation.Columns[1].Name})
			require.Equal(t, testCase.wantPrimaryKey, mutation.PrimaryKey)
			kinds := make([]sqlmodel.ConstraintKind, 0, len(mutation.Constraints))
			for _, constraint := range mutation.Constraints {
				kinds = append(kinds, constraint.Kind)
			}
			require.Equal(t, testCase.wantConstraints, kinds)
		})
	}
}

func TestApplyDDL_CreateTable_ColumnConstraints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "column with ON CONFLICT on NOT NULL",
			sql:  "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT NOT NULL ON CONFLICT REPLACE)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
				assert.False(t, mutation.Columns[1].Nullable)
			},
		},
		{
			name: "column with ON CONFLICT on UNIQUE",
			sql:  "CREATE TABLE t (id INTEGER PRIMARY KEY, code TEXT UNIQUE ON CONFLICT IGNORE)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
			},
		},
		{
			name: "column with CONSTRAINT name prefix",
			sql:  "CREATE TABLE t (id INTEGER, CONSTRAINT pk PRIMARY KEY (id))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
			},
		},
		{
			name: "column with inline REFERENCES with ON DELETE CASCADE",
			sql:  "CREATE TABLE t (user_id INTEGER REFERENCES users (id) ON DELETE CASCADE)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "user_id", mutation.Columns[0].Name)
			},
		},
		{
			name: "FOREIGN KEY with ON UPDATE and ON DELETE",
			sql:  "CREATE TABLE t (id INTEGER, uid INTEGER, FOREIGN KEY (uid) REFERENCES users (id) ON DELETE SET NULL ON UPDATE CASCADE)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[0].Kind)
				assert.Equal(t, "users", mutation.Constraints[0].ForeignTable)
				assert.Equal(t, []string{"id"}, mutation.Constraints[0].ForeignColumns)

				require.Len(t, mutation.Columns, 2, "ON DELETE clause leaked into the column list as a phantom column")
			},
		},
		{

			name: "two table-level FOREIGN KEYs with ON DELETE CASCADE",
			sql: `CREATE TABLE identity_oauth_tokens (
                id TEXT PRIMARY KEY,
                account_id TEXT NOT NULL,
                authentication_method_id TEXT NOT NULL,
                created_at TEXT NOT NULL,
                FOREIGN KEY (account_id) REFERENCES identity_accounts (id) ON DELETE CASCADE,
                FOREIGN KEY (authentication_method_id) REFERENCES identity_authentication_methods (id) ON DELETE CASCADE
            )`,
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 4, "ON DELETE CASCADE was leaked into the column list")
				columnNames := make([]string, len(mutation.Columns))
				for index, column := range mutation.Columns {
					columnNames[index] = column.Name
				}
				assert.Equal(t, []string{"id", "account_id", "authentication_method_id", "created_at"}, columnNames)
				require.Len(t, mutation.Constraints, 2)
				assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[0].Kind)
				assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[1].Kind)
			},
		},
		{

			name: "inline REFERENCES ON DELETE CASCADE then trailing columns",
			sql: `CREATE TABLE t (
                id TEXT PRIMARY KEY,
                user_id TEXT REFERENCES users (id) ON DELETE CASCADE,
                created_at TEXT NOT NULL
            )`,
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 3)
				columnNames := make([]string, len(mutation.Columns))
				for index, column := range mutation.Columns {
					columnNames[index] = column.Name
				}
				assert.Equal(t, []string{"id", "user_id", "created_at"}, columnNames)
			},
		},
		{
			name: "column with type modifiers applied to text",
			sql:  "CREATE TABLE t (name VARCHAR(100))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[0].SQLType.Category)
				require.NotNil(t, mutation.Columns[0].SQLType.Length, "text type with modifier should set Length")
				assert.Equal(t, 100, *mutation.Columns[0].SQLType.Length)
			},
		},
		{
			name: "column with DECIMAL precision and scale modifiers",
			sql:  "CREATE TABLE t (amount DECIMAL(10, 2))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, sqlmodel.TypeCategoryDecimal, mutation.Columns[0].SQLType.Category)
				require.NotNil(t, mutation.Columns[0].SQLType.Precision, "DECIMAL should set Precision")
				assert.Equal(t, 10, *mutation.Columns[0].SQLType.Precision)
				require.NotNil(t, mutation.Columns[0].SQLType.Scale, "DECIMAL(p,s) should set Scale")
				assert.Equal(t, 2, *mutation.Columns[0].SQLType.Scale)
			},
		},
		{
			name: "column with no explicit type defaults to blob",
			sql:  "CREATE TABLE t (id INTEGER PRIMARY KEY, data)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)

				assert.Equal(t, sqlmodel.TypeCategoryBinary, mutation.Columns[1].SQLType.Category)
			},
		},
		{
			name: "named CHECK constraint on table",
			sql:  "CREATE TABLE t (x INTEGER, CONSTRAINT chk_positive CHECK (x > 0))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintCheck, mutation.Constraints[0].Kind)
				assert.Equal(t, "chk_positive", mutation.Constraints[0].Name)
			},
		},
		{
			name: "named UNIQUE constraint on table",
			sql:  "CREATE TABLE t (email TEXT, CONSTRAINT uq_email UNIQUE (email))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintUnique, mutation.Constraints[0].Kind)
				assert.Equal(t, "uq_email", mutation.Constraints[0].Name)
				assert.Equal(t, []string{"email"}, mutation.Constraints[0].Columns)
			},
		},
		{
			name: "named FOREIGN KEY constraint",
			sql:  "CREATE TABLE t (uid INTEGER, CONSTRAINT fk_user FOREIGN KEY (uid) REFERENCES users (id))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[0].Kind)
				assert.Equal(t, "fk_user", mutation.Constraints[0].Name)
			},
		},
		{
			name: "multi-word type name",
			sql:  "CREATE TABLE t (x DOUBLE PRECISION)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)

				assert.Equal(t, sqlmodel.TypeCategoryFloat, mutation.Columns[0].SQLType.Category)
			},
		},
		{
			name: "PRIMARY KEY ASC column constraint",
			sql:  "CREATE TABLE t (id INTEGER PRIMARY KEY ASC)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
			},
		},
		{
			name: "PRIMARY KEY DESC column constraint",
			sql:  "CREATE TABLE t (id INTEGER PRIMARY KEY DESC)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_AlterTable_ImplicitColumnKeyword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "RENAME COLUMN without COLUMN keyword",
			sql:  "ALTER TABLE users RENAME name TO full_name",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableRenameColumn, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				assert.Equal(t, "name", mutation.ColumnName)
				assert.Equal(t, "full_name", mutation.NewName)
			},
		},
		{
			name: "DROP COLUMN without COLUMN keyword",
			sql:  "ALTER TABLE users DROP email",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableDropColumn, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				assert.Equal(t, "email", mutation.ColumnName)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_CreateVirtualTable_ModuleArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "FTS5 with content option using equals syntax",
			sql:  "CREATE VIRTUAL TABLE search USING fts5(title, body, content=posts, content_rowid=id)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.True(t, mutation.IsVirtual)
				assert.Equal(t, "fts5", mutation.VirtualModuleName)

				require.Len(t, mutation.Columns, 4)
				assert.Equal(t, "title", mutation.Columns[0].Name)
				assert.Equal(t, "body", mutation.Columns[1].Name)
				assert.Equal(t, "rank", mutation.Columns[2].Name)
				assert.Equal(t, "search", mutation.Columns[3].Name)
			},
		},
		{
			name: "generic virtual table with key=value options skipped",
			sql:  "CREATE VIRTUAL TABLE vt USING mymod(col1, opt1=val1, col2)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.True(t, mutation.IsVirtual)

				require.Len(t, mutation.Columns, 2)
				assert.Equal(t, "col1", mutation.Columns[0].Name)
				assert.Equal(t, "col2", mutation.Columns[1].Name)
			},
		},
		{
			name: "virtual table without arguments",
			sql:  "CREATE VIRTUAL TABLE vt USING mymod",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.True(t, mutation.IsVirtual)
				assert.Equal(t, "mymod", mutation.VirtualModuleName)
				assert.Empty(t, mutation.Columns)
			},
		},
		{
			name: "rtree_i32 virtual table",
			sql:  "CREATE VIRTUAL TABLE coords USING rtree_i32(id, x0, x1, y0, y1)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.True(t, mutation.IsVirtual)
				assert.Equal(t, "rtree_i32", mutation.VirtualModuleName)
				require.Len(t, mutation.Columns, 5)
				assert.Equal(t, "id", mutation.Columns[0].Name)
				assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			testCase.assertions(t, mutation)
		})
	}
}

func TestApplyDDL_NonDDL(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "SELECT 1")

	assert.Nil(t, mutation, "non-DDL statement should return nil mutation")
}

func TestEngine_Accessors(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("SupportedDirectivePrefixes", func(t *testing.T) {
		t.Parallel()
		prefixes := engine.Info().Placeholders.Sigils
		require.NotEmpty(t, prefixes)
	})

	t.Run("SupportedExpressions", func(t *testing.T) {
		t.Parallel()
		features := engine.Info().Expressions
		assert.NotZero(t, features)
	})

	t.Run("TableValuedFunctionColumns known", func(t *testing.T) {
		t.Parallel()
		columns := engine.TableValuedFunctionColumns("json_each")
		require.NotEmpty(t, columns)
	})

	t.Run("TableValuedFunctionColumns unknown", func(t *testing.T) {
		t.Parallel()
		columns := engine.TableValuedFunctionColumns("nonexistent_function")
		assert.Nil(t, columns)
	})

	t.Run("CommentStyle", func(t *testing.T) {
		t.Parallel()
		style := engine.Info().Comments
		assert.NotZero(t, style)
	})
}

func TestApplyDDL_PrimaryKeyDefaultFollowsTheRowidAlias(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		column         string
		wantEngineName string
		wantHasDefault bool
	}{
		{
			name:           "inline INTEGER PRIMARY KEY is the rowid alias",
			sql:            "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)",
			column:         "id",
			wantEngineName: "bigint",
			wantHasDefault: true,
		},
		{
			name:           "table-level PRIMARY KEY on an INTEGER column is the rowid alias",
			sql:            "CREATE TABLE t (id INTEGER NOT NULL, name TEXT, PRIMARY KEY (id))",
			column:         "id",
			wantEngineName: "bigint",
			wantHasDefault: true,
		},
		{
			name:           "table-level descending PRIMARY KEY is still the rowid alias",
			sql:            "CREATE TABLE t (id INTEGER, PRIMARY KEY (id DESC))",
			column:         "id",
			wantEngineName: "bigint",
			wantHasDefault: true,
		},
		{
			name:           "AUTOINCREMENT key is the rowid alias",
			sql:            "CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT)",
			column:         "id",
			wantEngineName: "bigint",
			wantHasDefault: true,
		},
		{
			name:           "inline TEXT PRIMARY KEY has no default",
			sql:            "CREATE TABLE t (code TEXT PRIMARY KEY, label TEXT)",
			column:         "code",
			wantEngineName: "text",
			wantHasDefault: false,
		},
		{
			name:           "inline INT PRIMARY KEY is not the rowid alias",
			sql:            "CREATE TABLE t (id INT PRIMARY KEY)",
			column:         "id",
			wantEngineName: "integer",
			wantHasDefault: false,
		},
		{
			name:           "inline descending INTEGER PRIMARY KEY is not the rowid alias",
			sql:            "CREATE TABLE t (id INTEGER PRIMARY KEY DESC)",
			column:         "id",
			wantEngineName: "integer",
			wantHasDefault: false,
		},
		{
			name:           "WITHOUT ROWID table has no rowid alias",
			sql:            "CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT) WITHOUT ROWID",
			column:         "id",
			wantEngineName: "integer",
			wantHasDefault: false,
		},
		{
			name:           "composite primary key has no rowid alias",
			sql:            "CREATE TABLE t (a INTEGER, b INTEGER, PRIMARY KEY (a, b))",
			column:         "a",
			wantEngineName: "integer",
			wantHasDefault: false,
		},
		{
			name:           "declared default still counts",
			sql:            "CREATE TABLE t (code TEXT PRIMARY KEY DEFAULT 'x')",
			column:         "code",
			wantEngineName: "text",
			wantHasDefault: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)

			var column *sqlmodel.Column
			for index := range mutation.Columns {
				if mutation.Columns[index].Name == testCase.column {
					column = &mutation.Columns[index]
				}
			}
			require.NotNil(t, column)
			assert.Equal(t, testCase.wantEngineName, column.SQLType.EngineName)
			assert.Equal(t, testCase.wantHasDefault, column.HasDefault)
		})
	}
}

func TestApplyDDL_DelimitedReservedWordsAreColumns(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, `CREATE TABLE "order" (id INTEGER PRIMARY KEY, "check" INTEGER NOT NULL, `+
		"`constraint` TEXT NOT NULL, [primary] TEXT, \"unique\" INTEGER, \"foreign\" TEXT, \"key\" TEXT)")
	require.NotNil(t, mutation)

	assert.Equal(t, "order", mutation.TableName)
	names := make([]string, 0, len(mutation.Columns))
	for _, column := range mutation.Columns {
		names = append(names, column.Name)
	}
	assert.Equal(t, []string{"id", "check", "constraint", "primary", "unique", "foreign", "key"}, names)
	assert.Empty(t, mutation.Constraints)
	assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
}

func TestApplyDDL_RTreeColumnsAreNotNullable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		sql                string
		wantCoordinateType string
	}{
		{name: "rtree stores real coordinates", sql: "CREATE VIRTUAL TABLE boxes USING rtree(id, min_x, max_x)", wantCoordinateType: "real"},
		{name: "rtree_i32 stores integer coordinates", sql: "CREATE VIRTUAL TABLE boxes USING rtree_i32(id, min_x, max_x)", wantCoordinateType: "integer"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			require.Len(t, mutation.Columns, 3)

			identifier := mutation.Columns[0]
			assert.Equal(t, "id", identifier.Name)
			assert.Equal(t, "bigint", identifier.SQLType.EngineName)
			assert.False(t, identifier.Nullable)
			assert.True(t, identifier.HasDefault)

			for _, coordinate := range mutation.Columns[1:] {
				assert.Equal(t, testCase.wantCoordinateType, coordinate.SQLType.EngineName)
				assert.False(t, coordinate.Nullable, "R*Tree coordinate %s is never NULL", coordinate.Name)
			}
		})
	}
}

func TestApplyDDL_RTreeAuxiliaryColumnsKeepTheirType(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE VIRTUAL TABLE boxes USING rtree(id, min_x, max_x, +label TEXT, +weight)")
	require.NotNil(t, mutation)
	require.Len(t, mutation.Columns, 5)

	label := mutation.Columns[3]
	assert.Equal(t, "label", label.Name)
	assert.Equal(t, "text", label.SQLType.EngineName)
	assert.True(t, label.Nullable)

	weight := mutation.Columns[4]
	assert.Equal(t, "weight", weight.Name)
	assert.True(t, weight.Nullable)
}

func TestApplyDDL_PrimaryKeyColumnsAreNullableAsSQLiteKeepsThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantNullable []bool
	}{
		{
			name:         "a table-level key of a rowid table holds NULL",
			sql:          "CREATE TABLE memberships (user_id INTEGER, group_id INTEGER, note TEXT, PRIMARY KEY (group_id, user_id))",
			wantNullable: []bool{true, true, true},
		},
		{
			name:         "a text key of a rowid table holds NULL",
			sql:          "CREATE TABLE plain (code TEXT PRIMARY KEY, note TEXT)",
			wantNullable: []bool{true, true},
		},
		{
			name:         "a key declared NOT NULL does not",
			sql:          "CREATE TABLE plain (code TEXT NOT NULL PRIMARY KEY, note TEXT)",
			wantNullable: []bool{false, true},
		},
		{
			name:         "the key of a WITHOUT ROWID table does not",
			sql:          "CREATE TABLE memberships (user_id INTEGER, group_id INTEGER, note TEXT, PRIMARY KEY (group_id, user_id)) WITHOUT ROWID",
			wantNullable: []bool{false, false, true},
		},
		{
			name:         "the key of a STRICT table does not",
			sql:          "CREATE TABLE plain (code TEXT PRIMARY KEY, note TEXT) STRICT",
			wantNullable: []bool{false, true},
		},
		{
			name:         "the rowid alias does not",
			sql:          "CREATE TABLE numbered (id INTEGER PRIMARY KEY, note TEXT)",
			wantNullable: []bool{false, true},
		},
		{
			name:         "a table-level key on one INTEGER column is the rowid alias",
			sql:          "CREATE TABLE numbered (id INTEGER, note TEXT, PRIMARY KEY (id))",
			wantNullable: []bool{false, true},
		},
		{
			name:         "INTEGER PRIMARY KEY DESC is not the rowid alias",
			sql:          "CREATE TABLE numbered (id INTEGER PRIMARY KEY DESC, note TEXT)",
			wantNullable: []bool{true, true},
		},
		{
			name:         "INT PRIMARY KEY is not the rowid alias",
			sql:          "CREATE TABLE numbered (id INT PRIMARY KEY, note TEXT)",
			wantNullable: []bool{true, true},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			nullable := make([]bool, 0, len(mutation.Columns))
			for _, column := range mutation.Columns {
				nullable = append(nullable, column.Nullable)
			}
			require.Equal(t, testCase.wantNullable, nullable)
		})
	}
}

func TestApplyDDL_CreateTableElementsEndInACommaOrTheClosingParenthesis(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (id INTEGER PRIMARY KEY stray words, name TEXT)")
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])

	require.ErrorIs(t, err, fault.ErrParse)
	assert.Nil(t, mutation)
	var parseError *dialectkit.ParseError
	require.ErrorAs(t, err, &parseError)
	assert.Equal(t, "stray", parseError.Token)
}

func TestApplyDDL_RecordsHowAStatementReplays(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		check func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "CREATE TABLE IF NOT EXISTS",
			sql:  "CREATE TABLE IF NOT EXISTS items (id INTEGER PRIMARY KEY)",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.True(t, mutation.IfNotExists)
			},
		},
		{
			name: "CREATE VIEW IF NOT EXISTS carries only its declared names",
			sql:  "CREATE VIEW IF NOT EXISTS v AS SELECT *, 1 AS one FROM items",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.True(t, mutation.IfNotExists)
				require.NotNil(t, mutation.ViewDefinition)
				require.Empty(t, mutation.Columns)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			testCase.check(t, applyDDL(t, testCase.sql))
		})
	}
}

func TestApplyDDL_RejectsAStatementThatRunsOnIntoTheNext(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		runsOnto string
	}{
		{name: "a CREATE TABLE missing its semicolon", sql: "CREATE TABLE a (id INTEGER)\nCREATE TABLE b (id INTEGER)", runsOnto: "CREATE"},
		{name: "an ALTER TABLE dropping a column named index", sql: "ALTER TABLE t\n  DROP index"},
		{name: "a trigger whose body holds statements", sql: "CREATE TRIGGER t AFTER INSERT ON a\nBEGIN\nDELETE FROM b;\nEND"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.NotEmpty(t, statements)
			_, applyError := engine.ApplyDDL(t.Context(), statements[0])
			if testCase.runsOnto == "" {
				if applyError != nil {
					require.NotContains(t, applyError.Error(), "semicolon is missing")
				}
				return
			}
			require.ErrorContains(t, applyError, "a semicolon is missing before "+testCase.runsOnto)
		})
	}
}

func TestApplyDDL_TableOptionsAreReadAsACommaSeparatedList(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		sql              string
		wantWithoutRowID bool
		wantStrict       bool
	}{
		{name: "STRICT then WITHOUT ROWID", sql: "CREATE TABLE t (a INT PRIMARY KEY) STRICT, WITHOUT ROWID", wantWithoutRowID: true, wantStrict: true},
		{name: "WITHOUT ROWID then STRICT", sql: "CREATE TABLE t (a INT PRIMARY KEY) WITHOUT ROWID, STRICT", wantWithoutRowID: true, wantStrict: true},
		{name: "STRICT alone", sql: "CREATE TABLE t (a INT PRIMARY KEY) STRICT", wantWithoutRowID: false, wantStrict: true},
		{name: "WITHOUT ROWID alone", sql: "CREATE TABLE t (a INT PRIMARY KEY) WITHOUT ROWID", wantWithoutRowID: true, wantStrict: false},
		{name: "no options", sql: "CREATE TABLE t (a INT PRIMARY KEY)", wantWithoutRowID: false, wantStrict: false},
		{name: "a list opening with a comma, as SQLite reads it", sql: "CREATE TABLE t (a INT PRIMARY KEY), STRICT", wantWithoutRowID: false, wantStrict: true},
		{name: "a repeated option, as SQLite reads it", sql: "CREATE TABLE t (a INT PRIMARY KEY) STRICT, STRICT", wantWithoutRowID: false, wantStrict: true},
		{name: "options in lower case", sql: "CREATE TABLE t (a INT PRIMARY KEY) strict, without rowid", wantWithoutRowID: true, wantStrict: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation)
			assert.Equal(t, testCase.wantWithoutRowID, mutation.IsWithoutRowID)
			require.Len(t, mutation.Columns, 1)
			wantEngineName := "integer"
			if testCase.wantStrict {
				wantEngineName = typeNameBigint
			}
			assert.Equal(t, wantEngineName, mutation.Columns[0].SQLType.EngineName)
			assert.Equal(t, testCase.wantStrict || testCase.wantWithoutRowID, !mutation.Columns[0].Nullable)
		})
	}
}

func TestApplyDDL_TableOptionsRefuseWhatSQLiteRefuses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantToken string
		wantError string
	}{
		{name: "a word after STRICT", sql: "CREATE TABLE t (a INT) STRICT nonsense", wantToken: "nonsense", wantError: "expected the end of the statement"},
		{name: "two options without a comma", sql: "CREATE TABLE t (a INT PRIMARY KEY) WITHOUT ROWID STRICT", wantToken: "STRICT", wantError: "expected the end of the statement"},
		{name: "an unknown option", sql: "CREATE TABLE t (a INT) foo", wantToken: "foo", wantError: "unknown table option"},
		{name: "an unknown option after a comma", sql: "CREATE TABLE t (a INT) STRICT, foo", wantToken: "foo", wantError: "unknown table option"},
		{name: "a trailing comma", sql: "CREATE TABLE t (a INT) STRICT,", wantToken: "", wantError: "unknown table option"},
		{name: "WITHOUT with no ROWID", sql: "CREATE TABLE t (a INT) WITHOUT", wantToken: "", wantError: "expected ROWID after WITHOUT"},
		{name: "WITHOUT with another word", sql: "CREATE TABLE t (a INT) WITHOUT rowidx", wantToken: "rowidx", wantError: "expected ROWID after WITHOUT"},
		{name: "a quoted option", sql: `CREATE TABLE t (a INT) "STRICT"`, wantToken: `"STRICT"`, wantError: "unknown table option"},
		{
			name:      "a datatype STRICT refuses, with STRICT written second",
			sql:       "CREATE TABLE t (a INT PRIMARY KEY, b VARCHAR(10)) WITHOUT ROWID, STRICT",
			wantToken: "",
			wantError: "STRICT table column",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDLError(t, testCase.sql)

			assert.Nil(t, mutation)
			require.ErrorContains(t, err, testCase.wantError)
			if parseError, isParseError := errors.AsType[*dialectkit.ParseError](err); isParseError {
				assert.Equal(t, testCase.wantToken, parseError.Token)
			}
		})
	}
}

func TestApplyDDL_TypeModifiersKeepTheirPlaces(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		wantPrecision *int
		wantScale     *int
		wantLength    *int
		name          string
		declaredType  string
	}{
		{name: "a precision and a scale", declaredType: "DECIMAL(10, 2)", wantPrecision: new(10), wantScale: new(2)},
		{name: "a negative scale", declaredType: "DECIMAL(10, -2)", wantPrecision: new(10), wantScale: new(-2)},
		{name: "a signed precision", declaredType: "DECIMAL(+5)", wantPrecision: new(5)},
		{
			name:          "an oversized precision is held at the limit and the scale keeps its place",
			declaredType:  "DECIMAL(99999999999999999999, 2)",
			wantPrecision: new(dialectkit.MaxTypeModifier),
			wantScale:     new(2),
		},
		{name: "an oversized negative scale is held at the limit", declaredType: "DECIMAL(4, -99999999999999999999)", wantPrecision: new(4), wantScale: new(-dialectkit.MaxTypeModifier)},
		{name: "a hexadecimal length", declaredType: "VARCHAR(0x10)", wantLength: new(16)},
		{name: "a fractional length is truncated", declaredType: "VARCHAR(1.5e3)", wantLength: new(1500)},
		{name: "no modifiers", declaredType: "VARCHAR"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, "CREATE TABLE t (a "+testCase.declaredType+")")

			require.Len(t, mutation.Columns, 1)
			sqlType := mutation.Columns[0].SQLType
			assert.Equal(t, testCase.wantPrecision, sqlType.Precision)
			assert.Equal(t, testCase.wantScale, sqlType.Scale)
			assert.Equal(t, testCase.wantLength, sqlType.Length)
		})
	}
}

func TestApplyDDL_MalformedTypeModifiersAreSyntaxErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		declaredType string
		wantToken    string
		wantError    string
	}{
		{name: "a word", declaredType: "VARCHAR(abc)", wantToken: "abc", wantError: "expected a number as the type's modifier"},
		{name: "no modifier", declaredType: "VARCHAR()", wantToken: ")", wantError: "expected a number as the type's modifier"},
		{name: "a sign with no number", declaredType: "DECIMAL(10, -)", wantToken: ")", wantError: "expected a number as the type's modifier"},
		{name: "three modifiers", declaredType: "DECIMAL(1, 2, 3)", wantToken: ",", wantError: "expected ) after the type's modifiers"},
		{name: "two modifiers without a comma", declaredType: "DECIMAL(10 2)", wantToken: "2", wantError: "expected ) after the type's modifiers"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDLError(t, "CREATE TABLE t (a "+testCase.declaredType+")")

			assert.Nil(t, mutation)
			require.ErrorIs(t, err, fault.ErrParse)
			require.ErrorContains(t, err, testCase.wantError)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantToken, parseError.Token)
		})
	}
}

func TestApplyDDL_ViewQuerySyntaxErrorsFailTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantError string
	}{
		{name: "words where a select item ends", sql: "CREATE VIEW v AS SELECT a b c d FROM", wantError: "syntax error"},
		{name: "unclosed parentheses", sql: "CREATE VIEW v AS SELECT (((1", wantError: "syntax error"},
		{name: "a parenthesised query, which SQLite refuses", sql: "CREATE VIEW v AS (SELECT 1)", wantError: "expected keyword SELECT"},
		{name: "no query", sql: "CREATE VIEW v AS", wantError: "expected keyword SELECT"},
		{name: "tokens after the query", sql: "CREATE VIEW v AS SELECT 1 AS a garbage here", wantError: "garbage"},
		{name: "a table query with a syntax error", sql: "CREATE TABLE t AS SELECT a b c d FROM", wantError: "syntax error"},
		{name: "a declared column list without commas", sql: "CREATE VIEW v (a b) AS SELECT 1 AS a", wantError: "expected , or ) after a column name"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDLError(t, testCase.sql)

			assert.Nil(t, mutation)
			require.ErrorIs(t, err, fault.ErrParse)
			assert.ErrorContains(t, err, testCase.wantError)
		})
	}
}

func TestApplyDDL_AViewQueryNestedPastTheDepthLimitIsRecordedAsUnanalysed(t *testing.T) {
	t.Parallel()

	const nesting = 20
	nestedValue := strings.Repeat("(", nesting) + "1" + strings.Repeat(")", nesting)
	nestedQuery := strings.Repeat("SELECT * FROM (", nesting) + "SELECT 1 AS x" + strings.Repeat(")", nesting)

	testCases := []struct {
		name        string
		sql         string
		body        string
		subject     string
		wantColumns []string
	}{
		{
			name:        "a view whose select item nests too deeply",
			sql:         "CREATE VIEW v AS SELECT " + nestedValue + " AS x FROM t",
			body:        "SELECT " + nestedValue + " AS x FROM t",
			subject:     "view",
			wantColumns: []string{"x"},
		},
		{
			name:        "a view whose subqueries nest too deeply keeps its declared names",
			sql:         "CREATE VIEW v (a) AS " + nestedQuery,
			body:        nestedQuery,
			subject:     "view",
			wantColumns: []string{"a"},
		},
		{
			name:        "a table created from a query that nests too deeply",
			sql:         "CREATE TABLE t AS " + nestedQuery,
			body:        nestedQuery,
			subject:     "table",
			wantColumns: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxParseDepth(8))
			statement := parseOne(t, engine, testCase.sql)

			mutation, err := engine.ApplyDDL(t.Context(), statement)

			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Nil(t, mutation.ViewDefinition)
			var names []string
			for _, column := range mutation.Columns {
				names = append(names, column.Name)
				assert.Equal(t, sqlmodel.TypeCategoryUnknown, column.SQLType.Category)
			}
			assert.Equal(t, testCase.wantColumns, names)
			require.Len(t, mutation.UnanalysedParts, 1)
			part := mutation.UnanalysedParts[0]
			assert.Contains(t, part.Message, "the "+testCase.subject+"'s query was not analysed")
			assert.Contains(t, part.Message, dialectkit.ErrParseDepth.Error())
			bodyStart := strings.Index(testCase.sql, testCase.body)
			assert.Equal(t, ast.Span{Start: bodyStart, End: bodyStart + len(testCase.body)}, part.Span)
		})
	}
}

func TestApplyDDL_ReturnsTheErrorsOfMalformedClauses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantError string
	}{
		{name: "a trigger with no table after ON", sql: "CREATE TRIGGER tr AFTER INSERT ON", wantError: "parsing trigger table"},
		{name: "a virtual table with no USING", sql: "CREATE VIRTUAL TABLE v", wantError: "expected keyword USING"},
		{name: "a virtual table with no module", sql: "CREATE VIRTUAL TABLE v USING", wantError: "parsing virtual table module"},
		{name: "a table body with no closing parenthesis", sql: "CREATE TABLE t (a INT", wantError: messageUnmatchedParenthesis},
		{name: "an empty table body", sql: "CREATE TABLE t ()", wantError: "expected an identifier"},
		{name: "a table body ending in a comma", sql: "CREATE TABLE t (a INT,)", wantError: "expected an identifier"},
		{name: "a column REFERENCES with no table", sql: "CREATE TABLE t (a INT REFERENCES)", wantError: "expected an identifier"},
		{name: "a foreign key with no REFERENCES", sql: "CREATE TABLE t (a INT, FOREIGN KEY (a))", wantError: "expected keyword REFERENCES"},
		{name: "a foreign key REFERENCES with no table", sql: "CREATE TABLE t (a INT, FOREIGN KEY (a) REFERENCES)", wantError: "expected an identifier"},
		{name: "a foreign key column list without commas", sql: "CREATE TABLE t (a INT, FOREIGN KEY (a) REFERENCES u (b c))", wantError: "expected , or ) after a column name"},
		{name: "a foreign key with no KEY", sql: "CREATE TABLE t (a INT, FOREIGN (a) REFERENCES u)", wantError: "expected keyword KEY"},
		{name: "an unknown foreign key action", sql: "CREATE TABLE t (a INT REFERENCES u ON garbage)", wantError: "expected one of DELETE, UPDATE, INSERT"},
		{name: "a foreign key MATCH with no name", sql: "CREATE TABLE t (a INT REFERENCES u MATCH)", wantError: "expected an identifier"},
		{name: "a deferral with no timing", sql: "CREATE TABLE t (a INT REFERENCES u DEFERRABLE INITIALLY)", wantError: "expected one of DEFERRED, IMMEDIATE"},
		{name: "NOT with no NULL", sql: "CREATE TABLE t (a INT NOT)", wantError: "expected keyword NULL"},
		{name: "PRIMARY with no KEY", sql: "CREATE TABLE t (a INT PRIMARY)", wantError: "expected keyword KEY"},
		{name: "DEFAULT with no value", sql: "CREATE TABLE t (a INT DEFAULT)", wantError: "expected a default value"},
		{name: "a signed name as a default", sql: "CREATE TABLE t (a INT DEFAULT -abc)", wantError: "expected a default value"},
		{name: "CHECK with no expression", sql: "CREATE TABLE t (a INT CHECK)", wantError: "expected ("},
		{name: "COLLATE with no collation", sql: "CREATE TABLE t (a INT COLLATE)", wantError: "expected an identifier"},
		{name: "a conflict clause with no resolution", sql: "CREATE TABLE t (a INT NOT NULL ON CONFLICT)", wantError: "expected one of ROLLBACK, ABORT, FAIL, IGNORE, REPLACE"},
		{name: "an unknown conflict resolution", sql: "CREATE TABLE t (a INT NOT NULL ON CONFLICT garbage)", wantError: "expected one of ROLLBACK, ABORT, FAIL, IGNORE, REPLACE"},
		{name: "ON with no CONFLICT", sql: "CREATE TABLE t (a INT UNIQUE ON)", wantError: "expected keyword CONFLICT"},
		{name: "a primary key list ending in a comma", sql: "CREATE TABLE t (a INT, PRIMARY KEY (a,))", wantError: "expected an identifier"},
		{name: "a table UNIQUE constraint with no columns", sql: "CREATE TABLE t (a INT, CONSTRAINT u UNIQUE)", wantError: "expected ("},
		{name: "a table CHECK constraint with no expression", sql: "CREATE TABLE t (a INT, CONSTRAINT c CHECK)", wantError: "expected ("},
		{name: "a generated column with no ALWAYS", sql: "CREATE TABLE t (a INT, b INT GENERATED AS (a))", wantError: "expected keyword ALWAYS"},
		{name: "a generated column with no expression", sql: "CREATE TABLE t (a INT, b INT AS)", wantError: "expected ("},
		{name: "IF with no NOT EXISTS", sql: "CREATE TABLE IF t (a INT)", wantError: "expected keyword NOT"},
		{name: "IF NOT with no EXISTS", sql: "CREATE VIEW IF NOT v AS SELECT 1", wantError: "expected keyword EXISTS"},
		{name: "a drop with IF and no EXISTS", sql: "DROP TABLE IF t", wantError: "expected keyword EXISTS"},
		{name: "an index with no column list", sql: "CREATE INDEX i ON t", wantError: "expected ("},
		{name: "an index with an unclosed column list", sql: "CREATE INDEX i ON t (a", wantError: messageUnmatchedParenthesis},
		{name: "a partial index with no condition", sql: "CREATE INDEX i ON t (a) WHERE", wantError: messageExpectedCondition},
		{name: "a partial index whose condition does not read", sql: "CREATE INDEX i ON t (a) WHERE a >", wantError: messageExpectedExpression},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDLError(t, testCase.sql)

			assert.Nil(t, mutation)
			require.ErrorIs(t, err, fault.ErrParse)
			assert.ErrorContains(t, err, testCase.wantError)
		})
	}
}

func TestApplyDDL_AcceptsTheClausesSQLiteAccepts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a constraint name alone on a column", sql: "CREATE TABLE t (a INT CONSTRAINT c)"},
		{name: "a constraint name alone in the body", sql: "CREATE TABLE t (a INT, CONSTRAINT c)"},
		{name: "every foreign key clause", sql: "CREATE TABLE t (a INT REFERENCES u ON DELETE CASCADE ON UPDATE NO ACTION MATCH full NOT DEFERRABLE INITIALLY DEFERRED)"},
		{name: "a foreign key action setting NULL", sql: "CREATE TABLE t (a INT, FOREIGN KEY (a) REFERENCES u ON UPDATE SET NULL ON DELETE SET DEFAULT)"},
		{name: "a deferrable reference", sql: "CREATE TABLE t (a INT REFERENCES u(b) DEFERRABLE)"},
		{name: "a reference followed by NOT NULL", sql: "CREATE TABLE t (a INT REFERENCES u(b) NOT NULL)"},
		{name: "defaults SQLite takes", sql: "CREATE TABLE t (a INT DEFAULT -1, b TEXT DEFAULT abc, c BLOB DEFAULT X'00', d TEXT DEFAULT CURRENT_TIMESTAMP, e INT DEFAULT +1, f INT DEFAULT (1 + 2), g INT DEFAULT -NULL)"},
		{name: "a conflict clause before AUTOINCREMENT", sql: "CREATE TABLE t (a INTEGER PRIMARY KEY ON CONFLICT ROLLBACK AUTOINCREMENT)"},
		{name: "a conflict clause on NULL", sql: "CREATE TABLE t (a INT NULL ON CONFLICT IGNORE)"},
		{name: "a table CHECK with a conflict clause", sql: "CREATE TABLE t (a INT, CHECK (a > 0) ON CONFLICT FAIL)"},
		{name: "a primary key column with a collation and order", sql: "CREATE TABLE t (a INT, PRIMARY KEY (a COLLATE nocase DESC))"},
		{name: "a generated column", sql: "CREATE TABLE t (a INT, b INT GENERATED ALWAYS AS (a * 2) STORED, c INT AS (a + 1) VIRTUAL)"},
		{name: "an index over an expression with a condition", sql: "CREATE UNIQUE INDEX IF NOT EXISTS i ON t (lower(a) COLLATE nocase DESC, b) WHERE a IS NOT NULL AND b > 0"},
		{name: "a dropped index that may not exist", sql: "DROP INDEX IF EXISTS main.i"},
		{name: "a dropped trigger that may not exist", sql: "DROP TRIGGER IF EXISTS tr"},
		{name: "a virtual table with no arguments", sql: "CREATE VIRTUAL TABLE IF NOT EXISTS v USING fts5"},
		{name: "a statement with a trailing semicolon", sql: "DROP TABLE t;"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDLError(t, testCase.sql)

			require.NoError(t, err)
			assert.NotNil(t, mutation)
		})
	}
}

func applyDDLError(t *testing.T, sql string) (*ast.CatalogueMutation, error) {
	t.Helper()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	return engine.ApplyDDL(t.Context(), statements[0])
}

func TestApplyDDL_ANameMayHoldADollarSign(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantColumns []string
	}{
		{name: "a column", sql: "CREATE TABLE t (price$usd INT)", wantColumns: []string{"price$usd"}},
		{name: "a column and a table", sql: "CREATE TABLE t$1 (a$ INT, b$c$d TEXT)", wantColumns: []string{"a$", "b$c$d"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			var names []string
			for _, column := range mutation.Columns {
				names = append(names, column.Name)
			}
			assert.Equal(t, testCase.wantColumns, names)
		})
	}
}
