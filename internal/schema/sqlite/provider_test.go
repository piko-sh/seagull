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
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

var errEmptyDatabaseUnsupported = errors.New("empty database: operation not supported")

type emptyDatabaseConnector struct{}

type emptyDatabaseDriver struct{}

type emptyDatabaseConnection struct{}

type emptyRows struct {
	columns []string
}

type lowerCaseTypeNormaliser struct{}

type cancellingNormaliser struct {
	cancel func(error)
	cause  error
}

type recordingNormaliser struct {
	modifiers map[string][]int
}

func (emptyDatabaseConnector) Connect(context.Context) (driver.Conn, error) {
	return emptyDatabaseConnection{}, nil
}

func (emptyDatabaseConnector) Driver() driver.Driver {
	return emptyDatabaseDriver{}
}

func (emptyDatabaseDriver) Open(string) (driver.Conn, error) {
	return emptyDatabaseConnection{}, nil
}

func (emptyDatabaseConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errEmptyDatabaseUnsupported
}

func (emptyDatabaseConnection) Close() error {
	return nil
}

func (emptyDatabaseConnection) Begin() (driver.Tx, error) {
	return nil, errEmptyDatabaseUnsupported
}

func (emptyDatabaseConnection) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &emptyRows{columns: []string{"value"}}, nil
}

func (rows *emptyRows) Columns() []string {
	return rows.columns
}

func (*emptyRows) Close() error {
	return nil
}

func (*emptyRows) Next([]driver.Value) error {
	return io.EOF
}

func (lowerCaseTypeNormaliser) NormaliseTypeName(name string, _ ...int) sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, strings.ToLower(name))
}

func (normaliser cancellingNormaliser) NormaliseTypeName(name string, _ ...int) sqlmodel.SQLType {
	normaliser.cancel(normaliser.cause)
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, strings.ToLower(name))
}

func (normaliser recordingNormaliser) NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType {
	normaliser.modifiers[name] = modifiers
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, strings.ToLower(name))
}

func TestBuildCatalogueOfAnEmptyDatabaseIsEmpty(t *testing.T) {
	database := sql.OpenDB(emptyDatabaseConnector{})
	t.Cleanup(func() { assert.NoError(t, database.Close()) })
	provider := New(database, lowerCaseTypeNormaliser{})

	catalogue, sourceErrors, err := provider.BuildCatalogue(t.Context())
	require.NoError(t, err)
	assert.Nil(t, sourceErrors)
	assert.Empty(t, catalogue.Schemas["main"].Tables)
	assert.Empty(t, catalogue.Schemas["main"].Views)
}

func TestIntrospectionFailsOnClosedDatabase(t *testing.T) {
	database := sql.OpenDB(emptyDatabaseConnector{})
	require.NoError(t, database.Close())
	provider := New(database, lowerCaseTypeNormaliser{})

	testCases := []struct {
		introspect func() error
		name       string
	}{
		{
			name: "tables",
			introspect: func() error {
				_, introspectError := provider.listTables(t.Context())
				return introspectError
			},
		},
		{
			name: "views",
			introspect: func() error {
				_, introspectError := provider.listViews(t.Context())
				return introspectError
			},
		},
		{
			name: "columns",
			introspect: func() error {
				_, introspectError := provider.relationColumns(t.Context())
				return introspectError
			},
		},
		{
			name: "indexes",
			introspect: func() error {
				_, introspectError := provider.tableIndexes(t.Context())
				return introspectError
			},
		},
		{
			name: "catalogue",
			introspect: func() error {
				_, _, introspectError := provider.BuildCatalogue(t.Context())
				return introspectError
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Error(t, testCase.introspect())
		})
	}
}

func TestClassifyGeneratedColumn(t *testing.T) {
	tests := []struct {
		name              string
		hidden            int
		wantIsGenerated   bool
		wantGeneratedKind sqlmodel.GeneratedKind
	}{
		{
			name:              "ordinary visible column",
			hidden:            0,
			wantIsGenerated:   false,
			wantGeneratedKind: sqlmodel.GeneratedKindNone,
		},
		{
			name:              "hidden column that is not generated",
			hidden:            1,
			wantIsGenerated:   false,
			wantGeneratedKind: sqlmodel.GeneratedKindNone,
		},
		{
			name:              "virtual generated column",
			hidden:            hiddenVirtualColumn,
			wantIsGenerated:   true,
			wantGeneratedKind: sqlmodel.GeneratedKindVirtual,
		},
		{
			name:              "stored generated column",
			hidden:            hiddenStoredColumn,
			wantIsGenerated:   true,
			wantGeneratedKind: sqlmodel.GeneratedKindStored,
		},
		{
			name:              "unknown flag treated as ordinary",
			hidden:            42,
			wantIsGenerated:   false,
			wantGeneratedKind: sqlmodel.GeneratedKindNone,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			isGenerated, generatedKind := classifyGeneratedColumn(testCase.hidden)
			require.Equal(t, testCase.wantIsGenerated, isGenerated, "isGenerated for hidden flag %d", testCase.hidden)
			require.Equal(t, testCase.wantGeneratedKind, generatedKind, "generatedKind for hidden flag %d", testCase.hidden)
		})
	}
}

func TestOrderedPrimaryKeyColumnsFollowsKeyPosition(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		byPosition map[int]string
		name       string
		want       []string
	}{
		{name: "no primary key", byPosition: map[int]string{}, want: nil},
		{name: "a single column", byPosition: map[int]string{1: "id"}, want: []string{"id"}},
		{
			name:       "columns declared out of key order",
			byPosition: map[int]string{3: "day", 1: "tenant", 2: "region"},
			want:       []string{"tenant", "region", "day"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, orderedPrimaryKeyColumns(testCase.byPosition))
		})
	}
}

func TestApplyRowidAlias(t *testing.T) {
	t.Parallel()

	columnsNamed := func(names ...string) []sqlmodel.Column {
		columns := make([]sqlmodel.Column, 0, len(names))
		for _, name := range names {
			columns = append(columns, sqlmodel.Column{Name: name, SQLType: sqlmodel.SQLType{EngineName: "int4"}})
		}
		return columns
	}
	primaryIndex := []sqlmodel.Index{{Name: "pk", IsPrimary: true}}

	tests := []struct {
		name          string
		primaryKey    []string
		declaredTypes map[string]string
		indexes       []sqlmodel.Index
		wantID        string
		wantDefault   bool
	}{
		{name: "integer primary key is the rowid", primaryKey: []string{"id"}, declaredTypes: map[string]string{"id": " INTEGER "}, wantID: "bigint", wantDefault: true},
		{name: "int primary key is not", primaryKey: []string{"id"}, declaredTypes: map[string]string{"id": "INT"}, wantID: "int4"},
		{name: "text primary key is not", primaryKey: []string{"id"}, declaredTypes: map[string]string{"id": "TEXT"}, wantID: "int4"},
		{name: "without rowid table", primaryKey: []string{"id"}, declaredTypes: map[string]string{"id": "INTEGER"}, indexes: primaryIndex, wantID: "int4"},
		{name: "composite key", primaryKey: []string{"id", "other"}, declaredTypes: map[string]string{"id": "INTEGER", "other": "INTEGER"}, wantID: "int4"},
		{name: "no key", declaredTypes: map[string]string{"id": "INTEGER"}, wantID: "int4"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns := columnsNamed("id", "other")
			applyRowidAlias(columns, testCase.primaryKey, testCase.declaredTypes, testCase.indexes)
			assert.Equal(t, testCase.wantID, columns[0].SQLType.EngineName)
			assert.Equal(t, testCase.wantDefault, columns[0].HasDefault)
			assert.Equal(t, "int4", columns[1].SQLType.EngineName)
			assert.False(t, columns[1].HasDefault)
		})
	}
}

func TestColumnFromPragmaReportsOnlyDeclaredDefaults(t *testing.T) {
	t.Parallel()

	provider := New(nil, lowerCaseTypeNormaliser{})
	tests := []struct {
		name         string
		row          pragmaColumn
		wantNullable bool
		wantDefault  bool
	}{
		{
			name:         "text primary key has no default and holds NULL as a rowid table lets it",
			row:          pragmaColumn{name: "name", typeName: "TEXT", primaryKeyPosition: 1},
			wantNullable: true,
			wantDefault:  false,
		},
		{
			name:         "composite primary key member has no default",
			row:          pragmaColumn{name: "tenant", typeName: "INTEGER", primaryKeyPosition: 2},
			wantNullable: true,
			wantDefault:  false,
		},
		{
			name:         "declared default",
			row:          pragmaColumn{name: "label", typeName: "TEXT", defaultValue: sql.NullString{String: "'x'", Valid: true}},
			wantNullable: true,
			wantDefault:  true,
		},
		{
			name:         "not null without default",
			row:          pragmaColumn{name: "body", typeName: "TEXT", notNull: 1},
			wantNullable: false,
			wantDefault:  false,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			column := provider.columnFromPragma(testCase.row, "")
			assert.Equal(t, testCase.row.name, column.Name)
			assert.Equal(t, testCase.wantNullable, column.Nullable)
			assert.Equal(t, testCase.wantDefault, column.HasDefault)
		})
	}
}

func usersSchema() *fakeSchema {
	return newFakeSchema().
		table("audit",
			column(0, "id", "INTEGER", false, nil, 1, 0),
			column(1, "message", "TEXT", false, nil, 0, 0)).
		table("memberships",
			column(0, "user_id", "INTEGER", true, nil, 2, 0),
			column(1, "group_id", "INTEGER", true, nil, 1, 0)).
		index("memberships", indexRow("sqlite_autoindex_memberships_1", true, "pk", "group_id"), indexRow("sqlite_autoindex_memberships_1", true, "pk", "user_id")).
		table("users",
			column(0, "id", "INTEGER", false, nil, 1, 0),
			column(1, "email", "TEXT", true, nil, 0, 0),
			column(2, "name", "TEXT", false, nil, 0, 0),
			column(3, "created_at", "TEXT", true, "CURRENT_TIMESTAMP", 0, 0),
			column(4, "name_upper", "TEXT", false, nil, 0, 2),
			column(5, "name_length", "INTEGER", false, nil, 0, 3)).
		index("users",
			indexRow("sqlite_autoindex_users_1", true, "u", "email"),
			indexRow("users_name_created", false, "c", "name"),
			indexRow("users_name_created", false, "c", "created_at"),
			indexRow("users_lower_email", true, "c", nil)).
		view("user_emails", "CREATE VIEW user_emails(id, address) AS SELECT id, email FROM users",
			column(0, "id", "INTEGER", false, nil, 0, 0),
			column(1, "address", "TEXT", false, nil, 0, 0))
}

func TestBuildCatalogueDescribesTheDatabase(t *testing.T) {
	t.Parallel()

	catalogue, sourceErrors, err := New(openFakeDatabase(t, usersSchema()), lowerCaseTypeNormaliser{}).BuildCatalogue(t.Context())
	require.NoError(t, err)
	assert.Nil(t, sourceErrors)
	assert.Equal(t, "main", catalogue.DefaultSchema)
	schema := catalogue.Schemas["main"]
	require.NotNil(t, schema)

	users := schema.Tables["users"]
	require.NotNil(t, users)
	assert.Equal(t, []string{"id"}, users.PrimaryKey)
	type described struct {
		name       string
		engineName string
		kind       sqlmodel.GeneratedKind
		nullable   bool
		hasDefault bool
	}
	got := make([]described, 0, len(users.Columns))
	for _, column := range users.Columns {
		got = append(got, described{name: column.Name, engineName: column.SQLType.EngineName, kind: column.GeneratedKind, nullable: column.Nullable, hasDefault: column.HasDefault})
	}
	assert.Equal(t, []described{
		{name: "id", engineName: "bigint", nullable: false, hasDefault: true},
		{name: "email", engineName: "text"},
		{name: "name", engineName: "text", nullable: true},
		{name: "created_at", engineName: "text", hasDefault: true},
		{name: "name_upper", engineName: "text", nullable: true, kind: sqlmodel.GeneratedKindVirtual},
		{name: "name_length", engineName: "integer", nullable: true, kind: sqlmodel.GeneratedKindStored},
	}, got)
	assert.Equal(t, []sqlmodel.Index{
		{Name: "sqlite_autoindex_users_1", Columns: []string{"email"}, IsUnique: true},
		{Name: "users_name_created", Columns: []string{"name", "created_at"}},
	}, users.Indexes, "an index keyed only by expressions is left out")

	memberships := schema.Tables["memberships"]
	require.NotNil(t, memberships)
	assert.Equal(t, []string{"group_id", "user_id"}, memberships.PrimaryKey)
	assert.Equal(t, "integer", memberships.Columns[0].SQLType.EngineName, "a WITHOUT ROWID key is not the rowid")
	assert.Equal(t, "bigint", schema.Tables["audit"].Columns[0].SQLType.EngineName)

	view := schema.Views["user_emails"]
	require.NotNil(t, view)
	assert.Equal(t, "SELECT id, email FROM users", view.Definition)
	assert.Equal(t, []string{"id", "address"}, []string{view.Columns[0].Name, view.Columns[1].Name})
	assert.True(t, view.Columns[1].Nullable, "SQLite does not say which view columns can hold NULL")
	assert.NotContains(t, schema.Tables, "user_emails")
}

func TestViewDefinition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		statement string
		want      string
	}{
		{statement: "CREATE VIEW v AS SELECT 1", want: "SELECT 1"},
		{statement: "CREATE VIEW v(a, b) AS SELECT x AS a, y AS b FROM t", want: "SELECT x AS a, y AS b FROM t"},
		{statement: `CREATE TEMP VIEW IF NOT EXISTS main."as" /* AS */ AS` + "\n  SELECT 'AS' AS label", want: "SELECT 'AS' AS label"},
		{statement: "create view lower as select 2", want: "select 2"},
		{statement: "CREATE VIEW v", want: ""},
		{statement: "", want: ""},
		{statement: "CREATE VIEW v AS SELECT 'unterminated", want: "SELECT 'unterminated"},
	}
	for _, testCase := range tests {
		assert.Equal(t, testCase.want, viewDefinition(testCase.statement), testCase.statement)
	}
}

func TestPrimaryKeysAreNotNullWhereSQLiteHoldsThemSo(t *testing.T) {
	t.Parallel()

	keyed := func(name string) *fakeSchema {
		return newFakeSchema().table(name,
			column(0, "code", "TEXT", false, nil, 1, 0),
			column(1, "note", "TEXT", false, nil, 0, 0)).
			index(name, indexRow("sqlite_autoindex_"+name+"_1", true, "pk", "code"))
	}
	testCases := []struct {
		schema       *fakeSchema
		name         string
		wantNullable []bool
	}{
		{name: "rowid table", schema: keyed("plain"), wantNullable: []bool{true, true}},
		{name: "WITHOUT ROWID table", schema: keyed("plain").kind("plain", true, false), wantNullable: []bool{false, true}},
		{name: "STRICT table", schema: keyed("plain").kind("plain", false, true), wantNullable: []bool{false, true}},
		{
			name: "rowid alias",
			schema: newFakeSchema().table("plain",
				column(0, "code", "INTEGER", false, nil, 1, 0),
				column(1, "note", "TEXT", false, nil, 0, 0)),
			wantNullable: []bool{false, true},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, _, err := New(openFakeDatabase(t, testCase.schema), lowerCaseTypeNormaliser{}).BuildCatalogue(t.Context())
			require.NoError(t, err)
			table := catalogue.Schemas["main"].Tables["plain"]
			require.NotNil(t, table)
			nullable := make([]bool, 0, len(table.Columns))
			for _, column := range table.Columns {
				nullable = append(nullable, column.Nullable)
			}
			require.Equal(t, testCase.wantNullable, nullable)
		})
	}
}

func TestBuildCatalogueReportsWhatFailed(t *testing.T) {
	t.Parallel()

	failure := errors.New("disk I/O error")
	tests := []struct {
		schema  *fakeSchema
		name    string
		message string
	}{
		{name: "listing tables", schema: usersSchema().fail(`list."wr"`, fakeResult{err: failure}), message: "listing tables: disk I/O error"},
		{name: "reading columns", schema: usersSchema().fail("pragma_table_xinfo", fakeResult{err: failure}), message: "reading columns: disk I/O error"},
		{name: "reading indexes", schema: usersSchema().fail("pragma_index_list", fakeResult{err: failure}), message: "reading indexes: disk I/O error"},
		{name: "listing views", schema: usersSchema().fail("type = 'view'", fakeResult{err: failure}), message: "listing views: disk I/O error"},
		{
			name: "a column row that does not scan", message: "reading columns: sql: Scan error on column index 1",
			schema: usersSchema().fail("pragma_table_xinfo", fakeResult{
				columns: []string{"relation", "cid", "name", "type", "notnull", "dflt_value", "pk", "hidden"},
				rows:    [][]driver.Value{{"audit", "zero", "id", "INTEGER", int64(0), nil, int64(1), int64(0)}},
			}),
		},
		{
			name: "an index row that does not scan", message: "reading indexes: sql: expected 1 destination arguments",
			schema: usersSchema().fail("pragma_index_list", fakeResult{columns: []string{"name"}, rows: [][]driver.Value{{"broken"}}}),
		},
		{
			name: "a view row that does not scan", message: "listing views: sql: expected 1 destination arguments",
			schema: usersSchema().fail("type = 'view'", fakeResult{columns: []string{"name"}, rows: [][]driver.Value{{"broken"}}}),
		},
		{
			name: "a table row that does not scan", message: "listing tables: sql: expected 2 destination arguments",
			schema: usersSchema().fail(`list."wr"`, fakeResult{columns: []string{"name", "extra"}, rows: [][]driver.Value{{"a", "b"}}}),
		},
		{
			name: "rows that fail part-way", message: "listing views: connection reset",
			schema: usersSchema().fail("type = 'view'", fakeResult{columns: []string{"name", "sql"}, nextError: errors.New("connection reset")}),
		},
		{
			name: "columns that fail part-way", message: "reading columns: connection reset",
			schema: usersSchema().fail("pragma_table_xinfo", fakeResult{columns: []string{"relation"}, nextError: errors.New("connection reset")}),
		},
		{
			name: "indexes that fail part-way", message: "reading indexes: connection reset",
			schema: usersSchema().fail("pragma_index_list", fakeResult{columns: []string{"table"}, nextError: errors.New("connection reset")}),
		},
		{
			name: "tables that fail part-way", message: "listing tables: connection reset",
			schema: usersSchema().fail(`list."wr"`, fakeResult{columns: []string{"name"}, nextError: errors.New("connection reset")}),
		},
		{
			name: "rows that fail to close", message: "listing tables: busy",
			schema: usersSchema().fail(`list."wr"`, fakeResult{columns: []string{"name"}, closeError: errors.New("busy")}),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue, sourceErrors, err := New(openFakeDatabase(t, testCase.schema), lowerCaseTypeNormaliser{}).BuildCatalogue(t.Context())
			require.ErrorContains(t, err, testCase.message)
			assert.Nil(t, catalogue)
			assert.Nil(t, sourceErrors)
		})
	}
}

func TestBuildCatalogueNeedsADatabaseAndANormaliser(t *testing.T) {
	t.Parallel()

	_, _, err := New(nil, lowerCaseTypeNormaliser{}).BuildCatalogue(t.Context())
	require.ErrorIs(t, err, ErrNotConfigured)
	_, _, err = New(openFakeDatabase(t, usersSchema()), nil).BuildCatalogue(t.Context())
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestBuildCatalogueStopsWithTheCauseAndHowFarItGot(t *testing.T) {
	t.Parallel()

	cause := errors.New("deploy cancelled")
	tests := []struct {
		normaliser func(cancel func(error)) dialect.TypeNormaliserPort
		name       string
		cancelled  bool
		want       string
	}{
		{
			name:       "before the first query",
			normaliser: func(func(error)) dialect.TypeNormaliserPort { return lowerCaseTypeNormaliser{} },
			cancelled:  true,
			want:       "listing tables: " + context.Canceled.Error() + ": deploy cancelled",
		},
		{
			name: "while the tables are assembled",
			normaliser: func(cancel func(error)) dialect.TypeNormaliserPort {
				return cancellingNormaliser{cancel: cancel, cause: cause}
			},
			want: "introspection stopped after 1 of 3 tables: " + context.Canceled.Error() + ": deploy cancelled",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			if testCase.cancelled {
				cancel(cause)
			}

			catalogue, _, err := New(openFakeDatabase(t, usersSchema()), testCase.normaliser(cancel)).BuildCatalogue(ctx)

			require.ErrorIs(t, err, context.Canceled)
			require.ErrorIs(t, err, cause)
			assert.EqualError(t, err, testCase.want)
			assert.Nil(t, catalogue)
		})
	}
}

func TestDeclaredTypeModifiersReachTheNormaliser(t *testing.T) {
	t.Parallel()

	tests := []struct {
		declared      string
		wantName      string
		wantModifiers []int
	}{
		{declared: "VARCHAR(100)", wantName: "VARCHAR", wantModifiers: []int{100}},
		{declared: " DECIMAL( 10 , 2 ) ", wantName: "DECIMAL", wantModifiers: []int{10, 2}},
		{declared: "NATIVE CHARACTER(70)", wantName: "NATIVE CHARACTER", wantModifiers: []int{70}},
		{declared: "TEXT", wantName: "TEXT"},
		{declared: "VARCHAR(n)", wantName: "VARCHAR(n)"},
		{declared: "VARCHAR(10", wantName: "VARCHAR(10"},
		{declared: "", wantName: ""},
	}
	for _, testCase := range tests {
		name, modifiers := splitDeclaredType(testCase.declared)
		assert.Equal(t, testCase.wantName, name, testCase.declared)
		assert.Equal(t, testCase.wantModifiers, modifiers, testCase.declared)
	}

	normaliser := recordingNormaliser{modifiers: map[string][]int{}}
	schema := newFakeSchema().table("products", column(0, "price", "NUMERIC(10,2)", true, nil, 0, 0))
	_, _, err := New(openFakeDatabase(t, schema), normaliser).BuildCatalogue(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[string][]int{"NUMERIC": {10, 2}}, normaliser.modifiers)
}

func featureSchema() *fakeSchema {
	return newFakeSchema().
		virtual("docs", "CREATE VIRTUAL TABLE docs USING fts5(title, body)",
			column(0, "title", "", false, nil, 0, 0),
			column(1, "body", "", false, nil, 0, 0),
			column(2, "docs", "", false, nil, 0, hiddenColumn),
			column(3, "rank", "", false, nil, 0, hiddenColumn)).
		virtual("boxes", `CREATE VIRTUAL TABLE "boxes" USING RTree(id, minx, maxx)`,
			column(0, "id", "INT", false, nil, 0, 0),
			column(1, "minx", "REAL", false, nil, 0, 0),
			column(2, "maxx", "REAL", false, nil, 0, 0)).
		table("kv",
			column(0, "k", "TEXT", true, nil, 1, 0),
			column(1, "v", "BLOB", false, nil, 0, 0)).
		kind("kv", true, false).
		index("kv", indexRow("sqlite_autoindex_kv_1", true, "pk", "k"))
}

func TestBuildCatalogueDescribesVirtualAndWithoutRowIDTables(t *testing.T) {
	t.Parallel()

	type described struct {
		name          string
		engineName    string
		generatedKind sqlmodel.GeneratedKind
		hidden        bool
		nullable      bool
	}
	describe := func(columns []sqlmodel.Column) []described {
		got := make([]described, 0, len(columns))
		for _, column := range columns {
			got = append(got, described{
				name:          column.Name,
				engineName:    column.SQLType.EngineName,
				generatedKind: column.GeneratedKind,
				hidden:        column.Hidden,
				nullable:      column.Nullable,
			})
		}
		return got
	}

	tests := []struct {
		name            string
		table           string
		wantModule      string
		wantColumns     []described
		wantPrimaryKey  []string
		wantVirtual     bool
		wantWithoutRows bool
	}{
		{
			name:        "an FTS5 table hides its rank and its table-named column",
			table:       "docs",
			wantModule:  "fts5",
			wantVirtual: true,
			wantColumns: []described{
				{name: "title", engineName: "text", nullable: true},
				{name: "body", engineName: "text", nullable: true},
				{name: "docs", engineName: "text", hidden: true, nullable: true},
				{name: "rank", engineName: "real", hidden: true, nullable: true, generatedKind: sqlmodel.GeneratedKindVirtual},
			},
		},
		{
			name:        "another module keeps the types it declares",
			table:       "boxes",
			wantModule:  "rtree",
			wantVirtual: true,
			wantColumns: []described{
				{name: "id", engineName: "int", nullable: true},
				{name: "minx", engineName: "real", nullable: true},
				{name: "maxx", engineName: "real", nullable: true},
			},
		},
		{
			name:            "a WITHOUT ROWID table says so and holds its key NOT NULL",
			table:           "kv",
			wantWithoutRows: true,
			wantPrimaryKey:  []string{"k"},
			wantColumns: []described{
				{name: "k", engineName: "text"},
				{name: "v", engineName: "blob", nullable: true},
			},
		},
	}
	catalogue, _, err := New(openFakeDatabase(t, featureSchema()), lowerCaseTypeNormaliser{}).BuildCatalogue(t.Context())
	require.NoError(t, err)
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			table := catalogue.Schemas["main"].Tables[testCase.table]
			require.NotNil(t, table)
			assert.Equal(t, testCase.wantVirtual, table.IsVirtual)
			assert.Equal(t, testCase.wantModule, table.VirtualModuleName)
			assert.Equal(t, testCase.wantWithoutRows, table.IsWithoutRowID)
			assert.Equal(t, testCase.wantPrimaryKey, table.PrimaryKey)
			assert.Equal(t, testCase.wantColumns, describe(table.Columns))
			if testCase.wantVirtual {
				assert.Empty(t, table.Indexes, "a virtual table has no indexes of its own")
			}
		})
	}
}

func TestVirtualModuleReadsTheNameAfterUsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		statement string
		want      string
	}{
		{statement: "CREATE VIRTUAL TABLE docs USING fts5(title, body)", want: "fts5"},
		{statement: `CREATE VIRTUAL TABLE "My Docs" USING FTS5(a)`, want: "fts5"},
		{statement: "CREATE VIRTUAL TABLE t USING rtree", want: "rtree"},
		{statement: `CREATE VIRTUAL TABLE "using" /* USING other */ USING [rtree_i32](id, a, b)`, want: "rtree_i32"},
		{statement: `create virtual table t using "Odd""Module"(a)`, want: `odd"module`},
		{statement: "CREATE VIRTUAL TABLE t USING 'fts5'(a)", want: ""},
		{statement: "CREATE VIRTUAL TABLE t USING", want: ""},
		{statement: "CREATE VIRTUAL TABLE t", want: ""},
		{statement: "", want: ""},
	}
	for _, testCase := range tests {
		t.Run(testCase.statement, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, virtualModule(testCase.statement))
		})
	}
}

func TestBuildCatalogueReadsAnyNumberOfTablesWithFourQueries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tables int
	}{
		{name: "one table", tables: 1},
		{name: "two hundred tables", tables: 200},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			schema := newFakeSchema().view("v", "CREATE VIEW v AS SELECT 1", column(0, "one", "", false, nil, 0, 0))
			for index := range testCase.tables {
				name := fmt.Sprintf("table_%03d", index)
				schema.table(name, column(0, "id", "INTEGER", false, nil, 1, 0)).
					index(name, indexRow(name+"_id", true, "c", "id"))
			}

			catalogue, _, err := New(openFakeDatabase(t, schema), lowerCaseTypeNormaliser{}).BuildCatalogue(t.Context())
			require.NoError(t, err)
			require.Len(t, catalogue.Schemas["main"].Tables, testCase.tables)
			assert.Equal(t, map[string]int{listTablesQuery: 1, listViewsQuery: 1, relationColumnsQuery: 1, tableIndexesQuery: 1}, schema.queries)
		})
	}
}
