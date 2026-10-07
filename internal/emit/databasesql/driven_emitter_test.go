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

package databasesql

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/duckdb"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/sqlite"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/generate"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestProfileForChoosesTheDriverFromTheDialect(t *testing.T) {
	t.Parallel()

	array := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray}
	testCases := []struct {
		name             string
		driver           string
		drivers          []string
		arrayReading     gogen.ColumnReading
		bindsNamedValues bool
	}{
		{name: "the first known driver of the dialect", drivers: []string{"example.com/unknown", driverClickHouse}, bindsNamedValues: true, arrayReading: gogen.ColumnReadAsIs},
		{name: "a dialect with no known driver gets the general profile", drivers: []string{"example.com/unknown"}, arrayReading: gogen.ColumnReadAsJSON},
		{name: "a dialect with no drivers gets the general profile", drivers: nil, arrayReading: gogen.ColumnReadAsJSON},
		{name: "a named driver wins over the dialect's", driver: driverClickHouse, drivers: []string{driverPgx}, bindsNamedValues: true, arrayReading: gogen.ColumnReadAsIs},
		{name: "a named driver the emitter does not know gets the general profile", driver: "example.com/unknown", drivers: []string{driverClickHouse}, arrayReading: gogen.ColumnReadAsJSON},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			profile := profileFor(testCase.drivers, testCase.driver)
			assert.Equal(t, testCase.bindsNamedValues, profile.bindsNamedValues)
			assert.Equal(t, testCase.arrayReading, profile.reading(array))
		})
	}
}

func TestStrategyTakesPlaceholdersFromTheDialect(t *testing.T) {
	t.Parallel()

	renamed := dialect.Wrap(mysql.New(mysql.WithName("renamed"))).Info()
	strategy := driverFor(New(), &emit.Request{Dialect: renamed})
	assert.Equal(t, renamed.Placeholders, strategy.dialect.Placeholders, "a renamed flavour keeps its dialect's placeholders")
	assert.Equal(t, "`order`", strategy.dialect.IdentifierQuote.Quote("order"))
	array := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray}
	decimal := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryDecimal}
	postgresStrategy := driverFor(New(), &emit.Request{Dialect: dialect.Wrap(postgres.New()).Info()})
	assert.Equal(t, gogen.ColumnReadAsJSON, postgresStrategy.ColumnReading(array), "pgx through database/sql cannot scan arrays")
	assert.Equal(t, gogen.ColumnReadAsIs, postgresStrategy.ColumnReading(decimal))

	clickHouseStrategy := driverFor(New(), &emit.Request{Dialect: dialect.Wrap(clickhouse.New()).Info()})
	assert.Equal(t, gogen.ColumnReadAsIs, clickHouseStrategy.ColumnReading(array), "clickhouse-go scans arrays itself")

	duckDBStrategy := driverFor(New(), &emit.Request{Dialect: dialect.Wrap(duckdb.New()).Info()})
	assert.Equal(t, gogen.ColumnReadConverted, duckDBStrategy.ColumnReading(sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct}),
		"the DuckDB driver returns a struct as a map of its own values, which the scan adapter converts")
	assert.Equal(t, gogen.ColumnReadAsText, duckDBStrategy.ColumnReading(decimal), "the DuckDB driver returns its own Decimal")
	assert.Equal(t, gogen.ColumnReadAsText, duckDBStrategy.ColumnReading(sqlmodel.SQLType{Category: sqlmodel.TypeCategoryJSON}),
		"the DuckDB driver decodes a JSON value, losing a string's quotes and a number's digits")
	assert.Equal(t, gogen.ColumnReadConverted, duckDBStrategy.ColumnReading(array))
	_, duckDBConverts := duckDBStrategy.reader().(gogen.ColumnConverter)
	assert.True(t, duckDBConverts, "the DuckDB driver's reader has the driver-composite scan adapter")
	_, postgresConverts := postgresStrategy.reader().(gogen.ColumnConverter)
	assert.False(t, postgresConverts, "a driver reading nothing converted has no scan adapter")
}

func defaultMappings() sqlmodel.GoTypeResolver {
	return &gotypes.MappingTable{
		Mappings: []gotypes.Mapping{
			{
				SQLCategory: sqlmodel.TypeCategoryInteger,
				SQLName:     "int8",
				NotNull:     sqlmodel.GoType{Name: "int64"},
				Nullable:    sqlmodel.GoType{Name: "*int64"},
			},
			{
				SQLCategory: sqlmodel.TypeCategoryInteger,
				NotNull:     sqlmodel.GoType{Name: "int32"},
				Nullable:    sqlmodel.GoType{Name: "*int32"},
			},
			{
				SQLCategory: sqlmodel.TypeCategoryText,
				NotNull:     sqlmodel.GoType{Name: "string"},
				Nullable:    sqlmodel.GoType{Name: "*string"},
			},
			{
				SQLCategory: sqlmodel.TypeCategoryBoolean,
				NotNull:     sqlmodel.GoType{Name: "bool"},
				Nullable:    sqlmodel.GoType{Name: "*bool"},
			},
			{
				SQLCategory: sqlmodel.TypeCategoryFloat,
				NotNull:     sqlmodel.GoType{Name: "float64"},
				Nullable:    sqlmodel.GoType{Name: "*float64"},
			},
		},
	}
}

func indexOf(source, substring string) int {
	return strings.Index(source, substring)
}

func TestEmitQuerier(t *testing.T) {
	file, err := emitkit.Querier("mydb", emit.Naming{}, sqlQuerier)
	require.NoError(t, err)
	assert.Equal(t, "querier.go", file.Name)

	source := string(file.Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "querier.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.True(t, strings.HasPrefix(source, "package mydb"), "the header is applied by the orchestration, not the part")
	assert.Contains(t, source, "type DBTX interface")
	assert.Contains(t, source, "ExecContext")
	assert.Contains(t, source, "QueryContext")
	assert.Contains(t, source, "QueryRowContext")
	assert.Contains(t, source, "type Queries struct")
	assert.Contains(t, source, "func New(")
	assert.Contains(t, source, "func NewWithReplica(")
	assert.Contains(t, source, "func (queries *Queries) WithTx(")

	assert.Contains(t, source, "reader DBTX")
	assert.Contains(t, source, "writer DBTX")

	assert.Contains(t, source, "type dbtxWrapper interface")
	assert.Contains(t, source, "WrapDBTX(inner any) any")
	assert.Contains(t, source, "wrapper dbtxWrapper")
	assert.Contains(t, source, "wrapper, _ := db.(dbtxWrapper)")
	assert.Contains(t, source, "wrapper, _ := writer.(dbtxWrapper)")
	assert.Contains(t, source, "db := queries.wrapDBTX(transaction)")
	assert.Contains(t, source, "&Queries{reader: db, writer: db, wrapper: queries.wrapper}")
	assert.Contains(t, source, "func (queries *Queries) wrapDBTX(db DBTX) DBTX")
	assert.Contains(t, source, "if queries.wrapper == nil {",
		"wrapDBTX returns the plain connection when the querier is uninstrumented")
	assert.NotContains(t, source, "if queries == nil || queries.wrapper == nil {",
		"WithTx already returns early on a nil receiver, so a second check would be unreachable")
	assert.Contains(t, source, "return &Queries{reader: transaction, writer: transaction, wrapper: nil}",
		"WithTx on a nil receiver returns a plain transaction-scoped Queries")

	assert.Contains(t, source, "func (queries *Queries) RunInTx(")
	assert.Contains(t, source, "db.BeginTx(ctx, nil)")
	assert.Contains(t, source, "defer transaction.Rollback()")
	assert.Contains(t, source, "queries.WithTx(transaction)")
	assert.Contains(t, source, "transaction.Commit()")

	assert.NotContains(t, source, "PgxDBTX")
	assert.NotContains(t, source, "PgxQueries")
	assert.NotContains(t, source, "NewPgx")
	assert.NotContains(t, source, "pgx")
	assert.NotContains(t, source, "SendBatch")
	assert.NotContains(t, source, "CopyFrom")
}

func TestEmitQueriesManyCommand(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "ListUsers",
			Command:  sqlmodel.QueryCommandMany,
			SQL:      "-- sgl.name: ListUsers\n-- sgl.command: many\nSELECT id, name FROM users",
			Filename: "list_users.sql",
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
				{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "list_users.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "type ListUsersRow struct")
	assert.Contains(t, source, "func (queries *Queries) ListUsers(")
	assert.Contains(t, source, "QueryContext")
	assert.Contains(t, source, "rows.Next()")
	assert.Contains(t, source, "rows.Close()")
	assert.Contains(t, source, "rows.Err()")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesExecCommand(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "DeleteUser",
			Command:  sqlmodel.QueryCommandExec,
			SQL:      "-- sgl.name: DeleteUser\n-- sgl.command: exec\nDELETE FROM users WHERE id = ?",
			Filename: "delete_user.sql",
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "delete_user.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "func (queries *Queries) DeleteUser(")
	assert.Contains(t, source, "ExecContext")
	assert.Contains(t, source, "error")
	assert.NotContains(t, source, "DeleteUserRow")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesExecRowsCommand(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "DeleteOldUsers",
			Command:  sqlmodel.QueryCommandExecRows,
			SQL:      "-- sgl.name: DeleteOldUsers\n-- sgl.command: execrows\nDELETE FROM users WHERE active = 0",
			Filename: "delete_old_users.sql",
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "delete_old_users.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "func (queries *Queries) DeleteOldUsers(")
	assert.Contains(t, source, "RowsAffected")
	assert.Contains(t, source, "int64")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesExecResultCommand(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "InsertUser",
			Command:  sqlmodel.QueryCommandExecResult,
			SQL:      "-- sgl.name: InsertUser\n-- sgl.command: execresult\nINSERT INTO users (name) VALUES (?)",
			Filename: "insert_user.sql",
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "insert_user.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "func (queries *Queries) InsertUser(")
	assert.Contains(t, source, "ExecContext")
	assert.Contains(t, source, "sql.Result")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesMultipleParameters(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "GetUserByNameAndEmail",
			Command:  sqlmodel.QueryCommandOne,
			SQL:      "-- sgl.name: GetUserByNameAndEmail\n-- sgl.command: one\nSELECT id FROM users WHERE name = ? AND email = ?",
			Filename: "get_user_by_name_and_email.sql",
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			},
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
				{Number: 2, Name: "email", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "get_user.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "type GetUserByNameAndEmailParams struct")
	assert.Contains(t, source, "params GetUserByNameAndEmailParams")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesEmpty(t *testing.T) {
	emitter := New()

	files, err := emitQueriesFor(t.Context(), emitter, "db", nil, defaultMappings())
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestEmitQueriesStreamCommand(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "StreamUsers",
			Command:  sqlmodel.QueryCommandStream,
			SQL:      "-- sgl.name: StreamUsers\n-- sgl.command: stream\nSELECT id, name FROM users",
			Filename: "stream_users.sql",
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
				{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "stream_users.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "type StreamUsersRow struct")
	assert.Contains(t, source, "func (queries *Queries) StreamUsers(")
	assert.Contains(t, source, "func(yield func(StreamUsersRow, error) bool)")
	assert.Contains(t, source, "QueryContext")
	assert.Contains(t, source, "rows.Next()")
	assert.Contains(t, source, "!yield(row, nil)")
	assert.Contains(t, source, "rows.Err()")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesDynamicLimitOffset(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:      "ListProducts",
			Command:   sqlmodel.QueryCommandMany,
			SQL:       "-- sgl.name: ListProducts\n-- sgl.command: many\n-- $1 as sgl.optional(category)\n-- $2 as sgl.limit(pageLimit) default:50 max:200\n-- $3 as sgl.offset(pageOffset)\nSELECT id, name FROM products WHERE ($1 IS NULL OR category = $1) LIMIT $2 OFFSET $3",
			Filename:  "list_products.sql",
			IsDynamic: true,
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
				{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "category", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, IsOptional: true, Nullable: true},
				{Number: 2, Name: "page_limit", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "int4"}, Context: sqlmodel.ParameterContextLimit, DefaultLimit: new(50), MaxLimit: new(200)},
				{Number: 3, Name: "page_offset", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "int4"}, Context: sqlmodel.ParameterContextOffset},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "list_products.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "type ListProductsParams struct")
	assert.Contains(t, source, "Category")
	assert.Contains(t, source, "PageLimit")
	assert.Contains(t, source, "PageOffset")
	assert.Contains(t, source, "params ListProductsParams")
	assert.Contains(t, source, "params.PageLimit == 0")
	assert.Contains(t, source, "params.PageLimit = 50")
	assert.Contains(t, source, "params.PageLimit > 200")
	assert.Contains(t, source, "params.PageLimit = 200")

	assert.NotContains(t, source, "Option")
	assert.NotContains(t, source, "WithCategory")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesDynamicOneCommand(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:      "FindUser",
			Command:   sqlmodel.QueryCommandOne,
			SQL:       "-- sgl.name: FindUser\n-- sgl.command: one\n-- $1 as sgl.param(userID)\n-- $2 as sgl.optional(email)\nSELECT id, name FROM users WHERE id = $1 AND ($2 IS NULL OR email = $2)",
			Filename:  "find_user.sql",
			IsDynamic: true,
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
				{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "user_id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}, Kind: sqlmodel.ParameterDirectiveParam},
				{Number: 2, Name: "email", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, IsOptional: true, Nullable: true},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "find_user.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "QueryRowContext")
	assert.Contains(t, source, ".Scan(")
	assert.Contains(t, source, "type FindUserParams struct")
	assert.Contains(t, source, "UserID")
	assert.Contains(t, source, "Email")
	assert.Contains(t, source, "params FindUserParams")
	assert.Contains(t, source, "type FindUserRow struct")

	assert.NotContains(t, source, "Option")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesDynamicOrderDirectionDedup(t *testing.T) {
	defaultLimit := 50
	maxLimit := 200
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:      "SearchUsers",
			Command:   sqlmodel.QueryCommandMany,
			SQL:       "SELECT id, name FROM users",
			Filename:  "queries.sql",
			IsDynamic: true,
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
				{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "sort", Kind: sqlmodel.ParameterDirectiveSortable, SortableColumns: []string{"name"}},
				{Number: 2, Name: "page_limit", Context: sqlmodel.ParameterContextLimit, DefaultLimit: &defaultLimit, MaxLimit: &maxLimit},
			},
		},
		{
			Name:      "SearchProducts",
			Command:   sqlmodel.QueryCommandMany,
			SQL:       "SELECT id, title FROM products",
			Filename:  "queries.sql",
			IsDynamic: true,
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
				{Name: "title", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "order_by", Kind: sqlmodel.ParameterDirectiveSortable, SortableColumns: []string{"title", "price"}},
				{Number: 2, Name: "page_limit", Context: sqlmodel.ParameterContextLimit, DefaultLimit: &defaultLimit, MaxLimit: &maxLimit},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 2, "expected the query file and sortable.go")

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "queries.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Zero(t, strings.Count(source, "type OrderDirection string"), "query files leave OrderDirection to sortable.go")
	assert.Equal(t, "sortable.go", files[1].Name)
	assert.Equal(t, 1, strings.Count(string(files[1].Content), "type OrderDirection string"), "OrderDirection should be emitted exactly once")
	assert.Contains(t, source, "type SearchUsersOrderBy string")
	assert.Contains(t, source, "type SearchProductsOrderBy string")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesDynamicStreamCommand(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:      "StreamProducts",
			Command:   sqlmodel.QueryCommandStream,
			SQL:       "-- sgl.name: StreamProducts\n-- sgl.command: stream\n-- $1 as sgl.optional(category)\nSELECT id, name FROM products WHERE ($1 IS NULL OR category = $1)",
			Filename:  "stream_products.sql",
			IsDynamic: true,
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
				{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
			Parameters: []sqlmodel.QueryParameter{
				{
					Number: 1,
					Name:   "category",
					SQLType: sqlmodel.SQLType{
						Category:   sqlmodel.TypeCategoryText,
						EngineName: "text",
					},
					IsOptional: true,
				},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "stream_products.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "StreamProductsParams")
	assert.Contains(t, source, "params StreamProductsParams")
	assert.Contains(t, source, "func(yield func(StreamProductsRow, error) bool)")
	assert.Contains(t, source, "!yield(row, nil)")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesEmbedInnerJoin(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "GetOrderWithUser",
			Command:  sqlmodel.QueryCommandOne,
			SQL:      "-- sgl.name: GetOrderWithUser\n-- sgl.command: one\nSELECT o.id, o.total, /* sgl.embed(users) */ u.id, u.name, u.email FROM orders o JOIN users u ON u.id = o.user_id WHERE o.id = $1",
			Filename: "get_order_with_user.sql",
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
				{Name: "total", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryFloat}},
				{Name: "user_id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}, IsEmbedded: true, EmbedTable: "users", EmbedIsOuter: false, SourceTable: "users"},
				{Name: "user_name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, IsEmbedded: true, EmbedTable: "users", EmbedIsOuter: false, SourceTable: "users"},
				{Name: "user_email", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, IsEmbedded: true, EmbedTable: "users", EmbedIsOuter: false, SourceTable: "users"},
			},
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "get_order_with_user.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "type GetOrderWithUserUsers struct")
	assert.Contains(t, source, "type GetOrderWithUserRow struct")
	assert.Contains(t, source, "Users GetOrderWithUserUsers")
	assert.NotContains(t, source, "*GetOrderWithUserUsers", "inner join should use value type, not pointer")
	assert.Contains(t, source, "&row.Users.UserID")
	assert.Contains(t, source, "&row.Users.UserName")
	assert.Contains(t, source, "&row.Users.UserEmail")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesEmbedLeftJoin(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "GetOrderWithUser",
			Command:  sqlmodel.QueryCommandOne,
			SQL:      "-- sgl.name: GetOrderWithUser\n-- sgl.command: one\nSELECT o.id, o.total, /* sgl.embed(users) */ u.id, u.name FROM orders o LEFT JOIN users u ON u.id = o.user_id WHERE o.id = $1",
			Filename: "get_order_with_user.sql",
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
				{Name: "total", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryFloat}},
				{Name: "user_id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}, IsEmbedded: true, EmbedTable: "users", EmbedIsOuter: true, SourceTable: "users", Nullable: true},
				{Name: "user_name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, IsEmbedded: true, EmbedTable: "users", EmbedIsOuter: true, SourceTable: "users", Nullable: true},
			},
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "get_order_with_user.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "type GetOrderWithUserUsers struct")
	assert.Contains(t, source, "type GetOrderWithUserRow struct")
	assert.Contains(t, source, "*GetOrderWithUserUsers", "left join should use pointer type")
	assert.Contains(t, source, "row.Users = &GetOrderWithUserUsers{}")
	assert.Contains(t, source, "&row.Users.UserID")
	assert.Contains(t, source, "row.Users.UserID == nil")
	assert.Contains(t, source, "row.Users = nil")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesGroupByOneToMany(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:       "ListOrdersWithItems",
			Command:    sqlmodel.QueryCommandMany,
			SQL:        "-- sgl.name: ListOrdersWithItems\n-- sgl.command: many\n-- sgl.group_by: orders.id\nSELECT /* sgl.embed(orders) */ o.id, o.total, /* sgl.embed(order_items) */ i.id, i.name, i.quantity FROM orders o LEFT JOIN order_items i ON i.order_id = o.id",
			Filename:   "list_orders_with_items.sql",
			GroupByKey: []string{"orders.id"},
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}, IsEmbedded: true, EmbedTable: "orders", SourceTable: "orders"},
				{Name: "total", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryFloat}, IsEmbedded: true, EmbedTable: "orders", SourceTable: "orders"},
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}, IsEmbedded: true, EmbedTable: "order_items", EmbedIsOuter: true, SourceTable: "order_items", Nullable: true},
				{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, IsEmbedded: true, EmbedTable: "order_items", EmbedIsOuter: true, SourceTable: "order_items", Nullable: true},
				{Name: "quantity", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}, IsEmbedded: true, EmbedTable: "order_items", EmbedIsOuter: true, SourceTable: "order_items", Nullable: true},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "list_orders_with_items.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "type ListOrdersWithItemsOrders struct")
	assert.Contains(t, source, "type ListOrdersWithItemsOrderItems struct")
	assert.Contains(t, source, "type ListOrdersWithItemsRow struct")
	assert.NotContains(t, source, "*ListOrdersWithItemsOrders", "key embed should be value type, not pointer")
	assert.Contains(t, source, "[]ListOrdersWithItemsOrderItems", "detail embed should be slice type")
	assert.Contains(t, source, "groupIndex")
	assert.Contains(t, source, "groupIndex := make(map[int32]int)")
	assert.Contains(t, source, "append(results[entry].OrderItems")

	assert.NotContains(t, source, "pgx")
}

func TestEmitQueriesEmbedManyCommand(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "ListOrdersWithUser",
			Command:  sqlmodel.QueryCommandMany,
			SQL:      "-- sgl.name: ListOrdersWithUser\n-- sgl.command: many\nSELECT o.id, o.total, /* sgl.embed(users) */ u.id, u.name FROM orders o LEFT JOIN users u ON u.id = o.user_id",
			Filename: "list_orders_with_user.sql",
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
				{Name: "total", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryFloat}},
				{Name: "user_id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}, IsEmbedded: true, EmbedTable: "users", EmbedIsOuter: true, SourceTable: "users", Nullable: true},
				{Name: "user_name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, IsEmbedded: true, EmbedTable: "users", EmbedIsOuter: true, SourceTable: "users", Nullable: true},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "db", queries, defaultMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	source := string(files[0].Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "list_orders_with_user.sql.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "type ListOrdersWithUserUsers struct")
	assert.Contains(t, source, "type ListOrdersWithUserRow struct")
	assert.Contains(t, source, "*ListOrdersWithUserUsers")
	assert.Contains(t, source, "row.Users = &ListOrdersWithUserUsers{UserID: embedRows.Users.UserID, UserName: embedRows.Users.UserName}")
	assert.Contains(t, source, "embedRows.Users.UserID != nil")

	assert.NotContains(t, source, "pgx")
}

func TestEmitPrepared(t *testing.T) {

	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:    "GetUserByID",
			SQL:     "SELECT id, name FROM users WHERE id = $1",
			Command: sqlmodel.QueryCommandOne,
		},
		{
			Name:    "ListUsers",
			SQL:     "SELECT id, name FROM users",
			Command: sqlmodel.QueryCommandMany,
		},
		{
			Name:      "SearchProducts",
			SQL:       "SELECT id, name FROM products",
			Command:   sqlmodel.QueryCommandMany,
			IsDynamic: true,
		},
		{
			Name:           "DynamicSearch",
			SQL:            "SELECT id FROM items",
			Command:        sqlmodel.QueryCommandMany,
			DynamicRuntime: true,
		},
	}

	file, err := renderPrepared(gogen.NewVocabulary(testRequest("mydb", nil, nil)), preparedRequest("mydb", queries))
	require.NoError(t, err)
	assert.Equal(t, "prepared.go", file.Name)

	source := string(file.Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "prepared.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "type PreparedDBTX struct")
	assert.Contains(t, source, "map[string]*sql.Stmt")
	assert.Contains(t, source, "sync.RWMutex")

	assert.Contains(t, source, "func Prepare(ctx context.Context, db *sql.DB)")
	assert.Contains(t, source, "getuserbyid")
	assert.Contains(t, source, "listusers")
	assert.NotContains(t, source, "searchproducts")
	assert.NotContains(t, source, "dynamicsearch")

	assert.Contains(t, source, "func (prepared *PreparedDBTX) cachedStatement(")
	assert.Contains(t, source, "prepared.mu.RLock()")
	assert.Contains(t, source, "prepared.mu.Lock()")
	assert.Contains(t, source, "PrepareContext")

	assert.Contains(t, source, "func (prepared *PreparedDBTX) ExecContext(")
	assert.Contains(t, source, "func (prepared *PreparedDBTX) QueryContext(")
	assert.Contains(t, source, "func (prepared *PreparedDBTX) QueryRowContext(")
	assert.Contains(t, source, "func (prepared *PreparedDBTX) BeginTx(")
	assert.Contains(t, source, "func (prepared *PreparedDBTX) Close()")

	assert.Contains(t, source, "prepared.db.BeginTx(ctx, options)")

	assert.NotContains(t, source, "pgx")
	assert.NotContains(t, source, "PgxDBTX")
}

func TestEmitPreparedCachesOnlyStaticQueries(t *testing.T) {

	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:    "GetUserByID",
			SQL:     "SELECT id, name FROM users WHERE id = $1",
			Command: sqlmodel.QueryCommandOne,
		},
		{
			Name:           "DynamicSearch",
			SQL:            "SELECT id FROM items",
			Command:        sqlmodel.QueryCommandMany,
			DynamicRuntime: true,
		},
	}

	file, err := renderPrepared(gogen.NewVocabulary(testRequest("mydb", nil, nil)), preparedRequest("mydb", queries))
	require.NoError(t, err)

	source := string(file.Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "prepared.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "func (prepared *PreparedDBTX) cachedStatement(query string) *sql.Stmt")
	assert.Contains(t, source, "return prepared.stmts[query]")

	cachedStart := strings.Index(source, "func (prepared *PreparedDBTX) cachedStatement(")
	require.GreaterOrEqual(t, cachedStart, 0, "expected cachedStatement in generated source:\n%s", source)
	nextFunc := strings.Index(source[cachedStart+1:], "\nfunc ")
	require.GreaterOrEqual(t, nextFunc, 0)
	cachedBody := source[cachedStart : cachedStart+1+nextFunc]
	assert.Contains(t, cachedBody, "prepared.mu.RLock()")
	assert.Contains(t, cachedBody, "defer prepared.mu.RUnlock()")
	assert.NotContains(t, cachedBody, "PrepareContext", "cachedStatement must not prepare; only Prepare prepares")
	assert.NotContains(t, cachedBody, "prepared.mu.Lock()", "cachedStatement must not take the write lock")

	assert.NotContains(t, source, "getOrPrepare")
	assert.NotContains(t, source, "existingStatement")

	assert.Contains(t, source, "statement := prepared.cachedStatement(query)")
	assert.Contains(t, source, "if statement == nil {")
	assert.Contains(t, source, "return prepared.db.ExecContext(ctx, query, arguments...)")
}

func TestEmitPreparedNoStaticQueries(t *testing.T) {

	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:      "SearchProducts",
			SQL:       "SELECT id FROM products",
			Command:   sqlmodel.QueryCommandMany,
			IsDynamic: true,
		},
	}

	file, err := renderPrepared(gogen.NewVocabulary(testRequest("mydb", nil, nil)), preparedRequest("mydb", queries))
	require.NoError(t, err)

	source := string(file.Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "prepared.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "staticQueries := []string{}")

	assert.NotContains(t, source, "pgx")
}

func TestEmitPreparedExcludesSliceQueries(t *testing.T) {

	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:    "GetUser",
			SQL:     "SELECT id FROM users WHERE id = ?",
			Command: sqlmodel.QueryCommandOne,
		},
		{
			Name:    "FetchByStatuses",
			SQL:     "SELECT id FROM tasks WHERE status IN (?1)",
			Command: sqlmodel.QueryCommandMany,
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "statuses", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, IsSlice: true},
			},
		},
	}

	file, err := renderPrepared(gogen.NewVocabulary(testRequest("mydb", nil, nil)), preparedRequest("mydb", queries))
	require.NoError(t, err)

	source := string(file.Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "prepared.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "getuser")

	assert.NotContains(t, source, "fetchbystatuses")
}

func taggedArrayQuery(name string, sql string) *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:     name,
		Command:  sqlmodel.QueryCommandMany,
		SQL:      sql,
		Filename: "tagged.sql",
		OutputColumns: []sqlmodel.OutputColumn{
			{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			{Name: "tags", SQLType: sqlmodel.SQLType{
				Category:    sqlmodel.TypeCategoryArray,
				ElementType: &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
			}},
		},
	}
}

func TestPostgresEmitterLoudErrorsOnUnwrappableArrayColumn(t *testing.T) {
	t.Parallel()
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{taggedArrayQuery("ListTagged", "SELECT * FROM tagged")}

	_, err := emitQueriesForDialect(t.Context(), emitter, dialect.Wrap(postgres.New()).Info(), "db", queries, defaultMappings())
	require.Error(t, err, "an array column under SELECT * must fail generation loudly, not silently emit a broken scan")
	assert.Contains(t, err.Error(), "tags", "the error must name the offending column")
	assert.Contains(t, err.Error(), "to_json", "the error must point at the wrapping fix")
}

func TestDefaultTypesFollowTheDriverProfile(t *testing.T) {
	t.Parallel()

	defaultTypes := func(emitter *Emitter, info dialect.Facts) sqlmodel.TypeSlots {
		return emitter.DefaultTypes(&info)
	}

	for _, info := range []dialect.Facts{dialect.Wrap(sqlite.New()).Info(), {}} {
		assert.Equal(t, sqlmodel.TypeSlots{}, defaultTypes(New(), info), info.Name)
	}

	text := sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Name: "string"}, Nullable: sqlmodel.GoType{Name: "*string"}}
	assert.Equal(t, text, defaultTypes(New(), dialect.Wrap(postgres.New()).Info()).TimeOfDay, "pgx's stdlib returns a time of day as text")
	assert.Equal(t, text, defaultTypes(New(), dialect.Wrap(mysql.New()).Info()).TimeOfDay, "go-sql-driver returns a time of day as text")

	duckDBSlots := defaultTypes(New(), dialect.Wrap(duckdb.New()).Info())
	assert.Equal(t, sqlmodel.GoType{Package: "math/big", Name: "*Int"}, duckDBSlots.BigInt.NotNull)
	assert.True(t, duckDBSlots.Decimal.IsZero())

	viaDialect := defaultTypes(New(), dialect.Wrap(clickhouse.New()).Info())
	named := defaultTypes(New(WithDriver(driverClickHouse)), dialect.Wrap(postgres.New()).Info())

	assert.Equal(t, viaDialect, named, "a named driver ignores the dialect's drivers")
	assert.Equal(t, sqlmodel.GoType{Package: "github.com/shopspring/decimal", Name: "Decimal"}, named.Decimal.NotNull)
	assert.Equal(t, named.Decimal, named.Money)
	assert.Equal(t, sqlmodel.GoType{Package: "math/big", Name: "*Int"}, named.BigInt.Nullable)
	assert.True(t, defaultTypes(New(WithDriver(driverPgx)), dialect.Wrap(clickhouse.New()).Info()).Decimal.IsZero())
	duration := sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Package: "time", Name: "Duration"}, Nullable: sqlmodel.GoType{Package: "time", Name: "*Duration"}}
	assert.Equal(t, duration, named.TimeOfDay, "clickhouse-go returns a Time or Time64 value as a time.Duration")
}

func TestWithPreparedControlsThePreparedFile(t *testing.T) {
	t.Parallel()

	query := &sqlmodel.AnalysedQuery{
		Name:     "CountTagged",
		Command:  sqlmodel.QueryCommandOne,
		SQL:      "SELECT count(*) AS total FROM tagged",
		Filename: "tagged.sql",
		OutputColumns: []sqlmodel.OutputColumn{
			{Name: "total", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
		},
	}

	names := func(emitter *Emitter) []string {
		request := testRequest("db", []*sqlmodel.AnalysedQuery{query}, defaultMappings())
		request.Catalogue = &sqlmodel.Catalogue{DefaultSchema: "public", Schemas: map[string]*sqlmodel.Schema{}}
		files, err := emitter.Emit(t.Context(), planned(request))
		require.NoError(t, err)
		result := make([]string, 0, len(files))
		for _, file := range files {
			result = append(result, file.Name)
		}
		return result
	}

	assert.Contains(t, names(New()), "prepared.go")
	assert.NotContains(t, names(New(WithPrepared(false))), "prepared.go")
}

func TestClickHouseReadsArraysAndMapsOfUnknownValuesThroughTheAdapter(t *testing.T) {
	t.Parallel()

	unknown := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: "Dynamic"}
	text := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}
	arrayOf := func(element sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "Array", ElementType: &element}
	}
	mapOf := func(value sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, EngineName: "Map", KeyType: &text, ElementType: &value}
	}
	testCases := []struct {
		name    string
		sqlType sqlmodel.SQLType
		want    gogen.ColumnReading
	}{
		{name: "an array of unknown values", sqlType: arrayOf(unknown), want: gogen.ColumnReadConverted},
		{name: "a map to arrays of unknown values", sqlType: mapOf(arrayOf(unknown)), want: gogen.ColumnReadConverted},
		{name: "an unknown value", sqlType: unknown, want: gogen.ColumnReadAsIs},
		{name: "an array of text", sqlType: arrayOf(text), want: gogen.ColumnReadAsIs},
	}
	clickHouse := clickHouseProfile()
	storesItsOwnValues := clickHouseProfile()
	storesItsOwnValues.readsNestedUnknownsInItsOwnTypes = false
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, clickHouse.reading(testCase.sqlType))
			require.Equal(t, gogen.ColumnReadAsIs, storesItsOwnValues.reading(testCase.sqlType),
				"a driver that returns such values in Go's own types reads them as they are")
		})
	}
}

func TestEmitRefusesARequestItCannotReadBeforeCheckingIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		request *emit.Request
		name    string
	}{
		{name: "a nil request", request: nil},
		{name: "a nil query", request: &emit.Request{Package: "store", Queries: []*sqlmodel.AnalysedQuery{nil}, Style: emit.Style{HelperPrefix: "sgl", ErrorPrefix: "sgl"}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files, err := New().Emit(t.Context(), testCase.request)

			require.ErrorIs(t, err, generate.ErrInvalidRequest)
			require.ErrorIs(t, err, emit.ErrRefused, "an invalid request is refused, not failed")
			require.Nil(t, files)
		})
	}
}

func sliceManyQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:     "FetchDueTasks",
		Command:  sqlmodel.QueryCommandMany,
		SQL:      "-- sgl.name: FetchDueTasks\n-- ?1 as sgl.slice(statuses)\nSELECT id, status FROM tasks WHERE status IN (?1)",
		Filename: "tasks.sql",
		Parameters: []sqlmodel.QueryParameter{
			{Number: 1, Name: "statuses", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, IsSlice: true},
		},
		OutputColumns: []sqlmodel.OutputColumn{
			{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			{Name: "status", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
		},
	}
}

func TestEmitQueriesStaticSliceUsesTheDialectsParameterLimit(t *testing.T) {
	emitter := New()
	info := dialect.Wrap(sqlite.New()).Info()
	info.MaxParameters = 4321

	files, err := emitQueriesForDialect(t.Context(), emitter, info, "db", []*sqlmodel.AnalysedQuery{sliceManyQuery()}, defaultMappings())
	require.NoError(t, err)

	bindLimits := findGeneratedFile(t, files, "bind_limits.go")

	assert.Contains(t, bindLimits, "const sglMaxBindVariables = 4321")
}

func TestEmitQueriesClickHouseWrapsParametersAndEmitsValueHelper(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "GetByName",
			Command:  sqlmodel.QueryCommandOne,
			SQL:      "SELECT id FROM users WHERE name = {name:String}",
			Filename: "users.sql",
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			},
		},
	}

	files, err := emitQueriesForDialect(t.Context(), emitter, dialect.Wrap(clickhouse.New()).Info(), "db", queries, defaultMappings())
	require.NoError(t, err)

	querySource := findGeneratedFile(t, files, "users.sql.go")
	helperSource := findGeneratedFile(t, files, "clickhouse_values.go")

	fileSet := token.NewFileSet()
	_, queryParseError := parser.ParseFile(fileSet, "users.sql.go", querySource, parser.AllErrors)
	require.NoError(t, queryParseError, "generated query must be valid Go:\n%s", querySource)
	_, helperParseError := parser.ParseFile(fileSet, "clickhouse_values.go", helperSource, parser.AllErrors)
	require.NoError(t, helperParseError, "generated helper must be valid Go:\n%s", helperSource)

	assert.Contains(t, querySource, "{p_name:String}")
	assert.Contains(t, querySource, `clickhouse.Named("p_name", sglClickHouseShapedValue(name, sglClickHouseShapes[0]))`)
	assert.Contains(t, querySource, `"github.com/ClickHouse/clickhouse-go/v2"`)

	assert.Contains(t, helperSource, "func sglClickHouseValue(value any, shape string) string")
	assert.Contains(t, helperSource, "func sglClickHouseAppendElement(text []byte, value reflect.Value, shape sglClickHouseShape, depth int) ([]byte, error)")
}

func TestEmitQueriesClickHousePrefixesReservedKeywordParamName(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "TopEvents",
			Command:  sqlmodel.QueryCommandMany,
			SQL:      "SELECT id FROM events ORDER BY id LIMIT {limit:Int32}",
			Filename: "events.sql",
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "limit", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "Int32"}},
			},
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
			},
		},
	}

	files, err := emitQueriesForDialect(t.Context(), emitter, dialect.Wrap(clickhouse.New()).Info(), "db", queries, defaultMappings())
	require.NoError(t, err)

	querySource := findGeneratedFile(t, files, "events.sql.go")

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "events.sql.go", querySource, parser.AllErrors)
	require.NoError(t, parseError, "generated query must be valid Go:\n%s", querySource)

	assert.Contains(t, querySource, "{p_limit:Int32}")
	assert.Contains(t, querySource, `clickhouse.Named("p_limit", sglClickHouseShapedValue(limit, sglClickHouseShapes[0]))`)
	assert.NotContains(t, querySource, "{limit:Int32}")
}

func TestEmitQueriesBindsABinaryOverrideAsTheBytesItMarshalsItselfTo(t *testing.T) {
	emitter := New()
	identifier := &sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"}
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "AddToken",
			Command:  sqlmodel.QueryCommandExec,
			SQL:      "INSERT INTO tokens (id, raw, label) VALUES (?, ?, ?)",
			Filename: "tokens.sql",
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryBinary}, GoTypeOverride: identifier},
				{Number: 2, Name: "raw", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryBinary}},
				{Number: 3, Name: "label", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, GoTypeOverride: identifier},
			},
		},
	}

	files, err := emitQueriesForDialect(t.Context(), emitter, dialect.Wrap(mysql.New()).Info(), "db", queries, defaultMappings())
	require.NoError(t, err)

	querySource := findGeneratedFile(t, files, "tokens.sql.go")
	helperSource := findGeneratedFile(t, files, "driver_values.go")
	assert.Contains(t, querySource, "sglDriverBinary{value: params.ID}")
	assert.Contains(t, querySource, "params.Raw,")
	assert.Contains(t, querySource, "params.Label)")
	assert.Contains(t, helperSource, "func (wrapped sglDriverBinary) Value() (driver.Value, error)")
	assert.NotContains(t, helperSource, "sglDriverText")
}
