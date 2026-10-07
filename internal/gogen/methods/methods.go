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
	"errors"
	"fmt"
	"go/ast"
	"go/token"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// identYield is the function a stream iterator yields its rows to.
	identYield = "yield"

	// identFalse is the predeclared false.
	identFalse = "false"

	// identTrue is the predeclared true.
	identTrue = "true"

	// identInt64 is the type an execrows method returns its count as.
	identInt64 = "int64"
)

var (
	// errUnsupportedCommand is wrapped by the error returned for a query whose command the
	// driver cannot run.
	errUnsupportedCommand = errors.New("unsupported command")
)

// Prelude is what the method of a query does before it runs the query: the statements
// that settle its parameters and SQL, and the expression its SQL is then read from.
type Prelude struct {
	// Source reads the SQL the query runs: its SQL constant, or the local the statements
	// wrote a sortable ORDER BY into.
	Source ast.Expr

	// Statements run first.
	Statements []ast.Stmt
}

// StaticPrelude returns the prelude of a query whose method runs its SQL constant as it
// is.
//
// Takes query (*gogen.Query) which is the query.
//
// Returns *Prelude which reads the SQL constant.
func StaticPrelude(query *gogen.Query) *Prelude {
	return &Prelude{Source: SQLConstant(query), Statements: nil}
}

// Method returns the method on *Queries of a query that is not a runtime builder, chosen
// by its command, after the prelude. A batch or copyfrom query goes to the driver's
// BatchBuilder, or runs as a plain statement when the driver has none.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
// Takes prelude (*Prelude) which the method starts with.
//
// Returns ast.Decl which is the method declaration.
// Returns error when the driver cannot generate the query's method.
func Method(emission *gogen.Emission, query *gogen.Query, prelude *Prelude) (ast.Decl, error) {
	switch query.Analysed.Command {
	case sqlmodel.QueryCommandOne:
		if query.Analysed.Optional {
			query.Imports.AddImport("errors")
			return buildOptionalOneMethod(emission, query, prelude), nil
		}
		return buildOneMethod(emission, query, prelude), nil
	case sqlmodel.QueryCommandMany:
		if HasGroupByKey(query.Analysed) {
			return GroupedManyMethod(emission, query, prelude), nil
		}
		return buildManyMethod(emission, query, prelude), nil
	case sqlmodel.QueryCommandStream:
		return buildStreamMethod(emission, query, prelude), nil
	case sqlmodel.QueryCommandExecResult:
		if err := RequireExecResult(emission, query.Analysed); err != nil {
			return nil, err
		}
		return buildExecResultMethod(emission, query, prelude), nil
	case sqlmodel.QueryCommandExecRows:
		if err := RequireExecResult(emission, query.Analysed); err != nil {
			return nil, err
		}
		return buildExecRowsMethod(emission, query, prelude), nil
	case sqlmodel.QueryCommandBatch, sqlmodel.QueryCommandCopyFrom:
		return batchMethod(emission, query, prelude)
	case sqlmodel.QueryCommandAsyncExec:
		return buildAsyncExecMethod(emission, query, prelude), nil
	default:
		return buildExecMethod(emission, query, prelude), nil
	}
}

// RowsIterationBody constructs the body of a many method from the query's call: the call,
// its error check, the deferred Close, the results, the scan loop, the rows' error check,
// and the return.
//
// Takes emission (*gogen.Emission) which names the scan adapter.
// Takes query (*gogen.Query) which is the query.
// Takes dbCall (*ast.CallExpr) which runs the query.
//
// Returns []ast.Stmt which contains the method body statements.
func RowsIterationBody(emission *gogen.Emission, query *gogen.Query, dbCall *ast.CallExpr) []ast.Stmt {
	return []ast.Stmt{
		goastutil.DefineStmtMulti([]string{goast.IdentRows, goast.IdentErr}, dbCall),
		BuildErrCheck(goastutil.CachedIdent(goast.IdentNil)),
		closeRows(),
		ResultsDeclaration(emission, goast.IdentResults, query.Naming.Row),
		ScanRows(emission, query, func(scan Scan, call *ast.CallExpr) []ast.Stmt {
			return rowsScanLoop(query, scan, call, goast.IdentResults)
		}),
		BuildRowsErrCheck(),
		goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentResults), goastutil.CachedIdent(goast.IdentNil)),
	}
}

// ResultsDeclaration returns the declaration of the slice a many method gathers its rows
// in: `var results []Row`, which stays nil when no row matches, or `results := []Row{}`
// when the style asks for empty slices.
//
// Takes emission (*gogen.Emission) which holds the style.
// Takes name (string) which names the slice.
// Takes row (string) which names the row struct.
//
// Returns ast.Stmt which is the declaration.
func ResultsDeclaration(emission *gogen.Emission, name, row string) ast.Stmt {
	sliceType := &ast.ArrayType{Elt: goastutil.CachedIdent(row)}
	if emission.Request.Style.EmptySlices {
		return goastutil.DefineStmt(name, goastutil.CompositeLit(sliceType))
	}
	return &ast.DeclStmt{
		Decl: &ast.GenDecl{
			Tok:   token.VAR,
			Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{goastutil.CachedIdent(name)}, Type: sliceType}},
		},
	}
}

// CollectRows builds the scan loop collecting row values into the named result slice.
//
// Takes emission (*gogen.Emission) which holds the package plan, driver and imports.
// Takes query (*gogen.Query) which is the planned query being emitted.
// Takes results (string) which names the result slice.
//
// Returns ast.Stmt which iterates rows and appends each decoded result.
func CollectRows(emission *gogen.Emission, query *gogen.Query, results string) ast.Stmt {
	return ScanRows(emission, query, func(scan Scan, call *ast.CallExpr) []ast.Stmt {
		return rowsScanLoop(query, scan, call, results)
	})
}

// BuildRowsErrCheck constructs the rows.Err() check after the iteration loop.
//
// Returns *ast.IfStmt which is the error check statement.
func BuildRowsErrCheck() *ast.IfStmt {
	return goastutil.IfStmt(
		goastutil.DefineStmt(goast.IdentErr,
			goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentRows), "Err")),
		),
		errorIsNotNil(),
		goastutil.BlockStmt(goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentNil), goastutil.CachedIdent(goast.IdentErr))),
	)
}

// RequireExecResult reports an error for a query whose command reads what Exec returns
// besides an error, such as the rows affected, on a driver whose Exec returns only an
// error.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*sqlmodel.AnalysedQuery) which is the execresult or execrows query.
//
// Returns error when the driver's Exec returns no result, or nil.
func RequireExecResult(emission *gogen.Emission, query *sqlmodel.AnalysedQuery) error {
	if emission.Driver.Connection.ExecResult != nil {
		return nil
	}
	return fmt.Errorf("%w: query %q uses command %s, which needs the result the driver's Exec does not return",
		errUnsupportedCommand, query.Name, query.Command)
}

// ExecRowsBody returns the body of an execrows method after its prelude: the Exec call,
// its error check and the rows affected, with the error RowsAffected returns on a driver
// whose result reports one.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes execCall (*ast.CallExpr) which is the Exec call.
//
// Returns []ast.Stmt which holds the body's statements.
func ExecRowsBody(emission *gogen.Emission, execCall *ast.CallExpr) []ast.Stmt {
	rowsAffected := goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentResults), "RowsAffected"))
	result := goastutil.ReturnStmt(rowsAffected, goastutil.CachedIdent(goast.IdentNil))
	if emission.Driver.Connection.RowsAffectedErrors {
		result = goastutil.ReturnStmt(rowsAffected)
	}
	return []ast.Stmt{
		goastutil.DefineStmtMulti([]string{goast.IdentResults, goast.IdentErr}, execCall),
		BuildErrCheck(goastutil.IntLit(0)),
		result,
	}
}

// DBCall returns the call queries.<field>.<method>(arguments...) of a connection method.
//
// Takes field (string) which is the Queries field holding the connection.
// Takes method (string) which is the connection method.
// Takes arguments ([]ast.Expr) which are the call's arguments.
//
// Returns *ast.CallExpr which is the call.
func DBCall(field string, method string, arguments []ast.Expr) *ast.CallExpr {
	return goastutil.CallExpr(
		goastutil.SelectorExprFrom(
			goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentQueriesReceiver), field),
			method,
		),
		arguments...,
	)
}

// Receiver returns the receiver of every method on *Queries.
//
// Returns *ast.FieldList which is the receiver (queries *Queries).
func Receiver() *ast.FieldList {
	return goastutil.FieldList(
		goastutil.Field(goast.IdentQueriesReceiver, goastutil.StarExpr(goastutil.CachedIdent(goast.IdentQueries))),
	)
}

// ExecReturn returns the statements that run an exec call and return its error: through
// the blank identifier when the driver's Exec returns a result as well, and directly when
// it returns only the error.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes execCall (*ast.CallExpr) which is the Exec call.
//
// Returns []ast.Stmt which holds the statements.
func ExecReturn(emission *gogen.Emission, execCall *ast.CallExpr) []ast.Stmt {
	if emission.Driver.Connection.ExecResult == nil {
		return []ast.Stmt{goastutil.ReturnStmt(execCall)}
	}
	return []ast.Stmt{
		goastutil.DefineStmtMulti([]string{goast.IdentBlank, goast.IdentErr}, execCall),
		goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentErr)),
	}
}

// batchMethod returns the method of a batch or copyfrom query from the driver's
// BatchBuilder, or the plain statement method when the driver has none.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the batch or copyfrom query.
// Takes prelude (*Prelude) which a plain statement method starts with.
//
// Returns ast.Decl which is the method declaration.
// Returns error when the driver cannot generate the method.
func batchMethod(emission *gogen.Emission, query *gogen.Query, prelude *Prelude) (ast.Decl, error) {
	builder := emission.Driver.Batch
	if builder == nil {
		return buildExecMethod(emission, query, prelude), nil
	}
	if query.Analysed.Command == sqlmodel.QueryCommandCopyFrom {
		return builder.CopyFrom(query)
	}
	return builder.Batch(query)
}

// methodDecl wraps a method body in its declaration on *Queries.
//
// Takes query (*gogen.Query) which names the method and its parameters.
// Takes results (*ast.FieldList) which are the method's results.
// Takes body ([]ast.Stmt) which is the method body.
//
// Returns *ast.FuncDecl which is the method declaration.
func methodDecl(query *gogen.Query, results *ast.FieldList, body []ast.Stmt) *ast.FuncDecl {
	return &ast.FuncDecl{
		Recv: Receiver(),
		Name: goastutil.CachedIdent(query.Naming.Method),
		Type: &ast.FuncType{Params: BuildMethodParams(query), Results: results},
		Body: goastutil.BlockStmt(body...),
	}
}

// buildOneMethod constructs a one query method using QueryRow + Scan.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
// Takes prelude (*Prelude) which the method starts with.
//
// Returns *ast.FuncDecl which is the method declaration.
func buildOneMethod(emission *gogen.Emission, query *gogen.Query, prelude *Prelude) *ast.FuncDecl {
	row := query.Naming.Row
	call := PrepareCall(emission, query, prelude.Source, ReturnGuard(zeroRow(row)))
	queryRowCall := ConnectionCall(emission, query, emission.Driver.Connection.QueryRow, &call)

	statements := concat(prelude.Statements, call.Statements)
	failure := goastutil.ReturnStmt(zeroRow(row), goastutil.CachedIdent(goast.IdentErr))
	statements = append(statements, scanOneStatements(emission, query, queryRowCall, failure, BuildErrCheck(zeroRow(row)))...)
	statements = append(statements, BuildEmbedNilCheckStatements(query)...)
	statements = append(statements, goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentRow), goastutil.CachedIdent(goast.IdentNil)))

	return methodDecl(query, goastutil.FieldList(
		goastutil.Field("", goastutil.CachedIdent(row)),
		goastutil.Field("", goastutil.CachedIdent(goast.IdentError)),
	), statements)
}

// scanOneStatements returns the statements that declare the row, allocate its outer
// embeds, scan the one row into it, and check the error.
//
// Takes emission (*gogen.Emission) which names the scan adapter.
// Takes query (*gogen.Query) which is the query.
// Takes queryRowCall (ast.Expr) which is the QueryRow call.
// Takes failure (ast.Stmt) which returns a conversion's error after the scan.
// Takes errorChecks (...ast.Stmt) which check the scan's error.
//
// Returns []ast.Stmt which holds the statements.
func scanOneStatements(emission *gogen.Emission, query *gogen.Query, queryRowCall ast.Expr, failure ast.Stmt, errorChecks ...ast.Stmt) []ast.Stmt {
	embedStatements := BuildEmbedPreAllocStatements(query)
	scan := BuildScan(emission, query)
	statements := make([]ast.Stmt, 0, 2+len(embedStatements)+len(scan.Declarations)+len(errorChecks)+len(scan.Conversions)+len(scan.Checked))
	statements = append(statements, goastutil.VarDecl(goast.IdentRow, goastutil.CachedIdent(query.Naming.Row)))
	statements = append(statements, embedStatements...)
	statements = append(statements, scan.Declarations...)
	statements = append(statements,
		goastutil.DefineStmt(goast.IdentErr,
			goastutil.CallExpr(goastutil.SelectorExprFrom(queryRowCall, "Scan"), scan.Arguments...),
		),
	)
	statements = append(statements, errorChecks...)
	statements = append(statements, scan.Conversions...)
	return append(statements, scan.CheckedConversions(failure)...)
}

// buildOptionalOneMethod constructs an optional one query method using QueryRow + Scan
// that returns (row, bool, error).
//
// A zero-row result yields (zeroRow, false, nil) rather than the driver no-rows sentinel,
// so a legitimately empty match is an ordinary outcome the caller distinguishes by the
// bool instead of an error it must special-case. It is emitted for a command:one query
// whose header sets optional: true.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
// Takes prelude (*Prelude) which the method starts with.
//
// Returns *ast.FuncDecl which is the method declaration.
func buildOptionalOneMethod(emission *gogen.Emission, query *gogen.Query, prelude *Prelude) *ast.FuncDecl {
	row := query.Naming.Row
	sentinel := query.Imports.AddType(emission.Driver.Connection.NoRows)
	call := PrepareCall(emission, query, prelude.Source, ReturnGuard(zeroRow(row), goastutil.CachedIdent(identFalse)))
	queryRowCall := ConnectionCall(emission, query, emission.Driver.Connection.QueryRow, &call)

	noRows := goastutil.IfStmt(nil,
		goastutil.CallExpr(goastutil.SelectorExpr("errors", "Is"), goastutil.CachedIdent(goast.IdentErr), sentinel),
		goastutil.BlockStmt(goastutil.ReturnStmt(zeroRow(row), goastutil.CachedIdent(identFalse), goastutil.CachedIdent(goast.IdentNil))),
	)
	statements := concat(prelude.Statements, call.Statements)
	failure := goastutil.ReturnStmt(zeroRow(row), goastutil.CachedIdent(identFalse), goastutil.CachedIdent(goast.IdentErr))
	statements = append(statements, scanOneStatements(emission, query, queryRowCall, failure, noRows,
		BuildErrCheck(zeroRow(row), goastutil.CachedIdent(identFalse)))...)
	statements = append(statements, BuildEmbedNilCheckStatements(query)...)
	statements = append(statements,
		goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentRow), goastutil.CachedIdent(identTrue), goastutil.CachedIdent(goast.IdentNil)),
	)

	return methodDecl(query, goastutil.FieldList(
		goastutil.Field("", goastutil.CachedIdent(row)),
		goastutil.Field("", goastutil.CachedIdent(goast.IdentBool)),
		goastutil.Field("", goastutil.CachedIdent(goast.IdentError)),
	), statements)
}

// buildManyMethod constructs a many query method using Query + loop.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
// Takes prelude (*Prelude) which the method starts with.
//
// Returns *ast.FuncDecl which is the method declaration.
func buildManyMethod(emission *gogen.Emission, query *gogen.Query, prelude *Prelude) *ast.FuncDecl {
	row := query.Naming.Row
	call := PrepareCall(emission, query, prelude.Source, ReturnGuard(goastutil.CachedIdent(goast.IdentNil)))
	dbCall := ConnectionCall(emission, query, emission.Driver.Connection.Query, &call)
	statements := concat(prelude.Statements, call.Statements)
	statements = append(statements, RowsIterationBody(emission, query, dbCall)...)

	return methodDecl(query, goastutil.FieldList(
		goastutil.Field("", &ast.ArrayType{Elt: goastutil.CachedIdent(row)}),
		goastutil.Field("", goastutil.CachedIdent(goast.IdentError)),
	), statements)
}

// closeRows returns the statement defer rows.Close().
//
// Returns ast.Stmt which is the statement.
func closeRows() ast.Stmt {
	return &ast.DeferStmt{
		Call: goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentRows), "Close")),
	}
}

// rowsScanLoop constructs the inner loop body that declares a row variable, scans into
// it, and appends to results.
//
// Takes query (*gogen.Query) which is the query.
// Takes scan (Scan) which describes scan destinations and conversions.
// Takes call (*ast.CallExpr) which is the rows.Scan call to emit.
// Takes results (string) which names the slice to append completed rows to.
//
// Returns []ast.Stmt which contains the loop body statements.
func rowsScanLoop(query *gogen.Query, scan Scan, call *ast.CallExpr, results string) []ast.Stmt {
	statements := []ast.Stmt{
		goastutil.IfStmt(
			goastutil.DefineStmt(goast.IdentErr, call),
			errorIsNotNil(),
			goastutil.BlockStmt(goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentNil), goastutil.CachedIdent(goast.IdentErr))),
		),
	}
	statements = append(statements, scan.Conversions...)
	statements = append(statements, scan.CheckedConversions(goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentNil), goastutil.CachedIdent(goast.IdentErr)))...)
	if !scan.embedsFinal {
		statements = append(statements, BuildEmbedNilCheckStatements(query)...)
	}
	return append(statements, appendTo(results, goastutil.CachedIdent(goast.IdentRow)))
}

// errorIsNotNil returns the condition err != nil.
//
// Returns ast.Expr which is the condition.
func errorIsNotNil() ast.Expr {
	return &ast.BinaryExpr{X: goastutil.CachedIdent(goast.IdentErr), Op: token.NEQ, Y: goastutil.CachedIdent(goast.IdentNil)}
}

// buildExecMethod constructs an exec query method using Exec.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
// Takes prelude (*Prelude) which the method starts with.
//
// Returns *ast.FuncDecl which is the method declaration.
func buildExecMethod(emission *gogen.Emission, query *gogen.Query, prelude *Prelude) *ast.FuncDecl {
	call := PrepareCall(emission, query, prelude.Source, ReturnGuard())
	execCall := ConnectionCall(emission, query, emission.Driver.Connection.Exec, &call)
	statements := concat(prelude.Statements, call.Statements)
	statements = append(statements, ExecReturn(emission, execCall)...)
	return methodDecl(query, goastutil.FieldList(goastutil.Field("", goastutil.CachedIdent(goast.IdentError))), statements)
}

// buildAsyncExecMethod constructs an asyncexec query method.
//
// The driver call matches exec: it returns the acceptance error while the server may
// still be applying the mutation.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
// Takes prelude (*Prelude) which the method starts with.
//
// Returns *ast.FuncDecl which is the method declaration.
func buildAsyncExecMethod(emission *gogen.Emission, query *gogen.Query, prelude *Prelude) *ast.FuncDecl {
	return buildExecMethod(emission, query, prelude)
}

// buildExecResultMethod constructs an execresult method returning the database-specific
// result type.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
// Takes prelude (*Prelude) which the method starts with.
//
// Returns *ast.FuncDecl which is the method declaration.
func buildExecResultMethod(emission *gogen.Emission, query *gogen.Query, prelude *Prelude) *ast.FuncDecl {
	call := PrepareCall(emission, query, prelude.Source, ReturnGuard(goastutil.CachedIdent(goast.IdentNil)))
	execCall := ConnectionCall(emission, query, emission.Driver.Connection.Exec, &call)
	statements := concat(prelude.Statements, call.Statements)
	statements = append(statements, goastutil.ReturnStmt(execCall))
	return methodDecl(query, goastutil.FieldList(
		goastutil.Field("", query.Imports.AddType(*emission.Driver.Connection.ExecResult)),
		goastutil.Field("", goastutil.CachedIdent(goast.IdentError)),
	), statements)
}

// buildExecRowsMethod constructs an execrows method returning int64.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
// Takes prelude (*Prelude) which the method starts with.
//
// Returns *ast.FuncDecl which is the method declaration.
func buildExecRowsMethod(emission *gogen.Emission, query *gogen.Query, prelude *Prelude) *ast.FuncDecl {
	call := PrepareCall(emission, query, prelude.Source, ReturnGuard(goastutil.IntLit(0)))
	execCall := ConnectionCall(emission, query, emission.Driver.Connection.Exec, &call)
	statements := concat(prelude.Statements, call.Statements)
	statements = append(statements, ExecRowsBody(emission, execCall)...)
	return methodDecl(query, goastutil.FieldList(
		goastutil.Field("", goastutil.CachedIdent(identInt64)),
		goastutil.Field("", goastutil.CachedIdent(goast.IdentError)),
	), statements)
}

// zeroRow returns the composite literal Row{} of a row struct.
//
// Takes row (string) which names the row struct.
//
// Returns ast.Expr which is the literal.
func zeroRow(row string) ast.Expr {
	return goastutil.CompositeLit(goastutil.CachedIdent(row))
}

// concat returns the statements of two lists in order, in a new slice.
//
// Takes first ([]ast.Stmt) which come first.
// Takes second ([]ast.Stmt) which follow.
//
// Returns []ast.Stmt which holds both.
func concat(first, second []ast.Stmt) []ast.Stmt {
	statements := make([]ast.Stmt, 0, len(first)+len(second))
	statements = append(statements, first...)
	return append(statements, second...)
}
