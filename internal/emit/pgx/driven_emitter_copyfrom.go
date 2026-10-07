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
	"errors"
	"fmt"
	"go/ast"
	"go/token"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
)

const (
	// identRowIndex is the argument of the CopyFromSlice function: the index of the row.
	identRowIndex = "i"
)

var (
	// errCopyFromShape reports a copyfrom query whose VALUES row cannot be sent as columns.
	errCopyFromShape = errors.New("the copyfrom query's VALUES row cannot be sent as columns")
)

// buildCopyFromMethod builds a query method that streams rows through pgx CopyFrom.
//
//	func (queries *Queries) CopyAudit(ctx context.Context, params []CopyAuditParams) (int64, error) {
//		return queries.db.CopyFrom(ctx, pgx.Identifier{"audit", "events"}, []string{"action", "actor"},
//			pgx.CopyFromSlice(len(params), func(i int) ([]any, error) {
//				return []any{params[i].Action, params[i].Actor}, nil
//			}))
//	}
//
// Takes query (*gogen.Query) which is the query being generated.
// Takes binder (pgxBinder) which binds each value as the package's other methods do.
//
// Returns *ast.FuncDecl which is the copyfrom method declaration.
// Returns error when the query's VALUES row is not one placeholder per named column.
func buildCopyFromMethod(query *gogen.Query, binder pgxBinder) (*ast.FuncDecl, error) {
	columns, ok := emitkit.RowColumns(query.Analysed)
	if !ok || len(columns) != len(query.Analysed.InsertColumns) || query.Analysed.InsertTarget.Name == "" {
		return nil, fmt.Errorf("query %s: %w", query.Analysed.Name, errCopyFromShape)
	}
	parametersArgument := goastutil.CachedIdent(query.Naming.ParamsArgument)
	values := make([]ast.Expr, len(columns))
	for position, parameter := range columns {
		values[position] = binder.Bind(goastutil.SelectorExprFrom(
			&ast.IndexExpr{X: parametersArgument, Index: goastutil.CachedIdent(identRowIndex)},
			query.Naming.ParameterFields[parameter],
		), &query.Analysed.Parameters[parameter], query.Imports)
	}
	columnNames := make([]ast.Expr, len(query.Analysed.InsertColumns))
	for index, column := range query.Analysed.InsertColumns {
		columnNames[index] = goastutil.StrLit(column)
	}
	target := []ast.Expr{goastutil.StrLit(query.Analysed.InsertTarget.Name)}
	if schema := query.Analysed.InsertTarget.Schema; schema != "" {
		target = []ast.Expr{goastutil.StrLit(schema), goastutil.StrLit(query.Analysed.InsertTarget.Name)}
	}
	anySlice := &ast.ArrayType{Elt: goastutil.CachedIdent(goast.IdentAny)}
	rowBody := []ast.Stmt{goastutil.IfStmt(nil,
		&ast.BinaryExpr{X: goastutil.CachedIdent(goast.IdentArgs), Op: token.EQL, Y: goastutil.CachedIdent(goast.IdentNil)},
		goastutil.BlockStmt(goastutil.AssignStmt(goastutil.CachedIdent(goast.IdentArgs),
			goastutil.CallExpr(goastutil.CachedIdent("make"), anySlice, goastutil.IntLit(len(values)))))),
	}
	for index, value := range values {
		rowBody = append(rowBody, goastutil.AssignStmt(
			&ast.IndexExpr{X: goastutil.CachedIdent(goast.IdentArgs), Index: goastutil.IntLit(index)}, value))
	}
	rowBody = append(rowBody, goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentArgs), goastutil.CachedIdent(goast.IdentNil)))
	rowSource := goastutil.CallExpr(pgxSelector(query, "CopyFromSlice"),
		goastutil.CallExpr(goastutil.CachedIdent("len"), parametersArgument),
		goastutil.FuncLit(
			goastutil.FuncType(
				goastutil.FieldList(goastutil.Field(identRowIndex, goastutil.CachedIdent("int"))),
				goastutil.FieldList(goastutil.Field("", anySlice), goastutil.Field("", goastutil.CachedIdent(identError))),
			),
			goastutil.BlockStmt(rowBody...),
		),
	)
	body := []ast.Stmt{goastutil.VarDecl(goast.IdentArgs, anySlice), goastutil.ReturnStmt(goastutil.CallExpr(
		goastutil.SelectorExprFrom(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentQueriesReceiver), pgxConnection.Writer), "CopyFrom"),
		goastutil.CachedIdent(goast.IdentCtx),
		&ast.CompositeLit{Type: pgxSelector(query, "Identifier"), Elts: target},
		&ast.CompositeLit{Type: &ast.ArrayType{Elt: goastutil.CachedIdent("string")}, Elts: columnNames},
		rowSource,
	))}
	results := goastutil.FieldList(goastutil.Field("", goastutil.CachedIdent("int64")), goastutil.Field("", goastutil.CachedIdent(identError)))
	return batchDeclaration(query, results, body), nil
}
