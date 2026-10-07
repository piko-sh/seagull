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

	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/methods"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// identItem is the local holding one row's parameters.
	identItem = "item"

	// identCloseErr is the local holding the error closing a failed batch reports.
	identCloseErr = "closeErr"
)

var (
	// errExpandedBatch reports a batch query whose SQL expands a slice per call, which a
	// queued statement cannot.
	errExpandedBatch = errors.New("a batch query cannot expand a slice parameter per row")
)

// buildBatchMethod builds a method that sends rows through a pgx.Batch.
//
// The generated method queues each row's statement, sends the batch and checks its
// results. Enum containers are converted to ordinary strings before binding.
//
// Takes query (*gogen.Query) which is the query being generated.
// Takes vocabulary (gogen.Vocabulary) which names the conversion helper.
// Takes binder (pgxBinder) which binds each parameter as the package's other methods do.
//
// Returns *ast.FuncDecl which is the batch method declaration.
// Returns error when the statement expands a slice parameter.
func buildBatchMethod(query *gogen.Query, vocabulary gogen.Vocabulary, binder pgxBinder) (*ast.FuncDecl, error) {
	queueArguments := []ast.Expr{goastutil.CachedIdent(query.Naming.SQLConstant)}
	for _, argument := range query.Rendered.Arguments {
		if argument.Expand {
			return nil, fmt.Errorf("query %s: %w", query.Analysed.Name, errExpandedBatch)
		}
		field := goastutil.SelectorExprFrom(goastutil.CachedIdent(identItem), query.Naming.ParameterFields[argument.Parameter])
		parameter := &query.Analysed.Parameters[argument.Parameter]
		queueArguments = append(queueArguments,
			binder.Bind(pgxEnums.Bound(field, query.Analysed, parameter, query.Types, vocabulary), parameter, query.Imports))
	}
	parametersArgument := goastutil.CachedIdent(query.Naming.ParamsArgument)
	body := []ast.Stmt{
		goastutil.DefineStmt(goast.IdentBatch, goastutil.AddressExpr(goastutil.CompositeLit(pgxSelector(query, "Batch"),
			goastutil.KeyValueIdent("QueuedQueries", goastutil.CallExpr(goastutil.CachedIdent("make"),
				&ast.ArrayType{Elt: goastutil.StarExpr(pgxSelector(query, "QueuedQuery"))},
				goastutil.IntLit(0), goastutil.CallExpr(goastutil.CachedIdent("len"), parametersArgument)))))),
		&ast.RangeStmt{
			Key:   goastutil.CachedIdent(goast.IdentBlank),
			Value: goastutil.CachedIdent(identItem),
			Tok:   token.DEFINE,
			X:     parametersArgument,
			Body: goastutil.BlockStmt(goastutil.ExprStmt(goastutil.CallExpr(
				goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentBatch), "Queue"), queueArguments...))),
		},
		goastutil.DefineStmt(goast.IdentResults, goastutil.CallExpr(
			goastutil.SelectorExprFrom(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentQueriesReceiver), pgxConnection.Writer), "SendBatch"),
			goastutil.CachedIdent(goast.IdentCtx), goastutil.CachedIdent(goast.IdentBatch))),
		&ast.RangeStmt{
			X: parametersArgument,
			Body: goastutil.BlockStmt(goastutil.IfStmt(
				goastutil.DefineStmtMulti([]string{goast.IdentBlank, goast.IdentErr},
					goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentResults), "Exec"))),
				&ast.BinaryExpr{X: goastutil.CachedIdent(goast.IdentErr), Op: token.NEQ, Y: goastutil.CachedIdent(goast.IdentNil)},
				goastutil.BlockStmt(closeAfterFailure(query)...),
			)),
		},
		goastutil.ReturnStmt(closeResults()),
	}
	return batchDeclaration(query, goastutil.FieldList(goastutil.Field("", goastutil.CachedIdent(identError))), body), nil
}

// closeAfterFailure builds the statements that end a batch whose statement failed: they
// close the batch's results and return the statement's error, joined with the error the
// close reports when that is another error, so a failure that arrives only as the batch
// is closed is not lost.
//
// Takes query (*gogen.Query) which collects the file's imports.
//
// Returns []ast.Stmt which are the statements.
func closeAfterFailure(query *gogen.Query) []ast.Stmt {
	errorsCall := func(name string, arguments ...ast.Expr) ast.Expr {
		return goastutil.CallExpr(query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: "errors", Name: name}), arguments...)
	}
	closed := goastutil.CachedIdent(identCloseErr)
	failure := goastutil.CachedIdent(goast.IdentErr)
	return []ast.Stmt{
		goastutil.IfStmt(
			goastutil.DefineStmt(identCloseErr, closeResults()),
			&ast.BinaryExpr{
				X:  &ast.BinaryExpr{X: closed, Op: token.NEQ, Y: goastutil.CachedIdent(goast.IdentNil)},
				Op: token.LAND,
				Y:  &ast.UnaryExpr{Op: token.NOT, X: errorsCall("Is", closed, failure)},
			},
			goastutil.BlockStmt(goastutil.ReturnStmt(errorsCall("Join", failure, closed))),
		),
		goastutil.ReturnStmt(failure),
	}
}

// closeResults builds the call that closes a batch's results, which reports the first
// error the batch met, one arriving only with its end included.
//
// Returns ast.Expr which is the call.
func closeResults() ast.Expr {
	return goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentResults), "Close"))
}

// batchDeclaration wraps a batch or copyfrom method body in its declaration, taking the
// context and the slice of the query's Params struct.
//
// Takes query (*gogen.Query) which names the method, its argument and its struct.
// Takes results (*ast.FieldList) which are the method's results.
// Takes body ([]ast.Stmt) which is the method body.
//
// Returns *ast.FuncDecl which is the method declaration.
func batchDeclaration(query *gogen.Query, results *ast.FieldList, body []ast.Stmt) *ast.FuncDecl {
	return &ast.FuncDecl{
		Recv: methods.Receiver(),
		Name: goastutil.CachedIdent(query.Naming.Method),
		Type: &ast.FuncType{
			Params: goastutil.FieldList(
				goastutil.Field(goast.IdentCtx, query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: "context", Name: "Context"})),
				goastutil.Field(query.Naming.ParamsArgument, &ast.ArrayType{Elt: goastutil.CachedIdent(query.Naming.Params)}),
			),
			Results: results,
		},
		Body: goastutil.BlockStmt(body...),
	}
}

// pgxSelector refers to a declaration of the pgx package by the qualifier the file
// imports it under.
//
// Takes query (*gogen.Query) which collects the file's imports.
// Takes name (string) which is the declaration's name.
//
// Returns ast.Expr which is the qualified name.
func pgxSelector(query *gogen.Query, name string) ast.Expr {
	return query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: importPgx, Name: name})
}
