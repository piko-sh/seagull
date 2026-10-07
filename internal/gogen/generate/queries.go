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
	"cmp"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/dynamic"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/methods"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// paragraphMarker is the bare line comment that parts two paragraphs of a doc comment.
	paragraphMarker = "//"
)

var (
	// errNoDeclaration reports a driver's builder that returned no declaration and no error.
	errNoDeclaration = errors.New("no declaration")
)

// emitQueries generates the query files of a package, one per query source file, and the
// helper files every package with such queries shares.
//
// Takes emission (*gogen.Emission) which is the package's generation.
//
// Returns []sqlmodel.GeneratedFile which holds the query files, then the helper files.
// Returns error when a query cannot be generated or the context is done before a file is.
func emitQueries(ctx context.Context, emission *gogen.Emission) ([]sqlmodel.GeneratedFile, error) {
	queries := emission.Request.Queries
	if len(queries) == 0 {
		return nil, nil
	}

	grouped := methods.GroupQueriesByFilename(queries)
	filenames := slices.Sorted(maps.Keys(grouped))

	files := make([]sqlmodel.GeneratedFile, 0, len(filenames))
	for _, filename := range filenames {
		if err := stopped(ctx, filename); err != nil {
			return nil, err
		}
		generated, err := emitQueryFile(ctx, emission, filename, grouped[filename])
		if err != nil {
			return nil, err
		}
		files = append(files, generated)
	}

	helperFiles, err := sharedHelperFiles(ctx, emission)
	if err != nil {
		return nil, err
	}
	return append(files, helperFiles...), nil
}

// emitQueryFile generates the file of the queries from one source SQL file, ordered by
// name, under the name the naming plan gives it, referring to every import by the
// package-wide qualifier the plan gives it.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes filename (string) which is the source SQL filename.
// Takes fileQueries ([]*sqlmodel.AnalysedQuery) which are the queries from this file.
//
// Returns sqlmodel.GeneratedFile which contains the generated source file.
// Returns error when a query cannot be generated or the file does not format.
func emitQueryFile(ctx context.Context, emission *gogen.Emission, filename string, fileQueries []*sqlmodel.AnalysedQuery) (sqlmodel.GeneratedFile, error) {
	slices.SortFunc(fileQueries, func(left, right *sqlmodel.AnalysedQuery) int {
		return cmp.Compare(left.Name, right.Name)
	})

	tracker := goast.NewPlannedImportTracker(emission.Request.Naming.Imports)
	tracker.AddImport("context")

	declarations := make([]ast.Decl, 0, len(fileQueries))
	for _, query := range fileQueries {
		queryDeclarations, err := queryDeclarations(emission, query, tracker)
		if err != nil {
			return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("query %s in %s: %w", query.Name, filename, err)
		}
		declarations = append(declarations, queryDeclarations...)
	}

	emission.RecordDeclarations(declarations, tracker)
	_, finishFormat := observe.From(ctx).Start(ctx, observe.StageFormat)
	content, err := goast.FormatFileWithAST(emission.Request.Package, tracker, declarations)
	finishFormat(err)
	if err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("formatting query file %s: %w", filename, err)
	}
	return sqlmodel.GeneratedFile{Name: queryFileName(emission, filename), Content: content}, nil
}

// queryFileName returns the name of the generated file holding a source file's queries:
// the name the naming plan gives it, or the source file's stem with .sql.go when the plan
// names none.
//
// Takes emission (*gogen.Emission) which holds the plan.
// Takes filename (string) which is the source SQL filename.
//
// Returns string which is the generated file's name.
func queryFileName(emission *gogen.Emission, filename string) string {
	if name, ok := emission.Request.Naming.Files[filename]; ok {
		return name
	}
	return strings.TrimSuffix(filename, ".sql") + ".sql.go"
}

// queryDeclarations returns every declaration of one query: its SQL constant, its
// parameter and row structs, and its method or runtime builder. A query whose rows are
// read into a struct another declaration declares, a table's model or a shared row type,
// declares no row struct.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes analysed (*sqlmodel.AnalysedQuery) which defines the query to emit.
// Takes tracker (*goast.ImportTracker) for import collection.
//
// Returns []ast.Decl which contains the declarations for this query.
// Returns error when the query cannot be rendered or its method cannot be generated.
func queryDeclarations(emission *gogen.Emission, analysed *sqlmodel.AnalysedQuery, tracker *goast.ImportTracker) ([]ast.Decl, error) {
	query, err := emission.Query(analysed, tracker)
	if err != nil {
		return nil, err
	}
	if err := methods.CheckColumnsReadable(emission, query); err != nil {
		return nil, err
	}
	declarations := []ast.Decl{methods.BuildConstant(query.Naming.SQLConstant, query.Rendered.SQL)}

	switch {
	case analysed.IsDynamic && !analysed.DynamicRuntime:
		declarations = append(declarations, dynamic.BuildDynamicDeclarations(query)...)
	case query.Naming.Params != "":
		declarations = append(declarations, methods.BuildParamsStruct(query, paramsStructParameters(analysed)))
	}

	switch {
	case query.Naming.SharedRow:
	case methods.HasOutputColumns(analysed):
		declarations = append(declarations, methods.BuildOutputStructs(emission, query)...)
	case query.Naming.Row != "" && methods.RequiresRowType(analysed):
		declarations = append(declarations, methods.EmptyRowStruct(query.Naming.Row))
	}

	if analysed.DynamicRuntime {
		declarations = append(declarations, dynamic.BuildRuntimeBuilderDeclarations(emission, query)...)
		documentMethod(emission, query, declarations)
		labelErrors(emission, query, declarations)
		return declarations, nil
	}
	method, err := queryMethod(emission, query)
	if err != nil {
		return nil, err
	}
	if method == nil {
		return nil, fmt.Errorf("%w: the driver generated no method for command %s", errNoDeclaration, analysed.Command)
	}
	declarations = append(declarations, method)
	documentMethod(emission, query, declarations)
	labelErrors(emission, query, declarations)
	return declarations, nil
}

// labelErrors makes every exported method among a query's declarations return the errors
// it meets labelled with its name, when the style asks: the Queries method under the
// query's method name, and a runtime builder's terminal under that name and its own, such
// as SearchBoats.All.
//
// Takes emission (*gogen.Emission) which holds the vocabulary and style.
// Takes query (*gogen.Query) which names the query's method.
// Takes declarations ([]ast.Decl) which are the query's declarations; methods among them
// are changed in place.
func labelErrors(emission *gogen.Emission, query *gogen.Query, declarations []ast.Decl) {
	if !emission.Vocabulary.WrapsErrors() {
		return
	}
	for _, declaration := range declarations {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Recv == nil || !function.Name.IsExported() {
			continue
		}
		name := query.Naming.Method
		if function.Name.Name != name {
			name += "." + function.Name.Name
		}
		methods.LabelErrors(emission.Vocabulary, name, function)
	}
}

// documentMethod gives the Queries method of a query its description as a doc comment,
// when the style asks for doc comments and the query has a description, ahead of any doc
// comment the method already carries.
//
// Takes emission (*gogen.Emission) which holds the style.
// Takes query (*gogen.Query) which is the query and names its method.
// Takes declarations ([]ast.Decl) which hold the query's declarations, the method among
// them; the method is changed in place.
func documentMethod(emission *gogen.Emission, query *gogen.Query, declarations []ast.Decl) {
	if !emission.Request.Style.DocComments {
		return
	}
	lines := emitkit.CommentLines(query.Analysed.Description)
	if len(lines) == 0 {
		return
	}
	for _, declaration := range declarations {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Recv == nil || function.Name.Name != query.Naming.Method {
			continue
		}
		if function.Doc != nil {
			lines = append(lines, paragraphMarker)
			for _, comment := range function.Doc.List {
				lines = append(lines, comment.Text)
			}
		}
		function.Doc = goast.DocComment(lines)
		return
	}
}

// queryMethod returns the method of a query that is not a runtime builder, after the
// prelude of a dynamic query or none.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
//
// Returns ast.Decl which is the method's declaration.
// Returns error when the method cannot be generated.
func queryMethod(emission *gogen.Emission, query *gogen.Query) (ast.Decl, error) {
	prelude := methods.StaticPrelude(query)
	if query.Analysed.IsDynamic {
		dynamicPrelude, err := dynamic.Prelude(query)
		if err != nil {
			return nil, err
		}
		prelude = dynamicPrelude
	}
	return methods.Method(emission, query, prelude)
}

// paramsStructParameters returns a query's params-struct parameters.
//
// For a batch command, it copies the parameters and promotes each Nullable flag from
// SQLType.Nullable, so a {name:Nullable(T)} placeholder generates a pointer field. Every
// other command keeps its parameters unchanged.
//
// Takes query (*sqlmodel.AnalysedQuery) which provides the parameters and command.
//
// Returns []sqlmodel.QueryParameter which are the params, maybe nullability-promoted.
func paramsStructParameters(query *sqlmodel.AnalysedQuery) []sqlmodel.QueryParameter {
	if query.Command != sqlmodel.QueryCommandBatch {
		return query.Parameters
	}
	promoted := make([]sqlmodel.QueryParameter, len(query.Parameters))
	copy(promoted, query.Parameters)
	for index := range promoted {
		if promoted[index].SQLType.Nullable {
			promoted[index].Nullable = true
		}
	}
	return promoted
}
