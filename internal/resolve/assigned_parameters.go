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

package resolve

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// valueArguments maps each function of standard SQL whose value is one of its arguments,
	// or NULL, to that argument's position: NULLIF(a, b) gives a unless it equals b, so a
	// parameter there is a whole value the column takes.
	valueArguments = map[string]int{"nullif": 0}

	// siblingExcludedCategories holds the kinds of value an argument cannot give a parameter
	// beside it in a call whose arguments share one type: a container beside an argument of
	// no declared type is usually what the argument is an element of, as in
	// array_append(tags, $1), and an unknown value says nothing.
	siblingExcludedCategories = map[sqlmodel.SQLTypeCategory]bool{
		sqlmodel.TypeCategoryUnknown:        true,
		sqlmodel.TypeCategoryArray:          true,
		sqlmodel.TypeCategoryMap:            true,
		sqlmodel.TypeCategoryStruct:         true,
		sqlmodel.TypeCategoryComposite:      true,
		sqlmodel.TypeCategoryUnion:          true,
		sqlmodel.TypeCategoryAggregateState: true,
	}

	// partnerCategories holds the kinds of value an operand can give the parameter beside it
	// in a binary operation: numbers combine with numbers and text with text, while a
	// timestamp or a JSON document is combined with values of other kinds.
	partnerCategories = map[sqlmodel.SQLTypeCategory]bool{
		sqlmodel.TypeCategoryInteger: true,
		sqlmodel.TypeCategoryFloat:   true,
		sqlmodel.TypeCategoryDecimal: true,
		sqlmodel.TypeCategoryText:    true,
	}
)

// assignedParameters is what a statement's assigned values say of the parameters it
// assigns to a column only as part of a larger expression.
type assignedParameters struct {
	// embedded holds the numbers of those parameters.
	embedded map[int]bool

	// partial holds the numbers of the parameters some assigned value holds as part of a
	// larger expression, whether or not another value is the parameter alone.
	partial map[int]bool

	// calls maps the number of each of them that is an argument of a call, alone or under a
	// cast, to that call.
	calls map[int]enclosingCall

	// operands maps the number of each of them that is an operand of a binary operation to
	// the operation's other operand.
	operands map[int]operandPartner
}

// enclosingCall is a call a parameter is an argument of.
type enclosingCall struct {
	// name is the function's name, lower-cased, qualified by its schema when the call names
	// one.
	name string

	// arguments holds the call's arguments, or is nil when only the dialect's record of the
	// call is known.
	arguments []ast.Expression

	// ordinal is the zero-based position of the argument.
	ordinal int
}

// operandPartner is the other operand of a binary operation a parameter is an operand of.
type operandPartner struct {
	// other is the other operand.
	other ast.Expression

	// operator is the operation's operator.
	operator string
}

// assignedValueScan gathers what the values an INSERT or UPDATE assigns say of their
// parameters, one value at a time.
type assignedValueScan struct {
	// whole holds the numbers of the parameters some value is in itself.
	whole map[int]bool

	// embedded holds, for each parameter a value holds, whether some value holds it as part
	// of a larger expression.
	embedded map[int]bool

	// calls maps parameters to the calls they are arguments of.
	calls map[int]enclosingCall

	// operands maps parameters to the other operands of the operations they are operands of.
	operands map[int]operandPartner

	// numbers maps each named parameter to its number.
	numbers map[string]int

	// items holds the spans of the VALUES rows' values, in order.
	items []ast.Span

	// placeholders holds the statement's placeholders, ordered by where they start.
	placeholders []ast.PlaceholderSpan

	// inserted counts the VALUES values scanned so far.
	inserted int

	// itemsMatch reports that items pairs one to one with the VALUES values the statement
	// records.
	itemsMatch bool
}

// add scans one clause expression when it is a value an INSERT or UPDATE assigns.
//
// Takes clauseExpression (*ast.ClauseExpression) which is the expression.
func (scan *assignedValueScan) add(clauseExpression *ast.ClauseExpression) {
	inserted := clauseExpression.Clause.Includes(ast.ClauseInsertValue)
	if !inserted && !clauseExpression.Clause.Includes(ast.ClauseSetValue) {
		return
	}
	value := clauseExpression.Expression
	alone := wholeValueParameters(value, 0)
	for _, number := range alone {
		scan.whole[number] = true
	}
	held := parametersIn(value)
	for _, number := range held {
		scan.embedded[number] = scan.embedded[number] || !slices.Contains(alone, number)
	}
	scan.collectRoles(value, 0)
	if !inserted {
		return
	}
	if scan.itemsMatch {
		markHiddenPlaceholders(scan.items[scan.inserted], scan.placeholders, scan.numbers, slices.Concat(alone, held), scan.embedded)
	}
	scan.inserted++
}

// result returns the parameters the scan found assigned only as part of a larger
// expression, with their calls and operations, and those some value holds as part of a
// larger expression at all.
//
// Returns assignedParameters which holds them, or none.
func (scan *assignedValueScan) result() assignedParameters {
	partial := make(map[int]bool)
	for number, inside := range scan.embedded {
		if inside {
			partial[number] = true
		}
		if !inside || scan.whole[number] {
			delete(scan.embedded, number)
		}
	}
	if len(scan.embedded) == 0 {
		return assignedParameters{embedded: nil, partial: partial, calls: nil, operands: nil}
	}
	return assignedParameters{embedded: scan.embedded, partial: partial, calls: scan.calls, operands: scan.operands}
}

// collectRoles records each parameter's enclosing call and argument position, or its
// other operand in a binary operation when that operand is not a parameter. Cast
// parameters are excluded, and the first record wins.
//
// Takes expression (ast.Expression) which is the expression.
// Takes depth (int) which bounds the search.
func (scan *assignedValueScan) collectRoles(expression ast.Expression, depth int) {
	if expression == nil || depth >= MaxExpressionResolveDepth {
		return
	}
	children := operands(expression)
	switch typed := expression.(type) {
	case *ast.FunctionCallExpression:
		if typed == nil {
			return
		}
		scan.noteCallArguments(typed)
		children = slices.Values(typed.Arguments)
	case *ast.BinaryOpExpression:
		if typed != nil {
			scan.noteOperand(typed.Left, typed.Right, typed.Operator)
			scan.noteOperand(typed.Right, typed.Left, typed.Operator)
		}
	}
	for child := range children {
		scan.collectRoles(child, depth+1)
	}
}

// noteCallArguments records the call each parameter argument of a call is passed to.
//
// Takes call (*ast.FunctionCallExpression) which is the call.
func (scan *assignedValueScan) noteCallArguments(call *ast.FunctionCallExpression) {
	for ordinal, argument := range call.Arguments {
		parameter, isParameter := argument.(*ast.ParameterExpression)
		if !isParameter || parameter == nil {
			continue
		}
		if _, recorded := scan.calls[parameter.Number]; !recorded {
			scan.calls[parameter.Number] = enclosingCall{name: qualifiedFunctionName(call), arguments: call.Arguments, ordinal: ordinal}
		}
	}
}

// noteOperand records the other operand of a binary operation an operand that is a
// parameter sits beside, unless the other is a parameter too.
//
// Takes operand (ast.Expression) which is the operand.
// Takes other (ast.Expression) which is the other operand.
// Takes operator (string) which is the operation's operator.
func (scan *assignedValueScan) noteOperand(operand, other ast.Expression, operator string) {
	parameter, isParameter := operand.(*ast.ParameterExpression)
	if !isParameter || parameter == nil || other == nil {
		return
	}
	if _, otherIsParameter := other.(*ast.ParameterExpression); otherIsParameter {
		return
	}
	if _, recorded := scan.operands[parameter.Number]; !recorded {
		scan.operands[parameter.Number] = operandPartner{other: other, operator: operator}
	}
}

// embeddedAssignment reports whether a parameter occurs inside an assigned expression,
// requiring its own type while retaining the target column's name. An explicit cast
// determines the parameter's type.
//
// Takes raw (ast.RawParameterReference) which is the reference.
//
// Returns bool which is true for such a reference.
func (r *TypeResolver) embeddedAssignment(raw ast.RawParameterReference) bool {
	return raw.Context == sqlmodel.ParameterContextAssignment && raw.ColumnReference != nil && raw.CastType == nil &&
		r.assigned.embedded[raw.Number]
}

// resolveEmbeddedParameterType types a parameter a statement assigns only as part of a
// larger expression from that expression: the call it is an argument of, else the other
// operand of the operation it is an operand of, else the call the dialect recorded, else
// its context.
//
// Takes raw (ast.RawParameterReference) which is the parameter.
// Takes scope (*ScopeChain) which is the scope the other operand is read in.
//
// Returns sqlmodel.SQLType which is the type.
// Returns bool which is true when the type allows NULL.
// Returns error which is always nil.
func (r *TypeResolver) resolveEmbeddedParameterType(ctx context.Context, raw ast.RawParameterReference, scope *ScopeChain) (sqlmodel.SQLType, bool, error) {
	if call, found := r.assigned.calls[raw.Number]; found {
		return r.resolveCallArgumentParameterType(ctx, raw, call, scope)
	}
	if operandType, typed := r.operandPartnerType(ctx, raw.Number, scope); typed {
		return operandType, false, nil
	}
	if raw.EnclosingFunctionName != "" {
		recorded := enclosingCall{name: raw.EnclosingFunctionName, arguments: nil, ordinal: raw.ArgumentOrdinal}
		return r.resolveCallArgumentParameterType(ctx, raw, recorded, scope)
	}
	return r.resolveContextOnlyParameterType(raw.Context), false, nil
}

// resolveCallArgumentParameterType types a call's parameter from its signature and
// argument position. When the signature shares argument and result types, the parameter
// takes the common type of the other arguments.
//
// Takes raw (ast.RawParameterReference) which is the parameter.
// Takes call (enclosingCall) which is the call and the argument's position.
// Takes scope (*ScopeChain) which is the scope the other arguments are read in.
//
// Returns sqlmodel.SQLType which is the declared or shared type, or the context's type
// when the signature gives none.
// Returns bool which is true when the signature takes NULL there as a value.
// Returns error which is always nil.
func (r *TypeResolver) resolveCallArgumentParameterType(
	ctx context.Context,
	raw ast.RawParameterReference,
	call enclosingCall,
	scope *ScopeChain,
) (sqlmodel.SQLType, bool, error) {
	nullable := r.functionResolver.ArgumentAcceptsNull(call.name, call.ordinal)
	argumentType, resolved := r.functionResolver.ArgumentType(call.name, call.ordinal)
	if resolved && argumentType.Category != sqlmodel.TypeCategoryUnknown {
		return r.resolveCustomType(argumentType), nullable, nil
	}
	if sharedType, shared := r.siblingArgumentType(ctx, call, scope); shared {
		return sharedType, nullable, nil
	}
	if resolved {
		return r.resolveCustomType(argumentType), nullable, nil
	}
	return r.resolveContextOnlyParameterType(raw.Context), nullable, nil
}

// siblingArgumentType gives a parameter passed to a call whose arguments and result share
// one type the common type of the call's other arguments at positions of that type,
// leaving out the parameters among them.
//
// Takes call (enclosingCall) which is the call and the parameter's position.
// Takes scope (*ScopeChain) which is the scope the arguments are read in.
//
// Returns sqlmodel.SQLType which is the common type.
// Returns bool which is false when the call's arguments do not share a type, or none of
// the others gives one a parameter can take.
func (r *TypeResolver) siblingArgumentType(ctx context.Context, call enclosingCall, scope *ScopeChain) (sqlmodel.SQLType, bool) {
	common := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	if call.arguments == nil || !r.functionResolver.SharesArgumentType(call.name, call.ordinal) {
		return common, false
	}
	for ordinal, argument := range call.arguments {
		if ordinal == call.ordinal || !r.functionResolver.SharesArgumentType(call.name, ordinal) {
			continue
		}
		if _, isParameter := argument.(*ast.ParameterExpression); isParameter {
			continue
		}
		if argumentType, typed := r.quietExpressionType(ctx, argument, scope); typed {
			common = r.commonSupertype(common, argumentType)
		}
	}
	if siblingExcludedCategories[common.Category] {
		return common, false
	}
	return common, true
}

// quietExpressionType types an expression for what it says of a parameter beside it,
// keeping none of the problems typing it finds, which the expression's own resolution
// reports.
//
// Takes expression (ast.Expression) which is the expression.
// Takes scope (*ScopeChain) which is the scope it is read in.
//
// Returns sqlmodel.SQLType which is its type.
// Returns bool which is false when typing it failed.
func (r *TypeResolver) quietExpressionType(ctx context.Context, expression ast.Expression, scope *ScopeChain) (sqlmodel.SQLType, bool) {
	enclosing := r.notices
	expressionType, _, err := r.resolveExpressionType(ctx, expression, scope, new(false))
	r.notices = enclosing
	return expressionType, err == nil
}

// operandPartnerType infers a parameter's type from the other operand in a numeric or
// text operation that preserves that type. Operands of other types provide no type.
//
// Takes number (int) which is the parameter's number.
// Takes scope (*ScopeChain) which is the scope the other operand is read in.
//
// Returns sqlmodel.SQLType which is the other operand's type.
// Returns bool which is false when the parameter is no such operand.
func (r *TypeResolver) operandPartnerType(ctx context.Context, number int, scope *ScopeChain) (sqlmodel.SQLType, bool) {
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	partner, found := r.assigned.operands[number]
	if !found {
		return unknown, false
	}
	partnerType, typed := r.quietExpressionType(ctx, partner.other, scope)
	if !typed || !partnerCategories[partnerType.Category] {
		return unknown, false
	}
	if resultType, _, typed := r.engine.BinaryResult(partner.operator, partnerType, partnerType); typed && resultType.Category != partnerType.Category {
		return unknown, false
	}
	return partnerType, true
}

// classifyAssignedParameters identifies parameters assigned only within larger
// expressions so they can be typed independently of the target column. Parameters
// assigned as whole values retain the column's type, including those passed through
// casts, COALESCE, CASE or NULLIF.
//
// Takes rawAnalysis (*ast.RawQueryAnalysis) which is the statement; nil gives none.
//
// Returns assignedParameters which holds the parameters, the calls they are arguments of
// and the operations they are operands of.
func classifyAssignedParameters(rawAnalysis *ast.RawQueryAnalysis) assignedParameters {
	if rawAnalysis == nil {
		return assignedParameters{embedded: nil, partial: nil, calls: nil, operands: nil}
	}
	items := valuesItems(rawAnalysis)
	scan := &assignedValueScan{
		whole:        make(map[int]bool),
		embedded:     make(map[int]bool),
		calls:        make(map[int]enclosingCall),
		operands:     make(map[int]operandPartner),
		numbers:      namedParameterNumbers(rawAnalysis.ParameterReferences),
		items:        items,
		placeholders: placeholdersByStart(rawAnalysis.Spans.Placeholders),
		inserted:     0,
		itemsMatch:   len(items) == countClause(rawAnalysis.ClauseExpressions, ast.ClauseInsertValue),
	}
	for index := range rawAnalysis.ClauseExpressions {
		scan.add(&rawAnalysis.ClauseExpressions[index])
	}
	return scan.result()
}

// positionalTargets maps parameters assigned as whole VALUES entries in an INSERT without
// a target column list to the table's columns by position. These parameters inherit the
// target column's type, nullability and name.
//
// Takes rawAnalysis (*ast.RawQueryAnalysis) which is the statement.
// Takes scope (*ScopeChain) which holds the table the INSERT writes.
//
// Returns map[int]ast.ColumnReference which maps the numbers to the columns, or nil for
// any other statement.
func positionalTargets(rawAnalysis *ast.RawQueryAnalysis, scope *ScopeChain) map[int]ast.ColumnReference {
	if rawAnalysis == nil || scope == nil || len(rawAnalysis.InsertColumns) > 0 || rawAnalysis.InsertTarget.Name == "" ||
		len(rawAnalysis.Spans.Values) == 0 {
		return nil
	}
	visible := rawAnalysis.InsertTarget.Alias
	if visible == "" {
		visible = rawAnalysis.InsertTarget.Name
	}
	table, found := scope.Table(visible)
	if !found {
		return nil
	}
	numbers := namedParameterNumbers(rawAnalysis.ParameterReferences)
	positions := valuePositions(rawAnalysis.Spans.Values)
	targets := make(map[int]ast.ColumnReference)
	for index := range rawAnalysis.Spans.Placeholders {
		placeholder := &rawAnalysis.Spans.Placeholders[index]
		position, whole := positions[placeholder.Span]
		if !whole || position >= len(table.Columns) {
			continue
		}
		number := placeholder.Number
		if placeholder.Name != "" {
			number = numbers[placeholder.Name]
		}
		if _, recorded := targets[number]; number != 0 && !recorded {
			targets[number] = ast.ColumnReference{TableAlias: visible, ColumnName: table.Columns[position].Name}
		}
	}
	return targets
}

// namedParameterNumbers maps each named parameter of a statement to its number.
//
// Takes references ([]ast.RawParameterReference) which are the statement's parameters.
//
// Returns map[string]int which maps the names to the numbers.
func namedParameterNumbers(references []ast.RawParameterReference) map[string]int {
	numbers := make(map[string]int)
	for index := range references {
		if references[index].Name != "" {
			numbers[references[index].Name] = references[index].Number
		}
	}
	return numbers
}

// valuePositions maps the span of each VALUES row value to the value's position in its
// row, so a placeholder that is a whole value finds its position at once. Where two
// values share a span, the first row's position is kept.
//
// Takes rows ([]ast.TupleSpan) which are the VALUES rows.
//
// Returns map[ast.Span]int which maps the spans to the positions.
func valuePositions(rows []ast.TupleSpan) map[ast.Span]int {
	positions := make(map[ast.Span]int)
	for _, row := range rows {
		for position, item := range row.Items {
			if _, recorded := positions[item]; !recorded {
				positions[item] = position
			}
		}
	}
	return positions
}

// placeholdersByStart returns a statement's placeholders ordered by where they start, so
// the placeholders inside a value are found by binary search.
//
// Takes placeholders ([]ast.PlaceholderSpan) which are the placeholders; they are not
// changed.
//
// Returns []ast.PlaceholderSpan which holds them in order.
func placeholdersByStart(placeholders []ast.PlaceholderSpan) []ast.PlaceholderSpan {
	if slices.IsSortedFunc(placeholders, comparePlaceholderStarts) {
		return placeholders
	}
	return slices.SortedStableFunc(slices.Values(placeholders), comparePlaceholderStarts)
}

// comparePlaceholderStarts orders two placeholders by where they start.
//
// Takes left (ast.PlaceholderSpan) which is the first placeholder.
// Takes right (ast.PlaceholderSpan) which is the second placeholder.
//
// Returns int which is negative, zero or positive as left starts before, with or after
// right.
func comparePlaceholderStarts(left, right ast.PlaceholderSpan) int {
	return cmp.Compare(left.Span.Start, right.Span.Start)
}

// valuesItems lists the spans of every value of every row of an INSERT's VALUES list, in
// order.
//
// Takes rawAnalysis (*ast.RawQueryAnalysis) which is the statement.
//
// Returns []ast.Span which holds the spans.
func valuesItems(rawAnalysis *ast.RawQueryAnalysis) []ast.Span {
	var items []ast.Span
	for _, row := range rawAnalysis.Spans.Values {
		items = append(items, row.Items...)
	}
	return items
}

// countClause counts the clause expressions of one clause.
//
// Takes clauseExpressions ([]ast.ClauseExpression) which are the expressions.
// Takes clause (ast.Clause) which is the clause.
//
// Returns int which is the count.
func countClause(clauseExpressions []ast.ClauseExpression, clause ast.Clause) int {
	count := 0
	for index := range clauseExpressions {
		if clauseExpressions[index].Clause.Includes(clause) {
			count++
		}
	}
	return count
}

// markHiddenPlaceholders files as part of a value each placeholder written inside the
// value's span that its expression does not hold, such as one in a subquery. Only the
// placeholders starting inside the span are visited.
//
// Takes item (ast.Span) which is the value's span.
// Takes placeholders ([]ast.PlaceholderSpan) which are the statement's placeholders,
// ordered by where they start.
// Takes numbers (map[string]int) which maps each named parameter to its number.
// Takes held ([]int) which holds the numbers of the parameters the expression holds.
// Takes embedded (map[int]bool) which receives the numbers.
func markHiddenPlaceholders(item ast.Span, placeholders []ast.PlaceholderSpan, numbers map[string]int, held []int, embedded map[int]bool) {
	first, _ := slices.BinarySearchFunc(placeholders, item.Start, func(placeholder ast.PlaceholderSpan, start int) int {
		return cmp.Compare(placeholder.Span.Start, start)
	})
	for index := first; index < len(placeholders) && placeholders[index].Span.Start < item.End; index++ {
		placeholder := &placeholders[index]
		if !item.Contains(placeholder.Span) {
			continue
		}
		number := placeholder.Number
		if placeholder.Name != "" {
			number = numbers[placeholder.Name]
		}
		if number != 0 && !slices.Contains(held, number) {
			embedded[number] = true
		}
	}
}

// wholeValueParameters returns the parameters an assigned value is in itself: a
// parameter, a cast of one, or the parameters among the arguments of a COALESCE, the
// results of a CASE or the argument a function of valueArguments passes through, each of
// which the value can be.
//
// Takes value (ast.Expression) which is the value.
// Takes depth (int) which bounds the search.
//
// Returns []int which holds the numbers.
func wholeValueParameters(value ast.Expression, depth int) []int {
	if depth >= MaxExpressionResolveDepth {
		return nil
	}
	switch typed := value.(type) {
	case *ast.ParameterExpression:
		if typed != nil {
			return []int{typed.Number}
		}
	case *ast.CastExpression:
		if typed != nil {
			return wholeValueParameters(typed.Inner, depth+1)
		}
	case *ast.CoalesceExpression:
		if typed != nil {
			return wholeValuesAmong(typed.Arguments, depth+1)
		}
	case *ast.CaseWhenExpression:
		if typed != nil {
			return wholeCaseResults(typed, depth+1)
		}
	case *ast.FunctionCallExpression:
		if typed != nil {
			return wholeValueArgument(typed, depth+1)
		}
	}
	return nil
}

// wholeValuesAmong returns the parameters each of several expressions is in itself.
//
// Takes expressions ([]ast.Expression) which are the expressions.
// Takes depth (int) which bounds the search.
//
// Returns []int which holds the numbers.
func wholeValuesAmong(expressions []ast.Expression, depth int) []int {
	numbers := make([]int, 0, len(expressions))
	for _, expression := range expressions {
		numbers = append(numbers, wholeValueParameters(expression, depth)...)
	}
	return numbers
}

// wholeCaseResults returns the parameters a CASE's results, ELSE included, are in
// themselves; its conditions are not its value.
//
// Takes expression (*ast.CaseWhenExpression) which is the CASE.
// Takes depth (int) which bounds the search.
//
// Returns []int which holds the numbers.
func wholeCaseResults(expression *ast.CaseWhenExpression, depth int) []int {
	results := make([]ast.Expression, 0, len(expression.Branches)+1)
	for _, branch := range expression.Branches {
		results = append(results, branch.Result)
	}
	return wholeValuesAmong(append(results, expression.ElseResult), depth)
}

// wholeValueArgument returns the parameters the argument a call of a function of
// valueArguments passes through is in itself.
//
// Takes call (*ast.FunctionCallExpression) which is the call.
// Takes depth (int) which bounds the search.
//
// Returns []int which holds the numbers, or none for any other call.
func wholeValueArgument(call *ast.FunctionCallExpression, depth int) []int {
	if call.Schema != "" {
		return nil
	}
	ordinal, passes := valueArguments[strings.ToLower(call.FunctionName)]
	if !passes || ordinal >= len(call.Arguments) {
		return nil
	}
	return wholeValueParameters(call.Arguments[ordinal], depth)
}

// qualifiedFunctionName names a call's function as a parameter's enclosing function is
// recorded: as the dialect gave the name, so the function resolver keys it by the
// catalogue's identifier rules, qualified by its schema when the call names one.
//
// Takes call (*ast.FunctionCallExpression) which is the call.
//
// Returns string which is the name.
func qualifiedFunctionName(call *ast.FunctionCallExpression) string {
	if call.Schema != "" {
		return call.Schema + "." + call.FunctionName
	}
	return call.FunctionName
}

// parametersIn lists the numbers of the parameters an expression holds outside any
// subquery.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns []int which holds the numbers in the order they appear.
func parametersIn(expression ast.Expression) []int {
	var numbers []int
	for node := range preorder(expression) {
		if parameter, isParameter := node.(*ast.ParameterExpression); isParameter && parameter != nil {
			numbers = append(numbers, parameter.Number)
		}
	}
	return numbers
}
