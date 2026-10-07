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

package assemble

import (
	"go/parser"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/sdk/emit"
)

const generatedQueries = "package store\n\n" +
	"import \"context\"\n\n" +
	"const sglMaxBindVariables = 65535\n\n" +
	"const (\n" +
	"\tgetauthor = `SELECT id, name\n" +
	"FROM authors WHERE id = ?;`\n" +
	"\tdeleteauthor = \"DELETE FROM authors WHERE note = '`'\"\n" +
	")\n\n" +
	"const a, b = \"first\", \"second\"\n\n" +
	"const computed = getauthor + \";\"\n\n" +
	"type GetAuthorRow struct {\n\tID   int64\n\tName string\n}\n\n" +
	"type Queries struct{ db DBTX }\n\n" +
	"type DBTX interface{}\n\n" +
	"func (queries *Queries) GetAuthor(ctx context.Context, id int64) (GetAuthorRow, error) {\n" +
	"\tlimit := sglMaxBindVariables\n" +
	"\t_ = limit\n" +
	"\treturn queries.db.QueryRowContext(ctx, getauthor, id).Scan()\n" +
	"}\n\n" +
	"func (queries *Queries) DeleteAuthor(ctx context.Context) error {\n" +
	"\treturn queries.exec(ctx, deleteauthor)\n" +
	"}\n\n" +
	"func (queries *Queries) Pair(ctx context.Context) error {\n" +
	"\treturn queries.exec(ctx, b)\n" +
	"}\n\n" +
	"func (queries *Queries) Selected(ctx context.Context) error {\n" +
	"\treturn queries.getauthor.exec(ctx)\n" +
	"}\n\n" +
	"func (queries *Queries) Computed(ctx context.Context) error {\n" +
	"\treturn queries.exec(ctx, computed)\n" +
	"}\n\n" +
	"func GetAuthor() string { return getauthor }\n\n" +
	"const searchAuthorsSQL = `SELECT id, name FROM authors`\n\n" +
	"type SearchAuthorsBuilder struct{ queries *Queries }\n\n" +
	"type OtherBuilder struct{}\n\n" +
	"func (queries *Queries) SearchAuthors(ctx context.Context) *SearchAuthorsBuilder {\n" +
	"\treturn &SearchAuthorsBuilder{queries: queries}\n" +
	"}\n\n" +
	"func (builder *OtherBuilder) buildQuery() string { return deleteauthor }\n\n" +
	"func (builder *SearchAuthorsBuilder) buildQuery() string { return searchAuthorsSQL }\n"

var generatedFiles = []emit.GeneratedFile{
	{Name: "README.md", Content: []byte("const readme = `not Go`")},
	{Name: "broken.go", Content: []byte("package store\n\nfunc (")},
	{Name: "queries.sql.go", Content: []byte(generatedQueries)},
}

func TestGoIndexSQLConst(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		method           string
		expectIdentifier string
		expectSQL        string
		expectFound      bool
	}{
		{method: "GetAuthor", expectIdentifier: "getauthor", expectSQL: "SELECT id, name\nFROM authors WHERE id = ?;", expectFound: true},
		{method: "DeleteAuthor", expectIdentifier: "deleteauthor", expectSQL: "DELETE FROM authors WHERE note = '`'", expectFound: true},
		{method: "Pair", expectIdentifier: "b", expectSQL: "second", expectFound: true},
		{method: "SearchAuthors", expectIdentifier: "searchAuthorsSQL", expectSQL: "SELECT id, name FROM authors", expectFound: true},
		{method: "Selected", expectFound: false},
		{method: "Computed", expectFound: false},
		{method: "Missing", expectFound: false},
	}

	index := NewGoIndex(generatedFiles)
	for _, testCase := range testCases {
		t.Run(testCase.method, func(t *testing.T) {
			t.Parallel()

			identifier, sql, found := index.SQLConst(testCase.method)
			require.Equal(t, testCase.expectFound, found)
			require.Equal(t, testCase.expectIdentifier, identifier)
			require.Equal(t, testCase.expectSQL, sql)
		})
	}
}

func TestGoIndexSignature(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		files    []emit.GeneratedFile
		method   string
		expected string
	}{
		{
			name:   "a method returning a row struct",
			files:  generatedFiles,
			method: "GetAuthor",
			expected: "type GetAuthorRow struct {\n\tID   int64\n\tName string\n}\n\n" +
				"func (queries *Queries) GetAuthor(ctx context.Context, id int64) (GetAuthorRow, error)",
		},
		{name: "a method using no struct", files: generatedFiles, method: "DeleteAuthor", expected: "func (queries *Queries) DeleteAuthor(ctx context.Context) error"},
		{name: "a missing method", files: generatedFiles, method: "Missing", expected: ""},
		{name: "no files", files: nil, method: "GetAuthor", expected: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, NewGoIndex(testCase.files).Signature(testCase.method))
		})
	}
}

func TestGoIndexPrefersTheQuerierMethodOverABuilderMethodOfTheSameName(t *testing.T) {
	t.Parallel()

	const builderFirst = "package store\n\nimport \"context\"\n\n" +
		"const browseSQL = `SELECT id FROM authors`\n\n" +
		"const countSQL = `SELECT count(*) FROM authors`\n\n" +
		"type BrowseBuilder struct{}\n\n" +
		"type Queries struct{}\n\n" +
		"func (builder *BrowseBuilder) Count(ctx context.Context) (int64, error) { return 0, nil }\n\n" +
		"func (builder *BrowseBuilder) buildQuery() string { return browseSQL }\n\n" +
		"func (queries *Queries) Browse(ctx context.Context) *BrowseBuilder { return &BrowseBuilder{} }\n\n" +
		"func (queries *Queries) Count(ctx context.Context) (int64, error) { return queries.run(ctx, countSQL) }\n"
	files := []emit.GeneratedFile{{Name: "queries.sql.go", Content: []byte(builderFirst)}}

	testCases := []struct {
		name          string
		method        string
		wantSignature string
		wantConstant  string
	}{
		{name: "a query named like a builder method", method: "Count", wantSignature: "func (queries *Queries) Count(ctx context.Context) (int64, error)", wantConstant: "countSQL"},
		{name: "the runtime-built query", method: "Browse", wantSignature: "type BrowseBuilder struct{}\n\nfunc (queries *Queries) Browse(ctx context.Context) *BrowseBuilder", wantConstant: "browseSQL"},
	}

	index := NewGoIndex(files)
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.wantSignature, index.Signature(testCase.method))
			identifier, _, found := index.SQLConst(testCase.method)
			require.True(t, found)
			require.Equal(t, testCase.wantConstant, identifier)
		})
	}
}

func TestGoIndexLooksUpDeclarations(t *testing.T) {
	t.Parallel()

	index := NewGoIndex(generatedFiles)
	searchAuthors := index.Method("SearchAuthors")
	require.NotNil(t, searchAuthors)

	testCases := []struct {
		name     string
		actual   any
		expected any
	}{
		{name: "the builder's query constant", actual: index.BuilderConstant(searchAuthors, BuilderQueryMethod), expected: "searchAuthorsSQL"},
		{name: "no count query", actual: index.BuilderConstant(searchAuthors, BuilderCountMethod), expected: ""},
		{name: "no builder for a nil method", actual: index.BuilderConstant(nil, BuilderQueryMethod), expected: ""},
		{name: "no constant for a nil method", actual: index.SentConstant(nil), expected: ""},
		{name: "the builder's methods", actual: len(index.MethodsOf("SearchAuthorsBuilder")), expected: 1},
		{name: "a declared type", actual: index.TypeSpec("GetAuthorRow") != nil, expected: true},
		{name: "an undeclared type", actual: index.TypeSpec("Missing") == nil, expected: true},
		{name: "the file set positions the methods", actual: index.FileSet().Position(searchAuthors.Pos()).Filename, expected: "queries.sql.go"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, testCase.actual)
		})
	}
	value, found := index.Constant("getauthor")
	require.True(t, found)
	require.Equal(t, "SELECT id, name\nFROM authors WHERE id = ?;", value)
}

func TestGoIndexReadsGeneratedCode(t *testing.T) {
	t.Parallel()

	migrations := fstest.MapFS{"0001_init.up.sql": {Data: []byte("CREATE TABLE authors (id INTEGER PRIMARY KEY, name TEXT NOT NULL);\n")}}
	queries := fstest.MapFS{"authors.sql": {Data: []byte("-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = ?;\n")}}
	configuration := &config.Config{Directives: config.Directives{Prefixes: []string{"sgl"}}}
	options, built, err := Options(configuration, database("sqlite", driverDatabaseSQL), Factories{})
	require.NoError(t, err)
	generator, err := seagull.New(built, options...)
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), seagull.Input{Migrations: migrations, Queries: queries, Package: "store"})
	require.NoError(t, err)
	require.NoError(t, result.Err())

	index := NewGoIndex(result.Files)
	_, sql, found := index.SQLConst("GetAuthor")
	require.True(t, found)
	require.Equal(t, "SELECT id, name FROM authors WHERE id = ?1;", sql, "SQLite executes the numbered form (D15)")
	require.Contains(t, index.Signature("GetAuthor"), "func (queries *Queries) GetAuthor(ctx context.Context, id int64) (GetAuthorRow, error)")
}

func TestLocalTypeNamesListsEachUnqualifiedExportedNameOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression string
		want       []string
	}{
		{name: "a builtin", expression: "string", want: nil},
		{name: "a local type", expression: "*GetAuthorRow", want: []string{"GetAuthorRow"}},
		{name: "a qualified type", expression: "time.Time", want: nil},
		{name: "a function type in order of appearance", expression: "func(ctx context.Context, arguments ListParams) ([]Author, Page, error)", want: []string{"ListParams", "Author", "Page"}},
		{name: "a name repeated", expression: "map[Key][]Key", want: []string{"Key"}},
		{name: "a generic type with a qualified argument", expression: "Option[uuid.UUID]", want: []string{"Option"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			expression, err := parser.ParseExpr(testCase.expression)
			require.NoError(t, err)

			require.Equal(t, testCase.want, LocalTypeNames(expression))
		})
	}
}
