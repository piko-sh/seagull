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

// Package multirow builds batch methods that repeat VALUES rows within a statement's bind
// limit.
package multirow

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/methods"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// decimalBase is the radix of SQL placeholder numbers.
	decimalBase = 10

	// importContext is the import path of context.
	importContext = "context"

	// importStrings is the import path of strings.
	importStrings = "strings"

	// importStrconv is the import path of strconv.
	importStrconv = "strconv"

	// identOffset is the local holding the index of a chunk's first row.
	identOffset = "offset"

	// identEnd is the local holding the index past a chunk's last row.
	identEnd = "end"

	// identChunk is the local holding a chunk's rows.
	identChunk = "chunk"

	// identValues is the local the chunk's rows are written to.
	identValues = "values"

	// identIndex is the local holding a row's index within its chunk.
	identIndex = "i"

	// identItem is the local holding one row's parameters.
	identItem = "item"

	// identRowNumber is the local holding a row's index as text, which ends the names of the
	// row's named arguments.
	identRowNumber = "row"

	// rowSeparatorText separates the rows of a multi-row VALUES.
	rowSeparatorText = ", "

	// chunkLabel follows a batch method's name in the label of a failed statement's error,
	// before the index of the row the statement starts at.
	chunkLabel = ": the statement starting at row "
)

var (
	// Locals lists the locals the method declares besides the shared ones, which an emitter
	// using it reserves.
	Locals = []string{identChunk, identEnd, identOffset, identValues}
)

// Binder binds one argument of a row: the item's field, through the driver's envelope,
// under the name the row binds it by for a named form.
type Binder func(access ast.Expr, parameter *sqlmodel.QueryParameter, name ast.Expr) ast.Expr

// Spec says how one driver runs the multi-row statements of a batch.
type Spec struct {
	// Bind binds one argument of a row.
	Bind Binder

	// Connection says how the generated code calls the connection: its writer field, its
	// Exec method and whether Exec returns a result besides the error.
	Connection gogen.Connection

	// Vocabulary names the helper labelling a failed statement's error with the row it
	// starts at, when the style asks for labelled errors.
	Vocabulary gogen.Vocabulary

	// Limit is the most arguments one statement binds.
	Limit int

	// PerRow sends each row in a statement of its own rather than many rows in one, for a
	// statement whose clause after its VALUES row acts on the rows of the statement, such as
	// an upsert that may meet one key twice in a batch.
	PerRow bool
}

// batchStatement is what the multi-row method of one batch or copyfrom query writes.
type batchStatement struct {
	// spec says how the driver runs the statements.
	spec *Spec

	// query is the query being generated.
	query *gogen.Query

	// row is the query's VALUES row template.
	row emitkit.Row

	// rowsPerStatement is how many rows one statement carries.
	rowsPerStatement int
}

// Method builds a batch or copyfrom method using multi-row INSERT statements, or one
// statement per row when the spec asks for it.
//
// Placeholders are distinct across rows and arguments retain their order. A row binding
// more arguments than one statement binds could never be sent, so it is refused here, as
// emitkit.CheckBatch refuses it given the same limit.
//
// Takes query (*gogen.Query) which is the query being generated.
// Takes spec (*Spec) which says how the driver runs the statements.
//
// Returns *ast.FuncDecl which is the method declaration.
// Returns error when the query's VALUES row cannot be repeated, or binds more arguments
// than the limit.
func Method(query *gogen.Query, spec *Spec) (*ast.FuncDecl, error) {
	row, err := emitkit.RowTemplate(query.Analysed, query.Renderer)
	if err != nil {
		return nil, err
	}
	limit := spec.Limit
	if len(row.Arguments) > limit {
		return nil, fmt.Errorf("%w: query %s binds %d arguments in each row, more than the %d one statement binds",
			errRowTooWide, query.Analysed.Name, len(row.Arguments), limit)
	}
	rowsPerStatement := max(limit/max(len(row.Arguments), 1), 1)
	if spec.PerRow {
		rowsPerStatement = 1
	}
	statement := &batchStatement{spec: spec, query: query, row: row, rowsPerStatement: rowsPerStatement}
	body := []ast.Stmt{
		goastutil.IfStmt(nil,
			&ast.BinaryExpr{X: lengthOfParams(query), Op: token.EQL, Y: goastutil.IntLit(0)},
			goastutil.BlockStmt(goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentNil)))),
		chunkLoop(statement),
		goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentNil)),
	}
	return &ast.FuncDecl{
		Recv: methods.Receiver(),
		Name: goastutil.CachedIdent(query.Naming.Method),
		Type: &ast.FuncType{
			Params: goastutil.FieldList(
				goastutil.Field(goast.IdentCtx, query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: importContext, Name: "Context"})),
				goastutil.Field(query.Naming.ParamsArgument, &ast.ArrayType{Elt: goastutil.CachedIdent(query.Naming.Params)}),
			),
			Results: goastutil.FieldList(goastutil.Field("", goastutil.CachedIdent(goast.IdentError))),
		},
		Body: goastutil.BlockStmt(body...),
	}, nil
}

// lengthOfParams builds `len(params)`.
//
// Takes query (*gogen.Query) which names the params argument.
//
// Returns ast.Expr which is the call.
func lengthOfParams(query *gogen.Query) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent("len"), goastutil.CachedIdent(query.Naming.ParamsArgument))
}

// chunkLoop builds a loop that writes and executes rows in bounded chunks.
//
// Each chunk becomes one statement with its own bind arguments.
//
// Takes statement (*batchStatement) which describes the statement.
//
// Returns ast.Stmt which is the loop.
func chunkLoop(statement *batchStatement) ast.Stmt {
	query := statement.query
	parametersArgument := goastutil.CachedIdent(query.Naming.ParamsArgument)
	step := goastutil.IntLit(statement.rowsPerStatement)
	body := []ast.Stmt{
		goastutil.DefineStmt(identEnd, goastutil.CallExpr(goastutil.CachedIdent("min"),
			&ast.BinaryExpr{X: goastutil.CachedIdent(identOffset), Op: token.ADD, Y: step}, lengthOfParams(query))),
		goastutil.DefineStmt(identChunk, &ast.SliceExpr{X: parametersArgument, Low: goastutil.CachedIdent(identOffset), High: goastutil.CachedIdent(identEnd)}),
		goastutil.DefineStmt(goast.IdentArgs, goastutil.CallExpr(goastutil.CachedIdent("make"),
			&ast.ArrayType{Elt: goastutil.CachedIdent(goast.IdentAny)},
			goastutil.IntLit(0),
			&ast.BinaryExpr{
				X:  goastutil.CallExpr(goastutil.CachedIdent("len"), goastutil.CachedIdent(identChunk)),
				Op: token.MUL,
				Y:  goastutil.IntLit(len(statement.row.Arguments)),
			})),
		goastutil.VarDecl(identValues, query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: importStrings, Name: "Builder"})),
	}
	body = append(body, prepareValues(statement)...)
	body = append(body, &ast.RangeStmt{
		Key:   goastutil.CachedIdent(identIndex),
		Value: goastutil.CachedIdent(identItem),
		Tok:   token.DEFINE,
		X:     goastutil.CachedIdent(identChunk),
		Body:  goastutil.BlockStmt(rowStatements(statement)...),
	})
	if statement.row.Values.End < len(query.Rendered.SQL) {
		body = append(body, writeValues(&ast.SliceExpr{X: goastutil.CachedIdent(query.Naming.SQLConstant), Low: goastutil.IntLit(statement.row.Values.End)}))
	}
	body = append(body, execute(statement))
	return &ast.ForStmt{
		Init: goastutil.DefineStmt(identOffset, goastutil.IntLit(0)),
		Cond: &ast.BinaryExpr{X: goastutil.CachedIdent(identOffset), Op: token.LSS, Y: lengthOfParams(query)},
		Post: &ast.AssignStmt{Lhs: []ast.Expr{goastutil.CachedIdent(identOffset)}, Tok: token.ADD_ASSIGN, Rhs: []ast.Expr{step}},
		Body: goastutil.BlockStmt(body...),
	}
}

// execute builds the call that runs one chunk's statement and returns its error.
//
//	if _, err := queries.writer.ExecContext(ctx, sql, args...); err != nil {
//		return err
//	}
//
// without the blank result for a connection whose Exec returns only an error, and with
// the error labelled with the method's name and the row the chunk starts at, the rows
// before it already written, when the style asks for labelled errors.
//
// Takes statement (*batchStatement) which describes the statement.
//
// Returns ast.Stmt which is the statement.
func execute(statement *batchStatement) ast.Stmt {
	connection := &statement.spec.Connection
	names := []string{goast.IdentErr}
	if connection.ExecResult != nil {
		names = []string{goast.IdentBlank, goast.IdentErr}
	}
	call := &ast.CallExpr{
		Fun: goastutil.SelectorExprFrom(
			goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentQueriesReceiver), connection.Writer),
			connection.Exec,
		),
		Args:     []ast.Expr{goastutil.CachedIdent(goast.IdentCtx), statementText(statement), goastutil.CachedIdent(goast.IdentArgs)},
		Ellipsis: 1,
	}
	var failure ast.Expr = goastutil.CachedIdent(goast.IdentErr)
	if vocabulary := statement.spec.Vocabulary; vocabulary.WrapsErrors() {
		label := &ast.BinaryExpr{
			X:  goastutil.StrLit(vocabulary.Message(statement.query.Naming.Method + chunkLabel)),
			Op: token.ADD,
			Y: goastutil.CallExpr(statement.query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: importStrconv, Name: "Itoa"}),
				goastutil.CachedIdent(identOffset)),
		}
		failure = vocabulary.WrapError(label, failure)
	}
	return goastutil.IfStmt(
		goastutil.DefineStmtMulti(names, call),
		&ast.BinaryExpr{X: goastutil.CachedIdent(goast.IdentErr), Op: token.NEQ, Y: goastutil.CachedIdent(goast.IdentNil)},
		goastutil.BlockStmt(goastutil.ReturnStmt(failure)),
	)
}

// prepareValues reserves one SQL buffer for the whole statement, including its fixed
// prefix and suffix. Placeholder widths are bounded by the driver's chunk limit.
//
// Takes statement (*batchStatement) which describes the batch query and its parameter
// bindings.
//
// Returns []ast.Stmt which initialises the output builder and argument buffer.
func prepareValues(statement *batchStatement) []ast.Stmt {
	row := statement.row
	width := len(strconv.Itoa(statement.rowsPerStatement * max(len(row.Arguments), 1)))
	perRow := len(rowSeparatorText) + len(row.Slots)*width
	for _, part := range row.Parts {
		perRow += len(part)
	}
	fixed := len(statement.query.Rendered.SQL) - (row.Values.End - row.Values.Start)
	size := &ast.BinaryExpr{X: goastutil.IntLit(fixed), Op: token.ADD, Y: &ast.BinaryExpr{
		X: goastutil.CallExpr(goastutil.CachedIdent("len"), goastutil.CachedIdent(identChunk)), Op: token.MUL, Y: goastutil.IntLit(perRow)}}
	return []ast.Stmt{
		goastutil.ExprStmt(goastutil.CallExpr(goastutil.SelectorExpr(identValues, "Grow"), size)),
		writeValues(&ast.SliceExpr{X: goastutil.CachedIdent(statement.query.Naming.SQLConstant), High: goastutil.IntLit(row.Values.Start)}),
	}
}

// statementText returns the entire statement already written into the shared buffer.
//
// Returns ast.Expr which reads the completed SQL builder's string.
func statementText(_ *batchStatement) ast.Expr {
	return goastutil.CallExpr(goastutil.SelectorExpr(identValues, "String"))
}

// rowStatements builds the SQL and argument appends for one row of a multi-row statement.
//
// Named placeholders receive a row-specific suffix to keep their binds distinct.
//
// Takes statement (*batchStatement) which describes the statement.
//
// Returns []ast.Stmt which holds the statements.
func rowStatements(statement *batchStatement) []ast.Stmt {
	query := statement.query
	statements := []ast.Stmt{goastutil.IfStmt(nil,
		&ast.BinaryExpr{X: goastutil.CachedIdent(identIndex), Op: token.GTR, Y: goastutil.IntLit(0)},
		goastutil.BlockStmt(writeValues(goastutil.StrLit(rowSeparatorText))))}
	if statement.row.Kind == dialect.PlaceholderNamed {
		statements = append(statements, goastutil.DefineStmt(identRowNumber,
			goastutil.CallExpr(query.Imports.AddType(sqlmodel.GoType{Composite: nil, Package: importStrconv, Name: "Itoa"}), goastutil.CachedIdent(identIndex))))
	}
	statements = append(statements, rowText(statement)...)
	arguments := make([]ast.Expr, 0, len(statement.row.Arguments)+1)
	arguments = append(arguments, goastutil.CachedIdent(goast.IdentArgs))
	for index, parameter := range statement.row.Arguments {
		arguments = append(arguments, rowArgument(statement, index, &query.Analysed.Parameters[parameter], query.Naming.ParameterFields[parameter]))
	}
	return append(statements, goastutil.AssignStmt(goastutil.CachedIdent(goast.IdentArgs), goastutil.CallExpr(goastutil.CachedIdent("append"), arguments...)))
}

// rowText writes a row directly into the statement buffer, avoiding temporary strings for
// numbered placeholders and concatenated rows.
//
// Takes statement (*batchStatement) which describes the batch row and its parameter
// bindings.
//
// Returns []ast.Stmt which appends one row's SQL and parameter placeholders.
func rowText(statement *batchStatement) []ast.Stmt {
	row := statement.row
	var statements []ast.Stmt
	if row.Kind != dialect.PlaceholderNamed && len(row.Slots) > 0 {
		statements = append(statements, goastutil.VarDecl("digits", &ast.ArrayType{Len: goastutil.IntLit(20), Elt: goastutil.CachedIdent("byte")}))
	}
	for index, part := range row.Parts {
		if part != "" {
			statements = append(statements, writeValues(goastutil.StrLit(part)))
		}
		if index == len(row.Slots) {
			break
		}
		if row.Kind == dialect.PlaceholderNamed {
			statements = append(statements, writeValues(goastutil.CachedIdent(identRowNumber)))
			continue
		}
		digits := goastutil.CallExpr(
			statement.query.Imports.AddType(sqlmodel.GoType{Package: importStrconv, Name: "AppendInt", Composite: nil}),
			&ast.SliceExpr{X: goastutil.CachedIdent("digits"), High: goastutil.IntLit(0)},
			goastutil.CallExpr(goastutil.CachedIdent("int64"), placeNumber(len(row.Arguments), row.Slots[index])), goastutil.IntLit(decimalBase))
		statements = append(statements, goastutil.ExprStmt(goastutil.CallExpr(goastutil.SelectorExpr(identValues, "Write"), digits)))
	}
	return statements
}

// placeNumber builds the number of a numbered place: `i*arguments + slot + 1`.
//
// Takes arguments (int) which is the number of arguments a row binds.
// Takes slot (int) which is the index of the place's argument among them.
//
// Returns ast.Expr which is the expression.
func placeNumber(arguments, slot int) ast.Expr {
	var rowBase ast.Expr = goastutil.CachedIdent(identIndex)
	if arguments != 1 {
		rowBase = &ast.BinaryExpr{X: rowBase, Op: token.MUL, Y: goastutil.IntLit(arguments)}
	}
	return &ast.BinaryExpr{X: rowBase, Op: token.ADD, Y: goastutil.IntLit(slot + 1)}
}

// rowArgument builds one argument a row binds: the item's field, bound by the driver,
// under the name ending with the row's index for a named form.
//
// Takes statement (*batchStatement) which describes the statement.
// Takes index (int) which is the argument's index among the row's.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter it carries.
// Takes field (string) which is the Params field holding it.
//
// Returns ast.Expr which binds the argument.
func rowArgument(statement *batchStatement, index int, parameter *sqlmodel.QueryParameter, field string) ast.Expr {
	access := goastutil.SelectorExprFrom(goastutil.CachedIdent(identItem), field)
	if statement.row.Kind != dialect.PlaceholderNamed {
		return statement.spec.Bind(access, parameter, nil)
	}
	name := &ast.BinaryExpr{X: goastutil.StrLit(statement.row.Names[index]), Op: token.ADD, Y: goastutil.CachedIdent(identRowNumber)}
	return statement.spec.Bind(access, parameter, name)
}

// writeValues builds `values.WriteString(text)`.
//
// Takes text (ast.Expr) which is the text written.
//
// Returns ast.Stmt which is the call.
func writeValues(text ast.Expr) ast.Stmt {
	return goastutil.ExprStmt(goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(identValues), "WriteString"), text))
}
