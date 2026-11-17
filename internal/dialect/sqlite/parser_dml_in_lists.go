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
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// missingInList reports whether IN has nothing after it to test against: the statement
// ends, a parenthesis closes, a comma follows, or a clause or connective starts.
//
// Returns bool which is true when the list is missing.
func (p *parser) missingInList() bool {
	if p.AtEnd() || p.Current().Kind == tokenRightParen || p.Current().Kind == tokenComma {
		return true
	}
	return p.IsKeyword("FROM", "WHERE", "GROUP", "HAVING", "ORDER", "LIMIT", "AND", "OR", "THEN")
}

// parseInListSuffix parses the IN (...) suffix and tags any parameters inside it with the
// IN-list context.
//
// Takes left (ast.Expression) which holds the inner expression.
//
// Returns ast.Expression which is the parsed InList expression.
func (p *parser) parseInListSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	if p.missingInList() {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected a list, a table or a subquery after IN"))
		return &ast.InListExpression{Inner: left, Values: nil}
	}
	if p.isSubqueryStart() {
		innerAnalysis, ok := p.analyseSubqueryBody()
		if !ok {
			return &ast.InListExpression{Inner: left, Values: nil}
		}
		return &ast.InListExpression{
			Inner:  left,
			Values: []ast.Expression{&ast.ScalarSubqueryExpression{InnerQuery: innerAnalysis}},
		}
	}
	parameterCountBefore := p.parameterCount
	referenceCountBefore := len(p.parameterReferences)
	values := p.parseParenthesisedExpressionList()

	var columnReference *ast.ColumnReference
	if columnExpression, ok := left.(*ast.ColumnRefExpression); ok {
		columnReference = &ast.ColumnReference{
			TableAlias: columnExpression.TableAlias,
			ColumnName: columnExpression.ColumnName,
		}
	}
	for reference := range p.newParameterReferences(referenceCountBefore, parameterCountBefore) {
		reference.Context = sqlmodel.ParameterContextInList
		if columnReference != nil {
			reference.ColumnReference = columnReference
		}
	}

	return &ast.InListExpression{Inner: left, Values: values}
}
