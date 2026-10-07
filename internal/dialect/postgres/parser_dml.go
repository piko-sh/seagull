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

package postgres

import (
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// analyseSelect parses a SELECT statement, including any UNION, INTERSECT, or EXCEPT
// arms, into a raw query analysis.
//
// Set-operation arms are parsed iteratively into the flat CompoundBranches list, so a
// long chain of arms consumes one level of analysis depth rather than one per arm.
//
// Returns *ast.RawQueryAnalysis which describes the parsed query.
// Returns error when the statement is malformed, or when the marks of its arms do not fit
// what was gathered, an error wrapping dialectkit.ErrArmExtent that is also recorded as
// the statement's syntax error.
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

	extents, err := p.parseCompoundBranches(analysis)
	if err != nil {
		return nil, err
	}

	mark := len(p.clauseExpressions)
	dialectkit.RecordOrderByPlaceAt(p.spans, p.Tokens, p.Position)
	p.parseOrderByIfPresent()
	p.parseSelectTail()
	analysis.ClauseExpressions = append(analysis.ClauseExpressions, p.takeClauseExpressions(mark)...)

	p.finaliseSelectAnalysis(analysis)
	if err := dialectkit.SplitCompoundArms(analysis, p.gathered(), extents); err != nil {
		return nil, p.recordSyntaxError(err)
	}
	return analysis, nil
}

// analyseSelectArm parses a SELECT, VALUES or TABLE arm without trailing set operations,
// ORDER BY or LIMIT. Parenthesised arms retain their own query clauses.
//
// Returns *ast.RawQueryAnalysis which describes the arm.
// Returns error when the arm is malformed.
func (p *parser) analyseSelectArm() (*ast.RawQueryAnalysis, error) {
	if p.Current().Kind == tokenLeftParen {
		return p.parseParenthesisedArm()
	}
	analysis := &ast.RawQueryAnalysis{}
	mark := len(p.clauseExpressions)

	if err := p.parseCTEListIfPresent(analysis); err != nil {
		return nil, err
	}

	if p.IsKeyword(keywordTABLE) {
		return p.parseTableCommand(analysis)
	}

	if p.IsKeyword(keywordVALUES) {
		if err := p.parseValuesRows(analysis); err != nil {
			return nil, err
		}
		analysis.ClauseExpressions = p.takeClauseExpressions(mark)
		return analysis, nil
	}

	if err := p.requireKeyword(keywordSELECT); err != nil {
		return nil, err
	}

	p.parseQuantifier()

	if err := p.parseProjection(analysis); err != nil {
		return nil, err
	}

	fromStart := p.Position
	if err := p.parseFromClauseIfPresent(analysis); err != nil {
		return nil, err
	}
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanFrom, p.Tokens, fromStart, p.Position)

	p.parseSelectWhereIfPresent(analysis)

	if err := p.parseGroupByIfPresent(analysis); err != nil {
		return nil, err
	}

	if p.IsKeyword(keywordHAVING) {
		start := p.Position
		p.Advance()
		p.parseCondition(ast.ClauseHaving)
		dialectkit.RecordTokenRange(p.spans, dialectkit.SpanHaving, p.Tokens, start, p.Position)
	}

	if err := p.parseWindowClauseIfPresent(); err != nil {
		return nil, err
	}

	analysis.ClauseExpressions = p.takeClauseExpressions(mark)
	return analysis, nil
}

// parseTableCommand parses a TABLE name arm, PostgreSQL's shorthand for SELECT * FROM
// name: the arm outputs every column of the table.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the star and the table.
//
// Returns *ast.RawQueryAnalysis which is the arm's analysis.
// Returns error when the table name is missing or malformed.
func (p *parser) parseTableCommand(analysis *ast.RawQueryAnalysis) (*ast.RawQueryAnalysis, error) {
	p.Advance()
	p.MatchKeyword("ONLY")
	schema, name, err := p.requireSchemaQualifiedName()
	if err != nil {
		return nil, err
	}
	if p.Current().Kind == tokenStar {
		p.Advance()
	}
	analysis.OutputColumns = []ast.RawOutputColumn{{IsStar: true, Expression: nil, Name: "", TableAlias: "", ColumnName: ""}}
	analysis.FromTables = []ast.TableReference{{Schema: schema, Name: name, Alias: ""}}
	analysis.FromOrder = []string{name}
	return analysis, nil
}

// parseParenthesisedArm parses a parenthesised SELECT branch in a child parser.
//
// It keeps local ORDER BY and LIMIT clauses inside the branch while sharing parameter
// state with the outer query. A branch of an INSERT's query assigns its select list to
// the INSERT's columns, as the bare query does.
//
// Returns *ast.RawQueryAnalysis which is the arm's analysis.
// Returns error when the parentheses do not balance or the query inside them is
// malformed.
func (p *parser) parseParenthesisedArm() (*ast.RawQueryAnalysis, error) {
	innerTokens, err := p.collectParenthesised()
	if err != nil {
		return nil, p.recordSyntaxError(err)
	}
	child := p.newChildParser(innerTokens)
	child.namedParameterMap = p.namedParameterMap
	child.insertProjectionTable = p.insertProjectionTable
	child.insertProjectionColumns = p.insertProjectionColumns
	analysis, err := child.analyseSelect()
	if err == nil {
		err = child.requireEnd()
	}
	if err != nil {
		return nil, p.recordSyntaxError(err)
	}
	p.adoptChildState(child)
	return analysis, nil
}

// adoptChildState joins what a child parser gathered for the statement to this parser's
// state.
//
// Takes child (*parser) which is the child parser.
func (p *parser) adoptChildState(child *parser) {
	p.parameterCount = child.parameterCount
	p.parameterReferences = append(p.parameterReferences, child.parameterReferences...)
	p.rawDerivedTables = append(p.rawDerivedTables, child.rawDerivedTables...)
	p.predicateSubqueries = append(p.predicateSubqueries, child.predicateSubqueries...)
	p.rawTableValuedFunctions = append(p.rawTableValuedFunctions, child.rawTableValuedFunctions...)
	for number, name := range child.argumentNames {
		p.recordArgumentName(number, name)
	}
	p.hasDataModifyingCTE = p.hasDataModifyingCTE || child.hasDataModifyingCTE
}

// parseQuantifier consumes the DISTINCT, DISTINCT ON (...) or ALL after SELECT and
// records where it is written.
func (p *parser) parseQuantifier() {
	start := p.Position
	if p.MatchKeyword("DISTINCT") {
		p.parseDistinctOn()
	}
	p.MatchKeyword(keywordALL)
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanQuantifier, p.Tokens, start, p.Position)
}

// parseProjection parses the select list of a SELECT arm and records where it and each of
// its items are written.
//
// PostgreSQL accepts an empty select list, such as SELECT FROM items, which outputs no
// columns, so a clause keyword where the list would start ends it rather than being read
// as a column.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the output columns.
//
// Returns error when an output column fails to parse.
func (p *parser) parseProjection(analysis *ast.RawQueryAnalysis) error {
	if p.selectListIsEmpty() {
		return nil
	}
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

// selectListIsEmpty reports whether the select list a SELECT arm starts at the cursor is
// empty: the statement or its parentheses end, or a clause keyword follows at once.
//
// Returns bool which is true for an empty select list.
func (p *parser) selectListIsEmpty() bool {
	switch p.Current().Kind {
	case tokenEOF, tokenSemicolon, tokenRightParen:
		return true
	default:
		return p.AtEnd() || p.isSelectTerminator()
	}
}

// parseSelectWhereIfPresent parses the WHERE clause of a SELECT arm when present and
// records where it is written.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the HasWhereClause flag.
func (p *parser) parseSelectWhereIfPresent(analysis *ast.RawQueryAnalysis) {
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

// parseGroupByIfPresent parses a GROUP BY clause when present and records where it is
// written.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the grouped columns.
//
// Returns error when BY does not follow GROUP.
func (p *parser) parseGroupByIfPresent(analysis *ast.RawQueryAnalysis) error {
	if !p.IsKeyword(keywordGROUP) {
		return nil
	}
	start := p.Position
	p.Advance()
	if err := p.requireKeyword(keywordBY); err != nil {
		return err
	}
	analysis.GroupByColumns, analysis.GroupingSets = p.parseGroupByClause(analysis.OutputColumns)
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanGroupBy, p.Tokens, start, p.Position)
	return nil
}

// parseSelectTail consumes the LIMIT, OFFSET, FETCH and row-locking clauses that end a
// SELECT, which PostgreSQL accepts in either order and with several locking clauses.
func (p *parser) parseSelectTail() {
	for {
		start := p.Position
		p.parsePaginationIfPresent()
		lockingStart := p.Position
		p.skipForUpdateClause()
		dialectkit.ExtendTokenRange(p.spans, dialectkit.SpanLocking, p.Tokens, lockingStart, p.Position)
		if p.Position == start {
			return
		}
	}
}

// parsePaginationIfPresent consumes a LIMIT, FETCH or OFFSET clause and records where it
// is written.
func (p *parser) parsePaginationIfPresent() {
	start := p.Position
	p.parseLimitOffsetIfPresent()
	dialectkit.ExtendTokenRange(p.spans, dialectkit.SpanPagination, p.Tokens, start, p.Position)
}

// parseWindowClauseIfPresent consumes a WINDOW clause: one or more `name AS (window
// specification)` definitions.
//
// Returns error when a definition is malformed.
func (p *parser) parseWindowClauseIfPresent() error {
	if !p.MatchKeyword("WINDOW") {
		return nil
	}
	for {
		if _, err := p.requireIdentifierOrKeyword(); err != nil {
			return err
		}
		if err := p.requireKeyword(keywordAS); err != nil {
			return err
		}
		if err := p.parseParenthesisedWindowSpecification(); err != nil {
			return err
		}
		if p.Current().Kind != tokenComma {
			return nil
		}
		p.Advance()
	}
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

// finaliseSelectAnalysis records the statement-wide read-only flag and the parser's
// accumulated references on a SELECT analysis or one of its set-operation arms.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the final state.
func (p *parser) finaliseSelectAnalysis(analysis *ast.RawQueryAnalysis) {
	analysis.ReadOnly = !*p.rowLocked && !p.hasDataModifyingCTE
	p.finaliseAnalysis(analysis)
}

// parseCTEListIfPresent parses a WITH clause when present.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the parsed CTE definitions.
//
// Returns error when the CTE list cannot be parsed.
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

// parseFromClauseIfPresent parses a FROM clause when present.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the parsed tables and join
// clauses.
//
// Returns error when the FROM clause cannot be parsed.
func (p *parser) parseFromClauseIfPresent(analysis *ast.RawQueryAnalysis) error {
	if !p.MatchKeyword(keywordFROM) {
		return nil
	}
	fromTables, joinClauses, fromOrder, err := p.parseFromClause()
	if err != nil {
		return err
	}
	if err := p.parseAsOfSystemTimeIfPresent(); err != nil {
		return err
	}
	analysis.FromTables = fromTables
	analysis.JoinClauses = joinClauses
	analysis.FromOrder = fromOrder
	return nil
}

// parseCompoundBranches parses the UNION, INTERSECT, and EXCEPT arms that follow the
// first SELECT arm, appending each to the flat CompoundBranches list.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives each parsed compound branch.
//
// Returns []dialectkit.ArmExtent which holds, parallel to the branches, the part of what
// the parser gathered that each holds.
// Returns error when a branch cannot be parsed.
func (p *parser) parseCompoundBranches(analysis *ast.RawQueryAnalysis) ([]dialectkit.ArmExtent, error) {
	var extents []dialectkit.ArmExtent
	for {
		compoundOperator := p.parseCompoundQuery()
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

// parseOrderByIfPresent consumes an ORDER BY clause when present.
func (p *parser) parseOrderByIfPresent() {
	if !p.IsKeyword(keywordORDER) {
		return
	}
	start := p.Position
	p.Advance()
	_ = p.requireKeyword(keywordBY)
	listStart := p.Position
	p.parseOrderByList()
	p.recordClauseExpressions(ast.ClauseOrderBy, p.reparse(listStart, p.Position, true, false))
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanOrderBy, p.Tokens, start, p.Position)
}

// parseLimitOffsetIfPresent consumes a LIMIT, FETCH, or OFFSET clause.
func (p *parser) parseLimitOffsetIfPresent() {
	if p.IsKeyword(keywordLIMIT) || p.IsKeyword(keywordFETCH) || p.IsKeyword(keywordOFFSET) {
		p.parseLimitOffset()
	}
}

// finaliseAnalysis copies parser-accumulated state into the analysis result.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives parameter references, derived
// tables, and table-valued function records.
func (p *parser) finaliseAnalysis(analysis *ast.RawQueryAnalysis) {
	analysis.ParameterReferences = p.parameterReferences
	analysis.RawDerivedTables = p.rawDerivedTables
	analysis.PredicateSubqueries = p.predicateSubqueries
	analysis.RawTableValuedFunctions = p.rawTableValuedFunctions
	analysis.ArgumentNames = p.argumentNames
}

// analyseInsert parses an INSERT statement into a raw query analysis.
//
// The analysis records what the statement's ON CONFLICT clause does with a conflicting
// row, or that it has none; a statement a flavour's insert keyword opens, such as UPSERT,
// replaces a conflicting row. An INSERT without a clause whose WITH list changes data
// leaves it unrecorded, since a skip in one of those statements can still empty its
// result.
//
// Returns *ast.RawQueryAnalysis which describes the parsed insert.
// Returns error when the statement is malformed.
func (p *parser) analyseInsert() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}

	if err := p.parseCTEListIfPresent(analysis); err != nil {
		return nil, err
	}

	if !p.isInsertKeyword() {
		return nil, p.syntaxError("expected keyword INSERT")
	}
	opening := p.Advance()
	p.MatchKeyword("INTO")

	schema, tableName, err := p.requireSchemaQualifiedName()
	if err != nil {
		return nil, err
	}
	alias := p.parseOptionalAlias()
	analysis.FromTables = []ast.TableReference{{Schema: schema, Name: tableName, Alias: alias}}
	analysis.InsertTarget = ast.TableReference{Schema: schema, Name: tableName, Alias: alias}

	columnNames, err := p.parseInsertColumnList()
	if err != nil {
		return nil, err
	}

	p.skipOverridingClause()
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

	if err := p.parseInsertConflictClause(analysis, opening, tableName); err != nil {
		return nil, err
	}

	if err := p.parseReturningIfPresent(analysis); err != nil {
		return nil, err
	}

	p.finaliseAnalysis(analysis)
	analysis.InsertTable = tableName
	analysis.InsertColumns = columnNames
	return analysis, nil
}

// parseInsertConflictClause parses an INSERT's ON CONFLICT clause when one follows and
// records its action on conflicting rows.
//
// Without a clause, the action is ConflictActionNone. An insert form such as
// CockroachDB's UPSERT implies ConflictActionDoUpdate and accepts no ON CONFLICT clause.
// The action is left unrecorded when the statement's WITH list changes data.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the INSERT's analysis.
// Takes opening (token) which is the word that opened the statement.
// Takes tableName (string) which is the INSERT's target table.
//
// Returns error when the clause is malformed, or when a statement a flavour's insert
// keyword opens has one.
func (p *parser) parseInsertConflictClause(analysis *ast.RawQueryAnalysis, opening token, tableName string) error {
	replacesConflictingRow := !opening.IsKeyword(tokenIdentifier, "INSERT")
	if replacesConflictingRow && p.IsKeyword(keywordON) {
		return p.syntaxError(opening.Keyword(tokenIdentifier) + " takes no ON CONFLICT clause")
	}
	if !p.hasDataModifyingCTE {
		analysis.ConflictAction = ast.ConflictActionNone
		if replacesConflictingRow {
			analysis.ConflictAction = ast.ConflictActionDoUpdate
		}
	}
	if !p.MatchKeyword(keywordON) {
		return nil
	}
	p.suppressClauses = true
	conflictMark := len(p.clauseExpressions)
	conflictAction, conflictErr := p.parseOnConflict(tableName)
	p.suppressClauses = false
	if conflictErr != nil {
		return conflictErr
	}
	analysis.ConflictAction = conflictAction
	analysis.ClauseExpressions = append(analysis.ClauseExpressions, p.takeClauseExpressions(conflictMark)...)
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

// parseOptionalAlias consumes an optional AS alias for a table reference.
//
// Returns string which is the alias when present, else empty.
func (p *parser) parseOptionalAlias() string {
	if p.MatchKeyword(keywordAS) {
		if p.Current().Kind == tokenIdentifier {
			return p.Advance().Value
		}
	}
	return ""
}

// parseInsertColumnList parses the optional column list of an INSERT. A parenthesis that
// opens a query, as in INSERT INTO t (SELECT ...), is the INSERT's rows, not a column
// list.
//
// Returns []string which lists the columns when a list is present, else nil.
// Returns error when the column list cannot be parsed.
func (p *parser) parseInsertColumnList() ([]string, error) {
	if p.Current().Kind == tokenLeftParen && !p.isSubqueryStart() {
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

// parseInsertValues parses the VALUES, DEFAULT VALUES, or query payload. A query may be
// written bare or in parentheses, as in INSERT INTO t (a, b) (SELECT ...), and either way
// its select list is assigned to the target columns in order.
//
// Takes tableName (string) which scopes column lookups in the values clause.
// Takes columnNames ([]string) which lists the target columns for parameter binding when
// present.
//
// Returns *ast.RawQueryAnalysis which is the analysed query of an INSERT ... SELECT (nil
// for VALUES and DEFAULT VALUES).
// Returns error when the query fails to parse, or a parenthesis opens no query.
func (p *parser) parseInsertValues(tableName string, columnNames []string) (*ast.RawQueryAnalysis, error) {
	switch {
	case p.MatchKeyword(keywordVALUES):
		p.parseValuesClause(tableName, columnNames)
	case p.MatchKeyword(keywordDEFAULT):
		p.MatchKeyword(keywordVALUES)
	case p.IsKeyword(keywordSELECT) || p.IsKeyword(keywordWITH) || p.IsKeyword(keywordTABLE) || p.isSubqueryStart():
		p.insertProjectionTable = tableName
		p.insertProjectionColumns = columnNames
		p.insertProjectionIndex = 0
		analysis, selectError := p.analyseSelect()
		p.insertProjectionTable = ""
		p.insertProjectionColumns = nil
		p.insertProjectionIndex = 0
		return analysis, selectError
	case p.Current().Kind == tokenLeftParen:
		return nil, p.parseInsertSource()
	}
	return nil, nil
}

// parseReturningIfPresent parses a RETURNING clause when present.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the parsed output columns and the
// HasReturning flag.
//
// Returns error when the RETURNING clause cannot be parsed.
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
		dialectkit.RecordTokenRange(p.spans, dialectkit.SpanReturning, p.Tokens, start, p.Position)
		p.spans.RecordReturningItems(p.itemSpans)
	}
	return nil
}

// analyseUpdate parses an UPDATE statement into a raw query analysis.
//
// Returns *ast.RawQueryAnalysis which describes the parsed update.
// Returns error when the statement is malformed.
func (p *parser) analyseUpdate() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	mark := len(p.clauseExpressions)

	if err := p.parseCTEListIfPresent(analysis); err != nil {
		return nil, err
	}

	if err := p.requireKeyword("UPDATE"); err != nil {
		return nil, err
	}
	p.MatchKeyword("ONLY")

	schema, tableName, err := p.requireSchemaQualifiedName()
	if err != nil {
		return nil, err
	}
	alias := p.parseUpdateAlias()
	analysis.FromTables = []ast.TableReference{{Schema: schema, Name: tableName, Alias: alias}}

	if err := p.requireKeyword(keywordSET); err != nil {
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

// parseUpdateAlias parses the optional alias of an UPDATE target table.
//
// Returns string which is the alias when present, else empty.
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

// parseAdditionalFromClause parses the optional FROM clause of an UPDATE.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the parsed additional tables and
// join clauses.
//
// Returns error when the FROM clause cannot be parsed.
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

// parseWhereOrCurrentOf consumes an optional WHERE clause, including the
// Postgres-specific WHERE CURRENT OF <cursor> form used in UPDATE and DELETE.
//
// It sets analysis.HasWhereClause when a WHERE keyword is matched, including the cursor
// form, and records the clause span for source rewriting.
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
		p.parseCondition(ast.ClauseWhere)
	}
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanWhere, p.Tokens, start, p.Position)
	dialectkit.RecordTokenRange(p.spans, dialectkit.SpanWhereCondition, p.Tokens, conditionStart, p.Position)
}

// analyseDelete parses a DELETE statement into a raw query analysis.
//
// Returns *ast.RawQueryAnalysis which describes the parsed delete.
// Returns error when the statement is malformed.
func (p *parser) analyseDelete() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	mark := len(p.clauseExpressions)

	if err := p.parseCTEListIfPresent(analysis); err != nil {
		return nil, err
	}

	if err := p.requireKeywordSequence("DELETE", keywordFROM); err != nil {
		return nil, err
	}
	p.MatchKeyword("ONLY")

	schema, tableName, err := p.requireSchemaQualifiedName()
	if err != nil {
		return nil, err
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

// parseDeleteAlias parses the optional alias of a DELETE target table.
//
// Returns string which is the alias when present, else empty.
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

// parseUsingClause parses an optional USING clause of a DELETE.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the parsed additional tables and
// join clauses.
//
// Returns error when the USING clause cannot be parsed.
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

// analyseCall parses CALL, registering parameters against the procedure signature when
// known and their casts otherwise. Procedures with output parameters expose one result
// row containing those parameters.
//
// Returns *ast.RawQueryAnalysis which holds the parameter references and the row of a
// procedure with output parameters.
// Returns error when the statement is not a procedure call.
func (p *parser) analyseCall() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}

	if err := p.requireKeyword("CALL"); err != nil {
		return nil, err
	}
	schema, name, err := p.requireSchemaQualifiedName()
	if err != nil {
		return nil, err
	}
	if p.Current().Kind != tokenLeftParen {
		return nil, p.syntaxError("expected '(' to open the procedure's arguments")
	}
	call := p.parseFunctionCall(name, schema)
	p.recordCallResult(analysis, schema, name, call)

	p.finaliseAnalysis(analysis)
	return analysis, nil
}

// recordCallResult records the row a CALL of a procedure the catalogue knows returns,
// when the procedure has output parameters: the call as a FROM item and every one of its
// columns as the statement's output.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the output columns.
// Takes schema (string) which is the procedure's schema as the call writes it, or empty.
// Takes name (string) which is the procedure's name as the call writes it.
// Takes call (ast.Expression) which is the parsed call.
func (p *parser) recordCallResult(analysis *ast.RawQueryAnalysis, schema, name string, call ast.Expression) {
	qualifiedName := name
	if schema != "" {
		qualifiedName = schema + "." + qualifiedName
	}
	if len(catalogueTableFunctionColumns(p.catalogue, qualifiedName)) == 0 {
		return
	}
	var arguments []ast.Expression
	if function, isCall := call.(*ast.FunctionCallExpression); isCall && function != nil {
		arguments = function.Arguments
	}
	p.rawTableValuedFunctions = append(p.rawTableValuedFunctions, ast.RawTableValuedFunctionReference{
		FunctionName:      qualifiedName,
		Arguments:         arguments,
		Alias:             name,
		ColumnDefinitions: nil,
		Ordinality:        nil,
		Functions:         nil,
		JoinKind:          ast.JoinInner,
	})
	analysis.OutputColumns = []ast.RawOutputColumn{{IsStar: true, Expression: nil, Name: "", TableAlias: "", ColumnName: ""}}
}

// parseDistinctOn consumes a DISTINCT ON (expr, ...) clause.
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
