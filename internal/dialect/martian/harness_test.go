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

package martian_test

import (
	"context"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/martian"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	martianMigration = `CREATE TABLE items (
	id WHOLE64 PRIMARY KEY,
	name TEXTUAL(80) NOT NULL,
	quantity WHOLE32,
	price EXACT(10, 2),
	active FLAG NOT NULL DEFAULT 1,
	created MOMENT NOT NULL,
	[display label] TEXTUAL
);
CREATE TABLE widths (small WHOLE16 NOT NULL, medium WHOLE32 NOT NULL, large WHOLE64 NOT NULL);
CREATE TABLE [Mixed] ([Label] TEXTUAL);
`
	martianQueries = `-- sgl.query(name: GetItem, command: one)
SELECT id, name, quantity, [display label] FROM items WHERE id = @p1;
-- sgl.query(name: ListActiveItems, command: many)
SELECT id, CONCAT(name, [display label]) AS label FROM items WHERE active = @p1 AND quantity > @p2 ORDER BY id;
-- sgl.query(name: CreateItem, command: exec)
INSERT INTO items (id, name, quantity, active, created) VALUES (@p1, @p2, @p3, @p4, NOW());
-- sgl.query(name: RenameItem, command: exec)
UPDATE items SET name = @p2 WHERE id = @p1;
-- sgl.query(name: DeleteItem, command: exec)
DELETE FROM items WHERE id = @p1;
-- sgl.query(name: ItemsByID, command: many)
-- @p1 as sgl.param(ids, kind: slice)
SELECT id, name FROM items WHERE id IN (@p1);
-- sgl.query(name: ListWidths, command: many)
SELECT small, medium, large FROM widths;
-- sgl.query(name: NextQuantity, command: many)
SELECT quantity + 1 AS next FROM items;
`
	generatedPackage = "store"
)

var versionSuffix = regexp.MustCompile(`^v[0-9]+$`)

type recordingEmitter struct {
	inner   emit.EmitterPort
	queries []*sqlmodel.AnalysedQuery
}

func (recorder *recordingEmitter) Emit(ctx context.Context, request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	recorder.queries = request.Queries
	if recorder.inner == nil {
		return nil, nil
	}
	return recorder.inner.Emit(ctx, request)
}

type generation struct {
	result  *app.Result
	queries []*sqlmodel.AnalysedQuery
}

func (g generation) hasCode(code string) bool {
	return slices.ContainsFunc(g.result.Diagnostics, func(diagnostic diag.SourceError) bool {
		return diagnostic.Code == code
	})
}

func (g generation) query(t *testing.T, name string) *sqlmodel.AnalysedQuery {
	t.Helper()

	index := slices.IndexFunc(g.queries, func(query *sqlmodel.AnalysedQuery) bool { return query.Name == name })
	require.GreaterOrEqual(t, index, 0, "query %s was not analysed; diagnostics: %+v", name, g.result.Diagnostics)
	return g.queries[index]
}

func (g generation) file(t *testing.T, name string) string {
	t.Helper()

	index := slices.IndexFunc(g.result.Files, func(file sqlmodel.GeneratedFile) bool { return file.Name == name })
	require.GreaterOrEqual(t, index, 0, "file %s was not generated; diagnostics: %+v", name, g.result.Diagnostics)
	return string(g.result.Files[index].Content)
}

type stubbingImporter struct {
	standard types.Importer
	stubbed  map[string]bool
}

func (stubber *stubbingImporter) Import(importPath string) (*types.Package, error) {
	if !strings.Contains(strings.Split(importPath, "/")[0], ".") {
		return stubber.standard.Import(importPath)
	}
	name := stubPackageName(importPath)
	stubber.stubbed[name] = true
	stub := types.NewPackage(importPath, name)
	stub.MarkComplete()
	return stub, nil
}

func generate(t *testing.T, recorder *recordingEmitter, queries string) generation {
	t.Helper()

	return generateWith(t, recorder, martianMigration, queries)
}

func generateWith(t *testing.T, recorder *recordingEmitter, migration, queries string, options ...app.Option) generation {
	t.Helper()

	generator, err := app.New(martian.New(), append([]app.Option{app.WithEmitter(recorder)}, options...)...)
	require.NoError(t, err)
	files := fstest.MapFS{
		"migrations":                  &fstest.MapFile{Mode: fs.ModeDir},
		"queries":                     &fstest.MapFile{Mode: fs.ModeDir},
		"migrations/001_items.up.sql": &fstest.MapFile{Data: []byte(migration)},
		"queries/items.sql":           &fstest.MapFile{Data: []byte(queries)},
	}
	result, err := generator.Generate(t.Context(), app.Input{
		Package:      generatedPackage,
		Migrations:   files,
		Queries:      nil,
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)
	return generation{result: result, queries: recorder.queries}
}

func probe(t *testing.T, queries string) generation {
	t.Helper()

	return generate(t, &recordingEmitter{inner: nil, queries: nil}, queries)
}

func structFieldTypes(t *testing.T, source string, structName string) map[string]string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "models.go", source, parser.SkipObjectResolution)
	require.NoError(t, err)
	fields := make(map[string]string)
	ast.Inspect(file, func(node ast.Node) bool {
		typeSpecification, isTypeSpecification := node.(*ast.TypeSpec)
		if !isTypeSpecification || typeSpecification.Name.Name != structName {
			return true
		}
		structure, isStruct := typeSpecification.Type.(*ast.StructType)
		require.True(t, isStruct)
		for _, field := range structure.Fields.List {
			for _, name := range field.Names {
				fields[name.Name] = types.ExprString(field.Type)
			}
		}
		return false
	})
	return fields
}

func stubPackageName(importPath string) string {
	elements := strings.Split(importPath, "/")
	name := elements[len(elements)-1]
	if versionSuffix.MatchString(name) && len(elements) > 1 {
		name = elements[len(elements)-2]
	}
	return strings.TrimSuffix(name, "-go")
}

func typeCheck(t *testing.T, files []sqlmodel.GeneratedFile) []string {
	t.Helper()

	fileSet := token.NewFileSet()
	parsed := make([]*ast.File, 0, len(files))
	for _, file := range files {
		if path.Ext(file.Name) != ".go" {
			continue
		}
		syntaxTree, err := parser.ParseFile(fileSet, file.Name, file.Content, parser.SkipObjectResolution)
		require.NoError(t, err)
		parsed = append(parsed, syntaxTree)
	}
	stubber := &stubbingImporter{standard: importer.ForCompiler(fileSet, "gc", nil), stubbed: make(map[string]bool)}
	var problems []string
	config := types.Config{
		Importer: stubber,
		Error: func(err error) {
			problems = append(problems, err.Error())
		},
	}
	_, _ = config.Check(generatedPackage, fileSet, parsed, nil)
	return slices.DeleteFunc(problems, func(problem string) bool {
		for name := range stubber.stubbed {
			if strings.Contains(problem, "undefined: "+name+".") {
				return true
			}
		}
		return false
	})
}
