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

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// resolvePredicate determines a predicate's type and records its parameter comparisons.
//
// It uses the dialect's predicate typer when available and otherwise returns the core
// boolean, NULL where an operand the predicate compares may be, as predicateNullable
// decides.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes scope (*ScopeChain) which resolves the operands' names.
// Takes dataModifying (*bool) which is set to true if an operand modifies data.
//
// Returns sqlmodel.SQLType which holds the predicate's type.
// Returns bool which indicates whether the result is nullable.
// Returns error which is always nil: an operand's problem is a notice.
func (r *TypeResolver) resolvePredicate(
	ctx context.Context,
	expression ast.Expression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	operands := predicateOperands(expression)
	operandTypes := make([]sqlmodel.SQLType, len(operands))
	operandNullability := make([]bool, len(operands))
	_, listed := expression.(*ast.InListExpression)
	for index, operand := range operands {
		sqlType, nullable, err := r.resolvePredicateOperand(ctx, operand, listed && index > 0, scope, dataModifying)
		r.noteOperandProblem(err)
		operandTypes[index], operandNullability[index] = sqlType, nullable
	}
	quantifiedElements(expression, operandTypes)
	if _, isLogical := expression.(*ast.LogicalOpExpression); isLogical {
		r.noteConditionParameters(operands...)
	}
	r.noteComparedParameters(expression, operands, operandTypes, scope)
	r.noteComparedRows(ctx, expression, scope, dataModifying)
	if typer, isTyper := r.engine.(dialect.PredicateTyperPort); isTyper {
		if resultType, behaviour, typed := typer.PredicateResult(expression, operandTypes); typed {
			return resultType, resultNullable(behaviour, 0, operandNullability), nil
		}
	}
	return r.core.Boolean, r.predicateNullable(expression, operands, operandNullability), nil
}

// resolvePredicateOperand types one operand of a predicate. A subquery an IN list tests
// its value against is NULL only where the column it selects is, rather than when it
// returns no row: a value tested against no row at all is not in it.
//
// Takes operand (ast.Expression) which is the operand.
// Takes member (bool) which is true for a member of an IN list.
// Takes scope (*ScopeChain) which resolves the operand's names.
// Takes dataModifying (*bool) which is set to true if the operand modifies data.
//
// Returns sqlmodel.SQLType which holds the operand's type.
// Returns bool which indicates whether the operand may be NULL.
// Returns error which holds the operand's problem.
func (r *TypeResolver) resolvePredicateOperand(
	ctx context.Context,
	operand ast.Expression,
	member bool,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	if subquery, isSubquery := operand.(*ast.ScalarSubqueryExpression); member && isSubquery && subquery != nil {
		column, found := r.resolveSubqueryColumn(ctx, subquery, scope, dataModifying)
		if found {
			return column.SQLType, column.Nullable, nil
		}
	}
	return r.resolveExpressionType(ctx, operand, scope, dataModifying)
}

// predicateNullable infers predicate nullability from its operands, treating bound
// parameters as non-null values. IS NULL, EXISTS and NULL-safe comparisons are never
// nullable; quantified comparisons may be nullable regardless of their operands.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes operands ([]ast.Expression) which holds its operands, as predicateOperands lists
// them.
// Takes operandNullability ([]bool) which holds, parallel to operands, whether each may
// be NULL.
//
// Returns bool which is true when the predicate may be NULL.
func (r *TypeResolver) predicateNullable(expression ast.Expression, operands []ast.Expression, operandNullability []bool) bool {
	if isQuantified(expression) {
		return true
	}
	switch expression.(type) {
	case *ast.IsNullExpression, *ast.ExistsExpression:
		return false
	case *ast.ComparisonExpression:
		if ast.RequiredFeature(expression, r.operatorFeatures) == ast.SQLFeatureIsNull {
			return false
		}
	}
	for index, nullable := range operandNullability {
		if _, isParameter := parameterStoodFor(operands[index], 0); nullable && !isParameter {
			return true
		}
	}
	return false
}

// noteComparedRows records what each parameter element of a row is compared with when a
// predicate compares two rows, as (created_at, id) < ($1, $2), or tests a row against a
// list of rows: the element at the same position of the other row, as SQL compares rows
// element by element.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes scope (*ScopeChain) which resolves the elements' names.
// Takes dataModifying (*bool) which is set to true if an element modifies data.
func (r *TypeResolver) noteComparedRows(ctx context.Context, expression ast.Expression, scope *ScopeChain, dataModifying *bool) {
	if r.comparedParameters == nil {
		return
	}
	switch typed := expression.(type) {
	case *ast.ComparisonExpression:
		if typed != nil {
			r.notePairedRows(ctx, typed.Left, typed.Right, scope, dataModifying)
		}
	case *ast.InListExpression:
		if typed != nil {
			for _, value := range typed.Values {
				r.notePairedRows(ctx, typed.Inner, value, scope, dataModifying)
			}
		}
	}
}

// notePairedRows records what the parameter elements of two rows of as many elements are
// compared with, pair by pair, typing the elements again without reporting their problems
// a second time.
//
// Takes left (ast.Expression) which is one row.
// Takes right (ast.Expression) which is the other.
// Takes scope (*ScopeChain) which resolves the elements' names.
// Takes dataModifying (*bool) which is set to true if an element modifies data.
func (r *TypeResolver) notePairedRows(ctx context.Context, left, right ast.Expression, scope *ScopeChain, dataModifying *bool) {
	leftRow, leftIsRow := left.(*ast.RowValueExpression)
	rightRow, rightIsRow := right.(*ast.RowValueExpression)
	if !leftIsRow || !rightIsRow || leftRow == nil || rightRow == nil || len(leftRow.Elements) != len(rightRow.Elements) {
		return
	}
	enclosing := r.notices
	defer func() { r.notices = enclosing }()
	for index := range leftRow.Elements {
		pair := []ast.Expression{leftRow.Elements[index], rightRow.Elements[index]}
		types := make([]sqlmodel.SQLType, len(pair))
		for position, element := range pair {
			types[position], _, _ = r.resolveExpressionType(ctx, element, scope, dataModifying)
		}
		r.noteComparedParameters(&ast.ComparisonExpression{Operator: "=", Left: pair[0], Right: pair[1], Quantifier: ""}, pair, types, scope)
	}
}

// quantifiedElements gives the right operand of a comparison quantified with ANY, ALL or
// SOME the type of the elements of the array it reads, which the comparison compares its
// left operand with; a subquery's rows are typed as its column already.
//
// Takes expression (ast.Expression) which is the predicate.
// Takes operandTypes ([]sqlmodel.SQLType) which holds its operands' types, changed in
// place.
func quantifiedElements(expression ast.Expression, operandTypes []sqlmodel.SQLType) {
	if !isQuantified(expression) || len(operandTypes) != comparisonOperandCount {
		return
	}
	if array := operandTypes[1]; array.Category == sqlmodel.TypeCategoryArray && array.ElementType != nil {
		operandTypes[1] = *array.ElementType
	}
}

// isQuantified reports whether an expression is a comparison quantified with ANY, ALL or
// SOME.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns bool which is true for such a comparison.
func isQuantified(expression ast.Expression) bool {
	comparison, isComparison := expression.(*ast.ComparisonExpression)
	return isComparison && comparison != nil && comparison.Quantifier != ""
}

// predicateOperands lists operands in the order required by dialect.PredicateTyperPort.
//
// Takes expression (ast.Expression) which is the predicate.
//
// Returns []ast.Expression which holds the operands; an absent operand is nil.
func predicateOperands(expression ast.Expression) []ast.Expression {
	switch typed := expression.(type) {
	case *ast.ComparisonExpression:
		if typed != nil {
			return []ast.Expression{typed.Left, typed.Right}
		}
	case *ast.IsNullExpression:
		if typed != nil {
			return []ast.Expression{typed.Inner}
		}
	case *ast.InListExpression:
		if typed != nil {
			return append([]ast.Expression{typed.Inner}, typed.Values...)
		}
	case *ast.BetweenExpression:
		if typed != nil {
			return []ast.Expression{typed.Inner, typed.Low, typed.High}
		}
	case *ast.LogicalOpExpression:
		if typed != nil {
			return typed.Operands
		}
	}
	return nil
}
