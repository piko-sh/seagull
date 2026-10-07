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
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// consumeExpressionTrackingParamsCommaAware consumes an expression body like
// consumeExpressionTrackingParams but also stops on a top-level comma.
//
// Clauses such as `LIMIT m, n` and `ORDER BY x, y, ...` use it where commas separate
// sibling expressions rather than continuing a single expression. The function never
// returns an error; the signature is kept void so the loop body stays free of an
// uninspected return value.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to register placeholders
// against.
// Takes context (sqlmodel.ParameterContext) which is the context to record for any
// placeholder found.
// Takes stopKeywords (...string) which are the clause keywords that halt the scan.
func (p *parser) consumeExpressionTrackingParamsCommaAware(
	analysis *ast.RawQueryAnalysis,
	context sqlmodel.ParameterContext,
	stopKeywords ...string,
) {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && tokenIsTopLevelStop(current, stopKeywords) {
			return
		}
		if current.Kind == tokenClickHouseParam {
			p.registerClickHouseParameter(analysis, current, context)
		}
		newDepth, halt := advanceParenDepth(current, depth)
		if halt {
			return
		}
		depth = newDepth
		p.Advance()
	}
}

// consumeTokensUntil consumes the tokens up to, but not including, the one at end,
// registering every `{name:Type}` placeholder among them against the analysis.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to register placeholders
// against.
// Takes end (int) which is the index of the token to stop at.
// Takes context (sqlmodel.ParameterContext) which is the context to record for any
// placeholder found.
func (p *parser) consumeTokensUntil(analysis *ast.RawQueryAnalysis, end int, context sqlmodel.ParameterContext) {
	for p.Position < end && !p.AtEnd() {
		if current := p.Current(); current.Kind == tokenClickHouseParam {
			p.registerClickHouseParameter(analysis, current, context)
		}
		p.Advance()
	}
}

// consumeOneExpression consumes a single expression, terminating at a comma or top-level
// clause keyword.
//
// The expression may be any combination of identifiers, numbers, strings, parameters,
// parenthesised groups, and infix operators. GROUP BY items use it. Any `{name:Type}`
// placeholder is registered against the analysis so a parameter inside a non-column GROUP
// BY expression (for example `GROUP BY toStartOfInterval(ts, {step:UInt32})`) is
// retained.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to register placeholders
// against.
// Takes context (sqlmodel.ParameterContext) which is the context to record for any
// placeholder found.
func (p *parser) consumeOneExpression(analysis *ast.RawQueryAnalysis, context sqlmodel.ParameterContext) {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 {
			if current.Kind == tokenComma {
				return
			}
			if current.Kind == tokenIdentifier && isTopClauseKeyword(current) {
				return
			}
		}
		if current.Kind == tokenClickHouseParam {
			p.registerClickHouseParameter(analysis, current, context)
		}
		newDepth, halt := advanceParenDepth(current, depth)
		if halt {
			return
		}
		depth = newDepth
		p.Advance()
	}
}

// consumeExpressionTrackingParams consumes an expression body and records every
// `{name:Type}` parameter token it encounters as a parameter reference on the analysis.
//
// WHERE clauses use it where parameter context matters for downstream codegen.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to register placeholders
// against.
// Takes context (sqlmodel.ParameterContext) which is the context to record for any
// placeholder found.
// Takes stopKeywords (...string) which are the clause keywords that halt the scan.
func (p *parser) consumeExpressionTrackingParams(
	analysis *ast.RawQueryAnalysis,
	context sqlmodel.ParameterContext,
	stopKeywords ...string,
) {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && current.Kind == tokenIdentifier && identifierMatchesAny(current, stopKeywords) {
			return
		}
		if current.Kind == tokenClickHouseParam {
			p.registerClickHouseParameter(analysis, current, context)
		}
		newDepth, halt := advanceParenDepth(current, depth)
		if halt {
			return
		}
		depth = newDepth
		p.Advance()
	}
}

// collectClickHouseParametersUntilReturning walks the statement up to a top-level
// RETURNING keyword, or its end, registering every `{name:Type}` placeholder with the
// analysis, so the VALUES list of an INSERT keeps its parameters while a RETURNING clause
// after it is left for parseReturningIfPresent to record.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to register placeholders
// against.
// Takes context (sqlmodel.ParameterContext) which is the context to record for any
// placeholder found.
func (p *parser) collectClickHouseParametersUntilReturning(
	analysis *ast.RawQueryAnalysis,
	context sqlmodel.ParameterContext,
) {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && current.IsKeyword(tokenIdentifier, kwReturning) {
			return
		}
		if current.Kind == tokenClickHouseParam {
			p.registerClickHouseParameter(analysis, current, context)
		}
		depth = nestingAfter(current, depth)
		p.Advance()
	}
}

// consumeTableFunctionArguments consumes the parenthesised argument list of a table
// function, registering each `{name:Type}` placeholder in it as a function argument, as
// ClickHouse binds one there.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to register placeholders
// against.
//
// Returns error which is a dialectkit.ParseError when the cursor is not on `(` or the
// list is never closed.
func (p *parser) consumeTableFunctionArguments(analysis *ast.RawQueryAnalysis) error {
	opening := p.Current()
	if opening.Kind != tokenLeftParen {
		return dialectkit.NewParseError(opening, "expected '('")
	}
	p.Advance()
	depth := 1
	for !p.AtEnd() {
		current := p.Advance()
		switch current.Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
			if depth == 0 {
				return nil
			}
		case tokenClickHouseParam:
			p.registerClickHouseParameter(analysis, current, sqlmodel.ParameterContextFunctionArgument)
		default:
		}
	}
	return dialectkit.NewParseError(opening, messageUnmatchedParenthesis)
}

// registerClickHouseParameter records a `{name:Type}` placeholder on the analysis. The
// token's value is `name:Type`; we split on `:` to extract the name and the type tag for
// downstream resolution.
//
// Malformed or unrecognised type tags are recorded as the parser's first parameter-type
// error and cause the surrounding analysis to fail. The reference is still appended:
// malformed tags have a nil CastType, while unrecognised names carry an Unknown type.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to register the
// placeholder against.
// Takes candidate (token) which is the `{name:Type}` placeholder token.
// Takes context (sqlmodel.ParameterContext) which is the context to record.
func (p *parser) registerClickHouseParameter(
	analysis *ast.RawQueryAnalysis,
	candidate token,
	context sqlmodel.ParameterContext,
) {
	name, typeName := splitClickHouseParamBody(candidate.Value)
	number, exists := p.namedParameterMap[name]
	if !exists {
		p.parameterCount++
		number = p.parameterCount
		p.namedParameterMap[name] = number
	}
	var castType *sqlmodel.SQLType
	if typeName != "" {
		parsedType, err := p.types.parse(typeName)
		if err == nil && parsedType.Nullable {
			parsedType.SQLType.Nullable = true
		}
		switch {
		case err != nil:
			if p.firstParameterTypeError == nil {
				p.firstParameterTypeError = dialectkit.NewParseError(candidate, fmt.Sprintf("parameter %q has malformed type tag %q: %v", name, typeName, err))
			}
		case parsedType.SQLType.Category == sqlmodel.TypeCategoryUnknown:
			castType = &parsedType.SQLType
			if p.firstParameterTypeError == nil {
				p.firstParameterTypeError = dialectkit.NewParseError(candidate, fmt.Sprintf("parameter %q has unrecognised type tag %q", name, typeName))
			}
		default:
			castType = &parsedType.SQLType
		}
	}
	var columnReference *ast.ColumnReference
	if column, assigned := p.assignedPlaceholders[candidate.Start]; assigned && context == sqlmodel.ParameterContextAssignment {
		columnReference = &column
	}
	analysis.ParameterReferences = append(analysis.ParameterReferences, ast.RawParameterReference{
		Name:                  name,
		Number:                number,
		Context:               context,
		CastType:              castType,
		ColumnReference:       columnReference,
		EnclosingFunctionName: "",
		ArgumentOrdinal:       0,
	})
}

// markValuesPlaceholders records each `{name:Type}` placeholder that is the whole value
// of a column in the rows of an INSERT ... VALUES, among the tokens from first up to end,
// as assigned to the column the INSERT lists at its position, so the parameter is
// recorded against that column.
//
// Takes first (int) which is the index of the first token after VALUES.
// Takes end (int) which is one past the index of the rows' last token.
// Takes table (string) which is the INSERT's table.
// Takes columns ([]string) which are the columns the INSERT lists, in order.
func (p *parser) markValuesPlaceholders(first, end int, table string, columns []string) {
	end = min(end, len(p.Tokens))
	depth, item := 0, 0
	for index := first; index < end; index++ {
		current := p.Tokens[index]
		switch {
		case depth == 0 && current.Kind == tokenLeftParen:
			item = 0
		case depth == 1 && current.Kind == tokenComma:
			item++
		case depth == 1 && current.Kind == tokenClickHouseParam && item < len(columns) && p.wholeValueAt(index, end):
			p.assignedPlaceholders[current.Start] = ast.ColumnReference{TableAlias: table, ColumnName: columns[item]}
		}
		depth = nestingAfter(current, depth)
	}
}

// wholeValueAt reports whether the placeholder at index is a whole value of a list: the
// token before it opens the list or separates its values, and the token after it closes
// the list or separates its values.
//
// Takes index (int) which is the placeholder's index.
// Takes end (int) which is one past the index of the last token that may follow it.
//
// Returns bool which is true when the placeholder stands alone.
func (p *parser) wholeValueAt(index, end int) bool {
	if index < 1 || index+1 >= end {
		return false
	}
	before, after := p.Tokens[index-1].Kind, p.Tokens[index+1].Kind
	return (before == tokenLeftParen || before == tokenComma) && (after == tokenRightParen || after == tokenComma)
}

// numberPlacedParameters numbers every ParameterExpression the statement's parsers built
// as the statement numbers the parameter its placeholder names, so a placeholder read by
// a nested parser, which numbers its placeholders from one, carries the statement's
// number. A placeholder whose name the statement never registered keeps the number its
// parser gave it.
func (p *parser) numberPlacedParameters() {
	for parameter, name := range p.placedParameters {
		if number, registered := p.namedParameterMap[name]; registered {
			parameter.Number = number
		}
	}
}

// mergeNestedParameterReferences folds a nested parser's parameter references into the
// parent analysis so they appear in the final parameter list.
//
// The nested parser is the one for a CTE body or a FROM-derived subquery. Each named
// placeholder is re-keyed through the parent parser's namedParameterMap so binding
// numbers stay consistent across the whole statement and the same {name:Type} used in
// several scopes collapses to one parameter. This mirrors the INSERT ... SELECT merge in
// analyseInsertBody; without it CTE and derived-subquery parameters are dropped because
// each nested parser numbers from one and never flattens into the outer list.
//
// The first malformed parameter-type tag seen inside a nested body is also lifted onto
// the parent parser so the surrounding analyser surfaces a diagnostic, matching the
// behaviour for top-level SELECT and INSERT placeholders.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the parent analysis to extend.
// Takes nested (*parser) which is the sub-parser that produced nestedAnalysis.
// Takes nestedAnalysis (*ast.RawQueryAnalysis) which holds the nested references.
func (p *parser) mergeNestedParameterReferences(
	analysis *ast.RawQueryAnalysis,
	nested *parser,
	nestedAnalysis *ast.RawQueryAnalysis,
) {
	if nestedAnalysis != nil {
		for index := range nestedAnalysis.ParameterReferences {
			reference := nestedAnalysis.ParameterReferences[index]
			if reference.Name != "" {
				number, exists := p.namedParameterMap[reference.Name]
				if !exists {
					p.parameterCount++
					number = p.parameterCount
					p.namedParameterMap[reference.Name] = number
				}
				reference.Number = number
			}
			analysis.ParameterReferences = append(analysis.ParameterReferences, reference)
		}
	}
	if p.firstParameterTypeError == nil && nested != nil && nested.firstParameterTypeError != nil {
		p.firstParameterTypeError = nested.firstParameterTypeError
	}
}

// nestingAfter returns the bracket nesting depth after a token, counting parentheses,
// square brackets and the braces of map literals, and never going below zero.
//
// Takes candidate (token) which is the token under inspection.
// Takes depth (int) which is the depth before the token.
//
// Returns int which is the depth after the token.
func nestingAfter(candidate token, depth int) int {
	switch candidate.Kind {
	case tokenLeftParen, tokenLeftBracket, tokenLeftBrace:
		return depth + 1
	case tokenRightParen, tokenRightBracket, tokenRightBrace:
		return max(depth-1, 0)
	default:
		return depth
	}
}

// identifierMatchesAny reports whether a token is one of the supplied stop keywords. A
// delimited identifier is a name, so it never matches.
//
// It is extracted to avoid inlining a nested loop in the consume helpers.
//
// Takes candidate (token) which is the token to test.
// Takes stopKeywords ([]string) which are the keywords to match against.
//
// Returns bool which is true when the token is a stop keyword.
func identifierMatchesAny(candidate token, stopKeywords []string) bool {
	return slices.ContainsFunc(stopKeywords, func(stop string) bool {
		return candidate.IsKeyword(tokenIdentifier, stop)
	})
}

// splitClickHouseParamBody splits the placeholder body `name:Type` into its two halves.
//
// Type may be empty when malformed; the catalogue resolver surfaces a diagnostic in that
// case.
//
// Takes body (string) which is the placeholder body of shape `name:Type`.
//
// Returns name (string) which is the parameter identifier.
// Returns typeName (string) which is the type tag, possibly empty.
func splitClickHouseParamBody(body string) (name string, typeName string) {
	nameSegment, typeSegment, found := strings.Cut(body, ":")
	if !found {
		return body, ""
	}
	return strings.TrimSpace(nameSegment), strings.TrimSpace(typeSegment)
}
