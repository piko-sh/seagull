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

package pgx

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	config := types.Config{Importer: importer.ForCompiler(fileSet, "source", nil)}
	_, checkError := config.Check("testpkg", fileSet, astFiles, nil)
	require.NoError(t, checkError, "generated files must type-check, not merely parse")
}

func TestPgxEmitQuerier(t *testing.T) {
	result, err := emitkit.Querier("testpkg", emit.Naming{}, pgxQuerier)
	require.NoError(t, err)

	source := string(result.Content)
	assert.Equal(t, "querier.go", result.Name)

	requireValidGo(t, "querier.go", source)

	assert.Contains(t, source, "DBTX")
	assert.Contains(t, source, "Exec")
	assert.Contains(t, source, "Query")
	assert.Contains(t, source, "QueryRow")
	assert.Contains(t, source, "SendBatch")
	assert.Contains(t, source, "CopyFrom")
}

func TestPgxEmitQuerierNoDatabaseSQL(t *testing.T) {
	result, err := emitkit.Querier("testpkg", emit.Naming{}, pgxQuerier)
	require.NoError(t, err)

	source := string(result.Content)

	assert.NotContains(t, source, "ExecContext")
	assert.NotContains(t, source, "QueryContext")
	assert.NotContains(t, source, "QueryRowContext")
	assert.NotContains(t, source, "sql.Result")
	assert.NotContains(t, source, "sql.Row")
	assert.NotContains(t, source, "sql.Rows")
	assert.NotContains(t, source, "database/sql")
}

func TestPgxEmitQueriesOne(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "get_user",
			SQL:      "SELECT id, name FROM users WHERE id = $1",
			Command:  sqlmodel.QueryCommandOne,
			Filename: "users.sql",
			Parameters: []sqlmodel.QueryParameter{
				{
					Name:    "id",
					Number:  1,
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint"},
				},
			},
			OutputColumns: []sqlmodel.OutputColumn{
				{
					Name:    "id",
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint"},
				},
				{
					Name:    "name",
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"},
				},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "testpkg", queries, defaultMappings())
	require.NoError(t, err)
	require.NotEmpty(t, files)

	source := string(files[0].Content)

	requireValidGo(t, "users.sql.go", source)

	assert.Contains(t, source, "QueryRow")
	assert.Contains(t, source, "Scan")
}

func TestPgxEmitQueriesMany(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "list_users",
			SQL:      "SELECT id, name FROM users",
			Command:  sqlmodel.QueryCommandMany,
			Filename: "users.sql",
			OutputColumns: []sqlmodel.OutputColumn{
				{
					Name:    "id",
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint"},
				},
				{
					Name:    "name",
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"},
				},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "testpkg", queries, defaultMappings())
	require.NoError(t, err)
	require.NotEmpty(t, files)

	source := string(files[0].Content)

	requireValidGo(t, "users.sql.go", source)

	assert.Contains(t, source, "Query")
	assert.Contains(t, source, "for")
}

func TestPgxEmitQueriesExec(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "delete_user",
			SQL:      "DELETE FROM users WHERE id = $1",
			Command:  sqlmodel.QueryCommandExec,
			Filename: "users.sql",
			Parameters: []sqlmodel.QueryParameter{
				{
					Name:    "id",
					Number:  1,
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint"},
				},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "testpkg", queries, defaultMappings())
	require.NoError(t, err)
	require.NotEmpty(t, files)

	source := string(files[0].Content)

	requireValidGo(t, "users.sql.go", source)

	assert.Contains(t, source, "Exec")
}

func TestPgxEmitQueriesExecRows(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "delete_inactive_users",
			SQL:      "DELETE FROM users WHERE active = false",
			Command:  sqlmodel.QueryCommandExecRows,
			Filename: "users.sql",
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "testpkg", queries, defaultMappings())
	require.NoError(t, err)
	require.NotEmpty(t, files)

	source := string(files[0].Content)

	requireValidGo(t, "users.sql.go", source)

	assert.Contains(t, source, "func (queries *Queries)")
	assert.Contains(t, source, "Exec(")
	assert.NotContains(t, source, "ExecContext")
	assert.Contains(t, source, "RowsAffected()")
	assert.Contains(t, source, "(int64, error)")
	assert.NotContains(t, source, "sql.Result")
}

func TestPgxEmitQueriesExecResult(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "update_user_email",
			SQL:      "UPDATE users SET email = $1 WHERE id = $2",
			Command:  sqlmodel.QueryCommandExecResult,
			Filename: "users.sql",
			Parameters: []sqlmodel.QueryParameter{
				{
					Name:    "email",
					Number:  1,
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"},
				},
				{
					Name:    "id",
					Number:  2,
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint"},
				},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "testpkg", queries, defaultMappings())
	require.NoError(t, err)
	require.NotEmpty(t, files)

	source := string(files[0].Content)

	requireValidGo(t, "users.sql.go", source)

	assert.Contains(t, source, "CommandTag")
	assert.NotContains(t, source, "sql.Result")
}

func TestPgxEmitQueriesStream(t *testing.T) {
	emitter := New()
	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:     "stream_users",
			SQL:      "SELECT id, name FROM users",
			Command:  sqlmodel.QueryCommandStream,
			Filename: "users.sql",
			OutputColumns: []sqlmodel.OutputColumn{
				{
					Name:    "id",
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint"},
				},
				{
					Name:    "name",
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"},
				},
			},
		},
	}

	files, err := emitQueriesFor(t.Context(), emitter, "testpkg", queries, defaultMappings())
	require.NoError(t, err)
	require.NotEmpty(t, files)

	source := string(files[0].Content)

	requireValidGo(t, "users.sql.go", source)

	assert.Contains(t, source, "yield")
	assert.Contains(t, source, "func(")
}

func TestPgxEmitWritesNoPreparedOrEmptyFiles(t *testing.T) {
	files, err := New().Emit(t.Context(), testRequest("testpkg", nil, defaultMappings()))
	require.NoError(t, err)

	for _, file := range files {
		assert.NotEmpty(t, file.Name)
		assert.NotEqual(t, "prepared.go", file.Name)
		assert.True(t, strings.HasPrefix(string(file.Content), "// Code generated by sgl. DO NOT EDIT.\n\n"))
	}
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

func TestPgxQueriesTypeCheck(t *testing.T) {
	tests := []struct {
		name    string
		queries []*sqlmodel.AnalysedQuery
	}{
		{
			name: "one",
			queries: []*sqlmodel.AnalysedQuery{
				{
					Name:     "GetUser",
					SQL:      "SELECT id, name FROM users WHERE id = $1",
					Command:  sqlmodel.QueryCommandOne,
					Filename: "users.sql",
					Parameters: []sqlmodel.QueryParameter{
						{Name: "id", Number: 1, SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint"}},
					},
					OutputColumns: []sqlmodel.OutputColumn{
						{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint"}},
						{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}},
					},
				},
			},
		},
		{
			name: "many",
			queries: []*sqlmodel.AnalysedQuery{
				{
					Name:     "ListUsers",
					SQL:      "SELECT id, name FROM users",
					Command:  sqlmodel.QueryCommandMany,
					Filename: "users.sql",
					OutputColumns: []sqlmodel.OutputColumn{
						{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, EngineName: "bigint"}},
						{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}},
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
