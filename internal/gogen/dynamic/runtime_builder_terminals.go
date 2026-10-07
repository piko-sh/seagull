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

package dynamic

import (
	"cmp"
	"go/ast"
	"go/token"
	"slices"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gogen/methods"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// identFilter is the local holding the conditions the caller added, written as SQL.
	identFilter = "filter"

	// identPagination is the local holding the pagination the caller set, written as SQL.
	identPagination = "pagination"

	// identCount is the local the Count terminal scans the count into.
	identCount = "count"

	// methodBuildQuery is the builder's unexported method assembling its query.
	methodBuildQuery = "buildQuery"

	// methodBuildCountQuery is the builder's unexported method assembling its count.
	methodBuildCountQuery = "buildCountQuery"

	// suffixFilter names the helper that writes the conditions.
	suffixFilter = "FilterInto"

	// suffixPagination names the helper that writes the pagination.
	suffixPagination = "PaginationInto"

	// suffixWhereOpen names the helper that opens the parentheses around a condition.
	suffixWhereOpen = "WhereOpen"

	// suffixWhereClose names the helper that adds the conditions.
	suffixWhereClose = "WhereClose"

	// suffixOrder names the helper that adds the orderings.
	suffixOrder = "Order"

	// suffixAt names the helper that inserts text.
	suffixAt = "At"

	// suffixSplice names the helper that writes edits into a query.
	suffixSplice = "Splice"

	// suffixCheckBinds names the helper that refuses too many bound values.
	suffixCheckBinds = "CheckBinds"

	// identTrue is the predeclared true.
	identTrue = "true"

	// identFalse is the predeclared false.
	identFalse = "false"
)

const (
	// rankWhereOpen orders the parenthesis opening a condition first among edits at one
	// offset.
	rankWhereOpen = iota

	// rankList orders a slice's list next.
	rankList

	// rankWhereClose orders the conditions next.
	rankWhereClose

	// rankOrder orders the orderings next.
	rankOrder

	// rankPagination orders the pagination last.
	rankPagination
)

// edit is one group of edits a terminal writes into its query, at the offset of its first
// edit.
type edit struct {
	// call returns the group of edits.
	call ast.Expr

	// offset is the offset of the group's first edit.
	offset int

	// rank orders the groups at one offset as the clauses go.
	rank int
}

// assembly is what one terminal's query is assembled from: the rendering, its marks, and
// where the caller's additions go.
type assembly struct {
	// arguments reads the rendering's values from the builder.
	arguments *methods.Arguments

	// constant names the constant the query starts from.
	constant string

	// splits holds where, among the rendering's arguments, the values the caller's
	// conditions and pagination bind go for an anonymous form.
	splits splits

	// ordered reports a terminal that writes orderings and pagination.
	ordered bool
}

// splits says where, among a rendering's arguments in binding order, the values of the
// caller's conditions and pagination go.
type splits struct {
	// filter is the number of arguments before the conditions' values.
	filter int

	// pagination is the number of arguments before the pagination's values.
	pagination int
}

// occurrenceCounter counts a template's placeholder occurrences in the order they are
// written, noting the count at the end of the filter and the start of the pagination.
type occurrenceCounter struct {
	// count is the number of occurrences seen so far.
	count int

	// filter is the count at the end of the filter, or -1 before it is seen.
	filter int

	// pagination is the count at the start of the pagination, or -1 before it is seen.
	pagination int
}

// walk counts the occurrences of segments, depth first.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the segments.
func (counter *occurrenceCounter) walk(segments []sqlmodel.SQLSegment) {
	for index := range segments {
		switch segments[index].Kind {
		case sqlmodel.SegmentParameter, sqlmodel.SegmentList:
			counter.count++
		case sqlmodel.SegmentFilter:
			counter.walk(segments[index].Children)
			counter.filter = counter.count
		case sqlmodel.SegmentPagination:
			counter.pagination = counter.count
			counter.walk(segments[index].Children)
		default:
			counter.walk(segments[index].Children)
		}
	}
}

// buildQueryMethod constructs the buildQuery() method that writes the caller's
// conditions, orderings and pagination into the query's SQL and returns it with the
// values it binds.
//
// Returns *ast.FuncDecl which is the buildQuery method declaration.
func (b *builder) buildQueryMethod() *ast.FuncDecl {
	return b.assembleMethod(methodBuildQuery, &assembly{
		arguments: b.arguments(&b.query.Rendered),
		constant:  b.query.Naming.SQLConstant,
		splits:    b.splits(b.query.Analysed.Template.Segments, len(b.query.Rendered.Arguments)),
		ordered:   true,
	})
}

// buildCountQueryMethod constructs the buildCountQuery() method that writes the caller's
// conditions into the count's SQL, with its own numbering, and returns it with the values
// it binds. Orderings and pagination do not change a count.
//
// Returns *ast.FuncDecl which is the buildCountQuery method declaration.
func (b *builder) buildCountQueryMethod() *ast.FuncDecl {
	return b.assembleMethod(methodBuildCountQuery, &assembly{
		arguments: b.arguments(b.query.Count),
		constant:  b.query.Naming.CountConstant,
		splits:    b.splits(b.query.Analysed.CountTemplate.Segments, len(b.query.Count.Arguments)),
		ordered:   false,
	})
}

// arguments returns how a rendering's values are read from the builder.
//
// Takes rendered (*emitkit.Rendered) which is the rendering.
//
// Returns *methods.Arguments which reads them.
func (b *builder) arguments(rendered *emitkit.Rendered) *methods.Arguments {
	return &methods.Arguments{Query: b.query, Rendered: rendered, Access: b.parameterAccess}
}

// splits returns where the caller's values go among a rendering's arguments: after them
// all for a numbered form, whose placeholders the helpers number after the rendering's,
// and after the placeholders written before the condition's end and before the
// pagination's place for an anonymous form, whose values follow the text.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the rendered template's segments.
// Takes total (int) which is the number of the rendering's arguments.
//
// Returns splits which says where they go.
func (b *builder) splits(segments []sqlmodel.SQLSegment, total int) splits {
	if b.emission.BindForm().Kind != dialect.PlaceholderAnonymous {
		return splits{filter: total, pagination: total}
	}
	counter := &occurrenceCounter{count: 0, filter: -1, pagination: -1}
	counter.walk(segments)
	found := splits{filter: counter.filter, pagination: counter.pagination}
	if found.filter < 0 {
		found.filter = total
	}
	if found.pagination < 0 {
		found.pagination = total
	}
	return found
}

// assembleMethod constructs an unexported builder method that returns the pending error,
// writes the caller's additions as SQL and values, gathers every value in binding order,
// checks them against the bind limit, and returns the query spliced with the additions.
//
// Takes name (string) which names the method.
// Takes plan (*assembly) which says what it assembles.
//
// Returns *ast.FuncDecl which is the method declaration.
func (b *builder) assembleMethod(name string, plan *assembly) *ast.FuncDecl {
	next := plan.arguments.Count(1)
	var capacity []ast.Expr
	statements := make([]ast.Stmt, 0, 4+len(plan.arguments.Rendered.Arguments))
	statements = append(statements, pendingErrorGuard(goastutil.StrLit(""), goastutil.CachedIdent(goast.IdentNil)))
	if b.filters(plan) {
		capacity = append(capacity, b.helperCall("FilterCount", builderField(fieldPredicates)))
	}
	if plan.ordered && b.pagination != nil {
		capacity = append(capacity, b.helperCall("PaginationCount", builderField(fieldLimit), builderField(fieldOffset)))
	}
	statements = append(statements, goastutil.DefineStmt(goast.IdentArgs, goastutil.CallExpr(
		goastutil.CachedIdent("make"),
		&ast.ArrayType{Elt: goastutil.CachedIdent(goast.IdentAny)},
		goastutil.IntLit(0),
		sum(plan.arguments.Count(0), capacity...),
	)))
	statements = append(statements, b.gatherArguments(plan, next)...)
	statements = append(statements,
		goastutil.IfStmt(
			goastutil.DefineStmt(goast.IdentErr, goastutil.CallExpr(goastutil.CachedIdent(b.helper(suffixCheckBinds)), lengthOf(goast.IdentArgs))),
			&ast.BinaryExpr{X: goastutil.CachedIdent(goast.IdentErr), Op: token.NEQ, Y: goastutil.CachedIdent(goast.IdentNil)},
			goastutil.BlockStmt(goastutil.ReturnStmt(goastutil.StrLit(""), goastutil.CachedIdent(goast.IdentNil), goastutil.CachedIdent(goast.IdentErr))),
		),
		goastutil.ReturnStmt(b.splice(plan), goastutil.CachedIdent(goast.IdentArgs), goastutil.CachedIdent(goast.IdentNil)),
	)
	return &ast.FuncDecl{
		Recv: b.receiver(),
		Name: goastutil.CachedIdent(name),
		Type: &ast.FuncType{Results: goastutil.FieldList(
			goastutil.Field("", goastutil.CachedIdent(goast.IdentString)),
			goastutil.Field("", &ast.ArrayType{Elt: goastutil.CachedIdent(goast.IdentAny)}),
			goastutil.Field("", goastutil.CachedIdent(goast.IdentError)),
		)},
		Body: goastutil.BlockStmt(statements...),
	}
}

// gatherArguments returns the statements that append every value to args in binding
// order: the rendering's values up to the conditions', the conditions' values, the
// rendering's values up to the pagination's, the pagination's values, and the rest.
//
// Takes plan (*assembly) which says what is assembled.
// Takes next (ast.Expr) which is the first placeholder number for the runtime additions.
//
// Returns []ast.Stmt which holds the statements.
func (b *builder) gatherArguments(plan *assembly, next ast.Expr) []ast.Stmt {
	total := len(plan.arguments.Rendered.Arguments)
	statements := plan.arguments.Append(b.emission, 0, plan.splits.filter)
	if b.filters(plan) {
		statements = append(statements, goastutil.DefineStmtMulti([]string{identFilter, goast.IdentArgs},
			b.helperCall(suffixFilter, builderField(fieldPredicates), next, goastutil.CachedIdent(goast.IdentArgs))))
	}
	statements = append(statements, plan.arguments.Append(b.emission, plan.splits.filter, plan.splits.pagination)...)
	if plan.ordered && b.pagination != nil {
		if b.filters(plan) {
			next = sum(next, b.helperCall("FilterCount", builderField(fieldPredicates)))
		}
		statements = append(statements, goastutil.DefineStmtMulti([]string{identPagination, goast.IdentArgs},
			b.helperCall(suffixPagination, builderField(fieldLimit), builderField(fieldOffset), next, goastutil.CachedIdent(goast.IdentArgs))))
	}
	return append(statements, plan.arguments.Append(b.emission, plan.splits.pagination, total)...)
}

// splice returns the call that writes the slices' lists and the caller's additions into
// the query's constant, in the order of their offsets.
//
// Takes plan (*assembly) which says what is assembled.
//
// Returns ast.Expr which is the call.
func (b *builder) splice(plan *assembly) ast.Expr {
	marks := &plan.arguments.Rendered.Marks
	var edits []edit
	for index, call := range plan.arguments.ListEdits(b.emission) {
		edits = append(edits, edit{call: call, offset: marks.Lists[index].Operator.Start, rank: rankList})
	}
	if filter := marks.Filter; b.filters(plan) {
		present := boolIdent(filter.Present)
		edits = append(edits,
			edit{call: b.helperCall(suffixWhereOpen, goastutil.CachedIdent(identFilter), goastutil.IntLit(filter.Condition.Start), present), offset: filter.Condition.Start, rank: rankWhereOpen},
			edit{call: b.helperCall(suffixWhereClose, goastutil.CachedIdent(identFilter), goastutil.IntLit(filter.InsertAt), present), offset: filter.InsertAt, rank: rankWhereClose},
		)
	}
	if order := marks.OrderBy; plan.ordered && order != nil {
		present := order.Start != order.End
		offset := order.Start
		if present {
			offset = order.End
		}
		edits = append(edits, edit{
			call:   b.helperCall(suffixOrder, builderField(fieldOrders), goastutil.IntLit(order.Start), goastutil.IntLit(order.End), boolIdent(present)),
			offset: offset,
			rank:   rankOrder,
		})
	}
	if plan.ordered && b.pagination != nil {
		edits = append(edits, edit{call: b.helperCall(suffixAt, goastutil.CachedIdent(identPagination), goastutil.IntLit(b.pagination.Start)), offset: b.pagination.Start, rank: rankPagination})
	}
	slices.SortStableFunc(edits, func(left, right edit) int {
		return cmp.Or(cmp.Compare(left.offset, right.offset), cmp.Compare(left.rank, right.rank))
	})
	arguments := make([]ast.Expr, 0, 1+len(edits))
	arguments = append(arguments, goastutil.CachedIdent(plan.constant))
	for index := range edits {
		arguments = append(arguments, edits[index].call)
	}
	return goastutil.CallExpr(goastutil.CachedIdent(b.helper(suffixSplice)), arguments...)
}

// filters reports whether an assembled query writes the caller's conditions: the builder
// takes conditions and the rendering has a place for them.
//
// Takes plan (*assembly) which says what is assembled.
//
// Returns bool which is true when it writes them.
func (b *builder) filters(plan *assembly) bool {
	return b.filter != nil && plan.arguments.Rendered.Marks.Filter != nil
}

// helperCall returns a call of a generated helper.
//
// Takes suffix (string) which names the helper without its prefix.
// Takes arguments (...ast.Expr) which are the call's arguments.
//
// Returns ast.Expr which is the call.
func (b *builder) helperCall(suffix string, arguments ...ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent(b.helper(suffix)), arguments...)
}

// allMethod constructs the All(ctx) terminal, which runs the assembled query and reads
// every row.
//
// Returns *ast.FuncDecl which is the All method declaration.
func (b *builder) allMethod() *ast.FuncDecl {
	row := b.query.Naming.Row
	return b.terminal("All", &ast.ArrayType{Elt: goastutil.CachedIdent(row)}, []ast.Stmt{
		goastutil.DefineStmtMulti([]string{goast.IdentQuery, goast.IdentArgs, goast.IdentErr}, callOwn(methodBuildQuery)),
		methods.BuildErrCheck(goastutil.CachedIdent(goast.IdentNil)),
		goastutil.DefineStmtMulti([]string{goast.IdentRows, goast.IdentErr}, b.readerCall(b.emission.Driver.Connection.Query)),
		methods.BuildErrCheck(goastutil.CachedIdent(goast.IdentNil)),
		&ast.DeferStmt{Call: goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentRows), "Close"))},
		methods.ResultsDeclaration(b.emission, goast.IdentItems, row),
		methods.CollectRows(b.emission, b.query, goast.IdentItems),
		methods.BuildRowsErrCheck(),
		goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentItems), goastutil.CachedIdent(goast.IdentNil)),
	})
}

// oneMethod constructs the One(ctx) terminal, which runs the assembled query and reads
// one row. It returns the zero row with any error, never a row read in part.
//
// Returns *ast.FuncDecl which is the One method declaration.
func (b *builder) oneMethod() *ast.FuncDecl {
	row := b.query.Naming.Row
	zero := goastutil.CompositeLit(goastutil.CachedIdent(row))
	reading := methods.BuildScan(b.emission, b.query)
	statements := make([]ast.Stmt, 0, 6+len(reading.Declarations)+len(reading.Conversions)+len(reading.Checked))
	statements = append(statements,
		goastutil.DefineStmtMulti([]string{goast.IdentQuery, goast.IdentArgs, goast.IdentErr}, callOwn(methodBuildQuery)),
		methods.BuildErrCheck(zero),
		goastutil.VarDecl(goast.IdentRow, goastutil.CachedIdent(row)),
	)
	statements = append(statements, reading.Declarations...)
	statements = append(statements, goastutil.AssignStmt(goastutil.CachedIdent(goast.IdentErr), goastutil.CallExpr(
		goastutil.SelectorExprFrom(b.readerCall(b.emission.Driver.Connection.QueryRow), "Scan"),
		reading.Arguments...,
	)), methods.BuildErrCheck(zero))
	statements = append(statements, reading.Conversions...)
	statements = append(statements, reading.CheckedConversions(goastutil.ReturnStmt(zero, goastutil.CachedIdent(goast.IdentErr)))...)
	statements = append(statements, goastutil.ReturnStmt(goastutil.CachedIdent(goast.IdentRow), goastutil.CachedIdent(goast.IdentNil)))
	return b.terminal("One", goastutil.CachedIdent(row), statements)
}

// countMethod constructs the Count(ctx) terminal, which runs the assembled count and
// scans it, converting the driver's count type to int64 when it is another. It returns
// zero with any error.
//
// Returns *ast.FuncDecl which is the Count method declaration.
func (b *builder) countMethod() *ast.FuncDecl {
	countType := identInt64
	if scan := b.emission.Driver.Connection.CountScan; scan != "" {
		countType = scan
	}
	var result ast.Expr = goastutil.CachedIdent(identCount)
	if countType != identInt64 {
		result = goastutil.CallExpr(goastutil.CachedIdent(identInt64), result)
	}
	return b.terminal("Count", goastutil.CachedIdent(identInt64), []ast.Stmt{
		goastutil.DefineStmtMulti([]string{goast.IdentQuery, goast.IdentArgs, goast.IdentErr}, callOwn(methodBuildCountQuery)),
		methods.BuildErrCheck(goastutil.IntLit(0)),
		goastutil.VarDecl(identCount, goastutil.CachedIdent(countType)),
		goastutil.AssignStmt(goastutil.CachedIdent(goast.IdentErr), goastutil.CallExpr(
			goastutil.SelectorExprFrom(b.readerCall(b.emission.Driver.Connection.QueryRow), "Scan"),
			goastutil.AddressExpr(goastutil.CachedIdent(identCount)),
		)),
		methods.BuildErrCheck(goastutil.IntLit(0)),
		goastutil.ReturnStmt(result, goastutil.CachedIdent(goast.IdentNil)),
	})
}

// terminal wraps a terminal's body in its declaration, taking the context and returning a
// value and an error.
//
// Takes name (string) which names the terminal.
// Takes result (ast.Expr) which is the type of its value.
// Takes body ([]ast.Stmt) which is its body.
//
// Returns *ast.FuncDecl which is the method declaration.
func (b *builder) terminal(name string, result ast.Expr, body []ast.Stmt) *ast.FuncDecl {
	return &ast.FuncDecl{
		Recv: b.receiver(),
		Name: goastutil.CachedIdent(name),
		Type: &ast.FuncType{
			Params: goastutil.FieldList(goastutil.Field(goast.IdentCtx, goastutil.SelectorExpr(goast.IdentContext, goast.IdentContextType))),
			Results: goastutil.FieldList(
				goastutil.Field("", result),
				goastutil.Field("", goastutil.CachedIdent(goast.IdentError)),
			),
		},
		Body: goastutil.BlockStmt(body...),
	}
}

// readerCall returns the call a terminal runs its query with:
// builder.queries.<reader>.<method>(ctx, query, args...).
//
// Takes method (string) which is the connection method.
//
// Returns *ast.CallExpr which is the call.
func (b *builder) readerCall(method string) *ast.CallExpr {
	return &ast.CallExpr{
		Fun: goastutil.SelectorExprFrom(
			goastutil.SelectorExprFrom(builderField(goast.IdentQueriesReceiver), b.emission.Driver.Connection.Reader),
			method,
		),
		Args:     []ast.Expr{goastutil.CachedIdent(goast.IdentCtx), goastutil.CachedIdent(goast.IdentQuery), goastutil.CachedIdent(goast.IdentArgs)},
		Ellipsis: 1,
	}
}

// pendingErrorGuard constructs the guard that returns builder.pendingError, set by a
// chainable call that met an error, before anything is assembled.
//
// Takes zeroValues (...ast.Expr) which are returned ahead of the pending error.
//
// Returns *ast.IfStmt which is the guard statement.
func pendingErrorGuard(zeroValues ...ast.Expr) *ast.IfStmt {
	results := make([]ast.Expr, 0, len(zeroValues)+1)
	results = append(results, zeroValues...)
	return &ast.IfStmt{
		Cond: &ast.BinaryExpr{X: builderField(fieldPendingError), Op: token.NEQ, Y: goastutil.CachedIdent(goast.IdentNil)},
		Body: goastutil.BlockStmt(goastutil.ReturnStmt(append(results, builderField(fieldPendingError))...)),
	}
}

// callOwn returns the call builder.<method>() of the builder's own method.
//
// Takes method (string) which names the method.
//
// Returns ast.Expr which is the call.
func callOwn(method string) ast.Expr {
	return goastutil.CallExpr(goastutil.SelectorExprFrom(goastutil.CachedIdent(goast.IdentBuilder), method))
}

// lengthOf returns the call len(name).
//
// Takes name (string) which names the slice.
//
// Returns ast.Expr which is the call.
func lengthOf(name string) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent("len"), goastutil.CachedIdent(name))
}

// sum returns an expression plus others, leaving out a first term of zero when there are
// others.
//
// Takes first (ast.Expr) which is the first term.
// Takes others (...ast.Expr) which are added to it.
//
// Returns ast.Expr which is the sum.
func sum(first ast.Expr, others ...ast.Expr) ast.Expr {
	if literal, ok := first.(*ast.BasicLit); ok && literal.Value == "0" && len(others) > 0 {
		first, others = others[0], others[1:]
	}
	total := first
	for _, other := range others {
		total = &ast.BinaryExpr{X: total, Op: token.ADD, Y: other}
	}
	return total
}

// boolIdent returns the predeclared identifier of a boolean.
//
// Takes value (bool) which is the boolean.
//
// Returns ast.Expr which is true or false.
func boolIdent(value bool) ast.Expr {
	if value {
		return goastutil.CachedIdent(identTrue)
	}
	return goastutil.CachedIdent(identFalse)
}
