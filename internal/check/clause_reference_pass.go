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

package check

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlmodel"
)

// clauseReferencePass reports unknown columns and functions in query clauses, including
// nested queries, UPDATE assignments and the arguments of FROM-clause function calls.
// INSERT VALUES expressions are excluded.
//
// References use the select list's scope and the dialect's rules for aliases and implicit
// columns. Unknown columns produce Q001 and unknown functions Q005, with name
// suggestions. Lambda parameters, struct fields and columns of relations with unknown
// schemas are excluded.
//
// The pass is not safe for concurrent use: its builder's type resolver keeps state while
// it types a call.
type clauseReferencePass struct {
	// builder builds the scopes of nested queries and types function calls, made once for
	// the dialect; nil when there is no dialect, and the pass then reports nothing.
	builder *resolve.ScopeBuilder

	// dialect holds what the pass reads of the dialect: its system columns and the clauses a
	// select-list alias is visible in.
	dialect *dialectFacts
}

// newClauseReferencePass returns the pass for a dialect and catalogue, with the scope
// builder of its nested queries made once.
//
// Takes engine (dialect.DialectPort) which is the dialect, or nil.
// Takes facts (*dialectFacts) which is what the passes read of the dialect, or nil.
// Takes catalogue (*sqlmodel.Catalogue) which holds the relations and functions names
// resolve against.
//
// Returns *clauseReferencePass which is the pass.
func newClauseReferencePass(engine dialect.DialectPort, facts *dialectFacts, catalogue *sqlmodel.Catalogue) *clauseReferencePass {
	pass := &clauseReferencePass{builder: nil, dialect: facts}
	if engine != nil && facts != nil {
		pass.builder = resolve.NewCatalogueScopeBuilder(engine, catalogue)
	}
	return pass
}

// Analyse checks every clause expression of the query and of each query nested in it.
//
// Takes subject (*DiagnosticContext) which carries the query, its raw analysis and its
// scope.
//
// Returns []diag.SourceError which holds a Q001 warning per unknown column and a Q005
// warning per unknown function, or nil when the context lacks what the pass needs.
func (p *clauseReferencePass) Analyse(ctx context.Context, subject *DiagnosticContext) []diag.SourceError {
	if subject == nil || subject.RawAnalysis == nil || subject.Scope == nil || subject.Query == nil || p.builder == nil {
		return nil
	}
	checker := newReferenceChecker(p.builder, p.dialect, subject)
	checker.checkQuery(ctx, subject.RawAnalysis, subject.Scope)
	return checker.problems.finish()
}

// referenceKey identifies one reported problem, so a name reached twice, as a subquery
// reachable both from a clause expression and from the predicate subqueries is, is
// reported once.
type referenceKey struct {
	// code is the diagnostic code.
	code string

	// message is the diagnostic message.
	message string

	// start is the offset of the name.
	start int
}

// referenceChecker walks the clause expressions of one query and the queries nested in
// it.
type referenceChecker struct {
	// builder builds nested scopes and types function calls.
	builder *resolve.ScopeBuilder

	// problems places the problems found in the query.
	problems *queryReporter

	// reported indexes the diagnostics the query had before the pass ran, such as the
	// parameter resolver's report of a column a placeholder is compared with.
	reported reportIndex

	// seen holds the problems already reported.
	seen map[referenceKey]struct{}

	// visited holds the nested queries already checked, so a query reachable both from a
	// clause expression and from the predicate subqueries is built and checked once.
	visited map[*ast.RawQueryAnalysis]struct{}

	// systemColumns lists the columns every table has without declaring them.
	systemColumns []string

	// selectAliases is the set of clauses a select-list alias is visible in.
	selectAliases ast.Clause
}

// newReferenceChecker returns the checker of one query's references.
//
// Takes builder (*resolve.ScopeBuilder) which builds nested scopes and types calls.
// Takes facts (*dialectFacts) which gives the dialect's system columns and alias rules.
// Takes subject (*DiagnosticContext) which carries the query and its diagnostics so far;
// its Query is not nil.
//
// Returns *referenceChecker which has reported nothing yet.
func newReferenceChecker(builder *resolve.ScopeBuilder, facts *dialectFacts, subject *DiagnosticContext) *referenceChecker {
	return &referenceChecker{
		builder:       builder,
		problems:      newQueryReporter(subject.Filename, subject.Query.SQL, subject.Query.Line),
		reported:      newReportIndex(subject.ExistingDiagnostics),
		seen:          make(map[referenceKey]struct{}),
		visited:       make(map[*ast.RawQueryAnalysis]struct{}),
		systemColumns: facts.info.SystemColumns,
		selectAliases: facts.info.SelectAliases,
	}
}

// checkQuery checks the clause expressions of a query and then of every query nested in
// it.
//
// Takes raw (*ast.RawQueryAnalysis) which is the query.
// Takes scope (*resolve.ScopeChain) which resolves the query's names; its depth is the
// query's nesting depth.
func (c *referenceChecker) checkQuery(ctx context.Context, raw *ast.RawQueryAnalysis, scope *resolve.ScopeChain) {
	if raw == nil {
		return
	}
	aliases := outputAliases(raw.OutputColumns)
	unresolved := unresolvedRelations(raw, scope)
	for _, clauseExpression := range raw.ClauseExpressions {
		if clauseExpression.Clause.Includes(ast.ClauseInsertValue) {
			continue
		}
		c.checkExpression(ctx, clauseExpression.Expression, expressionContext{
			scope:            scope,
			aliases:          aliases,
			lambdaParameters: nil,
			unresolved:       unresolved,
			clause:           clauseExpression.Clause,
		})
	}
	c.checkTableFunctionArguments(ctx, raw.RawTableValuedFunctions, expressionContext{
		scope:            scope,
		aliases:          nil,
		lambdaParameters: nil,
		unresolved:       unresolved,
		clause:           0,
	})
	for _, nested := range nestedQueries(raw) {
		c.checkNestedQuery(ctx, nested, scope)
	}
	for _, column := range raw.OutputColumns {
		for _, inner := range resolve.Subqueries(column.Expression) {
			c.checkNestedQuery(ctx, inner, scope)
		}
	}
}

// checkTableFunctionArguments checks the arguments of a query's FROM-clause function
// calls in the query's whole scope, so an argument reading a relation registered later,
// as a LATERAL call may, still resolves; the select list's aliases are not visible there.
//
// Takes references ([]ast.RawTableValuedFunctionReference) which are the calls.
// Takes where (expressionContext) which says where the arguments sit.
func (c *referenceChecker) checkTableFunctionArguments(
	ctx context.Context,
	references []ast.RawTableValuedFunctionReference,
	where expressionContext,
) {
	for index := range references {
		for _, argument := range references[index].Arguments {
			c.checkExpression(ctx, argument, where)
		}
		for callIndex := range references[index].Functions {
			for _, argument := range references[index].Functions[callIndex].Arguments {
				c.checkExpression(ctx, argument, where)
			}
		}
	}
}

// checkNestedQuery builds the scope of a query nested in another and checks it.
//
// The nested scope is a child of the enclosing query's scope, allowing correlated
// references and access to its CTEs, including recursive CTE references. It shares the
// enclosing depth budget, and each query is checked once. Checking stops when the context
// is done or nesting reaches resolve.MaxExpressionResolveDepth.
//
// Missing catalogue relations and ARRAY JOIN sources that cannot be found or typed are
// reported unless the analyser has already reported them.
//
// Takes raw (*ast.RawQueryAnalysis) which is the nested query.
// Takes parent (*resolve.ScopeChain) which is the enclosing query's scope.
func (c *referenceChecker) checkNestedQuery(ctx context.Context, raw *ast.RawQueryAnalysis, parent *resolve.ScopeChain) {
	if raw == nil || parent.Depth() >= resolve.MaxExpressionResolveDepth || ctx.Err() != nil {
		return
	}
	if _, checked := c.visited[raw]; checked {
		return
	}
	c.visited[raw] = struct{}{}
	scope := parent.CreateChildScope(resolve.ScopeKindSubquery)
	maps.Copy(scope.CTEs, parent.CTEs)
	for _, problem := range c.builder.BuildQueryScope(ctx, raw, scope) {
		if problem != nil && problem.Code == diag.CodeUnknownTable {
			c.reportResolutionProblem(problem)
		}
	}
	for _, problem := range c.builder.ResolveArrayJoinClauses(ctx, raw.ArrayJoinClauses, scope) {
		c.reportResolutionProblem(problem)
	}
	c.checkQuery(ctx, raw, scope)
	c.checkOutputColumns(ctx, raw, scope)
}

// reportResolutionProblem reports a problem building a nested query's scope found, unless
// the analyser reported it already, as it does for a CTE body or a derived table.
//
// Takes problem (*resolve.Error) which is the problem; nil reports nothing.
func (c *referenceChecker) reportResolutionProblem(problem *resolve.Error) {
	if problem == nil || c.reported.has(problem.Code, problem.Message) {
		return
	}
	c.report(ast.Span{Start: problem.Span.Start, End: problem.Span.End}, problem.Code, problem.Severity, problem.Message,
		problem.Suggestion)
}

// checkOutputColumns checks the columns a nested query's select list names, which the
// analyser types only for the query's own select list; a star names no column.
//
// Takes raw (*ast.RawQueryAnalysis) which is the nested query.
// Takes scope (*resolve.ScopeChain) which resolves its names.
func (c *referenceChecker) checkOutputColumns(ctx context.Context, raw *ast.RawQueryAnalysis, scope *resolve.ScopeChain) {
	where := expressionContext{
		scope:            scope,
		aliases:          nil,
		lambdaParameters: nil,
		unresolved:       unresolvedRelations(raw, scope),
		clause:           ast.ClauseSelectList,
	}
	for _, column := range raw.OutputColumns {
		switch {
		case column.IsStar:
			continue
		case column.Expression != nil:
			c.checkExpression(ctx, column.Expression, where)
		case column.ColumnName != "":
			c.checkColumn(&ast.ColumnRefExpression{TableAlias: column.TableAlias, ColumnName: column.ColumnName, Span: ast.Span{}}, where)
		}
	}
}

// checkExpression checks every column and function an expression names, and every query
// nested in it.
//
// The expression is walked with an explicit stack over ast.Children, so a tree of any
// depth is checked without exhausting the goroutine's stack. A lambda's body is checked
// with the lambda's parameters visible; its bindings stand for values written elsewhere
// in the call and are checked there.
//
// Takes expression (ast.Expression) which is the expression.
// Takes where (expressionContext) which says where it sits.
func (c *referenceChecker) checkExpression(ctx context.Context, expression ast.Expression, where expressionContext) {
	stack := []pendingExpression{{expression: expression, lambdaParameters: where.lambdaParameters}}
	for len(stack) > 0 {
		last := len(stack) - 1
		pending := stack[last]
		stack[last] = pendingExpression{expression: nil, lambdaParameters: nil}
		stack = stack[:last]
		if pending.expression == nil {
			continue
		}
		here := where
		here.lambdaParameters = pending.lambdaParameters
		if inner, holds := ast.InnerQuery(pending.expression); holds {
			c.checkNestedQuery(ctx, inner, where.scope)
			continue
		}
		switch typed := pending.expression.(type) {
		case *ast.ColumnRefExpression:
			c.checkColumn(typed, here)
			continue
		case *ast.LambdaExpression:
			parameters := append(slices.Clone(pending.lambdaParameters), typed.Parameters...)
			stack = append(stack, pendingExpression{expression: typed.Body, lambdaParameters: parameters})
			continue
		case *ast.FunctionCallExpression:
			c.checkFunction(ctx, typed, here)
		}
		mark := len(stack)
		for child := range ast.Children(pending.expression) {
			stack = append(stack, pendingExpression{expression: child, lambdaParameters: pending.lambdaParameters})
		}
		slices.Reverse(stack[mark:])
	}
}

// checkFunction reports a function the catalogue and the engine do not know;
// checkExpression checks its arguments.
//
// Takes call (*ast.FunctionCallExpression) which is the call.
// Takes where (expressionContext) which says where it sits.
func (c *referenceChecker) checkFunction(ctx context.Context, call *ast.FunctionCallExpression, where expressionContext) {
	if call.FunctionName == "" {
		return
	}
	_, _, err := c.builder.TypeResolver.ResolveExpressionType(ctx, call, where.scope, nil)
	if resolveError, ok := errors.AsType[*resolve.Error](err); ok && resolveError.Code == diag.CodeUnknownFunction &&
		!c.reported.quotes(resolveError.Code, call.FunctionName) {
		c.report(call.Span, resolveError.Code, diag.SeverityWarning, resolveError.Message, resolveError.Suggestion)
	}
}

// checkColumn reports a column reference that resolves to nothing the query can see.
//
// Takes reference (*ast.ColumnRefExpression) which is the reference.
// Takes where (expressionContext) which says where it sits.
func (c *referenceChecker) checkColumn(reference *ast.ColumnRefExpression, where expressionContext) {
	if reference.ColumnName == "" {
		return
	}
	_, _, err := where.scope.ResolveColumn(reference.TableAlias, reference.ColumnName)
	if err == nil || errors.Is(err, resolve.ErrScopeChainDepthExceeded) {
		return
	}
	resolveError, ok := errors.AsType[*resolve.Error](err)
	if !ok || resolveError.Code != diag.CodeUnknownColumn || c.excused(reference, where) {
		return
	}
	if c.reported.quotes(resolveError.Code, reference.ColumnName) {
		return
	}
	message := resolveError.Message
	if reference.TableAlias == "" && c.matches(where.scope, where.aliases, reference.ColumnName) {
		message = fmt.Sprintf("%s; the select list's alias %q is not visible in %s", message, reference.ColumnName, where.clause)
	}
	c.report(reference.Span, resolveError.Code, diag.SeverityWarning, message, resolveError.Suggestion)
}

// excused reports valid references that need no column in the query's relations.
//
// Takes reference (*ast.ColumnRefExpression) which is the reference.
// Takes where (expressionContext) which says where it sits.
//
// Returns bool which is true when the reference must not be reported.
func (c *referenceChecker) excused(reference *ast.ColumnRefExpression, where expressionContext) bool {
	if c.matches(where.scope, c.systemColumns, reference.ColumnName) {
		return true
	}
	if reference.TableAlias != "" {
		return c.excusedQualified(reference, where)
	}
	if len(where.unresolved) > 0 || c.matches(where.scope, where.lambdaParameters, reference.ColumnName) {
		return true
	}
	if _, _, isRow := where.scope.ResolveRowValue(reference.ColumnName); isRow {
		return true
	}
	if c.selectAliases.Includes(where.clause) && c.matches(where.scope, where.aliases, reference.ColumnName) {
		return true
	}
	for _, table := range where.scope.OrderedTables() {
		if len(table.Columns) == 0 {
			return true
		}
	}
	return false
}

// excusedQualified reports whether a qualified column reference that resolves to no
// column of the query's relations is still valid: a field of a lambda parameter or of a
// struct or composite column, or a column of a relation whose columns the analyser could
// not learn.
//
// Takes reference (*ast.ColumnRefExpression) which is the qualified reference.
// Takes where (expressionContext) which says where it sits.
//
// Returns bool which is true when the reference must not be reported.
func (c *referenceChecker) excusedQualified(reference *ast.ColumnRefExpression, where expressionContext) bool {
	if c.matches(where.scope, where.lambdaParameters, reference.TableAlias) || c.matches(where.scope, where.unresolved, reference.TableAlias) {
		return true
	}
	if _, _, err := where.scope.ResolveColumn("", reference.TableAlias); err == nil {
		return true
	}
	table, exists := where.scope.Table(reference.TableAlias)
	return exists && len(table.Columns) == 0
}

// matches reports whether a name is one of a list of names, compared under the dialect's
// identifier rules.
//
// Takes scope (*resolve.ScopeChain) which supplies the identifier rules.
// Takes names ([]string) which are the candidates.
// Takes name (string) which is the name.
//
// Returns bool which is true when the name is one of them.
func (*referenceChecker) matches(scope *resolve.ScopeChain, names []string, name string) bool {
	identifiers := scope.Rules().Identifiers
	for _, candidate := range names {
		if identifiers.Equal(identifiers.Fold(candidate, false), name) || identifiers.Equal(candidate, name) {
			return true
		}
	}
	return false
}

// report records a problem once, at the name's position when the dialect recorded it and
// on the query otherwise.
//
// Takes span (ast.Span) which locates the name in the query block's text.
// Takes code (string) which is the diagnostic code.
// Takes severity (diag.ErrorSeverity) which is how serious the problem is.
// Takes message (string) which describes the problem.
// Takes suggestion (string) which is the name the query most likely meant, or empty.
func (c *referenceChecker) report(span ast.Span, code string, severity diag.ErrorSeverity, message, suggestion string) {
	key := referenceKey{code: code, message: message, start: span.Start}
	if _, exists := c.seen[key]; exists {
		return
	}
	c.seen[key] = struct{}{}
	c.problems.add(span, code, severity, message, suggestion)
}

// expressionContext is where in a query an expression is checked.
type expressionContext struct {
	// scope resolves the query's names.
	scope *resolve.ScopeChain

	// aliases lists the aliases the query's select list gives its columns.
	aliases []string

	// lambdaParameters lists the parameters of the lambdas the expression sits in.
	lambdaParameters []string

	// unresolved lists the names of the query's relations the analyser could not find, such
	// as an unknown table it has reported already.
	unresolved []string

	// clause is the clause the expression sits in.
	clause ast.Clause
}

// pendingExpression is an expression waiting to be checked, with the parameters of the
// lambdas it sits in.
type pendingExpression struct {
	// expression is the expression.
	expression ast.Expression

	// lambdaParameters lists the parameters of the lambdas it sits in.
	lambdaParameters []string
}

// unresolvedRelations lists the names a query's relations are visible under that its
// scope does not hold, because the analyser could not find them: an unknown table, view,
// table-valued function or derived table, which the analyser reports itself.
//
// A name the query uses through such a relation cannot be judged, so it is not reported
// again.
//
// Takes raw (*ast.RawQueryAnalysis) which is the query.
// Takes scope (*resolve.ScopeChain) which holds the relations the analyser found.
//
// Returns []string which holds the names, or nil when every relation resolved.
func unresolvedRelations(raw *ast.RawQueryAnalysis, scope *resolve.ScopeChain) []string {
	names := make([]string, 0, len(raw.FromTables)+len(raw.JoinClauses)+len(raw.RawTableValuedFunctions)+len(raw.RawDerivedTables))
	for _, reference := range raw.FromTables {
		names = append(names, visibleName(reference))
	}
	for _, join := range raw.JoinClauses {
		names = append(names, visibleName(join.Table))
	}
	for index := range raw.RawTableValuedFunctions {
		names = append(names, raw.RawTableValuedFunctions[index].Alias)
	}
	for _, derived := range raw.RawDerivedTables {
		names = append(names, derived.Alias)
	}
	var unresolved []string
	for _, name := range names {
		if _, found := scope.Table(name); name != "" && !found {
			unresolved = append(unresolved, name)
		}
	}
	return unresolved
}

// visibleName returns the name a FROM or JOIN reference is visible under: its alias, or
// its name when it has none.
//
// Takes reference (ast.TableReference) which names the relation and its alias.
//
// Returns string which is the visible name.
func visibleName(reference ast.TableReference) string {
	if reference.Alias != "" {
		return reference.Alias
	}
	return reference.Name
}

// outputAliases lists the names a query's select list gives its columns.
//
// Takes columns ([]ast.RawOutputColumn) which are the select list's columns.
//
// Returns []string which holds the names.
func outputAliases(columns []ast.RawOutputColumn) []string {
	aliases := make([]string, 0, len(columns))
	for _, column := range columns {
		if column.Name != "" {
			aliases = append(aliases, column.Name)
		}
	}
	return aliases
}

// nestedQueries lists the queries nested directly in a query: its predicate subqueries,
// derived tables, CTE bodies, compound branches and INSERT ... SELECT body.
//
// Takes raw (*ast.RawQueryAnalysis) which is the query.
//
// Returns []*ast.RawQueryAnalysis which holds the nested queries.
func nestedQueries(raw *ast.RawQueryAnalysis) []*ast.RawQueryAnalysis {
	nested := append([]*ast.RawQueryAnalysis(nil), raw.PredicateSubqueries...)
	for index := range raw.RawDerivedTables {
		nested = append(nested, raw.RawDerivedTables[index].InnerQuery)
	}
	for index := range raw.CTEDefinitions {
		nested = append(nested, raw.CTEDefinitions[index].Query)
	}
	for index := range raw.CompoundBranches {
		nested = append(nested, raw.CompoundBranches[index].Query)
	}
	return append(nested, raw.InsertSelect)
}
