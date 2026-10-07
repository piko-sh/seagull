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

package clickhouse

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/generate"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	typeCheckPrelude = `package testpkg

import "context"

type sglTestRows struct{}

func (sglTestRows) Close()            {}
func (sglTestRows) Next() bool        { return false }
func (sglTestRows) Scan(...any) error { return nil }
func (sglTestRows) Err() error        { return nil }

type sglTestRow struct{}

func (sglTestRow) Scan(...any) error { return nil }

type sglTestDB struct{}

func (sglTestDB) Query(ctx context.Context, query string, args ...any) (sglTestRows, error) {
	return sglTestRows{}, nil
}

func (sglTestDB) QueryRow(ctx context.Context, query string, args ...any) sglTestRow {
	return sglTestRow{}
}

func (sglTestDB) Exec(ctx context.Context, query string, args ...any) error { return nil }

type Queries struct {
	db sglTestDB
}
`
)

const stubDriverSource = `package clickhouse

type NamedValue struct {
	Name  string
	Value any
}

func Named(name string, value any) NamedValue {
	return NamedValue{Name: name, Value: value}
}
`

type stubDriverImporter struct {
	fileSet  *token.FileSet
	fallback types.Importer
}

func (stub *stubDriverImporter) Import(path string) (*types.Package, error) {
	if path != importClickHouse {
		return stub.fallback.Import(path)
	}
	parsed, err := parser.ParseFile(stub.fileSet, "clickhouse_stub.go", stubDriverSource, 0)
	if err != nil {
		return nil, err
	}
	config := types.Config{Importer: stub.fallback}
	return config.Check(importClickHouse, stub.fileSet, []*ast.File{parsed}, nil)
}

func defaultMappings() sqlmodel.GoTypeResolver {
	return &gotypes.MappingTable{
		Mappings: []gotypes.Mapping{
			{
				SQLCategory: sqlmodel.TypeCategoryInteger,
				NotNull:     sqlmodel.GoType{Name: "int64"},
				Nullable:    sqlmodel.GoType{Name: "*int64"},
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
		},
	}
}

func requireValidGo(t *testing.T, name, source string) {
	t.Helper()
	fileSet := token.NewFileSet()
	_, err := parser.ParseFile(fileSet, name, source, parser.AllErrors)
	require.NoErrorf(t, err, "generated source must be valid Go:\n%s", source)
}

func requireTypeChecks(t *testing.T, files []sqlmodel.GeneratedFile) {
	t.Helper()
	fileSet := token.NewFileSet()

	preludeFile, parseError := parser.ParseFile(fileSet, "prelude_test.go", typeCheckPrelude, parser.AllErrors)
	require.NoError(t, parseError, "type-check prelude must parse")
	astFiles := make([]*ast.File, 0, 1+len(files))
	astFiles = append(astFiles, preludeFile)

	for _, file := range files {
		parsed, fileError := parser.ParseFile(fileSet, file.Name, string(file.Content), parser.AllErrors)
		require.NoErrorf(t, fileError, "generated %s must parse:\n%s", file.Name, file.Content)
		astFiles = append(astFiles, parsed)
	}

	config := types.Config{Importer: &stubDriverImporter{
		fileSet:  fileSet,
		fallback: importer.ForCompiler(fileSet, "source", nil),
	}}
	_, checkError := config.Check("testpkg", fileSet, astFiles, nil)
	require.NoError(t, checkError, "generated files must type-check, not merely parse")
}

func findFile(t *testing.T, files []sqlmodel.GeneratedFile, name string) sqlmodel.GeneratedFile {
	t.Helper()
	for _, file := range files {
		if file.Name == name {
			return file
		}
	}
	require.Failf(t, "generated file not found", "no generated file is named %q", name)
	return sqlmodel.GeneratedFile{Name: "", Content: nil}
}

func TestEmitQuerierShape(t *testing.T) {
	result, err := emitkit.Querier("testpkg", emit.Naming{}, nativeQuerier)
	require.NoError(t, err)
	assert.Equal(t, "querier.go", result.Name)

	source := string(result.Content)
	requireValidGo(t, "querier.go", source)

	assert.Contains(t, source, "DBTX")
	assert.Contains(t, source, "PrepareBatch(")
	assert.Contains(t, source, "driver.Batch")
	assert.Contains(t, source, "driver.Rows")
	assert.Contains(t, source, "driver.Row")
	assert.Contains(t, source, importDriver)

	assert.Contains(t, source, "Exec(ctx context.Context, query string, args ...any) error")

	assert.NotContains(t, source, "database/sql")
	assert.NotContains(t, source, "ExecContext")
	assert.NotContains(t, source, "sql.Result")
	assert.NotContains(t, source, "pgx")
	assert.NotContains(t, source, "SendBatch")
	assert.NotContains(t, source, "CopyFrom")
	assert.NotContains(t, source, "WithTx")
	assert.NotContains(t, source, "RunInTx")
}

func TestEmitQueriesExecSingleValue(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "delete_event",
			SQL:      "DELETE FROM events WHERE id = {id:Int64}",
			Command:  sqlmodel.QueryCommandExec,
			Filename: "events.sql",
			Parameters: []sqlmodel.QueryParameter{
				{Name: "id", Number: 1, SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "Int64"}},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "testpkg", queries, defaultMappings())
	require.NoError(t, err)
	require.NotEmpty(t, files)

	source := string(files[0].Content)
	requireValidGo(t, "events.sql.go", source)

	assert.Contains(t, source, "(ctx context.Context, id int64) error")
	assert.Contains(t, source, "DELETE FROM events WHERE id = {p_id:Int64}")
	assert.Contains(t, source, `return queries.db.Exec(ctx, deleteEventSQL, clickhouse.Named("p_id", sglClickHouseShapedValue(id, sglClickHouseShapes[0])))`)
	assert.NotContains(t, source, "_, err := queries.db.Exec(")
	assert.NotContains(t, source, "clickhouse_format")
}

func TestEmitNamesClickHouseNamedByTheQualifierTheFileImportsTheDriverUnder(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		packageNames map[string]string
		name         string
		wantCall     string
		wantImport   string
	}{
		{name: "the package's own name", packageNames: nil, wantCall: `clickhouse.Named("p_id"`, wantImport: `"github.com/ClickHouse/clickhouse-go/v2"`},
		{name: "a name the style gives", packageNames: map[string]string{importClickHouse: "ch"}, wantCall: `ch.Named("p_id"`, wantImport: `ch "github.com/ClickHouse/clickhouse-go/v2"`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			queries := []*sqlmodel.AnalysedQuery{{
				Name: "delete_event", SQL: "DELETE FROM events WHERE id = {id:Int64}", Command: sqlmodel.QueryCommandExec, Filename: "events.sql",
				Parameters: []sqlmodel.QueryParameter{{Name: "id", Number: 1, SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "Int64"}}},
			}}
			request := testRequest("testpkg", queries, defaultMappings())
			request.Style.PackageNames = testCase.packageNames
			request = planned(request)

			files, err := generate.Package(t.Context(), request, newNativeDriver(request).driver())
			require.NoError(t, err)

			index := slices.IndexFunc(files, func(file sqlmodel.GeneratedFile) bool { return file.Name == "events.sql.go" })
			require.GreaterOrEqual(t, index, 0)
			source := string(files[index].Content)
			requireValidGo(t, "events.sql.go", source)
			assert.Contains(t, source, testCase.wantCall)
			assert.Contains(t, source, testCase.wantImport)
		})
	}
}

func TestCheckRefusesCommandsTheNativeDriverCannotHonour(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		want    string
		command sqlmodel.QueryCommand
	}{
		{name: "execrows", command: sqlmodel.QueryCommandExecRows, want: "command: execrows returns the number of rows the statement changed, which the native ClickHouse driver does not report, since its Exec returns only an error; use command: exec"},
		{name: "execresult", command: sqlmodel.QueryCommandExecResult, want: "command: execresult returns the sql.Result counting the rows the statement changed, which the native ClickHouse driver does not report, since its Exec returns only an error; use command: exec"},
		{name: "exec", command: sqlmodel.QueryCommandExec, want: ""},
		{name: "many", command: sqlmodel.QueryCommandMany, want: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{Name: "Purge", Command: testCase.command, Filename: "events.sql", Line: 3}
			problems := New().Check(testRequest("testpkg", []*sqlmodel.AnalysedQuery{query}, defaultMappings()))

			if testCase.want == "" {
				require.Empty(t, problems)
				return
			}
			require.Equal(t, []emit.Problem{{Query: query, Code: diag.CodeEmitterUnsupported, Message: testCase.want, Severity: diag.SeverityError}}, problems)
		})
	}
}

func TestCheckRefusesABatchRowWiderThanOneStatement(t *testing.T) {
	t.Parallel()

	parameters := []sqlmodel.QueryParameter{
		{Number: 1, Name: "id", SQLType: sqlmodel.SQLType{EngineName: "UInt64", Category: sqlmodel.TypeCategoryInteger, Width: 64, Unsigned: true}},
		{Number: 2, Name: "host", SQLType: sqlmodel.SQLType{EngineName: "String", Category: sqlmodel.TypeCategoryText}},
	}
	text := func(value string) sqlmodel.SQLSegment {
		return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentText, Text: value}
	}
	placeholder := func(index int, typeText string) sqlmodel.SQLSegment {
		return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentParameter, Parameter: index, TypeText: typeText}
	}
	insert := func(row ...sqlmodel.SQLSegment) []sqlmodel.SQLSegment {
		return []sqlmodel.SQLSegment{text("INSERT INTO events (id, host) VALUES "), {Kind: sqlmodel.SegmentValuesRow, Children: row}}
	}
	expressions := insert(text("("), placeholder(0, "UInt64"), text(", lower("), placeholder(1, "String"), text("))"))
	lone := insert(text("("), placeholder(0, "UInt64"), text(", "), placeholder(1, "String"), text(")"))

	testCases := []struct {
		name          string
		segments      []sqlmodel.SQLSegment
		maxParameters int
		want          string
	}{
		{name: "a repeated row within the limit", segments: expressions, maxParameters: 2},
		{name: "a repeated row past the limit", segments: expressions, maxParameters: 1,
			want: "command: batch binds 2 variables in each VALUES row, more than the 1 one statement of the driver binds, " +
				"so not even one row could be sent; insert fewer columns, or write the values every row shares into the SQL"},
		{name: "a row appended as columns binds nothing", segments: lone, maxParameters: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{
				Name: "InsertEvents", Command: sqlmodel.QueryCommandBatch, Filename: "events.sql", Line: 1,
				Parameters: parameters, Template: sqlmodel.SQLTemplate{Segments: testCase.segments},
			}
			request := testRequest("testpkg", []*sqlmodel.AnalysedQuery{query}, defaultMappings())
			request.Dialect.MaxParameters = testCase.maxParameters
			problems := New().Check(request)

			if testCase.want == "" {
				require.Empty(t, problems)
				return
			}
			require.Equal(t, []emit.Problem{{Query: query, Code: diag.CodeBatchShape, Message: testCase.want, Severity: diag.SeverityError}}, problems)
		})
	}
}

func TestEmitQueriesManyReads(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "list_events",
			SQL:      "SELECT host_id, kind FROM events WHERE host_id = {host_id:String}",
			Command:  sqlmodel.QueryCommandMany,
			ReadOnly: true,
			Filename: "events.sql",
			Parameters: []sqlmodel.QueryParameter{
				{Name: "host_id", Number: 1, SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
			},
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "host_id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
				{Name: "kind", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "testpkg", queries, defaultMappings())
	require.NoError(t, err)
	require.NotEmpty(t, files)

	source := string(files[0].Content)
	requireValidGo(t, "events.sql.go", source)
	assert.Contains(t, source, `queries.db.Query(ctx, listEventsSQL, clickhouse.Named("p_host_id", sglClickHouseShapedValue(hostID, sglClickHouseShapes[0])))`)
	assert.Contains(t, source, "rows.Scan(")
	assert.NotContains(t, source, "database/sql")
	assert.NotContains(t, source, "clickhouse_format")
}

func TestEmitQueriesOptionalOne(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "GetEvent",
			SQL:      "SELECT host_id, kind FROM events WHERE id = ?",
			Command:  sqlmodel.QueryCommandOne,
			Optional: true,
			Filename: "events.sql",
			Parameters: []sqlmodel.QueryParameter{
				{Name: "id", Number: 1, SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "Int64"}},
			},
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "host_id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
				{Name: "kind", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "testpkg", queries, defaultMappings())
	require.NoError(t, err)
	require.NotEmpty(t, files)

	queryFile := findFile(t, files, "events.sql.go")
	source := string(queryFile.Content)
	requireValidGo(t, queryFile.Name, source)

	assert.Contains(t, source, "sql.ErrNoRows",
		"ClickHouse optional one query must test the database/sql no-rows sentinel")
	assert.Contains(t, source, `"database/sql"`,
		"ClickHouse optional one query must import database/sql for sql.ErrNoRows")
	assert.Contains(t, source, "errors.Is",
		"optional one query must distinguish no rows from a real error via errors.Is")
	assert.Contains(t, source, "(GetEventRow, bool, error)",
		"optional one query must widen the return signature to (row, bool, error)")

	requireTypeChecks(t, files)
}

func TestEmitWritesNoPreparedOrEmptyFiles(t *testing.T) {
	files, err := New().Emit(t.Context(), testRequest("testpkg", nil, defaultMappings()))
	require.NoError(t, err)

	for _, file := range files {
		assert.NotEmpty(t, file.Name)
		assert.NotEqual(t, "prepared.go", file.Name)
	}
}

func TestEmitRejectsUnsupportedCommandsBeforeGenerating(t *testing.T) {
	request := testRequest("testpkg", []*sqlmodel.AnalysedQuery{{Name: "Purge", Command: sqlmodel.QueryCommandExecRows}}, defaultMappings())

	files, err := New().Emit(t.Context(), request)

	require.ErrorContains(t, err, "emitting queries for testpkg")
	require.ErrorContains(t, err, "query Purge: Q059 command: execrows")
	assert.Nil(t, files)
}

func TestDefaultTypesOverlayNativeDriverTypes(t *testing.T) {
	slots := New().DefaultTypes(&dialect.Facts{Name: "clickhouse", Family: "clickhouse"})

	assert.Equal(t, "github.com/shopspring/decimal", slots.Decimal.NotNull.Package)
	assert.Equal(t, "*Decimal", slots.Decimal.Nullable.Name)
	assert.Equal(t, slots.Decimal, slots.Money)
	assert.Equal(t, sqlmodel.GoType{Package: "math/big", Name: "*Int"}, slots.BigInt.NotNull)
	assert.True(t, slots.JSON.IsZero())
}

func TestTheNativeDriverReadsATimeOfDayAsADuration(t *testing.T) {
	t.Parallel()

	slots := New().DefaultTypes(&dialect.Facts{Name: "clickhouse", Family: "clickhouse"})

	require.Equal(t, sqlmodel.GoType{Package: "time", Name: "Duration"}, slots.TimeOfDay.NotNull)
	require.Equal(t, sqlmodel.GoType{Package: "time", Name: "*Duration"}, slots.TimeOfDay.Nullable)
}

func dynamicSortableQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:      "ListEvents",
		Filename:  "list_events.sql",
		SQL:       "SELECT host_id, kind FROM events WHERE host_id = ? ORDER BY created_at LIMIT ?",
		Command:   sqlmodel.QueryCommandMany,
		IsDynamic: true,
		ReadOnly:  true,
		Parameters: []sqlmodel.QueryParameter{
			{
				Name:    "host_id",
				Number:  1,
				SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"},
				Kind:    sqlmodel.ParameterDirectiveParam,
			},
			{
				Name:         "page_size",
				Number:       2,
				SQLType:      sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "Int64"},
				Context:      sqlmodel.ParameterContextLimit,
				DefaultLimit: new(20),
				MaxLimit:     new(100),
			},
			{
				Name:            "sort",
				Number:          3,
				SQLType:         sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"},
				Kind:            sqlmodel.ParameterDirectiveSortable,
				SortableColumns: []string{"created_at", "kind"},
			},
		},
		OutputColumns: []sqlmodel.OutputColumn{
			{Name: "host_id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
			{Name: "kind", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
		},
	}
}

func TestClickHouseQueriesTypeCheck(t *testing.T) {
	tests := []struct {
		name    string
		queries []*sqlmodel.AnalysedQuery
	}{
		{
			name: "one",
			queries: []*sqlmodel.AnalysedQuery{
				{
					Name:     "GetEvent",
					SQL:      "SELECT host_id, kind FROM events WHERE id = ?",
					Command:  sqlmodel.QueryCommandOne,
					Filename: "events.sql",
					Parameters: []sqlmodel.QueryParameter{
						{Name: "id", Number: 1, SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "Int64"}},
					},
					OutputColumns: []sqlmodel.OutputColumn{
						{Name: "host_id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
						{Name: "kind", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
					},
				},
			},
		},
		{
			name: "many",
			queries: []*sqlmodel.AnalysedQuery{
				{
					Name:     "ListEventsStatic",
					SQL:      "SELECT host_id, kind FROM events WHERE host_id = ?",
					Command:  sqlmodel.QueryCommandMany,
					Filename: "events.sql",
					Parameters: []sqlmodel.QueryParameter{
						{Name: "host_id", Number: 1, SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
					},
					OutputColumns: []sqlmodel.OutputColumn{
						{Name: "host_id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
						{Name: "kind", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}},
					},
				},
			},
		},
	}

	emitter := New()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files, err := emitQueriesFor(t.Context(), emitter, "testpkg", test.queries, defaultMappings())
			require.NoError(t, err)
			requireTypeChecks(t, files)
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

func TestCheckRefusesAValueWhoseTypeNestsDeeperThanGeneratedCodeFollows(t *testing.T) {
	t.Parallel()

	nested := func(levels int) sqlmodel.SQLType {
		sqlType := sqlmodel.SQLType{EngineName: "IPv4", Category: sqlmodel.TypeCategoryNetwork, Network: sqlmodel.NetworkKindAddress, Width: 32}
		for range levels {
			sqlType = sqlmodel.SQLType{EngineName: "Array", Category: sqlmodel.TypeCategoryArray, ElementType: new(sqlType)}
		}
		return sqlType
	}

	testCases := []struct {
		name    string
		sqlType sqlmodel.SQLType
		want    []emit.Problem
	}{
		{name: "a parameter as deep as generated code follows", sqlType: nested(sqlmodel.MaxTypeDepth - 1)},
		{name: "a parameter deeper than generated code follows", sqlType: nested(sqlmodel.MaxTypeDepth), want: []emit.Problem{{
			Code: diag.CodeEmitterUnsupported,
			Message: "parameter hosts has a type nesting 64 or more levels of elements, keys, fields or members, deeper than " +
				"generated code follows; flatten the type",
			Severity: diag.SeverityError,
		}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{
				Name: "PurgeHosts", Command: sqlmodel.QueryCommandExec, Filename: "events.sql",
				Parameters: []sqlmodel.QueryParameter{{Number: 1, Name: "hosts", SQLType: testCase.sqlType}},
			}
			problems := New().Check(testRequest("testpkg", []*sqlmodel.AnalysedQuery{query}, defaultMappings()))

			for index := range testCase.want {
				testCase.want[index].Query = query
			}
			require.Equal(t, testCase.want, problems)
		})
	}
}

func dynamicOptionalExecQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:      "ArchiveEvents",
		Filename:  "archive_events.sql",
		SQL:       "INSERT INTO archive SELECT * FROM events WHERE ({kind:Nullable(String)} IS NULL OR kind = {kind:Nullable(String)}) AND host_id = {host_id:String}",
		Command:   sqlmodel.QueryCommandExec,
		IsDynamic: true,
		Parameters: []sqlmodel.QueryParameter{
			{
				Name:       "kind",
				Number:     1,
				SQLType:    sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"},
				Kind:       sqlmodel.ParameterDirectiveParam,
				IsOptional: true,
			},
			{
				Name:    "host_id",
				Number:  2,
				SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"},
				Kind:    sqlmodel.ParameterDirectiveParam,
			},
		},
	}
}

func dynamicSortableExecQuery() *sqlmodel.AnalysedQuery {
	query := dynamicSortableQuery()
	query.Name = "ArchiveRecentEvents"
	query.Filename = "archive_recent_events.sql"
	query.SQL = "INSERT INTO archive SELECT * FROM events WHERE host_id = ? ORDER BY created_at LIMIT ?"
	query.Command = sqlmodel.QueryCommandExec
	query.ReadOnly = false
	query.OutputColumns = nil
	return query
}

func TestClickHouseDynamicExecReturnsOnlyTheError(t *testing.T) {
	testCases := []struct {
		name      string
		query     *sqlmodel.AnalysedQuery
		file      string
		signature string
	}{
		{
			name:      "optional parameters",
			query:     dynamicOptionalExecQuery(),
			file:      "archive_events.sql.go",
			signature: "func (queries *Queries) ArchiveEvents(ctx context.Context, params ArchiveEventsParams) error {",
		},
		{
			name:      "sortable",
			query:     dynamicSortableExecQuery(),
			file:      "archive_recent_events.sql.go",
			signature: "func (queries *Queries) ArchiveRecentEvents(ctx context.Context, params ArchiveRecentEventsParams) error {",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			files, err := emitQueriesFor(t.Context(), New(), "testpkg", []*sqlmodel.AnalysedQuery{testCase.query}, defaultMappings())
			require.NoError(t, err)
			requireTypeChecks(t, files)

			source := string(findFile(t, files, testCase.file).Content)
			require.Contains(t, source, testCase.signature)
			require.NotContains(t, source, "RowsAffected")
			require.NotContains(t, source, "sql.Result")
		})
	}
}
