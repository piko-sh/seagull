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

	"seagull.sh/seagull/internal/dialect/ast"
)

// columnListBeforeAs reports whether the tokens from an offset hold a parenthesised list
// of names followed by AS and an opening parenthesis, as the column list of a CTE such as
// t(a, b) AS (SELECT 1, 2) does, which a scalar alias such as max(x) AS m does not.
//
// Takes offset (int) which is the offset of the opening parenthesis from the current
// token.
//
// Returns bool which is true for a column list.
func (p *parser) columnListBeforeAs(offset int) bool {
	if p.PeekAt(offset).Kind != tokenLeftParen {
		return false
	}
	for index := offset + 1; ; index += 2 {
		if p.PeekAt(index).Kind != tokenIdentifier {
			return false
		}
		switch p.PeekAt(index + 1).Kind {
		case tokenComma:
			continue
		case tokenRightParen:
			return p.PeekAt(index+columnListAsOffset).IsKeyword(tokenIdentifier, kwAs) && p.PeekAt(index+columnListBodyOffset).Kind == tokenLeftParen
		default:
			return false
		}
	}
}

// parseSingleCTE consumes one CTE entry from the WITH list. Returns more=true when the
// caller should keep iterating because a trailing comma promised another entry;
// more=false otherwise.
//
// Recognises the optional trailing `MATERIALIZED` and `NOT MATERIALIZED` qualifiers the
// parser accepts after the closing paren of the CTE body. When present the qualifier is
// captured in the definition's detail.
//
// Takes analysis (*ast.RawQueryAnalysis), the analysis to populate with the parsed CTE.
// Takes isRecursive (bool), the WITH RECURSIVE flag propagated from parseCTEList.
//
// Returns more (bool), true when another CTE follows.
// Returns error when the entry is malformed.
func (p *parser) parseSingleCTE(analysis *ast.RawQueryAnalysis, isRecursive bool) (more bool, err error) {
	listed := p.Current().Kind == tokenIdentifier && p.columnListBeforeAs(1)
	if !listed && (p.Current().Kind != tokenIdentifier || !p.Peek().IsKeyword(tokenIdentifier, kwAs) || p.PeekAt(2).Kind != tokenLeftParen) {
		return p.parseExpressionAlias(analysis)
	}
	cteName, parseErr := p.parseIdentifierOrKeyword()
	if parseErr != nil {
		return false, parseErr
	}
	var columnNames []string
	if listed {
		if columnNames, parseErr = p.parseInsertColumnList(); parseErr != nil {
			return false, parseErr
		}
	}
	p.Advance()
	body, bodyErr := p.collectParenthesised()
	if bodyErr != nil {
		return false, bodyErr
	}
	nested := p.newChildParser(body)
	nestedAnalysis, nestedErr := nested.analyseSelect()
	if nestedErr != nil {
		return false, fmt.Errorf("CTE %q: %w", cteName, nestedErr)
	}
	definition := ast.RawCTEDefinition{
		Name:             cteName,
		ColumnNames:      columnNames,
		OutputColumns:    nestedAnalysis.OutputColumns,
		FromTables:       nestedAnalysis.FromTables,
		JoinClauses:      nestedAnalysis.JoinClauses,
		CompoundBranches: nestedAnalysis.CompoundBranches,
		FromOrder:        nestedAnalysis.FromOrder,
		Query:            nestedAnalysis,

		ParameterReferences: nestedAnalysis.ParameterReferences,
		IsRecursive:         isRecursive,
		Detail:              nil,
	}
	p.captureCTEMaterializedQualifier(&definition)
	p.mergeNestedParameterReferences(analysis, nested, nestedAnalysis)

	analysis.CTEDefinitions = append(analysis.CTEDefinitions, definition)
	if p.Current().Kind == tokenComma {
		p.Advance()
		return true, nil
	}
	return false, nil
}
