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
	"fmt"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// analyseSelect parses a SELECT statement and produces the raw query analysis with output
// columns, parameters, and table references.
//
// Compound arms (UNION, INTERSECT, EXCEPT) are collected iteratively into one flat list,
// so a long chain of arms consumes a single level of the analysis depth budget.
//
// Returns *ast.RawQueryAnalysis which describes the SELECT.
// Returns error when the statement fails to parse.
func (p *parser) analyseSelect() (*ast.RawQueryAnalysis, error) {
	if p.analysisDepth >= p.maxParseDepth {
		return nil, dialectkit.DepthError(p.maxParseDepth)
	}
	p.analysisDepth++
	defer func() { p.analysisDepth-- }()

	analysis, err := p.analyseSelectArm()
	if err != nil {
		return nil, err
	}

	extents, err := p.parseSelectCompoundBranches(analysis)
	if err != nil {
		return nil, err
	}

	if p.endsInValues && p.IsKeyword(keywordORDER, keywordLIMIT) {
		return nil, dialectkit.NewParseError(p.Current(), "SQLite takes no ORDER BY or LIMIT after a VALUES list")
	}
	mark := len(p.clauseExpressions)
	p.parseSelectTrailer()
	analysis.ClauseExpressions = append(analysis.ClauseExpressions, p.takeClauseExpressions(mark)...)

	p.recordCollectedReferences(analysis)
	if err := dialectkit.SplitCompoundArms(analysis, p.gathered(), extents); err != nil {
		return nil, err
	}
	return analysis, nil
}

// analyseSelectArm parses one SELECT arm including the optional WITH clause, the select
// list, and the FROM, WHERE, GROUP BY, and HAVING clauses, stopping before any compound
// operator or trailing ORDER BY and LIMIT.
//
// Returns *ast.RawQueryAnalysis which describes the arm.
// Returns error when the arm fails to parse.
func (p *parser) analyseSelectArm() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	mark := len(p.clauseExpressions)

	if p.IsKeyword(keywordWITH) {
		cteDefinitions, err := p.parseWithClause()
		if err != nil {
			return nil, err
		}
		analysis.CTEDefinitions = cteDefinitions
	}

	p.endsInValues = p.IsKeyword(keywordVALUES)
	if p.endsInValues {
		if err := p.parseValuesRows(analysis); err != nil {
			return nil, err
		}
		analysis.ReadOnly = true
		analysis.ClauseExpressions = p.takeClauseExpressions(mark)
		return analysis, nil
	}

	if _, err := p.ExpectKeyword(keywordSELECT); err != nil {
		return nil, err
	}
	quantifierStart := p.Position
	p.MatchKeyword("DISTINCT")
	p.MatchKeyword("ALL")
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanQuantifier, p.Tokens, quantifierStart, p.Position)

	if err := p.parseProjection(analysis); err != nil {
		return nil, err
	}

	fromStart := p.Position
	if p.MatchKeyword(keywordFROM) {
		if err := p.parseSelectFromClause(analysis); err != nil {
			return nil, err
		}
	}
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanFrom, p.Tokens, fromStart, p.Position)

	if err := p.parseSelectBody(analysis); err != nil {
		return nil, err
	}
	analysis.ClauseExpressions = p.takeClauseExpressions(mark)
	return analysis, nil
}

// parseProjection parses the select list of a SELECT arm and records where it and each of
// its items are written.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the output columns.
//
// Returns error when an item fails to parse.
func (p *parser) parseProjection(analysis *ast.RawQueryAnalysis) error {
	start := p.Position
	outputColumns, err := p.parseSelectList()
	if err != nil {
		return err
	}
	analysis.OutputColumns = outputColumns
	if !p.spans.Recorded(dialectkit.SpanProjection) {
		dialectkit.RecordTokenRange(p.spans, dialectkit.SpanProjection, p.Tokens, start, p.Position)
		p.spans.RecordProjectionItems(p.itemSpans)
	}
	return nil
}

// parseWhereIfPresent parses a WHERE clause when present, records the expression it holds
// and where it is written.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the HasWhereClause flag.
func (p *parser) parseWhereIfPresent(analysis *ast.RawQueryAnalysis) {
	if !p.IsKeyword(keywordWHERE) {
		return
	}
	start := p.Position
	p.Advance()
	analysis.HasWhereClause = true
	conditionStart := p.parseCondition(ast.ClauseWhere)
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanWhere, p.Tokens, start, p.Position)
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanWhereCondition, p.Tokens, conditionStart, p.Position)
}

// parseReturningIfPresent parses a RETURNING clause when present and records where it and
// each of its items are written.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the output columns and the
// HasReturning flag.
//
// Returns error when an item fails to parse.
func (p *parser) parseReturningIfPresent(analysis *ast.RawQueryAnalysis) error {
	if !p.IsKeyword(keywordRETURNING) {
		return nil
	}
	start := p.Position
	p.Advance()
	analysis.HasReturning = true
	outputColumns, err := p.parseSelectList()
	if err != nil {
		return err
	}
	analysis.OutputColumns = outputColumns
	if !p.spans.Recorded(dialectkit.SpanReturning) {
		dialectkit.RecordTokenRange(p.spans, dialectkit.SpanReturning, p.Tokens, start, p.Position)
		p.spans.RecordReturningItems(p.itemSpans)
	}
	return nil
}

// recordCollectedReferences marks a SELECT analysis read-only and attaches the
// parameters, derived tables, predicate subqueries, and table-valued functions the parser
// has collected so far.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the collected references.
func (p *parser) recordCollectedReferences(analysis *ast.RawQueryAnalysis) {
	analysis.ReadOnly = true
	analysis.ParameterReferences = p.parameterReferences
	analysis.RawDerivedTables = p.rawDerivedTables
	analysis.PredicateSubqueries = p.predicateSubqueries
	analysis.RawTableValuedFunctions = p.rawTableValuedFunctions
}

// parseSelectBody parses FROM, WHERE, GROUP BY, and HAVING clauses into the analysis
// record.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the parsed clause data.
//
// Returns error when a clause fails to parse.
func (p *parser) parseSelectBody(analysis *ast.RawQueryAnalysis) error {
	if p.MatchKeyword(keywordFROM) {
		if err := p.parseSelectFromClause(analysis); err != nil {
			return err
		}
	}

	p.parseWhereIfPresent(analysis)

	if p.IsKeyword(keywordGROUP) {
		start := p.Position
		p.Advance()
		p.MatchKeyword("BY")
		groupByColumns := p.parseGroupByList()
		analysis.GroupByColumns = groupByColumns
		dialectkit.RecordTokenRange(p.spans, dialectkit.SpanGroupBy, p.Tokens, start, p.Position)
	}

	if p.IsKeyword(keywordHAVING) {
		start := p.Position
		p.Advance()
		p.parseCondition(ast.ClauseHaving)
		dialectkit.RecordTokenRange(p.spans, dialectkit.SpanHaving, p.Tokens, start, p.Position)
	}

	p.parseWindowClause()
	return nil
}

// parseSelectCompoundBranches parses UNION, INTERSECT, and EXCEPT branches into the
// analysis record.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives any parsed branch entries.
//
// Returns []dialectkit.ArmExtent which holds, parallel to the branches, the part of what
// the parser gathered that each holds.
// Returns error when a branch fails to parse.
func (p *parser) parseSelectCompoundBranches(analysis *ast.RawQueryAnalysis) ([]dialectkit.ArmExtent, error) {
	var extents []dialectkit.ArmExtent
	for {
		compoundOperator := p.matchCompoundOperator()
		if compoundOperator == 0 {
			break
		}
		start := p.gathered().Mark()
		branchAnalysis, branchError := p.analyseSelectArm()
		if branchError != nil {
			return nil, branchError
		}
		extents = append(extents, dialectkit.ArmExtent{Start: start, End: p.gathered().Mark()})
		analysis.CompoundBranches = append(analysis.CompoundBranches, ast.RawCompoundBranch{
			Operator: compoundOperator,
			Query:    branchAnalysis,
		})
	}
	return extents, nil
}

// gathered returns what the parser has gathered across the statement so far.
//
// Returns dialectkit.Gathered which holds the parameter references, derived tables, table
// function references and predicate subqueries.
func (p *parser) gathered() dialectkit.Gathered {
	return dialectkit.Gathered{
		Parameters:          p.parameterReferences,
		DerivedTables:       p.rawDerivedTables,
		TableFunctions:      p.rawTableValuedFunctions,
		PredicateSubqueries: p.predicateSubqueries,
	}
}

// parseSelectTrailer consumes the trailing ORDER BY and LIMIT clauses of a SELECT
// statement.
func (p *parser) parseSelectTrailer() {
	dialectkit.RecordOrderByPlaceAt(p.spans, p.Tokens, p.Position)
	if p.IsKeyword(keywordORDER) {
		start := p.Position
		p.Advance()
		p.MatchKeyword("BY")
		listStart := p.Position
		p.parseOrderByList()
		p.recordClauseExpressions(ast.ClauseOrderBy, p.reparse(listStart, p.Position, true, true))
		dialectkit.RecordTokenRange(p.spans, dialectkit.SpanOrderBy, p.Tokens, start, p.Position)
	}

	if p.IsKeyword(keywordLIMIT) {
		start := p.Position
		p.Advance()
		p.parseLimitOffset()
		dialectkit.ExtendTokenRange(p.spans, dialectkit.SpanPagination, p.Tokens, start, p.Position)
	}
}

// analyseInsert parses an INSERT or REPLACE statement and produces the raw query
// analysis.
//
// Returns *ast.RawQueryAnalysis which describes the INSERT.
// Returns error when the statement fails to parse.
func (p *parser) analyseInsert() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}

	if p.IsKeyword(keywordWITH) {
		cteDefinitions, err := p.parseWithClause()
		if err != nil {
			return nil, err
		}
		analysis.CTEDefinitions = cteDefinitions
	}

	resolution, err := p.parseInsertVerb()
	if err != nil {
		return nil, err
	}
	p.MatchKeyword("INTO")

	targetStart := p.Position
	tableName, alias := p.parseTableReference()
	analysis.FromTables = []ast.TableReference{{Name: tableName, Alias: alias, Schema: ""}}
	analysis.InsertTarget = ast.TableReference{Schema: p.writtenSchema(targetStart), Name: tableName, Alias: alias}

	var columnNames []string
	if p.Current().Kind == tokenLeftParen {
		names, err := p.parseColumnList()
		if err != nil {
			return nil, err
		}
		columnNames = names
	}

	mark := len(p.clauseExpressions)
	derivedTables, predicateSubqueries := len(p.rawDerivedTables), len(p.predicateSubqueries)
	insertSelect, valuesError := p.parseInsertValues(tableName, columnNames)
	if valuesError != nil {
		return nil, valuesError
	}
	if insertSelect != nil {
		p.leaveNestedQueriesToInsertQuery(derivedTables, predicateSubqueries)
	}
	analysis.InsertSelect = insertSelect
	analysis.ClauseExpressions = p.takeClauseExpressions(mark)

	upsert, err := p.skipConflictClauses(tableName)
	if err != nil {
		return nil, err
	}

	if err := p.parseReturningIfPresent(analysis); err != nil {
		return nil, err
	}

	analysis.ParameterReferences = p.parameterReferences
	analysis.PredicateSubqueries = p.predicateSubqueries
	analysis.RawDerivedTables = p.rawDerivedTables
	analysis.RawTableValuedFunctions = p.rawTableValuedFunctions
	analysis.InsertTable = tableName
	analysis.InsertColumns = columnNames
	analysis.ConflictAction = upsert.conflictAction(resolution)
	return analysis, nil
}

// leaveNestedQueriesToInsertQuery drops from the statement the derived tables and
// predicate subqueries an INSERT's query gathered, which that query's own analysis holds,
// so a reference in them to the query's relations resolves through the query rather than
// through the INSERT.
//
// Takes derivedTables (int) which is the number of derived tables before the query.
// Takes predicateSubqueries (int) which is the number of predicate subqueries before the
// query.
func (p *parser) leaveNestedQueriesToInsertQuery(derivedTables, predicateSubqueries int) {
	p.rawDerivedTables = dialectkit.KeepFirst(p.rawDerivedTables, derivedTables)
	p.predicateSubqueries = dialectkit.KeepFirst(p.predicateSubqueries, predicateSubqueries)
}

// skipConflictClauses consumes the ON CONFLICT clauses after an INSERT's rows, recording
// none of their expressions, which name the excluded row, and gathers what they do with a
// conflicting row.
//
// Takes tableName (string) which is the table the INSERT writes.
//
// Returns upsertClauses which describes the clauses read.
// Returns error when a clause is malformed.
func (p *parser) skipConflictClauses(tableName string) (upsertClauses, error) {
	var upsert upsertClauses
	for p.MatchKeyword(keywordON) {
		p.suppressClauses = true
		action, conflictError := p.skipOnConflict(tableName)
		p.suppressClauses = false
		if conflictError != nil {
			return upsertClauses{}, conflictError
		}
		upsert.add(action)
	}
	return upsert, nil
}

// analyseUpdate parses an UPDATE statement and produces the raw query analysis.
//
// Returns *ast.RawQueryAnalysis which describes the UPDATE.
// Returns error when the statement fails to parse.
func (p *parser) analyseUpdate() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	mark := len(p.clauseExpressions)

	if p.IsKeyword(keywordWITH) {
		cteDefinitions, err := p.parseWithClause()
		if err != nil {
			return nil, err
		}
		analysis.CTEDefinitions = cteDefinitions
	}

	if _, err := p.ExpectKeyword("UPDATE"); err != nil {
		return nil, err
	}

	if _, err := p.parseConflictResolutionIfPresent(); err != nil {
		return nil, err
	}

	tableName, alias := p.parseTableReference()
	analysis.FromTables = []ast.TableReference{{Name: tableName, Alias: alias, Schema: ""}}

	if _, err := p.ExpectKeyword(keywordSET); err != nil {
		return nil, err
	}
	p.parseSetClause(tableName)

	if p.MatchKeyword(keywordFROM) {
		fromTables, joinClauses, err := p.parseFromClause()
		if err != nil {
			return nil, err
		}
		analysis.FromTables = append(analysis.FromTables, fromTables...)
		analysis.JoinClauses = joinClauses
	}

	p.parseWhereIfPresent(analysis)

	if err := p.parseReturningIfPresent(analysis); err != nil {
		return nil, err
	}

	analysis.ClauseExpressions = p.takeClauseExpressions(mark)
	analysis.ParameterReferences = p.parameterReferences
	analysis.PredicateSubqueries = p.predicateSubqueries
	analysis.RawDerivedTables = p.rawDerivedTables
	analysis.RawTableValuedFunctions = p.rawTableValuedFunctions
	return analysis, nil
}

// analyseDelete parses a DELETE statement and produces the raw query analysis.
//
// Returns *ast.RawQueryAnalysis which describes the DELETE.
// Returns error when the statement fails to parse.
func (p *parser) analyseDelete() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	mark := len(p.clauseExpressions)

	if p.IsKeyword(keywordWITH) {
		cteDefinitions, err := p.parseWithClause()
		if err != nil {
			return nil, err
		}
		analysis.CTEDefinitions = cteDefinitions
	}

	if err := p.expectKeywordSequence("DELETE", keywordFROM); err != nil {
		return nil, err
	}

	tableName, alias := p.parseTableReference()
	analysis.FromTables = []ast.TableReference{{Name: tableName, Alias: alias, Schema: ""}}

	p.parseWhereIfPresent(analysis)

	p.parseSelectTrailer()

	if err := p.parseReturningIfPresent(analysis); err != nil {
		return nil, err
	}

	analysis.ClauseExpressions = p.takeClauseExpressions(mark)
	analysis.ParameterReferences = p.parameterReferences
	analysis.PredicateSubqueries = p.predicateSubqueries
	analysis.RawDerivedTables = p.rawDerivedTables
	analysis.RawTableValuedFunctions = p.rawTableValuedFunctions
	return analysis, nil
}

// parseValuesRows parses a VALUES query, naming its columns column1, column2 and so on
// from the first row. It parses every row and registers all parameters.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the output columns.
//
// Returns error when a row is not enclosed in parentheses.
func (p *parser) parseValuesRows(analysis *ast.RawQueryAnalysis) error {
	if _, err := p.ExpectKeyword(keywordVALUES); err != nil {
		return err
	}
	for first := true; ; first = false {
		if p.Current().Kind != tokenLeftParen {
			return dialectkit.NewParseError(p.Current(), "expected '(' to open a VALUES row")
		}
		p.Advance()
		columns := p.parseValuesRow()
		if first {
			analysis.OutputColumns = columns
		}
		if p.Current().Kind != tokenRightParen {
			return dialectkit.NewParseError(p.Current(), "expected ')' to close the VALUES row")
		}
		p.Advance()
		if p.Current().Kind != tokenComma {
			return nil
		}
		p.Advance()
	}
}

// parseValuesRow parses the expressions of one row of a VALUES list and returns the
// columns they give, named as SQLite names them.
//
// Returns []ast.RawOutputColumn which describes the row's columns.
func (p *parser) parseValuesRow() []ast.RawOutputColumn {
	var outputColumns []ast.RawOutputColumn
	var columnIndex int

	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		expression := p.parseExpression()
		columnIndex++
		outputColumns = append(outputColumns, ast.RawOutputColumn{
			Name:       fmt.Sprintf("column%d", columnIndex),
			Expression: expression,
			TableAlias: "",
			ColumnName: "",
			IsStar:     false,
		})
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	return outputColumns
}

// parseWithClause parses the WITH clause and returns one definition per common table
// expression.
//
// Returns []ast.RawCTEDefinition which holds the parsed definitions in order.
// Returns error when a CTE fails to parse.
func (p *parser) parseWithClause() ([]ast.RawCTEDefinition, error) {
	if _, err := p.ExpectKeyword(keywordWITH); err != nil {
		return nil, err
	}
	isRecursive := p.MatchKeyword("RECURSIVE")

	var definitions []ast.RawCTEDefinition

	for {
		definition, err := p.parseSingleCTE(isRecursive)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)

		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	return definitions, nil
}

// parseSingleCTE parses one common table expression definition.
//
// Takes isRecursive (bool) which is true when the WITH clause is RECURSIVE.
//
// Returns ast.RawCTEDefinition which describes the CTE.
// Returns error when the definition fails to parse.
func (p *parser) parseSingleCTE(isRecursive bool) (ast.RawCTEDefinition, error) {
	cteName, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return ast.RawCTEDefinition{}, err
	}

	columnNames, columnError := p.parseCTEColumnNames()
	if columnError != nil {
		return ast.RawCTEDefinition{}, columnError
	}

	if _, err := p.ExpectKeyword(keywordAS); err != nil {
		return ast.RawCTEDefinition{}, err
	}

	cteTokens, err := p.collectParenthesised()
	if err != nil {
		return ast.RawCTEDefinition{}, err
	}

	cteParser, cteAnalysis, analyseError := p.analyseNested(cteTokens)
	if analyseError != nil {
		return ast.RawCTEDefinition{}, analyseError
	}

	definition := ast.RawCTEDefinition{}
	definition.Name = cteName
	definition.IsRecursive = isRecursive

	definition.OutputColumns = cteAnalysis.OutputColumns
	definition.ColumnNames = columnNames
	definition.FromTables = cteAnalysis.FromTables
	definition.JoinClauses = cteAnalysis.JoinClauses
	definition.FromOrder = cteAnalysis.FromOrder
	definition.Query = cteAnalysis
	definition.ParameterReferences = cteAnalysis.ParameterReferences

	p.parameterCount = cteParser.parameterCount
	p.parameterReferences = append(p.parameterReferences, cteParser.parameterReferences...)

	return definition, nil
}

// parseCTEColumnNames parses the optional column-name list that may follow a CTE name
// before the AS keyword.
//
// Returns []string which is the column-name list, or nil when absent.
// Returns error when the list is malformed.
func (p *parser) parseCTEColumnNames() ([]string, error) {
	if p.Current().Kind != tokenLeftParen || p.IsKeyword(keywordAS) {
		return nil, nil
	}
	if p.peekForAS() {
		return nil, nil
	}
	return p.parseColumnList()
}

// peekForAS reports whether a parenthesised group followed by AS lies ahead, used to
// distinguish CTE column lists from subquery clauses.
//
// Returns bool which is true when the lookahead does not find AS after a matching close
// parenthesis.
func (p *parser) peekForAS() bool {
	saved := p.Position
	depth := 0
	for index := p.Position; index < len(p.Tokens); index++ {
		current := p.Tokens[index]
		if current.Kind == tokenLeftParen {
			depth++
			continue
		}
		if current.Kind != tokenRightParen {
			continue
		}
		depth--
		if depth != 0 {
			continue
		}
		if p.isFollowedByAS(index) {
			return false
		}
		break
	}
	p.Position = saved
	return true
}

// isFollowedByAS reports whether the token after the supplied position is the AS keyword.
//
// Takes position (int) which is the index whose successor is checked.
//
// Returns bool which is true when the next token is AS.
func (p *parser) isFollowedByAS(position int) bool {
	return position+1 < len(p.Tokens) &&
		p.Tokens[position+1].IsKeyword(tokenIdentifier, keywordAS)
}

// parseSelectList parses the comma-separated select list.
//
// When this select list is the body of an INSERT ... SELECT, its top-level projection
// placeholders are bound positionally to the INSERT target columns (the same binding the
// VALUES path applies), so an INSERT ... SELECT $1, $2 types $1/$2 from the target
// columns rather than leaving them untyped. The INSERT target is consumed and cleared up
// front so nested subqueries and compound branches parsed inside the items do not inherit
// it.
//
// Returns []ast.RawOutputColumn which is the parsed output column list.
// Returns error when an item fails to parse.
func (p *parser) parseSelectList() ([]ast.RawOutputColumn, error) {
	insertTargetTable := p.insertTargetTable
	insertTargetColumns := p.insertTargetColumns
	p.insertTargetTable = ""
	p.insertTargetColumns = nil

	var columns []ast.RawOutputColumn
	var items []ast.ItemSpan

	projectionOrdinal := 0
	for {
		referenceCountBefore := len(p.parameterReferences)
		first := p.Position
		p.aliasStart = -1
		column, err := p.parseSelectItem()
		if err != nil {
			return nil, err
		}
		columns = append(columns, column)
		expressionEnd := p.Position
		if p.aliasStart >= 0 {
			expressionEnd = p.aliasStart
		}
		items = append(items, dialectkit.TokenItemSpan(p.spans, p.Tokens, first, expressionEnd, p.Position, column.IsStar))
		if insertTargetTable != "" {
			p.bindInsertSelectProjection(referenceCountBefore, insertTargetTable, insertTargetColumns, projectionOrdinal)
		}
		projectionOrdinal++

		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	p.itemSpans = items
	return columns, nil
}

// bindInsertSelectProjection binds the placeholders registered while parsing one
// top-level INSERT ... SELECT projection item to the matching INSERT target column.
//
// Only placeholders that do not already carry a column reference are bound, so a deeper
// comparison/LIKE column reference is preserved; the assignment context and the qualified
// target column reference mirror the VALUES path so the analyser types the placeholder
// from the target column. The boundary is the parameterReferences length (references are
// appended) so out-of-order numbered placeholders are handled correctly.
//
// Takes referenceCountBefore (int) which is len(parameterReferences) before the item was
// parsed.
// Takes tableName (string) which is the INSERT target table name (the column ref alias).
// Takes columnNames ([]string) which are the INSERT target column names in positional
// order.
// Takes projectionOrdinal (int) which is the 0-based projection slot of the item.
func (p *parser) bindInsertSelectProjection(referenceCountBefore int, tableName string, columnNames []string, projectionOrdinal int) {
	columnReference := columnReferenceForIndex(tableName, columnNames, projectionOrdinal)
	if columnReference == nil {
		return
	}
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		if p.parameterReferences[index].ColumnReference == nil {
			p.parameterReferences[index].Context = sqlmodel.ParameterContextAssignment
			p.parameterReferences[index].ColumnReference = columnReference
		}
	}
}

// parseSelectItem parses a single select-list item: a star, a qualified star, or an
// expression, including a plain or qualified column, with an optional alias.
//
// Returns ast.RawOutputColumn which describes the parsed item.
// Returns error when the item fails to parse.
func (p *parser) parseSelectItem() (ast.RawOutputColumn, error) {
	if p.Current().Kind == tokenStar {
		p.Advance()
		return ast.RawOutputColumn{IsStar: true, Expression: nil, Name: "", TableAlias: "", ColumnName: ""}, nil
	}

	if p.Current().Kind == tokenIdentifier && p.Peek().Kind == tokenDot && p.PeekAt(2).Kind == tokenStar {
		tableAlias := p.Advance().Value
		p.Advance()
		p.Advance()
		return ast.RawOutputColumn{IsStar: true, TableAlias: tableAlias, Expression: nil, Name: "", ColumnName: ""}, nil
	}

	expression := p.parseExpression()
	column := ast.RawOutputColumn{}
	p.expressionToOutputColumn(expression, &column)

	p.aliasStart = p.Position
	if p.MatchKeyword(keywordAS) {
		alias, aliasError := p.parseIdentifierOrKeyword()
		if aliasError != nil {
			return ast.RawOutputColumn{}, aliasError
		}
		column.Name = alias
	} else if p.Current().Kind == tokenIdentifier && !p.isSelectTerminator() {
		column.Name = p.Advance().Value
	}

	return column, nil
}

// expressionToOutputColumn copies the relevant fields from an expression into an
// output-column record.
//
// Takes expression (ast.Expression) which is the parsed select expression.
// Takes column (*ast.RawOutputColumn) which receives the derived column metadata.
func (*parser) expressionToOutputColumn(expression ast.Expression, column *ast.RawOutputColumn) {
	switch expr := expression.(type) {
	case *ast.ColumnRefExpression:
		column.Name = expr.ColumnName
		column.TableAlias = expr.TableAlias
		column.ColumnName = expr.ColumnName
	case *ast.FunctionCallExpression:
		column.Name = expr.FunctionName
		column.Expression = expression
	default:
		column.Expression = expression
	}
}

// isSelectTerminator reports whether the current token marks the end of the select list.
//
// Returns bool which is true when the token starts a clause that follows the select list.
func (p *parser) isSelectTerminator() bool {
	return p.IsKeyword(keywordFROM, keywordWHERE, keywordGROUP, keywordHAVING, keywordORDER, keywordLIMIT,
		keywordUNION, keywordINTERSECT, keywordEXCEPT, keywordON, keywordRETURNING)
}

// isParameterToken reports whether a token kind is one of the placeholder forms.
//
// Takes kind (tokenKind) which is the token kind to test.
//
// Returns bool which is true for question marks, numbered, and named parameters.
func isParameterToken(kind tokenKind) bool {
	return kind == tokenQuestionMark || kind == tokenNumberedParameter || kind == tokenNamedParameter
}
