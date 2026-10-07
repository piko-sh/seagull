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

package postgres

import (
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	{name: "a materialised view in a schema", sql: "COMMENT ON MATERIALIZED VIEW reporting.totals IS 'Totals per day'",
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

func applyDDLExpectError(t *testing.T, sql string) error {
	t.Helper()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.NotEmpty(t, statements)

	_, applyError := engine.ApplyDDL(t.Context(), statements[0])
	return applyError
}

func TestApplyDDL_CreateFunction_MalformedArgumentListDoesNotHang(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"CREATE FUNCTION f($1 int) RETURNS int AS $$ $$ LANGUAGE sql",
		"CREATE FUNCTION f(= int) RETURNS int AS $$ $$ LANGUAGE sql",
		"CREATE FUNCTION f(123 int) RETURNS int AS $$ $$ LANGUAGE sql",
	} {
		err := applyDDLExpectError(t, sql)
		require.Error(t, err, "expected a parse error (not a hang) for %q", sql)
	}
}

func TestApplyDDL_AlterTable_RejectsConsecutiveCommas(t *testing.T) {
	t.Parallel()

	err := applyDDLExpectError(t, "ALTER TABLE users ADD COLUMN a text, , ADD COLUMN b text")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "consecutive commas")
}

func TestApplyDDL_CreateTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple table with primary key and not null",
			sql:  "CREATE TABLE users (id integer PRIMARY KEY, name text NOT NULL)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				assert.Equal(t, "", mutation.SchemaName)
				require.Len(t, mutation.Columns, 2)

				assert.Equal(t, "id", mutation.Columns[0].Name)
				assert.Equal(t, sqlmodel.TypeCategoryInteger, mutation.Columns[0].SQLType.Category)
				assert.Equal(t, "int4", mutation.Columns[0].SQLType.EngineName)
				assert.False(t, mutation.Columns[0].Nullable, "primary key column should not be nullable")
				assert.False(t, mutation.Columns[0].HasDefault, "an integer primary key has no default of its own")

				assert.Equal(t, "name", mutation.Columns[1].Name)
				assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[1].SQLType.Category)
				assert.False(t, mutation.Columns[1].Nullable, "NOT NULL column should not be nullable")

				assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
			},
		},
		{
			name: "schema-qualified table name",
			sql:  "CREATE TABLE myschema.users (id integer PRIMARY KEY)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "myschema", mutation.SchemaName)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "IF NOT EXISTS is accepted",
			sql:  "CREATE TABLE IF NOT EXISTS users (id integer PRIMARY KEY)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				require.Len(t, mutation.Columns, 1)
			},
		},
		{
			name: "column with DEFAULT expression",
			sql:  "CREATE TABLE t (x int DEFAULT 0)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.True(t, mutation.Columns[0].HasDefault, "column with DEFAULT should have HasDefault set")
			},
		},
		{
			name: "serial column normalises to int4",
			sql:  "CREATE TABLE t (id serial)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "int4", mutation.Columns[0].SQLType.EngineName)
				assert.Equal(t, sqlmodel.TypeCategoryInteger, mutation.Columns[0].SQLType.Category)
			},
		},
		{
			name: "array column sets IsArray flag",
			sql:  "CREATE TABLE t (tags text[])",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.True(t, mutation.Columns[0].IsArray, "text[] column should have IsArray set")
				assert.Equal(t, 1, mutation.Columns[0].ArrayDimensions)
			},
		},
		{
			name: "INHERITS clause captures parent tables",
			sql:  "CREATE TABLE child_table (extra text) INHERITS (parent_one, parent_two)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "child_table", mutation.TableName)
				require.Len(t, mutation.InheritsTables, 2)
				assert.Equal(t, "parent_one", mutation.InheritsTables[0].Name)
				assert.Equal(t, "parent_two", mutation.InheritsTables[1].Name)
			},
		},
		{
			name: "table-level FOREIGN KEY constraint",
			sql:  "CREATE TABLE orders (id integer, user_id integer, CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES users (id))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[0].Kind)
				assert.Equal(t, "fk_user", mutation.Constraints[0].Name)
				assert.Equal(t, []string{"user_id"}, mutation.Constraints[0].Columns)
				assert.Equal(t, "users", mutation.Constraints[0].ForeignTable)
				assert.Equal(t, []string{"id"}, mutation.Constraints[0].ForeignColumns)
			},
		},
		{
			name: "table-level CHECK constraint",
			sql:  "CREATE TABLE products (id integer, price numeric, CONSTRAINT chk_price CHECK (price > 0))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintCheck, mutation.Constraints[0].Kind)
				assert.Equal(t, "chk_price", mutation.Constraints[0].Name)
			},
		},
		{
			name: "table-level UNIQUE constraint",
			sql:  "CREATE TABLE users (id integer, email text, UNIQUE (email))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintUnique, mutation.Constraints[0].Kind)
				assert.Equal(t, []string{"email"}, mutation.Constraints[0].Columns)
			},
		},
		{
			name: "table-level composite PRIMARY KEY",
			sql:  "CREATE TABLE order_items (order_id integer, product_id integer, quantity integer, PRIMARY KEY (order_id, product_id))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 3)
				assert.Equal(t, []string{"order_id", "product_id"}, mutation.PrimaryKey)
			},
		},
		{
			name: "multiple column constraints on one column",
			sql:  "CREATE TABLE t (email text NOT NULL UNIQUE DEFAULT 'unknown')",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.False(t, mutation.Columns[0].Nullable)
				assert.True(t, mutation.Columns[0].HasDefault)
			},
		},
		{
			name: "column with REFERENCES constraint",
			sql:  "CREATE TABLE posts (id integer, author_id integer REFERENCES users (id))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
				assert.Equal(t, "author_id", mutation.Columns[1].Name)
			},
		},
		{
			name: "column with CHECK constraint",
			sql:  "CREATE TABLE t (age integer CHECK (age >= 0))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "age", mutation.Columns[0].Name)
			},
		},
		{
			name: "column with GENERATED ALWAYS AS IDENTITY",
			sql:  "CREATE TABLE t (id integer GENERATED ALWAYS AS IDENTITY)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.True(t, mutation.Columns[0].HasDefault)
			},
		},
		{
			name: "column with GENERATED BY DEFAULT AS IDENTITY",
			sql:  "CREATE TABLE t (id integer GENERATED BY DEFAULT AS IDENTITY)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.True(t, mutation.Columns[0].HasDefault)
			},
		},
		{
			name: "column with GENERATED ALWAYS AS (expression) STORED",
			sql:  "CREATE TABLE t (a integer, b integer, c integer GENERATED ALWAYS AS (a + b) STORED)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 3)
				assert.True(t, mutation.Columns[2].IsGenerated)
				assert.Equal(t, sqlmodel.GeneratedKindStored, mutation.Columns[2].GeneratedKind)
			},
		},
		{
			name: "TEMP TABLE is accepted",
			sql:  "CREATE TEMP TABLE scratch (id integer)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "scratch", mutation.TableName)
			},
		},
		{
			name: "UNLOGGED TABLE is accepted",
			sql:  "CREATE UNLOGGED TABLE events (id integer, payload text)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "events", mutation.TableName)
			},
		},
		{
			name: "CREATE TABLE AS is accepted",
			sql:  "CREATE TABLE summary AS SELECT count(*) FROM users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				assert.Equal(t, "summary", mutation.TableName)
			},
		},
		{
			name: "timestamp with time zone column type",
			sql:  "CREATE TABLE t (created_at timestamp with time zone)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "timestamptz", mutation.Columns[0].SQLType.EngineName)
			},
		},
		{
			name: "character varying column type with length",
			sql:  "CREATE TABLE t (name character varying(100))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "varchar", mutation.Columns[0].SQLType.EngineName)
			},
		},
		{
			name: "double precision column type",
			sql:  "CREATE TABLE t (val double precision)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "float8", mutation.Columns[0].SQLType.EngineName)
			},
		},
		{
			name: "numeric with precision and scale",
			sql:  "CREATE TABLE t (price numeric(10,2))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "numeric", mutation.Columns[0].SQLType.EngineName)
			},
		},
		{
			name: "column with COLLATE",
			sql:  `CREATE TABLE t (name text COLLATE "en_GB")`,
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "name", mutation.Columns[0].Name)
			},
		},
		{
			name: "column explicitly marked NULL",
			sql:  "CREATE TABLE t (description text NULL)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.True(t, mutation.Columns[0].Nullable, "NULL column should be nullable")
			},
		},
		{
			name: "multi-dimensional array column",
			sql:  "CREATE TABLE t (matrix integer[][])",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.True(t, mutation.Columns[0].IsArray)
				assert.Equal(t, 2, mutation.Columns[0].ArrayDimensions)
			},
		},
		{
			name: "multiple table-level constraints in one table",
			sql: `CREATE TABLE inventory (
				id integer,
				warehouse_id integer,
				product_id integer,
				quantity integer,
				PRIMARY KEY (id),
				CONSTRAINT fk_warehouse FOREIGN KEY (warehouse_id) REFERENCES warehouses (id),
				CHECK (quantity >= 0)
			)`,
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 4)
				assert.Equal(t, []string{"id"}, mutation.PrimaryKey)

				require.Len(t, mutation.Constraints, 2)
				assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[0].Kind)
				assert.Equal(t, sqlmodel.ConstraintCheck, mutation.Constraints[1].Kind)
			},
		},
		{
			name: "time without time zone column type",
			sql:  "CREATE TABLE t (event_time time without time zone)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "time", mutation.Columns[0].SQLType.EngineName)
			},
		},
		{
			name: "time with time zone column type",
			sql:  "CREATE TABLE t (event_time time with time zone)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "timetz", mutation.Columns[0].SQLType.EngineName)
			},
		},
		{
			name: "timestamp without time zone column type",
			sql:  "CREATE TABLE t (created_at timestamp without time zone)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "timestamp", mutation.Columns[0].SQLType.EngineName)
			},
		},
		{
			name: "column with DEFAULT expression involving function call",
			sql:  "CREATE TABLE t (created_at timestamp DEFAULT now())",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.True(t, mutation.Columns[0].HasDefault)
			},
		},
		{
			name: "EXCLUDE constraint is accepted",
			sql:  "CREATE TABLE t (id integer, period tsrange, EXCLUDE USING gist (period WITH &&))",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
			},
		},
		{
			name: "bigserial column normalises to int8",
			sql:  "CREATE TABLE t (id bigserial)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "int8", mutation.Columns[0].SQLType.EngineName)
			},
		},
		{
			name: "boolean column type",
			sql:  "CREATE TABLE t (active boolean NOT NULL DEFAULT true)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, sqlmodel.TypeCategoryBoolean, mutation.Columns[0].SQLType.Category)
				assert.False(t, mutation.Columns[0].Nullable)
				assert.True(t, mutation.Columns[0].HasDefault)
			},
		},
		{
			name: "uuid column type",
			sql:  "CREATE TABLE t (id uuid PRIMARY KEY)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "uuid", mutation.Columns[0].SQLType.EngineName)
			},
		},
		{
			name: "jsonb column type",
			sql:  "CREATE TABLE t (data jsonb)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "jsonb", mutation.Columns[0].SQLType.EngineName)
			},
		},
		{
			name: "schema-qualified type for column",
			sql:  "CREATE TABLE t (status myschema.status_type)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "myschema", mutation.Columns[0].SQLType.Schema)
			},
		},
		{
			name: "FOREIGN KEY with ON DELETE CASCADE action",
			sql:  "CREATE TABLE orders (id integer, user_id integer, CONSTRAINT fk_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[0].Kind)

				require.Len(t, mutation.Columns, 2, "ON DELETE clause leaked into the column list")
			},
		},
		{

			name: "two table-level FOREIGN KEYs with ON DELETE CASCADE",
			sql: `CREATE TABLE identity.oauth_tokens (
                id uuid PRIMARY KEY,
                account_id uuid NOT NULL,
                authentication_method_id uuid NOT NULL,
                created_at timestamptz NOT NULL,
                FOREIGN KEY (account_id) REFERENCES identity.accounts (id) ON DELETE CASCADE,
                FOREIGN KEY (authentication_method_id) REFERENCES identity.authentication_methods (id) ON DELETE CASCADE
            )`,
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.Columns, 4, "ON DELETE CASCADE leaked into the column list")
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
                id uuid PRIMARY KEY,
                user_id uuid REFERENCES users (id) ON DELETE CASCADE,
                created_at timestamptz NOT NULL
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
			name: "INHERITS with schema-qualified parent",
			sql:  "CREATE TABLE child (extra text) INHERITS (myschema.parent)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
				require.Len(t, mutation.InheritsTables, 1)
				assert.Equal(t, "myschema", mutation.InheritsTables[0].Schema)
				assert.Equal(t, "parent", mutation.InheritsTables[0].Name)
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

func TestASchemaQualifiedTypeNamesItsSchemaAsTheDialectFoldsIt(t *testing.T) {
	t.Parallel()

	columnType := func(mutation *ast.CatalogueMutation) sqlmodel.SQLType { return mutation.Columns[0].SQLType }
	returnType := func(mutation *ast.CatalogueMutation) sqlmodel.SQLType { return mutation.Function.ReturnType }
	argumentType := func(mutation *ast.CatalogueMutation) sqlmodel.SQLType { return mutation.Function.Arguments[0].Type }
	testCases := []struct {
		typeOf     func(mutation *ast.CatalogueMutation) sqlmodel.SQLType
		name       string
		sql        string
		wantSchema string
	}{
		{name: "a column's unquoted schema folds", sql: "CREATE TABLE t (pair Types.pair)", typeOf: columnType, wantSchema: "types"},
		{name: "a column's quoted schema keeps its case", sql: `CREATE TABLE t (pair "Types".pair)`, typeOf: columnType, wantSchema: "Types"},
		{name: "an array column's unquoted schema folds", sql: "CREATE TABLE t (pairs TYPES.pair[])", typeOf: columnType, wantSchema: "types"},
		{name: "a return type's unquoted schema folds", sql: "CREATE FUNCTION f() RETURNS Types.pair LANGUAGE sql AS $$ SELECT NULL $$",
			typeOf: returnType, wantSchema: "types"},
		{name: "a return type's quoted schema keeps its case", sql: `CREATE FUNCTION f() RETURNS "Types".pair LANGUAGE sql AS $$ SELECT NULL $$`,
			typeOf: returnType, wantSchema: "Types"},
		{name: "a set's unquoted schema folds", sql: "CREATE FUNCTION f() RETURNS SETOF Types.pair LANGUAGE sql AS $$ SELECT NULL $$",
			typeOf: returnType, wantSchema: "types"},
		{name: "an argument's unquoted schema folds", sql: "CREATE FUNCTION f(value Types.pair) RETURNS int4 LANGUAGE sql AS $$ SELECT 1 $$",
			typeOf: argumentType, wantSchema: "types"},
		{name: "an argument's quoted schema keeps its case", sql: `CREATE FUNCTION f(value "Types".pair) RETURNS int4 LANGUAGE sql AS $$ SELECT 1 $$`,
			typeOf: argumentType, wantSchema: "Types"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			sqlType := testCase.typeOf(mutation)
			assert.Equal(t, testCase.wantSchema, sqlType.Schema)
			assert.Equal(t, "pair", sqlType.EngineName)
		})
	}
}

func TestAQuotedTypeNameKeepsItsLettersAndAnUnquotedOneFolds(t *testing.T) {
	t.Parallel()

	columnType := func(mutation *ast.CatalogueMutation) sqlmodel.SQLType { return mutation.Columns[0].SQLType }
	returnType := func(mutation *ast.CatalogueMutation) sqlmodel.SQLType { return mutation.Function.ReturnType }
	argumentType := func(mutation *ast.CatalogueMutation) sqlmodel.SQLType { return mutation.Function.Arguments[0].Type }
	testCases := []struct {
		typeOf         func(mutation *ast.CatalogueMutation) sqlmodel.SQLType
		name           string
		sql            string
		wantCategory   sqlmodel.SQLTypeCategory
		wantSchema     string
		wantEngineName string
	}{
		{name: "a quoted column type", sql: `CREATE TABLE t (mood "Mood")`, typeOf: columnType,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: "Mood"},
		{name: "an unquoted column type of the same letters", sql: "CREATE TABLE t (mood Mood)", typeOf: columnType,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: "mood"},
		{name: "a quoted array column type", sql: `CREATE TABLE t (moods "Mood"[])`, typeOf: columnType,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: "Mood"},
		{name: "a quoted qualified column type", sql: `CREATE TABLE t (pair "Shapes"."Pair")`, typeOf: columnType,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantSchema: "Shapes", wantEngineName: "Pair"},
		{name: "a quoted return type", sql: `CREATE FUNCTION f() RETURNS "Shapes"."Pair" LANGUAGE sql AS $$ SELECT NULL $$`,
			typeOf: returnType, wantCategory: sqlmodel.TypeCategoryUnknown, wantSchema: "Shapes", wantEngineName: "Pair"},
		{name: "a quoted set's type", sql: `CREATE FUNCTION f() RETURNS SETOF "Mood" LANGUAGE sql AS $$ SELECT NULL $$`,
			typeOf: returnType, wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: "Mood"},
		{name: "a quoted argument type", sql: `CREATE FUNCTION f(value "Mood") RETURNS int4 LANGUAGE sql AS $$ SELECT 1 $$`,
			typeOf: argumentType, wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: "Mood"},
		{name: "a quoted built-in name in lower case is the built-in", sql: `CREATE TABLE t (count "integer")`, typeOf: columnType,
			wantCategory: sqlmodel.TypeCategoryInteger, wantEngineName: "int4"},
		{name: "a quoted built-in name in another case is not the built-in", sql: `CREATE TABLE t (corner "Point")`, typeOf: columnType,
			wantCategory: sqlmodel.TypeCategoryUnknown, wantEngineName: "Point"},
		{name: "an unquoted built-in name in another case is the built-in", sql: "CREATE TABLE t (corner POINT)", typeOf: columnType,
			wantCategory: sqlmodel.TypeCategoryGeometric, wantEngineName: "point"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sqlType := testCase.typeOf(applyDDL(t, testCase.sql))
			assert.Equal(t, testCase.wantCategory, sqlType.Category)
			assert.Equal(t, testCase.wantSchema, sqlType.Schema)
			assert.Equal(t, testCase.wantEngineName, sqlType.EngineName)
		})
	}
}

func TestAQuotedWordOfATypeIsNeverAKeyword(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		columnType string
	}{
		{name: "double precision", columnType: `"double" precision`},
		{name: "character varying", columnType: `"character" varying(3)`},
		{name: "timestamp with time zone", columnType: `"timestamp" with time zone`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := applyDDLExpectError(t, "CREATE TABLE t (c "+testCase.columnType+")")
			require.ErrorIs(t, err, fault.ErrParse)
		})
	}
}

func TestATypeModifierListIsReadAsTheMagnitudesOfItsNumbers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		columnType    string
		wantModifiers []int
	}{
		{name: "integers", columnType: "numeric(10, 2)", wantModifiers: []int{10, 2}},
		{name: "a fraction is truncated", columnType: "numeric(10.5, 2)", wantModifiers: []int{10, 2}},
		{name: "an exponent", columnType: "varchar(1e3)", wantModifiers: []int{1000}},
		{name: "a hexadecimal integer", columnType: "varchar(0x10)", wantModifiers: []int{16}},
		{name: "a magnitude past the bound is held at it", columnType: "varchar(99999999999)", wantModifiers: []int{dialectkit.MaxTypeModifier}},
		{name: "numbers past the second are left out", columnType: "numeric(10, 2, 7)", wantModifiers: []int{10, 2}},
		{name: "a word in the list is passed over", columnType: "varchar(max)", wantModifiers: nil},
		{name: "a negative scale keeps its sign", columnType: "numeric(5, -2)", wantModifiers: []int{5, -2}},
		{name: "a negative magnitude past the bound is held at it", columnType: "numeric(5, -99999999999)",
			wantModifiers: []int{5, -dialectkit.MaxTypeModifier}},
		{name: "a number in nested parentheses", columnType: "numeric((4), 2)", wantModifiers: []int{4, 2}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var received []int
			engine := New(WithTypeNormaliserHook(func(_ string, modifiers []int) *sqlmodel.SQLType {
				received = slices.Clone(modifiers)
				return nil
			}))
			mutation, err := applyDDLWith(t, engine, "CREATE TABLE t (c "+testCase.columnType+")")
			require.NoError(t, err)
			require.Len(t, mutation.Columns, 1)
			assert.Equal(t, testCase.wantModifiers, received)
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
			sql:  "ALTER TABLE users ADD COLUMN email text",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "email", mutation.Columns[0].Name)
				assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[0].SQLType.Category)
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
		{
			name: "ALTER COLUMN TYPE",
			sql:  "ALTER TABLE users ALTER COLUMN name TYPE varchar(255)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAlterColumn, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				assert.Equal(t, "name", mutation.ColumnName)
				assert.True(t, mutation.SetsType)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "varchar", mutation.Columns[0].SQLType.EngineName)
			},
		},
		{
			name: "ALTER COLUMN SET DATA TYPE with USING",
			sql:  "ALTER TABLE users ALTER name SET DATA TYPE text[] USING ARRAY[name], ADD COLUMN age int",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.True(t, mutation.SetsType)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "text", mutation.Columns[0].SQLType.EngineName)
				assert.Equal(t, 1, mutation.Columns[0].ArrayDimensions)
				require.Len(t, mutation.AdditionalMutations, 1)
				assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.AdditionalMutations[0].Kind)
			},
		},
		{
			name: "ALTER COLUMN SET NOT NULL then DROP NOT NULL",
			sql:  "ALTER TABLE users ALTER COLUMN name SET NOT NULL, ALTER COLUMN email DROP NOT NULL, ALTER COLUMN age SET DEFAULT 1",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.True(t, mutation.SetsNotNull)
				assert.Equal(t, "name", mutation.ColumnName)
				require.Len(t, mutation.AdditionalMutations, 2)
				assert.True(t, mutation.AdditionalMutations[0].DropsNotNull)
				assert.Equal(t, "email", mutation.AdditionalMutations[0].ColumnName)
				assert.False(t, mutation.AdditionalMutations[1].SetsNotNull || mutation.AdditionalMutations[1].DropsNotNull || mutation.AdditionalMutations[1].SetsType)
			},
		},
		{
			name: "an action after a DROP COLUMN ... CASCADE still applies",
			sql:  "ALTER TABLE users DROP COLUMN legacy CASCADE, ADD COLUMN age int",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableDropColumn, mutation.Kind)
				require.Len(t, mutation.AdditionalMutations, 1)
				assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.AdditionalMutations[0].Kind)
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
			name: "RENAME TO (table rename)",
			sql:  "ALTER TABLE users RENAME TO accounts",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableRenameTable, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				assert.Equal(t, "accounts", mutation.NewName)
			},
		},
		{
			name: "ADD CONSTRAINT",
			sql:  "ALTER TABLE users ADD CONSTRAINT uq_email UNIQUE (email)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddConstraint, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintUnique, mutation.Constraints[0].Kind)
				assert.Equal(t, "uq_email", mutation.Constraints[0].Name)
			},
		},
		{
			name: "DROP CONSTRAINT",
			sql:  "ALTER TABLE users DROP CONSTRAINT uq_email",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableDropConstraint, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				assert.Equal(t, "uq_email", mutation.ConstraintName)
			},
		},
		{
			name: "SET SCHEMA moves table to new schema",
			sql:  "ALTER TABLE public.users SET SCHEMA archive",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableSetSchema, mutation.Kind)
				assert.Equal(t, "public", mutation.SchemaName)
				assert.Equal(t, "users", mutation.TableName)
				assert.Equal(t, "archive", mutation.NewName)
			},
		},
		{
			name: "ADD COLUMN IF NOT EXISTS",
			sql:  "ALTER TABLE users ADD COLUMN IF NOT EXISTS nickname text",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "nickname", mutation.Columns[0].Name)
			},
		},
		{
			name: "DROP COLUMN IF EXISTS",
			sql:  "ALTER TABLE users DROP COLUMN IF EXISTS legacy_field",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableDropColumn, mutation.Kind)
				assert.Equal(t, "legacy_field", mutation.ColumnName)
			},
		},
		{
			name: "DROP CONSTRAINT IF EXISTS CASCADE",
			sql:  "ALTER TABLE users DROP CONSTRAINT IF EXISTS uq_email CASCADE",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableDropConstraint, mutation.Kind)
				assert.Equal(t, "uq_email", mutation.ConstraintName)
			},
		},
		{
			name: "schema-qualified ALTER TABLE",
			sql:  "ALTER TABLE myschema.users ADD COLUMN bio text",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
				assert.Equal(t, "myschema", mutation.SchemaName)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "ALTER TABLE ONLY accepted",
			sql:  "ALTER TABLE ONLY users ADD COLUMN status text",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "ADD CONSTRAINT with FOREIGN KEY",
			sql:  "ALTER TABLE posts ADD CONSTRAINT fk_author FOREIGN KEY (user_id) REFERENCES users (id)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddConstraint, mutation.Kind)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintForeignKey, mutation.Constraints[0].Kind)
				assert.Equal(t, "fk_author", mutation.Constraints[0].Name)
				assert.Equal(t, "users", mutation.Constraints[0].ForeignTable)
			},
		},
		{
			name: "ADD CONSTRAINT with CHECK",
			sql:  "ALTER TABLE products ADD CONSTRAINT chk_positive CHECK (price > 0)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddConstraint, mutation.Kind)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, sqlmodel.ConstraintCheck, mutation.Constraints[0].Kind)
				assert.Equal(t, "chk_positive", mutation.Constraints[0].Name)
			},
		},
		{
			name: "multi-action ADD COLUMN, ADD COLUMN merges columns",
			sql:  "ALTER TABLE users ADD COLUMN nickname text, ADD COLUMN bio text",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
				require.Len(t, mutation.Columns, 2)
				assert.Equal(t, "nickname", mutation.Columns[0].Name)
				assert.Equal(t, "bio", mutation.Columns[1].Name)
				assert.Empty(t, mutation.AdditionalMutations)
			},
		},
		{
			name: "multi-action ADD COLUMN then ADD CONSTRAINT preserves both",
			sql:  "ALTER TABLE users ADD COLUMN code text, ADD CONSTRAINT uq_code UNIQUE (code)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "code", mutation.Columns[0].Name)
				require.Len(t, mutation.AdditionalMutations, 1)
				followUp := mutation.AdditionalMutations[0]
				assert.Equal(t, ast.MutationAlterTableAddConstraint, followUp.Kind)
				require.Len(t, followUp.Constraints, 1)
				assert.Equal(t, "uq_code", followUp.Constraints[0].Name)
			},
		},
		{
			name: "multi-action ADD CONSTRAINT then ADD COLUMN preserves both",
			sql:  "ALTER TABLE users ADD CONSTRAINT chk_age CHECK (age >= 0), ADD COLUMN bio text",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddConstraint, mutation.Kind)
				require.Len(t, mutation.Constraints, 1)
				assert.Equal(t, "chk_age", mutation.Constraints[0].Name)
				require.Len(t, mutation.AdditionalMutations, 1)
				followUp := mutation.AdditionalMutations[0]
				assert.Equal(t, ast.MutationAlterTableAddColumn, followUp.Kind)
				require.Len(t, followUp.Columns, 1)
				assert.Equal(t, "bio", followUp.Columns[0].Name)
			},
		},
		{
			name: "multi-action merges consecutive AddColumns after constraint",
			sql:  "ALTER TABLE users ADD CONSTRAINT uq_email UNIQUE (email), ADD COLUMN bio text, ADD COLUMN locale text",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddConstraint, mutation.Kind)
				require.Len(t, mutation.AdditionalMutations, 1)
				addColumns := mutation.AdditionalMutations[0]
				assert.Equal(t, ast.MutationAlterTableAddColumn, addColumns.Kind)
				require.Len(t, addColumns.Columns, 2)
				assert.Equal(t, "bio", addColumns.Columns[0].Name)
				assert.Equal(t, "locale", addColumns.Columns[1].Name)
			},
		},
		{
			name: "multi-action merges consecutive AddColumns after constraint and non-AddColumn",
			sql:  "ALTER TABLE users ADD COLUMN a text, ADD CONSTRAINT uq UNIQUE (a), ADD COLUMN b text, ADD COLUMN c text",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "a", mutation.Columns[0].Name)
				require.Len(t, mutation.AdditionalMutations, 2)
			},
		},
		{
			name: "multi-action merges AddColumns across interleaved AddConstraint",
			sql:  "ALTER TABLE users ADD COLUMN a text, ADD CONSTRAINT uq UNIQUE (a), ADD COLUMN b text, ADD COLUMN c text",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
				require.Len(t, mutation.Columns, 1)
				assert.Equal(t, "a", mutation.Columns[0].Name)
				require.Len(t, mutation.AdditionalMutations, 2)
				addConstraint := mutation.AdditionalMutations[0]
				assert.Equal(t, ast.MutationAlterTableAddConstraint, addConstraint.Kind)
				addBC := mutation.AdditionalMutations[1]
				assert.Equal(t, ast.MutationAlterTableAddColumn, addBC.Kind)
				require.Len(t, addBC.Columns, 2)
				assert.Equal(t, "b", addBC.Columns[0].Name)
				assert.Equal(t, "c", addBC.Columns[1].Name)
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
			name: "simple drop",
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
		{
			name: "DROP TABLE CASCADE",
			sql:  "DROP TABLE users CASCADE",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropTable, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "DROP TABLE schema-qualified",
			sql:  "DROP TABLE myschema.users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropTable, mutation.Kind)
				assert.Equal(t, "myschema", mutation.SchemaName)
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
			sql:  "CREATE VIEW active_users AS SELECT id, name FROM users WHERE active = true",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateView, mutation.Kind)
				assert.Equal(t, "active_users", mutation.TableName)
				assert.NotNil(t, mutation.ViewDefinition, "view should have a parsed definition")
			},
		},
		{
			name: "CREATE OR REPLACE VIEW",
			sql:  "CREATE OR REPLACE VIEW active_users AS SELECT id FROM users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateView, mutation.Kind)
				assert.Equal(t, "active_users", mutation.TableName)
				assert.NotNil(t, mutation.ViewDefinition)
			},
		},
		{
			name: "CREATE MATERIALIZED VIEW",
			sql:  "CREATE MATERIALIZED VIEW user_stats AS SELECT count(*) FROM users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateView, mutation.Kind)
				assert.Equal(t, "user_stats", mutation.TableName)
				assert.NotNil(t, mutation.ViewDefinition)
			},
		},
		{
			name: "view with schema qualification",
			sql:  "CREATE VIEW reporting.monthly_totals AS SELECT 1",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateView, mutation.Kind)
				assert.Equal(t, "reporting", mutation.SchemaName)
				assert.Equal(t, "monthly_totals", mutation.TableName)
			},
		},
		{
			name: "view with explicit column list",
			sql:  "CREATE VIEW user_count (cnt) AS SELECT count(*) FROM users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateView, mutation.Kind)
				assert.Equal(t, "user_count", mutation.TableName)
				require.NotNil(t, mutation.ViewDefinition)
			},
		},
		{
			name: "CREATE MATERIALIZED VIEW IF NOT EXISTS",
			sql:  "CREATE MATERIALIZED VIEW IF NOT EXISTS user_stats AS SELECT count(*) FROM users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateView, mutation.Kind)
				assert.Equal(t, "user_stats", mutation.TableName)
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
			name: "simple drop view",
			sql:  "DROP VIEW active_users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropView, mutation.Kind)
				assert.Equal(t, "active_users", mutation.TableName)
			},
		},
		{
			name: "DROP MATERIALIZED VIEW",
			sql:  "DROP MATERIALIZED VIEW user_stats",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropView, mutation.Kind)
				assert.Equal(t, "user_stats", mutation.TableName)
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
				assert.Equal(t, "idx_users_name", mutation.NewName)
			},
		},
		{
			name: "unique index",
			sql:  "CREATE UNIQUE INDEX idx_users_email ON users (email)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateIndex, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				assert.Equal(t, "idx_users_email", mutation.NewName)
			},
		},
		{
			name: "index with USING method",
			sql:  "CREATE INDEX idx_users_name ON users USING btree (name)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateIndex, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "index with CONCURRENTLY",
			sql:  "CREATE INDEX CONCURRENTLY idx_users_name ON users (name)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateIndex, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "index IF NOT EXISTS",
			sql:  "CREATE INDEX IF NOT EXISTS idx_users_name ON users (name)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateIndex, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "index on schema-qualified table",
			sql:  "CREATE INDEX idx_name ON myschema.users (name)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateIndex, mutation.Kind)
				assert.Equal(t, "myschema", mutation.SchemaName)
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
			name: "simple drop index",
			sql:  "DROP INDEX idx_users_name",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropIndex, mutation.Kind)
				assert.Equal(t, "idx_users_name", mutation.NewName)
			},
		},
		{
			name: "DROP INDEX CONCURRENTLY IF EXISTS",
			sql:  "DROP INDEX CONCURRENTLY IF EXISTS idx_users_name",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropIndex, mutation.Kind)
				assert.Equal(t, "idx_users_name", mutation.NewName)
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

func TestApplyDDL_CreateType(t *testing.T) {
	t.Parallel()

	t.Run("enum type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, "CREATE TYPE mood AS ENUM ('happy', 'sad', 'neutral')")

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateEnum, mutation.Kind)
		assert.Equal(t, "mood", mutation.EnumName)
		assert.Equal(t, []string{"happy", "sad", "neutral"}, mutation.EnumValues)
	})

	t.Run("composite type", func(t *testing.T) {
		t.Parallel()

		mutation := applyDDL(t, "CREATE TYPE address AS (street text, city text, postcode text)")

		require.NotNil(t, mutation)
		assert.Equal(t, ast.MutationCreateCompositeType, mutation.Kind)
		assert.Equal(t, "address", mutation.EnumName)
		require.Len(t, mutation.Columns, 3)
		assert.Equal(t, "street", mutation.Columns[0].Name)
		assert.Equal(t, "city", mutation.Columns[1].Name)
		assert.Equal(t, "postcode", mutation.Columns[2].Name)
	})
}

func TestApplyDDL_AlterType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "ADD VALUE",
			sql:  "ALTER TYPE mood ADD VALUE 'anxious'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterEnumAddValue, mutation.Kind)
				assert.Equal(t, "mood", mutation.EnumName)
				assert.Equal(t, []string{"anxious"}, mutation.EnumValues)
			},
		},
		{
			name: "ADD VALUE IF NOT EXISTS",
			sql:  "ALTER TYPE mood ADD VALUE IF NOT EXISTS 'anxious'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterEnumAddValue, mutation.Kind)
				assert.Equal(t, "mood", mutation.EnumName)
				assert.Equal(t, []string{"anxious"}, mutation.EnumValues)
			},
		},
		{
			name: "ADD VALUE BEFORE",
			sql:  "ALTER TYPE mood ADD VALUE 'anxious' BEFORE 'sad'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterEnumAddValue, mutation.Kind)
				assert.Equal(t, "mood", mutation.EnumName)
				assert.Equal(t, []string{"anxious"}, mutation.EnumValues)
			},
		},
		{
			name: "ADD VALUE AFTER",
			sql:  "ALTER TYPE mood ADD VALUE 'anxious' AFTER 'happy'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterEnumAddValue, mutation.Kind)
				assert.Equal(t, "mood", mutation.EnumName)
				assert.Equal(t, []string{"anxious"}, mutation.EnumValues)
			},
		},
		{
			name: "RENAME VALUE",
			sql:  "ALTER TYPE mood RENAME VALUE 'sad' TO 'melancholy'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterEnumRenameValue, mutation.Kind)
				assert.Equal(t, "mood", mutation.EnumName)
				assert.Equal(t, []string{"sad", "melancholy"}, mutation.EnumValues)
			},
		},
		{
			name: "schema-qualified ALTER TYPE",
			sql:  "ALTER TYPE myschema.mood ADD VALUE 'excited'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationAlterEnumAddValue, mutation.Kind)
				assert.Equal(t, "myschema", mutation.SchemaName)
				assert.Equal(t, "mood", mutation.EnumName)
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

func TestApplyDDL_CreateFunction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple function with RETURNS and LANGUAGE",
			sql:  `CREATE FUNCTION add_numbers(a integer, b integer) RETURNS integer LANGUAGE sql AS 'SELECT a + b'`,
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "add_numbers", mutation.Function.Name)
				require.Len(t, mutation.Function.Arguments, 2)
				assert.Equal(t, "a", mutation.Function.Arguments[0].Name)
				assert.Equal(t, "b", mutation.Function.Arguments[1].Name)
				assert.Equal(t, sqlmodel.TypeCategoryInteger, mutation.Function.ReturnType.Category)
				assert.Equal(t, "sql", mutation.Function.Language)
			},
		},
		{
			name: "function with array argument keeps the array type",
			sql:  `CREATE FUNCTION filter_tags(names text[]) RETURNS boolean LANGUAGE sql AS 'SELECT true'`,
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.NotNil(t, mutation.Function)
				require.Len(t, mutation.Function.Arguments, 1)
				argument := mutation.Function.Arguments[0]
				assert.Equal(t, "names", argument.Name)
				assert.Equal(t, sqlmodel.TypeCategoryArray, argument.Type.Category,
					"text[] argument must be an array type, not scalar text")
				assert.Equal(t, "text[]", argument.Type.EngineName)
				require.NotNil(t, argument.Type.ElementType)
				assert.Equal(t, sqlmodel.TypeCategoryText, argument.Type.ElementType.Category)
			},
		},
		{
			name: "function RETURNS TABLE",
			sql:  "CREATE FUNCTION get_users() RETURNS TABLE (id integer, name text) LANGUAGE sql AS 'SELECT id, name FROM users'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "get_users", mutation.Function.Name)
				assert.True(t, mutation.Function.ReturnsSet, "RETURNS TABLE should set ReturnsSet")
				assert.Equal(t, "sql", mutation.Function.Language)
			},
		},
		{
			name: "function RETURNS SETOF",
			sql:  "CREATE FUNCTION all_users() RETURNS SETOF users LANGUAGE sql AS 'SELECT * FROM users'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.True(t, mutation.Function.ReturnsSet, "RETURNS SETOF should set ReturnsSet")
			},
		},
		{
			name: "function with STRICT",
			sql:  "CREATE FUNCTION safe_div(a integer, b integer) RETURNS integer LANGUAGE sql STRICT AS 'SELECT a / b'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.True(t, mutation.Function.IsStrict, "STRICT should set IsStrict")
			},
		},
		{
			name: "function with RETURNS NULL ON NULL INPUT",
			sql:  "CREATE FUNCTION safe_div(a integer, b integer) RETURNS integer LANGUAGE sql RETURNS NULL ON NULL INPUT AS 'SELECT a / b'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.True(t, mutation.Function.IsStrict, "RETURNS NULL ON NULL INPUT should set IsStrict")
			},
		},
		{
			name: "function with CALLED ON NULL INPUT",
			sql:  "CREATE FUNCTION maybe_null(a integer) RETURNS integer LANGUAGE sql CALLED ON NULL INPUT AS 'SELECT a'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.False(t, mutation.Function.IsStrict, "CALLED ON NULL INPUT should not set IsStrict")
			},
		},
		{
			name: "function with IMMUTABLE",
			sql:  "CREATE FUNCTION constant_value() RETURNS integer LANGUAGE sql IMMUTABLE AS 'SELECT 42'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, sqlmodel.DataAccessReadOnly, mutation.Function.DataAccess)
			},
		},
		{
			name: "function with STABLE",
			sql:  "CREATE FUNCTION current_count() RETURNS integer LANGUAGE sql STABLE AS 'SELECT count(*) FROM users'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, sqlmodel.DataAccessReadOnly, mutation.Function.DataAccess)
			},
		},
		{
			name: "function with VOLATILE leaves the data access to the body",
			sql:  "CREATE FUNCTION do_something() RETURNS void LANGUAGE sql VOLATILE AS 'SELECT 1'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, sqlmodel.DataAccessUnknown, mutation.Function.DataAccess)
			},
		},
		{
			name: "CREATE OR REPLACE FUNCTION",
			sql:  "CREATE OR REPLACE FUNCTION greet(name text) RETURNS text LANGUAGE sql AS 'SELECT name'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "greet", mutation.Function.Name)
				require.Len(t, mutation.Function.Arguments, 1)
				assert.Equal(t, "name", mutation.Function.Arguments[0].Name)
			},
		},
		{
			name: "function with LANGUAGE plpgsql and dollar-quoted body",
			sql:  `CREATE FUNCTION increment(val integer) RETURNS integer LANGUAGE plpgsql AS $$ BEGIN RETURN val + 1; END; $$`,
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "plpgsql", mutation.Function.Language)
				assert.NotEmpty(t, mutation.Function.BodySQL)
			},
		},
		{
			name: "function with no arguments",
			sql:  "CREATE FUNCTION now_utc() RETURNS timestamp LANGUAGE sql AS 'SELECT now()'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "now_utc", mutation.Function.Name)
				assert.Empty(t, mutation.Function.Arguments)
			},
		},
		{
			name: "function with unnamed arguments",
			sql:  "CREATE FUNCTION add_ints(integer, integer) RETURNS integer LANGUAGE sql AS 'SELECT $1 + $2'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				require.Len(t, mutation.Function.Arguments, 2)

				assert.Empty(t, mutation.Function.Arguments[0].Name)
				assert.Empty(t, mutation.Function.Arguments[1].Name)
			},
		},
		{
			name: "function with multiple clauses combined",
			sql:  "CREATE FUNCTION safe_add(a integer, b integer) RETURNS integer LANGUAGE sql IMMUTABLE STRICT AS 'SELECT a + b'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.True(t, mutation.Function.IsStrict)
				assert.Equal(t, sqlmodel.DataAccessReadOnly, mutation.Function.DataAccess)
				assert.Equal(t, "sql", mutation.Function.Language)
			},
		},
		{
			name: "schema-qualified function",
			sql:  "CREATE FUNCTION myschema.my_func() RETURNS void LANGUAGE sql AS 'SELECT 1'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "myschema", mutation.SchemaName)
				assert.Equal(t, "my_func", mutation.Function.Name)
			},
		},
		{
			name: "CREATE PROCEDURE",
			sql:  "CREATE PROCEDURE do_cleanup() LANGUAGE sql AS 'DELETE FROM old_data'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "do_cleanup", mutation.Function.Name)
			},
		},
		{
			name: "function with VARIADIC argument",
			sql:  "CREATE FUNCTION concat_all(VARIADIC items text[]) RETURNS text LANGUAGE sql AS 'SELECT array_to_string(items, '''')'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.True(t, mutation.Function.IsVariadic)
			},
		},
		{
			name: "function with DEFAULT argument",
			sql:  "CREATE FUNCTION greet(name text DEFAULT 'world') RETURNS text LANGUAGE sql AS 'SELECT name'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				require.Len(t, mutation.Function.Arguments, 1)
				assert.True(t, mutation.Function.Arguments[0].IsOptional)
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

func TestApplyDDL_DropFunction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "drop function with argument types",
			sql:  "DROP FUNCTION add_numbers(integer, integer)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "add_numbers", mutation.Function.Name)
			},
		},
		{
			name: "drop function without argument types",
			sql:  "DROP FUNCTION simple_func",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "simple_func", mutation.Function.Name)
			},
		},
		{
			name: "DROP FUNCTION IF EXISTS",
			sql:  "DROP FUNCTION IF EXISTS maybe_exists(text)",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "maybe_exists", mutation.Function.Name)
			},
		},
		{
			name: "DROP FUNCTION CASCADE",
			sql:  "DROP FUNCTION my_func(integer) CASCADE",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "my_func", mutation.Function.Name)
			},
		},
		{
			name: "DROP PROCEDURE",
			sql:  "DROP PROCEDURE cleanup_proc()",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropFunction, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "cleanup_proc", mutation.Function.Name)
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

func TestApplyDDL_CreateSchema(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE SCHEMA analytics")

	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateSchema, mutation.Kind)
	assert.Equal(t, "analytics", mutation.SchemaName)
}

func TestApplyDDL_CreateExtension(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE EXTENSION IF NOT EXISTS pgcrypto")

	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateExtension, mutation.Kind)
	assert.Equal(t, "pgcrypto", mutation.TableName)
}

func TestApplyDDL_CreateSequence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple sequence",
			sql:  "CREATE SEQUENCE user_id_seq",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateSequence, mutation.Kind)
				assert.Equal(t, "user_id_seq", mutation.SequenceName)
			},
		},
		{
			name: "sequence with options",
			sql:  "CREATE SEQUENCE order_seq START 1000 INCREMENT 10 MINVALUE 1 MAXVALUE 99999",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateSequence, mutation.Kind)
				assert.Equal(t, "order_seq", mutation.SequenceName)
			},
		},
		{
			name: "sequence with OWNED BY",
			sql:  "CREATE SEQUENCE user_id_seq OWNED BY users.id",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateSequence, mutation.Kind)
				assert.Equal(t, "user_id_seq", mutation.SequenceName)
				assert.Equal(t, "users", mutation.OwnedByTable)
				assert.Equal(t, "id", mutation.OwnedByColumn)
			},
		},
		{
			name: "sequence IF NOT EXISTS",
			sql:  "CREATE SEQUENCE IF NOT EXISTS counter_seq",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateSequence, mutation.Kind)
				assert.Equal(t, "counter_seq", mutation.SequenceName)
			},
		},
		{
			name: "sequence with schema qualification",
			sql:  "CREATE SEQUENCE myschema.my_seq",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateSequence, mutation.Kind)
				assert.Equal(t, "myschema", mutation.SchemaName)
				assert.Equal(t, "my_seq", mutation.SequenceName)
			},
		},
		{
			name: "sequence OWNED BY NONE",
			sql:  "CREATE SEQUENCE detached_seq OWNED BY NONE",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateSequence, mutation.Kind)
				assert.Equal(t, "detached_seq", mutation.SequenceName)
				assert.Empty(t, mutation.OwnedByTable, "OWNED BY NONE should leave OwnedByTable empty")
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
			name: "simple BEFORE INSERT trigger",
			sql:  "CREATE TRIGGER audit_insert BEFORE INSERT ON users FOR EACH ROW EXECUTE FUNCTION audit_func()",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTrigger, mutation.Kind)
				assert.Equal(t, "audit_insert", mutation.TriggerName)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "AFTER UPDATE trigger",
			sql:  "CREATE TRIGGER after_update AFTER UPDATE ON posts FOR EACH ROW EXECUTE FUNCTION notify()",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTrigger, mutation.Kind)
				assert.Equal(t, "after_update", mutation.TriggerName)
				assert.Equal(t, "posts", mutation.TableName)
			},
		},
		{
			name: "INSTEAD OF trigger",
			sql:  "CREATE TRIGGER view_insert INSTEAD OF INSERT ON active_users FOR EACH ROW EXECUTE FUNCTION insert_user()",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateTrigger, mutation.Kind)
				assert.Equal(t, "view_insert", mutation.TriggerName)
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

func TestATriggerCreatedOrReplacedReplaysAsATrigger(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantTrigger string
		wantTable   string
	}{
		{
			name:        "a trigger created or replaced",
			sql:         "CREATE OR REPLACE TRIGGER touch_items BEFORE UPDATE ON items FOR EACH ROW EXECUTE FUNCTION touch()",
			wantTrigger: "touch_items",
			wantTable:   "items",
		},
		{
			name:        "a constraint trigger",
			sql:         "CREATE CONSTRAINT TRIGGER check_items AFTER UPDATE ON items DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION touch()",
			wantTrigger: "check_items",
			wantTable:   "items",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)
			require.NotNil(t, mutation)
			require.Equal(t, ast.MutationCreateTrigger, mutation.Kind)
			require.Equal(t, testCase.wantTrigger, mutation.TriggerName)
			require.Equal(t, testCase.wantTable, mutation.TableName)
		})
	}
}

func TestASequenceCarriesTheColumnsAQueryReadingItSees(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE SEQUENCE IF NOT EXISTS app.public.counter START 10 OWNED BY items.id")

	require.NotNil(t, mutation)
	require.Equal(t, ast.MutationCreateSequence, mutation.Kind)
	require.Equal(t, "counter", mutation.SequenceName)
	described := make([]string, 0, len(mutation.Columns))
	for index := range mutation.Columns {
		column := &mutation.Columns[index]
		described = append(described, column.Name+" "+column.SQLType.EngineName+" "+strconv.FormatBool(column.Nullable))
	}
	require.Equal(t, []string{"last_value int8 false", "log_cnt int8 false", "is_called bool false"}, described)
}

func TestAKeyColumnTakesASortOrderOnlyWhereTheDialectReadsIndexElements(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"CREATE TABLE authors (one bigint, two bigint, PRIMARY KEY (one, two DESC))",
		"CREATE TABLE authors (one bigint, two bigint, UNIQUE (one ASC, two))",
		"ALTER TABLE authors ADD PRIMARY KEY (one DESC)",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			require.Error(t, applyDDLExpectError(t, sql))

			engine := New(WithInlineIndexElements())
			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)
			_, err = engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)
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
			name: "simple drop trigger",
			sql:  "DROP TRIGGER audit_insert ON users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropTrigger, mutation.Kind)
				assert.Equal(t, "audit_insert", mutation.TriggerName)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "DROP TRIGGER IF EXISTS",
			sql:  "DROP TRIGGER IF EXISTS audit_insert ON users",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropTrigger, mutation.Kind)
				assert.Equal(t, "audit_insert", mutation.TriggerName)
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

func TestApplyDDL_DropType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple drop type",
			sql:  "DROP TYPE mood",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropType, mutation.Kind)
				assert.Equal(t, "mood", mutation.EnumName)
			},
		},
		{
			name: "DROP TYPE IF EXISTS CASCADE",
			sql:  "DROP TYPE IF EXISTS mood CASCADE",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropType, mutation.Kind)
				assert.Equal(t, "mood", mutation.EnumName)
			},
		},
		{
			name: "DROP TYPE schema-qualified",
			sql:  "DROP TYPE myschema.address",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropType, mutation.Kind)
				assert.Equal(t, "myschema", mutation.SchemaName)
				assert.Equal(t, "address", mutation.EnumName)
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

func TestApplyDDL_DropSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple drop schema",
			sql:  "DROP SCHEMA analytics",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropSchema, mutation.Kind)
				assert.Equal(t, "analytics", mutation.SchemaName)
			},
		},
		{
			name: "DROP SCHEMA IF EXISTS CASCADE",
			sql:  "DROP SCHEMA IF EXISTS old_data CASCADE",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropSchema, mutation.Kind)
				assert.Equal(t, "old_data", mutation.SchemaName)
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

func TestApplyDDL_DropSequence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "simple drop sequence",
			sql:  "DROP SEQUENCE user_id_seq",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropSequence, mutation.Kind)
				assert.Equal(t, "user_id_seq", mutation.SequenceName)
			},
		},
		{
			name: "DROP SEQUENCE IF EXISTS",
			sql:  "DROP SEQUENCE IF EXISTS user_id_seq",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropSequence, mutation.Kind)
				assert.Equal(t, "user_id_seq", mutation.SequenceName)
			},
		},
		{
			name: "DROP SEQUENCE CASCADE",
			sql:  "DROP SEQUENCE user_id_seq CASCADE",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationDropSequence, mutation.Kind)
				assert.Equal(t, "user_id_seq", mutation.SequenceName)
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

func TestApplyDDL_CreateExtension_Options(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "extension with SCHEMA",
			sql:  "CREATE EXTENSION hstore WITH SCHEMA public",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateExtension, mutation.Kind)
				assert.Equal(t, "hstore", mutation.TableName)
				assert.Equal(t, "public", mutation.SchemaName)
			},
		},
		{
			name: "extension without IF NOT EXISTS",
			sql:  "CREATE EXTENSION postgis",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateExtension, mutation.Kind)
				assert.Equal(t, "postgis", mutation.TableName)
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

func TestApplyDDL_DropExtension(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "DROP EXTENSION IF EXISTS pgcrypto")
	require.NoError(t, err)
	require.NotEmpty(t, statements)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)

	assert.Nil(t, mutation)
}

func TestApplyDDL_CreateSchema_Options(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "CREATE SCHEMA IF NOT EXISTS",
			sql:  "CREATE SCHEMA IF NOT EXISTS staging",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateSchema, mutation.Kind)
				assert.Equal(t, "staging", mutation.SchemaName)
			},
		},
		{
			name: "CREATE SCHEMA AUTHORIZATION",
			sql:  "CREATE SCHEMA AUTHORIZATION admin_role",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationCreateSchema, mutation.Kind)
				assert.Equal(t, "admin_role", mutation.SchemaName)
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

func TestApplyDDL_Comment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		assertions func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "COMMENT ON TABLE",
			sql:  "COMMENT ON TABLE users IS 'The main users table'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationComment, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "COMMENT ON COLUMN",
			sql:  "COMMENT ON COLUMN users.email IS 'Primary email address'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationComment, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
				assert.Equal(t, "email", mutation.ColumnName)
			},
		},
		{
			name: "COMMENT ON TYPE",
			sql:  "COMMENT ON TYPE mood IS 'User mood enumeration'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationComment, mutation.Kind)
				assert.Equal(t, "mood", mutation.EnumName)
			},
		},
		{
			name: "COMMENT ON FUNCTION",
			sql:  "COMMENT ON FUNCTION add_numbers(integer, integer) IS 'Adds two numbers'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationComment, mutation.Kind)
				require.NotNil(t, mutation.Function)
				assert.Equal(t, "add_numbers", mutation.Function.Name)
			},
		},
		{
			name: "COMMENT ON SCHEMA",
			sql:  "COMMENT ON SCHEMA public IS 'Default schema'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationComment, mutation.Kind)
				assert.Equal(t, "public", mutation.SchemaName)
			},
		},
		{
			name: "COMMENT IS NULL removes comment",
			sql:  "COMMENT ON TABLE users IS NULL",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationComment, mutation.Kind)
				assert.Equal(t, "users", mutation.TableName)
			},
		},
		{
			name: "COMMENT ON COLUMN with schema-qualified name",
			sql:  "COMMENT ON COLUMN public.users.email IS 'User email address'",
			assertions: func(t *testing.T, mutation *ast.CatalogueMutation) {
				assert.Equal(t, ast.MutationComment, mutation.Kind)
				assert.Equal(t, "public", mutation.SchemaName)
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

func TestApplyDDL_NonDDL(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "SELECT 1")

	assert.Nil(t, mutation, "non-DDL statement should return nil mutation")
}

func TestApplyDDL_CreateTableMultiWordColumnTypes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		columnType string
		wantEngine string
		wantLength int
		hasLength  bool
	}{
		{name: "bit varying with a length", columnType: "bit varying(5)", wantEngine: "varbit", wantLength: 5, hasLength: true},
		{name: "bit varying without a length", columnType: "BIT VARYING", wantEngine: "varbit"},
		{name: "bit with a length", columnType: "bit(3)", wantEngine: "bit", wantLength: 3, hasLength: true},
		{name: "plain bit", columnType: "bit", wantEngine: "bit"},
		{name: "character varying", columnType: "character varying(10)", wantEngine: "varchar", wantLength: 10, hasLength: true},
		{name: "double precision", columnType: "double precision", wantEngine: "float8"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, "CREATE TABLE t (value "+testCase.columnType+" NOT NULL, other int)")

			require.Len(t, mutation.Columns, 2)
			column := mutation.Columns[0]
			assert.Equal(t, testCase.wantEngine, column.SQLType.EngineName)
			assert.False(t, column.Nullable)
			if testCase.hasLength {
				require.NotNil(t, column.SQLType.Length)
				assert.Equal(t, testCase.wantLength, *column.SQLType.Length)
			}
			assert.Equal(t, "other", mutation.Columns[1].Name)
		})
	}
}

func TestApplyDDL_CreateTableSecondaryColumnConstraints(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "collation", sql: `CREATE TABLE t (name text COLLATE "C" NOT NULL, other int)`},
		{name: "named constraint", sql: "CREATE TABLE t (name text CONSTRAINT name_present NOT NULL, other int)"},
		{name: "foreign key reference", sql: "CREATE TABLE t (name text NOT NULL REFERENCES s.u (id) ON DELETE CASCADE, other int)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.Len(t, mutation.Columns, 2)
			assert.Equal(t, "name", mutation.Columns[0].Name)
			assert.False(t, mutation.Columns[0].Nullable)
			assert.Equal(t, "other", mutation.Columns[1].Name)
		})
	}
}

func TestApplyDDLCreateTableRequiresASeparatorAfterEachElement(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"CREATE TABLE t (a int b int)",
		"CREATE TABLE t (a int NOT NULL extra, b int)",
		"CREATE TABLE t (a int, PRIMARY KEY (a) junk)",
		"CREATE TABLE t (a int",
		"CREATE TABLE t (a timestamp(3) with zone)",
		"CREATE TABLE t (tags text[)",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			err := applyDDLExpectError(t, sql)
			require.ErrorIs(t, err, fault.ErrParse)
		})
	}
}

func TestApplyDDLCreateTableReadsTypeSpellings(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		column         string
		wantEngine     string
		wantNullable   bool
		wantDimensions int
	}{
		{name: "timestamp with time zone and precision", column: "ts timestamp(3) with time zone NOT NULL", wantEngine: "timestamptz"},
		{name: "timestamp without time zone and precision", column: "ts timestamp(3) without time zone", wantEngine: "timestamp", wantNullable: true},
		{name: "time with time zone and precision", column: "t time(3) with time zone NOT NULL", wantEngine: "timetz"},
		{name: "time without time zone and precision", column: "t time(6) without time zone NOT NULL", wantEngine: "time"},
		{name: "array keyword", column: "va int ARRAY NOT NULL", wantEngine: "int4", wantDimensions: 1},
		{name: "array keyword with a bound", column: "vb int ARRAY[4]", wantEngine: "int4", wantNullable: true, wantDimensions: 1},
		{name: "bracketed dimensions", column: "vc int[3][3] NOT NULL", wantEngine: "int4", wantDimensions: 2},
		{name: "interval fields", column: "iv interval day to second(3) NOT NULL", wantEngine: "interval"},
		{name: "interval single field", column: "iv interval year NOT NULL", wantEngine: "interval"},
		{name: "numeric precision", column: "d numeric(10, 2) NOT NULL", wantEngine: "numeric"},
		{name: "collation before not null", column: `c character varying(20) COLLATE "C" NOT NULL`, wantEngine: "varchar"},
		{name: "qualified collation", column: `c text COLLATE pg_catalog."default" NOT NULL`, wantEngine: "text"},
		{name: "references actions before not null", column: "r int REFERENCES p (id) ON DELETE CASCADE ON UPDATE SET NULL NOT NULL", wantEngine: "int4"},
		{name: "not deferrable is not not null", column: "u int UNIQUE NOT DEFERRABLE", wantEngine: "int4", wantNullable: true},
		{name: "deferrable before not null", column: "u int UNIQUE DEFERRABLE INITIALLY DEFERRED NOT NULL", wantEngine: "int4"},
		{name: "unique nulls not distinct", column: "u int UNIQUE NULLS NOT DISTINCT NOT NULL", wantEngine: "int4"},
		{name: "compression and storage", column: "s text COMPRESSION pglz STORAGE EXTERNAL NOT NULL", wantEngine: "text"},
		{name: "check with no inherit", column: "k int CHECK (k > 0) NO INHERIT NOT NULL", wantEngine: "int4"},
		{name: "named constraint", column: "k int CONSTRAINT k_positive CHECK (k > 0) NOT NULL", wantEngine: "int4"},
		{name: "quoted reserved column", column: `"check" int NOT NULL`, wantEngine: "int4"},
		{name: "default then not null", column: "n int DEFAULT 0 NOT NULL", wantEngine: "int4"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, "CREATE TABLE t ("+testCase.column+", last int)")

			require.Len(t, mutation.Columns, 2, "columns %v", columnNames(mutation.Columns))
			column := mutation.Columns[0]
			assert.Equal(t, testCase.wantEngine, column.SQLType.EngineName)
			assert.Equal(t, testCase.wantNullable, column.Nullable)
			assert.Equal(t, testCase.wantDimensions, column.ArrayDimensions)
			assert.Equal(t, testCase.wantDimensions > 0, column.IsArray)
			assert.Equal(t, "last", mutation.Columns[1].Name)
		})
	}
}

func TestApplyDDLCreateTableKeepsQuotedReservedWordColumns(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, `CREATE TABLE t (id bigint PRIMARY KEY, "check" int NOT NULL, "primary" text NOT NULL, `+
		`"unique" int NOT NULL, "constraint" text NOT NULL, "foreign" int NOT NULL, "key" text NOT NULL, "exclude" int)`)

	assert.Equal(t, []string{"id", "check", "primary", "unique", "constraint", "foreign", "key", "exclude"}, columnNames(mutation.Columns))
	assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
	for _, column := range mutation.Columns[1:7] {
		assert.False(t, column.Nullable, column.Name)
	}
}

func TestApplyDDLCreateTableDefaultsFollowTheColumnKind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		column       string
		wantDefault  bool
		wantNullable bool
	}{
		{name: "inline primary key has no default", column: "id int PRIMARY KEY"},
		{name: "serial has a default and is not null", column: "id serial", wantDefault: true},
		{name: "bigserial primary key", column: "id bigserial PRIMARY KEY", wantDefault: true},
		{name: "smallserial", column: "id smallserial", wantDefault: true},
		{name: "identity always", column: "id int GENERATED ALWAYS AS IDENTITY", wantDefault: true},
		{name: "identity by default with options", column: "id bigint GENERATED BY DEFAULT AS IDENTITY (START WITH 10)", wantDefault: true},
		{name: "default value", column: "id int DEFAULT 1", wantDefault: true, wantNullable: true},
		{name: "plain column", column: "id int", wantNullable: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, "CREATE TABLE t ("+testCase.column+")")

			require.Len(t, mutation.Columns, 1)
			assert.Equal(t, testCase.wantDefault, mutation.Columns[0].HasDefault)
			assert.Equal(t, testCase.wantNullable, mutation.Columns[0].Nullable)
		})
	}
}

func TestApplyDDLCreateTableReadsTableConstraintAttributes(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE TABLE t (a int, b int, "+
		"CONSTRAINT pk PRIMARY KEY (a) INCLUDE (b) WITH (fillfactor = 70) USING INDEX TABLESPACE fast DEFERRABLE INITIALLY DEFERRED, "+
		"UNIQUE NULLS NOT DISTINCT (b), "+
		"CHECK (a > 0) NO INHERIT, "+
		"EXCLUDE USING gist (a WITH =) WHERE (a > 0), "+
		"FOREIGN KEY (b) REFERENCES o (id) MATCH FULL ON DELETE SET NULL (b) ON UPDATE NO ACTION NOT VALID) "+
		"PARTITION BY RANGE (a) WITH (fillfactor = 70) TABLESPACE fast")

	assert.Equal(t, []string{"a", "b"}, columnNames(mutation.Columns))
	assert.Equal(t, []string{"a"}, mutation.PrimaryKey)
}

func TestApplyDDLCreateTableCopiesColumnsFromParents(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantCopied   []ast.CopiedTable
		wantInherits []string
		wantColumns  []string
	}{
		{
			name: "like with options copies the key",
			sql:  "CREATE TABLE c (LIKE parent INCLUDING ALL, extra int)",
			wantCopied: []ast.CopiedTable{{
				Table: ast.TableReference{Schema: "", Name: "parent", Alias: ""}, Position: 0, Keys: true,
			}},
			wantColumns: []string{"extra"},
		},
		{
			name: "like after a column goes after it",
			sql:  "CREATE TABLE c (id int, LIKE parent EXCLUDING INDEXES, extra int)",
			wantCopied: []ast.CopiedTable{{
				Table: ast.TableReference{Schema: "", Name: "parent", Alias: ""}, Position: 1, Keys: false,
			}},
			wantColumns: []string{"id", "extra"},
		},
		{
			name:         "inherits names parents without copying",
			sql:          "CREATE TABLE c (extra int) INHERITS (parent, other)",
			wantInherits: []string{"parent", "other"},
			wantColumns:  []string{"extra"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.Equal(t, ast.MutationCreateTable, mutation.Kind)
			require.Equal(t, testCase.wantCopied, mutation.CopiedTables)
			inherits := make([]string, 0, len(mutation.InheritsTables))
			for _, parent := range mutation.InheritsTables {
				inherits = append(inherits, parent.Name)
			}
			require.Equal(t, len(testCase.wantInherits), len(inherits))
			if len(testCase.wantInherits) > 0 {
				require.Equal(t, testCase.wantInherits, inherits)
			}
			require.Equal(t, testCase.wantColumns, columnNames(mutation.Columns))
		})
	}
}

func TestApplyDDLCreateTablePartitionOfNamesTheParent(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantSchema string
		wantTable  string
		wantParent ast.TableReference
	}{
		{
			name:       "partition of a range",
			sql:        "CREATE TABLE p1 PARTITION OF parent FOR VALUES FROM (1) TO (10)",
			wantTable:  "p1",
			wantParent: ast.TableReference{Name: "parent"},
		},
		{
			name:       "default partition with constraints",
			sql:        "CREATE TABLE p2 PARTITION OF public.parent (CONSTRAINT p2_positive CHECK (id > 0), id WITH OPTIONS NOT NULL) DEFAULT",
			wantTable:  "p2",
			wantParent: ast.TableReference{Schema: "public", Name: "parent"},
		},
		{
			name:       "qualified partition of a list that is itself partitioned",
			sql:        "CREATE TABLE IF NOT EXISTS archive.p3 PARTITION OF parent FOR VALUES IN ('a', 'b') PARTITION BY RANGE (id)",
			wantSchema: "archive",
			wantTable:  "p3",
			wantParent: ast.TableReference{Name: "parent"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.Equal(t, ast.MutationCreateTable, mutation.Kind)
			require.Equal(t, testCase.wantSchema, mutation.SchemaName)
			require.Equal(t, testCase.wantTable, mutation.TableName)
			require.NotNil(t, mutation.PartitionOf)
			require.Equal(t, testCase.wantParent, *mutation.PartitionOf)
			require.Empty(t, mutation.InheritsTables)
			require.Empty(t, mutation.Columns)
		})
	}
}

func TestApplyDDLAlterTableAttachesAndDetachesPartitions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantSchema string
		wantTable  string
		wantParent ast.TableReference
		wantKind   ast.MutationKind
	}{
		{
			name:       "attach for a range",
			sql:        "ALTER TABLE measurements ATTACH PARTITION measurements_2028 FOR VALUES FROM ('2028-01-01') TO ('2029-01-01')",
			wantTable:  "measurements_2028",
			wantParent: ast.TableReference{Name: "measurements"},
			wantKind:   ast.MutationAttachPartition,
		},
		{
			name:       "attach a qualified default partition",
			sql:        "ALTER TABLE IF EXISTS ONLY public.measurements ATTACH PARTITION archive.measurements_rest DEFAULT",
			wantSchema: "archive",
			wantTable:  "measurements_rest",
			wantParent: ast.TableReference{Schema: "public", Name: "measurements"},
			wantKind:   ast.MutationAttachPartition,
		},
		{
			name:       "attach for a list",
			sql:        "ALTER TABLE regions ATTACH PARTITION regions_eu FOR VALUES IN ('uk', 'fr', 'de')",
			wantTable:  "regions_eu",
			wantParent: ast.TableReference{Name: "regions"},
			wantKind:   ast.MutationAttachPartition,
		},
		{
			name:       "detach",
			sql:        "ALTER TABLE measurements DETACH PARTITION measurements_2027",
			wantTable:  "measurements_2027",
			wantParent: ast.TableReference{Name: "measurements"},
			wantKind:   ast.MutationDetachPartition,
		},
		{
			name:       "detach concurrently",
			sql:        "ALTER TABLE measurements DETACH PARTITION measurements_2027 CONCURRENTLY",
			wantTable:  "measurements_2027",
			wantParent: ast.TableReference{Name: "measurements"},
			wantKind:   ast.MutationDetachPartition,
		},
		{
			name:       "finish a concurrent detach",
			sql:        "ALTER TABLE public.measurements DETACH PARTITION public.measurements_2027 FINALIZE",
			wantSchema: "public",
			wantTable:  "measurements_2027",
			wantParent: ast.TableReference{Schema: "public", Name: "measurements"},
			wantKind:   ast.MutationDetachPartition,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.Equal(t, testCase.wantKind, mutation.Kind)
			require.Equal(t, testCase.wantSchema, mutation.SchemaName)
			require.Equal(t, testCase.wantTable, mutation.TableName)
			require.NotNil(t, mutation.PartitionOf)
			require.Equal(t, testCase.wantParent, *mutation.PartitionOf)
			require.Empty(t, mutation.AdditionalMutations)
		})
	}
}

func TestApplyDDLAlterTableRejectsAPartitionActionWithoutAName(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"ALTER TABLE measurements ATTACH PARTITION",
		"ALTER TABLE measurements DETACH PARTITION",
		"ALTER TABLE measurements ATTACH PARTITION FOR VALUES FROM (1) TO (2)",
	} {
		require.Error(t, applyDDLExpectError(t, sql), sql)
	}
}

func TestApplyDDLCreateForeignTable(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE FOREIGN TABLE IF NOT EXISTS f (id int NOT NULL, name text OPTIONS (column_name 'n')) "+
		"SERVER files OPTIONS (filename '/data/f.csv', format 'csv')")

	require.Equal(t, ast.MutationCreateTable, mutation.Kind)
	assert.Equal(t, "f", mutation.TableName)
	assert.Equal(t, []string{"id", "name"}, columnNames(mutation.Columns))
	assert.False(t, mutation.Columns[0].Nullable)
}

func applyDDLWith(t *testing.T, engine *Dialect, sql string) (*ast.CatalogueMutation, error) {
	t.Helper()

	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.NotEmpty(t, statements)

	return engine.ApplyDDL(t.Context(), statements[0])
}

func TestFlavourTableFormsNeedTheirOptions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		options []Option
		sql     string
	}{
		{name: "a computed column shorthand", options: []Option{WithComputedColumnShorthand()}, sql: "CREATE TABLE t (a text, b text AS (lower(a)) STORED)"},
		{name: "a column not visible", options: []Option{WithNotVisibleColumns()}, sql: "CREATE TABLE t (a text NOT VISIBLE)"},
		{name: "a hash-sharded primary key", options: []Option{WithInlineIndexElements()}, sql: "CREATE TABLE t (id int8, ts timestamptz, PRIMARY KEY (ts, id) USING HASH)"},
		{name: "a unique constraint storing columns", options: []Option{WithInlineIndexElements()}, sql: "CREATE TABLE t (a text, b text, CONSTRAINT t_a_key UNIQUE (a) STORING (b))"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, withoutOption := applyDDLWith(t, New(), testCase.sql)
			require.Error(t, withoutOption)

			mutation, withOption := applyDDLWith(t, New(testCase.options...), testCase.sql)
			require.NoError(t, withOption)
			require.NotNil(t, mutation)
		})
	}
}

func TestComputedColumnShorthandDeclaresAGeneratedColumn(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDLWith(t, New(WithComputedColumnShorthand()),
		"CREATE TABLE t (a text, b text AS (lower(a)) STORED, c text NOT NULL AS (upper(a)) VIRTUAL)")

	require.NoError(t, err)
	require.Len(t, mutation.Columns, 3)
	require.True(t, mutation.Columns[1].IsGenerated)
	require.Equal(t, sqlmodel.GeneratedKindStored, mutation.Columns[1].GeneratedKind)
	require.True(t, mutation.Columns[2].IsGenerated)
	require.Equal(t, sqlmodel.GeneratedKindVirtual, mutation.Columns[2].GeneratedKind)
	require.False(t, mutation.Columns[2].Nullable)
}

func TestNotVisibleColumnsAreHidden(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDLWith(t, New(WithNotVisibleColumns()), "CREATE TABLE t (id int8, secret text NOT NULL NOT VISIBLE)")

	require.NoError(t, err)
	require.Len(t, mutation.Columns, 2)
	require.False(t, mutation.Columns[0].Hidden)
	require.True(t, mutation.Columns[1].Hidden)
	require.False(t, mutation.Columns[1].Nullable)
}

func TestApplyDDLRecordsHowAStatementReplays(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		check func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "CREATE TABLE IF NOT EXISTS",
			sql:  "CREATE TABLE IF NOT EXISTS items (id int)",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.True(t, mutation.IfNotExists)
			},
		},
		{
			name: "ALTER TABLE IF EXISTS ONLY reaches every action",
			sql:  "ALTER TABLE IF EXISTS ONLY items ADD COLUMN IF NOT EXISTS a int, DROP COLUMN IF EXISTS b",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
				require.True(t, mutation.IfExists)
				require.True(t, mutation.Only)
				require.True(t, mutation.IfNotExists)
				require.Len(t, mutation.AdditionalMutations, 1)
				dropped := mutation.AdditionalMutations[0]
				require.Equal(t, ast.MutationAlterTableDropColumn, dropped.Kind)
				require.True(t, dropped.IfExists)
				require.True(t, dropped.IfColumnExists)
			},
		},
		{
			name: "ADD PRIMARY KEY makes its columns NOT NULL",
			sql:  "ALTER TABLE items ADD CONSTRAINT items_key PRIMARY KEY (a, b)",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.Equal(t, ast.MutationAlterTableAddConstraint, mutation.Kind)
				require.Equal(t, []string{"a", "b"}, mutation.PrimaryKey)
				require.Len(t, mutation.AdditionalMutations, 2)
				for index, column := range []string{"a", "b"} {
					require.Equal(t, column, mutation.AdditionalMutations[index].ColumnName)
					require.True(t, mutation.AdditionalMutations[index].SetsNotNull)
				}
			},
		},
		{
			name: "a table-level key makes its declared columns NOT NULL",
			sql:  "CREATE TABLE pairs (a int, b int, note text, PRIMARY KEY (a, b))",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.False(t, mutation.Columns[0].Nullable)
				require.False(t, mutation.Columns[1].Nullable)
				require.True(t, mutation.Columns[2].Nullable)
				require.Empty(t, mutation.AdditionalMutations)
			},
		},
		{
			name: "ALTER TYPE ADD VALUE IF NOT EXISTS AFTER",
			sql:  "ALTER TYPE mood ADD VALUE IF NOT EXISTS 'idle' AFTER 'calm'",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.Equal(t, ast.MutationAlterEnumAddValue, mutation.Kind)
				require.True(t, mutation.IfNotExists)
				require.Equal(t, ast.Placement{Anchor: "calm", Kind: ast.PlacementAfter}, mutation.Placement)
			},
		},
		{
			name: "ALTER TYPE RENAME TO",
			sql:  "ALTER TYPE mood RENAME TO feeling",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.Equal(t, ast.MutationAlterTypeRename, mutation.Kind)
				require.Equal(t, "mood", mutation.EnumName)
				require.Equal(t, "feeling", mutation.NewName)
			},
		},
		{
			name: "ALTER DOMAIN SET SCHEMA",
			sql:  "ALTER DOMAIN app.email SET SCHEMA core",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.Equal(t, ast.MutationAlterTypeSetSchema, mutation.Kind)
				require.Equal(t, "app", mutation.SchemaName)
				require.Equal(t, "email", mutation.EnumName)
				require.Equal(t, "core", mutation.NewName)
			},
		},
		{
			name: "CREATE TYPE IF NOT EXISTS",
			sql:  "CREATE TYPE IF NOT EXISTS community_role AS ENUM ('creator', 'admin')",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.Equal(t, ast.MutationCreateEnum, mutation.Kind)
				require.True(t, mutation.IfNotExists)
				require.Equal(t, []string{"creator", "admin"}, mutation.EnumValues)
			},
		},
		{
			name: "ALTER MATERIALIZED VIEW IF EXISTS SET SCHEMA",
			sql:  "ALTER MATERIALIZED VIEW IF EXISTS something SET SCHEMA computed",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.Equal(t, ast.MutationAlterTableSetSchema, mutation.Kind)
				require.Equal(t, "something", mutation.TableName)
				require.Equal(t, "computed", mutation.NewName)
				require.True(t, mutation.IfExists)
			},
		},
		{
			name: "ALTER VIEW RENAME COLUMN",
			sql:  "ALTER VIEW v RENAME COLUMN a TO b",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.Equal(t, ast.MutationAlterTableRenameColumn, mutation.Kind)
				require.Equal(t, "a", mutation.ColumnName)
				require.Equal(t, "b", mutation.NewName)
			},
		},
		{
			name: "ALTER TABLE RENAME CONSTRAINT changes nothing",
			sql:  "ALTER TABLE items RENAME CONSTRAINT old_name TO new_name",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				require.Nil(t, mutation)
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

func TestApplyDDLRejectsAStatementThatRunsOnIntoTheNext(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		wantErr string
	}{
		{
			name:    "a CREATE TABLE missing its semicolon",
			sql:     "CREATE TABLE a (id int)\nCREATE TABLE b (id int)",
			wantErr: `syntax error near "CREATE": a semicolon is missing before CREATE`,
		},
		{
			name:    "a COMMENT missing its semicolon",
			sql:     "COMMENT ON COLUMN providers.path IS 'x'\nALTER TABLE providers DROP COLUMN path",
			wantErr: `syntax error near "ALTER": a semicolon is missing before ALTER`,
		},
		{
			name: "an ALTER TABLE whose actions open lines",
			sql:  "ALTER TABLE events\n  ALTER type SET NOT NULL,\n  DROP view,\n  ALTER COLUMN x TYPE int",
		},
		{
			name: "a view whose query opens lines",
			sql:  "CREATE VIEW v AS\nSELECT a\nFROM t\nUNION\nSELECT b FROM u",
		},
		{
			name: "a schema with its elements",
			sql:  "CREATE SCHEMA app\n  CREATE TABLE a (id int)\n  CREATE VIEW v AS SELECT id FROM a",
		},
		{
			name: "a function body",
			sql:  "CREATE FUNCTION f() RETURNS void LANGUAGE sql\nBEGIN ATOMIC\nCREATE TABLE x (id int);\nEND",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.NotEmpty(t, statements)
			_, applyError := engine.ApplyDDL(t.Context(), statements[0])
			if testCase.wantErr == "" {
				if applyError != nil {
					require.NotContains(t, applyError.Error(), "semicolon is missing")
				}
				return
			}
			require.ErrorContains(t, applyError, testCase.wantErr)
		})
	}
}

func TestApplyDDLLeavesAGrantOfCreateAlone(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "GRANT USAGE,\n      CREATE ON SCHEMA app TO app_user")
	require.NoError(t, err)
	require.Len(t, statements, 1)
	_, applyError := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, applyError)
}

func TestCreateTableAsTakesItsColumnsFromTheQueryNamedByItsList(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantNames []string
	}{
		{name: "a list naming every column", sql: "CREATE TABLE t (x, y) AS SELECT 1 AS a, 'b' AS b", wantNames: []string{"x", "y"}},
		{name: "a list naming the first columns", sql: "CREATE TABLE t (x) AS SELECT 1 AS a, 'b' AS b", wantNames: []string{"x", "b"}},
		{name: "storage clauses before AS", sql: "CREATE TABLE t (x) USING heap WITH (fillfactor = 70) ON COMMIT PRESERVE ROWS TABLESPACE fast AS SELECT 1 AS a", wantNames: []string{"x"}},
		{name: "storage clauses with no list", sql: "CREATE TEMP TABLE t WITHOUT OIDS ON COMMIT DROP AS SELECT 1 AS a", wantNames: []string{"a"}},
		{name: "a query in parentheses", sql: "CREATE TABLE t (x) AS (SELECT 1 AS a)", wantNames: []string{"x"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			assert.Empty(t, mutation.Columns)
			require.NotNil(t, mutation.ViewDefinition)
			var names []string
			for _, column := range mutation.ViewDefinition.OutputColumns {
				names = append(names, column.Name)
			}
			assert.Equal(t, testCase.wantNames, names)
		})
	}
}

func TestACreateTableListOfBareNamesNeedsAQuery(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{name: "a list of names alone", sql: "CREATE TABLE t (x, y)", wantMessage: "expected AS before the query whose columns the list names"},
		{name: "a list of names before a query without AS", sql: "CREATE TABLE t (x) SELECT 1", wantMessage: "expected AS before the query whose columns the list names"},
		{name: "an ON COMMIT with no action", sql: "CREATE TEMP TABLE t ON COMMIT AS SELECT 1", wantMessage: "expected one of PRESERVE, DELETE, DROP"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := applyDDLExpectError(t, testCase.sql)

			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}
