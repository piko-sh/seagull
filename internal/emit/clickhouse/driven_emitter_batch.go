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

package clickhouse

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/methods"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// identItem is the local holding one row's parameters.
	identItem = "item"
)

var (
	// errBatchShape reports a batch query whose VALUES row cannot be appended as columns.
	errBatchShape = errors.New("the batch query's VALUES row cannot be appended as columns")
)

// buildClickHouseBatchMethod builds a method that appends rows to a native ClickHouse
// batch.
//
// The generated method returns immediately for an empty input. Values that need
// validation or conversion pass through sglAppendNativeRow before the batch is sent.
//
// Takes query (*gogen.Query) which is the query being generated.
// Takes vocabulary (gogen.Vocabulary) which names the conversion helpers.
// Takes shapes (*chvalues.Shapes) which collects the package's compiled parameter
// descriptors.
//
// Returns *ast.FuncDecl which is the batch method declaration.
// Returns error when the query's VALUES row is not one placeholder per column.
func buildClickHouseBatchMethod(query *gogen.Query, vocabulary gogen.Vocabulary, shapes *chvalues.Shapes) (*ast.FuncDecl, error) {
	columns, ok := emitkit.RowColumns(query.Analysed)
	if !ok {
		return nil, fmt.Errorf("query %s: %w", query.Analysed.Name, errBatchShape)
	}
	parametersArgument := goastutil.CachedIdent(query.Naming.ParamsArgument)
	batch := goastutil.CachedIdent(goast.IdentBatch)
	body := []ast.Stmt{
		goastutil.IfStmt(nil,
			&ast.BinaryExpr{X: goastutil.CallExpr(goastutil.CachedIdent("len"), parametersArgument), Op: token.EQL, Y: goastutil.IntLit(0)},
			goastutil.BlockStmt(goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentNil)))),
		goastutil.DefineStmtMulti([]string{goast.IdentBatch, goast.IdentErr}, goastutil.CallExpr(
			goastutil.SelectorExprFrom(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentQueriesReceiver), nativeConnection.Writer), "PrepareBatch"),
			goastutil.CachedIdent(goast.IdentCtx),
			goastutil.CachedIdent(query.Naming.SQLConstant),
		)),
		methods.BuildErrCheck(),
		&ast.DeferStmt{Call: goastutil.CallExpr(goastutil.SelectorExprFrom(batch, "Close"))},
		&ast.RangeStmt{
			Key:   goastutil.CachedIdent(goast.IdentBlank),
			Value: goastutil.CachedIdent(identItem),
			Tok:   token.DEFINE,
			X:     parametersArgument,
			Body: goastutil.BlockStmt(goastutil.IfStmt(
				goastutil.DefineStmt(goast.IdentErr, appendRow(query, columns, vocabulary, shapes)),
				&ast.BinaryExpr{X: goastutil.CachedIdent(goast.IdentErr), Op: token.NEQ, Y: goastutil.CachedIdent(goast.IdentNil)},
				goastutil.BlockStmt(goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentErr))),
			)),
		},
		goastutil.ReturnStmt(goastutil.CallExpr(goastutil.SelectorExprFrom(batch, "Send"))),
	}
	return &ast.FuncDecl{
		Recv: methods.Receiver(),
		Name: goastutil.CachedIdent(query.Naming.Method),
		Type: &ast.FuncType{
			Params: goastutil.FieldList(
				goastutil.Field(goast.IdentCtx, query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: "context", Name: "Context"})),
				goastutil.Field(query.Naming.ParamsArgument, &ast.ArrayType{Elt: goastutil.CachedIdent(query.Naming.Params)}),
			),
			Results: goastutil.FieldList(goastutil.Field("", goastutil.CachedIdent(goast.IdentError))),
		},
		Body: goastutil.BlockStmt(body...),
	}, nil
}

// appendRow builds a batch append call, adding value checks and conversions where needed.
//
// Takes query (*gogen.Query) which is the query being generated.
// Takes columns ([]int) which are the indexes of the parameters the VALUES row appends,
// in order.
// Takes vocabulary (gogen.Vocabulary) which names the conversion helpers.
// Takes shapes (*chvalues.Shapes) which collects the package's compiled parameter
// descriptors.
//
// Returns ast.Expr which is the call.
func appendRow(query *gogen.Query, columns []int, vocabulary gogen.Vocabulary, shapes *chvalues.Shapes) ast.Expr {
	values := make([]ast.Expr, len(columns))
	parameters := make([]*sqlmodel.QueryParameter, len(columns))
	for position, index := range columns {
		parameters[position] = &query.Analysed.Parameters[index]
		field := goastutil.SelectorExprFrom(goastutil.CachedIdent(identItem), query.Naming.ParameterFields[index])
		values[position] = nativeBatchEnums.Bound(field, query.Analysed, parameters[position], query.Types, vocabulary)
	}
	appendMethod := goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentBatch), "Append")
	if !appendsRowThroughConversion(query.Analysed, query.Types) {
		return goastutil.CallExpr(appendMethod, values...)
	}
	return shapes.AppendRow(vocabulary, appendMethod, parameters, values)
}
