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

package fuzzruntime

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

// sqlRows supplies bounded results through database/sql.
//
//go:embed rows_sql_worker.go
var sqlRows string

// nativeRows supplies bounded results through native driver interfaces.
//
//go:embed rows_native_worker.go
var nativeRows string

// fuzzQueryMethods names the recorder methods whose results the row reader replaces.
var fuzzQueryMethods = map[string]bool{"Query": true, "QueryContext": true, "QueryRow": true}

// fuzzEmptyResults names the recorder result types the row reader replaces.
var fuzzEmptyResults = map[string]bool{"emptyRows": true, "missingRow": true}

// RowsSource supplies deterministic row readers for one recorder.
//
// Takes driver (string) which names the emitter.
//
// Returns string which is a standalone Go source file.
func RowsSource(driver string) string {
	if driver == "databasesql" {
		return workerSource(sqlRows)
	}
	result, returned, documented := "", "", ""
	if driver == "clickhouse" {
		result, returned = "error", "return nil"
		documented = "//\n// Returns error which is nil after closure.\n"
	}
	return strings.NewReplacer(
		"//\n// Returns CLOSE_TYPE which is nil after closure.\n", documented,
		"CLOSE_TYPE", result,
		"CLOSE_RETURN", returned,
	).Replace(workerSource(nativeRows))
}

// PopulateRecorder redirects query methods to the bounded row reader.
//
// Takes source (string) which contains the existing recorder implementation.
//
// Returns string which preserves statement recording and batch behaviour.
// Returns error when the expected recorder methods cannot be found.
func PopulateRecorder(source string) (string, error) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "recorder.go", source, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parsing row recorder: %w", err)
	}
	changed := 0
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || function.Recv == nil || len(function.Recv.List) == 0 {
			continue
		}
		query := fuzzRecorderQuery(function)
		if query == nil {
			continue
		}
		changed += redirectFuzzRows(function, query)
	}
	if changed == 0 {
		return "", errors.New("recorder has no replaceable query methods")
	}
	var output bytes.Buffer
	if err := format.Node(&output, files, file); err != nil {
		return "", fmt.Errorf("formatting row recorder: %w", err)
	}
	return output.String(), nil
}

// fuzzRecorderQuery finds the SQL expression used by a connection query method.
//
// Takes function (*ast.FuncDecl) which declares a recorder method.
//
// Returns ast.Expr which is nil for methods outside the row oracle.
func fuzzRecorderQuery(function *ast.FuncDecl) ast.Expr {
	if !fuzzQueryMethods[function.Name.Name] {
		return nil
	}
	receiver := function.Recv.List[0]
	kind := receiver.Type
	if pointer, ok := kind.(*ast.StarExpr); ok {
		kind = pointer.X
	}
	name, ok := kind.(*ast.Ident)
	if !ok {
		return nil
	}
	switch name.Name {
	case "connection", "Connection":
		return ast.NewIdent("query")
	case "preparedStatement":
		if len(receiver.Names) == 0 {
			return nil
		}
		return &ast.SelectorExpr{X: ast.NewIdent(receiver.Names[0].Name), Sel: ast.NewIdent("query")}
	default:
		return nil
	}
}

// redirectFuzzRows replaces empty results in a selected recorder method.
//
// Takes function (*ast.FuncDecl) which is a connection query method.
// Takes query (ast.Expr) which identifies its SQL text.
//
// Returns int which counts redirected return statements.
func redirectFuzzRows(function *ast.FuncDecl, query ast.Expr) int {
	changed := 0
	ast.Inspect(function.Body, func(node ast.Node) bool {
		statement, ok := node.(*ast.ReturnStmt)
		if !ok || len(statement.Results) == 0 {
			return true
		}
		literal, ok := statement.Results[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		name, ok := literal.Type.(*ast.Ident)
		if ok && fuzzEmptyResults[name.Name] {
			statement.Results[0] = &ast.CallExpr{Fun: ast.NewIdent("newFuzzRows"), Args: []ast.Expr{query}}
			changed++
		}
		return true
	})
	return changed
}
