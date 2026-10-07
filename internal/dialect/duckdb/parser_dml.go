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

package duckdb

import (
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// analyseSelect parses a SELECT query and returns its analysis.
//
// A chain of UNION, INTERSECT, and EXCEPT arms is parsed iteratively into the flat
// CompoundBranches list, so a long chain costs one level of analysis depth rather than
// one per arm. Each arm takes the derived tables, table functions, predicate subqueries
// and parameters gathered while it was parsed; the query keeps every parameter.
//
// Returns *ast.RawQueryAnalysis which captures the parsed select structure including
// output columns, joins and parameters.
// Returns error when parsing the CTE list, FROM clause or compound branches fails.
func (p *parser) analyseSelect() (*ast.RawQueryAnalysis, error) {
	if p.analysisDepth >= p.maxParseDepth {
		depthErr := dialectkit.DepthError(p.maxParseDepth)
		p.recordSyntaxError(depthErr)
		return nil, depthErr
	}
	p.analysisDepth++
	defer func() { p.analysisDepth-- }()

	enclosingPivotArm := p.pivotArm
	p.pivotArm = false
	defer func() { p.pivotArm = enclosingPivotArm }()

	analysis, err := p.analyseSelectArm()
	if err != nil {
		return nil, err
	}

	extents, err := p.parseCompoundBranches(analysis)
	if err != nil {
		return nil, err
	}

	mark := len(p.clauseExpressions)
	p.recordOrderByPlace()
	p.parseOrderByIfPresent()
	p.parseLimitOffsetIfPresent()
	p.rejectLockingClause()
	if p.pivotArm {
		p.dropClauseExpressions(mark)
	}
	analysis.ClauseExpressions = append(analysis.ClauseExpressions, p.takeClauseExpressions(mark)...)

	p.finaliseSelectAnalysis(analysis)
	if err := dialectkit.SplitCompoundArms(analysis, p.gathered(), extents); err != nil {
		p.recordSyntaxError(err)
		return nil, err
	}
	return analysis, nil
}

// analyseSelectArm parses one SELECT arm, from its optional WITH list to its QUALIFY
// clause, stopping before any set operator, ORDER BY, or LIMIT that applies to the whole
// query.
//
// Returns *ast.RawQueryAnalysis which captures the arm's structure, not yet finalised.
// Returns error when the arm is malformed.
func (p *parser) analyseSelectArm() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	mark := len(p.clauseExpressions)

	if err := p.parseCTEListIfPresent(analysis); err != nil {
		return nil, err
	}

	if p.Current().Kind == tokenLeftParen {
		return p.analyseParenthesisedArm(analysis)
	}
	if p.IsKeyword(keywordVALUES) {
		if err := p.parseValuesRows(analysis); err != nil {
			p.recordSyntaxError(err)
			return nil, err
		}
		analysis.ReadOnly = true
		analysis.ClauseExpressions = p.takeClauseExpressions(mark)
		return analysis, nil
	}
	if p.IsKeyword(keywordPIVOT, keywordPIVOTWIDER, keywordUNPIVOT, keywordPIVOTLONGER) {
		defer p.dropClauseExpressions(mark)
		p.pivotArm = true
		return p.analysePivotStatement(analysis)
	}

	switch {
	case p.IsKeyword(keywordFROM):
		if err := p.parseFromFirstSelect(analysis); err != nil {
			return nil, err
		}
	case p.IsKeyword(keywordTABLE):
		if err := p.parseTableQuery(analysis); err != nil {
			return nil, err
		}
	default:
		if err := p.parseSelectListAndFrom(analysis); err != nil {
			return nil, err
		}
	}

	if err := p.parseArmFilters(analysis); err != nil {
		return nil, err
	}

	p.skipSampleClause()

	analysis.ClauseExpressions = p.takeClauseExpressions(mark)
	return analysis, nil
}

// parseArmFilters parses the WHERE, GROUP BY, HAVING, WINDOW and QUALIFY clauses of a
// SELECT arm, each when present, recording where each is written and the expressions it
// holds.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the HasWhereClause flag and the
// grouped columns.
//
// Returns error when BY does not follow GROUP.
func (p *parser) parseArmFilters(analysis *ast.RawQueryAnalysis) error {
	if p.IsKeyword(keywordWHERE) {
		start := p.Position
		p.Advance()
		analysis.HasWhereClause = true
		conditionStart := p.parseCondition(ast.ClauseWhere, whereExpressionTerminatorKeywords)
		p.recordSpan(dialectkit.SpanWhere, start, p.Position)
		p.recordSpan(dialectkit.SpanWhereCondition, conditionStart, p.Position)
	}

	if p.IsKeyword(keywordGROUP) {
		start := p.Position
		p.Advance()
		if _, err := p.ExpectKeyword(keywordBY); err != nil {
			p.recordSyntaxError(err)
			return err
		}
		analysis.GroupByColumns, analysis.GroupingSets = p.parseGroupByClause(analysis.OutputColumns)
		p.recordSpan(dialectkit.SpanGroupBy, start, p.Position)
	}

	if p.IsKeyword(keywordHAVING) {
		start := p.Position
		p.Advance()
		p.parseCondition(ast.ClauseHaving, whereExpressionTerminatorKeywords)
		p.recordSpan(dialectkit.SpanHaving, start, p.Position)
	}

	p.parseWindowClauseIfPresent()

	if p.IsKeyword(keywordQUALIFY) {
		start := p.Position
		p.Advance()
		p.parseCondition(ast.ClauseRowShaping, whereExpressionTerminatorKeywords)
		p.recordRowShaping(start, p.Position)
	}
	return nil
}

// parseSelectListAndFrom parses SELECT [DISTINCT [ON (...)] | ALL] list [FROM ...].
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the output columns and the FROM
// clause's tables and joins.
//
// Returns error when the SELECT keyword is missing or a clause fails to parse.
func (p *parser) parseSelectListAndFrom(analysis *ast.RawQueryAnalysis) error {
	if _, err := p.ExpectKeyword(keywordSELECT); err != nil {
		return err
	}
	if err := p.parseSelectList(analysis, true); err != nil {
		return err
	}
	fromStart := p.Position
	if err := p.parseFromClauseIfPresent(analysis); err != nil {
		return err
	}
	p.recordSpan(dialectkit.SpanFrom, fromStart, p.Position)
	return nil
}

// parseSelectList parses the projection list after SELECT, with its optional DISTINCT,
// DISTINCT ON (...) or ALL.
//
// The FROM-first form writes its select list after its FROM clause, where a code
// generator cannot cut the statement the way it cuts SELECT ... FROM, so its select list
// is not recorded.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the output columns.
// Takes record (bool) which is true when the select list's spans are recorded.
//
// Returns error when an output column fails to parse.
func (p *parser) parseSelectList(analysis *ast.RawQueryAnalysis, record bool) error {
	quantifierStart := p.Position
	if p.MatchKeyword("DISTINCT") {
		p.parseDistinctOn()
	}
	p.MatchKeyword(keywordALL)
	if record {
		p.recordSpan(dialectkit.SpanQuantifier, quantifierStart, p.Position)
	}

	start := p.Position
	outputColumns, err := p.parseOutputColumns(true)
	if err != nil {
		return err
	}
	analysis.OutputColumns = outputColumns
	end := p.Position
	if end > start && p.Tokens[end-1].Kind == tokenComma {
		end--
	}
	if record && !p.spans.Recorded(dialectkit.SpanProjection) {
		p.recordSpan(dialectkit.SpanProjection, start, end)
		p.spans.RecordProjectionItems(p.itemSpans)
	}
	return nil
}

// parseFromFirstSelect parses DuckDB's FROM-first form, FROM ... [SELECT list], whose
// projection is every column when the SELECT is left out.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the FROM clause and the
// projection.
//
// Returns error when a clause fails to parse.
func (p *parser) parseFromFirstSelect(analysis *ast.RawQueryAnalysis) error {
	if err := p.parseFromClauseIfPresent(analysis); err != nil {
		return err
	}
	if p.MatchKeyword(keywordSELECT) {
		return p.parseSelectList(analysis, false)
	}
	analysis.OutputColumns = []ast.RawOutputColumn{{IsStar: true, Expression: nil, Name: "", TableAlias: "", ColumnName: ""}}
	return nil
}

// parseTableQuery parses TABLE name, which selects every column of one table or view, as
// FROM name does.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the table and the projection.
//
// Returns error when the table reference is malformed or names more than one relation.
func (p *parser) parseTableQuery(analysis *ast.RawQueryAnalysis) error {
	keyword := p.Advance()
	fromTables, joinClauses, fromOrder, err := p.parseFromClause()
	if err != nil {
		return err
	}
	if len(fromTables)+len(joinClauses) != 1 {
		tableError := dialectkit.NewParseError(keyword, "TABLE names exactly one table or view")
		p.recordSyntaxError(tableError)
		return tableError
	}
	analysis.FromTables, analysis.JoinClauses, analysis.FromOrder = fromTables, joinClauses, fromOrder
	analysis.OutputColumns = []ast.RawOutputColumn{{IsStar: true, Expression: nil, Name: "", TableAlias: "", ColumnName: ""}}
	return nil
}

// analyseParenthesisedArm parses a set-operation arm written in parentheses, such as each
// arm of (SELECT ...) UNION (SELECT ...), as a nested query whose analysis is the arm's.
// The nested query's FROM-clause state joins this parser's, as the arm belongs to this
// statement.
//
// Takes analysis (*ast.RawQueryAnalysis) which holds any CTE list parsed before the
// parenthesis.
//
// Returns *ast.RawQueryAnalysis which is the arm's analysis.
// Returns error when the parenthesised query is malformed.
func (p *parser) analyseParenthesisedArm(analysis *ast.RawQueryAnalysis) (*ast.RawQueryAnalysis, error) {
	innerTokens, err := p.collectParenthesised()
	if err != nil {
		p.recordSyntaxError(err)
		return nil, err
	}
	child, inner, err := p.analyseNestedQuery(innerTokens)
	if err != nil {
		return nil, err
	}
	p.rawDerivedTables = append(p.rawDerivedTables, child.rawDerivedTables...)
	p.rawTableValuedFunctions = append(p.rawTableValuedFunctions, child.rawTableValuedFunctions...)
	p.predicateSubqueries = append(p.predicateSubqueries, child.predicateSubqueries...)
	p.hasDataModifyingCTE = p.hasDataModifyingCTE || child.hasDataModifyingCTE
	if len(analysis.CTEDefinitions) > 0 {
		inner.CTEDefinitions = append(analysis.CTEDefinitions, inner.CTEDefinitions...)
	}
	return inner, nil
}

// parseWindowClauseIfPresent consumes a WINDOW name AS (spec), ... clause naming the
// windows the projection refers to, registering any parameter its specifications hold.
func (p *parser) parseWindowClauseIfPresent() {
	if !p.MatchKeyword(keywordWINDOW) {
		return
	}
	for {
		if p.Current().Kind != tokenIdentifier {
			p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected a window name"))
			return
		}
		p.Advance()
		if _, err := p.ExpectKeyword(keywordAS); err != nil {
			p.recordSyntaxError(err)
			return
		}
		if p.Current().Kind != tokenLeftParen {
			p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected '(' to open the window specification"))
			return
		}
		p.Advance()
		p.parseWindowSpec()
		if p.Current().Kind != tokenRightParen {
			p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ')' to close the window specification"))
			return
		}
		p.Advance()
		if p.Current().Kind != tokenComma {
			return
		}
		p.Advance()
	}
}

// skipSampleClause consumes a USING SAMPLE clause, such as USING SAMPLE 10% or USING
// SAMPLE reservoir(50 ROWS) REPEATABLE (42), which samples the rows the query reads.
func (p *parser) skipSampleClause() {
	if !p.IsKeyword(keywordUSING) || !p.Peek().IsKeyword(tokenIdentifier, keywordSAMPLE) {
		return
	}
	p.Advance()
	p.Advance()
	p.skipSampleSpecification()
}

// finaliseSelectAnalysis records the read-only flag and copies the parser-side state into
// a SELECT analysis.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to finalise.
func (p *parser) finaliseSelectAnalysis(analysis *ast.RawQueryAnalysis) {
	analysis.ReadOnly = !p.hasDataModifyingCTE
	p.finaliseAnalysis(analysis)
}

// parseCTEListIfPresent parses a leading WITH clause when present.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the parsed CTE definitions on
// success.
//
// Returns error when parsing the CTE list fails.
func (p *parser) parseCTEListIfPresent(analysis *ast.RawQueryAnalysis) error {
	if !p.IsKeyword(keywordWITH) {
		return nil
	}
	cteDefinitions, err := p.parseCTEList()
	if err != nil {
		return err
	}
	analysis.CTEDefinitions = cteDefinitions
	return nil
}

// parseFromClauseIfPresent parses a FROM clause when the keyword is present.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the parsed from tables and join
// clauses.
//
// Returns error when parsing the FROM clause fails.
func (p *parser) parseFromClauseIfPresent(analysis *ast.RawQueryAnalysis) error {
	if !p.MatchKeyword(keywordFROM) {
		return nil
	}
	fromTables, joinClauses, fromOrder, err := p.parseFromClause()
	if err != nil {
		return err
	}
	analysis.FromTables = fromTables
	analysis.JoinClauses = joinClauses
	analysis.FromOrder = fromOrder
	return nil
}

// parseCompoundBranches parses trailing UNION, INTERSECT and EXCEPT branches.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives each parsed compound branch.
//
// Returns []dialectkit.ArmExtent which holds, parallel to the branches, the part of what
// the parser gathered that each holds.
// Returns error when parsing a branch query fails.
func (p *parser) parseCompoundBranches(analysis *ast.RawQueryAnalysis) ([]dialectkit.ArmExtent, error) {
	var extents []dialectkit.ArmExtent
	for {
		compoundOperator := p.parseCompoundQuery()
		if compoundOperator == 0 {
			break
		}
		if p.MatchKeyword(keywordBY) {
			if _, err := p.ExpectKeyword("NAME"); err != nil {
				p.recordSyntaxError(err)
				return nil, err
			}
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

// parseOrderByIfPresent parses an ORDER BY clause when present.
func (p *parser) parseOrderByIfPresent() {
	if !p.IsKeyword(keywordORDER) {
		return
	}
	start := p.Position
	p.Advance()
	p.requireKeywordOrRecord(keywordBY)
	listStart := p.Position
	p.parseOrderByList()
	p.recordOrderBy(listStart, p.Position)
	p.recordSpan(dialectkit.SpanOrderBy, start, p.Position)
}

// parseLimitOffsetIfPresent parses LIMIT, FETCH or OFFSET when present and records where
// it is written.
func (p *parser) parseLimitOffsetIfPresent() {
	if p.IsKeyword(keywordLIMIT) || p.IsKeyword(keywordFETCH) || p.IsKeyword(keywordOFFSET) {
		start := p.Position
		p.parseLimitOffset()
		p.extendSpan(dialectkit.SpanPagination, start, p.Position)
	}
}

// finaliseAnalysis copies parser-side state into the analysis result.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives parameter references and raw
// derived tables collected during parsing.
func (p *parser) finaliseAnalysis(analysis *ast.RawQueryAnalysis) {
	analysis.ParameterReferences = p.parameterReferences
	analysis.RawDerivedTables = p.rawDerivedTables
	analysis.PredicateSubqueries = p.predicateSubqueries
	analysis.RawTableValuedFunctions = p.rawTableValuedFunctions
	analysis.ArgumentNames = p.argumentNames
}

// analyseInsert parses an INSERT statement and returns its analysis.
//
// Returns *ast.RawQueryAnalysis which captures the target table, columns, values source
// and any RETURNING clause.
// Returns error when parsing the CTE list, column list or RETURNING clause fails.
func (p *parser) analyseInsert() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}

	if err := p.parseCTEListIfPresent(analysis); err != nil {
		return nil, err
	}

	if _, err := p.ExpectKeyword("INSERT"); err != nil {
		return nil, err
	}
	analysis.ConflictAction = p.parseInsertOrAction()
	p.MatchKeyword("INTO")

	schema, tableName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nil, nameError
	}
	alias := p.parseOptionalAlias()
	analysis.FromTables = []ast.TableReference{{Schema: schema, Name: tableName, Alias: alias}}
	analysis.InsertTarget = ast.TableReference{Schema: schema, Name: tableName, Alias: alias}

	if p.MatchKeyword(keywordBY) {
		if _, err := p.ExpectKeyword("NAME", "POSITION"); err != nil {
			p.recordSyntaxError(err)
			return nil, err
		}
	}

	columnNames, err := p.parseInsertColumnList()
	if err != nil {
		return nil, err
	}

	p.skipOverridingClause()
	if err := p.parseInsertedRows(analysis, tableName, columnNames); err != nil {
		return nil, err
	}

	if p.MatchKeyword(keywordON) {
		p.suppressClauses = true
		action, conflictErr := p.parseOnConflict(tableName)
		p.suppressClauses = false
		if conflictErr != nil {
			return nil, conflictErr
		}
		analysis.ConflictAction = action
	}

	if err := p.parseReturningIfPresent(analysis); err != nil {
		return nil, err
	}

	p.finaliseAnalysis(analysis)
	analysis.InsertTable = tableName
	analysis.InsertColumns = columnNames
	return analysis, nil
}

// parseInsertOrAction reads the OR REPLACE or OR IGNORE that may follow INSERT, DuckDB's
// shorthand for ON CONFLICT DO UPDATE and ON CONFLICT DO NOTHING.
//
// Returns ast.ConflictAction which is DoUpdate for OR REPLACE, DoNothing for OR IGNORE,
// and None when neither is written.
func (p *parser) parseInsertOrAction() ast.ConflictAction {
	if !p.MatchKeyword("OR") {
		return ast.ConflictActionNone
	}
	if p.MatchKeyword("REPLACE") {
		return ast.ConflictActionDoUpdate
	}
	if p.MatchKeyword("IGNORE") {
		return ast.ConflictActionDoNothing
	}
	return ast.ConflictActionNone
}

// parseInsertedRows parses the rows an INSERT inserts, a VALUES list or a query, into the
// analysis, keeping the values the VALUES rows assign as its clause expressions.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the rows.
// Takes tableName (string) which is the target table.
// Takes columnNames ([]string) which is the target column list.
//
// Returns error when the rows cannot be parsed.
func (p *parser) parseInsertedRows(analysis *ast.RawQueryAnalysis, tableName string, columnNames []string) error {
	mark := len(p.clauseExpressions)
	derivedTables, predicateSubqueries := len(p.rawDerivedTables), len(p.predicateSubqueries)
	insertSelect, err := p.parseInsertValues(tableName, columnNames)
	if err != nil {
		return err
	}
	if insertSelect != nil {
		p.leaveNestedQueriesToInsertQuery(derivedTables, predicateSubqueries)
	}
	analysis.InsertSelect = insertSelect
	analysis.ClauseExpressions = p.takeClauseExpressions(mark)
	return nil
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

// parseOptionalAlias parses an optional AS alias after a table reference.
//
// Returns string which is the alias name, or empty when absent.
func (p *parser) parseOptionalAlias() string {
	if p.MatchKeyword(keywordAS) {
		if p.Current().Kind == tokenIdentifier {
			return p.Advance().Value
		}
	}
	return ""
}

// parseInsertColumnList parses an optional (col1, col2, ...) column list.
//
// Returns []string which is the parsed column name list, or nil when no list is present.
// Returns error when parsing the column list fails.
func (p *parser) parseInsertColumnList() ([]string, error) {
	if p.Current().Kind == tokenLeftParen && !p.IsKeyword(keywordSELECT) && !p.IsKeyword(keywordVALUES) {
		return p.parseColumnList()
	}
	return nil, nil
}

// skipOverridingClause consumes an OVERRIDING SYSTEM/USER VALUE clause.
func (p *parser) skipOverridingClause() {
	if p.MatchKeyword("OVERRIDING") {
		p.MatchKeyword("SYSTEM")
		p.MatchKeyword("USER")
		p.MatchKeyword("VALUE")
	}
}

// parseInsertValues parses the VALUES, DEFAULT VALUES or query source.
//
// Takes tableName (string) which is the insert target table name.
// Takes columnNames ([]string) which lists the target columns when given.
//
// Returns *ast.RawQueryAnalysis which is the analysed SELECT body of an INSERT ... SELECT
// (nil for VALUES/DEFAULT VALUES/parenthesised sources).
// Returns error when the SELECT body fails to parse.
func (p *parser) parseInsertValues(tableName string, columnNames []string) (*ast.RawQueryAnalysis, error) {
	switch {
	case p.MatchKeyword(keywordVALUES):
		p.parseValuesClause(tableName, columnNames)
	case p.MatchKeyword(keywordDEFAULT):
		p.MatchKeyword(keywordVALUES)
	case p.IsKeyword(keywordSELECT) || p.IsKeyword(keywordWITH):

		p.insertProjectionTable = tableName
		p.insertProjectionColumns = columnNames
		p.insertProjectionIndex = 0
		analysis, analyseErr := p.analyseSelect()
		p.insertProjectionTable = ""
		p.insertProjectionColumns = nil
		return analysis, analyseErr
	case p.Current().Kind == tokenLeftParen:
		p.parseInsertSource()
	}
	return nil, nil
}

// parseReturningIfPresent parses a RETURNING clause when present.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the RETURNING output columns and
// the HasReturning flag.
//
// Returns error when parsing the RETURNING clause fails.
func (p *parser) parseReturningIfPresent(analysis *ast.RawQueryAnalysis) error {
	if !p.IsKeyword(keywordRETURNING) {
		return nil
	}
	start := p.Position
	p.Advance()
	analysis.HasReturning = true
	outputColumns, err := p.parseReturningClause()
	if err != nil {
		return err
	}
	analysis.OutputColumns = outputColumns
	if !p.spans.Recorded(dialectkit.SpanReturning) {
		p.recordSpan(dialectkit.SpanReturning, start, p.Position)
		p.spans.RecordReturningItems(p.itemSpans)
	}
	return nil
}

// analyseUpdate parses an UPDATE statement and returns its analysis.
//
// Returns *ast.RawQueryAnalysis which captures the target table, SET clause, optional
// FROM and RETURNING clauses.
// Returns error when parsing the CTE list, FROM clause or RETURNING clause fails.
func (p *parser) analyseUpdate() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	mark := len(p.clauseExpressions)

	if err := p.parseCTEListIfPresent(analysis); err != nil {
		return nil, err
	}

	if _, err := p.ExpectKeyword("UPDATE"); err != nil {
		return nil, err
	}
	p.MatchKeyword("ONLY")

	schema, tableName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nil, nameError
	}
	alias := p.parseUpdateAlias()
	analysis.FromTables = []ast.TableReference{{Schema: schema, Name: tableName, Alias: alias}}

	if _, err := p.ExpectKeyword(keywordSET); err != nil {
		return nil, err
	}
	p.parseSetClause(tableName)

	if err := p.parseAdditionalFromClause(analysis); err != nil {
		return nil, err
	}

	p.parseWhereOrCurrentOf(analysis)

	if err := p.parseReturningIfPresent(analysis); err != nil {
		return nil, err
	}

	p.finaliseAnalysis(analysis)
	analysis.ClauseExpressions = p.takeClauseExpressions(mark)
	return analysis, nil
}

// parseUpdateAlias parses the optional alias following an UPDATE target.
//
// Returns string which is the alias name, or empty when absent.
func (p *parser) parseUpdateAlias() string {
	if p.MatchKeyword(keywordAS) {
		if p.Current().Kind == tokenIdentifier {
			return p.Advance().Value
		}
		return ""
	}
	if p.Current().Kind == tokenIdentifier && !p.IsKeyword(keywordSET) {
		return p.Advance().Value
	}
	return ""
}

// parseAdditionalFromClause parses a UPDATE/DELETE auxiliary FROM clause.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the additional from tables and
// join clauses.
//
// Returns error when parsing the FROM clause fails.
func (p *parser) parseAdditionalFromClause(analysis *ast.RawQueryAnalysis) error {
	if !p.MatchKeyword(keywordFROM) {
		return nil
	}
	fromTables, joinClauses, _, err := p.parseFromClause()
	if err != nil {
		return err
	}
	analysis.FromTables = append(analysis.FromTables, fromTables...)
	analysis.JoinClauses = joinClauses
	return nil
}

// parseWhereOrCurrentOf consumes an optional WHERE clause, accepting a predicate or the
// WHERE CURRENT OF <cursor> form.
//
// It sets analysis.HasWhereClause when a WHERE keyword is matched and records the clause
// span for source rewriting.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the HasWhereClause flag.
func (p *parser) parseWhereOrCurrentOf(analysis *ast.RawQueryAnalysis) {
	if !p.IsKeyword(keywordWHERE) {
		return
	}
	start := p.Position
	p.Advance()
	analysis.HasWhereClause = true
	conditionStart := p.Position
	if p.MatchKeyword(keywordCURRENT) {
		p.MatchKeyword("OF")
		if p.Current().Kind == tokenIdentifier {
			p.Advance()
		}
	} else {
		p.parseCondition(ast.ClauseWhere, whereExpressionTerminatorKeywords)
	}
	p.recordSpan(dialectkit.SpanWhere, start, p.Position)
	p.recordSpan(dialectkit.SpanWhereCondition, conditionStart, p.Position)
}

// analyseDelete parses a DELETE statement and returns its analysis.
//
// Returns *ast.RawQueryAnalysis which captures the target table, USING clause, WHERE
// predicate and any RETURNING clause.
// Returns error when parsing the CTE list, USING clause or RETURNING clause fails.
func (p *parser) analyseDelete() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	mark := len(p.clauseExpressions)

	if err := p.parseCTEListIfPresent(analysis); err != nil {
		return nil, err
	}

	if err := p.expectKeywords("DELETE", keywordFROM); err != nil {
		return nil, err
	}
	p.MatchKeyword("ONLY")

	schema, tableName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nil, nameError
	}
	alias := p.parseDeleteAlias()
	analysis.FromTables = []ast.TableReference{{Schema: schema, Name: tableName, Alias: alias}}

	if err := p.parseUsingClause(analysis); err != nil {
		return nil, err
	}

	p.parseWhereOrCurrentOf(analysis)

	if err := p.parseReturningIfPresent(analysis); err != nil {
		return nil, err
	}

	p.finaliseAnalysis(analysis)
	analysis.ClauseExpressions = p.takeClauseExpressions(mark)
	return analysis, nil
}

// parseDeleteAlias parses the optional alias following a DELETE target.
//
// Returns string which is the alias name, or empty when absent.
func (p *parser) parseDeleteAlias() string {
	if p.MatchKeyword(keywordAS) {
		if p.Current().Kind == tokenIdentifier {
			return p.Advance().Value
		}
		return ""
	}
	if p.Current().Kind == tokenIdentifier && !p.IsKeyword(keywordUSING, keywordWHERE, keywordRETURNING) {
		return p.Advance().Value
	}
	return ""
}

// parseUsingClause parses a DELETE USING clause when present.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the additional from tables and
// join clauses.
//
// Returns error when parsing the USING clause fails.
func (p *parser) parseUsingClause(analysis *ast.RawQueryAnalysis) error {
	if !p.MatchKeyword(keywordUSING) {
		return nil
	}
	fromTables, joinClauses, _, err := p.parseFromClause()
	if err != nil {
		return err
	}
	analysis.FromTables = append(analysis.FromTables, fromTables...)
	analysis.JoinClauses = joinClauses
	return nil
}

// parseDistinctOn consumes a DISTINCT ON (...) clause from a SELECT.
func (p *parser) parseDistinctOn() {
	if !p.MatchKeyword(keywordON) {
		return
	}
	if p.Current().Kind != tokenLeftParen {
		return
	}
	p.Advance()
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		p.parseExpression()
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
}
