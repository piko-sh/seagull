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

package mysql

import (
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// analyseSelect parses a query: a SELECT, a VALUES table value constructor, a query in
// parentheses, or a set operation over them, into a raw query analysis.
//
// Compound arms (UNION, INTERSECT, EXCEPT) are collected iteratively into one flat list,
// so a long chain of arms consumes a single level of the analysis depth budget; an arm
// that is itself a set operation in parentheses adds its arms to the list.
//
// Returns *ast.RawQueryAnalysis which is the populated analysis.
// Returns error when parsing fails.
func (p *parser) analyseSelect() (*ast.RawQueryAnalysis, error) {
	if err := p.enterAnalysis(); err != nil {
		return nil, err
	}
	defer p.leaveAnalysis()

	analysis, extents, err := p.analyseQueryExpression()
	if err != nil {
		return nil, err
	}

	analysis.ReadOnly = !*p.rowLocked && !p.hasDataModifyingCTE
	p.finaliseAnalysis(analysis)
	if err := dialectkit.SplitCompoundArms(analysis, p.gathered(), extents); err != nil {
		p.recordSyntaxError(err)
		return nil, err
	}
	return analysis, nil
}

// analyseQueryExpression parses a query's arms and trailing ORDER BY, LIMIT and locking
// clauses. When the first arm is VALUES, it discards ORDER BY expressions because their
// generated column names are not checked by the analyser.
//
// Returns *ast.RawQueryAnalysis which describes the first arm, with the other arms as its
// compound branches.
// Returns []dialectkit.ArmExtent which holds, parallel to the branches, the part of what
// the parser gathered that each holds.
// Returns error when an arm or a clause fails to parse.
func (p *parser) analyseQueryExpression() (*ast.RawQueryAnalysis, []dialectkit.ArmExtent, error) {
	valuesFirst := p.IsKeyword(keywordVALUES)
	analysis, extents, err := p.analyseQueryArm()
	if err != nil {
		return nil, nil, err
	}

	branchExtents, err := p.parseCompoundBranches(analysis)
	if err != nil {
		return nil, nil, err
	}
	extents = append(extents, branchExtents...)

	mark := len(p.clauseExpressions)
	dialectkit.RecordOrderByPlaceAt(p.spans, p.Tokens, p.Position)
	p.parseOrderByIfPresent()
	p.parsePaginationIfPresent()
	p.parseLockingIfPresent()
	if valuesFirst {
		p.dropClauseExpressions(mark)
	}
	analysis.ClauseExpressions = append(analysis.ClauseExpressions, p.takeClauseExpressions(mark)...)
	return analysis, extents, nil
}

// analyseQueryArm parses one arm of a query: a query in parentheses, a VALUES table value
// constructor, or a SELECT with its optional WITH list.
//
// Returns *ast.RawQueryAnalysis which describes the arm.
// Returns []dialectkit.ArmExtent which holds the extents of the compound branches of an
// arm that is a set operation in parentheses, and is nil for any other arm.
// Returns error when the arm fails to parse.
func (p *parser) analyseQueryArm() (*ast.RawQueryAnalysis, []dialectkit.ArmExtent, error) {
	switch {
	case p.Current().Kind == tokenLeftParen:
		return p.analyseParenthesisedArm()
	case p.IsKeyword(keywordVALUES):
		analysis, err := p.analyseValuesArm()
		return analysis, nil, err
	default:
		analysis, err := p.analyseSelectArm()
		return analysis, nil, err
	}
}

// analyseParenthesisedArm parses a parenthesised query arm, keeping its set operations,
// ORDER BY, LIMIT and locking clauses inside the parentheses. It records no
// statement-level clause spans for them.
//
// Returns *ast.RawQueryAnalysis which describes the query in parentheses.
// Returns []dialectkit.ArmExtent which holds the extents of its compound branches.
// Returns error when the query fails to parse or the parentheses do not close.
func (p *parser) analyseParenthesisedArm() (*ast.RawQueryAnalysis, []dialectkit.ArmExtent, error) {
	p.Advance()
	if !p.IsKeyword(keywordSELECT, keywordWITH, keywordVALUES) && p.Current().Kind != tokenLeftParen {
		return nil, nil, p.syntaxErrorAt("expected a query inside the parentheses")
	}
	spans := p.spans
	p.spans = nil
	defer func() { p.spans = spans }()

	analysis, extents, err := p.analyseQueryExpression()
	if err != nil {
		return nil, nil, err
	}
	if p.Current().Kind != tokenRightParen {
		return nil, nil, p.syntaxErrorAt("expected ')' to close the query in parentheses")
	}
	p.Advance()
	return analysis, extents, nil
}

// analyseValuesArm parses a VALUES table value constructor written as a query or an arm
// of one: VALUES ROW(...), ROW(...) as MySQL writes it, or VALUES (...), (...) as MariaDB
// does. Its columns are those of its first row, named column1, column2 and so on.
//
// Returns *ast.RawQueryAnalysis which describes the rows.
// Returns error when a row is not enclosed in parentheses.
func (p *parser) analyseValuesArm() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	analysis.ReadOnly = true
	if _, err := p.ExpectKeyword(keywordVALUES); err != nil {
		return nil, err
	}
	for first := true; ; first = false {
		p.matchRowConstructor()
		if p.Current().Kind != tokenLeftParen {
			return nil, p.syntaxErrorAt("expected '(' to open a VALUES row")
		}
		p.Advance()
		columns := p.parseValuesFirstRow()
		if first {
			analysis.OutputColumns = columns
		}
		if p.Current().Kind != tokenRightParen {
			return nil, p.syntaxErrorAt("expected ')' to close the VALUES row")
		}
		p.Advance()
		if p.Current().Kind != tokenComma {
			return analysis, nil
		}
		p.Advance()
	}
}

// analyseSelectArm parses one SELECT arm including the optional WITH list, the select
// list, and the FROM, WHERE, GROUP BY, and HAVING clauses, stopping before any compound
// operator or trailing ORDER BY, LIMIT, and locking clauses.
//
// Returns *ast.RawQueryAnalysis which describes the arm.
// Returns error when the arm fails to parse.
func (p *parser) analyseSelectArm() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	mark := len(p.clauseExpressions)

	if err := p.parseCTEListIfPresent(analysis); err != nil {
		return nil, err
	}

	if _, err := p.ExpectKeyword(keywordSELECT); err != nil {
		return nil, err
	}
	p.skipSelectModifiers()

	if err := p.parseProjection(analysis); err != nil {
		return nil, err
	}

	fromStart := p.Position
	if err := p.parseFromClauseIfPresent(analysis); err != nil {
		return nil, err
	}
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanFrom, p.Tokens, fromStart, p.Position)

	p.parseWhereIfPresent(analysis)

	if p.IsKeyword(keywordGROUP) {
		start := p.Position
		p.Advance()
		p.MatchKeyword(keywordBY)
		analysis.GroupByColumns, analysis.GroupingSets = p.parseGroupByClause(analysis.OutputColumns)
		dialectkit.RecordTokenRange(p.spans, dialectkit.SpanGroupBy, p.Tokens, start, p.Position)
	}

	if p.IsKeyword(keywordHAVING) {
		start := p.Position
		p.Advance()
		p.parseCondition(ast.ClauseHaving, p.parseWhereClause)
		dialectkit.RecordTokenRange(p.spans, dialectkit.SpanHaving, p.Tokens, start, p.Position)
	}

	if err := p.parseWindowClauseIfPresent(); err != nil {
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
// Returns error when an output column fails to parse.
func (p *parser) parseProjection(analysis *ast.RawQueryAnalysis) error {
	start := p.Position
	outputColumns, err := p.parseOutputColumns()
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

// parseWhereIfPresent parses a WHERE clause when present and records where it and its
// condition are written.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the HasWhereClause flag.
func (p *parser) parseWhereIfPresent(analysis *ast.RawQueryAnalysis) {
	if !p.IsKeyword(keywordWHERE) {
		return
	}
	start := p.Position
	p.Advance()
	analysis.HasWhereClause = true
	conditionStart := p.parseCondition(ast.ClauseWhere, p.parseWhereClause)
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanWhere, p.Tokens, start, p.Position)
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanWhereCondition, p.Tokens, conditionStart, p.Position)
}

// parseWindowClauseIfPresent consumes a WINDOW clause of named window definitions, such
// as WINDOW w AS (PARTITION BY a ORDER BY b), registering any parameters in its frames.
//
// Returns error when a definition is malformed.
func (p *parser) parseWindowClauseIfPresent() error {
	if !p.MatchKeyword(keywordWINDOW) {
		return nil
	}
	for {
		if _, err := p.parseIdentifierOrKeyword(); err != nil {
			p.recordSyntaxError(err)
			return err
		}
		if _, err := p.ExpectKeyword(keywordAS); err != nil {
			p.recordSyntaxError(err)
			return err
		}
		if p.Current().Kind != tokenLeftParen {
			return p.syntaxErrorAt("expected '(' to open the window definition")
		}
		p.Advance()
		p.parseWindowSpec()
		if p.Current().Kind != tokenRightParen {
			return p.syntaxErrorAt("expected ')' to close the window definition")
		}
		p.Advance()
		if p.Current().Kind != tokenComma {
			return nil
		}
		p.Advance()
	}
}

// skipSelectModifiers consumes optional SELECT modifier keywords, recording them as the
// statement's quantifier when they include DISTINCT, DISTINCTROW or ALL, which decide the
// rows the statement returns. Modifiers that only steer the server, such as SQL_NO_CACHE,
// are left out of the quantifier when they stand alone.
func (p *parser) skipSelectModifiers() {
	start := p.Position
	quantified := false
	for p.IsKeyword(keywordALL, "DISTINCT", "DISTINCTROW", "HIGH_PRIORITY", "STRAIGHT_JOIN",
		"SQL_SMALL_RESULT", "SQL_BIG_RESULT", "SQL_BUFFER_RESULT",
		"SQL_CACHE", "SQL_NO_CACHE", "SQL_CALC_FOUND_ROWS") {
		quantified = quantified || p.IsKeyword(keywordALL, "DISTINCT", "DISTINCTROW")
		p.Advance()
	}
	if quantified {
		dialectkit.RecordTokenRange(p.spans, dialectkit.SpanQuantifier, p.Tokens, start, p.Position)
	}
}

// parseCTEListIfPresent parses a WITH CTE list onto the analysis.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the CTE definitions when present.
//
// Returns error when CTE parsing fails.
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

// parseFromClauseIfPresent parses a FROM clause onto the analysis.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the tables and joins when a FROM
// clause is present.
//
// Returns error when FROM parsing fails.
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

// parseCompoundBranches collects UNION, INTERSECT, and EXCEPT branches. A branch that is
// a set operation in parentheses adds its own arms as branches after it, so the list
// stays flat.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives any compound branches appended.
//
// Returns []dialectkit.ArmExtent which holds, parallel to the branches, the part of what
// the parser gathered that each holds.
// Returns error when a branch query fails to parse.
func (p *parser) parseCompoundBranches(analysis *ast.RawQueryAnalysis) ([]dialectkit.ArmExtent, error) {
	var extents []dialectkit.ArmExtent
	for {
		compoundOperator := p.parseCompoundQuery()
		if compoundOperator == 0 {
			break
		}
		start := p.gathered().Mark()
		branchAnalysis, branchExtents, branchError := p.analyseQueryArm()
		if branchError != nil {
			return nil, branchError
		}
		end := p.gathered().Mark()
		if len(branchExtents) > 0 {
			end = branchExtents[0].Start
		}
		nested := branchAnalysis.CompoundBranches
		branchAnalysis.CompoundBranches = nil
		extents = append(extents, dialectkit.ArmExtent{Start: start, End: end})
		analysis.CompoundBranches = append(analysis.CompoundBranches, ast.RawCompoundBranch{
			Operator: compoundOperator,
			Query:    branchAnalysis,
		})
		extents = append(extents, branchExtents...)
		analysis.CompoundBranches = append(analysis.CompoundBranches, nested...)
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

// parseOrderByIfPresent consumes an optional ORDER BY clause, recording its keys and
// where it is written.
func (p *parser) parseOrderByIfPresent() {
	if !p.IsKeyword(keywordORDER) {
		return
	}
	start := p.Position
	p.Advance()
	p.MatchKeyword(keywordBY)
	listStart := p.Position
	p.parseOrderByList()
	p.recordClauseExpressions(ast.ClauseOrderBy, p.reparse(listStart, p.Position, true, false))
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanOrderBy, p.Tokens, start, p.Position)
}

// parseLimitOffsetIfPresent consumes an optional LIMIT or OFFSET clause, or, where the
// dialect takes it, a FETCH FIRST clause.
func (p *parser) parseLimitOffsetIfPresent() {
	if p.IsKeyword(keywordLIMIT) || p.IsKeyword(keywordOFFSET) || (p.offsetFetch && p.IsKeyword("FETCH")) {
		p.parseLimitOffset()
	}
}

// parsePaginationIfPresent consumes an optional LIMIT or OFFSET clause and records where
// it is written.
func (p *parser) parsePaginationIfPresent() {
	start := p.Position
	p.parseLimitOffsetIfPresent()
	dialectkit.ExtendTokenRange(p.spans, dialectkit.SpanPagination, p.Tokens, start, p.Position)
}

// parseLockingIfPresent consumes an optional FOR UPDATE, FOR SHARE or LOCK IN SHARE MODE
// clause and records where it is written.
func (p *parser) parseLockingIfPresent() {
	start := p.Position
	p.skipForUpdateClause()
	dialectkit.ExtendTokenRange(p.spans, dialectkit.SpanLocking, p.Tokens, start, p.Position)
}

// analyseCall parses a CALL statement, CALL procedure or CALL procedure(arguments),
// registering the parameters its arguments hold as the procedure's arguments.
//
// Returns *ast.RawQueryAnalysis which holds the arguments' parameters.
// Returns error when the procedure name is missing.
func (p *parser) analyseCall() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	if _, err := p.ExpectKeyword(keywordCALL); err != nil {
		return nil, err
	}
	schema, name, err := p.parseSchemaQualifiedName()
	if err != nil {
		p.recordSyntaxError(err)
		return nil, err
	}
	if p.Current().Kind == tokenLeftParen {
		p.parseFunctionCall(name, schema)
	}
	p.finaliseAnalysis(analysis)
	return analysis, nil
}

// finaliseAnalysis copies parser-collected state onto the analysis.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the parameter references, derived
// tables, and table-valued functions.
func (p *parser) finaliseAnalysis(analysis *ast.RawQueryAnalysis) {
	analysis.ParameterReferences = p.parameterReferences
	analysis.RawDerivedTables = p.rawDerivedTables
	analysis.PredicateSubqueries = p.predicateSubqueries
	analysis.RawTableValuedFunctions = p.rawTableValuedFunctions
}

// analyseInsert parses an INSERT or REPLACE statement, recording what it does with a row
// that conflicts with an existing one: ON DUPLICATE KEY UPDATE and REPLACE write the row
// in place of the existing one, IGNORE skips it, and a plain INSERT handles no conflict.
//
// Returns *ast.RawQueryAnalysis which is the populated analysis.
// Returns error when parsing fails.
func (p *parser) analyseInsert() (*ast.RawQueryAnalysis, error) {
	if err := p.enterAnalysis(); err != nil {
		return nil, err
	}
	defer p.leaveAnalysis()

	analysis := &ast.RawQueryAnalysis{}

	if err := p.parseCTEListIfPresent(analysis); err != nil {
		return nil, err
	}

	verb, err := p.ExpectKeyword("INSERT", keywordREPLACE)
	if err != nil {
		return nil, err
	}
	ignoring := p.skipStatementModifiers("LOW_PRIORITY", "DELAYED", "HIGH_PRIORITY", keywordIGNORE)
	p.MatchKeyword("INTO")

	schema, tableName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}
	if err := p.skipPartitionClause(); err != nil {
		return nil, err
	}
	alias := p.parseOptionalAlias()
	analysis.FromTables = []ast.TableReference{{Schema: schema, Name: tableName, Alias: alias}}
	analysis.InsertTarget = ast.TableReference{Schema: schema, Name: tableName, Alias: alias}

	columnNames, err := p.parseInsertColumnList()
	if err != nil {
		return nil, err
	}

	if err := p.parseInsertedRows(analysis, tableName, columnNames); err != nil {
		return nil, err
	}

	if err := p.parseInsertRowAlias(); err != nil {
		return nil, err
	}

	updating, err := p.parseOnDuplicateKeyUpdateIfPresent(tableName)
	if err != nil {
		return nil, err
	}

	if err := p.parseReturningIfPresent(analysis); err != nil {
		return nil, err
	}

	p.finaliseAnalysis(analysis)
	analysis.InsertTable = tableName
	analysis.InsertColumns = columnNames
	analysis.ConflictAction = insertConflictAction(verb.IsKeyword(tokenIdentifier, keywordREPLACE), ignoring, updating)
	return analysis, nil
}

// parseOnDuplicateKeyUpdateIfPresent parses an INSERT's ON DUPLICATE KEY UPDATE clause
// when one follows, recording none of its expressions, which name the inserted row.
//
// Takes tableName (string) which is the target table.
//
// Returns bool which is true when the clause was read.
// Returns error when the clause is malformed.
func (p *parser) parseOnDuplicateKeyUpdateIfPresent(tableName string) (bool, error) {
	if !p.MatchKeyword(keywordON) {
		return false, nil
	}
	p.suppressClauses = true
	defer func() { p.suppressClauses = false }()
	return true, p.parseOnDuplicateKeyUpdate(tableName)
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

// skipStatementModifiers consumes the modifier keywords that may follow the verb of a
// data-modifying statement, such as LOW_PRIORITY or IGNORE, in any order.
//
// Takes modifiers (...string) which are the modifiers the statement accepts.
//
// Returns bool which is true when IGNORE is among the modifiers consumed.
func (p *parser) skipStatementModifiers(modifiers ...string) bool {
	ignoring := false
	for p.IsKeyword(modifiers...) {
		ignoring = ignoring || p.IsKeyword(keywordIGNORE)
		p.Advance()
	}
	return ignoring
}

// parseInsertRowAlias consumes the row alias of INSERT ...
//
// VALUES (...) AS alias [(columns)], which ON DUPLICATE KEY UPDATE refers to the inserted
// row by. A dialect without the row alias rejects it with a syntax error rather than
// generating SQL the server refuses.
//
// Returns error when the alias is unavailable or malformed.
func (p *parser) parseInsertRowAlias() error {
	if !p.IsKeyword(keywordAS) {
		return nil
	}
	if p.rowAliasUnavailable {
		return p.syntaxErrorAt("a row alias after the inserted values is not available in this dialect; " +
			"refer to the inserted values with VALUES(column) in ON DUPLICATE KEY UPDATE")
	}
	p.Advance()
	if _, err := p.parseIdentifierOrKeyword(); err != nil {
		p.recordSyntaxError(err)
		return err
	}
	if p.Current().Kind == tokenLeftParen {
		if _, err := p.parseColumnList(); err != nil {
			return err
		}
	}
	return nil
}

// parseOptionalAlias consumes an optional AS-prefixed alias identifier.
//
// Returns string which is the alias when present, else the empty string.
func (p *parser) parseOptionalAlias() string {
	if p.MatchKeyword(keywordAS) {
		if p.Current().Kind == tokenIdentifier {
			return p.Advance().Value
		}
	}
	return ""
}

// skipPartitionClause consumes an optional MySQL PARTITION (partition_list) clause that
// may follow the target table of an INSERT or REPLACE.
//
// Returns error when the partition list's parentheses are unmatched.
func (p *parser) skipPartitionClause() error {
	if !p.MatchKeyword("PARTITION") {
		return nil
	}
	return p.skipParenthesisedIfPresent()
}

// parseInsertColumnList parses the optional INSERT column-name list.
//
// Returns []string which is the list of column names, or nil when absent.
// Returns error when the list is malformed.
func (p *parser) parseInsertColumnList() ([]string, error) {
	if p.Current().Kind == tokenLeftParen && !p.IsKeyword(keywordSELECT) && !p.IsKeyword(keywordVALUES) {
		return p.parseColumnList()
	}
	return nil, nil
}

// parseInsertValues dispatches to the appropriate INSERT body parser. A query giving the
// rows, bare or in parentheses, as in INSERT INTO t (a) (SELECT b FROM u), is analysed as
// the INSERT's query, so its projection binds to the target columns.
//
// Takes tableName (string) which is the target table name.
// Takes columnNames ([]string) which are the target column names.
//
// Returns *ast.RawQueryAnalysis which is the analysed query of an INSERT ... SELECT (nil
// for VALUES and SET sources).
// Returns error when the query fails to parse, or a parenthesis opens no query.
func (p *parser) parseInsertValues(tableName string, columnNames []string) (*ast.RawQueryAnalysis, error) {
	switch {
	case p.MatchKeyword(keywordVALUES) || p.MatchKeyword("VALUE"):
		p.parseValuesClause(tableName, columnNames)
	case p.MatchKeyword(keywordSET):
		p.suppressClauses = true
		p.parseSetClause(tableName)
		p.suppressClauses = false
	case p.IsKeyword(keywordSELECT) || p.IsKeyword(keywordWITH) || p.isSubqueryStart():
		p.insertProjectionTable = tableName
		p.insertProjectionColumns = columnNames
		p.insertProjectionIndex = 0
		analysis, selectError := p.analyseSelect()
		p.insertProjectionTable = ""
		p.insertProjectionColumns = nil
		return analysis, selectError
	case p.Current().Kind == tokenLeftParen:
		return nil, p.parseInsertSource()
	}
	return nil, nil
}

// parseReturningIfPresent parses an optional RETURNING clause.
//
// The clause is recorded whether or not the dialect supports RETURNING: the analyser
// judges it against Info.Returning and reports it, pointing at the keyword, when the
// dialect has none.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the returning flag, the keyword's
// location and the output columns.
//
// Returns error when the returning clause cannot be parsed.
func (p *parser) parseReturningIfPresent(analysis *ast.RawQueryAnalysis) error {
	keyword := p.Current()
	start := p.Position
	if !p.MatchKeyword(keywordRETURNING) {
		return nil
	}
	analysis.HasReturning = true
	analysis.ReturningLocation = keyword.Start
	outputColumns, err := p.parseReturningClause()
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

// analyseUpdate parses an UPDATE statement.
//
// Returns *ast.RawQueryAnalysis which is the populated analysis.
// Returns error when parsing fails.
func (p *parser) analyseUpdate() (*ast.RawQueryAnalysis, error) {
	if err := p.enterAnalysis(); err != nil {
		return nil, err
	}
	defer p.leaveAnalysis()

	analysis := &ast.RawQueryAnalysis{}
	mark := len(p.clauseExpressions)

	if err := p.parseCTEListIfPresent(analysis); err != nil {
		return nil, err
	}

	if _, err := p.ExpectKeyword("UPDATE"); err != nil {
		return nil, err
	}
	p.skipStatementModifiers("LOW_PRIORITY", keywordIGNORE)

	fromTables, joinClauses, err := p.parseUpdateTableReferences()
	if err != nil {
		return nil, err
	}
	analysis.FromTables = fromTables
	analysis.JoinClauses = joinClauses

	if _, err := p.ExpectKeyword(keywordSET); err != nil {
		return nil, err
	}

	tableName := ""
	if len(fromTables) > 0 {
		tableName = fromTables[0].Name
	}
	p.parseSetClause(tableName)

	p.parseWhereIfPresent(analysis)

	p.parseOrderByIfPresent()
	p.parsePaginationIfPresent()

	if err := p.parseReturningIfPresent(analysis); err != nil {
		return nil, err
	}

	p.finaliseAnalysis(analysis)
	analysis.ClauseExpressions = p.takeClauseExpressions(mark)
	return analysis, nil
}

// parseUpdateTableReferences parses the table list of an UPDATE.
//
// Returns []ast.TableReference which lists each referenced table.
// Returns []ast.JoinClause which lists any join clauses.
// Returns error when a table reference or join clause fails to parse.
func (p *parser) parseUpdateTableReferences() ([]ast.TableReference, []ast.JoinClause, error) {
	var tables []ast.TableReference
	var joins []ast.JoinClause

	tableReference, err := p.parseTableReference()
	if err != nil {
		return nil, nil, err
	}
	tables = append(tables, tableReference)

	for {
		joinKind, natural, isJoin := p.parseJoinKeyword()
		if isJoin {
			if err := p.appendExplicitJoin(joinKind, natural, &joins); err != nil {
				return nil, nil, err
			}
			continue
		}

		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
		additionalTable, err := p.parseTableReference()
		if err != nil {
			return nil, nil, err
		}
		tables = append(tables, additionalTable)
	}

	return tables, joins, nil
}

// analyseDelete parses a DELETE statement.
//
// Dispatches to the simple or multi-table variant based on the keyword that follows
// DELETE.
//
// Returns *ast.RawQueryAnalysis which is the populated analysis.
// Returns error when parsing fails.
func (p *parser) analyseDelete() (*ast.RawQueryAnalysis, error) {
	if err := p.enterAnalysis(); err != nil {
		return nil, err
	}
	defer p.leaveAnalysis()

	analysis := &ast.RawQueryAnalysis{}

	if err := p.parseCTEListIfPresent(analysis); err != nil {
		return nil, err
	}

	if _, err := p.ExpectKeyword("DELETE"); err != nil {
		return nil, err
	}
	p.skipStatementModifiers("LOW_PRIORITY", "QUICK", keywordIGNORE)

	mark := len(p.clauseExpressions)
	parse := p.parseMultiTableDelete
	if p.IsKeyword(keywordFROM) && !p.isDeleteUsingForm() {
		parse = p.parseSimpleDelete
	}
	result, err := parse(analysis)
	if result != nil {
		result.ClauseExpressions = p.takeClauseExpressions(mark)
	}
	return result, err
}

// parseSimpleDelete parses a single-table DELETE FROM statement.
//
// Takes analysis (*ast.RawQueryAnalysis) which is populated with the parsed table, WHERE
// clause, and RETURNING data.
//
// Returns *ast.RawQueryAnalysis which is the same analysis value.
// Returns error when parsing fails.
func (p *parser) parseSimpleDelete(analysis *ast.RawQueryAnalysis) (*ast.RawQueryAnalysis, error) {
	if _, err := p.ExpectKeyword(keywordFROM); err != nil {
		return nil, err
	}

	schema, tableName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}
	alias := p.parseDeleteAlias()
	if err := p.skipPartitionClause(); err != nil {
		return nil, err
	}
	analysis.FromTables = []ast.TableReference{{Schema: schema, Name: tableName, Alias: alias}}

	p.parseWhereIfPresent(analysis)

	p.parseOrderByIfPresent()
	p.parsePaginationIfPresent()

	if err := p.parseReturningIfPresent(analysis); err != nil {
		return nil, err
	}

	p.finaliseAnalysis(analysis)
	return analysis, nil
}

// isDeleteUsingForm reports whether a DELETE FROM names its target tables before a USING
// clause, as in DELETE FROM t1 USING t1 JOIN t2 ..., by finding USING at the top level
// before the WHERE clause.
//
// Returns bool which is true for the USING form.
func (p *parser) isDeleteUsingForm() bool {
	depth := 0
	for index := p.Position; index < len(p.Tokens); index++ {
		current := p.Tokens[index]
		switch {
		case current.Kind == tokenLeftParen:
			depth++
		case current.Kind == tokenRightParen:
			depth--
		case depth > 0:
		case current.IsKeyword(tokenIdentifier, keywordUSING):
			return true
		case current.IsKeyword(tokenIdentifier, keywordWHERE), current.IsKeyword(tokenIdentifier, keywordORDER),
			current.IsKeyword(tokenIdentifier, keywordLIMIT), current.IsKeyword(tokenIdentifier, keywordRETURNING):
			return false
		}
	}
	return false
}

// parseMultiTableDelete parses a multi-table DELETE statement.
//
// Takes analysis (*ast.RawQueryAnalysis) which is populated with FROM tables, joins, and
// WHERE clause data.
//
// Returns *ast.RawQueryAnalysis which is the same analysis value.
// Returns error when parsing fails.
func (p *parser) parseMultiTableDelete(analysis *ast.RawQueryAnalysis) (*ast.RawQueryAnalysis, error) {
	usingForm := p.MatchKeyword(keywordFROM)
	for p.Current().Kind == tokenIdentifier && !p.IsKeyword(keywordFROM, keywordUSING) {
		if _, _, err := p.parseSchemaQualifiedName(); err != nil {
			return nil, err
		}
		if p.Current().Kind == tokenDot && p.Peek().Kind == tokenStar {
			p.Advance()
			p.Advance()
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	sourceKeyword := keywordFROM
	if usingForm {
		sourceKeyword = keywordUSING
	}
	if _, err := p.ExpectKeyword(sourceKeyword); err != nil {
		return nil, err
	}

	fromTables, joinClauses, _, err := p.parseFromClause()
	if err != nil {
		return nil, err
	}
	analysis.FromTables = fromTables
	analysis.JoinClauses = joinClauses

	p.parseWhereIfPresent(analysis)

	p.finaliseAnalysis(analysis)
	return analysis, nil
}

// parseDeleteAlias parses an optional table alias in a DELETE statement.
//
// Returns string which is the alias when present, else the empty string.
func (p *parser) parseDeleteAlias() string {
	if p.MatchKeyword(keywordAS) {
		if p.Current().Kind == tokenIdentifier {
			return p.Advance().Value
		}
		return ""
	}
	if p.Current().Kind == tokenIdentifier && !p.IsKeyword(keywordUSING, keywordWHERE, keywordRETURNING, keywordORDER, keywordLIMIT) {
		return p.Advance().Value
	}
	return ""
}

// parseColumnList parses a parenthesised list of column identifiers.
//
// Returns []string which is the parsed column names.
// Returns error when the opening parenthesis is missing, a name is invalid, or the list
// is not closed by ')' after its last name.
func (p *parser) parseColumnList() ([]string, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected '('")
	}
	p.Advance()

	var names []string
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		name, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		names = append(names, name)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	if p.Current().Kind != tokenRightParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected ',' or ')' in the column list")
	}
	p.Advance()

	return names, nil
}

// insertConflictAction names what an INSERT or REPLACE does with a row that conflicts
// with an existing one.
//
// ON DUPLICATE KEY UPDATE updates the existing row, and REPLACE replaces it with the new
// one, so either still writes a row for every row given; IGNORE skips the row, so a
// RETURNING clause gives no row for it.
//
// Takes replacing (bool) which is true for a REPLACE statement.
// Takes ignoring (bool) which is true when the statement says IGNORE.
// Takes updating (bool) which is true when the statement has ON DUPLICATE KEY UPDATE,
// which takes precedence over IGNORE for a duplicate key.
//
// Returns ast.ConflictAction which is the statement's conflict action.
func insertConflictAction(replacing, ignoring, updating bool) ast.ConflictAction {
	switch {
	case replacing, updating:
		return ast.ConflictActionDoUpdate
	case ignoring:
		return ast.ConflictActionDoNothing
	default:
		return ast.ConflictActionNone
	}
}
