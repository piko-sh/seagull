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
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// messageMapLiteralOutsideValues is the syntax error at the brace of a map literal
	// outside the rows of an INSERT ... VALUES, where ClickHouse reads '{' only as a
	// placeholder.
	messageMapLiteralOutsideValues = "a map literal {key: value} is read only in the rows of INSERT ... VALUES; " +
		"elsewhere ClickHouse reads '{' as a {name:Type} placeholder, so write map(key, value, ...) instead"

	// messageUnmatchedBrace is the syntax error at a '}' that closes no map literal.
	messageUnmatchedBrace = "unexpected '}'"

	// messagePlaceholderInMapLiteral is the syntax error at a placeholder inside a map
	// literal, which ClickHouse reads as data and does not substitute.
	messagePlaceholderInMapLiteral = "ClickHouse reads a map literal {key: value} as data and substitutes no placeholder inside it: " +
		"write map(key, value, ...) around it, or bind the whole map as one {name:Map(K, V)} placeholder"
)

// analyseInsert parses an INSERT statement.
//
// The ClickHouse INSERT shape is:
//
//	INSERT INTO [db.]table [(col1, col2, ...)] {VALUES (...) | SELECT ...
//	    | FORMAT format_name data | FUNCTION ...}
//
// The analysis captures the target table, optional column list, and any SELECT body.
// FORMAT-driven inserts (CSV, JSONEachRow, etc.) are accepted but the body data is
// discarded. A RETURNING clause, which ClickHouse does not have, is recorded so the
// analyser reports it.
//
// Returns *ast.RawQueryAnalysis which is the analysed INSERT statement.
// Returns error when the statement is malformed or the parse depth limit is exceeded.
func (p *parser) analyseInsert() (*ast.RawQueryAnalysis, error) {
	if p.analysisDepth >= p.maxParseDepth {
		return nil, p.depthError()
	}
	p.analysisDepth++
	defer func() { p.analysisDepth-- }()

	analysis, err := p.analyseInsertHeader()
	if err != nil {
		return nil, err
	}
	if err := p.analyseInsertBody(analysis); err != nil {
		return nil, err
	}
	p.parseMutationTail(analysis)
	if p.analysisDepth == 1 && p.firstParameterTypeError != nil {
		return analysis, p.firstParameterTypeError
	}
	return analysis, nil
}

// analyseInsertHeader consumes the "INSERT INTO [db.]table [(cols)]" prefix and returns
// the initialised analysis with the target table and any column list populated.
//
// Returns *ast.RawQueryAnalysis which is the analysis with the header populated.
// Returns error when the prefix is malformed.
func (p *parser) analyseInsertHeader() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}

	if p.MatchKeyword(kwWith) {
		if err := p.parseCTEList(analysis); err != nil {
			return nil, err
		}
	}

	if _, err := p.ExpectKeyword("INSERT"); err != nil {
		return nil, err
	}
	if !p.MatchKeyword("INTO") {
		return nil, dialectkit.NewParseError(p.Current(), "expected INTO after INSERT")
	}
	p.MatchKeyword("TABLE")

	database, name, nameErr := p.parseDatabaseQualifiedName()
	if nameErr != nil {
		return nil, nameErr
	}

	analysis.InsertTable = name
	analysis.InsertTarget = ast.TableReference{Schema: database, Name: name, Alias: ""}
	analysis.FromTables = []ast.TableReference{{
		Schema: database,
		Name:   name,
		Alias:  "",
	}}

	if p.Current().Kind == tokenLeftParen {
		columns, err := p.parseInsertColumnList()
		if err != nil {
			return nil, err
		}
		analysis.InsertColumns = columns
	}
	return analysis, nil
}

// analyseInsertBody dispatches on the keyword after the target list (VALUES, FORMAT,
// SELECT or WITH) and populates the analysis with any nested SELECT columns or with the
// captured parameter references for VALUES and opaque-body forms.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the parsed body details.
//
// Returns error when the nested SELECT body is malformed.
func (p *parser) analyseInsertBody(analysis *ast.RawQueryAnalysis) error {
	switch {
	case p.MatchKeyword("VALUES"):
		start := p.Position
		p.insertData = start
		rowsEnd := topLevelKeywordIndex(p.Tokens, start, kwReturning)
		if rowsEnd < 0 {
			rowsEnd = len(p.Tokens)
		}
		p.markValuesPlaceholders(start, rowsEnd, analysis.InsertTable, analysis.InsertColumns)
		p.collectClickHouseParametersUntilReturning(analysis, sqlmodel.ParameterContextAssignment)
		p.recordValuesRows(analysis, start, p.Position)
	case p.MatchKeyword("FORMAT"):
		p.insertData = p.Position
		p.Advance()
		p.consumeRemainder()
	case p.IsKeyword("SELECT") || p.IsKeyword("WITH"):
		nested, err := p.analyseSelect()
		if err != nil {
			return err
		}

		analysis.ParameterReferences = append(analysis.ParameterReferences, nested.ParameterReferences...)
		analysis.InsertSelect = nested
	default:
		p.collectClickHouseParametersUntilReturning(analysis, sqlmodel.ParameterContextAssignment)
	}
	return nil
}

// parseInsertColumnList reads the parenthesised column list on "INSERT INTO t (col1,
// col2, ...) ...".
//
// Returns []string which is the parsed column names in declaration order.
// Returns error when the list is missing a delimiter or closing parenthesis.
func (p *parser) parseInsertColumnList() ([]string, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected '('")
	}
	p.Advance()
	var columns []string
	for {
		name, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		columns = append(columns, name)
		switch p.Current().Kind {
		case tokenComma:
			p.Advance()
		case tokenRightParen:
			p.Advance()
			return columns, nil
		default:
			return nil, dialectkit.NewParseError(p.Current(), "expected ',' or ')' in column list")
		}
	}
}

// misplacedBrace rejects map literals and placeholders where ClickHouse cannot parse
// them.
//
// It permits map literals only in INSERT data and refuses placeholders inside them.
//
// Returns error which is a dialectkit.ParseError at the brace or placeholder, or nil when
// there is none.
func (p *parser) misplacedBrace() error {
	depth := 0
	for index, current := range p.Tokens {
		inData := p.insertData >= 0 && index >= p.insertData
		switch {
		case current.Kind == tokenLeftBrace && !inData:
			return dialectkit.NewParseError(current, messageMapLiteralOutsideValues)
		case current.Kind == tokenRightBrace && !inData:
			return dialectkit.NewParseError(current, messageUnmatchedBrace)
		case current.Kind == tokenClickHouseParam && depth > 0:
			return dialectkit.NewParseError(current, messagePlaceholderInMapLiteral)
		}
		depth = braceDepthAfter(current, depth)
	}
	return nil
}

// braceDepthAfter returns the nesting depth of map-literal braces after a token, never
// going below zero.
//
// Takes candidate (token) which is the token under inspection.
// Takes depth (int) which is the depth before the token.
//
// Returns int which is the depth after the token.
func braceDepthAfter(candidate token, depth int) int {
	switch candidate.Kind {
	case tokenLeftBrace:
		return depth + 1
	case tokenRightBrace:
		return max(depth-1, 0)
	default:
		return depth
	}
}
