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

import "strings"

const (
	// expressionElementName is the name of an index element no other name fits.
	expressionElementName = "expr"

	// strengthNone marks an expression that gives no name.
	strengthNone = 0

	// strengthWeak marks a name PostgreSQL gives up for a cast's type name.
	strengthWeak = 1

	// strengthStrong marks a column or function name.
	strengthStrong = 2
)

// indexExpressionName names an expression element of an index as PostgreSQL names an
// unnamed index after it: the column, function or cast type it reduces to, case for a
// CASE expression, or expr.
//
// Takes element ([]token) which is the element, with any sort order or operator class.
//
// Returns string which is the element's name.
func indexExpressionName(element []token) string {
	if len(element) > 0 && element[0].Kind == tokenLeftParen {
		element = element[:matchingParenthesis(element, 0)+1]
	} else if len(element) > 1 && element[1].Kind == tokenLeftParen {
		element = element[:matchingParenthesis(element, 1)+1]
	}
	if name, strength := figureExpressionName(element); strength > strengthNone {
		return name
	}
	return expressionElementName
}

// figureExpressionName names an expression as PostgreSQL's FigureColname does for an
// index: a column or function by its name, a cast by its operand or else its type, and a
// CASE expression as case.
//
// Takes expression ([]token) which is the expression.
//
// Returns string which is the name, or empty.
// Returns int which is how strongly the expression gives it.
func figureExpressionName(expression []token) (string, int) {
	expression = withoutEnclosingParentheses(expression)
	if len(expression) == 0 {
		return "", strengthNone
	}
	if cast := lastTopLevelCast(expression); cast > 0 && cast+1 < len(expression) {
		if name, strength := figureExpressionName(expression[:cast]); strength > strengthWeak {
			return name, strength
		}
		return strings.ToLower(expression[cast+1].Value), strengthWeak
	}
	first := expression[0]
	if first.Kind != tokenIdentifier {
		return "", strengthNone
	}
	if first.IsKeyword(tokenIdentifier, "CASE") {
		return "case", strengthWeak
	}
	name, rest := qualifiedTail(expression)
	switch {
	case len(rest) == 0:
		return name, strengthStrong
	case rest[0].Kind == tokenLeftParen && matchingParenthesis(rest, 0) == len(rest)-1:
		if first.IsKeyword(tokenIdentifier, "CAST") && len(expression) == len(rest)+1 {
			return castName(rest[1 : len(rest)-1])
		}
		return name, strengthStrong
	default:
		return "", strengthNone
	}
}

// castName names CAST(operand AS type) by its operand, or else by its type.
//
// Takes inner ([]token) which is what the parentheses enclose.
//
// Returns string which is the name, or empty.
// Returns int which is how strongly the cast gives it.
func castName(inner []token) (string, int) {
	depth := 0
	for index, word := range inner {
		depth = adjustParenthesisDepth(word.Kind, depth)
		if depth != 0 || !word.IsKeyword(tokenIdentifier, keywordAS) || index+1 >= len(inner) {
			continue
		}
		if name, strength := figureExpressionName(inner[:index]); strength > strengthWeak {
			return name, strength
		}
		return strings.ToLower(inner[index+1].Value), strengthWeak
	}
	return "", strengthNone
}

// qualifiedTail reads a dotted name at the start of an expression.
//
// Takes expression ([]token) which starts with an identifier.
//
// Returns string which is the name's last part.
// Returns []token which is what follows the name.
func qualifiedTail(expression []token) (string, []token) {
	name := expression[0].Value
	index := 1
	for index+1 < len(expression) && expression[index].Kind == tokenDot && expression[index+1].Kind == tokenIdentifier {
		name = expression[index+1].Value
		index += 2
	}
	return name, expression[index:]
}

// lastTopLevelCast finds the last :: outside any parentheses.
//
// Takes expression ([]token) which is the expression.
//
// Returns int which is the cast's index, or -1.
func lastTopLevelCast(expression []token) int {
	depth, found := 0, -1
	for index, word := range expression {
		depth = adjustParenthesisDepth(word.Kind, depth)
		if depth == 0 && word.Kind == tokenCast {
			found = index
		}
	}
	return found
}

// withoutEnclosingParentheses strips parentheses that enclose a whole expression.
//
// Takes expression ([]token) which is the expression.
//
// Returns []token which is the expression without them.
func withoutEnclosingParentheses(expression []token) []token {
	for len(expression) > 1 && expression[0].Kind == tokenLeftParen && matchingParenthesis(expression, 0) == len(expression)-1 {
		expression = expression[1 : len(expression)-1]
	}
	return expression
}

// matchingParenthesis finds the parenthesis that closes the one at open.
//
// Takes tokens ([]token) which hold the group.
// Takes open (int) which is the opening parenthesis's index.
//
// Returns int which is the closing parenthesis's index, or the last index when unclosed.
func matchingParenthesis(tokens []token, open int) int {
	depth := 0
	for index := open; index < len(tokens); index++ {
		depth = adjustParenthesisDepth(tokens[index].Kind, depth)
		if depth == 0 {
			return index
		}
	}
	return len(tokens) - 1
}

// parseCheckBody consumes the parenthesised expression of a CHECK constraint and an
// optional NO INHERIT.
//
// Returns []string which holds the names the expression may read as columns, in order and
// with repeats, which the engine names an unnamed check after.
// Returns error when the expression is missing or unbalanced.
func (p *parser) parseCheckBody() ([]string, error) {
	body, err := p.collectParenthesised()
	if err != nil {
		return nil, p.recordSyntaxError(err)
	}
	if p.IsKeyword("NO") && p.Peek().IsKeyword(tokenIdentifier, "INHERIT") {
		p.Advance()
		p.Advance()
	}
	return checkReferences(body), nil
}

// checkReferences lists the words of a CHECK expression that may name columns, leaving
// out function names, qualifiers and the type of a :: cast. The catalogue keeps only the
// table's columns among them.
//
// Takes body ([]token) which is the expression.
//
// Returns []string which holds the names in order, with repeats.
func checkReferences(body []token) []string {
	var names []string
	for index, word := range body {
		if word.Kind != tokenIdentifier {
			continue
		}
		if index+1 < len(body) && (body[index+1].Kind == tokenLeftParen || body[index+1].Kind == tokenDot) {
			continue
		}
		if index > 0 && body[index-1].Kind == tokenCast {
			continue
		}
		names = append(names, word.Value)
	}
	return names
}
