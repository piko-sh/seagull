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
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

const preparedTxContractPrelude = `package testpkg

var (
	_ dbtxWrapper = (*PreparedDBTX)(nil)
	_ DBTX        = (*PreparedDBTX)(nil)
	_ DBTX        = (*preparedTx)(nil)
)
`

func TestPreparedListsTheStaticQueriesInThePlansOrder(t *testing.T) {
	t.Parallel()

	queries := []*sqlmodel.AnalysedQuery{
		{Name: "ListUsers", Filename: "users.sql", Command: sqlmodel.QueryCommandMany},
		{Name: "GetUser", Filename: "users.sql", Command: sqlmodel.QueryCommandOne},
		{Name: "InsertUsers", Filename: "users.sql", Command: sqlmodel.QueryCommandBatch},
		{Name: "ListAudits", Filename: "audit.sql", Command: sqlmodel.QueryCommandMany},
	}
	request := testRequest("db", queries, nil)
	request.Naming.Queries = []emit.QueryNaming{
		{SQLConstant: "listUsersSQL"}, {SQLConstant: "getUserSQL"}, {SQLConstant: "insertUsersSQL"}, {SQLConstant: "listAuditsSQL"},
	}

	file, err := renderPrepared(gogen.NewVocabulary(request), request)
	require.NoError(t, err)

	require.Contains(t, string(file.Content), "staticQueries := []string{listAuditsSQL, getUserSQL, listUsersSQL}",
		"the constants come from the naming plan, by file and then by name, without the batch")
}

func TestPreparedPlansTheNamesOfARequestWithoutAPlan(t *testing.T) {
	t.Parallel()

	request := testRequest("db", []*sqlmodel.AnalysedQuery{{Name: "GetUser", Filename: "users.sql", Command: sqlmodel.QueryCommandOne}}, nil)

	constants := staticConstants(request)

	require.Len(t, constants, 1)
	require.NotEmpty(t, constants[0])
}

func TestEmitOmitsPreparedForClickHouse(t *testing.T) {
	query := &sqlmodel.AnalysedQuery{
		Name:     "CountEvents",
		Command:  sqlmodel.QueryCommandOne,
		SQL:      "SELECT count() AS total FROM events",
		Filename: "events.sql",
		OutputColumns: []sqlmodel.OutputColumn{
			{Name: "total", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
		},
	}

	testCases := []struct {
		emitter *Emitter
		name    string
		refused bool
	}{
		{name: "default", emitter: New()},
		{name: "driver named", emitter: New(WithDriver(driverClickHouse))},
		{name: "prepared refused", emitter: New(WithPrepared(false))},
		{name: "prepared asked", emitter: New(WithPrepared(true)), refused: true},
		{name: "named and asked", emitter: New(WithDriver(driverClickHouse), WithPrepared(true)), refused: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			files, err := emitClickHouseRequest(t, testCase.emitter, query)
			if testCase.refused {
				require.ErrorContains(t, err, "WithPrepared(true) asks for prepared.go")
				return
			}
			require.NoError(t, err)
			require.NotContains(t, fileNames(files), "prepared.go",
				"clickhouse-go prepares only INSERT statements, so a PreparedDBTX cannot start")
		})
	}
}

func TestEmitPreparedLeavesOutBatchAndCopyFromQueries(t *testing.T) {
	queries := []*sqlmodel.AnalysedQuery{
		{Name: "GetEvent", Command: sqlmodel.QueryCommandOne, SQL: "SELECT id FROM events WHERE id = ?1"},
		{Name: "InsertEvents", Command: sqlmodel.QueryCommandBatch, SQL: "INSERT INTO events (name) VALUES (?1)"},
		{Name: "CopyEvents", Command: sqlmodel.QueryCommandCopyFrom, SQL: "INSERT INTO events (name) VALUES (?1)"},
	}

	file, err := renderPrepared(gogen.NewVocabulary(testRequest("db", nil, nil)), preparedRequest("db", queries))
	require.NoError(t, err)

	source := string(file.Content)
	require.Contains(t, source, "staticQueries := []string{getevent}")
	require.NotContains(t, source, "insertevents")
	require.NotContains(t, source, "copyevents")
}

func TestEmitPreparedExcludesCopyFromQueries(t *testing.T) {
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:    "GetUserByID",
			Command: sqlmodel.QueryCommandOne,
			SQL:     "SELECT id FROM users WHERE id = $1",
		},
		{
			Name:    "BulkInsertEvents",
			Command: sqlmodel.QueryCommandCopyFrom,
			SQL:     "INSERT INTO events (name) VALUES (?)",
			Parameters: []sqlmodel.QueryParameter{
				{Number: 1, Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
		},
	}

	file, err := renderPrepared(gogen.NewVocabulary(testRequest("mydb", nil, nil)), preparedRequest("mydb", queries))
	require.NoError(t, err)

	source := string(file.Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "prepared.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated code must be valid Go:\n%s", source)

	assert.Contains(t, source, "getuserbyid")
	assert.NotContains(t, source, "bulkinsertevents")
}

func TestEmitPreparedWrapsTransactions(t *testing.T) {
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "ListUsers",
			Filename: "users.sql",
			SQL:      "SELECT id FROM users",
			Command:  sqlmodel.QueryCommandMany,
			OutputColumns: []sqlmodel.OutputColumn{
				{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			},
		},
	}
	request := testRequest("testpkg", queries, &gotypes.MappingTable{Mappings: []gotypes.Mapping{
		{
			SQLCategory: sqlmodel.TypeCategoryText,
			NotNull:     sqlmodel.GoType{Name: "string"},
			Nullable:    sqlmodel.GoType{Name: "*string"},
		},
	}})

	files, err := New().Emit(t.Context(), planned(request))
	require.NoError(t, err)

	var prepared string
	fileSet := token.NewFileSet()
	prelude, err := parser.ParseFile(fileSet, "prelude.go", preparedTxContractPrelude, parser.AllErrors)
	require.NoError(t, err)
	astFiles := make([]*ast.File, 0, 1+len(files))
	astFiles = append(astFiles, prelude)
	for _, file := range files {
		if file.Name == "prepared.go" {
			prepared = string(file.Content)
		}
		parsed, parseError := parser.ParseFile(fileSet, file.Name, file.Content, parser.AllErrors)
		require.NoErrorf(t, parseError, "generated %s must parse:\n%s", file.Name, file.Content)
		astFiles = append(astFiles, parsed)
	}
	require.NotEmpty(t, prepared)

	config := types.Config{Importer: importer.ForCompiler(fileSet, "source", nil)}
	_, err = config.Check("testpkg", fileSet, astFiles, nil)
	require.NoError(t, err, "PreparedDBTX and its transaction view must satisfy DBTX and dbtxWrapper")

	require.Contains(t, prepared, "func (prepared *PreparedDBTX) WrapDBTX(inner any) any {")
	require.Contains(t, prepared, "transaction, ok := inner.(*sql.Tx)")
	require.Contains(t, prepared, "return &preparedTx{source: prepared, transaction: transaction}")
	for _, method := range []string{"ExecContext", "QueryContext", "QueryRowContext"} {
		require.Contains(t, prepared, "func (bound *preparedTx) "+method+"(ctx context.Context, query string, arguments ...any)")
		require.Contains(t, prepared, "return bound.transaction.StmtContext(ctx, statement)."+method+"(ctx, arguments...)")
		require.Contains(t, prepared, "return bound.transaction."+method+"(ctx, query, arguments...)")
	}
}
