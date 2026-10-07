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

package duckdb

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
)

var viewCommentCases = []struct {
	name        string
	sql         string
	wantSchema  string
	wantView    string
	wantComment string
}{
	{name: "a view", sql: "COMMENT ON VIEW active_users IS 'Users seen this month'", wantView: "active_users", wantComment: "Users seen this month"},
	{name: "a view in a schema", sql: "COMMENT ON VIEW reporting.totals IS 'Totals per day'",
		wantSchema: "reporting", wantView: "totals", wantComment: "Totals per day"},
	{name: "a view's comment removed", sql: "COMMENT ON VIEW active_users IS NULL", wantView: "active_users"},
}

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

func TestApplyDDL_CreateTable(t *testing.T) {
	t.Parallel()

	t.Run("simple table with typed columns", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			name VARCHAR NOT NULL,
			email TEXT
		)`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
		require.Len(t, mutation.Columns, 3)

		assert.Equal(t, "id", mutation.Columns[0].Name)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, mutation.Columns[0].SQLType.Category)
		assert.False(t, mutation.Columns[0].Nullable, "primary key column should not be nullable")

		assert.Equal(t, "name", mutation.Columns[1].Name)
		assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[1].SQLType.Category)
		assert.False(t, mutation.Columns[1].Nullable, "NOT NULL column should not be nullable")

		assert.Equal(t, "email", mutation.Columns[2].Name)
		assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[2].SQLType.Category)
		assert.True(t, mutation.Columns[2].Nullable, "column without NOT NULL should be nullable")

		assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
	})

	t.Run("IF NOT EXISTS is tolerated", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE IF NOT EXISTS products (
			id BIGINT PRIMARY KEY,
			title VARCHAR
		)`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
		assert.Equal(t, "products", mutation.TableName)
		require.Len(t, mutation.Columns, 2)
	})

	t.Run("column constraints are parsed", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE items (
			id INTEGER PRIMARY KEY,
			name VARCHAR NOT NULL UNIQUE,
			quantity INTEGER DEFAULT 0,
			price NUMERIC CHECK (price >= 0)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 4)

		assert.True(t, mutation.Columns[2].HasDefault, "column with DEFAULT should have HasDefault set")
	})

	t.Run("schema-qualified table name", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE myschema.orders (id INTEGER PRIMARY KEY)`)

		require.NotNil(t, mutation)
		assert.Equal(t, "myschema", mutation.SchemaName)
		assert.Equal(t, "orders", mutation.TableName)
	})

	t.Run("composite primary key via table constraint", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE order_items (
			order_id INTEGER,
			product_id INTEGER,
			quantity INTEGER,
			PRIMARY KEY (order_id, product_id)
		)`)

		require.NotNil(t, mutation)
		assert.Equal(t, []string{"order_id", "product_id"}, mutation.PrimaryKey)
	})

	t.Run("CREATE TABLE AS SELECT produces empty columns", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE snapshot AS SELECT * FROM users`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
		assert.Equal(t, "snapshot", mutation.TableName)
		assert.Empty(t, mutation.Columns, "CTAS should not produce column definitions")
	})

	t.Run("generated always as identity column", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE accounts (
			id INTEGER GENERATED ALWAYS AS IDENTITY,
			name VARCHAR NOT NULL
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.True(t, mutation.Columns[0].HasDefault, "identity column should have HasDefault set")
	})

	t.Run("foreign key table constraint", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE reviews (
			id INTEGER PRIMARY KEY,
			user_id INTEGER,
			FOREIGN KEY (user_id) REFERENCES users (id)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Constraints, 1)
		assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[0].Kind)
		assert.Equal(t, "users", mutation.Constraints[0].ForeignTable)
		assert.Equal(t, []string{"user_id"}, mutation.Constraints[0].Columns)
		assert.Equal(t, []string{"id"}, mutation.Constraints[0].ForeignColumns)
	})

	t.Run("unique table constraint", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE emails (
			id INTEGER PRIMARY KEY,
			address VARCHAR,
			UNIQUE (address)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Constraints, 1)
		assert.Equal(t, sqlmodel.ConstraintUnique, mutation.Constraints[0].Kind)
		assert.Equal(t, []string{"address"}, mutation.Constraints[0].Columns)
	})

	t.Run("DuckDB struct column type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE events (
			id INTEGER PRIMARY KEY,
			metadata STRUCT(key VARCHAR, value INTEGER)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.Equal(t, sqlmodel.TypeCategoryStruct, mutation.Columns[1].SQLType.Category)
		require.Len(t, mutation.Columns[1].SQLType.StructFields, 2)
		assert.Equal(t, "key", mutation.Columns[1].SQLType.StructFields[0].Name)
		assert.Equal(t, "value", mutation.Columns[1].SQLType.StructFields[1].Name)
	})

	t.Run("array column type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE tags (
			id INTEGER PRIMARY KEY,
			labels VARCHAR[]
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.True(t, mutation.Columns[1].IsArray, "column with [] should be an array")
		assert.Equal(t, 1, mutation.Columns[1].ArrayDimensions)
	})

	t.Run("map column type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE config (
			id INTEGER PRIMARY KEY,
			settings MAP(VARCHAR, INTEGER)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.Equal(t, sqlmodel.TypeCategoryMap, mutation.Columns[1].SQLType.Category)
	})

	t.Run("union column type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE data (
			id INTEGER PRIMARY KEY,
			payload UNION(i INTEGER, s VARCHAR)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.Equal(t, sqlmodel.TypeCategoryUnion, mutation.Columns[1].SQLType.Category)
	})

	t.Run("list column type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE lists (
			id INTEGER PRIMARY KEY,
			items INTEGER[]
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.Equal(t, 1, mutation.Columns[1].ArrayDimensions)
	})

	t.Run("GENERATED ALWAYS AS stored column", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE products (
			price NUMERIC,
			tax NUMERIC,
			total NUMERIC GENERATED ALWAYS AS (price + tax) STORED
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 3)
		assert.True(t, mutation.Columns[2].IsGenerated, "generated column should be marked generated")
		assert.Equal(t, sqlmodel.GeneratedKindStored, mutation.Columns[2].GeneratedKind)
	})

	t.Run("CHECK constraint on table", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE amounts (
			id INTEGER PRIMARY KEY,
			value NUMERIC,
			CHECK (value >= 0)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Constraints, 1)
		assert.Equal(t, sqlmodel.ConstraintCheck, mutation.Constraints[0].Kind)
	})

	t.Run("timestamp with time zone column", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE logs (
			id INTEGER PRIMARY KEY,
			created_at TIMESTAMP WITH TIME ZONE
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.Equal(t, sqlmodel.TypeCategoryTemporal, mutation.Columns[1].SQLType.Category)
	})

	t.Run("double precision column type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE measurements (
			id INTEGER PRIMARY KEY,
			value DOUBLE PRECISION
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, mutation.Columns[1].SQLType.Category)
	})

	t.Run("column with DEFAULT value", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE flags (
			id INTEGER PRIMARY KEY,
			active BOOLEAN DEFAULT true
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.True(t, mutation.Columns[1].HasDefault, "column with DEFAULT should have HasDefault set")
	})

	t.Run("named CONSTRAINT on table", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE t (
			x INTEGER,
			CONSTRAINT pk_x PRIMARY KEY (x)
		)`)

		require.NotNil(t, mutation)
		assert.Equal(t, []string{"x"}, mutation.PrimaryKey)
	})

	t.Run("column with REFERENCES constraint", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE comments (
			id INTEGER PRIMARY KEY,
			user_id INTEGER REFERENCES users (id)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.Equal(t, "user_id", mutation.Columns[1].Name)
	})

	t.Run("column with COLLATE constraint", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE names (
			id INTEGER PRIMARY KEY,
			name VARCHAR COLLATE NOCASE
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.Equal(t, "name", mutation.Columns[1].Name)
	})

	t.Run("column with GENERATED BY DEFAULT AS IDENTITY", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE tickets (
			id INTEGER GENERATED BY DEFAULT AS IDENTITY,
			title VARCHAR
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.True(t, mutation.Columns[0].HasDefault)
	})

	t.Run("character varying column type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE texts (
			id INTEGER PRIMARY KEY,
			content CHARACTER VARYING(500)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[1].SQLType.Category)
	})

	t.Run("timestamp without time zone", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE events (
			id INTEGER PRIMARY KEY,
			created_at TIMESTAMP WITHOUT TIME ZONE
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
		assert.Equal(t, sqlmodel.TypeCategoryTemporal, mutation.Columns[1].SQLType.Category)
	})

	t.Run("DEFAULT with complex expression", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE t (
			id INTEGER PRIMARY KEY,
			created_at TIMESTAMP DEFAULT (now()),
			status VARCHAR DEFAULT 'active'
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 3)
		assert.True(t, mutation.Columns[1].HasDefault)
		assert.True(t, mutation.Columns[2].HasDefault)
	})

	t.Run("multiple FOREIGN KEY constraints", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE reviews (
			id INTEGER PRIMARY KEY,
			user_id INTEGER,
			product_id INTEGER,
			FOREIGN KEY (user_id) REFERENCES users (id),
			FOREIGN KEY (product_id) REFERENCES products (id)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Constraints, 2)
		assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[0].Kind)
		assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[1].Kind)
	})
}

func TestApplyDDL_AlterTable(t *testing.T) {
	t.Parallel()

	t.Run("ADD COLUMN", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE users ADD COLUMN age INTEGER`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
		require.Len(t, mutation.Columns, 1)
		assert.Equal(t, "age", mutation.Columns[0].Name)
		assert.Equal(t, sqlmodel.TypeCategoryInteger, mutation.Columns[0].SQLType.Category)
	})

	t.Run("ADD COLUMN without COLUMN keyword", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE users ADD bio TEXT`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
		require.Len(t, mutation.Columns, 1)
		assert.Equal(t, "bio", mutation.Columns[0].Name)
	})

	t.Run("DROP COLUMN", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE users DROP COLUMN email`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableDropColumn, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
		assert.Equal(t, "email", mutation.ColumnName)
	})

	t.Run("DROP COLUMN without COLUMN keyword", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE users DROP email`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableDropColumn, mutation.Kind)
		assert.Equal(t, "email", mutation.ColumnName)
	})

	t.Run("RENAME COLUMN", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE users RENAME COLUMN name TO full_name`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableRenameColumn, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
		assert.Equal(t, "name", mutation.ColumnName)
		assert.Equal(t, "full_name", mutation.NewName)
	})

	t.Run("RENAME TABLE", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE users RENAME TO accounts`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableRenameTable, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
		assert.Equal(t, "accounts", mutation.NewName)
	})

	t.Run("SET SCHEMA", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE users SET SCHEMA archive`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableSetSchema, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
		assert.Equal(t, "archive", mutation.NewName)
	})

	t.Run("ADD CONSTRAINT", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE orders ADD UNIQUE (order_number)`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableAddConstraint, mutation.Kind)
		assert.Equal(t, "orders", mutation.TableName)
		require.Len(t, mutation.Constraints, 1)
		assert.Equal(t, sqlmodel.ConstraintUnique, mutation.Constraints[0].Kind)
	})

	t.Run("DROP CONSTRAINT", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE orders DROP CONSTRAINT orders_number_unique`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableDropConstraint, mutation.Kind)
		assert.Equal(t, "orders", mutation.TableName)
		assert.Equal(t, "orders_number_unique", mutation.ConstraintName)
	})

	t.Run("schema-qualified ALTER TABLE", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE myschema.users ADD COLUMN age INTEGER`)

		require.NotNil(t, mutation)
		assert.Equal(t, "myschema", mutation.SchemaName)
		assert.Equal(t, "users", mutation.TableName)
	})

	t.Run("ALTER COLUMN", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE users ALTER COLUMN email SET NOT NULL`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableAlterColumn, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
		assert.Equal(t, "email", mutation.ColumnName)
	})
}

func TestApplyDDL_DropTable(t *testing.T) {
	t.Parallel()

	t.Run("simple drop", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP TABLE users`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropTable, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
	})

	t.Run("IF EXISTS", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP TABLE IF EXISTS users`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropTable, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
	})

	t.Run("CASCADE", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP TABLE users CASCADE`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropTable, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
	})

	t.Run("schema-qualified drop", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP TABLE myschema.users`)

		require.NotNil(t, mutation)
		assert.Equal(t, "myschema", mutation.SchemaName)
		assert.Equal(t, "users", mutation.TableName)
	})
}

func TestApplyDDL_CreateView(t *testing.T) {
	t.Parallel()

	t.Run("simple view", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE VIEW active_users AS SELECT id, name FROM users WHERE active = true`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateView, mutation.Kind)
		assert.Equal(t, "active_users", mutation.TableName)
		assert.NotNil(t, mutation.ViewDefinition, "view body should be analysed")
	})

	t.Run("view with explicit column names", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE VIEW user_summary (user_id, user_name) AS SELECT id, name FROM users`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateView, mutation.Kind)
		require.NotNil(t, mutation.ViewDefinition)
		require.Len(t, mutation.ViewDefinition.OutputColumns, 2)
		assert.Equal(t, "user_id", mutation.ViewDefinition.OutputColumns[0].Name)
		assert.Equal(t, "user_name", mutation.ViewDefinition.OutputColumns[1].Name)
	})

	t.Run("OR REPLACE view", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE OR REPLACE VIEW active_users AS SELECT id FROM users`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateView, mutation.Kind)
		assert.Equal(t, "active_users", mutation.TableName)
	})

	t.Run("IF NOT EXISTS view", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE VIEW IF NOT EXISTS active_users AS SELECT id FROM users`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateView, mutation.Kind)
	})

	t.Run("schema-qualified view", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE VIEW reporting.summary AS SELECT count(*) FROM users`)

		require.NotNil(t, mutation)
		assert.Equal(t, "reporting", mutation.SchemaName)
		assert.Equal(t, "summary", mutation.TableName)
	})
}

func TestApplyDDL_DropView(t *testing.T) {
	t.Parallel()

	t.Run("simple drop view", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP VIEW active_users`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropView, mutation.Kind)
		assert.Equal(t, "active_users", mutation.TableName)
	})

	t.Run("IF EXISTS drop view", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP VIEW IF EXISTS active_users`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropView, mutation.Kind)
		assert.Equal(t, "active_users", mutation.TableName)
	})
}

func TestApplyDDL_CreateIndex(t *testing.T) {
	t.Parallel()

	t.Run("simple index", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE INDEX idx_users_email ON users (email)`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateIndex, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
		assert.Equal(t, "idx_users_email", mutation.NewName)
	})

	t.Run("unique index", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE UNIQUE INDEX idx_users_email_unique ON users (email)`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateIndex, mutation.Kind)
		assert.Equal(t, "idx_users_email_unique", mutation.NewName)
	})

	t.Run("IF NOT EXISTS index", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE INDEX IF NOT EXISTS idx_users_email ON users (email)`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateIndex, mutation.Kind)
		assert.Equal(t, "idx_users_email", mutation.NewName)
	})

	t.Run("index on schema-qualified table", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE INDEX idx_orders_date ON myschema.orders (order_date)`)

		require.NotNil(t, mutation)
		assert.Equal(t, "myschema", mutation.SchemaName)
		assert.Equal(t, "orders", mutation.TableName)
	})
}

func TestApplyDDL_DropIndex(t *testing.T) {
	t.Parallel()

	t.Run("simple drop index", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP INDEX idx_users_email`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropIndex, mutation.Kind)
		assert.Equal(t, "idx_users_email", mutation.NewName)
	})

	t.Run("IF EXISTS drop index", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP INDEX IF EXISTS idx_users_email`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropIndex, mutation.Kind)
		assert.Equal(t, "idx_users_email", mutation.NewName)
	})
}

func TestApplyDDL_CreateType(t *testing.T) {
	t.Parallel()

	t.Run("enum type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TYPE mood AS ENUM ('happy', 'sad', 'neutral')`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateEnum, mutation.Kind)
		assert.Equal(t, "mood", mutation.EnumName)
		assert.Equal(t, []string{"happy", "sad", "neutral"}, mutation.EnumValues)
	})

	t.Run("schema-qualified enum type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TYPE myschema.status AS ENUM ('active', 'inactive')`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateEnum, mutation.Kind)
		assert.Equal(t, "myschema", mutation.SchemaName)
		assert.Equal(t, "status", mutation.EnumName)
	})

	t.Run("composite type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TYPE address AS (
			street VARCHAR,
			city VARCHAR,
			postcode VARCHAR
		)`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateCompositeType, mutation.Kind)
		assert.Equal(t, "address", mutation.EnumName, "composite type name stored in EnumName field")
		require.Len(t, mutation.Columns, 3)
		assert.Equal(t, "street", mutation.Columns[0].Name)
		assert.Equal(t, "city", mutation.Columns[1].Name)
		assert.Equal(t, "postcode", mutation.Columns[2].Name)
	})

	t.Run("composite type with mixed types", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TYPE measurement AS (
			value DOUBLE,
			unit VARCHAR,
			timestamp TIMESTAMP
		)`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateCompositeType, mutation.Kind)
		require.Len(t, mutation.Columns, 3)
		assert.Equal(t, sqlmodel.TypeCategoryFloat, mutation.Columns[0].SQLType.Category)
		assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[1].SQLType.Category)
		assert.Equal(t, sqlmodel.TypeCategoryTemporal, mutation.Columns[2].SQLType.Category)
	})

	t.Run("enum type with many values", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TYPE colour AS ENUM ('red', 'green', 'blue', 'yellow', 'purple')`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateEnum, mutation.Kind)
		assert.Equal(t, "colour", mutation.EnumName)
		require.Len(t, mutation.EnumValues, 5)
		assert.Equal(t, "red", mutation.EnumValues[0])
		assert.Equal(t, "purple", mutation.EnumValues[4])
	})
}

func TestApplyDDL_DropType(t *testing.T) {
	t.Parallel()

	t.Run("simple drop type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP TYPE mood`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropType, mutation.Kind)
		assert.Equal(t, "mood", mutation.EnumName)
	})

	t.Run("IF EXISTS drop type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP TYPE IF EXISTS mood`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropType, mutation.Kind)
		assert.Equal(t, "mood", mutation.EnumName)
	})

	t.Run("CASCADE drop type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP TYPE mood CASCADE`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropType, mutation.Kind)
	})

	t.Run("schema-qualified DROP TYPE", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP TYPE myschema.mood`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropType, mutation.Kind)
		assert.Equal(t, "myschema", mutation.SchemaName)
		assert.Equal(t, "mood", mutation.EnumName)
	})
}

func TestApplyDDL_AlterType(t *testing.T) {
	t.Parallel()

	t.Run("ALTER TYPE ADD VALUE", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TYPE mood ADD VALUE 'excited'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterEnumAddValue, mutation.Kind)
		assert.Equal(t, "mood", mutation.EnumName)
		require.Len(t, mutation.EnumValues, 1)
		assert.Equal(t, "excited", mutation.EnumValues[0])
	})

	t.Run("ALTER TYPE ADD VALUE IF NOT EXISTS", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TYPE mood ADD VALUE IF NOT EXISTS 'calm'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterEnumAddValue, mutation.Kind)
		assert.Equal(t, "mood", mutation.EnumName)
		require.Len(t, mutation.EnumValues, 1)
		assert.Equal(t, "calm", mutation.EnumValues[0])
	})

	t.Run("ALTER TYPE ADD VALUE BEFORE", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TYPE mood ADD VALUE 'anxious' BEFORE 'sad'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterEnumAddValue, mutation.Kind)
		require.Len(t, mutation.EnumValues, 1)
		assert.Equal(t, "anxious", mutation.EnumValues[0])
	})

	t.Run("ALTER TYPE ADD VALUE AFTER", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TYPE mood ADD VALUE 'ecstatic' AFTER 'happy'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterEnumAddValue, mutation.Kind)
		require.Len(t, mutation.EnumValues, 1)
		assert.Equal(t, "ecstatic", mutation.EnumValues[0])
	})

	t.Run("ALTER TYPE RENAME VALUE", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TYPE mood RENAME VALUE 'sad' TO 'melancholy'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterEnumRenameValue, mutation.Kind)
		assert.Equal(t, "mood", mutation.EnumName)
		require.Len(t, mutation.EnumValues, 2)
		assert.Equal(t, "sad", mutation.EnumValues[0])
		assert.Equal(t, "melancholy", mutation.EnumValues[1])
	})

	t.Run("schema-qualified ALTER TYPE ADD VALUE", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TYPE myschema.mood ADD VALUE 'relaxed'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterEnumAddValue, mutation.Kind)
		assert.Equal(t, "myschema", mutation.SchemaName)
		assert.Equal(t, "mood", mutation.EnumName)
	})
}

func TestApplyDDL_CreateMacro(t *testing.T) {
	t.Parallel()

	t.Run("scalar macro", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE MACRO add_one(x) AS x + 1`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
		require.NotNil(t, mutation.Function)
		assert.Equal(t, "add_one", mutation.Function.Name)
		require.Len(t, mutation.Function.Arguments, 1)

		assert.Equal(t, "x", mutation.Function.Arguments[0].Name)
	})

	t.Run("table macro", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE MACRO recent_users() AS TABLE SELECT * FROM users WHERE created_at > now() - INTERVAL '1 day'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
		require.NotNil(t, mutation.Function)
		assert.Equal(t, "recent_users", mutation.Function.Name)
		assert.True(t, mutation.Function.ReturnsSet, "table macro should have ReturnsSet")
	})

	t.Run("table macro captures projected columns", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE MACRO active_users() AS TABLE SELECT id, name FROM users WHERE active = true`)

		require.NotNil(t, mutation)
		require.NotNil(t, mutation.Function)
		assert.True(t, mutation.Function.ReturnsSet, "table macro should have ReturnsSet")
		require.Len(t, mutation.Columns, 2,
			"table macro projection columns must be captured so the function resolves in FROM")
		assert.Equal(t, "id", mutation.Columns[0].Name)
		assert.Equal(t, "name", mutation.Columns[1].Name)
	})

	t.Run("OR REPLACE macro", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE OR REPLACE MACRO double_it(x) AS x * 2`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
		assert.Equal(t, "double_it", mutation.Function.Name)
	})

	t.Run("CREATE FUNCTION alias for macro", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE FUNCTION triple(x) AS x * 3`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
		assert.Equal(t, "triple", mutation.Function.Name)
	})

	t.Run("schema-qualified macro", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE MACRO myschema.greet(n) AS 'Hello, ' || n`)

		require.NotNil(t, mutation)
		assert.Equal(t, "myschema", mutation.SchemaName)
		assert.Equal(t, "greet", mutation.Function.Name)
	})

	t.Run("macro with multiple arguments", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE MACRO add(a, b) AS a + b`)

		require.NotNil(t, mutation)
		require.NotNil(t, mutation.Function)
		require.Len(t, mutation.Function.Arguments, 2)

		assert.Equal(t, "a", mutation.Function.Arguments[0].Name)
		assert.Equal(t, "b", mutation.Function.Arguments[1].Name)
	})

	t.Run("table macro with arguments", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE MACRO user_orders(uid) AS TABLE SELECT * FROM orders WHERE user_id = uid`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
		require.NotNil(t, mutation.Function)
		assert.Equal(t, "user_orders", mutation.Function.Name)
		assert.True(t, mutation.Function.ReturnsSet, "table macro should have ReturnsSet")
		require.Len(t, mutation.Function.Arguments, 1)
	})

	t.Run("macro with string concatenation body", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE MACRO greeting(name) AS 'Hello, ' || name || '!'`)

		require.NotNil(t, mutation)
		require.NotNil(t, mutation.Function)
		assert.Equal(t, "greeting", mutation.Function.Name)
	})
}

func TestApplyDDL_DropMacro(t *testing.T) {
	t.Parallel()

	t.Run("DROP MACRO", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP MACRO add_one`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropFunction, mutation.Kind)
		require.NotNil(t, mutation.Function)
		assert.Equal(t, "add_one", mutation.Function.Name)
	})

	t.Run("DROP FUNCTION", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP FUNCTION my_func`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropFunction, mutation.Kind)
		require.NotNil(t, mutation.Function)
		assert.Equal(t, "my_func", mutation.Function.Name)
	})

	t.Run("IF EXISTS drop macro", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP MACRO IF EXISTS add_one`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropFunction, mutation.Kind)
	})

	t.Run("DROP MACRO TABLE names the table macro", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP MACRO TABLE main.recent_boats`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropFunction, mutation.Kind)
		require.NotNil(t, mutation.Function)
		assert.Equal(t, "recent_boats", mutation.Function.Name)
		assert.Equal(t, "main", mutation.Function.Schema)
	})
}

func TestApplyDDL_CreateSchema(t *testing.T) {
	t.Parallel()

	t.Run("simple schema", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE SCHEMA analytics`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateSchema, mutation.Kind)
		assert.Equal(t, "analytics", mutation.SchemaName)
	})

	t.Run("IF NOT EXISTS schema", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE SCHEMA IF NOT EXISTS analytics`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateSchema, mutation.Kind)
		assert.Equal(t, "analytics", mutation.SchemaName)
	})
}

func TestApplyDDL_DropSchema(t *testing.T) {
	t.Parallel()

	t.Run("simple drop schema", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP SCHEMA analytics`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropSchema, mutation.Kind)
		assert.Equal(t, "analytics", mutation.SchemaName)
	})

	t.Run("IF EXISTS drop schema", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP SCHEMA IF EXISTS analytics`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropSchema, mutation.Kind)
		assert.Equal(t, "analytics", mutation.SchemaName)
	})

	t.Run("CASCADE drop schema", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP SCHEMA analytics CASCADE`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropSchema, mutation.Kind)
	})
}

func TestApplyDDL_CreateSequence(t *testing.T) {
	t.Parallel()

	t.Run("simple sequence", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE SEQUENCE user_id_seq`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateSequence, mutation.Kind)
		assert.Equal(t, "user_id_seq", mutation.SequenceName)
	})

	t.Run("IF NOT EXISTS sequence", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE SEQUENCE IF NOT EXISTS user_id_seq`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateSequence, mutation.Kind)
		assert.Equal(t, "user_id_seq", mutation.SequenceName)
	})

	t.Run("schema-qualified sequence", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE SEQUENCE myschema.order_id_seq`)

		require.NotNil(t, mutation)
		assert.Equal(t, "myschema", mutation.SchemaName)
		assert.Equal(t, "order_id_seq", mutation.SequenceName)
	})

	t.Run("sequence with OWNED BY", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE SEQUENCE user_id_seq OWNED BY users.id`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateSequence, mutation.Kind)
		assert.Equal(t, "users", mutation.OwnedByTable)
		assert.Equal(t, "id", mutation.OwnedByColumn)
	})

	t.Run("sequence with all options", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE SEQUENCE order_id_seq START WITH 1000 INCREMENT BY 1 MINVALUE 1 MAXVALUE 999999 OWNED BY orders.id`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateSequence, mutation.Kind)
		assert.Equal(t, "order_id_seq", mutation.SequenceName)
		assert.Equal(t, "orders", mutation.OwnedByTable)
		assert.Equal(t, "id", mutation.OwnedByColumn)
	})

	t.Run("sequence with OWNED BY NONE", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE SEQUENCE standalone_seq OWNED BY NONE`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateSequence, mutation.Kind)
		assert.Equal(t, "standalone_seq", mutation.SequenceName)
		assert.Empty(t, mutation.OwnedByTable, "OWNED BY NONE should not set table")
		assert.Empty(t, mutation.OwnedByColumn, "OWNED BY NONE should not set column")
	})
}

func TestApplyDDL_DropSequence(t *testing.T) {
	t.Parallel()

	t.Run("simple drop sequence", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP SEQUENCE user_id_seq`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropSequence, mutation.Kind)
		assert.Equal(t, "user_id_seq", mutation.SequenceName)
	})

	t.Run("IF EXISTS drop sequence", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP SEQUENCE IF EXISTS user_id_seq`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropSequence, mutation.Kind)
		assert.Equal(t, "user_id_seq", mutation.SequenceName)
	})

	t.Run("schema-qualified drop sequence", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP SEQUENCE myschema.order_id_seq`)

		require.NotNil(t, mutation)
		assert.Equal(t, "myschema", mutation.SchemaName)
		assert.Equal(t, "order_id_seq", mutation.SequenceName)
	})

	t.Run("CASCADE drop sequence", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `DROP SEQUENCE user_id_seq CASCADE`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropSequence, mutation.Kind)
		assert.Equal(t, "user_id_seq", mutation.SequenceName)
	})
}

func TestACommentOnAViewNamesTheView(t *testing.T) {
	t.Parallel()

	for _, testCase := range viewCommentCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			require.Equal(t, ast.MutationComment, mutation.Kind)
			require.Equal(t, testCase.wantSchema, mutation.SchemaName)
			require.Equal(t, testCase.wantView, mutation.TableName)
			require.Equal(t, testCase.wantComment, mutation.Comment)
		})
	}
}

func TestApplyDDL_Comment(t *testing.T) {
	t.Parallel()

	t.Run("comment on table", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `COMMENT ON TABLE users IS 'User accounts'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationComment, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
	})

	t.Run("comment on column", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `COMMENT ON COLUMN users.email IS 'Primary email address'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationComment, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
		assert.Equal(t, "email", mutation.ColumnName)
	})

	t.Run("comment on schema", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `COMMENT ON SCHEMA analytics IS 'Analytics data'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationComment, mutation.Kind)
		assert.Equal(t, "analytics", mutation.SchemaName)
	})

	t.Run("comment on function", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `COMMENT ON FUNCTION add_one IS 'Adds one'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationComment, mutation.Kind)
		require.NotNil(t, mutation.Function)
		assert.Equal(t, "add_one", mutation.Function.Name)
	})

	t.Run("comment on type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `COMMENT ON TYPE mood IS 'Mood enumeration'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationComment, mutation.Kind)
		assert.Equal(t, "mood", mutation.EnumName)
	})

	t.Run("remove comment with NULL", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `COMMENT ON TABLE users IS NULL`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationComment, mutation.Kind)
		assert.Equal(t, "users", mutation.TableName)
	})
}

func TestApplyDDL_NonDDL(t *testing.T) {
	t.Parallel()

	t.Run("SELECT returns nil mutation", func(t *testing.T) {
		t.Parallel()

		engine := New()
		statements, err := engine.ParseStatements(t.Context(), `SELECT * FROM users`)
		require.NoError(t, err)
		require.NotEmpty(t, statements)

		mutation, err := engine.ApplyDDL(t.Context(), statements[0])
		require.NoError(t, err)
		assert.Nil(t, mutation, "SELECT should not produce a DDL mutation")
	})

	t.Run("INSERT returns nil mutation", func(t *testing.T) {
		t.Parallel()

		engine := New()
		statements, err := engine.ParseStatements(t.Context(), `INSERT INTO users (name) VALUES ('alice')`)
		require.NoError(t, err)
		require.NotEmpty(t, statements)

		mutation, err := engine.ApplyDDL(t.Context(), statements[0])
		require.NoError(t, err)
		assert.Nil(t, mutation, "INSERT should not produce a DDL mutation")
	})

	t.Run("UPDATE returns nil mutation", func(t *testing.T) {
		t.Parallel()

		engine := New()
		statements, err := engine.ParseStatements(t.Context(), `UPDATE users SET name = 'bob' WHERE id = 1`)
		require.NoError(t, err)
		require.NotEmpty(t, statements)

		mutation, err := engine.ApplyDDL(t.Context(), statements[0])
		require.NoError(t, err)
		assert.Nil(t, mutation, "UPDATE should not produce a DDL mutation")
	})

	t.Run("DELETE returns nil mutation", func(t *testing.T) {
		t.Parallel()

		engine := New()
		statements, err := engine.ParseStatements(t.Context(), `DELETE FROM users WHERE id = 1`)
		require.NoError(t, err)
		require.NotEmpty(t, statements)

		mutation, err := engine.ApplyDDL(t.Context(), statements[0])
		require.NoError(t, err)
		assert.Nil(t, mutation, "DELETE should not produce a DDL mutation")
	})
}

func TestApplyDDL_CreateTable_ConstraintsAndDefaults(t *testing.T) {
	t.Parallel()

	t.Run("column with DEFAULT function call expression", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE t (
			id INTEGER PRIMARY KEY,
			uid UUID DEFAULT (gen_random_uuid()),
			ts TIMESTAMP DEFAULT (now() + INTERVAL '1 day')
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 3)
		assert.True(t, mutation.Columns[1].HasDefault)
		assert.True(t, mutation.Columns[2].HasDefault)
	})

	t.Run("column with REFERENCES ON DELETE CASCADE ON UPDATE SET NULL", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE child (
			id INTEGER PRIMARY KEY,
			parent_id INTEGER REFERENCES parent (id) ON DELETE CASCADE ON UPDATE SET NULL
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 2)
	})

	t.Run("table FOREIGN KEY with ON DELETE and ON UPDATE", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE child (
			id INTEGER PRIMARY KEY,
			parent_id INTEGER,
			FOREIGN KEY (parent_id) REFERENCES parent (id) ON DELETE SET DEFAULT ON UPDATE NO ACTION
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Constraints, 1)
	})

	t.Run("named UNIQUE constraint", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE t (
			a INTEGER,
			b INTEGER,
			CONSTRAINT uq_ab UNIQUE (a, b)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Constraints, 1)
		assert.Equal(t, sqlmodel.ConstraintUnique, mutation.Constraints[0].Kind)
	})

	t.Run("PRIMARY KEY with named constraint", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE t (
			a INTEGER,
			b INTEGER,
			CONSTRAINT pk_ab PRIMARY KEY (a, b)
		)`)

		require.NotNil(t, mutation)
		assert.Equal(t, []string{"a", "b"}, mutation.PrimaryKey)
	})

	t.Run("multi-word type: CHARACTER VARYING", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE t (
			name CHARACTER VARYING(100)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 1)
		assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[0].SQLType.Category)
	})

	t.Run("GENERATED ALWAYS AS virtual column", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE products (
			price NUMERIC,
			tax NUMERIC,
			total NUMERIC GENERATED ALWAYS AS (price + tax) VIRTUAL
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 3)
		assert.True(t, mutation.Columns[2].IsGenerated)

		assert.Equal(t, sqlmodel.GeneratedKindVirtual, mutation.Columns[2].GeneratedKind)
	})

	t.Run("nested struct type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE t (
			data STRUCT(inner_struct STRUCT(x INTEGER, y INTEGER), label VARCHAR)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 1)
		assert.Equal(t, sqlmodel.TypeCategoryStruct, mutation.Columns[0].SQLType.Category)
	})

	t.Run("multi-dimensional array type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE t (
			matrix INTEGER[][]
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 1)
		assert.True(t, mutation.Columns[0].IsArray)
	})

	t.Run("numeric with precision and scale", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE TABLE t (
			amount NUMERIC(10, 2)
		)`)

		require.NotNil(t, mutation)
		require.Len(t, mutation.Columns, 1)
		assert.Equal(t, sqlmodel.TypeCategoryDecimal, mutation.Columns[0].SQLType.Category)
	})
}

func TestApplyDDL_AlterTable_ConditionalAndConstraints(t *testing.T) {
	t.Parallel()

	t.Run("ADD COLUMN IF NOT EXISTS", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE users ADD COLUMN IF NOT EXISTS age INTEGER`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
		require.Len(t, mutation.Columns, 1)
		assert.Equal(t, "age", mutation.Columns[0].Name)
	})

	t.Run("DROP COLUMN IF EXISTS", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE users DROP COLUMN IF EXISTS email`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableDropColumn, mutation.Kind)
		assert.Equal(t, "email", mutation.ColumnName)
	})

	t.Run("ADD CONSTRAINT CHECK", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE users ADD CHECK (age >= 0)`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableAddConstraint, mutation.Kind)
	})

	t.Run("ADD CONSTRAINT FOREIGN KEY", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `ALTER TABLE orders ADD FOREIGN KEY (user_id) REFERENCES users (id)`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterTableAddConstraint, mutation.Kind)
		require.Len(t, mutation.Constraints, 1)
		assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[0].Kind)
	})
}

func TestApplyDDL_CreateMacro_ArgumentVariants(t *testing.T) {
	t.Parallel()

	t.Run("macro with typed arguments", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE FUNCTION add_nums(a INTEGER, b INTEGER) AS a + b`)

		require.NotNil(t, mutation)
		require.NotNil(t, mutation.Function)
		assert.Equal(t, "add_nums", mutation.Function.Name)
		require.Len(t, mutation.Function.Arguments, 2)
	})

	t.Run("macro with default argument value", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `CREATE MACRO greet(name, greeting DEFAULT 'Hello') AS greeting || ', ' || name`)

		require.NotNil(t, mutation)
		require.NotNil(t, mutation.Function)
		assert.Equal(t, "greet", mutation.Function.Name)
	})
}

func TestApplyDDL_Comment_SchemaQualified(t *testing.T) {
	t.Parallel()

	t.Run("comment on schema-qualified column", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `COMMENT ON COLUMN myschema.users.email IS 'Email address'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationComment, mutation.Kind)
	})

	t.Run("comment on schema-qualified table", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `COMMENT ON TABLE myschema.users IS 'Users table'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationComment, mutation.Kind)
	})

	t.Run("comment on function with parentheses", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, `COMMENT ON FUNCTION add_one(integer) IS 'Adds one to an integer'`)

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationComment, mutation.Kind)
	})
}

func TestParseStatements_Classification(t *testing.T) {
	t.Parallel()

	engine := New()

	t.Run("CREATE TEMP TABLE classified correctly", func(t *testing.T) {
		t.Parallel()

		statements, err := engine.ParseStatements(t.Context(), `CREATE TEMP TABLE t (id INTEGER)`)
		require.NoError(t, err)
		require.Len(t, statements, 1)

		mutation, err := engine.ApplyDDL(t.Context(), statements[0])
		require.NoError(t, err)
		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	})

	t.Run("CREATE TEMPORARY TABLE classified correctly", func(t *testing.T) {
		t.Parallel()

		statements, err := engine.ParseStatements(t.Context(), `CREATE TEMPORARY TABLE t (id INTEGER)`)
		require.NoError(t, err)
		require.Len(t, statements, 1)

		mutation, err := engine.ApplyDDL(t.Context(), statements[0])
		require.NoError(t, err)
		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	})

	t.Run("ALTER TYPE classified correctly", func(t *testing.T) {
		t.Parallel()

		statements, err := engine.ParseStatements(t.Context(), `ALTER TYPE mood ADD VALUE 'calm'`)
		require.NoError(t, err)
		require.Len(t, statements, 1)

		mutation, err := engine.ApplyDDL(t.Context(), statements[0])
		require.NoError(t, err)
		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationAlterEnumAddValue, mutation.Kind)
	})

	t.Run("INSTALL classified as DDL returning nil", func(t *testing.T) {
		t.Parallel()

		statements, err := engine.ParseStatements(t.Context(), `INSTALL httpfs`)
		require.NoError(t, err)
		require.Len(t, statements, 1)

		mutation, err := engine.ApplyDDL(t.Context(), statements[0])
		require.NoError(t, err)
		assert.Nil(t, mutation, "INSTALL should return nil mutation")
	})

	t.Run("LOAD classified as DDL returning nil", func(t *testing.T) {
		t.Parallel()

		statements, err := engine.ParseStatements(t.Context(), `LOAD httpfs`)
		require.NoError(t, err)
		require.Len(t, statements, 1)

		mutation, err := engine.ApplyDDL(t.Context(), statements[0])
		require.NoError(t, err)
		assert.Nil(t, mutation, "LOAD should return nil mutation")
	})

	t.Run("VALUES statement is classified as DML", func(t *testing.T) {
		t.Parallel()

		statements, err := engine.ParseStatements(t.Context(), `VALUES (1, 2), (3, 4)`)
		require.NoError(t, err)
		require.Len(t, statements, 1)

		catalogue := buildTestCatalogue()
		analysis, err := engine.AnalyseQuery(t.Context(), catalogue, statements[0])
		require.NoError(t, err)
		assert.True(t, analysis.ReadOnly)
	})

	t.Run("DROP TABLE TABLE classified correctly", func(t *testing.T) {
		t.Parallel()

		statements, err := engine.ParseStatements(t.Context(), `DROP TABLE t`)
		require.NoError(t, err)
		require.Len(t, statements, 1)

		mutation, err := engine.ApplyDDL(t.Context(), statements[0])
		require.NoError(t, err)
		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationDropTable, mutation.Kind)
	})
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
		columns := engine.TableValuedFunctionColumns("generate_series")
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

	t.Run("DefaultSchema", func(t *testing.T) {
		t.Parallel()
		schema := engine.Info().SchemaSearchPath[0]
		assert.Equal(t, "main", schema)
	})

	t.Run("Dialect", func(t *testing.T) {
		t.Parallel()
		name := engine.Info().Name
		assert.Equal(t, "duckdb", name)
	})

	t.Run("SupportsReturning", func(t *testing.T) {
		t.Parallel()
		assert.NotZero(t, engine.Info().Returning)
	})

	t.Run("ParameterStyle", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}, engine.Info().Placeholders.Execution)
	})
}

func TestApplyDDL_CreateOrReplaceTable(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantColumns []string
	}{
		{
			name:        "OR REPLACE TABLE with a column list",
			sql:         `CREATE OR REPLACE TABLE events (id BIGINT PRIMARY KEY, slug VARCHAR NOT NULL)`,
			wantColumns: []string{"id", "slug"},
		},
		{
			name:        "OR REPLACE TEMP TABLE",
			sql:         `CREATE OR REPLACE TEMP TABLE events (id BIGINT)`,
			wantColumns: []string{"id"},
		},
		{
			name:        "OR REPLACE TABLE AS query",
			sql:         `CREATE OR REPLACE TABLE events AS SELECT 1 AS id`,
			wantColumns: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation)
			assert.Equal(t, ast.MutationDropTable, mutation.Kind)
			assert.Equal(t, "events", mutation.TableName)
			require.Len(t, mutation.AdditionalMutations, 1)
			create := mutation.AdditionalMutations[0]
			assert.Equal(t, ast.MutationCreateTable, create.Kind)
			assert.Equal(t, "events", create.TableName)
			var columnNames []string
			for _, column := range create.Columns {
				columnNames = append(columnNames, column.Name)
			}
			assert.Equal(t, testCase.wantColumns, columnNames)
		})
	}
}

func TestApplyDDL_CreateTableWithoutReplaceHasNoFollowUp(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, `CREATE TEMPORARY TABLE events (id BIGINT PRIMARY KEY, slug VARCHAR)`)

	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	assert.Empty(t, mutation.AdditionalMutations)
	assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
	require.Len(t, mutation.Columns, 2)
}

func TestApplyDDL_ArrayDimensions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantDimensions int
	}{
		{name: "single dimension", sql: `CREATE TABLE t (x INTEGER[])`, wantDimensions: 1},
		{name: "sized dimension", sql: `CREATE TABLE t (x INTEGER[3])`, wantDimensions: 1},
		{name: "maximum dimensions", sql: `CREATE TABLE t (x INTEGER[][][][][][])`, wantDimensions: maxArrayDimensions},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.Len(t, mutation.Columns, 1)
			assert.True(t, mutation.Columns[0].IsArray)
			assert.Equal(t, testCase.wantDimensions, mutation.Columns[0].ArrayDimensions)
		})
	}
}

func applyDDLError(t *testing.T, sql string) error {
	t.Helper()
	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	_, err = engine.ApplyDDL(t.Context(), statements[0])
	return err
}

func TestApplyDDL_CreateTableRequiresElementSeparators(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"CREATE TABLE t (a INTEGER b INTEGER)",
		"CREATE TABLE t (a INTEGER NOT NULL WITHOUT ROWID, b INTEGER)",
		"CREATE TABLE t (a TIMESTAMP(3) WITH TIME ZONE NOT NULL)",
		"CREATE TABLE t (a INTEGER, b VARCHAR) WITH (fillfactor = 10)",
		"CREATE TABLE t (a INTEGER",
	} {
		err := applyDDLError(t, sql)
		require.ErrorIs(t, err, fault.ErrParse, sql)
	}
}

func TestApplyDDL_QuotedKeywordsAreColumnNames(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, `CREATE TABLE "order" (id BIGINT PRIMARY KEY, "primary" BOOLEAN NOT NULL, `+
		`"check" VARCHAR, "constraint" INTEGER NOT NULL, "unique" INTEGER, "not" VARCHAR NOT NULL, "default" INTEGER)`)

	names := make([]string, 0, len(mutation.Columns))
	for _, column := range mutation.Columns {
		names = append(names, column.Name)
	}
	assert.Equal(t, "order", mutation.TableName)
	assert.Equal(t, []string{"id", "primary", "check", "constraint", "unique", "not", "default"}, names)
	assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
	assert.False(t, mutation.Columns[1].Nullable)
	assert.True(t, mutation.Columns[2].Nullable)
	assert.False(t, mutation.Columns[3].Nullable)
	assert.False(t, mutation.Columns[5].Nullable)
}

func TestApplyDDL_PrimaryKeyHasNoDefault(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE TABLE t (id INTEGER PRIMARY KEY, seq BIGINT DEFAULT nextval('s'))")

	assert.False(t, mutation.Columns[0].HasDefault, "a DuckDB primary key fills no value of its own")
	assert.True(t, mutation.Columns[1].HasDefault)
}

func TestApplyDDL_GeneratedColumnForms(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE TABLE t (a INTEGER, b INTEGER GENERATED ALWAYS AS (a * 2) VIRTUAL, "+
		"c INTEGER AS (a + 1), d AS (a - 1), e INTEGER GENERATED ALWAYS AS (a) STORED)")

	require.Len(t, mutation.Columns, 5)
	for index, want := range []sqlmodel.GeneratedKind{sqlmodel.GeneratedKindNone, sqlmodel.GeneratedKindVirtual,
		sqlmodel.GeneratedKindVirtual, sqlmodel.GeneratedKindVirtual, sqlmodel.GeneratedKindStored} {
		assert.Equal(t, want, mutation.Columns[index].GeneratedKind, mutation.Columns[index].Name)
	}
	assert.Equal(t, sqlmodel.TypeCategoryUnknown, mutation.Columns[3].SQLType.Category)
}

func TestApplyDDL_AlterColumnNullability(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		sql              string
		wantType         string
		wantSetsNotNull  bool
		wantDropsNotNull bool
	}{
		{name: "set not null", sql: "ALTER TABLE t ALTER COLUMN a SET NOT NULL", wantSetsNotNull: true},
		{name: "set not null without COLUMN", sql: "ALTER TABLE t ALTER a SET NOT NULL", wantSetsNotNull: true},
		{name: "drop not null", sql: "ALTER TABLE t ALTER COLUMN a DROP NOT NULL", wantDropsNotNull: true},
		{name: "set default", sql: "ALTER TABLE t ALTER COLUMN a SET DEFAULT 1"},
		{name: "drop default", sql: "ALTER TABLE t ALTER COLUMN a DROP DEFAULT"},
		{name: "type with using", sql: "ALTER TABLE t ALTER COLUMN a TYPE VARCHAR USING a::VARCHAR", wantType: "varchar"},
		{name: "set data type", sql: "ALTER TABLE t ALTER COLUMN a SET DATA TYPE DECIMAL(12, 2)", wantType: "numeric"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.Equal(t, ast.MutationAlterTableAlterColumn, mutation.Kind)
			assert.Equal(t, "a", mutation.ColumnName)
			assert.Equal(t, testCase.wantSetsNotNull, mutation.SetsNotNull)
			assert.Equal(t, testCase.wantDropsNotNull, mutation.DropsNotNull)
			assert.Equal(t, testCase.wantType != "", mutation.SetsType)
			if testCase.wantType == "" {
				assert.Empty(t, mutation.Columns)
				return
			}
			require.Len(t, mutation.Columns, 1)
			assert.Equal(t, testCase.wantType, mutation.Columns[0].SQLType.EngineName)
		})
	}
}

func TestApplyDDL_AddColumnRejectsWhatDuckDBRejects(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"ALTER TABLE t ADD COLUMN a VARCHAR NOT NULL DEFAULT ''",
		"ALTER TABLE t ADD COLUMN a INTEGER UNIQUE",
		"ALTER TABLE t ADD COLUMN a INTEGER CHECK (a > 0)",
		"ALTER TABLE t ADD COLUMN a INTEGER REFERENCES u (id)",
		"ALTER TABLE t ADD COLUMN a INTEGER GENERATED ALWAYS AS (1)",
		"ALTER TABLE t ADD COLUMN a INTEGER PRIMARY KEY",
	} {
		err := applyDDLError(t, sql)
		require.ErrorIs(t, err, fault.ErrParse, sql)
		assert.Contains(t, err.Error(), "SET NOT NULL", sql)
	}

	for _, sql := range []string{
		"ALTER TABLE t ADD COLUMN a VARCHAR DEFAULT 'x'",
		"ALTER TABLE t ADD COLUMN IF NOT EXISTS a INTEGER NULL",
		"ALTER TABLE t ADD a VARCHAR COLLATE NOCASE",
	} {
		require.NoError(t, applyDDLError(t, sql), sql)
	}
}

func TestApplyDDL_AlterTableRequiresEndOfStatement(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"ALTER TABLE t DROP COLUMN a b",
		"ALTER TABLE t RENAME TO u v",
		"ALTER TABLE t FROB",
	} {
		require.ErrorIs(t, applyDDLError(t, sql), fault.ErrParse, sql)
	}
	require.NoError(t, applyDDLError(t, "ALTER TABLE t DROP COLUMN IF EXISTS a CASCADE"))
}

func TestApplyDDL_ListAndArrayAreNotTypeConstructors(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"CREATE TABLE lists (items LIST(INTEGER))",
		"CREATE TABLE lists (items ARRAY(INTEGER))",
		"CREATE TABLE lists (items MAP(VARCHAR, LIST(INTEGER)))",
	} {
		err := applyDDLError(t, sql)

		require.ErrorIs(t, err, fault.ErrParse, sql)
		assert.Contains(t, err.Error(), "write a list of T as T[]", sql)
	}
}

func TestApplyDDL_NestedTypesKeepTheirListSuffix(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, `CREATE TABLE routes (
		waypoints STRUCT(x DOUBLE, tags VARCHAR[]),
		metadata MAP(VARCHAR, INTEGER[]),
		payload UNION(numbers INTEGER[], label VARCHAR)
	)`)

	require.Len(t, mutation.Columns, 3)
	waypoints := mutation.Columns[0].SQLType
	require.Len(t, waypoints.StructFields, 2)
	assert.Equal(t, "varchar[]", waypoints.StructFields[1].SQLType.EngineName)
	assert.Equal(t, "int4[]", mutation.Columns[1].SQLType.ElementType.EngineName)
	assert.Equal(t, "int4[]", mutation.Columns[2].SQLType.UnionMembers[0].SQLType.EngineName)
}

func TestApplyDDL_StructFieldsAreAlwaysNullable(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, `CREATE TABLE berths (
		info STRUCT(number INTEGER, pontoon STRUCT(name VARCHAR)) NOT NULL,
		history STRUCT(at TIMESTAMP)[] NOT NULL
	)`)

	require.Len(t, mutation.Columns, 2)
	info := mutation.Columns[0].SQLType
	require.Len(t, info.StructFields, 2)
	for _, field := range info.StructFields {
		assert.True(t, field.SQLType.Nullable, field.Name)
	}
	require.Len(t, info.StructFields[1].SQLType.StructFields, 1)
	assert.True(t, info.StructFields[1].SQLType.StructFields[0].SQLType.Nullable)
	assert.False(t, mutation.Columns[0].Nullable)
}

func TestApplyDDLRecordsHowAStatementReplays(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		check func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "a table-level key makes its columns NOT NULL",
			sql:  "CREATE TABLE IF NOT EXISTS pairs (a INTEGER, b INTEGER, note TEXT, PRIMARY KEY (a, b))",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()
				require.True(t, mutation.IfNotExists)
				require.False(t, mutation.Columns[0].Nullable)
				require.False(t, mutation.Columns[1].Nullable)
				require.True(t, mutation.Columns[2].Nullable)
			},
		},
		{
			name: "ALTER TABLE IF EXISTS ADD COLUMN IF NOT EXISTS",
			sql:  "ALTER TABLE IF EXISTS items ADD COLUMN IF NOT EXISTS name TEXT",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()
				require.True(t, mutation.IfExists)
				require.True(t, mutation.IfNotExists)
			},
		},
		{
			name: "ALTER VIEW RENAME TO",
			sql:  "ALTER VIEW IF EXISTS items_view RENAME TO renamed_view",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()
				require.Equal(t, ast.MutationAlterTableRenameTable, mutation.Kind)
				require.Equal(t, "items_view", mutation.TableName)
				require.Equal(t, "renamed_view", mutation.NewName)
				require.True(t, mutation.IfExists)
			},
		},
		{
			name: "CREATE VIEW keeps its declared names",
			sql:  "CREATE VIEW IF NOT EXISTS named_view (item_id, item_name) AS SELECT * FROM items",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()
				require.True(t, mutation.IfNotExists)
				require.NotNil(t, mutation.ViewDefinition)
				require.True(t, mutation.ViewDefinition.OutputColumns[0].IsStar)
				require.Equal(t, []string{"item_id", "item_name"}, []string{mutation.Columns[0].Name, mutation.Columns[1].Name})
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

func TestAWalkStoppedInsideAViewOrTableMacroBodyFailsTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a view body", sql: "CREATE VIEW v AS SELECT $1::hooked, " + numberedList("%d", walkedItems)},
		{name: "a view body under a column list", sql: "CREATE VIEW v (a) AS SELECT $1::hooked, " + numberedList("%d", walkedItems)},
		{name: "a table created from a query", sql: "CREATE TABLE t AS SELECT $1::hooked, " + numberedList("%d", walkedItems)},
		{name: "a table macro body", sql: "CREATE MACRO m() AS TABLE SELECT $1::hooked, " + numberedList("%d", walkedItems)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			engine := New(cancelOnType("hooked", cancel))

			mutation, err := engine.ApplyDDL(ctx, parseOne(t, engine, testCase.sql))

			require.ErrorIs(t, err, errWalkStopped)
			assert.Nil(t, mutation)
		})
	}
}

func TestApplyDDLRequiresATypeWhereDuckDBDoes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a column with no type", sql: "CREATE TABLE t (a)"},
		{name: "a column with a constraint but no type", sql: "CREATE TABLE t (a NOT NULL)"},
		{name: "a column with no type before another", sql: "CREATE TABLE t (a, b INTEGER)"},
		{name: "an added column with no type", sql: "ALTER TABLE t ADD COLUMN c"},
		{name: "a column altered to no type", sql: "ALTER TABLE t ALTER COLUMN a TYPE"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := applyDDLError(t, testCase.sql)

			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, "expected a type", parseError.Message)
		})
	}
}

func TestApplyDDLAcceptsAGeneratedColumnWithNoType(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"CREATE TABLE t (a INTEGER, b AS (a + 1))",
		"CREATE TABLE t (a INTEGER, b GENERATED ALWAYS AS (a + 1) STORED)",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, sql)

			require.Len(t, mutation.Columns, 2)
			assert.True(t, mutation.Columns[1].IsGenerated)
		})
	}
}

func TestATablePrimaryKeyWithoutItsColumnListIsPlacedAtTheTokenAfterKey(t *testing.T) {
	t.Parallel()

	sql := "CREATE TABLE t (a INTEGER, PRIMARY KEY a)"

	err := applyDDLError(t, sql)

	require.ErrorIs(t, err, fault.ErrParse)
	var parseError *dialectkit.ParseError
	require.ErrorAs(t, err, &parseError)
	assert.Equal(t, "a", parseError.Token)
	assert.Equal(t, strings.LastIndex(sql, "a)"), parseError.Position)
}

func TestApplyDDLRefusesATypeNestedDeeperThanSeagullReads(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		refused bool
	}{
		{name: "structs nested just within the limit",
			sql: "CREATE TABLE t (a " + nestedStructType(sqlmodel.MaxTypeDepth-1, "INTEGER") + ")", refused: false},
		{name: "structs nested to the limit",
			sql: "CREATE TABLE t (a " + nestedStructType(sqlmodel.MaxTypeDepth, "INTEGER") + ")", refused: true},
		{name: "maps nested to the limit",
			sql: "CREATE TABLE t (a " + nestedMapType(sqlmodel.MaxTypeDepth) + ")", refused: true},
		{name: "a field's list dimensions taking it to the limit",
			sql: "CREATE TABLE t (a " + nestedStructType(sqlmodel.MaxTypeDepth-2, "INTEGER[][]") + ")", refused: true},
		{name: "a column's array dimensions taking it to the limit",
			sql: "CREATE TABLE t (a " + nestedStructType(sqlmodel.MaxTypeDepth-2, "INTEGER") + "[][])", refused: true},
		{name: "a composite type field nested to the limit",
			sql: "CREATE TYPE t AS (a " + nestedStructType(sqlmodel.MaxTypeDepth, "INTEGER") + ")", refused: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := applyDDLError(t, testCase.sql)

			if !testCase.refused {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errTypeDepth)
			require.ErrorIs(t, err, fault.ErrLimit)
			require.ErrorContains(t, err, "64 levels or more")
		})
	}
}

func nestedStructType(levels int, innermost string) string {
	return strings.Repeat("STRUCT(f ", levels) + innermost + strings.Repeat(")", levels)
}

func nestedMapType(levels int) string {
	return strings.Repeat("MAP(VARCHAR, ", levels) + "INTEGER" + strings.Repeat(")", levels)
}

func TestApplyDDLRecordsWhetherADropSaidIfExists(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantKind     ast.MutationKind
		wantIfExists bool
	}{
		{name: "DROP TABLE", sql: "DROP TABLE boats", wantKind: ast.MutationDropTable, wantIfExists: false},
		{name: "DROP TABLE IF EXISTS", sql: "DROP TABLE IF EXISTS boats", wantKind: ast.MutationDropTable, wantIfExists: true},
		{name: "DROP VIEW", sql: "DROP VIEW harbour_view", wantKind: ast.MutationDropView, wantIfExists: false},
		{name: "DROP VIEW IF EXISTS", sql: "DROP VIEW IF EXISTS harbour_view", wantKind: ast.MutationDropView, wantIfExists: true},
		{name: "DROP SEQUENCE", sql: "DROP SEQUENCE ids", wantKind: ast.MutationDropSequence, wantIfExists: false},
		{name: "DROP SEQUENCE IF EXISTS", sql: "DROP SEQUENCE IF EXISTS ids", wantKind: ast.MutationDropSequence, wantIfExists: true},
		{name: "DROP INDEX", sql: "DROP INDEX boats_name", wantKind: ast.MutationDropIndex, wantIfExists: false},
		{name: "DROP INDEX IF EXISTS", sql: "DROP INDEX IF EXISTS boats_name", wantKind: ast.MutationDropIndex, wantIfExists: true},
		{name: "DROP SCHEMA", sql: "DROP SCHEMA reports", wantKind: ast.MutationDropSchema, wantIfExists: false},
		{name: "DROP SCHEMA IF EXISTS", sql: "DROP SCHEMA IF EXISTS reports", wantKind: ast.MutationDropSchema, wantIfExists: true},
		{name: "DROP TYPE", sql: "DROP TYPE mood", wantKind: ast.MutationDropType, wantIfExists: false},
		{name: "DROP TYPE IF EXISTS", sql: "DROP TYPE IF EXISTS mood", wantKind: ast.MutationDropType, wantIfExists: true},
		{name: "DROP MACRO", sql: "DROP MACRO add_one", wantKind: ast.MutationDropFunction, wantIfExists: false},
		{name: "DROP FUNCTION IF EXISTS", sql: "DROP FUNCTION IF EXISTS add_one", wantKind: ast.MutationDropFunction, wantIfExists: true},
		{name: "DROP MACRO TABLE IF EXISTS", sql: "DROP MACRO TABLE IF EXISTS recent_boats", wantKind: ast.MutationDropFunction,
			wantIfExists: true},
		{name: "the drop CREATE OR REPLACE TABLE makes first", sql: "CREATE OR REPLACE TABLE boats (id INTEGER)",
			wantKind: ast.MutationDropTable, wantIfExists: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation)
			require.Equal(t, testCase.wantKind, mutation.Kind)
			require.Equal(t, testCase.wantIfExists, mutation.IfExists)
		})
	}
}

func TestCreateOrReplaceTableCreatesWhetherOrNotTheTableExists(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE OR REPLACE TABLE boats (id INTEGER)")

	require.Len(t, mutation.AdditionalMutations, 1)
	created := mutation.AdditionalMutations[0]
	require.Equal(t, ast.MutationCreateTable, created.Kind)
	require.False(t, created.IfExists)
	require.False(t, created.IfNotExists)
}
