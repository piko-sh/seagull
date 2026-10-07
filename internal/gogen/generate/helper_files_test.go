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

package generate

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

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

type rowBatch struct {
	repeats bool
}

func (rowBatch) Batch(*gogen.Query) (ast.Decl, error) {
	return nil, nil
}

func (rowBatch) CopyFrom(*gogen.Query) (ast.Decl, error) {
	return nil, nil
}

func (batch rowBatch) RepeatsRow(*gogen.Query) bool {
	return batch.repeats
}

func (batch rowBatch) CopyStatement(*gogen.Query) gogen.CopyStatement {
	if batch.repeats {
		return gogen.CopyStatementRepeatsRow
	}
	return gogen.CopyStatementAsIs
}

type copyBatch struct {
	rowBatch
	statement gogen.CopyStatement
}

func (batch copyBatch) CopyStatement(*gogen.Query) gogen.CopyStatement {
	return batch.statement
}

func TestEmitQueryNames_StaticQueries(t *testing.T) {
	t.Parallel()

	queries := []*sqlmodel.AnalysedQuery{
		{Name: "list_tasks", SQL: "SELECT id, name FROM tasks"},
		{Name: "create_task", SQL: "INSERT INTO tasks (name) VALUES ($1)"},
		{Name: "get_task_by_id", SQL: "SELECT id, name FROM tasks WHERE id = $1"},
	}

	file, err := queryNamesFor(t.Context(), queries)
	require.NoError(t, err)
	assert.Equal(t, "query_names.go", file.Name)

	source := string(file.Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "query_names.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated query_names.go must be valid Go:\n%s", source)

	assert.Contains(t, source, "queryNameMap")
	assert.Contains(t, source, "func QueryName(")
	assert.Contains(t, source, `"ListTasks"`)
	assert.Contains(t, source, `"CreateTask"`)
	assert.Contains(t, source, `"GetTaskByID"`)
}

func TestEmitQueryNames_IncludesDynamicQueries(t *testing.T) {
	t.Parallel()

	queries := []*sqlmodel.AnalysedQuery{
		{Name: "list_tasks", SQL: "SELECT id FROM tasks"},
		{Name: "search_tasks", IsDynamic: true, SQL: "SELECT id FROM tasks WHERE name LIKE $1"},
		{Name: "find_tasks", DynamicRuntime: true, SQL: "SELECT id FROM tasks WHERE status = $1"},
	}

	file, err := queryNamesFor(t.Context(), queries)
	require.NoError(t, err)

	source := string(file.Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "query_names.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated query_names.go must be valid Go:\n%s", source)

	assert.Contains(t, source, `"ListTasks"`)
	assert.Contains(t, source, `"SearchTasks"`)
	assert.Contains(t, source, `"FindTasks"`)
}

func TestEmitQueryNames_NamesACopyFromQueryByTheSQLItsMethodRuns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		batch      gogen.BatchBuilder
		name       string
		wantExact  bool
		wantPrefix bool
	}{
		{name: "a method that repeats the VALUES row runs the SQL edited from the row on", batch: copyBatch{statement: gogen.CopyStatementRepeatsRow}, wantExact: true, wantPrefix: true},
		{name: "a method that prepares a batch from the SQL runs it as it is", batch: copyBatch{statement: gogen.CopyStatementAsIs}, wantExact: true},
		{name: "a method that streams through a copy protocol runs no SQL text", batch: copyBatch{statement: gogen.CopyStatementNone}},
		{name: "a driver that runs a copy as a plain statement runs the SQL as it is", batch: nil, wantExact: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			driver := testDriver()
			driver.Batch = test.batch
			emission := testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), textMappings(), driver)
			emission.Request.Package = "generated"
			emission.Request.Style.QueryNames = emit.QueryNames{Enabled: true, File: "query_names.go", Function: "QueryName"}
			emission.Request.Queries = []*sqlmodel.AnalysedQuery{{
				Name: "import_tasks", Filename: "tasks.sql", Command: sqlmodel.QueryCommandCopyFrom,
				Parameters: []sqlmodel.QueryParameter{{Name: "name", Number: 1, SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}},
				SQL:        "INSERT INTO tasks (name) VALUES ($1)",
				Template: template(text("INSERT INTO tasks (name) VALUES "),
					sqlmodel.SQLSegment{Kind: sqlmodel.SegmentValuesRow, Children: []sqlmodel.SQLSegment{text("("), parameter(0), text(")")}}),
			}}

			file, err := queryNamesFile(t.Context(), emission)
			require.NoError(t, err)

			source := string(file.Content)
			require.Equal(t, test.wantExact, strings.Contains(source, `importTasksSQL: "ImportTasks"`), source)
			require.Equal(t, test.wantPrefix, strings.Contains(source, `var queryNamePrefixes = []string{importTasksSQL[:32]}`), source)
		})
	}
}

func TestEmitQueryNames_NamesEachOfTwoQueriesWrittenAlike(t *testing.T) {
	t.Parallel()

	queries := []*sqlmodel.AnalysedQuery{
		{Name: "get_one_task", Command: sqlmodel.QueryCommandOne, SQL: "SELECT id, name FROM tasks"},
		{Name: "list_tasks", Command: sqlmodel.QueryCommandMany, SQL: "SELECT id, name FROM tasks"},
	}

	file, err := queryNamesFor(t.Context(), queries)
	require.NoError(t, err)

	source := string(file.Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "query_names.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated query_names.go must be valid Go:\n%s", source)

	require.Equal(t, 1, strings.Count(source, "queryNameMap = map[string]string{"))
	require.Contains(t, source, `getOneTaskSQL: "GetOneTask"`)
	require.Contains(t, source, `listTasksSQL: "ListTasks"`)
}

func TestEmitQueryNames_EmptyQueries(t *testing.T) {
	t.Parallel()

	file, err := queryNamesFor(t.Context(), nil)
	require.NoError(t, err)

	source := string(file.Content)

	fileSet := token.NewFileSet()
	_, parseError := parser.ParseFile(fileSet, "query_names.go", source, parser.AllErrors)
	require.NoError(t, parseError, "generated query_names.go must be valid Go:\n%s", source)

	assert.Contains(t, source, "queryNameMap")
	assert.Contains(t, source, "func QueryName(")
}

func TestEmitQueryNames_PackageName(t *testing.T) {
	t.Parallel()

	emission := testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), textMappings(), testDriver())
	emission.Request.Package = "mypackage"
	emission.Request.Queries = []*sqlmodel.AnalysedQuery{withTemplate(&sqlmodel.AnalysedQuery{Name: "get_user", SQL: "SELECT id FROM users"})}
	emission.Request.Style.QueryNames = emit.QueryNames{Enabled: true, File: "query_names.go", Function: "QueryName"}
	file, err := queryNamesFile(t.Context(), emission)
	require.NoError(t, err)

	source := string(file.Content)
	lines := strings.Split(source, "\n")

	foundPackage := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "package ") {
			assert.Equal(t, "package mypackage", trimmed)
			foundPackage = true
			break
		}
	}
	assert.True(t, foundPackage)
}

func TestEmitQueryNames_NamesEditedSQLByTheConstantsPrefix(t *testing.T) {
	t.Parallel()

	slice := sqlmodel.QueryParameter{Name: "ids", Number: 1, IsSlice: true, SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}
	queries := []*sqlmodel.AnalysedQuery{
		{Name: "tasks_by_ids", Filename: "tasks.sql", Command: sqlmodel.QueryCommandMany, Parameters: []sqlmodel.QueryParameter{slice},
			SQL: "SELECT id FROM tasks WHERE id IN ($1)"},
		{Name: "search_tasks", Filename: "tasks.sql", Command: sqlmodel.QueryCommandMany, DynamicRuntime: true,
			Template:      template(text("SELECT id FROM tasks"), clause(sqlmodel.SegmentFilter), clause(sqlmodel.SegmentOrderBy), clause(sqlmodel.SegmentPagination)),
			CountTemplate: new(template(text("SELECT COUNT(*) FROM tasks"), clause(sqlmodel.SegmentFilter)))},
	}

	file, err := queryNamesFor(t.Context(), queries)
	require.NoError(t, err)

	source := string(file.Content)
	assert.Contains(t, source, `var queryNameMap = map[string]string{searchTasksSQL: "SearchTasks", searchTasksCountSQL: "SearchTasks", tasksByIDsSQL: "TasksByIDs"}`)
	assert.Contains(t, source, `var queryNamePrefixes = []string{tasksByIDsSQL[:30], searchTasksCountSQL[:26], searchTasksSQL[:20]}`)
	assert.Contains(t, source, `var queryNamePrefixNames = []string{"TasksByIDs", "SearchTasks", "SearchTasks"}`)
}

func TestEmitQueryNames_FollowsThePlansOrderNotTheSourceOrder(t *testing.T) {
	t.Parallel()

	forward, err := queryNamesFor(t.Context(), []*sqlmodel.AnalysedQuery{
		{Name: "b_task", Filename: "tasks.sql", SQL: "SELECT 2"},
		{Name: "a_task", Filename: "tasks.sql", SQL: "SELECT 1"},
	})
	require.NoError(t, err)
	backward, err := queryNamesFor(t.Context(), []*sqlmodel.AnalysedQuery{
		{Name: "a_task", Filename: "tasks.sql", SQL: "SELECT 1"},
		{Name: "b_task", Filename: "tasks.sql", SQL: "SELECT 2"},
	})
	require.NoError(t, err)

	assert.Equal(t, string(forward.Content), string(backward.Content))
	assert.Contains(t, string(forward.Content), `{aTaskSQL: "ATask", bTaskSQL: "BTask"}`)
}

func TestEmitQueryNames_NamesABatchByItsPrefixOnlyWhenTheDriverRepeatsTheRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		batch      gogen.BatchBuilder
		name       string
		wantPrefix bool
	}{
		{name: "a driver that repeats the VALUES row edits the SQL from the row on", batch: rowBatch{repeats: true}, wantPrefix: true},
		{name: "a driver that queues each row runs the SQL as it is", batch: rowBatch{repeats: false}, wantPrefix: false},
		{name: "a driver that runs a batch as a plain statement runs the SQL as it is", batch: nil, wantPrefix: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			driver := testDriver()
			driver.Batch = test.batch
			emission := testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), textMappings(), driver)
			emission.Request.Package = "generated"
			emission.Request.Style.QueryNames = emit.QueryNames{Enabled: true, File: "query_names.go", Function: "QueryName"}
			emission.Request.Queries = []*sqlmodel.AnalysedQuery{{
				Name: "insert_tasks", Filename: "tasks.sql", Command: sqlmodel.QueryCommandBatch,
				Parameters: []sqlmodel.QueryParameter{{Name: "name", Number: 1, SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}},
				SQL:        "INSERT INTO tasks (name) VALUES ($1)",
				Template: template(text("INSERT INTO tasks (name) VALUES "),
					sqlmodel.SQLSegment{Kind: sqlmodel.SegmentValuesRow, Children: []sqlmodel.SQLSegment{text("("), parameter(0), text(")")}}),
			}}

			file, err := queryNamesFile(t.Context(), emission)
			require.NoError(t, err)

			source := string(file.Content)
			assert.Contains(t, source, `insertTasksSQL: "InsertTasks"`)
			if test.wantPrefix {
				assert.Contains(t, source, `var queryNamePrefixes = []string{insertTasksSQL[:32]}`)
				return
			}
			assert.NotContains(t, source, "queryNamePrefixes")
		})
	}
}

func TestEmitQueryNamesBuildsQueryNameMap(t *testing.T) {
	queries := []*sqlmodel.AnalysedQuery{
		{Name: "list_tasks", Command: sqlmodel.QueryCommandMany, Template: template(text("SELECT 1"))},
		{Name: "create_task", Command: sqlmodel.QueryCommandExec, Template: template(text("SELECT 2"))},
		{Name: "bulk_insert", Command: sqlmodel.QueryCommandCopyFrom, Template: template(text("SELECT 3"))},
	}

	emission := testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), textMappings(), testDriver())
	emission.Request.Queries = queries
	emission.Request.Style.QueryNames = emit.QueryNames{Enabled: true, File: "otel.go", Function: "QueryNameResolver"}
	file, err := queryNamesFile(t.Context(), emission)
	require.NoError(t, err)
	require.Equal(t, "otel.go", file.Name)

	source := string(file.Content)
	requireValidGo(t, file.Content)
	assert.Contains(t, source, "var queryNameMap = map[string]string")
	assert.Contains(t, source, "listTasksSQL:")
	assert.Contains(t, source, "\"ListTasks\"")
	assert.Contains(t, source, "func QueryNameResolver(")
	assert.Contains(t, source, "bulkInsertSQL:", "a driver without a batch builder runs a copy as a plain statement")
	assert.NotContains(t, source, "queryNamePrefixes", "static queries are named by their whole SQL alone")
}

func TestPackageAddsInlineJSONWhenReferenced(t *testing.T) {
	t.Parallel()

	model := sqlmodel.GeneratedFile{Name: "extra.go", Content: []byte("package db\n\ntype Doc struct {\n\tBody JSON\n\tMeta pkg.JSON\n}\n")}
	files, err := Package(t.Context(), testRequest(), stubDriver(model))
	require.NoError(t, err)

	require.Contains(t, fileNames(files), "sgl_json.go")
	source := string(files[len(files)-1].Content)
	require.True(t, strings.HasPrefix(source, "// Code generated by sgl. DO NOT EDIT.\n\npackage db\n"))
	require.Contains(t, source, "type JSON []byte")
	require.NotContains(t, source, "sglScanInto")
	require.Contains(t, source, `"sgl: encode %T to JSON: %w"`)
	_, parseErr := parser.ParseFile(token.NewFileSet(), "sgl_json.go", source, parser.AllErrors)
	require.NoError(t, parseErr)
}

func TestInlineJSONFileCoversTheScanAdapterAlone(t *testing.T) {
	t.Parallel()

	files := []sqlmodel.GeneratedFile{{Name: "q.sql.go", Content: []byte("package db\n\nfunc f() { _ = sglScanInto(&x) }\n")}}

	file, err := inlineJSONFile(t.Context(), testEmission(testDialectInfo(question(true), question(true)), textMappings(), testDriver()), unqualifiedIdentifiers(files))
	require.NoError(t, err)

	source := string(file.Content)
	require.Equal(t, "sgl_json.go", file.Name)
	require.Contains(t, source, "func sglScanInto[T any](dest *T) sql.Scanner")
	require.Contains(t, source, "type sglJSONScanner[T any] struct")
	require.NotContains(t, source, "type JSON []byte")
	_, parseErr := parser.ParseFile(token.NewFileSet(), "sgl_json.go", source, parser.AllErrors)
	require.NoError(t, parseErr)
}

func TestInlineJSONFileSkipsQualifiedAndAbsentReferences(t *testing.T) {
	t.Parallel()

	files := []sqlmodel.GeneratedFile{
		{Name: "models.go", Content: []byte("package db\n\nvar v dbjson.JSON\n")},
		{Name: "notes.txt", Content: []byte("JSON sglScanInto")},
	}

	file, err := inlineJSONFile(t.Context(), testEmission(testDialectInfo(question(true), question(true)), textMappings(), testDriver()), unqualifiedIdentifiers(files))
	require.NoError(t, err)

	require.Empty(t, file.Content)
}

func TestConvertStringsFileIsWrittenOnlyWhenTheCodeCallsIt(t *testing.T) {
	t.Parallel()

	slices := "func sglConvertStrings[To, From ~string](values []From) []To {"
	pointers := "func sglConvertStringPointers[To, From ~string](values []*From) []*To {"
	maps := "func sglConvertStringMap[To ~string, Key comparable, From ~string](values map[Key]From) map[Key]To {"
	testCases := []struct {
		name     string
		source   string
		wants    []string
		unwanted []string
	}{
		{name: "a call of the slice helper", source: "package db\n\nvar v = sglConvertStrings[string](moods)\n", wants: []string{slices}, unwanted: []string{pointers, maps}},
		{name: "a call of the pointer helper", source: "package db\n\nvar v = sglConvertStringPointers[string](moods)\n", wants: []string{pointers}, unwanted: []string{slices, maps}},
		{name: "a call of the map helper", source: "package db\n\nvar v = sglConvertStringMap[string](moods)\n", wants: []string{maps}, unwanted: []string{slices, pointers}},
		{name: "a qualified name", source: "package db\n\nvar v = other.sglConvertStrings[string](moods)\n"},
		{name: "no reference", source: "package db\n\nvar v = moods\n"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := []sqlmodel.GeneratedFile{{Name: "q.sql.go", Content: []byte(testCase.source)}}
			file, err := convertStringsFile(t.Context(), testEmission(testDialectInfo(question(true), question(true)), textMappings(), testDriver()),
				unqualifiedIdentifiers(files))
			require.NoError(t, err)

			if len(testCase.wants) == 0 {
				require.Empty(t, file.Content)
				return
			}
			require.Equal(t, "convert_strings.go", file.Name)
			for _, want := range testCase.wants {
				require.Contains(t, string(file.Content), want)
			}
			for _, unwanted := range testCase.unwanted {
				require.NotContains(t, string(file.Content), unwanted)
			}
		})
	}
}

func TestInlineJSONFileTypeChecks(t *testing.T) {
	t.Parallel()

	files := []sqlmodel.GeneratedFile{{Name: "q.sql.go", Content: []byte("package db\n\nvar _ JSON\nvar _ = sglScanInto[int]\n")}}
	emission := testEmission(testDialectInfo(question(true), question(true)), textMappings(), testDriver())
	emission.Request.Package = "db"
	file, err := inlineJSONFile(t.Context(), emission, unqualifiedIdentifiers(files))
	require.NoError(t, err)

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, file.Name, file.Content, parser.AllErrors)
	require.NoError(t, err)
	config := types.Config{Importer: importer.Default()}
	_, err = config.Check("db", fileSet, []*ast.File{parsed}, nil)
	require.NoError(t, err)
}
