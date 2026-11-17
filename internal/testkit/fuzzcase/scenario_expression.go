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

package fuzzcase

import (
	"fmt"
	"strings"
)

const (
	// expressionParentheses wraps its operand in redundant parentheses.
	expressionParentheses = "parentheses"

	// expressionCoalesce falls back to its second operand.
	expressionCoalesce = "coalesce"

	// expressionCase chooses between its operands with an always true condition.
	expressionCase = "case"

	// expressionSubquery wraps its operand in a scalar subquery.
	expressionSubquery = "subquery"
)

// typedExpressionRenderers writes each constructor around its rendered operands.
var typedExpressionRenderers = map[string]func(node typedExpression, operands []string) string{
	scenarioCast: func(node typedExpression, operands []string) string {
		return "CAST(" + operands[0] + " AS " + node.sqlType + scenarioClose
	},
	expressionParentheses: func(_ typedExpression, operands []string) string { return "(" + operands[0] + scenarioClose },
	expressionCoalesce: func(_ typedExpression, operands []string) string {
		return "COALESCE(" + operands[0] + ", " + operands[1] + scenarioClose
	},
	expressionCase: func(_ typedExpression, operands []string) string {
		return "CASE WHEN v.id > 0 THEN " + operands[0] + " ELSE " + operands[1] + " END"
	},
	expressionSubquery: func(_ typedExpression, operands []string) string { return "(SELECT " + operands[0] + scenarioClose },
}

// typedExpression is a bounded independent expression tree.
type typedExpression struct {
	// operation names a leaf or type-preserving constructor.
	operation string

	// sqlType retains the independently selected native conversion type.
	sqlType string

	// leaf retains a native literal, column or parameter spelling.
	leaf string

	// children preserves operand order.
	children []typedExpression
}

// sql renders the independently constructed tree.
//
// Returns string which contains a bounded SQL expression.
// Returns error when a node names an unknown constructor.
func (expression typedExpression) sql() (string, error) {
	if len(expression.children) == 0 {
		return expression.leaf, nil
	}
	render, ok := typedExpressionRenderers[expression.operation]
	if !ok {
		return "", fmt.Errorf("unknown independent expression constructor %q", expression.operation)
	}
	operands := make([]string, len(expression.children))
	for index, child := range expression.children {
		operand, err := child.sql()
		if err != nil {
			return "", err
		}
		operands[index] = operand
	}
	return render(expression, operands), nil
}

// scenarioExpression composes at most three wrappers around a typed position.
//
// Takes engine (string) which selects native syntax and binding rules.
// Takes data ([]byte) which supplies bounded versioned choices.
// Takes capability (Capability) which supplies native conversion types.
// Takes value (TypedValue) which retains the exact leaf representation.
//
// Returns string which contains the bounded expression tree.
// Returns error when an internally constructed value cannot render as SQL.
func scenarioExpression(engine string, data []byte, capability Capability, value TypedValue) (string, error) {
	literal, err := value.Literal(engine)
	if err != nil {
		return "", fmt.Errorf("rendering scenario expression value: %w", err)
	}
	leaf := expressionLeaf(literal)
	cast := expressionNode(scenarioCast, capability.CastType, leaf)
	nullType := capability.CastType
	if engine == engineClickHouse && !strings.HasPrefix(nullType, "Nullable(") && value.Kind != typedArray {
		nullType = clickHouseNullable(nullType)
	}
	null := expressionNode(scenarioCast, nullType, expressionLeaf("NULL"))
	if value.Kind == typedArray && engine == engineClickHouse {
		null = cast
	}
	expression := scenarioPosition(engine, data, capability, value, cast, null)
	for index := scenarioWrapperStart; index < min(len(data), scenarioWrapperEnd); index++ {
		expression = scenarioWrapper(data[index]%scenarioWrapperCount, capability.CastType, expression, null)
	}
	return expression.sql()
}

// scenarioPosition places the typed value in the position the input chooses.
//
// Takes engine (string) which selects native syntax and binding rules.
// Takes data ([]byte) which supplies bounded versioned choices.
// Takes capability (Capability) which supplies native conversion types.
// Takes value (TypedValue) which retains the exact leaf representation.
// Takes cast (typedExpression) which is the value cast to its native type.
// Takes null (typedExpression) which is a NULL of the same type.
//
// Returns typedExpression which holds the value in its chosen position.
func scenarioPosition(engine string, data []byte, capability Capability, value TypedValue, cast, null typedExpression) typedExpression {
	switch scenarioByte(data, 2) % scenarioPositionCount {
	case positionColumn:
		return expressionLeaf("v.value")
	case positionParameter:
		return expressionNode(scenarioCast, capability.CastType, expressionLeaf(scenarioPlaceholder(engine, 1, value.Kind, value.Null)))
	case positionCoalesce:
		return expressionNode(expressionCoalesce, capability.CastType, null, cast)
	case positionCase:
		return expressionNode(expressionCase, capability.CastType, cast, cast)
	case positionParentheses:
		return expressionNode(expressionParentheses, capability.CastType, cast)
	case positionSubquery:
		return expressionNode(expressionSubquery, capability.CastType, cast)
	case positionCast:
		return expressionNode(scenarioCast, capability.CastType, cast)
	default:
		return cast
	}
}

// scenarioWrapper wraps an expression in the constructor a wrapper choice selects.
//
// Takes choice (byte) which selects the wrapper.
// Takes castType (string) which supplies the native conversion type.
// Takes expression (typedExpression) which is the wrapped operand.
// Takes null (typedExpression) which is a NULL of the same type.
//
// Returns typedExpression which holds the wrapped operand.
func scenarioWrapper(choice byte, castType string, expression, null typedExpression) typedExpression {
	switch choice {
	case 0:
		return expressionNode(expressionParentheses, castType, expression)
	case 1:
		return expressionNode(scenarioCast, castType, expression)
	case 2:
		return expressionNode(expressionCoalesce, castType, expression, null)
	default:
		return expressionNode(expressionCase, castType, expression, null)
	}
}

// expressionLeaf constructs a source fragment without compiler-derived facts.
//
// Takes sql (string) which supplies independent leaf syntax.
//
// Returns typedExpression which has no child expressions.
func expressionLeaf(sql string) typedExpression {
	return typedExpression{operation: "leaf", sqlType: "", leaf: sql, children: nil}
}

// expressionNode preserves operand order and native type context.
//
// Takes operation (string) which selects a constructor.
// Takes sqlType (string) which supplies the native conversion context.
// Takes children (...typedExpression) which preserves operand order.
//
// Returns typedExpression which retains the supplied operands.
func expressionNode(operation, sqlType string, children ...typedExpression) typedExpression {
	return typedExpression{operation: operation, sqlType: sqlType, leaf: "", children: children}
}
