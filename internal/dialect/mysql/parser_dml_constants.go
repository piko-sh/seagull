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

package mysql

import (
	"math/big"
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// constantIntegerBits bounds exact values used for unary result inference.
const constantIntegerBits = 64

// constantExpressionDepth bounds constant evaluation independently of parser depth.
const constantExpressionDepth = 64

// numericPlus identifies arithmetic addition and its unary prefix.
const numericPlus = "+"

// numericMinus identifies arithmetic subtraction and its unary prefix.
const numericMinus = "-"

// widenedNegationType is the exact type MariaDB gives a negated bound integer constant,
// whose value may lie outside the signed integer range.
const widenedNegationType = "decimal(20,0)"

// minimumBigintMagnitude is the magnitude of the smallest BIGINT, 2 to the power 63,
// which only a negated literal reaches.
var minimumBigintMagnitude = new(big.Int).Lsh(big.NewInt(1), constantIntegerBits-1)

// constantUnaryOperators lists the prefix operators a bound integer constant may pass
// through.
var constantUnaryOperators = []string{numericPlus, numericMinus, keywordNOT}

// constantBinaryOperators lists the arithmetic operators a bound integer constant may
// pass through.
var constantBinaryOperators = []string{numericPlus, numericMinus, "*", keywordDIV, "%", keywordMOD}

// parseNumericPrefix retains constant values until its operand has been typed.
//
// Returns ast.Expression which preserves the engine's numeric result category.
func (p *parser) parseNumericPrefix() ast.Expression {
	operator := p.Advance().Value
	ownsConstants := p.constantNumbers == nil
	if ownsConstants {
		p.constantNumbers = make(map[ast.Expression]*big.Int)
		defer func() { p.constantNumbers, p.constantFractions = nil, nil }()
	}
	inner := p.parseCollateExpression()
	result := &ast.UnaryOpExpression{Operator: operator, Operand: inner}
	value := p.constantNumber(inner, 0)
	if operator != numericMinus {
		return result
	}
	if value == nil {
		return p.parameterNegationResult(result)
	}
	typeName, folds := p.negatedConstantType(inner, value)
	if !folds {
		return result
	}
	folded := &ast.LiteralExpression{TypeName: typeName}
	p.constantNumbers[folded] = new(big.Int).Neg(value)
	return folded
}

// negatedConstantType gives the type of a negated constant. A negation the engine widens
// to an exact DECIMAL folds, and so does a negated literal, which keeps its own type
// unless it is the magnitude of the smallest BIGINT.
//
// Takes inner (ast.Expression) which is the negated operand.
// Takes value (*big.Int) which is the operand's known value.
//
// Returns string which is the folded literal's type name.
// Returns bool which is false when the negation keeps its operand's typing.
func (p *parser) negatedConstantType(inner ast.Expression, value *big.Int) (string, bool) {
	literal, isLiteral := inner.(*ast.LiteralExpression)
	foldedLiteral := isLiteral && p.foldNegatedLiterals && value.IsInt64() && value.BitLen() < constantIntegerBits
	decimal := (value.Sign() < 0 || !value.IsInt64()) && !foldedLiteral
	switch {
	case !decimal && !isLiteral:
		return "", false
	case isLiteral && value.Cmp(minimumBigintMagnitude) == 0:
		return bigintTypeName, true
	case decimal:
		return "decimal", true
	default:
		return literal.TypeName, true
	}
}

// parameterNegationResult widens MariaDB's bound integer constants.
//
// Takes expression (*ast.UnaryOpExpression) which retains its parameter bindings.
//
// Returns ast.Expression which widens a value-dependent result to exact decimal.
func (p *parser) parameterNegationResult(expression *ast.UnaryOpExpression) ast.Expression {
	if p.foldNegatedLiterals {
		if parameter, constant := p.parameterConstantInteger(expression.Operand, false, 0); parameter && constant {
			return &ast.CastExpression{Inner: expression, TypeName: widenedNegationType}
		}
	}
	return expression
}

// parameterConstantInteger recognises integer constants supplied at execution time.
//
// MariaDB can promote their negation beyond the signed integer range.
//
// Takes expression (ast.Expression) which supplies a numeric operand.
// Takes integerCast (bool) which allows parameters inside an explicit integer cast.
// Takes depth (int) which bounds inspection independently of parser nesting.
//
// Returns bool which reports an execution parameter.
// Returns bool which excludes row dependencies and unsupported operations.
func (p *parser) parameterConstantInteger(expression ast.Expression, integerCast bool, depth int) (parameter, constant bool) {
	if expression == nil || depth >= constantExpressionDepth {
		return false, false
	}
	switch typed := expression.(type) {
	case *ast.ParameterExpression:
		return true, integerCast
	case *ast.LiteralExpression:
		return false, p.constantNumber(typed, 0) != nil
	case *ast.CastExpression:
		if p.types.normaliseWritten(typed.TypeName).Category != sqlmodel.TypeCategoryInteger {
			return false, false
		}
		return p.parameterConstantInteger(typed.Inner, true, depth+1)
	case *ast.UnaryOpExpression:
		if !slices.Contains(constantUnaryOperators, typed.Operator) {
			return false, false
		}
	case *ast.BinaryOpExpression:
		if !slices.Contains(constantBinaryOperators, typed.Operator) {
			return false, false
		}
	case *ast.ComparisonExpression, *ast.LogicalOpExpression, *ast.CoalesceExpression, *ast.CaseWhenExpression:
	default:
		return false, false
	}
	parameter = false
	for child := range ast.Children(expression) {
		bound, constant := p.parameterConstantInteger(child, integerCast, depth+1)
		if !constant {
			return false, false
		}
		parameter = parameter || bound
	}
	return parameter, true
}

// rememberIntegerLiteral retains bounded numbers inside numeric prefixes.
//
// Takes literal (*ast.LiteralExpression) which identifies the parsed number.
// Takes text (string) which preserves the numeric spelling.
func (p *parser) rememberIntegerLiteral(literal *ast.LiteralExpression, text string) {
	const decimalBase = 10
	if p.constantNumbers == nil || len(text) > constantNumericSpelling {
		return
	}
	value, ok := new(big.Int).SetString(text, decimalBase)
	if ok && value.BitLen() <= constantIntegerBits {
		p.constantNumbers[literal] = value
		return
	}
	p.rememberFractionLiteral(literal, text)
}

// constantNumber evaluates bounded integer expressions without executing SQL functions.
//
// Takes expression (ast.Expression) which may contain an exact numeric constant.
// Takes depth (int) which bounds recursive evaluation.
//
// Returns *big.Int which is nil for unknown or unsupported expressions.
func (p *parser) constantNumber(expression ast.Expression, depth int) *big.Int {
	if expression == nil || depth >= constantExpressionDepth {
		return nil
	}
	if value, exists := p.constantNumbers[expression]; exists {
		return value
	}
	value := p.evaluateConstantNumber(expression, depth+1)
	if value != nil && value.BitLen() > constantIntegerBits {
		value = nil
	}
	p.constantNumbers[expression] = value
	return value
}

// evaluateConstantNumber computes one expression from its constant operands.
//
// Takes expression (ast.Expression) which selects the operation.
// Takes depth (int) which bounds recursive evaluation.
//
// Returns *big.Int which is nil when the expression depends on runtime state.
func (p *parser) evaluateConstantNumber(expression ast.Expression, depth int) *big.Int {
	switch typed := expression.(type) {
	case *ast.CastExpression:
		return p.constantCast(typed, depth)
	case *ast.UnaryOpExpression:
		value := p.constantNumber(typed.Operand, depth)
		if value == nil {
			return nil
		}
		switch typed.Operator {
		case numericMinus:
			return new(big.Int).Neg(value)
		case numericPlus:
			return value
		case keywordNOT:
			return constantBoolean(value.Sign() == 0)
		}
	case *ast.BinaryOpExpression:
		return p.constantBinary(typed.Operator, typed.Left, typed.Right, depth)
	case *ast.ComparisonExpression:
		return p.constantBinary(typed.Operator, typed.Left, typed.Right, depth)
	case *ast.LogicalOpExpression:
		return p.constantLogical(typed, depth)
	case *ast.CoalesceExpression:
		return p.constantCoalesce(typed, depth)
	case *ast.CaseWhenExpression:
		return p.constantCase(typed, depth)
	}
	return nil
}

// constantCoalesce selects the first known value after literal NULL arguments.
//
// Takes expression (*ast.CoalesceExpression) which must have only constant operands.
// Takes depth (int) which bounds recursive evaluation.
//
// Returns *big.Int which is nil for unknown operands or an entirely NULL result.
func (p *parser) constantCoalesce(expression *ast.CoalesceExpression, depth int) *big.Int {
	var result *big.Int
	for _, argument := range expression.Arguments {
		if argument == nil {
			continue
		}
		value := p.constantNumber(argument, depth)
		if value == nil {
			return nil
		}
		if result == nil {
			result = value
		}
	}
	return result
}

// constantCast evaluates exact integer casts and preserves bounded decimal values.
//
// Takes expression (*ast.CastExpression) which supplies the target type.
// Takes depth (int) which bounds recursive evaluation.
//
// Returns *big.Int which is nil for unsupported conversions.
func (p *parser) constantCast(expression *ast.CastExpression, depth int) *big.Int {
	target := p.types.normaliseWritten(expression.TypeName)
	value := p.constantNumber(expression.Inner, depth)
	if value == nil && target.Category == sqlmodel.TypeCategoryInteger {
		value = p.constantRoundedInteger(expression.Inner, target.Unsigned, depth)
	}
	if value == nil {
		return nil
	}
	if target.Category == sqlmodel.TypeCategoryDecimal {
		return value
	}
	if target.Category != sqlmodel.TypeCategoryInteger {
		return nil
	}
	if target.Unsigned {
		modulus := new(big.Int).Lsh(big.NewInt(1), constantIntegerBits)
		return new(big.Int).Mod(value, modulus)
	}
	if value.IsInt64() {
		return value
	}
	return nil
}

// constantRoundedInteger applies native rounding before integer range checks.
//
// Takes expression (ast.Expression) which supplies a fractional constant.
// Takes unsigned (bool) which selects unsigned conversion rules.
// Takes depth (int) which bounds recursive evaluation.
//
// Returns *big.Int which is nil for unknown or oversized values.
func (p *parser) constantRoundedInteger(expression ast.Expression, unsigned bool, depth int) *big.Int {
	fraction, known := p.fractionNumber(expression, depth)
	if !known {
		return nil
	}
	value := fraction.integer()
	if value.BitLen() > constantIntegerBits {
		return nil
	}
	if unsigned && p.foldNegatedLiterals && fraction.value.Sign() < 0 {
		return new(big.Int)
	}
	return value
}

// constantBinary evaluates integer arithmetic and comparisons with known operands.
//
// Takes operator (string) which selects the operation.
// Takes left (ast.Expression) which supplies the first operand.
// Takes right (ast.Expression) which supplies the second operand.
// Takes depth (int) which bounds recursive evaluation.
//
// Returns *big.Int which is nil for unsupported or invalid operations.
func (p *parser) constantBinary(operator string, left, right ast.Expression, depth int) *big.Int {
	first, second := p.constantNumber(left, depth), p.constantNumber(right, depth)
	if first == nil || second == nil {
		return nil
	}
	switch operator {
	case numericPlus:
		return new(big.Int).Add(first, second)
	case numericMinus:
		return new(big.Int).Sub(first, second)
	case "*":
		return new(big.Int).Mul(first, second)
	case keywordDIV:
		if second.Sign() != 0 {
			return new(big.Int).Quo(first, second)
		}
	case "%", keywordMOD:
		if second.Sign() != 0 {
			return new(big.Int).Rem(first, second)
		}
	case "=", "<=>":
		return constantBoolean(first.Cmp(second) == 0)
	case "!=", "<>":
		return constantBoolean(first.Cmp(second) != 0)
	case "<":
		return constantBoolean(first.Cmp(second) < 0)
	case "<=":
		return constantBoolean(first.Cmp(second) <= 0)
	case ">":
		return constantBoolean(first.Cmp(second) > 0)
	case ">=":
		return constantBoolean(first.Cmp(second) >= 0)
	}
	return nil
}

// constantOperands requires every operand to be constant before evaluating a function.
//
// Takes expressions ([]ast.Expression) which supply all operands.
// Takes depth (int) which bounds recursive evaluation.
//
// Returns []*big.Int which is nil when any operand depends on runtime state.
func (p *parser) constantOperands(expressions []ast.Expression, depth int) []*big.Int {
	values := make([]*big.Int, 0, len(expressions))
	for _, expression := range expressions {
		value := p.constantNumber(expression, depth)
		if value == nil {
			return nil
		}
		values = append(values, value)
	}
	return values
}

// constantLogical combines known Boolean operands without assuming unknown values.
//
// Takes expression (*ast.LogicalOpExpression) which supplies the Boolean operation.
// Takes depth (int) which bounds recursive evaluation.
//
// Returns *big.Int which is nil when any operand is unknown.
func (p *parser) constantLogical(expression *ast.LogicalOpExpression, depth int) *big.Int {
	conjunction := expression.Operator == keywordAND
	if !conjunction && expression.Operator != keywordOR {
		return nil
	}
	values := p.constantOperands(expression.Operands, depth)
	if len(values) == 0 {
		return nil
	}
	result := conjunction
	for _, value := range values {
		if conjunction {
			result = result && value.Sign() != 0
		} else {
			result = result || value.Sign() != 0
		}
	}
	return constantBoolean(result)
}

// constantCase selects a branch only when the entire CASE expression is constant.
//
// Takes expression (*ast.CaseWhenExpression) which supplies conditions and results.
// Takes depth (int) which bounds recursive evaluation.
//
// Returns *big.Int which is nil for unknown branches or an implicit NULL result.
func (p *parser) constantCase(expression *ast.CaseWhenExpression, depth int) *big.Int {
	result := p.constantNumber(expression.ElseResult, depth)
	if result == nil && expression.ElseResult != nil {
		return nil
	}
	selected := false
	for _, branch := range expression.Branches {
		condition := p.constantNumber(branch.Condition, depth)
		value := p.constantNumber(branch.Result, depth)
		if (condition == nil && branch.Condition != nil) || (value == nil && branch.Result != nil) {
			return nil
		}
		if !selected && condition != nil && condition.Sign() != 0 {
			result, selected = value, true
		}
	}
	return result
}

// constantBoolean encodes a known SQL comparison as zero or one.
//
// Takes value (bool) which is the comparison result.
//
// Returns *big.Int which contains its integer representation.
func constantBoolean(value bool) *big.Int {
	if value {
		return big.NewInt(1)
	}
	return new(big.Int)
}
