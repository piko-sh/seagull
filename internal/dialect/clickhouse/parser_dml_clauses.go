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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// parseOrderByList reads the body of an ORDER BY clause and records one OrderByColumn
// entry per comma-separated column expression.
//
// Each entry captures the expression text plus its trailing direction (ASC / DESC), nulls
// placement (NULLS FIRST / NULLS LAST), and any WITH FILL [FROM .. TO .. STEP ..] body.
//
// The expression body itself is consumed via the parameter-tracking capture so
// `{name:Type}` placeholders inside the body still register against
// analysis.ParameterReferences. Modifiers stop the expression scan via
// stopAfterOrderByColumn so the per-column direction and fill bodies attach to the right
// column even when an expression contains nested commas (e.g. tuple syntax `(a, b)`).
//
// Takes analysis (*ast.RawQueryAnalysis) whose OrderByColumns and parameter references
// are populated.
func (p *parser) parseOrderByList(analysis *ast.RawQueryAnalysis) {
	for {
		if p.AtEnd() {
			return
		}
		column := p.parseOrderByColumn(analysis)
		if column.Expression == "" && column.Direction == ast.OrderDirectionUnspecified &&
			column.Nulls == ast.OrderNullsUnspecified && !column.HasFill {
			return
		}
		analysis.OrderByColumns = append(analysis.OrderByColumns, column)
		if p.Current().Kind != tokenComma {
			return
		}
		p.Advance()
	}
}

// parseOrderByColumn reads a single ORDER BY entry.
//
// The expression body is reconstructed from tokens. ASC / DESC, NULLS FIRST / NULLS LAST,
// and WITH FILL [FROM .. TO .. STEP ..] modifiers are recognised and recorded on the
// returned OrderByColumn.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to register any captured
// parameter references against.
//
// Returns ast.OrderByColumn which is the populated entry.
func (p *parser) parseOrderByColumn(analysis *ast.RawQueryAnalysis) ast.OrderByColumn {
	start := p.Position
	expression := p.captureOrderByPart(analysis)
	addClauseExpressions(analysis, ast.ClauseOrderBy, p.reparseExpression(start, p.Position))
	column := ast.OrderByColumn{}
	column.Expression = strings.TrimSpace(expression)
	column.Direction = p.matchOrderDirection()
	column.Nulls = p.matchOrderNullsPlacement()
	if p.MatchKeyword(kwWith) && p.MatchKeyword(kwFill) {
		column.HasFill = true
		p.parseOrderByFillBoundaries(analysis, &column)
	}
	return column
}

// matchOrderDirection consumes an optional ASC or DESC keyword.
//
// Returns ast.OrderDirection which is the matched direction, or OrderDirectionUnspecified
// when no direction keyword appears at the cursor.
func (p *parser) matchOrderDirection() ast.OrderDirection {
	if p.MatchKeyword(kwAsc) {
		return ast.OrderDirectionAsc
	}
	if p.MatchKeyword(kwDesc) {
		return ast.OrderDirectionDesc
	}
	return ast.OrderDirectionUnspecified
}

// matchOrderNullsPlacement consumes an optional `NULLS FIRST` or `NULLS LAST` clause.
//
// When NULLS is followed by an unexpected token the parser rewinds so the caller can
// re-interpret it as part of the next modifier.
//
// Returns ast.OrderNullsPlacement which is the matched placement, or
// OrderNullsUnspecified when no placement clause appears at the cursor.
func (p *parser) matchOrderNullsPlacement() ast.OrderNullsPlacement {
	if !p.IsKeyword(kwNulls) {
		return ast.OrderNullsUnspecified
	}
	saved := p.Position
	p.Advance()
	if p.MatchKeyword(kwFirst) {
		return ast.OrderNullsFirst
	}
	if p.MatchKeyword(kwLast) {
		return ast.OrderNullsLast
	}
	p.Position = saved
	return ast.OrderNullsUnspecified
}

// parseOrderByFillBoundaries consumes any combination of `FROM expr`, `TO expr`, and
// `STEP expr` clauses that may follow `WITH FILL` on an ORDER BY column. Each boundary
// body is reconstructed from tokens onto the OrderByColumn.
//
// Takes analysis (*ast.RawQueryAnalysis), the analysis to register any captured parameter
// references against.
// Takes column (*ast.OrderByColumn), the column whose Fill fields should be populated.
func (p *parser) parseOrderByFillBoundaries(
	analysis *ast.RawQueryAnalysis, column *ast.OrderByColumn,
) {
	for p.IsKeyword(kwFrom, kwTo, kwStep) {
		keyword := p.Current().Keyword(tokenIdentifier)
		p.Advance()
		body := p.captureOrderByPart(analysis)
		body = strings.TrimSpace(body)
		switch keyword {
		case kwFrom:
			column.FillFrom = body
		case kwTo:
			column.FillTo = body
		case kwStep:
			column.FillStep = body
		}
	}
}

// parseLimitWithModifier handles the LIMIT ... WITH TIES / WITH FILL trailing modifier
// and the FROM / TO / STEP fields that may follow WITH FILL, for parseLimitClause.
//
// FROM / TO / STEP bodies are captured as textual expressions in the analysis's detail,
// so a later consumer can re-emit or rewrite the LIMIT clause without re-parsing the
// source SQL. Parameter placeholders inside the bodies remain registered through
// consumeExpressionTrackingParams.
//
// Takes analysis (*ast.RawQueryAnalysis) whose detail and parameter references are
// populated.
func (p *parser) parseLimitWithModifier(analysis *ast.RawQueryAnalysis) {
	if p.MatchKeyword(kwTies) {
		return
	}
	if !p.MatchKeyword(kwFill) {
		return
	}
	fillStops := []string{kwFrom, kwTo, kwStep, kwSettings, kwFormat, kwUnion, kwIntersect, kwExcept}
	for p.IsKeyword(kwFrom, kwTo, kwStep) {
		boundary := p.Current().Value
		p.Advance()
		body := p.captureExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, fillStops...)
		recordLimitFill(analysis, boundary, body)
	}
}

// captureExpressionTrackingParams consumes an expression body just like
// consumeExpressionTrackingParams but additionally returns the concatenated textual body
// of the consumed tokens.
//
// Used by clauses that need both the parameter-tracking side effect and the literal SQL
// text of the captured expression (e.g. LIMIT WITH FILL bodies). The returned text joins
// tokens with a single space because the tokeniser strips inter-token whitespace;
// consumers that care about formatting should normalise further. Parameter placeholders
// render as `{name:Type}` exactly as in the source.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to register parameters
// against.
// Takes context (sqlmodel.ParameterContext) which is the context tag applied to any
// captured parameter references.
// Takes stopKeywords (...string) which is the list of top-level keywords that terminate
// the consume loop.
//
// Returns string which is the joined textual body of the consumed tokens.
func (p *parser) captureExpressionTrackingParams(
	analysis *ast.RawQueryAnalysis,
	context sqlmodel.ParameterContext,
	stopKeywords ...string,
) string {
	return p.captureExpression(analysis, context, false, stopKeywords)
}

// captureOrderByPart captures one part of an ORDER BY column, its expression or a WITH
// FILL bound, as captureExpressionTrackingParams does, stopping also at the comma that
// starts the next column.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to register parameters
// against.
//
// Returns string which is the joined textual body of the consumed tokens.
func (p *parser) captureOrderByPart(analysis *ast.RawQueryAnalysis) string {
	return p.captureExpression(analysis, sqlmodel.ParameterContextComparison, true, stopAfterOrderByColumn)
}

// captureExpression is captureExpressionTrackingParams, stopping also at a top-level
// comma when asked.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to register parameters
// against.
// Takes context (sqlmodel.ParameterContext) which is the context tag applied to any
// captured parameter references.
// Takes stopAtComma (bool) which is true when a top-level comma ends the expression.
// Takes stopKeywords ([]string) which is the list of top-level keywords that terminate
// the consume loop.
//
// Returns string which is the joined textual body of the consumed tokens.
func (p *parser) captureExpression(
	analysis *ast.RawQueryAnalysis,
	context sqlmodel.ParameterContext,
	stopAtComma bool,
	stopKeywords []string,
) string {
	var builder strings.Builder
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && (stopAtComma && current.Kind == tokenComma ||
			current.Kind == tokenIdentifier && identifierMatchesAny(current, stopKeywords)) {
			return builder.String()
		}
		if current.Kind == tokenClickHouseParam {
			p.registerClickHouseParameter(analysis, current, context)
		}
		newDepth, halt := advanceParenDepth(current, depth)
		if halt {
			return builder.String()
		}
		depth = newDepth
		appendTokenText(&builder, current)
		p.Advance()
	}
	return builder.String()
}

// parseSettingsClause reads the body of a SETTINGS clause, `name = value, ...`,
// registering any placeholder in a value. The settings a query sets that change its
// analysis, such as join_use_nulls and group_by_use_nulls, are read before the statement
// is walked; see statementSetting.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives any placeholders.
func (p *parser) parseSettingsClause(analysis *ast.RawQueryAnalysis) {
	p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, stopAfterSettings...)
}

// analysePrewhere parses a PREWHERE clause, which ClickHouse applies like a WHERE before
// reading the other columns, so a statement's FROM clause covers it.
//
// PREWHERE is treated as a WHERE for count-rewrite purposes; it sets HasWhereClause so
// the downstream rewriter takes the WHERE path even on PREWHERE-only queries.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
func (p *parser) analysePrewhere(analysis *ast.RawQueryAnalysis) {
	if !p.MatchKeyword(kwPrewhere) {
		return
	}
	analysis.HasWhereClause = true
	start := p.Position
	p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, stopAfterPrewhere...)
	p.parseCondition(analysis, ast.ClauseWhere, start)
}

// analyseLimitClauses parses the LIMIT clauses that close a SELECT arm, a LIMIT n BY
// clause and a plain LIMIT, in either order, and a standalone OFFSET clause, recording
// LIMIT n BY as a clause that shapes the rows and the others as the arm's pagination.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
func (p *parser) analyseLimitClauses(analysis *ast.RawQueryAnalysis) {
	for p.IsKeyword(kwLimit) {
		start := p.Position
		p.Advance()
		if p.parseLimitClause(analysis) {
			p.tail.rowShaping = append(p.tail.rowShaping, p.tailPart(start, p.Position))
			continue
		}
		p.tail.pagination = p.tail.pagination.widened(p.tailPart(start, p.Position))
	}
	if p.IsKeyword(kwOffset) {
		start := p.Position
		p.Advance()
		p.parseOffsetClause(analysis)
		p.tail.pagination = p.tail.pagination.widened(p.tailPart(start, p.Position))
	}
}

// analyseTrailingClauses parses the SETTINGS and FORMAT clauses that close a SELECT arm.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
func (p *parser) analyseTrailingClauses(analysis *ast.RawQueryAnalysis) {
	if p.IsKeyword(kwSettings) {
		start := p.Position
		p.Advance()
		p.parseSettingsClause(analysis)
		p.tail.trailing = p.tail.trailing.widened(p.tailPart(start, p.Position))
	}
	if p.IsKeyword(kwFormat) {
		start := p.Position
		p.Advance()
		p.Advance()
		p.tail.trailing = p.tail.trailing.widened(p.tailPart(start, p.Position))
	}
}

// parseExpressionAlias consumes one `expression AS name` entry of a WITH list, which
// names a value the query may use, such as WITH now() - 7 AS bound, and records the name
// in the analysis's detail. Returns more=true when a comma promises another entry.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the expression's placeholders and
// the name.
//
// Returns more (bool), true when another WITH entry follows.
// Returns error when the entry has no AS name.
func (p *parser) parseExpressionAlias(analysis *ast.RawQueryAnalysis) (more bool, err error) {
	start := p.Position
	p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, kwAs, "SELECT")
	if p.Position == start || !p.MatchKeyword(kwAs) {
		return false, dialectkit.NewParseError(p.Current(), "expected AS and a name after a WITH expression")
	}
	alias, aliasErr := p.parseIdentifierOrKeyword()
	if aliasErr != nil {
		return false, aliasErr
	}
	detail := detailOf(analysis)
	detail.expressionAliases = append(detail.expressionAliases, alias)
	if p.Current().Kind == tokenComma {
		p.Advance()
		return true, nil
	}
	return false, nil
}

// analyseSelectFilters parses the WHERE clause of a SELECT.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
func (p *parser) analyseSelectFilters(analysis *ast.RawQueryAnalysis) {
	if !p.IsKeyword(kwWhere) {
		return
	}
	start := p.Position
	p.Advance()
	analysis.HasWhereClause = true
	conditionStart := p.Position
	p.parseWhereClause(analysis)
	p.parseCondition(analysis, ast.ClauseWhere, conditionStart)
	p.recordSpan(dialectkit.SpanWhere, start, p.Position)
	p.recordSpan(dialectkit.SpanWhereCondition, conditionStart, p.Position)
}

// analyseSelectTail parses the optional QUALIFY clause and the ORDER BY, LIMIT, SETTINGS,
// and FORMAT clauses of a SELECT arm.
//
// QUALIFY filters rows after window-function evaluation; it mirrors the HAVING clause
// shape and is parsed with the same expression helper. The clause's presence is recorded
// in the analysis's detail so a later consumer knows of it without re-scanning the SQL.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns error when a tail clause is malformed.
func (p *parser) analyseSelectTail(analysis *ast.RawQueryAnalysis) error {
	if p.IsKeyword(kwQualify) {
		start := p.Position
		p.Advance()
		conditionStart := p.Position
		p.consumeExpressionTrackingParams(analysis, sqlmodel.ParameterContextComparison, stopAfterQualify...)
		detailOf(analysis).qualify = true
		p.parseCondition(analysis, ast.ClauseRowShaping, conditionStart)
		p.tail.rowShaping = append(p.tail.rowShaping, p.tailPart(start, p.Position))
	}
	p.tail.orderByPlace = p.placeAfterLastToken()
	if p.IsKeyword(kwOrder) {
		start := p.Position
		p.Advance()
		if !p.MatchKeyword(kwBy) {
			return dialectkit.NewParseError(p.Current(), "expected BY after ORDER")
		}
		p.parseOrderByList(analysis)
		p.tail.orderBy = p.tailPart(start, p.Position)
	}
	p.analyseLimitClauses(analysis)
	p.analyseTrailingClauses(analysis)
	return nil
}

// recordLimitFill records the text of one LIMIT ... WITH FILL boundary, FROM, TO or STEP,
// in the analysis's detail, ignoring any other keyword.
//
// Takes analysis (*ast.RawQueryAnalysis) whose detail receives the text.
// Takes keyword (string) which is the FROM, TO or STEP keyword.
// Takes body (string) which is the boundary expression's text.
func recordLimitFill(analysis *ast.RawQueryAnalysis, keyword string, body string) {
	switch strings.ToUpper(keyword) {
	case kwFrom:
		detailOf(analysis).limitFillFrom = body
	case kwTo:
		detailOf(analysis).limitFillTo = body
	case kwStep:
		detailOf(analysis).limitFillStep = body
	}
}

// appendTokenText writes a token's textual form to the supplied builder, separated from
// prior tokens by a single space. Used by captureExpressionTrackingParams to reconstruct
// the source body of a clause expression from the lexed token stream.
//
// String tokens are wrapped in single quotes and parameter tokens are wrapped in `{}` to
// recover the original SQL surface syntax that the tokeniser strips during lexing. All
// other token kinds emit their value verbatim.
//
// The identifier round-trip is lossy for delimited identifiers: the tokeniser unwraps
// backtick and double-quoted identifiers to their bare value, and this helper ignores the
// token's Delimited flag and source text. A name that originally required quoting (a
// keyword, or one containing spaces or special characters) is re-emitted bare here. A
// consumer that re-runs this text through ClickHouse must not assume the output preserves
// the original delimited form.
//
// Takes builder (*strings.Builder), the target output buffer.
// Takes candidate (token), the token whose text should be appended.
func appendTokenText(builder *strings.Builder, candidate token) {
	if builder.Len() > 0 {
		builder.WriteByte(' ')
	}
	switch candidate.Kind {
	case tokenString:

		builder.WriteByte('\'')
		builder.WriteString(escapeClickHouseStringBody(candidate.Value))
		builder.WriteByte('\'')
	case tokenClickHouseParam:
		builder.WriteByte('{')
		builder.WriteString(candidate.Value)
		builder.WriteByte('}')
	default:
		builder.WriteString(candidate.Value)
	}
}
