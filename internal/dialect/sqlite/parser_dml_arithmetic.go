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

package sqlite

import (
	"math"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
)

// parseAddExpression parses left-associative additive operators (+, -, and the string
// concatenation operator ||).
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseAddExpression() ast.Expression {
	left := p.parseMulExpression()

	for p.Current().Kind == tokenOperator &&
		(p.Current().Value == "+" || p.Current().Value == "-" || p.Current().Value == "||") {
		operator := p.Advance().Value
		right := p.parseMulExpression()
		left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: false}
	}

	return left
}

// parseMulExpression parses left-associative multiplicative operators (*, /, %). SQLite
// answers a division or a remainder by zero with NULL, so one whose divisor is not a
// constant SQLite never takes as zero may be NULL over operands that are not.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseMulExpression() ast.Expression {
	left := p.parseUnaryExpression()

	for p.isMulOperator() {
		operator := p.Advance().Value
		divisorStart := p.Position
		right := p.parseUnaryExpression()
		divides := operator != "*" && !p.isNonZeroDivisor(operator, divisorStart, p.Position)
		left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: divides}
	}

	return left
}

// isNonZeroDivisor checks whether a signed or unsigned numeric divisor stays nonzero for
// / or %. Remainder first converts operands to integers, so divisors smaller than one in
// magnitude can produce NULL.
//
// Takes operator (string) which is / or %.
// Takes start (int) which is the index of the divisor's first token.
// Takes end (int) which is one past the index of its last token.
//
// Returns bool which is true for a divisor that is never zero.
func (p *parser) isNonZeroDivisor(operator string, start, end int) bool {
	if end-start == 2 && p.Tokens[start].Kind == tokenOperator &&
		(p.Tokens[start].Value == "-" || p.Tokens[start].Value == "+") {
		start++
	}
	if end-start != 1 || start >= len(p.Tokens) || p.Tokens[start].Kind != tokenNumber {
		return false
	}
	size, readable := numberSize(p.Tokens[start].Value)
	if !readable {
		return false
	}
	if operator == "%" {
		return size >= 1
	}
	return size != 0
}

// isMulOperator reports whether the current token is a multiplicative operator.
//
// Returns bool which is true for *, /, and %.
func (p *parser) isMulOperator() bool {
	if p.Current().Kind == tokenStar {
		return true
	}
	return p.Current().Kind == tokenOperator &&
		(p.Current().Value == "/" || p.Current().Value == "%")
}

// numberSize reads the size of a numeric literal: its value without its sign.
//
// Takes text (string) which is the literal's source text, decimal or hexadecimal.
//
// Returns float64 which is the size.
// Returns bool which is false when the text is no number Go can read.
func numberSize(text string) (float64, bool) {
	lowered := strings.ToLower(text)
	if digits, isHex := strings.CutPrefix(lowered, "0x"); isHex {
		value, err := strconv.ParseUint(digits, hexadecimalBase, 64)
		return float64(value), err == nil
	}
	value, err := strconv.ParseFloat(lowered, 64)
	return math.Abs(value), err == nil
}
