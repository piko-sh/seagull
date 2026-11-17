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
	"strconv"
)

// preparedRecorderHooks counts the expected recorder interception points.
const preparedRecorderHooks = 6

// preparedDriver names the execution mode that uses the generated statement cache.
const preparedDriver = "databasesql_prepared"

// preparedStatementOperations maps each instrumented statement method to its lifecycle
// event.
var preparedStatementOperations = map[string]string{
	"Close":        preparedClose,
	"Query":        preparedUse,
	"QueryContext": preparedUse,
	"Exec":         preparedUse,
	"ExecContext":  preparedUse,
}

// preparedSource exercises the generated statement cache.
//
//go:embed prepared_sql_worker.go
var preparedSource string

// directSource preserves the direct execution path.
//
//go:embed prepared_direct_worker.go
var directSource string

// PreparedSource supplies query construction for one execution mode.
//
// Takes driver (string) which selects direct or prepared execution.
//
// Returns string which is compiled alongside the generated package.
func PreparedSource(driver string) string {
	if driver == preparedDriver {
		return workerSource(preparedSource)
	}
	return workerSource(directSource)
}

// InstrumentPreparedRecorder observes statement ownership without changing production
// code.
//
// Takes source (string) which contains the populated database/sql recorder.
//
// Returns string which records preparation, execution and closure.
// Returns error when the recorder contract changes.
func InstrumentPreparedRecorder(source string) (string, error) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "recorder.go", source, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parsing prepared recorder: %w", err)
	}
	changes := 0
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || function.Recv == nil || len(function.Recv.List) == 0 {
			continue
		}
		receiver := function.Recv.List[0]
		kind, ok := receiver.Type.(*ast.Ident)
		if !ok {
			continue
		}
		prefix := preparedRecorderPrefix(kind.Name, function.Name.Name)
		if prefix == "" {
			continue
		}
		if len(receiver.Names) == 0 {
			receiver.Names = []*ast.Ident{ast.NewIdent("statement")}
		}
		if err := prependPreparedHook(files, function, prefix); err != nil {
			return "", err
		}
		changes++
	}
	if changes != preparedRecorderHooks {
		return "", fmt.Errorf("prepared recorder has %d hooks, expected %d", changes, preparedRecorderHooks)
	}
	var output bytes.Buffer
	if err := format.Node(&output, files, file); err != nil {
		return "", fmt.Errorf("formatting prepared recorder: %w", err)
	}
	return output.String(), nil
}

// preparedRecorderPrefix supplies narrowly scoped statement lifecycle hooks.
//
// Takes receiver (string) which names a recording driver type.
// Takes method (string) which names the method being instrumented.
//
// Returns string which is empty for unrelated recorder methods.
func preparedRecorderPrefix(receiver, method string) string {
	if receiver == "connection" && method == "Prepare" {
		return "if err := fuzzPrepare(query); err != nil { return nil, err };"
	}
	if receiver != "preparedStatement" {
		return ""
	}
	operation, ok := preparedStatementOperations[method]
	if !ok {
		return ""
	}
	return "fuzzPreparedEvent(" + strconv.Quote(operation) + ", statement.query);"
}

// prependPreparedHook inserts a parsed hook before the recorder method body.
//
// Takes files (*token.FileSet) which owns the source positions.
// Takes function (*ast.FuncDecl) which receives the hook.
// Takes prefix (string) which contains the lifecycle observation.
//
// Returns error when the hook cannot be parsed as a function body.
func prependPreparedHook(files *token.FileSet, function *ast.FuncDecl, prefix string) error {
	parsed, err := parser.ParseFile(files, "hook.go", "package main; func hook() {"+prefix+"}", 0)
	if err != nil {
		return fmt.Errorf("parsing prepared recorder hook: %w", err)
	}
	hook, ok := parsed.Decls[0].(*ast.FuncDecl)
	if !ok {
		return errors.New("prepared recorder hook did not parse as a function")
	}
	function.Body.List = append(hook.Body.List, function.Body.List...)
	return nil
}
