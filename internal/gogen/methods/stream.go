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

package methods

import (
	"go/ast"
	"go/token"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// iterPackage is the standard library package declaring the iterator types.
	iterPackage = "iter"

	// iterSeq2 is the iterator type over pairs of values.
	iterSeq2 = "Seq2"
)

// buildStreamMethod constructs a stream query method that returns a range-over-func
// iterator: iter.Seq2[Row, error] when the style asks for current idioms, and otherwise
// the same type spelled func(func(Row, error) bool). The prelude and the call's
// preparation run inside the iterator, so an error they meet is yielded.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
// Takes prelude (*Prelude) which the iterator starts with.
//
// Returns *ast.FuncDecl which is the stream method declaration.
func buildStreamMethod(emission *gogen.Emission, query *gogen.Query, prelude *Prelude) *ast.FuncDecl {
	row := query.Naming.Row
	call := PrepareCall(emission, query, prelude.Source, YieldGuard(row))
	dbCall := ConnectionCall(emission, query, emission.Driver.Connection.Query, &call)
	iteratorBody := concat(prelude.Statements, call.Statements)
	iteratorBody = append(iteratorBody, streamIteratorBody(emission, query, dbCall)...)

	yieldFuncType := goastutil.FuncType(goastutil.FieldList(goastutil.Field(identYield, streamYieldType(row))), nil)
	return methodDecl(query, goastutil.FieldList(goastutil.Field("", streamIteratorType(emission, query))), []ast.Stmt{
		goastutil.ReturnStmt(goastutil.FuncLit(yieldFuncType, goastutil.BlockStmt(iteratorBody...))),
	})
}

// streamIteratorType constructs the type a stream method returns: iter.Seq2[Row, error],
// with iter imported under the qualifier the query's file gives it, when the style asks
// for current idioms, and func(func(Row, error) bool) otherwise.
//
// Takes emission (*gogen.Emission) which holds the style.
// Takes query (*gogen.Query) which names the row type and collects the file's imports.
//
// Returns ast.Expr which is the type.
func streamIteratorType(emission *gogen.Emission, query *gogen.Query) ast.Expr {
	row := query.Naming.Row
	if !emission.Request.Style.ModernIdioms {
		return goastutil.FuncType(goastutil.FieldList(goastutil.Field("", streamYieldType(row))), nil)
	}
	return &ast.IndexListExpr{
		X:       query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: iterPackage, Name: iterSeq2}),
		Indices: []ast.Expr{goastutil.CachedIdent(row), goastutil.CachedIdent(goast.IdentError)},
	}
}

// streamYieldType constructs the type func(RowType, error) bool.
//
// Takes rowTypeName (string) which is the name of the row result type.
//
// Returns *ast.FuncType which represents func(RowType, error) bool.
func streamYieldType(rowTypeName string) *ast.FuncType {
	return goastutil.FuncType(
		goastutil.FieldList(
			goastutil.Field("", goastutil.CachedIdent(rowTypeName)),
			goastutil.Field("", goastutil.CachedIdent(goast.IdentError)),
		),
		goastutil.FieldList(
			goastutil.Field("", goastutil.CachedIdent(goast.IdentBool)),
		),
	)
}

// streamIteratorBody constructs the body of the iterator closure after the call's
// preparation: the call, the error yield, the deferred Close, the scan and yield loop,
// and the final rows.Err yield.
//
// Takes emission (*gogen.Emission) which names the scan adapter.
// Takes query (*gogen.Query) which is the query.
// Takes dbCall (*ast.CallExpr) which runs the query.
//
// Returns []ast.Stmt which contains the statements.
func streamIteratorBody(emission *gogen.Emission, query *gogen.Query, dbCall *ast.CallExpr) []ast.Stmt {
	row := query.Naming.Row
	return []ast.Stmt{
		goastutil.DefineStmtMulti([]string{goast.IdentRows, goast.IdentErr}, dbCall),
		&ast.IfStmt{
			Cond: errorIsNotNil(),
			Body: goastutil.BlockStmt(YieldGuard(row)()...),
		},
		closeRows(),
		ScanRows(emission, query, func(scan Scan, call *ast.CallExpr) []ast.Stmt {
			return streamScanLoop(query, scan, call)
		}),
		goastutil.IfStmt(
			goastutil.DefineStmt(goast.IdentErr,
				goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentRows), "Err")),
			),
			errorIsNotNil(),
			goastutil.BlockStmt(&ast.ExprStmt{X: yieldCall(zeroRow(row), goastutil.CachedIdent(goast.IdentErr))}),
		),
	}
}

// streamScanLoop constructs the inner loop body that declares a row, scans into it, and
// yields it.
//
// Takes query (*gogen.Query) which is the query.
// Takes scan (Scan) which describes scan destinations and conversions.
// Takes call (*ast.CallExpr) which is the rows.Scan call to emit.
//
// Returns []ast.Stmt which are the statements forming the scan loop body.
func streamScanLoop(query *gogen.Query, scan Scan, call *ast.CallExpr) []ast.Stmt {
	row := query.Naming.Row
	stopUnless := func(value ast.Expr) ast.Stmt {
		return &ast.IfStmt{
			Cond: &ast.UnaryExpr{Op: token.NOT, X: yieldCall(value, nilOrError(value))},
			Body: goastutil.BlockStmt(goastutil.ReturnStmt()),
		}
	}
	statements := []ast.Stmt{
		goastutil.IfStmt(
			goastutil.DefineStmt(goast.IdentErr, call),
			errorIsNotNil(),
			goastutil.BlockStmt(stopUnless(zeroRow(row)), &ast.BranchStmt{Tok: token.CONTINUE}),
		),
	}
	statements = append(statements, scan.Conversions...)
	statements = append(statements, scan.CheckedConversions(stopUnless(zeroRow(row)), &ast.BranchStmt{Tok: token.CONTINUE})...)
	if !scan.embedsFinal {
		statements = append(statements, BuildEmbedNilCheckStatements(query)...)
	}
	return append(statements, stopUnless(goastutil.CachedIdent(goast.IdentRow)))
}

// yieldCall returns the call yield(value, err).
//
// Takes value (ast.Expr) which is the row yielded.
// Takes err (ast.Expr) which is the error yielded.
//
// Returns *ast.CallExpr which is the call.
func yieldCall(value, err ast.Expr) *ast.CallExpr {
	return goastutil.CallExpr(goastutil.CachedIdent(identYield), value, err)
}

// nilOrError returns the error a stream yields with a value: nil with the scanned row,
// and err with the zero row a failed scan yields.
//
// Takes value (ast.Expr) which is the row yielded.
//
// Returns ast.Expr which is the error.
func nilOrError(value ast.Expr) ast.Expr {
	if identifier, ok := value.(*ast.Ident); ok && identifier.Name == goast.IdentRow {
		return goastutil.CachedIdent(goast.IdentNil)
	}
	return goastutil.CachedIdent(goast.IdentErr)
}
