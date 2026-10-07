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

package postgres

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// selectListRank orders a query's select list after its join conditions.
	selectListRank = 1

	// returningRank orders an UPDATE's or DELETE's RETURNING list after its WHERE clause and
	// before the values an UPDATE assigns, as PostgreSQL types them.
	returningRank = 6

	// offsetRank orders a query's OFFSET value after the rest of the query, as PostgreSQL
	// types it before its LIMIT value.
	offsetRank = 8

	// limitRank orders a query's LIMIT value last.
	limitRank = 9

	// untypedParameterMessage warns of a parameter PostgreSQL meets first where nothing
	// gives it a type.
	untypedParameterMessage = "the server cannot determine the type of %[1]s, which it meets first %[2]s, so every " +
		"call fails; cast it where it first appears to the type it holds, such as %[1]s::text or %[1]s::bigint"

	// nullTestText says a parameter is met first as the operand of IS NULL.
	nullTestText = "as the operand of IS NULL"

	// lateRank orders a parameter that is a whole value an INSERT or UPDATE assigns after
	// every other place, as PostgreSQL types such a value from its column only once it has
	// read the rest of the statement.
	lateRank = 100
)

var (
	// variadicAnyArguments maps functions taking "any" arguments to the first such argument
	// position. Parameters passed there cannot infer a type from the function, including the
	// any-element arguments of to_json and to_jsonb.
	variadicAnyArguments = map[string]int{
		"concat": 0, "concat_ws": 1, "format": 1, "json_build_object": 0, "jsonb_build_object": 0,
		"json_build_array": 0, "jsonb_build_array": 0, "num_nulls": 0, "num_nonnulls": 0, "to_json": 0, "to_jsonb": 0,
	}

	// clauseRanks orders the clauses as PostgreSQL types a statement's parameters: the FROM
	// clause's join conditions, then the select list, WHERE, HAVING, ORDER BY and GROUP BY,
	// then an UPDATE's or DELETE's RETURNING list, and last the values an UPDATE or INSERT
	// assigns.
	clauseRanks = map[ast.Clause]int{
		ast.ClauseJoinCondition: 0, ast.ClauseWhere: 2, ast.ClauseHaving: 3, ast.ClauseOrderBy: 4, ast.ClauseGroupBy: 5,
		ast.ClauseSetValue: 7, ast.ClauseInsertValue: 7,
	}

	// arithmeticOperators lists the operators PostgreSQL cannot choose between when both
	// operands are parameters of no type.
	arithmeticOperators = []string{"+", "-", "*", "/", "%", "^"}
)

// placeReason says where a place leaves a parameter untyped; its zero value is a place
// that types the parameter.
type placeReason struct {
	// text says where the parameter stands, or is empty for a place that types it.
	text string

	// nullTest reports that the place is the operand of a test for NULL, which gives a
	// parameter no type on any engine of the family.
	nullTest bool
}

// parameterPlace is one place a statement writes a parameter.
type parameterPlace struct {
	// reason says where the parameter stands when nothing there types it.
	reason placeReason

	// rank orders the place as PostgreSQL types the statement's parameters.
	rank int

	// number is the parameter's number.
	number int
}

// untypedPlaces says which places leave a parameter untyped on the dialect's engine.
type untypedPlaces struct {
	// pagination holds the values of the statement's LIMIT and OFFSET clauses, which
	// PostgreSQL types only after the rest of the query, or nil where the engine types them
	// as integers.
	pagination []rankedTree

	// onlyNullTests reports that the engine types a parameter from what its place wants, as
	// CockroachDB does, so only a test for NULL leaves it untyped.
	onlyNullTests bool
}

// rankedTree is an expression of a statement, with the rank of the clause it sits in.
type rankedTree struct {
	// expression is the expression.
	expression ast.Expression

	// rank orders the clause as PostgreSQL types the statement's parameters.
	rank int
}

// placedOperand is an expression nested in another, with where it stands when that gives
// a parameter standing for it no type.
type placedOperand struct {
	// expression is the nested expression.
	expression ast.Expression

	// reason says where the expression stands when that gives a parameter no type.
	reason placeReason
}

// untypedPlaces returns which places leave a parameter untyped on the dialect's engine.
//
// Returns untypedPlaces which holds the statement's LIMIT and OFFSET values and whether
// only tests for NULL leave a parameter untyped.
func (p *parser) untypedPlaces() untypedPlaces {
	if p.parametersTypedFromContext {
		return untypedPlaces{pagination: nil, onlyNullTests: true}
	}
	return untypedPlaces{pagination: p.paginationValues, onlyNullTests: false}
}

// untypedParameterWarnings flags parameters PostgreSQL first encounters without enough
// context to infer a type, causing statement preparation to fail. It checks only
// parameters whose every occurrence is recorded in the statement's own clauses, excluding
// those also found in subqueries, FROM items or LIMIT.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the statement's analysis.
// Takes untyped (untypedPlaces) which says which places leave a parameter untyped.
// Takes tokens ([]token) which is the statement's token stream, which locates each
// warning at the parameter's first placeholder.
// Takes warnings (*statementWarnings) which receives one warning per such parameter while
// it has room.
func untypedParameterWarnings(analysis *ast.RawQueryAnalysis, untyped untypedPlaces, tokens []token, warnings *statementWarnings) {
	places := parameterPlaces(analysis, untyped.pagination)
	written := map[int]int{}
	names := map[int]string{}
	for _, reference := range analysis.ParameterReferences {
		written[reference.Number]++
		if reference.Name != "" {
			names[reference.Number] = reference.Name
		}
	}
	seen := map[int]int{}
	for _, place := range places {
		seen[place.number]++
	}
	judged := map[int]bool{}
	for _, place := range places {
		if judged[place.number] || seen[place.number] != written[place.number] || place.rank >= lateRank {
			continue
		}
		judged[place.number] = true
		if place.reason.text == "" || (untyped.onlyNullTests && !place.reason.nullTest) || !warnings.room() {
			continue
		}
		written := placeholderText(place.number, names[place.number])
		warning := ast.Warning{Message: fmt.Sprintf(untypedParameterMessage, written, place.reason.text), Span: ast.Span{}}
		if placeholder, found := firstPlaceholder(tokens, written); found {
			warning.Span = ast.Span{Start: placeholder.Start, End: placeholder.End}
		}
		warnings.add(warning)
	}
}

// parameterPlaces lists the places the statement's own clauses write a parameter, in the
// order PostgreSQL types them.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the statement's analysis.
// Takes pagination ([]rankedTree) which holds the values of its LIMIT and OFFSET clauses.
//
// Returns []parameterPlace which holds the places in order.
func parameterPlaces(analysis *ast.RawQueryAnalysis, pagination []rankedTree) []parameterPlace {
	trees := slices.Clone(pagination)
	outputRank := selectListRank
	switch {
	case analysis.DataChange&ast.DataChangeInsert != 0:
		outputRank = lateRank + 1
	case analysis.DataChange != 0:
		outputRank = returningRank
	}
	for index := range analysis.OutputColumns {
		trees = append(trees, rankedTree{expression: analysis.OutputColumns[index].Expression, rank: outputRank})
	}
	for _, clause := range analysis.ClauseExpressions {
		rank, ranked := clauseRanks[clause.Clause]
		if !ranked {
			continue
		}
		if _, whole := clause.Expression.(*ast.ParameterExpression); whole && rank == clauseRanks[ast.ClauseSetValue] {
			rank = lateRank
		}
		trees = append(trees, rankedTree{expression: clause.Expression, rank: rank})
	}
	slices.SortStableFunc(trees, func(left, right rankedTree) int { return cmp.Compare(left.rank, right.rank) })
	var places []parameterPlace
	for _, tree := range trees {
		collectParameterPlaces(tree.expression, placeReason{}, tree.rank, &places, 0)
	}
	return places
}

// collectParameterPlaces appends each place an expression writes a parameter, left to
// right as PostgreSQL types them, saying where a place gives the parameter no type. A
// subquery's places are left out, so a parameter written in one is not judged.
//
// Takes expression (ast.Expression) which is the expression; nil writes none.
// Takes reason (placeReason) which says where the expression stands when that gives a
// parameter standing for the whole expression no type.
// Takes rank (int) which orders the expression's clause.
// Takes places (*[]parameterPlace) which receives the places.
// Takes depth (int) which bounds the walk.
func collectParameterPlaces(expression ast.Expression, reason placeReason, rank int, places *[]parameterPlace, depth int) {
	if expression == nil || depth > dialectkit.DefaultMaxParseDepth {
		return
	}
	switch typed := expression.(type) {
	case *ast.ParameterExpression:
		*places = append(*places, parameterPlace{reason: reason, rank: rank, number: typed.Number})
	case *ast.FunctionCallExpression:
		collectCallParameterPlaces(typed, rank, places, depth+1)
	default:
		for _, operand := range placedOperands(expression) {
			collectParameterPlaces(operand.expression, operand.reason, rank, places, depth+1)
		}
	}
}

// placedOperands returns the expressions nested in an expression other than a parameter
// or a call, each with where it stands: the operand of IS NULL, of IS DISTINCT FROM NULL,
// of a sign, and an operand of arithmetic between two parameters give a parameter no
// type.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns []placedOperand which holds the nested expressions, left to right.
func placedOperands(expression ast.Expression) []placedOperand {
	switch typed := expression.(type) {
	case *ast.IsNullExpression:
		return []placedOperand{{expression: typed.Inner, reason: placeReason{text: nullTestText, nullTest: true}}}
	case *ast.ComparisonExpression:
		var leftReason placeReason
		if typed.Right == nil && (typed.Operator == "IS DISTINCT FROM" || typed.Operator == "IS NOT DISTINCT FROM") {
			leftReason = placeReason{text: "as the operand of " + typed.Operator + " NULL", nullTest: true}
		}
		return []placedOperand{{expression: typed.Left, reason: leftReason}, {expression: typed.Right, reason: placeReason{}}}
	case *ast.UnaryOpExpression:
		var operandReason placeReason
		if typed.Operator == "-" || typed.Operator == "+" {
			operandReason = placeReason{text: "as the operand of a sign", nullTest: false}
		}
		return []placedOperand{{expression: typed.Operand, reason: operandReason}}
	case *ast.BinaryOpExpression:
		var operandReason placeReason
		if betweenParameters(typed) {
			operandReason = placeReason{text: "as an operand of " + typed.Operator + " between two parameters", nullTest: false}
		}
		return []placedOperand{{expression: typed.Left, reason: operandReason}, {expression: typed.Right, reason: operandReason}}
	default:
		nested := nestedExpressions(expression)
		operands := make([]placedOperand, 0, len(nested))
		for _, inner := range nested {
			operands = append(operands, placedOperand{expression: inner, reason: placeReason{}})
		}
		return operands
	}
}

// nestedExpressions returns the expressions nested in one whose place gives a parameter
// standing for one of them a type of its own.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns []ast.Expression which holds the nested expressions, left to right.
func nestedExpressions(expression ast.Expression) []ast.Expression {
	switch typed := expression.(type) {
	case *ast.CastExpression:
		return []ast.Expression{typed.Inner}
	case *ast.LogicalOpExpression:
		return typed.Operands
	case *ast.CoalesceExpression:
		return typed.Arguments
	case *ast.InListExpression:
		return append([]ast.Expression{typed.Inner}, typed.Values...)
	case *ast.BetweenExpression:
		return []ast.Expression{typed.Inner, typed.Low, typed.High}
	case *ast.CaseWhenExpression:
		var nested []ast.Expression
		for _, branch := range typed.Branches {
			nested = append(nested, branch.Condition, branch.Result)
		}
		return append(nested, typed.ElseResult)
	case *ast.RowValueExpression:
		return typed.Elements
	case *ast.ArraySubscriptExpression:
		return []ast.Expression{typed.Array, typed.Index}
	case *ast.StructFieldAccessExpression:
		return []ast.Expression{typed.Struct}
	case *ast.NamedArgumentExpression:
		return []ast.Expression{typed.Value}
	case *ast.WindowFunctionExpression:
		if typed.Function != nil {
			return []ast.Expression{typed.Function}
		}
	}
	return nil
}

// collectCallParameterPlaces appends the places of a call's arguments: an argument of a
// function that takes any type at its position gives a parameter no type.
//
// Takes call (*ast.FunctionCallExpression) which is the call.
// Takes rank (int) which orders the call's clause.
// Takes places (*[]parameterPlace) which receives the places.
// Takes depth (int) which bounds the walk.
func collectCallParameterPlaces(call *ast.FunctionCallExpression, rank int, places *[]parameterPlace, depth int) {
	start, takesAny := variadicAnyArguments[call.FunctionName]
	takesAny = takesAny && (call.Schema == "" || call.Schema == "pg_catalog")
	for index, argument := range call.Arguments {
		var argumentReason placeReason
		if takesAny && index >= start {
			argumentReason = placeReason{text: "as an argument of " + call.FunctionName + ", which takes any type", nullTest: false}
		}
		collectParameterPlaces(argument, argumentReason, rank, places, depth)
	}
	collectParameterPlaces(call.FilterExpression, placeReason{}, rank, places, depth)
}

// betweenParameters reports whether an arithmetic operation's operands are both
// parameters of no type, between which PostgreSQL cannot choose an operator.
//
// Takes operation (*ast.BinaryOpExpression) which is the operation.
//
// Returns bool which is true when both operands are bare parameters.
func betweenParameters(operation *ast.BinaryOpExpression) bool {
	_, leftParameter := operation.Left.(*ast.ParameterExpression)
	_, rightParameter := operation.Right.(*ast.ParameterExpression)
	return leftParameter && rightParameter && slices.Contains(arithmeticOperators, operation.Operator)
}

// placeholderText returns how a statement writes a parameter: :name for a named one and
// $n for a numbered one.
//
// Takes number (int) which is the parameter's number.
// Takes name (string) which is its name, or empty.
//
// Returns string which is the placeholder's text.
func placeholderText(number int, name string) string {
	if name != "" {
		return ":" + name
	}
	return "$" + strconv.Itoa(number)
}

// firstPlaceholder returns the first placeholder token a statement writes as text.
//
// Takes tokens ([]token) which is the statement's token stream.
// Takes text (string) which is the placeholder's text.
//
// Returns token which is the placeholder.
// Returns bool which is false when the statement writes none.
func firstPlaceholder(tokens []token, text string) (token, bool) {
	for _, current := range tokens {
		if isParameterToken(current.Kind) && current.Value == text {
			return current, true
		}
	}
	return token{}, false
}
