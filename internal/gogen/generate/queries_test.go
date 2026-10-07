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
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/methods"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit"
)

type jsonArrayReader struct{}

func (jsonArrayReader) ColumnReading(sqlType sqlmodel.SQLType) gogen.ColumnReading {
	if sqlType.Category == sqlmodel.TypeCategoryArray {
		return gogen.ColumnReadAsJSON
	}
	return gogen.ColumnReadAsIs
}

type adapterImporter struct {
	packages map[string]*types.Package
}

func (adapters adapterImporter) Import(path string) (*types.Package, error) {
	if adapter, found := adapters.packages[path]; found {
		return adapter, nil
	}
	return adapters.Default().Import(path)
}

func (adapterImporter) Default() types.Importer {
	return importer.Default()
}

type cancellingBinder struct {
	cancel context.CancelCauseFunc
	cause  error
}

func (binder cancellingBinder) Bind(access ast.Expr, _ *sqlmodel.QueryParameter, _ *goast.ImportTracker) ast.Expr {
	binder.cancel(binder.cause)
	return access
}

const labelledErrorsProgram = `package mypkg

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
)

var errRefused = errors.New("the server refused the statement")

type refusingDriver struct{}

func (refusingDriver) Open(string) (driver.Conn, error) { return refusingConnection{}, nil }

type refusingConnection struct{}

func (refusingConnection) Prepare(string) (driver.Stmt, error) { return refusingStatement{}, nil }
func (refusingConnection) Close() error                        { return nil }
func (refusingConnection) Begin() (driver.Tx, error)           { return nil, errRefused }

type refusingStatement struct{}

func (refusingStatement) Close() error                               { return nil }
func (refusingStatement) NumInput() int                              { return -1 }
func (refusingStatement) Exec([]driver.Value) (driver.Result, error) { return nil, errRefused }
func (refusingStatement) Query([]driver.Value) (driver.Rows, error)  { return emptyRows{}, nil }

type emptyRows struct{}

func (emptyRows) Columns() []string          { return []string{"id", "title"} }
func (emptyRows) Close() error               { return nil }
func (emptyRows) Next([]driver.Value) error  { return io.EOF }

func TestMethodsReturnLabelledErrors(t *testing.T) {
	sql.Register("refusing", refusingDriver{})
	database, err := sql.Open("refusing", "")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	queries := New(database)

	_, err = queries.GetNote(context.Background(), "a")
	if !errors.Is(err, sql.ErrNoRows) || err.Error() != "sgl: GetNote: sql: no rows in result set" {
		t.Fatalf("GetNote returned %v", err)
	}
	err = queries.DeleteNote(context.Background(), "a")
	if !errors.Is(err, errRefused) || err.Error() != "sgl: DeleteNote: the server refused the statement" {
		t.Fatalf("DeleteNote returned %v", err)
	}
	if _, err = queries.PurgeNotes(context.Background()); !errors.Is(err, errRefused) || err.Error() != "sgl: PurgeNotes: the server refused the statement" {
		t.Fatalf("PurgeNotes returned %v", err)
	}
	if notes, listErr := queries.ListNotes(context.Background()); listErr != nil || len(notes) != 0 {
		t.Fatalf("ListNotes returned %v, %v", notes, listErr)
	}
}
`

func requireValidGo(t *testing.T, content []byte) {
	t.Helper()
	fileSet := token.NewFileSet()
	_, err := parser.ParseFile(fileSet, "generated.go", content, parser.AllErrors)
	require.NoErrorf(t, err, "emitted source must be valid Go:\n%s", content)
}

func TestEmitQueryFileEmitsCompilableMethodPerCommand(t *testing.T) {
	emission := indexedEmission(true)
	mappings := textMappings()

	tests := []struct {
		name          string
		query         *sqlmodel.AnalysedQuery
		wantSubstrs   []string
		unwantSubstrs []string
	}{
		{
			name: "one method scans single row",
			query: &sqlmodel.AnalysedQuery{
				Name:          "GetUser",
				Filename:      "users.sql",
				SQL:           "SELECT id, name FROM users WHERE id = ?1",
				Command:       sqlmodel.QueryCommandOne,
				ReadOnly:      true,
				Parameters:    []sqlmodel.QueryParameter{textParam("id", 1)},
				OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), textColumn("name", false)},
			},
			wantSubstrs: []string{
				"func (queries *Queries) GetUser(ctx context.Context, id string) (GetUserRow, error)",
				"type GetUserRow struct",
				"const getUserSQL = `SELECT id, name FROM users WHERE id = ?1`",
				"queries.reader.QueryRowContext(ctx, getUserSQL, id)",
			},
		},
		{
			name: "many method iterates rows",
			query: &sqlmodel.AnalysedQuery{
				Name:          "ListUsers",
				Filename:      "users.sql",
				SQL:           "SELECT id FROM users",
				Command:       sqlmodel.QueryCommandMany,
				ReadOnly:      true,
				OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
			},
			wantSubstrs: []string{
				"func (queries *Queries) ListUsers(ctx context.Context) ([]ListUsersRow, error)",
				"for more := true; more; more = rows.Next()",
				"rows.Close()",
			},
		},
		{
			name: "exec method returns error only",
			query: &sqlmodel.AnalysedQuery{
				Name:       "DeleteUser",
				Filename:   "users.sql",
				SQL:        "DELETE FROM users WHERE id = ?1",
				Command:    sqlmodel.QueryCommandExec,
				Parameters: []sqlmodel.QueryParameter{textParam("id", 1)},
			},
			wantSubstrs: []string{
				"func (queries *Queries) DeleteUser(ctx context.Context, id string) error",
				"queries.writer.ExecContext(ctx, deleteUserSQL, id)",
			},
		},
		{
			name: "execresult method returns sql.Result",
			query: &sqlmodel.AnalysedQuery{
				Name:       "InsertUser",
				Filename:   "users.sql",
				SQL:        "INSERT INTO users (name) VALUES (?1)",
				Command:    sqlmodel.QueryCommandExecResult,
				Parameters: []sqlmodel.QueryParameter{textParam("name", 1)},
			},
			wantSubstrs: []string{
				"(sql.Result, error)",
				"return queries.writer.ExecContext(ctx, insertUserSQL, name)",
			},
		},
		{
			name: "execrows method returns int64",
			query: &sqlmodel.AnalysedQuery{
				Name:       "TouchUsers",
				Filename:   "users.sql",
				SQL:        "UPDATE users SET seen = 1",
				Command:    sqlmodel.QueryCommandExecRows,
				Parameters: nil,
			},
			wantSubstrs: []string{
				"(int64, error)",
				"results.RowsAffected()",
			},
		},
		{
			name: "stream method returns iterator",
			query: &sqlmodel.AnalysedQuery{
				Name:          "StreamUsers",
				Filename:      "users.sql",
				SQL:           "SELECT id FROM users",
				Command:       sqlmodel.QueryCommandStream,
				ReadOnly:      true,
				OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
			},
			wantSubstrs: []string{
				"func(yield func(StreamUsersRow, error) bool)",
				"yield(StreamUsersRow{}, err)",
			},
		},
		{
			name: "asyncexec method returns the acceptance error",
			query: &sqlmodel.AnalysedQuery{
				Name:     "PurgeUsers",
				Filename: "users.sql",
				SQL:      "DELETE FROM users WHERE stale = 1",
				Command:  sqlmodel.QueryCommandAsyncExec,
			},
			wantSubstrs: []string{
				"func (queries *Queries) PurgeUsers(ctx context.Context) error",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file, err := emitFile(t.Context(), emission, mappings, test.query.Filename, []*sqlmodel.AnalysedQuery{test.query})
			require.NoError(t, err)
			require.Equal(t, "users.sql.go", file.Name)

			source := string(file.Content)
			requireValidGo(t, file.Content)

			for _, want := range test.wantSubstrs {
				assert.Containsf(t, source, want, "emitted source missing %q\n%s", want, source)
			}
			for _, unwanted := range test.unwantSubstrs {
				assert.NotContainsf(t, source, unwanted, "emitted source unexpectedly contains %q", unwanted)
			}
		})
	}
}

func TestEmitQueryFileMultiParameterUsesParamsStruct(t *testing.T) {
	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:     "FindUser",
		Filename: "users.sql",
		SQL:      "SELECT id FROM users WHERE name = ?1 AND city = ?2",
		Command:  sqlmodel.QueryCommandOne,
		ReadOnly: true,
		Parameters: []sqlmodel.QueryParameter{
			textParam("name", 1),
			textParam("city", 2),
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)
	assert.Contains(t, source, "type FindUserParams struct")
	assert.Contains(t, source, "params FindUserParams")
	assert.Contains(t, source, "params.Name")
	assert.Contains(t, source, "params.City")
}

func TestEmitQueryFileSliceParameterEmitsExpansionPreamble(t *testing.T) {
	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:     "UsersByIDs",
		Filename: "users.sql",
		SQL:      "SELECT id FROM users WHERE id IN (?1)",
		Command:  sqlmodel.QueryCommandMany,
		ReadOnly: true,
		Parameters: []sqlmodel.QueryParameter{
			{
				Name:    "ids",
				Number:  1,
				IsSlice: true,
				SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
				Kind:    sqlmodel.ParameterDirectiveParam,
			},
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)
	assert.Contains(t, source, "if err := sglCheckBinds(len(params.IDs)); err != nil {")
	assert.Contains(t, source, "query := sglSplice(usersByIDsSQL, sglList(sglListMark{Empty: ``, Operator: 30, Start: 33, End: 37}, 1, len(params.IDs)))")
	assert.Contains(t, source, "for _, v := range params.IDs")
	assert.Contains(t, source, "queries.reader.QueryContext(ctx, query, args...)")
}

func TestEmitQueryFileAnonymousPlaceholderEngineCollapsesIndices(t *testing.T) {
	emission := indexedEmission(false)
	query := &sqlmodel.AnalysedQuery{
		Name:     "GetByEmail",
		Filename: "users.sql",
		SQL:      "SELECT id FROM users WHERE email = ?1 OR backup = ?1",
		Command:  sqlmodel.QueryCommandOne,
		ReadOnly: true,
		Parameters: []sqlmodel.QueryParameter{
			textParam("email", 1),
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.Contains(t, source, "WHERE email = ? OR backup = ?")
	assert.Contains(t, source, "queries.reader.QueryRowContext(ctx, getByEmailSQL, email, email)")
}

func TestEmitQueryFileOptionalOneEmitsNoRowsSentinel(t *testing.T) {
	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:          "GetUser",
		Filename:      "users.sql",
		SQL:           "SELECT id, name FROM users WHERE id = ?1",
		Command:       sqlmodel.QueryCommandOne,
		Optional:      true,
		ReadOnly:      true,
		Parameters:    []sqlmodel.QueryParameter{textParam("id", 1)},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), textColumn("name", false)},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.Contains(t, source, "sql.ErrNoRows",
		"database/sql optional one query must test the database/sql no-rows sentinel")
	assert.Contains(t, source, "errors.Is",
		"optional one query must distinguish no rows from a real error via errors.Is")
	assert.Contains(t, source, "(GetUserRow, bool, error)",
		"optional one query must widen the return signature to (row, bool, error)")
}

func TestEmitQueryFileDeclaresARowStructForABatchQueryOnlyWhenItReturnsColumns(t *testing.T) {
	testCases := []struct {
		name    string
		columns []sqlmodel.OutputColumn
		want    bool
	}{
		{name: "a batch query returning nothing declares no row struct", columns: nil, want: false},
		{name: "a batch query returning columns keeps its row struct", columns: []sqlmodel.OutputColumn{textColumn("id", false)}, want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			query := &sqlmodel.AnalysedQuery{
				Name:          "InsertUsers",
				Filename:      "users.sql",
				SQL:           "INSERT INTO users (name, city) VALUES (?1, ?2)",
				Command:       sqlmodel.QueryCommandBatch,
				Parameters:    []sqlmodel.QueryParameter{textParam("name", 1), textParam("city", 2)},
				OutputColumns: testCase.columns,
			}

			file, err := emitFile(t.Context(), indexedEmission(true), textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
			require.NoError(t, err)

			requireValidGo(t, file.Content)
			require.Equal(t, testCase.want, strings.Contains(string(file.Content), "type InsertUsersRow struct"))
		})
	}
}

func checkAdapterPackage(t *testing.T, path, source string) *types.Package {
	t.Helper()
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "adapter.go", source, parser.AllErrors)
	require.NoError(t, err)
	config := types.Config{Importer: importer.Default()}
	checked, err := config.Check(path, fileSet, []*ast.File{parsed}, nil)
	require.NoError(t, err)
	return checked
}

func TestPackageCallsAJSONScanAdapterByTheQualifierItImportsItUnder(t *testing.T) {
	t.Parallel()

	const adapterSource = "package json\n\nimport \"database/sql\"\n\nfunc ScanInto[T any](dest *T) sql.Scanner { return nil }\n"
	testCases := []struct {
		packageNames map[string]string
		name         string
		path         string
	}{
		{name: "a package named like encoding/json", path: "example.com/acme/json"},
		{name: "a versioned package named like encoding/json", path: "example.com/acme/json/v2"},
		{name: "a package whose name a style gives", path: "example.com/acme/go-json", packageNames: map[string]string{"example.com/acme/go-json": "json"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			info := testDialectInfo(question(true), dialect.PlaceholderSigil{})
			info.ToJSON = "json_array"
			driver := testDriver()
			driver.Reader = jsonArrayReader{}
			emission := testEmission(info, textMappings(), driver)
			emission.Request.Style.JSONScanInto = sqlmodel.GoType{Package: testCase.path, Name: "ScanInto"}
			emission.Request.Style.PackageNames = testCase.packageNames
			query := &sqlmodel.AnalysedQuery{
				Name: "ListTags", Filename: "tags.sql", SQL: "SELECT tags FROM posts", Command: sqlmodel.QueryCommandMany, ReadOnly: true,
				OutputColumns: []sqlmodel.OutputColumn{{Name: "tags", SQLType: sqlmodel.SQLType{
					Category: sqlmodel.TypeCategoryArray, ElementType: &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
				}}},
				Template: template(text("SELECT "), sqlmodel.SQLSegment{Kind: sqlmodel.SegmentColumn, Column: 0, Children: []sqlmodel.SQLSegment{text("tags")}},
					text(" FROM posts")),
			}
			emission.Request.Queries = []*sqlmodel.AnalysedQuery{query}

			files, err := Package(t.Context(), emission.Request, emission.Driver)
			require.NoError(t, err)

			fileSet := token.NewFileSet()
			parsed := make([]*ast.File, 0, len(files))
			for _, file := range files {
				syntax, parseErr := parser.ParseFile(fileSet, file.Name, file.Content, parser.AllErrors)
				require.NoError(t, parseErr)
				parsed = append(parsed, syntax)
			}
			adapter := checkAdapterPackage(t, testCase.path, adapterSource)
			config := types.Config{Importer: adapterImporter{packages: map[string]*types.Package{testCase.path: adapter}}}
			_, err = config.Check(emission.Request.Package, fileSet, parsed, nil)
			require.NoError(t, err)
			require.Contains(t, string(files[0].Content), ".ScanInto(&row.Tags)")
		})
	}
}

func TestEmitQueriesStopsAtTheNextFileOnceTheContextIsCancelled(t *testing.T) {
	t.Parallel()

	cause := errors.New("cancelled while emitting")
	ctx, cancel := context.WithCancelCause(t.Context())
	t.Cleanup(func() { cancel(nil) })
	driver := testDriver()
	driver.Binder = cancellingBinder{cancel: cancel, cause: cause}
	emission := testEmission(testDialectInfo(question(true), dialect.PlaceholderSigil{}), textMappings(), driver)
	queries := make([]*sqlmodel.AnalysedQuery, 0, 3)
	for _, filename := range []string{"a.sql", "b.sql", "c.sql"} {
		queries = append(queries, &sqlmodel.AnalysedQuery{
			Name: "Delete" + strings.ToUpper(filename[:1]), Filename: filename, SQL: "DELETE FROM t WHERE id = ?1",
			Command: sqlmodel.QueryCommandExec, Parameters: []sqlmodel.QueryParameter{textParam("id", 1)},
		})
	}

	files, err := emitQueriesFor(ctx, emission, queries)

	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "emitting b.sql")
	require.Empty(t, files)
}

func TestPackageWritesADescriptionHoldingRunesGoCommentsCannotHold(t *testing.T) {
	t.Parallel()

	for _, text := range awkwardTexts {
		t.Run(text.name, func(t *testing.T) {
			t.Parallel()

			emission := indexedEmission(true)
			emission.Request.Style.DocComments = true
			emission.Request.Queries = []*sqlmodel.AnalysedQuery{withTemplate(&sqlmodel.AnalysedQuery{
				Name: "ListNotes", Filename: "notes.sql", SQL: "SELECT id FROM notes", Command: sqlmodel.QueryCommandMany, ReadOnly: true,
				Description:   "Lists the notes " + text.text + " in order.",
				OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
			})}

			files, err := Package(t.Context(), emission.Request, emission.Driver)

			require.NoError(t, err)
			require.Contains(t, string(files[0].Content), "// Lists the notes ")
		})
	}
}

func labelledQueries() []*sqlmodel.AnalysedQuery {
	note := []sqlmodel.OutputColumn{textColumn("id", false), textColumn("title", false)}
	return []*sqlmodel.AnalysedQuery{
		{Name: "GetNote", Filename: "notes.sql", Command: sqlmodel.QueryCommandOne, ReadOnly: true, OutputColumns: note,
			Parameters: []sqlmodel.QueryParameter{textParam("id", 1)}, Template: template(text("SELECT id, title FROM notes WHERE id = "), parameter(0))},
		{Name: "ListNotes", Filename: "notes.sql", Command: sqlmodel.QueryCommandMany, ReadOnly: true, OutputColumns: note,
			Template: template(text("SELECT id, title FROM notes"))},
		{Name: "DeleteNote", Filename: "notes.sql", Command: sqlmodel.QueryCommandExec,
			Parameters: []sqlmodel.QueryParameter{textParam("id", 1)}, Template: template(text("DELETE FROM notes WHERE id = "), parameter(0))},
		{Name: "PurgeNotes", Filename: "notes.sql", Command: sqlmodel.QueryCommandExecRows, Template: template(text("DELETE FROM notes"))},
		{Name: "ArchiveNotes", Filename: "notes.sql", Command: sqlmodel.QueryCommandExecResult, Template: template(text("UPDATE notes SET archived = 1"))},
		runtimeBuilderQuery(),
	}
}

func generateLabelled(t *testing.T, wraps bool) map[string][]byte {
	t.Helper()
	emission := indexedEmission(true)
	emission.Request.Queries = labelledQueries()
	emission.Request.Style.WrapErrors = wraps
	files, err := Package(t.Context(), emission.Request, emission.Driver)
	require.NoError(t, err)
	generated := make(map[string][]byte, len(files))
	for _, file := range files {
		generated[file.Name] = file.Content
	}
	return generated
}

func TestPackageLabelsTheErrorsItsMethodsReturnOnlyWhenTheStyleAsks(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		wants   []string
		absents []string
		wraps   bool
	}{
		{
			name:  "labelled errors",
			wraps: true,
			wants: []string{
				`return GetNoteRow{}, sglWrapError("sgl: GetNote", err)`,
				`return sglWrapError("sgl: DeleteNote", err)`,
				`return sglWrapResult[int64]("sgl: PurgeNotes")(results.RowsAffected())`,
				`return nil, sglWrapError("sgl: SearchPosts.All", err)`,
			},
			absents: []string{`sglWrapError("sgl: SearchPosts.buildQuery"`},
		},
		{name: "errors as they are", wraps: false, absents: []string{"sglWrapError", "sglWrapResult"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			generated := generateLabelled(t, testCase.wraps)
			_, hasHelpers := generated["error_wrapping.go"]
			require.Equal(t, testCase.wraps, hasHelpers, "the package holds the helpers only when it labels errors")

			fileSet := token.NewFileSet()
			syntax := make([]*ast.File, 0, len(generated))
			for name, content := range generated {
				parsed, err := parser.ParseFile(fileSet, name, content, parser.AllErrors)
				require.NoError(t, err)
				syntax = append(syntax, parsed)
			}
			_, err := (&types.Config{Importer: importer.Default()}).Check("mypkg", fileSet, syntax, nil)
			require.NoError(t, err, "the package type-checks")

			source := string(generated["notes.sql.go"]) + string(generated["posts.sql.go"])
			for _, want := range testCase.wants {
				require.Contains(t, source, want)
			}
			for _, absent := range testCase.absents {
				require.NotContains(t, source, absent)
			}
		})
	}
}

func TestPackageLabelledErrorsKeepTheErrorBeneathAtRunTime(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs go test over a generated module; skipped under -short")
	}

	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module labelled\n\ngo 1.22\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "labelled_test.go"), []byte(labelledErrorsProgram), 0o600))
	for name, content := range generateLabelled(t, true) {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), content, 0o600))
	}

	command := testkit.GoCommand(t, directory, []string{"GOFLAGS=-mod=mod"}, "test", "-count=1", "./...")
	output, err := command.CombinedOutput()
	require.NoErrorf(t, err, "go test failed:\n%s", output)
}

func dynamicSortableQuery(command sqlmodel.QueryCommand) *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:      "SearchPosts",
		Filename:  "posts.sql",
		SQL:       "SELECT id, title FROM posts WHERE author = ?1 ORDER BY created_at LIMIT ?2",
		Command:   command,
		IsDynamic: true,
		ReadOnly:  true,
		Parameters: []sqlmodel.QueryParameter{
			{
				Name:    "author",
				Number:  1,
				SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
				Kind:    sqlmodel.ParameterDirectiveParam,
			},
			{
				Name:         "page_size",
				Number:       2,
				SQLType:      sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger},
				Context:      sqlmodel.ParameterContextLimit,
				DefaultLimit: new(20),
				MaxLimit:     new(100),
			},
			{
				Name:            "sort",
				Number:          3,
				SQLType:         sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
				Kind:            sqlmodel.ParameterDirectiveSortable,
				SortableColumns: []string{"created_at", "title"},
			},
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), textColumn("title", false)},
		Template: template(
			text("SELECT id, title FROM posts WHERE "),
			clause(sqlmodel.SegmentFilter, text("author = "), parameter(0)),
			text(" "),
			clause(sqlmodel.SegmentOrderBy, text("ORDER BY created_at")),
			text(" "),
			clause(sqlmodel.SegmentPagination, text("LIMIT "), parameter(1)),
		),
	}
}

func TestEmitQueryFileDynamicQueryEmitsParamsAndSortableScaffolding(t *testing.T) {
	emission := indexedEmission(true)
	query := dynamicSortableQuery(sqlmodel.QueryCommandMany)

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.Contains(t, source, "type SearchPostsParams struct")
	assert.Contains(t, source, "type SearchPostsOrderBy string")
	assert.Contains(t, source, "SearchPostsOrderByCreatedAt SearchPostsOrderBy = \"created_at\"")
	assert.Contains(t, source, "SortDirection OrderDirection")
	assert.NotContains(t, source, "type OrderDirection string", "the package declares OrderDirection once, in sortable.go")

	assert.Contains(t, source, "if params.PageSize == 0")
	assert.Contains(t, source, "if params.PageSize > 100")

	assert.Contains(t, source, "query := searchPostsSQL[:46]")
	assert.Contains(t, source, "query = query + (\" ORDER BY \" + string(params.Sort))")
}

func TestEmitQueryFileDynamicCommandsProduceValidGo(t *testing.T) {
	emission := indexedEmission(true)

	tests := []struct {
		name           string
		command        sqlmodel.QueryCommand
		wantResultPart string
	}{
		{name: "dynamic one", command: sqlmodel.QueryCommandOne, wantResultPart: "(SearchPostsRow, error)"},
		{name: "dynamic many", command: sqlmodel.QueryCommandMany, wantResultPart: "([]SearchPostsRow, error)"},
		{name: "dynamic exec", command: sqlmodel.QueryCommandExec, wantResultPart: ") error {"},
		{name: "dynamic execresult", command: sqlmodel.QueryCommandExecResult, wantResultPart: "(sql.Result, error)"},
		{name: "dynamic execrows", command: sqlmodel.QueryCommandExecRows, wantResultPart: "(int64, error)"},
		{name: "dynamic stream", command: sqlmodel.QueryCommandStream, wantResultPart: "func(yield func(SearchPostsRow, error) bool)"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := dynamicSortableQuery(test.command)
			file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
			require.NoError(t, err)

			source := string(file.Content)
			requireValidGo(t, file.Content)
			assert.Containsf(t, source, test.wantResultPart, "emitted source missing %q\n%s", test.wantResultPart, source)
		})
	}
}

func TestEmitQueryFileDynamicSliceQueryEmitsExpansionAndSortable(t *testing.T) {
	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:      "FilterPosts",
		Filename:  "posts.sql",
		SQL:       "SELECT id FROM posts WHERE tag IN (?1) ORDER BY id",
		Command:   sqlmodel.QueryCommandMany,
		IsDynamic: true,
		ReadOnly:  true,
		Parameters: []sqlmodel.QueryParameter{
			{
				Name:    "tags",
				Number:  1,
				IsSlice: true,
				SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
				Kind:    sqlmodel.ParameterDirectiveParam,
			},
			{
				Name:            "sort",
				Number:          2,
				SQLType:         sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
				Kind:            sqlmodel.ParameterDirectiveSortable,
				SortableColumns: []string{"id"},
			},
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
		Template: template(
			text("SELECT id FROM posts WHERE "),
			clause(sqlmodel.SegmentFilter, text("tag "), list(0)),
			text(" "),
			clause(sqlmodel.SegmentOrderBy, text("ORDER BY id")),
			clause(sqlmodel.SegmentPagination),
		),
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)
	assert.Contains(t, source, "query := filterPostsSQL\n")
	assert.Contains(t, source, "query = sglSplice(query, sglList(sglListMark{")
	assert.Contains(t, source, "for _, v := range params.Tags")
	assert.Contains(t, source, "string(params.Sort)")
}

func embeddedColumn(name, embedTable string, isOuter bool) sqlmodel.OutputColumn {
	return sqlmodel.OutputColumn{
		Name:         name,
		SQLType:      sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
		IsEmbedded:   true,
		EmbedTable:   embedTable,
		EmbedIsOuter: isOuter,

		Nullable: isOuter,
	}
}

func namedEmbeddedColumn(name, embedTable, embedName string, isOuter bool) sqlmodel.OutputColumn {
	column := embeddedColumn(name, embedTable, isOuter)
	column.EmbedName = embedName
	return column
}

func TestEmitQueryFileEmbedNameNamesTheFieldAndKeepsSelfJoinSidesApart(t *testing.T) {
	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:     "ListEmployees",
		Filename: "employees.sql",
		SQL:      "SELECT e.name, m.name FROM employees e LEFT JOIN employees m ON m.id = e.manager_id",
		Command:  sqlmodel.QueryCommandMany,
		ReadOnly: true,
		OutputColumns: []sqlmodel.OutputColumn{
			namedEmbeddedColumn("name", "employees", "Report", false),
			namedEmbeddedColumn("name", "employees", "Manager", true),
		},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.Contains(t, source, "type ListEmployeesReport struct")
	assert.Contains(t, source, "type ListEmployeesManager struct")
	assert.Regexp(t, `\n\tReport\s+ListEmployeesReport\n`, source)
	assert.Regexp(t, `\n\tManager\s+\*ListEmployeesManager\n`, source)
	assert.Contains(t, source, "&row.Report.Name, &embedRows.Manager.Name")
	assert.Contains(t, source, "row.Manager = &ListEmployeesManager{Name: embedRows.Manager.Name}")
	assert.Contains(t, source, "if embedRows.Manager.Name != nil {")
	assert.NotContains(t, source, "ListEmployeesEmployees")
}

func TestEmitQueryFileGroupedQueryNamesEmbedsAfterTheirEmbedName(t *testing.T) {
	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:       "ListOrdersWithItems",
		Filename:   "orders.sql",
		SQL:        "SELECT o.id, i.sku FROM orders o JOIN items i ON i.order_id = o.id",
		Command:    sqlmodel.QueryCommandMany,
		ReadOnly:   true,
		GroupByKey: []string{"orders.id"},
		OutputColumns: []sqlmodel.OutputColumn{
			namedEmbeddedColumn("id", "orders", "Order", false),
			namedEmbeddedColumn("sku", "items", "LineItems", false),
		},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.Regexp(t, `\n\tOrder\s+ListOrdersWithItemsOrder\n`, source)
	assert.Regexp(t, `\n\tLineItems\s+\[\]ListOrdersWithItemsLineItems\n`, source)
	assert.Contains(t, source, "Order: ListOrdersWithItemsOrder{")
	assert.Contains(t, source, "results[entry].LineItems = append(results[entry].LineItems, ListOrdersWithItemsLineItems{")
}

func TestEmitQueryFileEmbedQueryEmitsNestedStructs(t *testing.T) {
	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:     "GetOrder",
		Filename: "orders.sql",
		SQL:      "SELECT o.id, o.total, u.id, u.name FROM orders o JOIN users u ON u.id = o.user_id LEFT JOIN coupons c ON c.id = o.coupon_id",
		Command:  sqlmodel.QueryCommandMany,
		ReadOnly: true,
		OutputColumns: []sqlmodel.OutputColumn{
			textColumn("id", false),
			embeddedColumn("name", "users", false),
			embeddedColumn("code", "coupons", true),
		},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.Contains(t, source, "type GetOrderUsers struct")
	assert.Contains(t, source, "type GetOrderCoupons struct")
	assert.Contains(t, source, "type GetOrderRow struct")

	assert.Regexp(t, `Users\s+GetOrderUsers`, source)
	assert.Regexp(t, `Coupons\s+\*GetOrderCoupons`, source)

	assert.Contains(t, source, "row.Coupons = &GetOrderCoupons{Code: embedRows.Coupons.Code}")
	assert.Contains(t, source, "if embedRows.Coupons.Code != nil")
}

func TestEmitQueryFileOuterEmbedNonNilableSentinelSkipsNilCheck(t *testing.T) {

	emission := indexedEmission(true)
	valueColumn := sqlmodel.OutputColumn{
		Name:         "code",
		SQLType:      sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
		IsEmbedded:   true,
		EmbedTable:   "coupons",
		EmbedIsOuter: true,
		Nullable:     false,
	}
	query := &sqlmodel.AnalysedQuery{
		Name:     "GetOrder",
		Filename: "orders.sql",
		SQL:      "SELECT o.id, c.code FROM orders o LEFT JOIN coupons c ON c.id = o.coupon_id",
		Command:  sqlmodel.QueryCommandMany,
		ReadOnly: true,
		OutputColumns: []sqlmodel.OutputColumn{
			textColumn("id", false),
			valueColumn,
		},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.Contains(t, source, "row.Coupons = &GetOrderCoupons{}")
	assert.NotContains(t, source, "row.Coupons.Code == nil",
		"a value-typed sentinel must not produce a nil comparison")
}

func TestEmitQueryFileOuterEmbedPicksFirstNilableSentinel(t *testing.T) {

	emission := indexedEmission(true)
	valueColumn := sqlmodel.OutputColumn{
		Name:         "code",
		SQLType:      sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
		IsEmbedded:   true,
		EmbedTable:   "coupons",
		EmbedIsOuter: true,
		Nullable:     false,
	}
	nilableColumn := sqlmodel.OutputColumn{
		Name:         "label",
		SQLType:      sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
		IsEmbedded:   true,
		EmbedTable:   "coupons",
		EmbedIsOuter: true,
		Nullable:     true,
	}
	query := &sqlmodel.AnalysedQuery{
		Name:     "GetOrder",
		Filename: "orders.sql",
		SQL:      "SELECT o.id, c.code, c.label FROM orders o LEFT JOIN coupons c ON c.id = o.coupon_id",
		Command:  sqlmodel.QueryCommandMany,
		ReadOnly: true,
		OutputColumns: []sqlmodel.OutputColumn{
			textColumn("id", false),
			valueColumn,
			nilableColumn,
		},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.NotContains(t, source, "row.Coupons.Code == nil")
	assert.Contains(t, source, "if embedRows.Coupons.Label != nil")
}

func TestEmitQueryFileCollidingColumnNamesProduceCompilableStruct(t *testing.T) {

	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:     "GetThings",
		Filename: "things.sql",
		SQL:      "SELECT a AS foo_bar, b AS foo__bar FROM things",
		Command:  sqlmodel.QueryCommandMany,
		ReadOnly: true,
		OutputColumns: []sqlmodel.OutputColumn{
			textColumn("foo_bar", false),
			textColumn("foo__bar", false),
		},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.Contains(t, source, "FooBar  string")
	assert.Contains(t, source, "FooBar2 string")
	assert.Contains(t, source, "&row.FooBar")
	assert.Contains(t, source, "&row.FooBar2")
}

func TestEmitQueryFileLeadingDigitColumnNameProducesAnExportedField(t *testing.T) {

	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:     "GetUser",
		Filename: "users.sql",
		SQL:      `SELECT enabled AS "2fa_enabled" FROM users`,
		Command:  sqlmodel.QueryCommandMany,
		ReadOnly: true,
		OutputColumns: []sqlmodel.OutputColumn{
			textColumn("2fa_enabled", false),
		},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.Contains(t, source, "X2faEnabled string")
	assert.Contains(t, source, "&row.X2faEnabled")
	assert.Contains(t, source, "`json:\"2fa_enabled\"`")
	assert.NotContains(t, source, "_2faEnabled",
		"the field must be exported, or encoding/json drops the column from the response")
}

func TestEmitQueryFileGroupedQueryEmitsGroupingScaffolding(t *testing.T) {
	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:       "ListOrdersWithItems",
		Filename:   "orders.sql",
		SQL:        "SELECT o.id, i.sku FROM orders o JOIN items i ON i.order_id = o.id",
		Command:    sqlmodel.QueryCommandMany,
		ReadOnly:   true,
		GroupByKey: []string{"orders.id"},
		OutputColumns: []sqlmodel.OutputColumn{
			embeddedColumn("id", "orders", false),
			embeddedColumn("sku", "items", false),
		},
	}

	require.True(t, methods.HasGroupByKey(query), "fixture must be a group_by query")

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.Regexp(t, `Items\s+\[\]ListOrdersWithItemsItems`, source)

	assert.Contains(t, source, "groupIndex")
	assert.Contains(t, source, "entry := len(results)")
	assert.NotContains(t, source, "groupOrder")
	assert.Contains(t, source, "exists")
}

func TestEmitQueryFileDynamicGroupedQueryEmitsParamPreamble(t *testing.T) {
	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:       "ListOrdersForUser",
		Filename:   "orders.sql",
		SQL:        "SELECT o.id, i.sku FROM orders o JOIN items i ON i.order_id = o.id WHERE o.user_id = ?1",
		Command:    sqlmodel.QueryCommandMany,
		IsDynamic:  true,
		ReadOnly:   true,
		GroupByKey: []string{"orders.id"},
		Parameters: []sqlmodel.QueryParameter{
			textParam("user_id", 1),
		},
		OutputColumns: []sqlmodel.OutputColumn{
			embeddedColumn("id", "orders", false),
			embeddedColumn("sku", "items", false),
		},
	}

	require.True(t, methods.HasGroupByKey(query), "fixture must be a group_by query")

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)
	assert.Contains(t, source, "params ListOrdersForUserParams")
	assert.Contains(t, source, "params.UserID")
	assert.Contains(t, source, "groupIndex")
}

func TestEmitQueryFileMultiParamAnonymousEngineOrdersArgs(t *testing.T) {
	emission := indexedEmission(false)
	query := &sqlmodel.AnalysedQuery{
		Name:     "MatchPair",
		Filename: "pairs.sql",
		SQL:      "SELECT id FROM pairs WHERE a = ?2 AND b = ?1 AND c = ?2",
		Command:  sqlmodel.QueryCommandOne,
		ReadOnly: true,
		Parameters: []sqlmodel.QueryParameter{
			textParam("first", 1),
			textParam("second", 2),
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.Contains(t, source, "WHERE a = ? AND b = ? AND c = ?")
	assert.Contains(t, source, "params.Second, params.First, params.Second")
}

func TestEmitQueryFileStreamSliceEmitsYieldGuard(t *testing.T) {
	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:     "StreamByIDs",
		Filename: "stream.sql",
		SQL:      "SELECT id FROM users WHERE id IN (?1)",
		Command:  sqlmodel.QueryCommandStream,
		ReadOnly: true,
		Parameters: []sqlmodel.QueryParameter{
			{
				Name:    "ids",
				Number:  1,
				IsSlice: true,
				SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
				Kind:    sqlmodel.ParameterDirectiveParam,
			},
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)
	assert.Contains(t, source, "query := sglSplice(streamByIDsSQL, sglList(")
	assert.Contains(t, source, "if err := sglCheckBinds(len(params.IDs)); err != nil {\n\t\t\tyield(StreamByIDsRow{}, err)\n\t\t\treturn\n\t\t}")
}

func TestEmitQueryFileRequiresRowTypeEmitsEmptyStub(t *testing.T) {
	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:          "MysteryRows",
		Filename:      "mystery.sql",
		SQL:           "SELECT * FROM mystery_view",
		Command:       sqlmodel.QueryCommandMany,
		ReadOnly:      true,
		OutputColumns: nil,
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)
	assert.Regexp(t, `type MysteryRowsRow struct \{\s*\}`, source)
}

func TestEmitQueryFileDynamicAnonymousPlaceholderOrdersArgs(t *testing.T) {
	emission := indexedEmission(false)
	query := &sqlmodel.AnalysedQuery{
		Name:      "FindThings",
		Filename:  "things.sql",
		SQL:       "SELECT id FROM things WHERE a = ?2 AND b = ?1 AND c = ?2",
		Command:   sqlmodel.QueryCommandMany,
		IsDynamic: true,
		ReadOnly:  true,
		Parameters: []sqlmodel.QueryParameter{
			textParam("first", 1),
			textParam("second", 2),
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
	}

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)

	assert.Contains(t, source, "params.Second, params.First, params.Second")
}

func namedPlaceholderQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:          "OrdersFor",
		Filename:      "orders.sql",
		SQL:           "SELECT id FROM orders WHERE customer = :who OR title = :who OR id = :id",
		Command:       sqlmodel.QueryCommandMany,
		ReadOnly:      true,
		Parameters:    []sqlmodel.QueryParameter{textParam("who", 1), textParam("id", 2)},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
	}
}

func TestNamedPlaceholdersTakeTheEngineMarker(t *testing.T) {
	tests := []struct {
		name     string
		emission *gogen.Emission
		want     string
	}{
		{
			name:     "question mark engines number with a question mark",
			emission: indexedEmission(true),
			want:     "SELECT id FROM orders WHERE customer = ?1 OR title = ?1 OR id = ?2",
		},
		{
			name:     "dollar engines number with a dollar",
			emission: dollarEmission(),
			want:     "SELECT id FROM orders WHERE customer = $1 OR title = $1 OR id = $2",
		},
		{
			name:     "anonymous placeholder engines collapse the index",
			emission: indexedEmission(false),
			want:     "SELECT id FROM orders WHERE customer = ? OR title = ? OR id = ?",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := withTemplate(namedPlaceholderQuery())
			test.emission.Request.Queries = []*sqlmodel.AnalysedQuery{query}
			generated, err := test.emission.Query(query, goast.NewImportTracker())
			require.NoError(t, err)
			require.Equal(t, test.want, generated.Rendered.SQL)
		})
	}
}

func TestRepeatedNamedPlaceholderBindsOncePerOccurrenceOnAnonymousEngines(t *testing.T) {
	emission := indexedEmission(false)
	query := namedPlaceholderQuery()

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)
	requireValidGo(t, file.Content)
	require.Contains(t, string(file.Content), "QueryContext(ctx, ordersForSQL, params.Who, params.Who, params.ID)")

	single := &sqlmodel.AnalysedQuery{
		Name:          "OrdersFor",
		Filename:      "orders.sql",
		SQL:           "SELECT id FROM orders WHERE customer = :who OR title = :who",
		Command:       sqlmodel.QueryCommandMany,
		ReadOnly:      true,
		Parameters:    []sqlmodel.QueryParameter{textParam("who", 1)},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
	}
	file, err = emitFile(t.Context(), emission, textMappings(), single.Filename, []*sqlmodel.AnalysedQuery{single})
	require.NoError(t, err)
	require.Contains(t, string(file.Content), "QueryContext(ctx, ordersForSQL, who, who)")
}

func runtimeBuilderQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:           "SearchPosts",
		Filename:       "posts.sql",
		Command:        sqlmodel.QueryCommandMany,
		DynamicRuntime: true,
		ReadOnly:       true,
		Template: template(text("SELECT id, title FROM posts WHERE "),
			clause(sqlmodel.SegmentFilter, text("environment_id = "), parameter(0)),
			clause(sqlmodel.SegmentOrderBy), clause(sqlmodel.SegmentPagination)),
		CountTemplate: new(template(text("SELECT COUNT(*) FROM posts WHERE "),
			clause(sqlmodel.SegmentFilter, text("environment_id = "), parameter(0)))),
		Parameters: []sqlmodel.QueryParameter{
			textParam("environment_id", 1),
		},
		AllowedColumns: []sqlmodel.AllowedColumn{
			{Name: "title", SourceExpression: "posts.title", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			{Name: "created_at", SourceExpression: "posts.created_at", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), textColumn("title", false)},
	}
}

func TestEmitQueryFileRuntimeBuilderProducesValidGo(t *testing.T) {
	emission := indexedEmission(true)
	query := runtimeBuilderQuery()

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	source := string(file.Content)
	requireValidGo(t, file.Content)
	assert.Contains(t, source, "type SearchPostsBuilder struct")
	assert.Contains(t, source, "const searchPostsCountSQL = `SELECT COUNT(*) FROM posts WHERE environment_id = ?1`")
}

func TestSortableOrderBySplicedBeforeTrailingClause(t *testing.T) {
	tests := []struct {
		name     string
		emission *gogen.Emission
		tail     []sqlmodel.SQLSegment
		wantSQL  string
		point    int
	}{
		{
			name:     "a written order by is replaced before the limit",
			emission: indexedEmission(true),
			tail: []sqlmodel.SQLSegment{
				text(" "), clause(sqlmodel.SegmentOrderBy, text("ORDER BY created_at")),
				text(" "), clause(sqlmodel.SegmentPagination, text("LIMIT "), parameter(1)),
			},
			wantSQL: "SELECT id, title FROM posts WHERE author = ?1  ORDER BY title DESC LIMIT ?2",
			point:   46,
		},
		{
			name:     "the order by goes before the statement terminator",
			emission: indexedEmission(true),
			tail: []sqlmodel.SQLSegment{
				text(" AND "), parameter(1), text(" > 0"),
				clause(sqlmodel.SegmentOrderBy), clause(sqlmodel.SegmentPagination), text(";"),
			},
			wantSQL: "SELECT id, title FROM posts WHERE author = ?1 AND ?2 > 0 ORDER BY title DESC;",
			point:   56,
		},
		{
			name:     "anonymous placeholders keep the place",
			emission: indexedEmission(false),
			tail: []sqlmodel.SQLSegment{
				clause(sqlmodel.SegmentOrderBy),
				text(" "), clause(sqlmodel.SegmentPagination, text("LIMIT "), parameter(1), text(", 10")),
			},
			wantSQL: "SELECT id, title FROM posts WHERE author = ? ORDER BY title DESC LIMIT ?, 10",
			point:   44,
		},
	}

	commands := []sqlmodel.QueryCommand{
		sqlmodel.QueryCommandOne,
		sqlmodel.QueryCommandMany,
		sqlmodel.QueryCommandStream,
	}

	for _, test := range tests {
		for _, command := range commands {
			t.Run(test.name+"/"+command.String(), func(t *testing.T) {
				query := dynamicSortableQuery(command)
				query.Template = template(append([]sqlmodel.SQLSegment{
					text("SELECT id, title FROM posts WHERE "),
					clause(sqlmodel.SegmentFilter, text("author = "), parameter(0)),
				}, test.tail...)...)
				file, err := emitFile(t.Context(), test.emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
				require.NoError(t, err)
				requireValidGo(t, file.Content)

				generated, err := test.emission.Query(query, goast.NewImportTracker())
				require.NoError(t, err)
				text := generated.Rendered.SQL
				require.Equal(t, test.point, generated.Rendered.Marks.OrderBy.Start)
				require.Equal(t, test.wantSQL, text[:test.point]+" ORDER BY title DESC"+text[test.point:])

				source := string(file.Content)
				require.Contains(t, source, fmt.Sprintf("query := searchPostsSQL[:%d]", test.point))
				require.Contains(t, source, fmt.Sprintf("query = query + searchPostsSQL[%d:]", test.point))
				require.Less(t,
					strings.Index(source, `query = query + (" ORDER BY " + string(params.Sort))`),
					strings.Index(source, fmt.Sprintf("query = query + searchPostsSQL[%d:]", test.point)),
				)
			})
		}
	}
}

func TestSortableOrderByWithoutTrailingClauseAppendsToConstant(t *testing.T) {
	emission := indexedEmission(true)
	query := dynamicSortableQuery(sqlmodel.QueryCommandMany)
	query.Template = template(
		text("SELECT id, title FROM posts WHERE "),
		clause(sqlmodel.SegmentFilter, text("author = "), parameter(0), text(" AND "), parameter(1), text(" > 0")),
		clause(sqlmodel.SegmentOrderBy),
		clause(sqlmodel.SegmentPagination),
	)

	file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)
	requireValidGo(t, file.Content)

	source := string(file.Content)
	require.Contains(t, source, "query := searchPostsSQL\n")
	require.NotContains(t, source, "searchPostsSQL[")
}

func TestSortableOrderBySplicedBeforeTheSlicesAreWritten(t *testing.T) {
	emission := indexedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:      "FilterPosts",
		Filename:  "posts.sql",
		Command:   sqlmodel.QueryCommandMany,
		IsDynamic: true,
		ReadOnly:  true,
		Parameters: []sqlmodel.QueryParameter{
			{
				Name:    "tags",
				Number:  1,
				IsSlice: true,
				SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
				Kind:    sqlmodel.ParameterDirectiveParam,
			},
			{
				Name:    "page_size",
				Number:  2,
				SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger},
				Context: sqlmodel.ParameterContextLimit,
			},
			{
				Name:            "sort",
				SQLType:         sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
				Kind:            sqlmodel.ParameterDirectiveSortable,
				SortableColumns: []string{"id"},
			},
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
		Template: template(
			text("SELECT id FROM posts WHERE "),
			clause(sqlmodel.SegmentFilter, text("tag "), list(0)),
			clause(sqlmodel.SegmentOrderBy),
			text(" "), clause(sqlmodel.SegmentPagination, text("LIMIT "), parameter(1)),
		),
	}

	commands := []sqlmodel.QueryCommand{
		sqlmodel.QueryCommandOne,
		sqlmodel.QueryCommandMany,
		sqlmodel.QueryCommandStream,
	}
	for _, command := range commands {
		t.Run(command.String(), func(t *testing.T) {
			query.Command = command
			file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
			require.NoError(t, err)
			requireValidGo(t, file.Content)

			source := string(file.Content)
			tailAppend := "query = query + filterPostsSQL[38:]"
			splice := "query = sglSplice(query, sglList(sglListMark{Empty: ``, Operator: 31, Start: 34, End: 38}, 2, len(params.Tags)))"
			require.Contains(t, source, "query := filterPostsSQL[:38]")
			require.Contains(t, source, splice)
			require.Less(t, strings.Index(source, tailAppend), strings.Index(source, splice))
		})
	}
}

func TestDynamicQueryArgumentsAreWrappedByTheStrategy(t *testing.T) {
	tests := []struct {
		name     string
		emission *gogen.Emission
	}{
		{name: "sortable query", emission: namedEmission(true)},
		{name: "sortable query in occurrence order", emission: namedEmission(false)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := dynamicSortableQuery(sqlmodel.QueryCommandMany)
			file, err := emitFile(t.Context(), test.emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
			require.NoError(t, err)
			requireValidGo(t, file.Content)

			source := string(file.Content)
			require.Contains(t, source, `driver.Named("author", params.Author)`)
			require.Contains(t, source, `driver.Named("page_size", params.PageSize)`)
			require.NotContains(t, source, `driver.Named("sort"`)
		})
	}
}

func TestDynamicOptionalQueryArgumentsAreWrappedByTheStrategy(t *testing.T) {
	emission := namedEmission(true)
	query := &sqlmodel.AnalysedQuery{
		Name:      "SearchPosts",
		Filename:  "posts.sql",
		SQL:       "SELECT id FROM posts WHERE (?1 IS NULL OR author = ?1) AND title = ?2",
		Command:   sqlmodel.QueryCommandMany,
		IsDynamic: true,
		ReadOnly:  true,
		Parameters: []sqlmodel.QueryParameter{
			{
				Name:       "author",
				Number:     1,
				SQLType:    sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
				Kind:       sqlmodel.ParameterDirectiveParam,
				IsOptional: true,
			},
			textParam("title", 2),
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
	}

	commands := []sqlmodel.QueryCommand{
		sqlmodel.QueryCommandOne,
		sqlmodel.QueryCommandMany,
		sqlmodel.QueryCommandExec,
		sqlmodel.QueryCommandStream,
	}
	for _, command := range commands {
		t.Run(command.String(), func(t *testing.T) {
			query.Command = command
			file, err := emitFile(t.Context(), emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})
			require.NoError(t, err)
			requireValidGo(t, file.Content)

			source := string(file.Content)
			require.Contains(t, source, `driver.Named("author", params.Author)`)
			require.Contains(t, source, `driver.Named("title", params.Title)`)
		})
	}
}

func TestCountConstantIsTheCountTemplateInThePositionalForm(t *testing.T) {
	tests := []struct {
		name     string
		emission *gogen.Emission
		want     string
	}{
		{name: "a numbered form", emission: indexedEmission(true), want: "const searchPostsCountSQL = `SELECT COUNT(*) FROM posts WHERE author = ?1`"},
		{name: "a form binding by name writes the positional form", emission: bracedEmission(), want: "const searchPostsCountSQL = `SELECT COUNT(*) FROM posts WHERE author = $1`"},
		{name: "an anonymous form", emission: indexedEmission(false), want: "const searchPostsCountSQL = `SELECT COUNT(*) FROM posts WHERE author = ?`"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			query := &sqlmodel.AnalysedQuery{
				Name:           "SearchPosts",
				Filename:       "posts.sql",
				Command:        sqlmodel.QueryCommandMany,
				DynamicRuntime: true,
				Parameters:     []sqlmodel.QueryParameter{textParam("author", 1)},
				OutputColumns:  []sqlmodel.OutputColumn{textColumn("id", false)},
				Template: template(text("SELECT id FROM posts WHERE "), clause(sqlmodel.SegmentFilter, text("author = "), parameter(0)),
					clause(sqlmodel.SegmentOrderBy), clause(sqlmodel.SegmentPagination)),
				CountTemplate: new(template(text("SELECT COUNT(*) FROM posts WHERE "), clause(sqlmodel.SegmentFilter, text("author = "), parameter(0)))),
			}

			file, err := emitFile(t.Context(), test.emission, textMappings(), query.Filename, []*sqlmodel.AnalysedQuery{query})

			require.NoError(t, err)
			requireValidGo(t, file.Content)
			require.Contains(t, string(file.Content), test.want)
		})
	}
}

func TestEmitQueriesGroupsByFilenameAndEmitsSharedHelpers(t *testing.T) {
	emission := indexedEmission(true)

	queries := []*sqlmodel.AnalysedQuery{
		{
			Name:          "GetUser",
			Filename:      "users.sql",
			SQL:           "SELECT id FROM users WHERE id = ?1",
			Command:       sqlmodel.QueryCommandOne,
			ReadOnly:      true,
			Parameters:    []sqlmodel.QueryParameter{textParam("id", 1)},
			OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
		},
		{
			Name:     "PostsByIDs",
			Filename: "posts.sql",
			SQL:      "SELECT id FROM posts WHERE id IN (?1)",
			Command:  sqlmodel.QueryCommandMany,
			ReadOnly: true,
			Parameters: []sqlmodel.QueryParameter{
				{
					Name:    "ids",
					Number:  1,
					IsSlice: true,
					SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
					Kind:    sqlmodel.ParameterDirectiveParam,
				},
			},
			OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false)},
		},
	}

	files, err := emitQueriesFor(t.Context(), emission, queries)
	require.NoError(t, err)

	usersFile := generatedFileByName(t, files, "users.sql.go")
	postsFile := generatedFileByName(t, files, "posts.sql.go")
	requireValidGo(t, usersFile.Content)
	requireValidGo(t, postsFile.Content)

	bindLimits := generatedFileByName(t, files, "bind_limits.go")
	sliceHelpers := generatedFileByName(t, files, "slice_helpers.go")
	requireValidGo(t, bindLimits.Content)
	requireValidGo(t, sliceHelpers.Content)

	assert.Contains(t, string(bindLimits.Content), "const sglMaxBindVariables = 999")
	assert.Contains(t, string(sliceHelpers.Content), "func sglList(mark sglListMark, first, count int) []sglEdit {")
	assert.Contains(t, string(bindLimits.Content), "func sglSplice(query string, edits ...[]sglEdit) string {")
}

func TestEmitQueriesEmitsRuntimeBuilderHelperWhenDynamicRuntimeUsed(t *testing.T) {
	emission := indexedEmission(true)
	query := runtimeBuilderQuery()

	files, err := emitQueriesFor(t.Context(), emission, []*sqlmodel.AnalysedQuery{query})
	require.NoError(t, err)

	runtimeHelpers := generatedFileByName(t, files, "runtime_helpers.go")
	bindLimits := generatedFileByName(t, files, "bind_limits.go")
	requireValidGo(t, runtimeHelpers.Content)
	requireValidGo(t, bindLimits.Content)
	assert.Contains(t, string(runtimeHelpers.Content), "func sglNewPredicate(")
}

func TestEmitQueriesReturnsNilForNoQueries(t *testing.T) {
	files, err := emitQueriesFor(t.Context(), indexedEmission(false), nil)
	require.NoError(t, err)
	assert.Nil(t, files)
}
