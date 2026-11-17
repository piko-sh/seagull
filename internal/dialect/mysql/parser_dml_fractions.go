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
	"math"
	"math/big"
	"strconv"

	"seagull.sh/seagull/internal/dialect/ast"
)

// constantFractionBits bounds intermediate exact numerator and denominator sizes.
const constantFractionBits = 512

// constantNumericSpelling bounds the numeric literals retained for cast inference.
const constantNumericSpelling = 128

// constantFraction retains exact decimal or native floating point constant values.
type constantFraction struct {
	// value holds the bounded numeric value.
	value *big.Rat

	// approximate selects native floating point arithmetic and integer rounding.
	approximate bool
}

// rememberFractionLiteral retains a bounded noninteger numeric literal.
//
// Takes literal (*ast.LiteralExpression) which supplies the numeric category.
// Takes text (string) which preserves the original spelling.
func (p *parser) rememberFractionLiteral(literal *ast.LiteralExpression, text string) {
	value := constantFraction{value: nil, approximate: literal.TypeName == "double"}
	if value.approximate {
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return
		}
		value.value = new(big.Rat).SetFloat64(number)
	} else if literal.TypeName == "decimal" {
		parsed, ok := new(big.Rat).SetString(text)
		if !ok {
			return
		}
		value.value = parsed
	}
	if !value.bounded() {
		return
	}
	if p.constantFractions == nil {
		p.constantFractions = make(map[ast.Expression]constantFraction)
	}
	p.constantFractions[literal] = value
}

// bounded reports whether a numeric constant stays within the evaluation budget.
//
// Returns bool which rejects nonfinite values and oversized intermediate fractions.
func (value constantFraction) bounded() bool {
	return value.value != nil && value.value.Num().BitLen() <= constantFractionBits && value.value.Denom().BitLen() <= constantFractionBits
}

// fractionNumber reads constants needed by an explicit integer cast.
//
// Takes expression (ast.Expression) which may contain decimal or approximate operands.
// Takes depth (int) which bounds evaluation independently of parser nesting.
//
// Returns constantFraction which retains the native numeric representation.
// Returns bool which rejects runtime dependencies and unsupported expressions.
func (p *parser) fractionNumber(expression ast.Expression, depth int) (constantFraction, bool) {
	if expression == nil || depth >= constantExpressionDepth {
		return constantFraction{}, false
	}
	if value, ok := p.constantFractions[expression]; ok {
		return value, true
	}
	if integer := p.constantNumber(expression, depth); integer != nil {
		return constantFraction{value: new(big.Rat).SetInt(integer), approximate: false}, true
	}
	switch typed := expression.(type) {
	case *ast.UnaryOpExpression:
		value, ok := p.fractionNumber(typed.Operand, depth+1)
		if !ok {
			return constantFraction{}, false
		}
		if typed.Operator == numericMinus {
			value.value = new(big.Rat).Neg(value.value)
			return value, true
		}
		return value, typed.Operator == numericPlus
	case *ast.BinaryOpExpression:
		left, leftOK := p.fractionNumber(typed.Left, depth+1)
		right, rightOK := p.fractionNumber(typed.Right, depth+1)
		if !leftOK || !rightOK {
			return constantFraction{}, false
		}
		return fractionBinary(typed.Operator, left, right)
	}
	return constantFraction{}, false
}

// integer rounds a numeric constant using its native conversion rule.
//
// Exact decimals round ties away from zero, while approximate values round to even.
//
// Returns *big.Int which preserves the rounded value before range checks.
func (value constantFraction) integer() *big.Int {
	if value.approximate {
		number, _ := value.value.Float64()
		rounded, _ := new(big.Float).SetFloat64(math.RoundToEven(number)).Int(nil)
		return rounded
	}
	result, remainder := new(big.Int), new(big.Int)
	result.QuoRem(value.value.Num(), value.value.Denom(), remainder)
	remainder.Abs(remainder).Lsh(remainder, 1)
	if remainder.Cmp(value.value.Denom()) >= 0 {
		result.Add(result, big.NewInt(int64(value.value.Sign())))
	}
	return result
}

// fractionBinary evaluates bounded addition, subtraction and multiplication.
//
// Takes operator (string) which selects a supported arithmetic operation.
// Takes left (constantFraction) which supplies the first operand.
// Takes right (constantFraction) which supplies the second operand.
//
// Returns constantFraction which preserves native floating point evaluation when needed.
// Returns bool which rejects unsupported operations and oversized results.
func fractionBinary(operator string, left, right constantFraction) (constantFraction, bool) {
	result := constantFraction{value: new(big.Rat), approximate: left.approximate || right.approximate}
	if result.approximate {
		first, _ := left.value.Float64()
		second, _ := right.value.Float64()
		switch operator {
		case numericPlus:
			result.value = result.value.SetFloat64(first + second)
		case numericMinus:
			result.value = result.value.SetFloat64(first - second)
		case "*":
			result.value = result.value.SetFloat64(first * second)
		default:
			return constantFraction{}, false
		}
	} else {
		switch operator {
		case numericPlus:
			result.value.Add(left.value, right.value)
		case numericMinus:
			result.value.Sub(left.value, right.value)
		case "*":
			result.value.Mul(left.value, right.value)
		default:
			return constantFraction{}, false
		}
	}
	return result, result.bounded()
}
