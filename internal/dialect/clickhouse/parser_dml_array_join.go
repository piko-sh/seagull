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

// parseFromArrayJoinChain handles the ARRAY JOIN and LEFT ARRAY JOIN chain that may
// appear after the trailing modifiers.
//
// Each match runs through parseArrayJoinList to register the joined arrays. The cursor is
// saved before consuming LEFT and restored when the expected ARRAY does not follow.
// Without the save and restore the LEFT keyword can be eaten by matchKeyword and then the
// missing ARRAY check would return, leaving the parser positioned after a stray LEFT that
// the surrounding clause never sees.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns error when an ARRAY JOIN entry is malformed.
func (p *parser) parseFromArrayJoinChain(analysis *ast.RawQueryAnalysis) error {
	for p.IsKeyword(kwArray, kwLeft) {
		savedPosition := p.Position
		isLeft := p.MatchKeyword(kwLeft)
		if !p.MatchKeyword(kwArray) {
			p.Position = savedPosition
			return nil
		}
		if !p.MatchKeyword(kwJoin) {
			return dialectkit.NewParseError(p.Current(), "expected JOIN after ARRAY")
		}
		if err := p.parseArrayJoinList(analysis, isLeft); err != nil {
			return err
		}
	}
	return nil
}

// parseArrayJoinList reads one or more comma-separated ARRAY JOIN entries of shape
// `<source> [AS <alias>]`.
//
// The source may be a bare column reference, a literal array (`[1, 2, 3]`), or an
// arbitrary array-producing expression (`arrayMap(f, src)`). The bare column form treats
// the source name as both the source and the alias because ClickHouse shadows the
// original Array column under the same name within the SELECT scope. Each entry is
// appended to analysis.ArrayJoinClauses so the analyser can register the element column
// under its alias.
//
// Bare column references populate SourceColumn. Expression sources, meaning anything not
// a simple identifier followed by AS, a comma, or a clause keyword, populate
// SourceExpression with the captured textual body. Parameter placeholders inside
// expression bodies are tracked so `{name:Type}` references inside the source still
// register against the analysis.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
// Takes isLeft (bool) which is the LEFT ARRAY JOIN flag for each entry.
//
// Returns error when an ARRAY JOIN entry is malformed.
func (p *parser) parseArrayJoinList(analysis *ast.RawQueryAnalysis, isLeft bool) error {
	for {
		if p.AtEnd() {
			return nil
		}
		clause, err := p.parseSingleArrayJoinEntry(analysis, isLeft)
		if err != nil {
			return err
		}
		analysis.ArrayJoinClauses = append(analysis.ArrayJoinClauses, clause)
		if p.Current().Kind == tokenComma {
			p.Advance()
			continue
		}
		return nil
	}
}

// parseSingleArrayJoinEntry parses an ARRAY JOIN source and its optional alias.
//
// It records both the source expression and any bare-column reference needed for
// unfolding.
//
// Takes analysis (*ast.RawQueryAnalysis), the analysis to register any captured parameter
// references against.
// Takes isLeft (bool), the LEFT ARRAY JOIN flag propagated from parseFromArrayJoinChain.
//
// Returns the populated RawArrayJoinClause and any parse error.
func (p *parser) parseSingleArrayJoinEntry(
	analysis *ast.RawQueryAnalysis, isLeft bool,
) (ast.RawArrayJoinClause, error) {
	if source, alias, isBare := p.tryParseBareArrayJoinSource(analysis); isBare {
		clause := ast.RawArrayJoinClause{
			Source:           source,
			Alias:            alias,
			SourceColumn:     source.ColumnName,
			IsLeft:           isLeft,
			SourceExpression: "",
			ElementPrefix:    "",
		}
		if alias == source.ColumnName {
			clause.ElementPrefix = source.ColumnName + nestedFieldSeparator
		}
		return clause, nil
	}
	start := p.Position
	expression := p.captureExpressionTrackingParams(
		analysis, sqlmodel.ParameterContextComparison, stopArrayJoinSource...,
	)
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return ast.RawArrayJoinClause{}, dialectkit.NewParseError(p.Current(), "expected ARRAY JOIN source")
	}
	var source ast.Expression
	if parsed := p.reparseExpression(start, p.Position); len(parsed) == 1 {
		source = parsed[0]
	}
	alias := expression
	if p.MatchKeyword(kwAs) {
		explicit, aliasErr := p.parseIdentifierOrKeyword()
		if aliasErr != nil {
			return ast.RawArrayJoinClause{}, aliasErr
		}
		alias = explicit
	}
	return ast.RawArrayJoinClause{
		Source:           source,
		Alias:            alias,
		SourceExpression: expression,
		IsLeft:           isLeft,
		SourceColumn:     "",
		ElementPrefix:    "",
	}, nil
}

// tryParseBareArrayJoinSource recognises a column reference with an optional alias.
//
// A leading FROM relation name qualifies the column; other dotted names remain part of
// the column name.
//
// On a miss the cursor is left untouched so the caller can fall through to the
// expression-source path.
//
// Takes analysis (*ast.RawQueryAnalysis) which holds the tables of the FROM clause read
// so far.
//
// Returns source (*ast.ColumnRefExpression) which is the column, placed where it is
// written.
// Returns alias (string) which is the column name when no AS alias appears, or the
// explicit alias otherwise.
// Returns matched (bool) which is true when a bare-column source was consumed.
func (p *parser) tryParseBareArrayJoinSource(analysis *ast.RawQueryAnalysis) (source *ast.ColumnRefExpression, alias string, matched bool) {
	if p.Current().Kind != tokenIdentifier || isTopClauseKeyword(p.Current()) {
		return nil, "", false
	}
	saved := p.Position
	names := []string{p.Advance().Value}
	for p.Current().Kind == tokenDot && p.Peek().Kind == tokenIdentifier {
		p.Advance()
		names = append(names, p.Advance().Value)
	}
	source = arrayJoinColumnReference(analysis, names)
	source.Span = p.absoluteSpan(saved, p.Position)
	switch {
	case p.MatchKeyword(kwAs):
		explicit, err := p.parseIdentifierOrKeyword()
		if err != nil {
			p.Position = saved
			return nil, "", false
		}
		return source, explicit, true
	case p.Current().Kind == tokenComma, p.AtEnd(), p.Current().Kind == tokenIdentifier && isTopClauseKeyword(p.Current()):
		return source, source.ColumnName, true
	}
	p.Position = saved
	return nil, "", false
}

// arrayJoinColumnReference builds the column reference a dotted ARRAY JOIN source names:
// qualified by its first name when that names a table of the FROM clause, and otherwise a
// column whose name is every name joined by dots.
//
// Takes analysis (*ast.RawQueryAnalysis) which holds the tables of the FROM clause.
// Takes names ([]string) which are the source's names, at least one.
//
// Returns *ast.ColumnRefExpression which is the reference.
func arrayJoinColumnReference(analysis *ast.RawQueryAnalysis, names []string) *ast.ColumnRefExpression {
	reference := &ast.ColumnRefExpression{TableAlias: "", ColumnName: strings.Join(names, nestedFieldSeparator), Span: ast.Span{}}
	if len(names) < 2 {
		return reference
	}
	for _, table := range analysis.FromTables {
		if names[0] == table.Name || names[0] == table.Alias {
			reference.TableAlias = names[0]
			reference.ColumnName = strings.Join(names[1:], nestedFieldSeparator)
			return reference
		}
	}
	return reference
}
