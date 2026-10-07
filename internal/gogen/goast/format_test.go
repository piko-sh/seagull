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

package goast

import (
	"context"
	"go/ast"
	"go/token"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func typedVariable(name string, typeExpression ast.Expr) ast.Decl {
	return &ast.GenDecl{
		Tok: token.VAR,
		Specs: []ast.Spec{&ast.ValueSpec{
			Names: []*ast.Ident{ast.NewIdent(name)},
			Type:  typeExpression,
		}},
	}
}

func TestFormatFileWithASTRendersPackageAndImportsWithoutHeader(t *testing.T) {
	t.Parallel()

	tracker := NewImportTracker()
	declaration := typedVariable("Identifier", tracker.AddType(sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"}))

	output, err := FormatFileWithAST("models", tracker, []ast.Decl{declaration})

	require.NoError(t, err)
	text := string(output)
	assert.True(t, strings.HasPrefix(text, "package models"))
	assert.Contains(t, text, "package models")
	assert.Contains(t, text, `"github.com/google/uuid"`)
	assert.Contains(t, text, "var Identifier uuid.UUID")
}

func TestFormatFileWithASTAliasesCollidingImports(t *testing.T) {
	t.Parallel()

	tracker := NewImportTracker()
	first := typedVariable("First", tracker.AddType(sqlmodel.GoType{Package: "example.com/one/uuid", Name: "UUID"}))
	second := typedVariable("Second", tracker.AddType(sqlmodel.GoType{Package: "example.com/two/uuid", Name: "UUID"}))

	output, err := FormatFileWithAST("models", tracker, []ast.Decl{first, second})

	require.NoError(t, err)
	text := string(output)
	assert.Contains(t, text, `"example.com/one/uuid"`)
	assert.Contains(t, text, `uuid2 "example.com/two/uuid"`)
	assert.Contains(t, text, "var Second uuid2.UUID")
}

func TestImportTrackerAddImportIsIdempotent(t *testing.T) {
	t.Parallel()

	tracker := NewImportTracker()
	tracker.AddImport("example.com/lib/v3")
	tracker.AddImport("example.com/lib/v3")

	assert.Equal(t, "lib", tracker.resolveAlias("example.com/lib/v3"))
	assert.Len(t, tracker.imports, 1)
}

func TestNaturalImportAlias(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		importPath string
		want       string
	}{
		{importPath: "github.com/jackc/pgx/v5", want: "pgx"},
		{importPath: "github.com/google/uuid", want: "uuid"},
		{importPath: "example.com/v2x", want: "v2x"},
		{importPath: "example.com/v", want: "v"},
		{importPath: "v2", want: "v2"},
	}

	for _, testCase := range testCases {
		assert.Equal(t, testCase.want, naturalImportAlias(testCase.importPath), testCase.importPath)
	}
}

func TestFormatFileWithASTOmitsImportsTheDeclarationsDoNotUse(t *testing.T) {
	t.Parallel()

	tracker := NewImportTracker()
	tracker.AddImport("database/sql")
	declaration := typedVariable("Identifier", tracker.AddType(sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"}))

	output, err := FormatFileWithAST("models", tracker, []ast.Decl{declaration})

	require.NoError(t, err)
	text := string(output)
	assert.Contains(t, text, `import "github.com/google/uuid"`)
	assert.NotContains(t, text, `"database/sql"`)
}

func TestFormatFileWithASTKeepsImportsReferencedByTheirPackageName(t *testing.T) {
	t.Parallel()

	tracker := NewImportTracker()
	tracker.AddImport("github.com/ClickHouse/clickhouse-go/v2")
	declaration := typedVariable("Named", &ast.SelectorExpr{X: ast.NewIdent("clickhouse"), Sel: ast.NewIdent("NamedValue")})

	output, err := FormatFileWithAST("store", tracker, []ast.Decl{declaration})

	require.NoError(t, err)
	assert.Contains(t, string(output), `import "github.com/ClickHouse/clickhouse-go/v2"`)
}

func TestFormatSourceLeavesImportsAsGiven(t *testing.T) {
	t.Parallel()

	fileSet := token.NewFileSet()
	file := &ast.File{Name: ast.NewIdent("store"), Decls: []ast.Decl{typedVariable("Count", ast.NewIdent("int"))}}

	output, err := FormatSource(fileSet, file)

	require.NoError(t, err)
	assert.Equal(t, "package store\n\nvar Count int\n", string(output))
}

func TestAssumedPackageName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		importPath string
		want       string
	}{
		{importPath: "github.com/ClickHouse/clickhouse-go/v2", want: "clickhouse"},
		{importPath: "github.com/go-sql-driver/mysql", want: "mysql"},
		{importPath: "gopkg.in/yaml.v3", want: "yaml"},
		{importPath: "github.com/google/uuid", want: "uuid"},
		{importPath: "example.com/go-thing", want: "thing"},
		{importPath: "database/sql", want: "sql"},
	}
	for _, testCase := range testCases {
		assert.Equal(t, testCase.want, assumedPackageName(testCase.importPath), testCase.importPath)
	}
}

func TestGroupImports(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "no imports",
			source: "package store\n\nvar Count int\n",
			want:   "package store\n\nvar Count int\n",
		},
		{
			name:   "lone import stays unparenthesised",
			source: "package store\n\nimport \"context\"\n\nvar _ context.Context\n",
			want:   "package store\n\nimport \"context\"\n\nvar _ context.Context\n",
		},
		{
			name:   "lone parenthesised import keeps its parentheses",
			source: "package store\n\nimport (\n\t\"context\"\n)\n",
			want:   "package store\n\nimport (\n\t\"context\"\n)\n",
		},
		{
			name:   "standard library sorts before third party with a blank line between",
			source: "package store\n\nimport (\n\t\"github.com/google/uuid\"\n\t\"strings\"\n\t\"context\"\n)\n",
			want:   "package store\n\nimport (\n\t\"context\"\n\t\"strings\"\n\n\t\"github.com/google/uuid\"\n)\n",
		},
		{
			name:   "appengine paths form the last group",
			source: "package store\n\nimport (\n\t\"appengine/datastore\"\n\t\"example.com/lib\"\n\t\"fmt\"\n)\n",
			want:   "package store\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/lib\"\n\n\t\"appengine/datastore\"\n)\n",
		},
		{
			name:   "named imports sort by path then name",
			source: "package store\n\nimport (\n\tuuid2 \"example.com/two/uuid\"\n\t\"example.com/one/uuid\"\n\t_ \"example.com/one/uuid\"\n)\n",
			want:   "package store\n\nimport (\n\t\"example.com/one/uuid\"\n\t_ \"example.com/one/uuid\"\n\tuuid2 \"example.com/two/uuid\"\n)\n",
		},
		{
			name:   "exact duplicates are dropped",
			source: "package store\n\nimport (\n\t\"fmt\"\n\t\"fmt\"\n)\n",
			want:   "package store\n\nimport (\n\t\"fmt\"\n)\n",
		},
		{
			name:   "separate declarations merge into one",
			source: "package store\n\nimport \"strings\"\nimport \"context\"\n\nvar Count int\n",
			want:   "package store\n\nimport (\n\t\"context\"\n\t\"strings\"\n)\n\nvar Count int\n",
		},
		{
			name:   "blank-line separated runs sort on their own",
			source: "package store\n\nimport (\n\t\"github.com/x/y\"\n\t\"fmt\"\n\n\t\"strings\"\n\t\"context\"\n)\n",
			want:   "package store\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/x/y\"\n\n\t\"context\"\n\t\"strings\"\n)\n",
		},
		{
			name:   "imports with comments are left in place",
			source: "package store\n\nimport (\n\t\"strings\" // trimming\n\t\"context\"\n)\n",
			want:   "package store\n\nimport (\n\t\"strings\" // trimming\n\t\"context\"\n)\n",
		},
		{
			name:   "a trailing comment on a lone import leaves it in place",
			source: "package store\n\nimport \"strings\" // trimming\n",
			want:   "package store\n\nimport \"strings\" // trimming\n",
		},
		{
			name:   "a cgo import is left in place",
			source: "package store\n\nimport (\n\t\"strings\"\n\t\"C\"\n)\n",
			want:   "package store\n\nimport (\n\t\"strings\"\n\t\"C\"\n)\n",
		},
		{
			name:   "comments around the package clause and the next declaration are kept",
			source: "// Package store holds queries.\npackage store\n\nimport (\n\t\"strings\"\n\t\"context\"\n)\n\n// Count counts.\nvar Count int\n",
			want:   "// Package store holds queries.\npackage store\n\nimport (\n\t\"context\"\n\t\"strings\"\n)\n\n// Count counts.\nvar Count int\n",
		},
		{
			name:   "source that does not parse is returned unchanged",
			source: "package store\n\nimport (\n",
			want:   "package store\n\nimport (\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, string(groupImports([]byte(testCase.source))))
		})
	}
}

func TestFormatSourceReportsSourceThatDoesNotParse(t *testing.T) {
	t.Parallel()

	fileSet := token.NewFileSet()
	file := &ast.File{Name: ast.NewIdent("store"), Decls: []ast.Decl{typedVariable("Count", ast.NewIdent("bad type"))}}

	_, err := FormatSource(fileSet, file)

	require.ErrorContains(t, err, "formatting Go AST: ")
}

func TestFormatFileWithASTGroupsStandardLibraryImportsFirst(t *testing.T) {
	t.Parallel()

	tracker := NewImportTracker()
	identifier := typedVariable("Identifier", tracker.AddType(sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"}))
	moment := typedVariable("Moment", tracker.AddType(sqlmodel.GoType{Package: "time", Name: "Time"}))
	nullable := typedVariable("Nullable", tracker.AddType(sqlmodel.GoType{Package: "database/sql", Name: "NullString"}))

	output, err := FormatFileWithAST("models", tracker, []ast.Decl{identifier, moment, nullable})

	require.NoError(t, err)
	require.Equal(t, "package models\n\nimport (\n\t\"database/sql\"\n\t\"time\"\n\n\t\"github.com/google/uuid\"\n)\n\nvar Identifier uuid.UUID\nvar Moment time.Time\nvar Nullable sql.NullString\n", string(output))
}

func TestFormattingRefusesSourceLongerThanTheCeilingBeforeFormattingIt(t *testing.T) {
	t.Parallel()

	oversized := strings.Repeat("x", maxFormattedFileBytes)
	testCases := []struct {
		format func() ([]byte, error)
		name   string
	}{
		{
			name: "written source",
			format: func() ([]byte, error) {
				return FormatText([]byte("package p\n\nconst c = \"" + oversized + "\"\n"))
			},
		},
		{
			name: "printed declarations",
			format: func() ([]byte, error) {
				declaration := &ast.GenDecl{Tok: token.CONST, Specs: []ast.Spec{&ast.ValueSpec{
					Names:  []*ast.Ident{ast.NewIdent("c")},
					Values: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: "`" + oversized + "`"}},
				}}}
				return FormatFileWithAST("p", NewImportTracker(), []ast.Decl{declaration})
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			scaling.Within(t, 2*time.Second, func(context.Context) {
				formatted, err := testCase.format()

				require.ErrorIs(t, err, errGeneratedFileTooLarge)
				require.Nil(t, formatted)
			})
		})
	}
}
