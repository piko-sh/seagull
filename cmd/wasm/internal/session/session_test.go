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

package session_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/cmd/wasm/internal/session"
	"seagull.sh/seagull/sdk/emit"
)

const (
	starterQueries    = "db/queries/example.sql"
	examplesDirectory = "../../../../examples"
)

var customEmitterVariants = []string{"embedding/052_custom_emitter", "embedding/053_custom_sgl_binary"}

type panickingContext struct {
	context.Context
}

func (panickingContext) Err() error {
	panic("injected failure")
}

func starter(t *testing.T, dialectName string) map[string]string {
	t.Helper()
	files, err := session.Starter(dialectName)
	require.NoError(t, err)
	return files
}

func run(t *testing.T, files map[string]string, override session.Override) session.Response {
	t.Helper()
	return session.Run(t.Context(), session.Request{Files: files, Override: override, V: session.ProtocolVersion})
}

func fileNames(response session.Response) []string {
	names := make([]string, 0, len(response.Files))
	for _, file := range response.Files {
		names = append(names, file.Name)
	}
	return names
}

func findFile(t *testing.T, response session.Response, name string) session.FileView {
	t.Helper()
	for _, file := range response.Files {
		if file.Name == name {
			return file
		}
	}
	require.Failf(t, "file not found", "no generated file named %q among %v", name, fileNames(response))
	return session.FileView{}
}

func findQuery(t *testing.T, response session.Response, name string) session.QueryView {
	t.Helper()
	for _, query := range response.Queries {
		if query.Name == name {
			return query
		}
	}
	require.Failf(t, "query not found", "no query named %q", name)
	return session.QueryView{}
}

func findDiagnostic(t *testing.T, response session.Response, code string) session.DiagnosticView {
	t.Helper()
	for _, diagnostic := range response.Diagnostics {
		if diagnostic.Code == code {
			return diagnostic
		}
	}
	require.Failf(t, "diagnostic not found", "no %s diagnostic among %d", code, len(response.Diagnostics))
	return session.DiagnosticView{}
}

func withoutTimings(response session.Response) session.Response {
	response.DurationMS = 0
	response.Stages = nil
	return response
}

func committedGoFiles(t *testing.T, directory string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	files := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		require.NoError(t, err)
		files[entry.Name()] = string(content)
	}
	return files
}

func TestRunGeneratesTheSQLiteStarter(t *testing.T) {
	t.Parallel()

	response := run(t, starter(t, "sqlite"), session.Override{})

	require.True(t, response.OK, response.Error)
	require.False(t, response.Internal)
	require.Empty(t, response.Error)
	require.Equal(t, session.ProtocolVersion, response.V)
	require.Equal(t, []string{"example.sql.go", "models.go", "querier.go"}, fileNames(response))
	require.Equal(t, session.Counts{Errors: 0, Warnings: 0, Hints: 0}, response.Counts)
	require.Equal(t, &session.ConfigView{
		File:               "sgl.yaml",
		Database:           "app",
		Dialect:            "sqlite",
		Driver:             "database/sql",
		Package:            "store",
		Migrations:         "db/migrations",
		Queries:            "db/queries",
		Warnings:           []string{},
		SearchPath:         []string{},
		StructTags:         map[string]string{},
		PackageNames:       map[string]string{},
		GroupByUseNulls:    nil,
		ANSIQuotes:         nil,
		InterfaceName:      "Querier",
		JSONCase:           "",
		Prepared:           false,
		SkipFailingQueries: false,
		NonNullElements:    false,
		KeepStars:          false,
		Interface:          false,
		JSONOmitEmpty:      false,
		JSONOmit:           false,
		EmptySlices:        false,
		DocComments:        false,
		ReuseModels:        false,
		WrapErrors:         false,
	}, response.Config)
	require.False(t, response.Partial)
	require.NotEmpty(t, response.Stages)
	require.Equal(t, "generate", response.Stages[0].Stage)

	lookup := findQuery(t, response, "GetAuthor")
	require.Equal(t, "one", lookup.Command)
	require.Equal(t, starterQueries, lookup.File)
	require.Equal(t, 1, lookup.Line)
	require.True(t, lookup.ReadOnly)
	require.Equal(t, "SELECT id, name, bio FROM authors WHERE id = ?1;", lookup.SQLSent)
	require.Equal(t, "generated", lookup.SQLSentFrom)
	require.Equal(t, "getAuthorSQL", lookup.SQLSentConst)
	require.Equal(t, "example.sql.go", lookup.GoFile)
	require.Positive(t, lookup.GoLine)
	require.Equal(t, "func (queries *Queries) GetAuthor(ctx context.Context, id int64) (GetAuthorRow, error)", lookup.Signature)
	require.Equal(t, "GetAuthorRow", lookup.RowType)
	require.Equal(t, "own", lookup.RowKind)
	require.False(t, lookup.Skipped)
	require.Contains(t, lookup.Types, "type GetAuthorRow struct")
	require.Equal(t, []session.ParamView{{Default: nil, MaxLimit: nil, Name: "id", SQLType: "bigint", GoType: "int64", Number: 1, Nullable: false, Slice: false, Optional: false}}, lookup.Params)
	require.Equal(t, []session.ResultColumnView{
		{Name: "id", SQLType: "bigint", Category: "integer", GoType: "int64", Source: "authors.id", Embedded: "", EmbedTable: "", Nullable: false, Outer: false},
		{Name: "name", SQLType: "text", Category: "text", GoType: "string", Source: "authors.name", Embedded: "", EmbedTable: "", Nullable: false, Outer: false},
		{Name: "bio", SQLType: "text", Category: "text", GoType: "*string", Source: "authors.bio", Embedded: "", EmbedTable: "", Nullable: true, Outer: false},
	}, lookup.Columns)

	create := findQuery(t, response, "CreateAuthor")
	require.Equal(t, []string{"string", "*string"}, []string{create.Params[0].GoType, create.Params[1].GoType})

	require.NotNil(t, response.Catalogue)
	require.Equal(t, "main", response.Catalogue.DefaultSchema)
	require.Len(t, response.Catalogue.Schemas, 1)
	table := response.Catalogue.Schemas[0].Tables[0]
	require.Equal(t, "authors", table.Name)
	require.Equal(t, "db/migrations/0001_init.up.sql", table.Origin)
	require.Equal(t, session.CatalogueColumnView{Name: "id", Type: "bigint", Comment: "", Nullable: false, PrimaryKey: true, HasDefault: true, Generated: false}, table.Columns[0])
	require.Empty(t, table.Comment)
	require.Empty(t, table.PartitionOf)
}

func TestRunPresentsDiagnosticsAsVetPrintsThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		queries string
		code    string
		text    string
		line    string
		help    string
		width   int
	}{
		{
			name:    "unknown column",
			queries: "-- sgl.query(ListAuthors, many)\nSELECT id, tilte FROM authors;\n",
			code:    "Q001",
			text: "db/queries/example.sql:2:12: warning[Q001]: unknown column \"tilte\"\n" +
				"   |\n" +
				" 2 | SELECT id, tilte FROM authors;\n" +
				"   |            ^^^^^\n",
			line:  "2",
			width: 5,
			help:  "",
		},
		{
			name:    "misspelt command",
			queries: "-- sgl.query(ListAuthors, mnay)\nSELECT id FROM authors;\n",
			code:    "Q028",
			text: "db/queries/example.sql:1:27: error[Q028]: invalid value \"mnay\" for keyword argument \"command\"; " +
				"allowed: one, many, exec, execresult, execrows, batch, stream, copyfrom, asyncexec\n" +
				"   |\n" +
				" 1 | -- sgl.query(ListAuthors, mnay)\n" +
				"   |                           ^^^^\n" +
				"help: did you mean \"many\"?\n",
			line:  "1",
			width: 4,
			help:  "did you mean \"many\"?",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := starter(t, "sqlite")
			files[starterQueries] = testCase.queries

			response := run(t, files, session.Override{})

			require.True(t, response.OK, response.Error)
			diagnostic := findDiagnostic(t, response, testCase.code)
			require.Equal(t, testCase.text, diagnostic.Text)
			require.Equal(t, starterQueries, diagnostic.File)
			require.Equal(t, testCase.width, diagnostic.CaretWidth)
			require.Equal(t, testCase.help, diagnostic.Help)
			require.Equal(t, testCase.line, diagnostic.LineNo)
			require.Equal(t, " ", diagnostic.Gutter)
			require.NotEmpty(t, diagnostic.Title)
			require.NotContains(t, diagnostic.Message, testCase.code+": ")
		})
	}
}

func TestRunStopsGenerationOnErrors(t *testing.T) {
	t.Parallel()

	files := starter(t, "sqlite")
	files[starterQueries] = "-- sgl.query(ListAuthors, mnay)\nSELECT id FROM authors;\n"

	response := run(t, files, session.Override{})

	require.True(t, response.OK, response.Error)
	require.Equal(t, 1, response.Counts.Errors)
	require.Empty(t, response.Files)
	query := findQuery(t, response, "ListAuthors")
	require.Equal(t, "as written", query.SQLSentFrom)
	require.Empty(t, query.SQLSentConst)
	require.Equal(t, "-- sgl.query(ListAuthors, mnay)\nSELECT id FROM authors;", query.SQLSent)
	require.Empty(t, query.Signature)
}

func TestRunGeneratesPostgresWithPgx(t *testing.T) {
	t.Parallel()

	response := run(t, starter(t, "postgres"), session.Override{Prepared: nil, Dialect: "", Driver: "pgx"})

	require.True(t, response.OK, response.Error)
	require.Equal(t, "pgx", response.Config.Driver)
	require.Equal(t, "postgres", response.Config.Dialect)
	require.Equal(t, []string{"example.sql.go", "models.go", "querier.go"}, fileNames(response))
	require.Contains(t, response.Files[2].Content, "github.com/jackc/pgx/v5")
	lookup := findQuery(t, response, "GetAuthor")
	require.Equal(t, "SELECT id, name, bio FROM authors WHERE id = $1;", lookup.SQLSent)
	require.Equal(t, "func (queries *Queries) GetAuthor(ctx context.Context, id int64) (GetAuthorRow, error)", lookup.Signature)
	require.Equal(t, "public", response.Catalogue.DefaultSchema)
}

func TestRunDescribesGeneratedShapes(t *testing.T) {
	t.Parallel()

	files := starter(t, "postgres")
	files["db/migrations/0002_books.up.sql"] = "CREATE TABLE books (\n" +
		"    id BIGSERIAL PRIMARY KEY,\n" +
		"    author_id BIGINT NOT NULL REFERENCES authors (id),\n" +
		"    title TEXT NOT NULL\n" +
		");\n"
	files["db/queries/books.sql"] = "-- sgl.query(GetBookWithAuthor, one)\n" +
		"-- sgl.embed(authors, from: a)\n" +
		"SELECT b.id, b.title, a.id, a.name\n" +
		"FROM books b\n" +
		"INNER JOIN authors a ON a.id = b.author_id\n" +
		"WHERE b.id = $1;\n" +
		"\n" +
		"-- sgl.query(SearchBooks, many, dynamic: runtime)\n" +
		"SELECT id, title FROM books\n" +
		"\n" +
		"-- sgl.query(ListBooks, many)\n" +
		"-- $1 as sgl.param(ids, type: int8, kind: slice)\n" +
		"-- sgl.sortable(order_by, [title, id])\n" +
		"-- $2 as sgl.param(page_size, default: 10, max: 100)\n" +
		"SELECT id, title FROM books WHERE id IN ($1) LIMIT $2;\n"

	response := run(t, files, session.Override{Prepared: nil, Dialect: "", Driver: "pgx"})

	require.True(t, response.OK, response.Error)
	require.Zero(t, response.Counts.Errors, response.Diagnostics)

	embedded := findQuery(t, response, "GetBookWithAuthor")
	require.Equal(t, "db/queries/books.sql", embedded.File)
	require.Equal(t, []string{"int64", "string", "int64", "string"}, []string{
		embedded.Columns[0].GoType, embedded.Columns[1].GoType, embedded.Columns[2].GoType, embedded.Columns[3].GoType,
	})
	require.Equal(t, "authors", embedded.Columns[2].Embedded)
	require.Contains(t, embedded.Types, "type GetBookWithAuthorAuthors struct")

	dynamic := findQuery(t, response, "SearchBooks")
	require.True(t, dynamic.Dynamic)
	require.Equal(t, "SELECT id, title FROM books", dynamic.SQLSent)
	require.Equal(t, "generated", dynamic.SQLSentFrom)
	require.Equal(t, []string{"id", "title"}, dynamic.AllowedColumns)
	require.Equal(t, "func (queries *Queries) SearchBooks() *SearchBooksBuilder", dynamic.Signature)
	require.Contains(t, dynamic.Types, "type SearchBooksRow struct")
	require.NotContains(t, dynamic.Types, "type SearchBooksBuilder struct")
	require.NotEmpty(t, dynamic.CountSQL)

	list := findQuery(t, response, "ListBooks")
	require.Equal(t, []string{"title", "id"}, list.Sortable)
	require.Equal(t, "[]int64", list.Params[0].GoType)
	require.True(t, list.Params[0].Slice)
	require.Equal(t, "int", list.Params[1].GoType)
	require.Equal(t, 10, *list.Params[1].Default)
	require.Equal(t, 100, *list.Params[1].MaxLimit)
}

func TestRunKeepsSelfJoinEmbedsApart(t *testing.T) {
	t.Parallel()

	files := starter(t, "postgres")
	files["db/migrations/0002_staff.up.sql"] = "CREATE TABLE staff (\n" +
		"    id BIGSERIAL PRIMARY KEY,\n" +
		"    name TEXT NOT NULL,\n" +
		"    manager_id BIGINT REFERENCES staff (id)\n" +
		");\n"
	files["db/queries/staff.sql"] = "-- sgl.query(GetStaffWithManager, one)\n" +
		"-- sgl.embed(staff, from: s, as: member)\n" +
		"-- sgl.embed(staff, from: m, as: manager)\n" +
		"SELECT s.id, s.name, m.id, m.name\n" +
		"FROM staff s\n" +
		"LEFT JOIN staff m ON m.id = s.manager_id\n" +
		"WHERE s.id = $1;\n"

	response := run(t, files, session.Override{})

	require.True(t, response.OK, response.Error)
	require.Zero(t, response.Counts.Errors, response.Diagnostics)
	query := findQuery(t, response, "GetStaffWithManager")
	require.Equal(t, []session.ResultColumnView{
		{Name: "id", SQLType: "int8", Category: "integer", GoType: "int64", Source: "staff.id", Embedded: "member", EmbedTable: "staff", Nullable: false, Outer: false},
		{Name: "name", SQLType: "text", Category: "text", GoType: "string", Source: "staff.name", Embedded: "member", EmbedTable: "staff", Nullable: false, Outer: false},
		{Name: "id", SQLType: "int8", Category: "integer", GoType: "*int64", Source: "staff.id", Embedded: "manager", EmbedTable: "staff", Nullable: true, Outer: true},
		{Name: "name", SQLType: "text", Category: "text", GoType: "*string", Source: "staff.name", Embedded: "manager", EmbedTable: "staff", Nullable: true, Outer: true},
	}, query.Columns)
	require.Contains(t, query.Types, "type GetStaffWithManagerManager struct")
}

func TestSentSQLAgreesWithDescribe(t *testing.T) {
	t.Parallel()

	files := starter(t, "postgres")
	files["db/queries/more.sql"] = "-- sgl.query(SearchAuthors, many, dynamic: runtime)\n" +
		"SELECT id, name FROM authors\n" +
		"\n" +
		"-- sgl.query(ListAuthorsSorted, many)\n" +
		"-- sgl.sortable(order_by, [name, id])\n" +
		"SELECT id, name FROM authors\n" +
		"\n" +
		"-- sgl.query(AddAuthors, copyfrom)\n" +
		"INSERT INTO authors (name, bio) VALUES ($1, $2);\n"

	for _, driver := range []string{"database/sql", "pgx"} {
		response := run(t, files, session.Override{Prepared: nil, Dialect: "", Driver: driver})
		require.True(t, response.OK, response.Error)
		generated := make([]emit.GeneratedFile, 0, len(response.Files))
		for _, file := range response.Files {
			generated = append(generated, emit.GeneratedFile{Name: file.Name, Content: []byte(file.Content)})
		}
		index := assemble.NewGoIndex(generated)
		for _, query := range response.Queries {
			identifier, sql, found := index.SQLConst(query.Name)
			require.Equal(t, identifier, query.SQLSentConst, "%s with %s", query.Name, driver)
			if found {
				require.Equal(t, sql, query.SQLSent, "%s with %s", query.Name, driver)
			}
		}
	}
}

func TestRunRejectsUnusableSettings(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		dialect  string
		override session.Override
		message  string
	}{
		{
			name:     "pgx needs postgres",
			dialect:  "sqlite",
			override: session.Override{Prepared: nil, Dialect: "", Driver: "pgx"},
			message:  `database "app": go.driver pgx needs a dialect that runs on pgx, such as postgres, not sqlite`,
		},
		{
			name:     "clickhouse needs clickhouse",
			dialect:  "postgres",
			override: session.Override{Prepared: nil, Dialect: "", Driver: "clickhouse"},
			message:  `database "app": go.driver clickhouse needs a dialect that runs on clickhouse-go, such as clickhouse, not postgres`,
		},
		{
			name:     "prepared needs database/sql",
			dialect:  "postgres",
			override: session.Override{Prepared: new(true), Dialect: "", Driver: "pgx"},
			message:  `database "app": go.prepared applies only to the database/sql driver, not pgx`,
		},
		{
			name:     "unknown driver",
			dialect:  "sqlite",
			override: session.Override{Prepared: nil, Dialect: "", Driver: "odbc"},
			message:  `database "app": unknown go.driver "odbc"; use database/sql, pgx or clickhouse`,
		},
		{
			name:     "unknown dialect",
			dialect:  "sqlite",
			override: session.Override{Prepared: nil, Dialect: "oracle", Driver: ""},
			message:  `database "app": unknown dialect "oracle"; this sgl knows clickhouse, cockroachdb, duckdb`,
		},
		{
			name:     "dialect overridden onto pgx",
			dialect:  "postgres",
			override: session.Override{Prepared: nil, Dialect: "mysql", Driver: "pgx"},
			message:  `go.driver pgx needs a dialect that runs on pgx, such as postgres, not mysql`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			response := run(t, starter(t, testCase.dialect), testCase.override)

			require.False(t, response.OK)
			require.False(t, response.Internal)
			require.Contains(t, response.Error, testCase.message)
			require.Empty(t, response.Files)
			require.NotNil(t, response.Config)
		})
	}
}

func TestRunRejectsUnusableInput(t *testing.T) {
	t.Parallel()

	many := map[string]string{}
	for index := range session.MaxFiles + 1 {
		many[fmt.Sprintf("db/queries/%02d.sql", index)] = ""
	}
	large := map[string]string{}
	for index := range 5 {
		large[fmt.Sprintf("db/queries/%d.sql", index)] = strings.Repeat("-", session.MaxFileBytes-1)
	}
	testCases := []struct {
		name    string
		request session.Request
		message string
	}{
		{
			name:    "too many files",
			request: session.Request{Files: many, Override: session.Override{}, V: 0},
			message: "the playground takes at most 64",
		},
		{
			name:    "file too large",
			request: session.Request{Files: map[string]string{"db/queries/a.sql": strings.Repeat("-", session.MaxFileBytes+1)}, Override: session.Override{}, V: 0},
			message: "files of at most 262144 bytes",
		},
		{
			name:    "project too large",
			request: session.Request{Files: large, Override: session.Override{}, V: 0},
			message: "the project is larger than 1048576 bytes",
		},
		{
			name:    "parent directory",
			request: session.Request{Files: map[string]string{"db/../../etc/passwd": ""}, Override: session.Override{}, V: 0},
			message: "must not use ..",
		},
		{
			name:    "absolute path",
			request: session.Request{Files: map[string]string{"/etc/passwd": ""}, Override: session.Override{}, V: 0},
			message: "must have a relative, slash-separated path",
		},
		{
			name:    "empty path",
			request: session.Request{Files: map[string]string{"": ""}, Override: session.Override{}, V: 0},
			message: "empty path",
		},
		{
			name:    "repeated path",
			request: session.Request{Files: map[string]string{"db/a.sql": "", "db//a.sql": ""}, Override: session.Override{}, V: 0},
			message: `file "db/a.sql" is given twice`,
		},
		{
			name:    "migrations outside the project",
			request: session.Request{Files: map[string]string{"sgl.yaml": "version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n    migrations: ../elsewhere\n"}, Override: session.Override{}, V: 0},
			message: `migrations "../elsewhere" must be a directory inside the project`,
		},
		{
			name:    "invalid configuration",
			request: session.Request{Files: map[string]string{"sgl.yaml": "version: 1\ndatabases:\n  - name: app\n    dialet: sqlite\n"}, Override: session.Override{}, V: 0},
			message: "sgl.yaml: invalid configuration",
		},
		{
			name:    "future protocol",
			request: session.Request{Files: map[string]string{}, Override: session.Override{}, V: session.ProtocolVersion + 1},
			message: "protocol version 2 is not supported",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			response := session.Run(t.Context(), testCase.request)

			require.False(t, response.OK)
			require.False(t, response.Internal)
			require.Contains(t, response.Error, testCase.message)
		})
	}
}

func TestRunRecoversFromPanics(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer
	ctx := panickingContext{Context: seagull.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logged, nil)))}

	response := session.Run(ctx, session.Request{Files: starter(t, "sqlite"), Override: session.Override{}, V: 0})

	require.False(t, response.OK)
	require.True(t, response.Internal)
	require.Contains(t, response.Error, "panic: injected failure")
	require.NotContains(t, response.Error, "goroutine")
	require.Equal(t, session.ProtocolVersion, response.V)
	require.NotNil(t, response.Files)
	require.Equal(t, 1, strings.Count(logged.String(), `"msg":"recovered a panic"`), logged.String())
	require.Contains(t, logged.String(), `"stack":"goroutine`)
}

func TestRunAppliesConfiguredTypeOverrides(t *testing.T) {
	t.Parallel()

	files := starter(t, "sqlite")
	files["sgl.yaml"] = "version: 1\n" +
		"databases:\n" +
		"  - name: app\n" +
		"    dialect: sqlite\n" +
		"    go:\n" +
		"      package: authors\n" +
		"      types:\n" +
		"        overrides:\n" +
		"          - column: authors.name\n" +
		"            go: example.com/names.Name\n"

	response := run(t, files, session.Override{})

	require.True(t, response.OK, response.Error)
	require.Equal(t, "authors", response.Config.Package)
	lookup := findQuery(t, response, "GetAuthor")
	require.Equal(t, "names.Name", lookup.Columns[1].GoType)
	models := findFile(t, response, "models.go")
	require.Contains(t, models.Content, `"example.com/names"`)
	require.Contains(t, models.Content, "package authors")
}

func TestRunUsesDefaultsWithoutConfiguration(t *testing.T) {
	t.Parallel()

	files := starter(t, "postgres")
	delete(files, "sgl.yaml")

	response := run(t, files, session.Override{Prepared: nil, Dialect: "postgres", Driver: ""})

	require.True(t, response.OK, response.Error)
	require.Equal(t, "", response.Config.File)
	require.Equal(t, "playground", response.Config.Database)
	require.Equal(t, "postgres", response.Config.Dialect)
	require.Equal(t, "database/sql", response.Config.Driver)
	require.NotEmpty(t, response.Files)
}

func TestRunGeneratesAnEmptyProject(t *testing.T) {
	t.Parallel()

	response := run(t, map[string]string{}, session.Override{})

	require.True(t, response.OK, response.Error)
	require.Equal(t, "sqlite", response.Config.Dialect)
	require.Equal(t, []string{"querier.go"}, fileNames(response))
	require.Empty(t, response.Queries)
}

func TestRunWarnsAboutIgnoredConfiguration(t *testing.T) {
	t.Parallel()

	files := starter(t, "sqlite")
	files["sgl.yaml"] = "version: 1\n" +
		"databases:\n" +
		"  - name: app\n" +
		"    dialect: sqlite\n" +
		"    schema_from: file:app.db\n" +
		"  - name: audit\n" +
		"    dialect: sqlite\n" +
		"    go:\n" +
		"      out: internal/audit\n"

	response := run(t, files, session.Override{})

	require.True(t, response.OK, response.Error)
	require.Equal(t, []string{
		`sgl.yaml declares 2 databases; the playground generates the first, "app"`,
		`database "app": schema_from is ignored; the playground builds the schema from the migrations`,
	}, response.Config.Warnings)
}

func TestRunMapsDiagnosticsBackToTheirDirectories(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"sgl.yaml":                "version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n    migrations: schema\n    queries: sql/app\n",
		"schema/0001_init.up.sql": "CREATE TABLE authors (id INTEGER PRIMARY KEY, name TEXT NOT NULL);\nCREATE TABLE broken (id INTEGER,, name);\n",
		"sql/app/authors.sql":     "-- sgl.query(ListAuthors, mnay)\nSELECT id FROM authors;\n",
	}

	response := run(t, files, session.Override{})

	require.True(t, response.OK, response.Error)
	require.Equal(t, "schema/0001_init.up.sql", findDiagnostic(t, response, "Q010").File)
	require.Equal(t, "sql/app/authors.sql", findDiagnostic(t, response, "Q028").File)
	require.Equal(t, "sql/app/authors.sql", findQuery(t, response, "ListAuthors").File)
	require.Equal(t, "schema", response.Config.Migrations)
	require.Equal(t, "sql/app", response.Config.Queries)
}

func TestRunIsDeterministic(t *testing.T) {
	t.Parallel()

	files := starter(t, "postgres")
	files["db/queries/more.sql"] = "-- sgl.query(CountAuthors, one)\nSELECT count(*) AS total FROM authors;\n\n-- sgl.query(Broken, one)\nSELECT nope FROM authors;\n"

	first, err := json.Marshal(withoutTimings(run(t, files, session.Override{Prepared: nil, Dialect: "", Driver: "pgx"})))
	require.NoError(t, err)
	second, err := json.Marshal(withoutTimings(run(t, files, session.Override{Prepared: nil, Dialect: "", Driver: "pgx"})))
	require.NoError(t, err)

	require.JSONEq(t, string(first), string(second))
	require.Equal(t, string(first), string(second))
}

func TestPlaygroundMatchesTheCommittedExamples(t *testing.T) {
	t.Parallel()

	directories, err := filepath.Glob(filepath.Join(examplesDirectory, "*", "[0-9][0-9][0-9]_*"))
	require.NoError(t, err)

	tested := 0
	for _, directory := range directories {
		variant, err := filepath.Rel(examplesDirectory, directory)
		require.NoError(t, err)
		configuration, readErr := os.ReadFile(filepath.Join(directory, "sgl.yaml"))
		if readErr != nil {
			continue
		}
		tested++
		t.Run(variant, func(t *testing.T) {
			t.Parallel()

			if slices.Contains(customEmitterVariants, filepath.ToSlash(variant)) {
				t.Skipf("%s generates its code with its own emitter", variant)
			}
			loaded, err := config.Parse(configuration, false)
			require.NoError(t, err)
			database := loaded.Databases[0]
			if path.Clean(database.Migrations) != "db/migrations" || path.Clean(database.Queries) != "db/queries" {
				t.Skipf("%s keeps its SQL outside db/migrations and db/queries", variant)
			}
			output := filepath.Join(directory, filepath.FromSlash(database.Go.Out))
			if _, statErr := os.Stat(output); statErr != nil {
				t.Skipf("%s has no committed generated code at %s", variant, database.Go.Out)
			}

			request, err := session.LoadRequest(os.DirFS(directory))
			require.NoError(t, err)
			response := session.Run(t.Context(), request)

			require.True(t, response.OK, response.Error)
			require.Zero(t, response.Counts.Errors, response.Diagnostics)
			generated := map[string]string{}
			for _, file := range response.Files {
				generated[file.Name] = file.Content
			}
			committed := committedGoFiles(t, output)
			require.Equal(t, slices.Sorted(maps.Keys(committed)), slices.Sorted(maps.Keys(generated)))
			for name, content := range committed {
				require.Equal(t, content, generated[name], "%s/%s/%s differs from the playground's output", variant, database.Go.Out, name)
			}
		})
	}
	require.Positive(t, tested)
}
