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

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// identElement is the loop variable a slice's elements are appended through.
	identElement = "v"

	// suffixCheckBinds is the helper that refuses more bound values than a statement binds.
	suffixCheckBinds = "CheckBinds"

	// suffixSplice is the helper that writes edits into a query's SQL.
	suffixSplice = "Splice"

	// suffixList is the helper that writes a slice parameter's list.
	suffixList = "List"

	// suffixListMark is the helper type that locates a slice parameter's list.
	suffixListMark = "ListMark"
)

// Call is how a method runs its query: the statements that settle the SQL and the
// arguments, and the arguments of the connection call.
type Call struct {
	// Statements run before the call.
	Statements []ast.Stmt

	// Arguments are the call's arguments: the context, the SQL, then the bound values.
	Arguments []ast.Expr

	// Spread reports that the last argument is a slice spread into the call.
	Spread bool
}

// Guard says how a method body leaves on an error: the statements that return or yield
// the error held by err.
type Guard func() []ast.Stmt

// Arguments are the values a rendering of a query binds, each parameter read through
// Access: from the method's arguments, or from wherever a builder holds them.
type Arguments struct {
	// Query is the query.
	Query *gogen.Query

	// Rendered is the rendering: the query's own, or its count's.
	Rendered *emitkit.Rendered

	// Access reads the parameter at an index into AnalysedQuery.Parameters.
	Access func(index int) ast.Expr
}

// Expands reports whether the rendering splices a slice into its SQL at run time.
//
// Returns bool which is true when an argument expands.
func (arguments *Arguments) Expands() bool {
	for index := range arguments.Rendered.Arguments {
		if arguments.Rendered.Arguments[index].Expand {
			return true
		}
	}
	return false
}

// Count returns the expression counting the values the rendering binds in a range of its
// arguments: one per argument that does not expand, and the length of each slice that
// does, plus a number more.
//
// Takes extra (int) which is added to the count.
//
// Returns ast.Expr which counts the values.
func (arguments *Arguments) Count(extra int) ast.Expr {
	return arguments.CountRange(0, len(arguments.Rendered.Arguments), extra)
}

// CountRange returns the expression counting the values a range of the rendering's
// arguments binds, plus a number more.
//
// Takes from (int) which is the first index into Rendered.Arguments.
// Takes to (int) which is one past the last.
// Takes extra (int) which is added to the count.
//
// Returns ast.Expr which counts the values.
func (arguments *Arguments) CountRange(from, to, extra int) ast.Expr {
	fixed := extra
	var lengths []ast.Expr
	for index := from; index < to; index++ {
		argument := &arguments.Rendered.Arguments[index]
		if !argument.Expand {
			fixed++
			continue
		}
		lengths = append(lengths, sliceLength(arguments.Access(argument.Parameter)))
	}
	return sumExpression(fixed, lengths)
}

// Append returns the statements that append a range of the rendering's arguments to args:
// one append per value, through the driver's binder, and a loop over each slice that
// expands.
//
// Takes emission (*gogen.Emission) which binds the values.
// Takes from (int) which is the first index into Rendered.Arguments.
// Takes to (int) which is one past the last.
//
// Returns []ast.Stmt which holds the statements.
func (arguments *Arguments) Append(emission *gogen.Emission, from, to int) []ast.Stmt {
	statements := make([]ast.Stmt, 0, to-from)
	for index := from; index < to; index++ {
		argument := &arguments.Rendered.Arguments[index]
		if !argument.Expand {
			statements = append(statements, appendTo(goast.IdentArgs, arguments.bound(emission, index)))
			continue
		}
		element := emission.Driver.Enums.BoundElement(goastutil.CachedIdent(identElement),
			&arguments.Query.Analysed.Parameters[argument.Parameter], arguments.Query.Types, emission.Vocabulary)
		statements = append(statements, &ast.RangeStmt{
			Key:   goastutil.CachedIdent(goast.IdentBlank),
			Value: goastutil.CachedIdent(identElement),
			Tok:   token.DEFINE,
			X:     arguments.Access(argument.Parameter),
			Body:  goastutil.BlockStmt(appendTo(goast.IdentArgs, element)),
		})
	}
	return statements
}

// ListEdits returns, for each slice list of the rendering in the order rendering marked
// them, the call that writes it: sglList(sglListMark{...}, first, count).
//
// Takes emission (*gogen.Emission) which names the helpers and the dialect's empty tests.
//
// Returns []ast.Expr which holds one call per list.
func (arguments *Arguments) ListEdits(emission *gogen.Emission) []ast.Expr {
	negated := negatedLists(arguments.Query.Analysed.Template.Segments)
	lists := emission.Dialect().Lists
	marks := arguments.Rendered.Marks.Lists
	edits := make([]ast.Expr, 0, len(marks))
	for index := range marks {
		mark := &marks[index]
		empty := lists.EmptyMembership
		if negated[mark.Parameter] {
			empty = lists.EmptyExclusion
		}
		markLiteral := &ast.CompositeLit{
			Type: goastutil.CachedIdent(emission.Vocabulary.Helper(suffixListMark)),
			Elts: []ast.Expr{
				goastutil.KeyValueIdent("Empty", goast.StringLiteral(empty)),
				goastutil.KeyValueIdent("Operator", goastutil.IntLit(mark.Operator.Start)),
				goastutil.KeyValueIdent("Start", goastutil.IntLit(mark.Range.Start)),
				goastutil.KeyValueIdent("End", goastutil.IntLit(mark.Range.End)),
			},
		}
		first := arguments.firstNumber(mark.First)
		count := sliceLength(arguments.Access(mark.Parameter))
		edits = append(edits, goastutil.CallExpr(goastutil.CachedIdent(emission.Vocabulary.Helper(suffixList)), markLiteral, first, count))
	}
	return edits
}

// bound builds an argument binding for the query's selected placeholder form.
//
// Named execution uses the driver's binding; a positional runtime builder uses the
// driver's positional binding, which binds most values as they are. Required enum
// conversions happen first.
//
// Takes emission (*gogen.Emission) which binds the value.
// Takes index (int) which is the index into Rendered.Arguments.
//
// Returns ast.Expr which binds the argument.
func (arguments *Arguments) bound(emission *gogen.Emission, index int) ast.Expr {
	parameter := &arguments.Query.Analysed.Parameters[arguments.Rendered.Arguments[index].Parameter]
	access := emission.Driver.Enums.Bound(arguments.Access(arguments.Rendered.Arguments[index].Parameter),
		arguments.Query.Analysed, parameter, arguments.Query.Types, emission.Vocabulary)
	if !emission.InExecutionForm(arguments.Query) {
		return emission.BindPositional(access, parameter)
	}
	return emission.Bind(access, parameter, arguments.Query.Imports)
}

// firstNumber returns the expression of the first number a list's placeholders take: its
// canonical number moved on by the elements of each slice numbered before it, less the
// one placeholder each of them held. A slice written in two lists counts once.
//
// Takes canonical (int) which is the list's canonical number.
//
// Returns ast.Expr which is the number.
func (arguments *Arguments) firstNumber(canonical int) ast.Expr {
	marks := arguments.Rendered.Marks.Lists
	counted := make(map[int]bool)
	var earlier []ast.Expr
	for index := range marks {
		if marks[index].First < canonical && !counted[marks[index].Parameter] {
			counted[marks[index].Parameter] = true
			earlier = append(earlier, sliceLength(arguments.Access(marks[index].Parameter)))
		}
	}
	return sumExpression(canonical-len(earlier), earlier)
}

// ReturnGuard returns the guard of a method returning values ahead of its error.
//
// Takes zeroValues (...ast.Expr) which are returned ahead of the error.
//
// Returns Guard which returns them and err.
func ReturnGuard(zeroValues ...ast.Expr) Guard {
	return func() []ast.Stmt {
		results := make([]ast.Expr, 0, len(zeroValues)+1)
		results = append(results, zeroValues...)
		return []ast.Stmt{goastutil.ReturnStmt(append(results, goastutil.CachedIdent(goast.IdentErr))...)}
	}
}

// YieldGuard returns the guard of a stream iterator, which yields the zero row with the
// error and stops.
//
// Takes rowType (string) which is the row struct the iterator yields.
//
// Returns Guard which yields the error.
func YieldGuard(rowType string) Guard {
	return func() []ast.Stmt {
		return []ast.Stmt{
			&ast.ExprStmt{X: goastutil.CallExpr(
				goastutil.CachedIdent(identYield),
				goastutil.CompositeLit(goastutil.CachedIdent(rowType)),
				goastutil.CachedIdent(goast.IdentErr),
			)},
			goastutil.ReturnStmt(),
		}
	}
}

// MethodArguments returns the values a query's method binds, read from its arguments.
//
// Takes query (*gogen.Query) which is the query.
//
// Returns *Arguments which reads them.
func MethodArguments(query *gogen.Query) *Arguments {
	return &Arguments{
		Query:    query,
		Rendered: &query.Rendered,
		Access:   func(index int) ast.Expr { return ParameterAccess(query, index) },
	}
}

// PrepareCall returns how a method runs its query from SQL it holds in source.
//
// The values are bound in the order Rendered.Arguments lists them, each through the
// driver's binder. A query whose slices are expanded at run time first checks the number
// of values against the bind limit, leaving through guard when it is exceeded, then
// writes each slice's list into the SQL at the offset rendering marked, numbered from the
// slice's canonical number onwards, and gathers every value into args, flattening each
// slice where its list binds. A driver binding a slice as an array gets it with an empty
// slice for nil when the query excludes it, so NOT IN an empty slice holds for every row
// whether the slice is nil or empty.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
// Takes source (ast.Expr) which reads the SQL to run: the SQL constant, or the query
// local a sortable ORDER BY was written into.
// Takes guard (Guard) which leaves the method on an error.
//
// Returns Call which runs the query.
func PrepareCall(emission *gogen.Emission, query *gogen.Query, source ast.Expr, guard Guard) Call {
	statements := normaliseExcludedArrays(query)
	arguments := MethodArguments(query)
	if !arguments.Expands() {
		call := make([]ast.Expr, 0, 2+len(query.Rendered.Arguments))
		call = append(call, goastutil.CachedIdent(goast.IdentCtx), source)
		for index := range query.Rendered.Arguments {
			call = append(call, arguments.bound(emission, index))
		}
		return Call{Statements: statements, Arguments: call, Spread: false}
	}
	statements = append(statements,
		goastutil.IfStmt(
			goastutil.DefineStmt(goast.IdentErr, goastutil.CallExpr(
				goastutil.CachedIdent(emission.Vocabulary.Helper(suffixCheckBinds)), arguments.Count(0))),
			errorIsNotNil(),
			goastutil.BlockStmt(guard()...),
		),
		spliceLists(emission, arguments, source),
		goastutil.DefineStmt(goast.IdentArgs, goastutil.CallExpr(
			goastutil.CachedIdent("make"),
			&ast.ArrayType{Elt: goastutil.CachedIdent(goast.IdentAny)},
			goastutil.IntLit(0),
			arguments.Count(0),
		)),
	)
	statements = append(statements, arguments.Append(emission, 0, len(query.Rendered.Arguments))...)
	return Call{
		Statements: statements,
		Arguments: []ast.Expr{
			goastutil.CachedIdent(goast.IdentCtx),
			goastutil.CachedIdent(goast.IdentQuery),
			goastutil.CachedIdent(goast.IdentArgs),
		},
		Spread: true,
	}
}

// ConnectionCall returns the call of a connection method that runs a query.
//
// Takes emission (*gogen.Emission) which is the package's generation.
// Takes query (*gogen.Query) which is the query.
// Takes method (string) which is the connection method.
// Takes call (*Call) which holds the call's arguments.
//
// Returns *ast.CallExpr which is the call.
func ConnectionCall(emission *gogen.Emission, query *gogen.Query, method string, call *Call) *ast.CallExpr {
	expression := DBCall(emission.ConnectionField(query.Analysed), method, call.Arguments)
	if call.Spread {
		expression.Ellipsis = 1
	}
	return expression
}

// sumExpression returns a number plus some lengths, leaving out a zero number when there
// are lengths.
//
// Takes fixed (int) which is the number.
// Takes lengths ([]ast.Expr) which are the lengths.
//
// Returns ast.Expr which is the sum.
func sumExpression(fixed int, lengths []ast.Expr) ast.Expr {
	var sum ast.Expr
	if fixed > 0 || len(lengths) == 0 {
		sum = goastutil.IntLit(fixed)
	}
	for _, length := range lengths {
		if sum == nil {
			sum = length
			continue
		}
		sum = &ast.BinaryExpr{X: sum, Op: token.ADD, Y: length}
	}
	return sum
}

// sliceLength returns the call len(access).
//
// Takes access (ast.Expr) which reads the slice.
//
// Returns ast.Expr which is the call.
func sliceLength(access ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent("len"), access)
}

// spliceLists returns the statement that writes every slice's list into the SQL, as the
// query local: sglSplice(source, sglList(mark, first, count), ...).
//
// Takes emission (*gogen.Emission) which names the helpers.
// Takes arguments (*Arguments) which holds the query and its rendering.
// Takes source (ast.Expr) which reads the SQL the lists are written into.
//
// Returns ast.Stmt which defines or assigns the query local.
func spliceLists(emission *gogen.Emission, arguments *Arguments, source ast.Expr) ast.Stmt {
	call := append([]ast.Expr{source}, arguments.ListEdits(emission)...)
	spliced := goastutil.CallExpr(goastutil.CachedIdent(emission.Vocabulary.Helper(suffixSplice)), call...)
	if identifier, ok := source.(*ast.Ident); ok && identifier.Name == goast.IdentQuery {
		return goastutil.AssignStmt(goastutil.CachedIdent(goast.IdentQuery), spliced)
	}
	return goastutil.DefineStmt(goast.IdentQuery, spliced)
}

// negatedLists returns the parameters whose list a template writes with NOT IN.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the template's segments.
//
// Returns map[int]bool which holds the parameters.
func negatedLists(segments []sqlmodel.SQLSegment) map[int]bool {
	negated := make(map[int]bool)
	var visit func(segments []sqlmodel.SQLSegment)
	visit = func(segments []sqlmodel.SQLSegment) {
		for index := range segments {
			if segments[index].Kind == sqlmodel.SegmentList && segments[index].Negated {
				negated[segments[index].Parameter] = true
			}
			visit(segments[index].Children)
		}
	}
	visit(segments)
	return negated
}

// appendTo returns the statement list = append(list, value).
//
// Takes list (string) which names the slice.
// Takes value (ast.Expr) which is appended.
//
// Returns ast.Stmt which is the statement.
func appendTo(list string, value ast.Expr) ast.Stmt {
	return goastutil.AssignStmt(
		goastutil.CachedIdent(list),
		goastutil.CallExpr(goastutil.CachedIdent("append"), goastutil.CachedIdent(list), value),
	)
}

// normaliseExcludedArrays returns the statements that give a slice bound as an array for
// NOT IN an empty slice in place of nil, since an array parameter bound to nil is NULL,
// under which every exclusion test fails, while an empty one holds for every row.
//
// Takes query (*gogen.Query) which is the query.
//
// Returns []ast.Stmt which holds the statements, or nil when no list is an excluded
// array.
func normaliseExcludedArrays(query *gogen.Query) []ast.Stmt {
	if query.Renderer.ListMode != emitkit.ListArray {
		return nil
	}
	negated := negatedLists(query.Analysed.Template.Segments)
	var statements []ast.Stmt
	for _, mark := range query.Rendered.Marks.Lists {
		if !negated[mark.Parameter] {
			continue
		}
		parameter := &query.Analysed.Parameters[mark.Parameter]
		access := ParameterAccess(query, mark.Parameter)
		statements = append(statements, goastutil.IfStmt(nil,
			&ast.BinaryExpr{X: access, Op: token.EQL, Y: goastutil.CachedIdent(goast.IdentNil)},
			goastutil.BlockStmt(goastutil.AssignStmt(access, goastutil.CompositeLit(ParameterType(query, parameter)))),
		))
	}
	return statements
}
