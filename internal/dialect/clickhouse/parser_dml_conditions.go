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

package clickhouse

import (
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// messageExpectedCondition is the syntax error at a clause keyword with no condition
	// after it.
	messageExpectedCondition = "expected a condition"

	// messageConditionEnd is the syntax error at a token left over after a complete
	// condition.
	messageConditionEnd = "expected the end of the condition"

	// messageIncompleteCondition is the syntax error for a condition the expression grammar
	// cannot read whole, such as one ending in an operator or leaving a parenthesis open.
	messageIncompleteCondition = "the condition is incomplete"
)

// parseCondition validates and records a clause's condition expression.
//
// The condition must contain exactly one complete expression; empty or malformed
// conditions produce syntax errors.
//
// Takes analysis (*ast.RawQueryAnalysis) which owns the clause.
// Takes clause (ast.Clause) which is the clause the condition belongs to.
// Takes first (int) which is the index of the condition's first token.
func (p *parser) parseCondition(analysis *ast.RawQueryAnalysis, clause ast.Clause, first int) {
	expression, err := p.readCondition(first, p.Position)
	if err != nil {
		p.recordSyntaxError(err)
		return
	}
	addClauseExpressions(analysis, clause, []ast.Expression{expression})
}

// readCondition reads the tokens from first up to end as one expression, in a parser of
// its own that registers no parameters, so the scan that stepped over them keeps the
// parameters it registered.
//
// Takes first (int) which is the index of the condition's first token.
// Takes end (int) which is one past the index of its last token.
//
// Returns ast.Expression which is the condition.
// Returns error which is a dialectkit.ParseError when the tokens are not one complete
// expression, or the failure of a subquery nested too deeply.
func (p *parser) readCondition(first, end int) (ast.Expression, error) {
	following := p.tokenAt(end)
	if first >= end {
		return nil, dialectkit.NewParseError(following, messageExpectedCondition)
	}
	scratch := p.newChildParser(p.Tokens[first:end:end])
	expression := scratch.parseExpression()
	switch {
	case scratch.syntaxError != nil:
		return nil, scratch.syntaxError
	case !scratch.AtEnd():
		return nil, dialectkit.NewParseError(scratch.Current(), messageConditionEnd)
	case containsUnknownExpression(expression):
		return nil, dialectkit.NewParseError(following, messageIncompleteCondition)
	default:
		return expression, nil
	}
}

// tokenAt returns the token at an index of the statement, or the end-of-input token past
// its last one.
//
// Takes index (int) which is the token's index.
//
// Returns token which is the token.
func (p *parser) tokenAt(index int) token {
	if index >= 0 && index < len(p.Tokens) {
		return p.Tokens[index]
	}
	return p.PeekAt(len(p.Tokens))
}
