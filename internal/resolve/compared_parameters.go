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
	"context"
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// comparisonOperandCount is the number of operands a comparison has.
const comparisonOperandCount = 2

var (
	// conditionClauses lists the clauses whose expression is a whole condition.
	conditionClauses = ast.ClauseWhere | ast.ClauseJoinCondition | ast.ClauseHaving

	// betweenBoundSuffixes holds, by the position of a BETWEEN's operand among its operands,
	// the suffix the name of a parameter bounding a column takes: none for the value it
	// tests, _from for the lower bound and _to for the upper.
	betweenBoundSuffixes = []string{"", "_from", "_to"}
)

// comparedValue is what a parameter is compared with: its type, and whether the parameter
// may be NULL there. A parameter compared with an expression takes a value, never NULL;
// one COALESCE falls back from may be NULL.
type comparedValue struct {
	// sqlType is the compared value's type.
	sqlType sqlmodel.SQLType

	// nullable reports a parameter that may be NULL where it is compared.
	nullable bool
}

// comparedColumn records a column directly compared with a parameter, together with the
// comparison's scope. Comparisons against other expressions are recorded separately to
// prevent an unrelated column from determining the parameter's type.
type comparedColumn struct {
	// scope is the scope the predicate's column reference resolves in.
	scope *ScopeChain

	// reference names the column.
	reference ast.ColumnReference

	// quantified reports that the predicate is a comparison quantified with ANY, ALL or SOME
	// reading the parameter as the array of values it compares the column with.
	quantified bool

	// expression reports that the predicate compares the parameter with an expression that
	// is no column alone, so reference names nothing.
	expression bool
}

// comparedName is the name a predicate gives a parameter from the column it compares the
// parameter with.
type comparedName struct {
	// name is the name.
	name string

	// reference names the column the name comes from.
	reference ast.ColumnReference

	// bound reports a bound of a BETWEEN, whose name says which bound it is and takes
	// precedence over the column the dialect records the parameter against.
	bound bool
}

// comparisons is what the predicates of a query compare its parameters with.
type comparisons struct {
	// values maps the number of each parameter a predicate compares to what it is compared
	// with.
	values map[int]comparedValue

	// columns maps the number of each parameter a predicate compares with a column, lists
	// against one or bounds by one to that column.
	columns map[int]comparedColumn

	// patterns holds the number of each parameter a pattern-matching operator, such as LIKE,
	// matches a value against.
	patterns map[int]bool

	// names maps the number of each parameter a predicate compares with a column, alone or
	// through a call over each side, to the name the column gives it.
	names map[int]comparedName
}

// collectComparedParameters gathers inferred types and columns from parameter
// comparisons.
//
// Expression typing is silent, so comparisons can infer parameter types without duplicate
// diagnostics. An expression of a clause in which the dialect lets an expression name a
// select-list alias sees the select list's aliased items.
//
// Takes rawAnalysis (*ast.RawQueryAnalysis) which holds the query's select list and
// clause expressions; nil collects nothing.
// Takes scope (*ScopeChain) which resolves the expressions' names.
//
// Returns comparisons which maps a parameter's number to what it is compared with and to
// the column it is compared with, and holds the parameters matched as patterns, each nil
// when no expression holds a parameter.
func (r *TypeResolver) collectComparedParameters(ctx context.Context, rawAnalysis *ast.RawQueryAnalysis, scope *ScopeChain) comparisons {
	if rawAnalysis == nil {
		return comparisons{values: nil, columns: nil, patterns: nil, names: nil}
	}
	collected := comparisons{values: nil, columns: nil, patterns: nil, names: nil}
	for index := range rawAnalysis.OutputColumns {
		r.collectComparisons(ctx, rawAnalysis.OutputColumns[index].Expression, scope, &collected)
	}
	for index := range rawAnalysis.ClauseExpressions {
		clauseExpression := &rawAnalysis.ClauseExpressions[index]
		restore := scope.exposeAliasOperands(clauseExpression.Clause)
		r.collectComparisons(ctx, clauseExpression.Expression, scope, &collected)
		restore()
		if conditionClauses.Includes(clauseExpression.Clause) {
			r.collectConditionParameter(clauseExpression.Expression, &collected)
		}
	}
	return collected
}

// collectConditionParameter types a parameter that is a clause's whole condition, as in
// WHERE $1, as the dialect's boolean.
//
// Takes expression (ast.Expression) which is the clause's condition.
// Takes collected (*comparisons) which receives the parameter's type.
func (r *TypeResolver) collectConditionParameter(expression ast.Expression, collected *comparisons) {
	parameter, isParameter := expression.(*ast.ParameterExpression)
	if !isParameter || parameter == nil {
		return
	}
	if collected.values == nil {
		*collected = comparisons{values: make(map[int]comparedValue), columns: make(map[int]comparedColumn),
			patterns: make(map[int]bool), names: make(map[int]comparedName)}
	}
	if _, recorded := collected.values[parameter.Number]; !recorded {
		collected.values[parameter.Number] = comparedValue{sqlType: r.core.Boolean, nullable: false}
	}
}

// noteConditionParameters types each parameter among expressions that stand where a
// condition stands, such as the operands of AND and OR and the condition of a CASE
// branch, as the dialect's boolean, while the comparisons of a query's parameters are
// collected.
//
// Takes expressions (...ast.Expression) which are the conditions.
func (r *TypeResolver) noteConditionParameters(expressions ...ast.Expression) {
	if r.comparedParameters == nil {
		return
	}
	for _, expression := range expressions {
		parameter, isParameter := expression.(*ast.ParameterExpression)
		if !isParameter || parameter == nil {
			continue
		}
		if _, recorded := r.comparedParameters[parameter.Number]; !recorded {
			r.comparedParameters[parameter.Number] = comparedValue{sqlType: r.core.Boolean, nullable: false}
		}
	}
}

// noteNegatedParameter types a parameter that is the operand of a prefix operator the
// dialect types boolean whatever its operand, such as NOT, as the dialect's boolean,
// while the comparisons of a query's parameters are collected.
//
// Takes expression (*ast.UnaryOpExpression) which is the operation.
func (r *TypeResolver) noteNegatedParameter(expression *ast.UnaryOpExpression) {
	if r.comparedParameters == nil {
		return
	}
	if _, isParameter := expression.Operand.(*ast.ParameterExpression); !isParameter {
		return
	}
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	if resultType, _, typed := r.engine.UnaryResult(expression.Operator, unknown); typed && resultType.Category == sqlmodel.TypeCategoryBoolean {
		r.noteConditionParameters(expression.Operand)
	}
}

// collectComparisons adds what an expression compares its parameters with to collected,
// typing the expression silently.
//
// Takes expression (ast.Expression) which is the expression; one that holds no parameter
// adds nothing.
// Takes scope (*ScopeChain) which resolves the expression's names.
// Takes collected (*comparisons) which receives the comparisons, its maps made on first
// use.
func (r *TypeResolver) collectComparisons(ctx context.Context, expression ast.Expression, scope *ScopeChain, collected *comparisons) {
	if !holdsParameter(expression) {
		return
	}
	if collected.values == nil {
		*collected = comparisons{values: make(map[int]comparedValue), columns: make(map[int]comparedColumn),
			patterns: make(map[int]bool), names: make(map[int]comparedName)}
	}
	enclosing := comparisons{values: r.comparedParameters, columns: r.comparedColumns, patterns: r.comparedPatterns, names: r.comparedNames}
	r.comparedParameters, r.comparedColumns = collected.values, collected.columns
	r.comparedPatterns, r.comparedNames = collected.patterns, collected.names
	_, _, _ = r.ResolveExpressionType(ctx, expression, scope, nil)
	r.comparedParameters, r.comparedColumns = enclosing.values, enclosing.columns
	r.comparedPatterns, r.comparedNames = enclosing.patterns, enclosing.names
}

// addComparedParameters adds what parameters are compared with to the comparisons of the
// query being resolved, keeping what a parameter was first compared with, and a column
// over a record that it is compared with an expression.
//
// Takes compared (comparisons) which maps parameter numbers to what they are compared
// with and to the columns they are compared with, and holds those matched as patterns;
// nil maps add nothing.
func (r *TypeResolver) addComparedParameters(compared comparisons) {
	r.parameterComparisons = keepFirst(r.parameterComparisons, compared.values)
	r.parameterColumns = keepColumns(r.parameterColumns, compared.columns)
	r.parameterPatterns = keepFirst(r.parameterPatterns, compared.patterns)
	r.parameterNames = keepFirst(r.parameterNames, compared.names)
}

// noteComparedParameters records the values and columns used to infer parameter types.
//
// Logical operators add no comparison, and each parameter keeps its first association.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes operands ([]ast.Expression) which holds its operands, as predicateOperands lists
// them.
// Takes operandTypes ([]sqlmodel.SQLType) which holds, parallel to operands, their types.
// Takes scope (*ScopeChain) which is the scope the predicate names its columns in.
func (r *TypeResolver) noteComparedParameters(
	expression ast.Expression,
	operands []ast.Expression,
	operandTypes []sqlmodel.SQLType,
	scope *ScopeChain,
) {
	if r.comparedParameters == nil || len(operands) < 2 {
		return
	}
	if _, isLogical := expression.(*ast.LogicalOpExpression); isLogical {
		return
	}
	r.noteComparedColumns(expression, operands, scope)
	for index, operand := range operands {
		parameter, isParameter := operand.(*ast.ParameterExpression)
		if !isParameter || parameter == nil {
			continue
		}
		if _, recorded := r.comparedParameters[parameter.Number]; recorded {
			continue
		}
		if isQuantified(expression) && index > 0 {
			r.noteQuantifiedArray(parameter.Number, operandTypes[0])
			continue
		}
		if counterpart, found := comparedCounterpart(index, operandTypes); found {
			r.comparedParameters[parameter.Number] = comparedValue{sqlType: operandTypes[counterpart], nullable: false}
		}
	}
}

// noteQuantifiedArray types a parameter a comparison quantified with ANY, ALL or SOME
// reads the elements of, as in id = ANY($1), as an array of the left operand's type,
// where the dialect names array types.
//
// Takes number (int) which is the parameter's number.
// Takes element (sqlmodel.SQLType) which is the left operand's type.
func (r *TypeResolver) noteQuantifiedArray(number int, element sqlmodel.SQLType) {
	typer, isTyper := r.engine.(dialect.ArrayTyperPort)
	if !isTyper || element.Category == sqlmodel.TypeCategoryUnknown {
		return
	}
	r.comparedParameters[number] = comparedValue{sqlType: typer.ArrayOf(element), nullable: false}
}

// noteCoalescedParameters infers COALESCE parameter types and names from sibling
// arguments. A parameter with a later fallback is nullable; calls containing only
// literals and parameters are left to comparison inference.
//
// Takes arguments ([]ast.Expression) which are COALESCE's arguments.
// Takes resultType (sqlmodel.SQLType) which is the common type of the arguments.
func (r *TypeResolver) noteCoalescedParameters(arguments []ast.Expression, resultType sqlmodel.SQLType) {
	if r.comparedParameters == nil || resultType.Category == sqlmodel.TypeCategoryUnknown || !holdsTypedArgument(arguments) {
		return
	}
	sibling, named := soleColumnAmong(arguments)
	for index, argument := range arguments {
		parameter, isParameter := argument.(*ast.ParameterExpression)
		if !isParameter || parameter == nil {
			continue
		}
		r.noteParameterValue(parameter.Number, comparedValue{sqlType: resultType, nullable: index < len(arguments)-1})
		if named {
			r.noteParameterName(parameter.Number, comparedName{name: sibling.ColumnName, reference: sibling, bound: false})
		}
	}
}

// noteSharedArgumentParameters infers parameter types for calls whose signatures share
// argument and result types, such as NULLIF, GREATEST and LEAST. A sole sibling column
// also supplies the parameter's name.
//
// Takes call (*ast.FunctionCallExpression) which is the call.
// Takes arguments ([]callArgument) which holds, parallel to the call's arguments, their
// types.
func (r *TypeResolver) noteSharedArgumentParameters(call *ast.FunctionCallExpression, arguments []callArgument) {
	if r.comparedParameters == nil || len(call.Arguments) < comparisonOperandCount || len(arguments) != len(call.Arguments) {
		return
	}
	shared, common := r.sharedArgumentType(call, arguments)
	if siblingExcludedCategories[common.Category] {
		return
	}
	sibling, named := soleColumnArgument(call)
	for ordinal, argument := range call.Arguments {
		parameter, isParameter := argument.(*ast.ParameterExpression)
		if !shared[ordinal] || !isParameter || parameter == nil {
			continue
		}
		r.noteParameterValue(parameter.Number, comparedValue{sqlType: common, nullable: false})
		if named {
			r.noteParameterName(parameter.Number, comparedName{name: sibling.ColumnName, reference: sibling, bound: false})
		}
	}
}

// sharedArgumentType finds which positions of a call share their type with the call's
// result and the common type of the arguments at those positions that are no parameters.
//
// Takes call (*ast.FunctionCallExpression) which is the call.
// Takes arguments ([]callArgument) which holds, parallel to the call's arguments, their
// types.
//
// Returns []bool which holds, parallel to the arguments, whether each shares the type.
// Returns sqlmodel.SQLType which is the common type, unknown when none gives one.
func (r *TypeResolver) sharedArgumentType(call *ast.FunctionCallExpression, arguments []callArgument) ([]bool, sqlmodel.SQLType) {
	name := qualifiedFunctionName(call)
	shared := make([]bool, len(call.Arguments))
	common := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	for ordinal, argument := range call.Arguments {
		shared[ordinal] = r.functionResolver.SharesArgumentType(name, ordinal)
		if _, isParameter := argument.(*ast.ParameterExpression); shared[ordinal] && !isParameter {
			common = r.commonSupertype(common, arguments[ordinal].sqlType)
		}
	}
	return shared, common
}

// noteParameterValue records what a parameter is compared with, unless an earlier place
// recorded it, while the comparisons of a query's parameters are collected.
//
// Takes number (int) which is the parameter's number.
// Takes value (comparedValue) which is what it is compared with.
func (r *TypeResolver) noteParameterValue(number int, value comparedValue) {
	if r.comparedParameters == nil {
		return
	}
	if _, recorded := r.comparedParameters[number]; !recorded {
		r.comparedParameters[number] = value
	}
}

// noteParameterName records the name a place gives a parameter, unless an earlier place
// named it, while the comparisons of a query's parameters are collected.
//
// Takes number (int) which is the parameter's number.
// Takes name (comparedName) which is the name.
func (r *TypeResolver) noteParameterName(number int, name comparedName) {
	if r.comparedNames == nil {
		return
	}
	if _, recorded := r.comparedNames[number]; !recorded {
		r.comparedNames[number] = name
	}
}

// noteArityTypedParameters types and names parameters from overloads selected by the
// call's typed arguments, then by its argument count. Parameters typed identically by
// every overload are left to the function argument path.
//
// Takes call (*ast.FunctionCallExpression) which is the call.
// Takes arguments ([]callArgument) which holds, parallel to the call's arguments, their
// types.
func (r *TypeResolver) noteArityTypedParameters(call *ast.FunctionCallExpression, arguments []callArgument) {
	if r.comparedParameters == nil {
		return
	}
	name := qualifiedFunctionName(call)
	argumentTypes := typedCallArguments(call, arguments)
	for ordinal, argument := range call.Arguments {
		parameter, isParameter := argument.(*ast.ParameterExpression)
		if !isParameter || parameter == nil {
			continue
		}
		if _, recorded := r.comparedParameters[parameter.Number]; recorded {
			continue
		}
		argumentType, settled := r.overloadParameterType(name, ordinal, argumentTypes)
		if !settled {
			continue
		}
		r.noteParameterValue(parameter.Number, comparedValue{
			sqlType:  r.resolveCustomType(argumentType),
			nullable: r.functionResolver.ArgumentAcceptsNull(name, ordinal),
		})
		if argumentName, named := r.functionResolver.ArgumentNameAtArity(name, ordinal, len(call.Arguments)); named {
			r.noteParameterName(parameter.Number, comparedName{name: argumentName, reference: ast.ColumnReference{}, bound: false})
		}
	}
}

// overloadParameterType returns the type a function's overloads give a parameter they do
// not all type alike. The overloads the call's typed arguments match exactly decide it
// first, then those taking the call's argument count.
//
// Takes name (string) which is the function name, optionally "schema.name".
// Takes ordinal (int) which is the parameter's zero-based position.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the call's argument types in
// order, unknown where an argument is untyped.
//
// Returns sqlmodel.SQLType which is the parameter's type.
// Returns bool which is false when every overload types the position alike, or the
// overloads settle no known type.
func (r *TypeResolver) overloadParameterType(name string, ordinal int, argumentTypes []sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	if _, settled := r.functionResolver.ArgumentType(name, ordinal); settled {
		return sqlmodel.SQLType{}, false
	}
	argumentType, settled := r.functionResolver.ArgumentTypeAmongExactMatches(name, ordinal, argumentTypes)
	if !settled {
		argumentType, settled = r.functionResolver.ArgumentTypeAtArity(name, ordinal, len(argumentTypes))
	}
	return argumentType, settled && argumentType.Category != sqlmodel.TypeCategoryUnknown
}

// noteComparedColumns associates parameters with the columns used in their predicates.
//
// It recognises comparisons, IN lists and BETWEEN bounds, including supported casts and
// COALESCE wrappers. Each parameter keeps the first column it is compared with. A
// parameter such a predicate compares only with expressions that are no column reference,
// such as my_macro(col), is recorded as compared with an expression until a predicate
// compares it with a column.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes operands ([]ast.Expression) which holds its operands, as predicateOperands lists
// them.
// Takes scope (*ScopeChain) which is the scope the predicate names its columns in.
func (r *TypeResolver) noteComparedColumns(expression ast.Expression, operands []ast.Expression, scope *ScopeChain) {
	if r.comparedColumns == nil {
		return
	}
	if matchesPattern(expression, r.operatorFeatures) {
		r.notePatternParameters(operands)
		return
	}
	if !comparesValues(expression, r.operatorFeatures) {
		return
	}
	r.noteComparedNames(expression, operands, scope)
	for index, operand := range operands {
		if number, isParameter := parameterStoodFor(operand, 0); isParameter {
			r.noteComparedColumn(number, expression, index, operands, scope)
		}
	}
}

// noteComparedColumn records what a predicate compares one parameter with: the column on
// the other side, unless the parameter already has a column, or that it is compared with
// an expression, unless it already has a record.
//
// Takes number (int) which is the parameter's number.
// Takes expression (ast.Expression) which is the predicate.
// Takes index (int) which is the parameter's operand's position among operands.
// Takes operands ([]ast.Expression) which holds the predicate's operands.
// Takes scope (*ScopeChain) which is the scope the predicate names its columns in.
func (r *TypeResolver) noteComparedColumn(number int, expression ast.Expression, index int, operands []ast.Expression, scope *ScopeChain) {
	existing, recorded := r.comparedColumns[number]
	if recorded && !existing.expression {
		return
	}
	reference, found := columnCounterpart(expression, index, operands)
	switch {
	case found && !r.namesLambdaParameter(scope, reference):
		r.comparedColumns[number] = comparedColumn{scope: scope, reference: reference, quantified: isQuantified(expression) && index > 0,
			expression: false}
	case !found && !recorded && hasCounterpart(expression, index, operands):
		r.comparedColumns[number] = comparedColumn{scope: scope, reference: ast.ColumnReference{}, quantified: false, expression: true}
	}
}

// noteComparedNames names predicate parameters after the opposing column, field or map
// key, preserving each parameter's first name. BETWEEN bounds use _from and _to suffixes,
// while lambda parameters supply no names.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes operands ([]ast.Expression) which holds its operands, as predicateOperands lists
// them.
// Takes scope (*ScopeChain) which is the scope the predicate names its columns in.
func (r *TypeResolver) noteComparedNames(expression ast.Expression, operands []ast.Expression, scope *ScopeChain) {
	if r.comparedNames == nil {
		return
	}
	for index, operand := range operands {
		number, isParameter := parameterStoodFor(operand, 0)
		wrapped := false
		if !isParameter {
			number, isParameter = soleParameterArgument(operand)
			wrapped = true
		}
		if !isParameter {
			continue
		}
		if _, recorded := r.comparedNames[number]; recorded {
			continue
		}
		if name, found := counterpartName(expression, index, operands, wrapped); found &&
			!r.namesLambdaParameter(scope, name.reference) {
			r.comparedNames[number] = name
		}
	}
}

// namesLambdaParameter reports whether a column reference names a parameter of a lambda
// being typed, or a field of one, rather than a column.
//
// Takes scope (*ScopeChain) which supplies the identifier rules.
// Takes reference (ast.ColumnReference) which is the reference.
//
// Returns bool which is true when it names a lambda parameter.
func (r *TypeResolver) namesLambdaParameter(scope *ScopeChain, reference ast.ColumnReference) bool {
	if reference.TableAlias != "" {
		_, bound := r.boundLambdaParameter(scope, reference.TableAlias)
		return bound
	}
	_, bound := r.boundLambdaParameter(scope, reference.ColumnName)
	return bound
}

// notePatternParameters records the parameters among the operands of a pattern-matching
// predicate, alone or under casts, as patterns.
//
// Takes operands ([]ast.Expression) which holds the predicate's operands.
func (r *TypeResolver) notePatternParameters(operands []ast.Expression) {
	for _, operand := range operands {
		if number, isParameter := parameterStoodFor(operand, 0); isParameter {
			r.comparedPatterns[number] = true
		}
	}
}

// typedCallArguments lists a call's argument types in order, with parameters and any
// argument beyond the resolved ones as unknown.
//
// Takes call (*ast.FunctionCallExpression) which is the call.
// Takes arguments ([]callArgument) which holds, parallel to the call's arguments, their
// types.
//
// Returns []sqlmodel.SQLType which holds one type per argument of the call.
func typedCallArguments(call *ast.FunctionCallExpression, arguments []callArgument) []sqlmodel.SQLType {
	argumentTypes := make([]sqlmodel.SQLType, len(call.Arguments))
	for ordinal, argument := range call.Arguments {
		argumentTypes[ordinal] = sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
		if _, isParameter := argument.(*ast.ParameterExpression); !isParameter && ordinal < len(arguments) {
			argumentTypes[ordinal] = arguments[ordinal].sqlType
		}
	}
	return argumentTypes
}

// keepColumns adds the columns parameters are compared with to those already recorded,
// keeping a recorded column, and replacing a record that a parameter is compared with an
// expression by a column it is compared with.
//
// Takes into (map[int]comparedColumn) which receives the entries; nil is made when there
// are any.
// Takes from (map[int]comparedColumn) which holds the entries to add.
//
// Returns map[int]comparedColumn which holds both.
func keepColumns(into, from map[int]comparedColumn) map[int]comparedColumn {
	if len(from) > 0 && into == nil {
		into = make(map[int]comparedColumn, len(from))
	}
	for number, column := range from {
		if existing, recorded := into[number]; recorded && (!existing.expression || column.expression) {
			continue
		}
		into[number] = column
	}
	return into
}

// keepFirst adds the entries of one map to another, keeping an entry the other already
// holds.
//
// Takes into (map[int]V) which receives the entries; nil is made when there are any.
// Takes from (map[int]V) which holds the entries to add.
//
// Returns map[int]V which holds both.
func keepFirst[V any](into, from map[int]V) map[int]V {
	if len(from) > 0 && into == nil {
		into = make(map[int]V, len(from))
	}
	for number, value := range from {
		if _, recorded := into[number]; !recorded {
			into[number] = value
		}
	}
	return into
}

// holdsTypedArgument reports whether a list of arguments holds one that is neither a
// literal nor a parameter, whose type the others take.
//
// Takes arguments ([]ast.Expression) which are the arguments.
//
// Returns bool which is true when one is.
func holdsTypedArgument(arguments []ast.Expression) bool {
	return slices.ContainsFunc(arguments, func(argument ast.Expression) bool {
		switch typed := argument.(type) {
		case *ast.ParameterExpression, *ast.LiteralExpression:
			return false
		default:
			return typed != nil
		}
	})
}

// hasCounterpart reports whether a predicate compares one of its operands with another,
// as counterpartOperand finds it.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes index (int) which is the operand's position among operands.
// Takes operands ([]ast.Expression) which holds the predicate's operands.
//
// Returns bool which is true when the predicate compares the operand with something.
func hasCounterpart(expression ast.Expression, index int, operands []ast.Expression) bool {
	_, found := counterpartOperand(expression, index, operands)
	return found
}

// counterpartName returns the name a predicate gives one of its operands from the column,
// field or entry on the other side, as noteComparedNames describes.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes index (int) which is the operand's position among operands.
// Takes operands ([]ast.Expression) which holds the predicate's operands.
// Takes wrapped (bool) which is true when the operand is a call holding the parameter.
//
// Returns comparedName which is the name.
// Returns bool which is false when the other side names nothing.
func counterpartName(expression ast.Expression, index int, operands []ast.Expression, wrapped bool) (comparedName, bool) {
	if !wrapped {
		counterpart, found := counterpartOperand(expression, index, operands)
		if !found {
			return comparedName{}, false
		}
		name, named := operandName(counterpart)
		if !named {
			return comparedName{}, false
		}
		if _, isBetween := expression.(*ast.BetweenExpression); isBetween {
			name.name += betweenBoundSuffixes[index]
			name.bound = true
		}
		return name, true
	}
	if !isComparison(expression) || len(operands) != comparisonOperandCount {
		return comparedName{}, false
	}
	other := operands[1-index]
	if name, named := operandName(other); named {
		return name, true
	}
	if reference, found := soleColumnArgument(other); found {
		return comparedName{name: reference.ColumnName, reference: reference, bound: false}, true
	}
	return comparedName{}, false
}

// operandName derives a compared parameter's name from a column, field or map key. Fields
// and entries retain their source column reference, so lambda parameters cannot supply a
// name.
//
// Takes operand (ast.Expression) which is the operand.
//
// Returns comparedName which is the name.
// Returns bool which is false when the operand names nothing.
func operandName(operand ast.Expression) (comparedName, bool) {
	switch typed := operand.(type) {
	case *ast.ColumnRefExpression:
		if typed == nil || typed.ColumnName == "" {
			return comparedName{}, false
		}
		reference := ast.ColumnReference{TableAlias: typed.TableAlias, ColumnName: typed.ColumnName}
		return comparedName{name: typed.ColumnName, reference: reference, bound: false}, true
	case *ast.StructFieldAccessExpression:
		if typed == nil || typed.FieldName == "" {
			return comparedName{}, false
		}
		return comparedName{name: typed.FieldName, reference: columnReadFrom(typed.Struct), bound: false}, true
	case *ast.ArraySubscriptExpression:
		if typed == nil || typed.Key == "" {
			return comparedName{}, false
		}
		return comparedName{name: typed.Key, reference: columnReadFrom(typed.Array), bound: false}, true
	default:
		return comparedName{}, false
	}
}

// columnReadFrom returns the column a value is read from through its fields and
// subscripts, as entry is in entry.note['x'].
//
// Takes value (ast.Expression) which is the value.
//
// Returns ast.ColumnReference which names the column, or is zero when the value is read
// from no column.
func columnReadFrom(value ast.Expression) ast.ColumnReference {
	for range MaxExpressionResolveDepth {
		switch typed := value.(type) {
		case *ast.StructFieldAccessExpression:
			if typed == nil {
				return ast.ColumnReference{}
			}
			value = typed.Struct
		case *ast.ArraySubscriptExpression:
			if typed == nil {
				return ast.ColumnReference{}
			}
			value = typed.Array
		case *ast.ColumnRefExpression:
			if typed == nil {
				return ast.ColumnReference{}
			}
			return ast.ColumnReference{TableAlias: typed.TableAlias, ColumnName: typed.ColumnName}
		default:
			return ast.ColumnReference{}
		}
	}
	return ast.ColumnReference{}
}

// soleParameterArgument returns the parameter a call takes as its one parameter argument,
// alone or under a cast, when no other argument holds one.
//
// Takes operand (ast.Expression) which is the operand.
//
// Returns int which is the parameter's number.
// Returns bool which is false when the operand is no such call.
func soleParameterArgument(operand ast.Expression) (int, bool) {
	call, isCall := operand.(*ast.FunctionCallExpression)
	if !isCall || call == nil {
		return 0, false
	}
	number, found := 0, false
	for _, argument := range call.Arguments {
		if candidate, isParameter := parameterStoodFor(argument, 0); isParameter {
			if found {
				return 0, false
			}
			number, found = candidate, true
			continue
		}
		if holdsParameter(argument) {
			return 0, false
		}
	}
	return number, found
}

// soleColumnArgument returns the column a call takes as its one column argument, when it
// takes exactly one.
//
// Takes operand (ast.Expression) which is the operand.
//
// Returns ast.ColumnReference which names the column.
// Returns bool which is false when the operand is no such call.
func soleColumnArgument(operand ast.Expression) (ast.ColumnReference, bool) {
	call, isCall := operand.(*ast.FunctionCallExpression)
	if !isCall || call == nil {
		return ast.ColumnReference{}, false
	}
	return soleColumnAmong(call.Arguments)
}

// soleColumnAmong returns the one column reference among a list of expressions, when
// exactly one of them is a column reference.
//
// Takes expressions ([]ast.Expression) which are the expressions.
//
// Returns ast.ColumnReference which names the column.
// Returns bool which is false when none or several of them are column references.
func soleColumnAmong(expressions []ast.Expression) (ast.ColumnReference, bool) {
	found := ast.ColumnReference{}
	for _, argument := range expressions {
		column, isColumn := argument.(*ast.ColumnRefExpression)
		if !isColumn || column == nil || column.ColumnName == "" {
			continue
		}
		if found.ColumnName != "" {
			return ast.ColumnReference{}, false
		}
		found = ast.ColumnReference{TableAlias: column.TableAlias, ColumnName: column.ColumnName}
	}
	return found, found.ColumnName != ""
}

// matchesPattern reports whether a predicate is a comparison whose operator
// ast.RequiredFeature judges a pattern match, such as LIKE or a regular expression match.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes operatorFeatures (map[string]ast.ExpressionFeature) which maps the dialect's
// operators to the features they need.
//
// Returns bool which is true for a pattern match.
func matchesPattern(expression ast.Expression, operatorFeatures map[string]ast.ExpressionFeature) bool {
	_, comparison := expression.(*ast.ComparisonExpression)
	return comparison && ast.RequiredFeature(expression, operatorFeatures) == ast.SQLFeaturePatternMatch
}

// comparesValues reports whether a predicate compares values: a comparison whose operator
// ast.RequiredFeature judges a comparison, or a NULL test that compares NULL as a value
// such as IS DISTINCT FROM, rather than a pattern match or any other test, an IN list or
// a BETWEEN.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes operatorFeatures (map[string]ast.ExpressionFeature) which maps the dialect's
// operators to the features they need.
//
// Returns bool which is true for a predicate comparing values.
func comparesValues(expression ast.Expression, operatorFeatures map[string]ast.ExpressionFeature) bool {
	switch expression.(type) {
	case *ast.ComparisonExpression:
		feature := ast.RequiredFeature(expression, operatorFeatures)
		return feature == ast.SQLFeatureBinaryComparison || feature == ast.SQLFeatureIsNull
	case *ast.InListExpression, *ast.BetweenExpression:
		return true
	default:
		return false
	}
}

// columnCounterpart returns the column a predicate compares one of its operands with, as
// counterpartOperand finds it, when that is a column reference alone.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes index (int) which is the operand's position among operands.
// Takes operands ([]ast.Expression) which holds the predicate's operands.
//
// Returns ast.ColumnReference which names the column.
// Returns bool which is false when the operand is compared with no column alone.
func columnCounterpart(expression ast.Expression, index int, operands []ast.Expression) (ast.ColumnReference, bool) {
	counterpart, found := counterpartOperand(expression, index, operands)
	if !found {
		return ast.ColumnReference{}, false
	}
	column, isColumn := counterpart.(*ast.ColumnRefExpression)
	if !isColumn || column == nil || column.ColumnName == "" {
		return ast.ColumnReference{}, false
	}
	return ast.ColumnReference{TableAlias: column.TableAlias, ColumnName: column.ColumnName}, true
}

// counterpartOperand returns the operand a predicate compares one of its operands with:
// for a comparison, the other operand, and for an IN list or a BETWEEN, the tested value,
// when the operand is a member or a bound.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes index (int) which is the operand's position among operands.
// Takes operands ([]ast.Expression) which holds the predicate's operands.
//
// Returns ast.Expression which is the counterpart.
// Returns bool which is false when the predicate compares the operand with nothing.
func counterpartOperand(expression ast.Expression, index int, operands []ast.Expression) (ast.Expression, bool) {
	switch {
	case index > 0:
		return operands[0], true
	case isComparison(expression) && len(operands) == comparisonOperandCount:
		return operands[1], true
	default:
		return nil, false
	}
}

// isComparison reports whether an expression is a comparison of two operands.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns bool which is true for a comparison.
func isComparison(expression ast.Expression) bool {
	_, comparison := expression.(*ast.ComparisonExpression)
	return comparison
}

// parameterStoodFor returns the parameter an operand stands for: a parameter, a cast of
// an operand that stands for one, or a COALESCE whose first argument stands for one and
// whose other arguments are literals.
//
// Takes operand (ast.Expression) which is the operand.
// Takes depth (int) which bounds the search.
//
// Returns int which is the parameter's number.
// Returns bool which is false when the operand stands for no parameter.
func parameterStoodFor(operand ast.Expression, depth int) (int, bool) {
	if depth >= MaxExpressionResolveDepth {
		return 0, false
	}
	switch typed := operand.(type) {
	case *ast.ParameterExpression:
		if typed == nil {
			return 0, false
		}
		return typed.Number, true
	case *ast.CastExpression:
		if typed == nil {
			return 0, false
		}
		return parameterStoodFor(typed.Inner, depth+1)
	case *ast.CoalesceExpression:
		if typed == nil || len(typed.Arguments) == 0 || !literalsOnly(typed.Arguments[1:]) {
			return 0, false
		}
		return parameterStoodFor(typed.Arguments[0], depth+1)
	default:
		return 0, false
	}
}

// literalsOnly reports whether every expression of a list is a literal.
//
// Takes expressions ([]ast.Expression) which is the list.
//
// Returns bool which is true when each is a literal.
func literalsOnly(expressions []ast.Expression) bool {
	for _, expression := range expressions {
		if literal, isLiteral := expression.(*ast.LiteralExpression); !isLiteral || literal == nil {
			return false
		}
	}
	return true
}

// comparedCounterpart returns the operand an operand of a predicate is compared with: for
// the first, the first other operand of a known type, and for any other, the first when
// its type is known.
//
// Takes index (int) which is the operand's position.
// Takes operandTypes ([]sqlmodel.SQLType) which holds the operands' types.
//
// Returns int which is the counterpart's position.
// Returns bool which is false when no counterpart of a known type exists.
func comparedCounterpart(index int, operandTypes []sqlmodel.SQLType) (int, bool) {
	if index > 0 {
		return 0, operandTypes[0].Category != sqlmodel.TypeCategoryUnknown
	}
	for counterpart := 1; counterpart < len(operandTypes); counterpart++ {
		if operandTypes[counterpart].Category != sqlmodel.TypeCategoryUnknown {
			return counterpart, true
		}
	}
	return 0, false
}

// holdsParameter reports whether an expression holds a parameter the dialect placed in
// it, outside any subquery.
//
// Takes expression (ast.Expression) which is the expression to search.
//
// Returns bool which is true when the expression holds one.
func holdsParameter(expression ast.Expression) bool {
	for node := range preorder(expression) {
		if _, isParameter := node.(*ast.ParameterExpression); isParameter {
			return true
		}
	}
	return false
}
