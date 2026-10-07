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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

var (
	// fromItemFollowerKeywords lists the keywords that may follow a FROM item, so none of
	// them is read as the item's alias when written without AS.
	fromItemFollowerKeywords = []string{
		keywordSET, keywordVALUES, keywordDEFAULT, keywordWHERE, keywordON, keywordUSING, keywordLATERAL,
		keywordQUALIFY, keywordWINDOW, keywordGROUP, keywordHAVING, keywordORDER, keywordLIMIT, keywordOFFSET,
		keywordUNION, keywordINTERSECT, keywordEXCEPT, keywordRETURNING, keywordPIVOT, keywordUNPIVOT,
		"TABLESAMPLE", keywordSELECT,
	}
)

// joinGroupMark is how much of the FROM clause and the statement-wide state the parser
// had gathered when a parenthesised join started.
type joinGroupMark struct {
	// joins is the number of joins of the FROM clause.
	joins int

	// derivedTables is the number of derived tables.
	derivedTables int

	// tableFunctions is the number of table function references.
	tableFunctions int

	// predicateSubqueries is the number of predicate subqueries.
	predicateSubqueries int

	// clauseExpressions is the number of clause expressions.
	clauseExpressions int

	// parameters is the number of parameter references.
	parameters int

	// fromItems is the number of names in the FROM order.
	fromItems int
}

// joinKeywordEntry maps a join keyword to its kind and whether OUTER may follow.
type joinKeywordEntry struct {
	// kind is the join kind produced when this keyword is matched.
	kind ast.JoinKind

	// hasOuter records whether an OUTER keyword may follow.
	hasOuter bool
}

var (
	// joinPositional is DuckDB's POSITIONAL JOIN, which pairs the rows of the two sides by
	// position and pads the shorter side with NULLs, so either side's columns may be NULL.
	joinPositional = ast.JoinKind{Keyword: keywordPOSITIONAL, Nullable: ast.JoinSideLeft | ast.JoinSideRight, Filters: false}

	// joinAsof is DuckDB's ASOF JOIN, which pairs each left row with its nearest right row
	// and drops a left row that has none, like an inner join.
	joinAsof = ast.JoinKind{Keyword: keywordASOF, Nullable: 0, Filters: true}

	// joinAsofLeft is DuckDB's ASOF LEFT JOIN, which keeps a left row that has no nearest
	// right row, so the right side's columns may be NULL.
	joinAsofLeft = ast.JoinKind{Keyword: keywordASOF + " LEFT", Nullable: ast.JoinSideRight, Filters: false}

	// joinSemi is DuckDB's SEMI JOIN, which keeps the left rows that have a match and
	// returns none of the right side's columns.
	joinSemi = ast.JoinKind{Keyword: "SEMI", Nullable: 0, Filters: true}

	// joinAnti is DuckDB's ANTI JOIN, which keeps the left rows that have no match and
	// returns none of the right side's columns.
	joinAnti = ast.JoinKind{Keyword: "ANTI", Nullable: 0, Filters: true}

	// joinKeywordDispatch maps each recognised join-prefix keyword to its dispatch entry.
	joinKeywordDispatch = map[string]joinKeywordEntry{
		"INNER":           {kind: ast.JoinInner, hasOuter: false},
		"LEFT":            {kind: ast.JoinLeft, hasOuter: true},
		"RIGHT":           {kind: ast.JoinRight, hasOuter: true},
		"FULL":            {kind: ast.JoinFull, hasOuter: true},
		"CROSS":           {kind: ast.JoinCross, hasOuter: false},
		keywordPOSITIONAL: {kind: joinPositional, hasOuter: false},
		"SEMI":            {kind: joinSemi, hasOuter: false},
		"ANTI":            {kind: joinAnti, hasOuter: false},
	}
)

// parseCTEList parses a WITH ... clause and returns the CTE definitions.
//
// Returns []ast.RawCTEDefinition which lists the parsed CTEs in declared order.
// Returns error when an individual CTE fails to parse.
func (p *parser) parseCTEList() ([]ast.RawCTEDefinition, error) {
	if _, err := p.ExpectKeyword(keywordWITH); err != nil {
		return nil, err
	}
	isRecursive := p.MatchKeyword("RECURSIVE")

	var definitions []ast.RawCTEDefinition

	for {
		definition, err := p.parseSingleCTEDefinition(isRecursive)
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

// parseSingleCTEDefinition parses one CTE definition within a WITH clause.
//
// Takes isRecursive (bool) which records whether the enclosing WITH used the RECURSIVE
// keyword.
//
// Returns ast.RawCTEDefinition which describes the parsed CTE.
// Returns error when parsing the name, columns, or body fails.
func (p *parser) parseSingleCTEDefinition(isRecursive bool) (ast.RawCTEDefinition, error) {
	cteName, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return ast.RawCTEDefinition{}, err
	}

	columnNames, columnListErr := p.parseCTEColumnNames()
	if columnListErr != nil {
		return ast.RawCTEDefinition{}, columnListErr
	}

	if _, err := p.ExpectKeyword(keywordAS); err != nil {
		return ast.RawCTEDefinition{}, err
	}

	p.skipMaterialisationHint()

	cteTokens, collectErr := p.collectParenthesised()
	if collectErr != nil {
		return ast.RawCTEDefinition{}, collectErr
	}

	cteAnalysis, analyseErr := p.analyseCTEBody(cteTokens)
	if analyseErr != nil {
		return ast.RawCTEDefinition{}, analyseErr
	}

	definition := ast.RawCTEDefinition{}
	definition.Name = cteName
	definition.IsRecursive = isRecursive
	p.populateCTEDefinition(&definition, cteAnalysis, columnNames)

	return definition, nil
}

// skipMaterialisationHint consumes an optional MATERIALIZED or NOT MATERIALIZED hint
// between AS and the CTE body.
func (p *parser) skipMaterialisationHint() {
	p.MatchKeyword("MATERIALIZED")
	if p.MatchKeyword(keywordNOT) {
		p.MatchKeyword("MATERIALIZED")
	}
}

// analyseCTEBody analyses a CTE body's tokens as a nested query, or as a data-modifying
// statement when it begins with INSERT, UPDATE or DELETE, requiring the body to end where
// its parentheses do and splicing its parameters back into this parser.
//
// Takes cteTokens ([]token) which are the parenthesised tokens of the CTE body.
//
// Returns *ast.RawQueryAnalysis which is the body's structural analysis.
// Returns error which is non-nil, and recorded as the statement's syntax error, when the
// body is malformed or leaves tokens unparsed.
func (p *parser) analyseCTEBody(cteTokens []token) (*ast.RawQueryAnalysis, error) {
	if len(cteTokens) == 0 || !cteTokens[0].IsKeyword(tokenIdentifier, "INSERT") &&
		!cteTokens[0].IsKeyword(tokenIdentifier, "UPDATE") && !cteTokens[0].IsKeyword(tokenIdentifier, "DELETE") {
		_, analysis, err := p.analyseNestedQuery(cteTokens)
		return analysis, err
	}
	p.hasDataModifyingCTE = true
	child := p.newChildParser(cteTokens)
	child.parameterCount = p.parameterCount
	analysis, err := p.analyseCTEBodyDML(child)
	if err == nil {
		err = child.requireEnd()
	}
	if err != nil {
		p.recordSyntaxError(err)
		return nil, err
	}
	p.parameterCount = child.parameterCount
	p.parameterReferences = append(p.parameterReferences, child.parameterReferences...)
	p.adoptArgumentNames(child)
	return analysis, nil
}

// analyseCTEBodyDML analyses a CTE body that begins with INSERT, UPDATE, or DELETE.
//
// Takes cteParser (*parser) which is the parser positioned at the CTE body's first token.
//
// Returns *ast.RawQueryAnalysis which is the body's analysis.
// Returns error when the underlying DML analysis fails.
func (*parser) analyseCTEBodyDML(cteParser *parser) (*ast.RawQueryAnalysis, error) {
	switch {
	case cteParser.IsKeyword("INSERT"):
		return dialectkit.ChangingRows(ast.DataChangeInsert, (*parser).analyseInsert)(cteParser)
	case cteParser.IsKeyword("UPDATE"):
		return dialectkit.ChangingRows(ast.DataChangeUpdate, (*parser).analyseUpdate)(cteParser)
	default:
		return dialectkit.ChangingRows(ast.DataChangeDelete, (*parser).analyseDelete)(cteParser)
	}
}

// parseCTEColumnNames parses the optional column list preceding AS in a CTE definition.
//
// Returns []string which lists the parsed column names, or nil when no column list
// precedes AS.
// Returns error when parsing an individual identifier fails.
func (p *parser) parseCTEColumnNames() ([]string, error) {
	if p.Current().Kind != tokenLeftParen || p.IsKeyword(keywordAS) {
		return nil, nil
	}
	if p.peekForAS() {
		return nil, nil
	}
	return p.parseColumnList()
}

// populateCTEDefinition copies the body analysis and explicit column names onto a CTE
// definition.
//
// Takes definition (*ast.RawCTEDefinition) which is the CTE record to populate.
// Takes analysis (*ast.RawQueryAnalysis) which provides the body's output columns,
// from-tables, and joins.
// Takes columnNames ([]string) which optionally overrides the body's output column names.
func (*parser) populateCTEDefinition(
	definition *ast.RawCTEDefinition,
	analysis *ast.RawQueryAnalysis,
	columnNames []string,
) {
	definition.OutputColumns = analysis.OutputColumns
	definition.ColumnNames = columnNames
	definition.FromTables = analysis.FromTables
	definition.JoinClauses = analysis.JoinClauses
	definition.CompoundBranches = analysis.CompoundBranches
	definition.FromOrder = analysis.FromOrder
	definition.Query = analysis

	definition.ParameterReferences = analysis.ParameterReferences
}

// peekForAS looks ahead past a balanced parenthesis run to detect whether a CTE column
// list precedes AS rather than the body.
//
// Returns bool which is true when the current parenthesis run is the CTE column list,
// false when it is the body.
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
		if index+1 < len(p.Tokens) && p.Tokens[index+1].Kind == tokenIdentifier &&
			p.Tokens[index+1].IsKeyword(tokenIdentifier, keywordAS) {
			return false
		}
		break
	}
	p.Position = saved
	return true
}

// parseColumnList parses a parenthesised comma-separated list of column identifiers.
//
// Returns []string which lists the parsed names in declared order.
// Returns error when the opening parenthesis is missing or an identifier cannot be
// parsed.
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
		return nil, dialectkit.NewParseError(p.Current(), "expected ')' to close the column list")
	}
	p.Advance()

	return names, nil
}

// parseOutputColumns parses the SELECT or RETURNING projection list. A select list may
// end in a comma before the clause that follows it, as DuckDB allows, but may not be
// empty: SELECT followed directly by FROM is a syntax error, as it is in DuckDB.
//
// Takes selectList (bool) which is true for a SELECT's list, which may end in a comma.
//
// Returns []ast.RawOutputColumn which lists the parsed columns in declared order.
// Returns error when the list is empty or an individual column fails to parse.
func (p *parser) parseOutputColumns(selectList bool) ([]ast.RawOutputColumn, error) {
	if p.IsKeyword(keywordFROM) {
		return nil, p.syntaxErrorAt("SELECT clause without selection list")
	}
	var columns []ast.RawOutputColumn

	var items []ast.ItemSpan
	for {
		first := p.Position
		p.aliasStart = -1
		column, err := p.parseOneOutputColumn()
		if err != nil {
			return nil, err
		}
		columns = append(columns, column)
		expressionEnd := p.Position
		if p.aliasStart >= 0 {
			expressionEnd = p.aliasStart
		}
		items = append(items, p.itemSpan(first, expressionEnd, p.Position, column.IsStar))

		if p.insertProjectionColumns != nil {
			p.insertProjectionIndex++
		}

		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
		if selectList && p.endsSelectList() {
			break
		}
	}

	p.insertProjectionColumns = nil
	p.itemSpans = items

	return columns, nil
}

// parseOneOutputColumn parses a single projection list entry, handling star, qualified
// star, expression, and alias forms.
//
// Returns ast.RawOutputColumn which describes the parsed entry.
// Returns error when parsing the optional alias fails.
func (p *parser) parseOneOutputColumn() (ast.RawOutputColumn, error) {
	if p.Current().Kind == tokenStar {
		p.Advance()
		return ast.RawOutputColumn{IsStar: true, Expression: nil, Name: "", TableAlias: "", ColumnName: ""}, p.rejectStarModifier()
	}

	if p.Current().Kind == tokenIdentifier && p.Peek().Kind == tokenDot && p.PeekAt(2).Kind == tokenStar {
		tableAlias := p.Advance().Value
		p.Advance()
		p.Advance()
		return ast.RawOutputColumn{
			IsStar:     true,
			TableAlias: tableAlias,
			Expression: nil,
			Name:       "",
			ColumnName: "",
		}, p.rejectStarModifier()
	}

	expression := p.parseExpression()
	column := ast.RawOutputColumn{}
	p.expressionToOutputColumn(expression, &column)

	if err := p.parseOutputColumnAlias(&column); err != nil {
		return ast.RawOutputColumn{}, err
	}

	return column, nil
}

// rejectStarModifier reports a syntax error for an EXCLUDE, REPLACE or RENAME list after
// a star: the star's columns are expanded from the catalogue, which cannot leave out,
// replace or rename one, so the generated code would not match the query's columns.
//
// Returns error which is the recorded syntax error, or nil when no modifier follows.
func (p *parser) rejectStarModifier() error {
	if !p.IsKeyword("EXCLUDE", "REPLACE", "RENAME") {
		return nil
	}
	return p.syntaxErrorAt("a * with " + p.Current().Keyword(tokenIdentifier) +
		" is not supported: list the columns the query returns")
}

// parseOutputColumnAlias parses an AS alias or bare identifier alias for an output column
// and writes it onto column.
//
// Takes column (*ast.RawOutputColumn) which is the column record to update with the
// alias.
//
// Returns error when parsing an AS alias identifier fails.
func (p *parser) parseOutputColumnAlias(column *ast.RawOutputColumn) error {
	p.aliasStart = p.Position
	if p.MatchKeyword(keywordAS) {
		alias, aliasErr := p.parseIdentifierOrKeyword()
		if aliasErr != nil {
			return aliasErr
		}
		column.Name = alias
		return nil
	}
	if p.Current().Kind == tokenIdentifier && !p.isSelectTerminator() {
		column.Name = p.Advance().Value
	}
	return nil
}

// parseReturningClause parses the projection list of a RETURNING clause.
//
// Returns []ast.RawOutputColumn which lists the parsed columns in declared order.
// Returns error when an individual column fails to parse.
func (p *parser) parseReturningClause() ([]ast.RawOutputColumn, error) {
	return p.parseOutputColumns(false)
}

// endsSelectList reports whether the current token ends a select list after a trailing
// comma: the end of the statement or of a parenthesised query, or the keyword of a clause
// that follows the select list.
//
// Returns bool which is true when the select list ends here.
func (p *parser) endsSelectList() bool {
	return p.AtEnd() || p.Current().Kind == tokenRightParen || p.IsKeyword(keywordFROM, keywordWHERE, keywordGROUP,
		keywordHAVING, keywordWINDOW, keywordQUALIFY, keywordORDER, keywordLIMIT, keywordOFFSET, keywordUNION,
		keywordINTERSECT, keywordEXCEPT)
}

// expressionToOutputColumn copies attributes from an expression into a raw output column,
// recognising column references and function calls.
//
// Takes expression (ast.Expression) which is the parsed projection expression.
// Takes column (*ast.RawOutputColumn) which receives the projected name, expression, and
// qualifiers.
func (*parser) expressionToOutputColumn(expression ast.Expression, column *ast.RawOutputColumn) {
	switch typed := expression.(type) {
	case *ast.ColumnRefExpression:
		column.Name = typed.ColumnName
		column.TableAlias = typed.TableAlias
		column.ColumnName = typed.ColumnName
	case *ast.FunctionCallExpression:
		column.Name = typed.FunctionName
		column.Expression = expression
	default:
		column.Expression = expression
	}
}

// isSelectTerminator reports whether the current token is a keyword that ends a SELECT
// projection or table source.
//
// Returns bool which is true when the current keyword terminates a SELECT sub-clause.
func (p *parser) isSelectTerminator() bool {
	return p.IsKeyword(keywordFROM, keywordWHERE, keywordGROUP, keywordHAVING, keywordORDER, keywordLIMIT, keywordOFFSET,
		keywordFETCH, keywordFOR, keywordUNION, keywordINTERSECT, keywordEXCEPT, keywordON, keywordRETURNING, "INTO", "WINDOW")
}

// parseFromClause parses a FROM clause, including comma-separated items and explicit
// joins.
//
// Returns []ast.TableReference which lists the comma-joined base tables in declared
// order.
// Returns []ast.JoinClause which lists the explicit joins in declared order.
// Returns []string which holds the names the FROM items are visible under, in order.
// Returns error when parsing a table source or join condition fails.
func (p *parser) parseFromClause() ([]ast.TableReference, []ast.JoinClause, []string, error) {
	var tables []ast.TableReference
	var joins []ast.JoinClause
	var order []string

	enclosingOrder := p.fromOrder
	p.fromOrder = &order
	defer func() { p.fromOrder = enclosingOrder }()

	if err := p.parseFromItem(&tables, &joins); err != nil {
		return nil, nil, nil, err
	}

	for {
		joinKind, natural, isJoin := p.parseJoinKeyword()
		if isJoin {
			if err := p.appendExplicitJoin(joinKind, natural, &joins); err != nil {
				return nil, nil, nil, err
			}
			continue
		}

		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
		if err := p.parseFromItem(&tables, &joins); err != nil {
			return nil, nil, nil, err
		}
	}

	return tables, joins, order, nil
}

// recordFromItem records the name a FROM item is visible under, in FROM order.
//
// Takes name (string) which is the item's alias, or its name when it has none.
func (p *parser) recordFromItem(name string) {
	if p.fromOrder != nil && name != "" {
		*p.fromOrder = append(*p.fromOrder, name)
	}
}

// parseFromItem parses one comma-separated FROM item and appends its table source.
//
// Takes tables (*[]ast.TableReference) which receives the item's table.
// Takes joins (*[]ast.JoinClause) which receives the joins inside a parenthesised item.
//
// Returns error when the table source fails to parse.
func (p *parser) parseFromItem(tables *[]ast.TableReference, joins *[]ast.JoinClause) error {
	p.MatchKeyword(keywordLATERAL)
	tableReference, err := p.parseTableSource(ast.JoinInner, joins)
	if err != nil {
		return err
	}
	if tableReference != nil {
		*tables = append(*tables, *tableReference)
	}
	return nil
}

// appendExplicitJoin parses an explicit JOIN, including its ON or USING condition, and
// appends it to joins.
//
// When the joined source is a parenthesised join, its first table takes the outer join's
// kind and its own joins follow it; under a LEFT or FULL join the inner joins become
// outer too, because every table inside the parentheses can then be missing. The right
// side of a SEMI or ANTI join only filters the left side, so it is recorded as a
// predicate subquery rather than a joined table.
//
// Takes joinKind (ast.JoinKind) which is the parsed join kind.
// Takes natural (bool) which is true for a NATURAL join.
// Takes joins (*[]ast.JoinClause) which is the slice to extend with the parsed join.
//
// Returns error when parsing the table source fails.
func (p *parser) appendExplicitJoin(joinKind ast.JoinKind, natural bool, joins *[]ast.JoinClause) error {
	p.MatchKeyword(keywordLATERAL)

	if joinKind == joinSemi || joinKind == joinAnti {
		return p.parseFilteringJoin()
	}

	insertAt := len(*joins)
	derivedAt := len(p.rawDerivedTables)
	tableReference, err := p.parseTableSource(joinKind, joins)
	if err != nil {
		return err
	}
	if joinKind == ast.JoinLeft || joinKind == ast.JoinFull {
		for index := insertAt; index < len(*joins); index++ {
			if (*joins)[index].Kind == ast.JoinInner || (*joins)[index].Kind == ast.JoinCross {
				(*joins)[index].Kind = joinKind
			}
		}
	}
	if tableReference != nil {
		*joins = slices.Insert(*joins, insertAt, ast.JoinClause{Kind: joinKind, Table: *tableReference, Using: nil})
	}

	using := p.parseJoinCondition(natural)
	switch {
	case using == nil:
	case tableReference != nil:
		(*joins)[insertAt].Using = using
	case len(p.rawDerivedTables) > derivedAt:
		p.rawDerivedTables[len(p.rawDerivedTables)-1].Using = using
	}
	return nil
}

// parseFilteringJoin parses the right side and condition of a SEMI or ANTI join. Its
// columns never reach the result, so the side and its condition are recorded as a
// predicate subquery, which types any parameter in the condition from the side's columns.
//
// Returns error when the joined table fails to parse.
func (p *parser) parseFilteringJoin() error {
	var sideTables []ast.TableReference
	var sideJoins []ast.JoinClause
	derivedBefore := len(p.rawDerivedTables)
	functionsBefore := len(p.rawTableValuedFunctions)
	tableReference, err := p.parseTableSource(ast.JoinInner, &sideJoins)
	if err != nil {
		return err
	}
	if tableReference != nil {
		sideTables = append(sideTables, *tableReference)
	}
	referencesBefore := len(p.parameterReferences)
	conditionMark := len(p.clauseExpressions)
	p.parseJoinCondition(false)

	side := &ast.RawQueryAnalysis{}
	side.ClauseExpressions = p.takeClauseExpressions(conditionMark)
	side.ReadOnly = true
	side.FromTables = sideTables
	side.JoinClauses = sideJoins
	side.RawDerivedTables = slices.Clone(p.rawDerivedTables[derivedBefore:])
	side.RawTableValuedFunctions = slices.Clone(p.rawTableValuedFunctions[functionsBefore:])
	side.ParameterReferences = slices.Clone(p.parameterReferences[referencesBefore:])
	p.rawDerivedTables = p.rawDerivedTables[:derivedBefore]
	p.rawTableValuedFunctions = p.rawTableValuedFunctions[:functionsBefore]
	p.predicateSubqueries = append(p.predicateSubqueries, side)
	return nil
}

// parseTableSource parses a table source: a derived table, a parenthesised join, a
// table-valued function or a base table, followed by any PIVOT or UNPIVOT applied to it
// and any TABLESAMPLE clause.
//
// Takes joinKind (ast.JoinKind) which records how the source is joined into the FROM
// clause.
// Takes joins (*[]ast.JoinClause) which receives the joins inside a parenthesised join.
//
// Returns *ast.TableReference which is the base table, or the first table of a
// parenthesised join, or nil when the source was recorded as a derived table or
// table-valued function.
// Returns error when the source fails to parse.
func (p *parser) parseTableSource(joinKind ast.JoinKind, joins *[]ast.JoinClause) (*ast.TableReference, error) {
	derivedBefore := len(p.rawDerivedTables)
	functionsBefore := len(p.rawTableValuedFunctions)
	orderBefore := p.fromOrderLength()
	tableReference, err := p.parseTableSourceBody(joinKind, joins)
	if err != nil {
		return nil, err
	}
	pivoted := false
	for p.isPivotClauseStart() {
		source := p.takePivotSource(tableReference, derivedBefore, functionsBefore)
		if err := p.parsePivotSuffix(joinKind, source); err != nil {
			return nil, err
		}
		tableReference = nil
		pivoted = true
	}
	if pivoted && p.fromOrder != nil && len(p.rawDerivedTables) > 0 {
		*p.fromOrder = (*p.fromOrder)[:orderBefore]
		p.recordFromItem(p.rawDerivedTables[len(p.rawDerivedTables)-1].Alias)
	}
	p.skipTableSample()
	return tableReference, nil
}

// fromOrderLength returns how many FROM items have been recorded so far.
//
// Returns int which is the count, or zero outside a FROM clause.
func (p *parser) fromOrderLength() int {
	if p.fromOrder == nil {
		return 0
	}
	return len(*p.fromOrder)
}

// isPivotClauseStart reports whether the cursor sits on a SQL-standard PIVOT or UNPIVOT
// clause applied to the FROM item before it.
//
// Returns bool which is true for PIVOT ( or UNPIVOT ( or UNPIVOT INCLUDE/EXCLUDE NULLS.
func (p *parser) isPivotClauseStart() bool {
	if !p.IsKeyword(keywordPIVOT, keywordUNPIVOT) || p.Current().Quoted {
		return false
	}
	return p.Peek().Kind == tokenLeftParen || p.Peek().IsKeyword(tokenIdentifier, "INCLUDE") ||
		p.Peek().IsKeyword(tokenIdentifier, "EXCLUDE")
}

// parseTableSourceBody parses a table source without the PIVOT, UNPIVOT or TABLESAMPLE
// that may follow it.
//
// Takes joinKind (ast.JoinKind) which records how the source is joined into the FROM
// clause.
// Takes joins (*[]ast.JoinClause) which receives the joins inside a parenthesised join.
//
// Returns *ast.TableReference which is the base table, or nil when the source was
// recorded as a derived table or table-valued function.
// Returns error when the source fails to parse.
func (p *parser) parseTableSourceBody(joinKind ast.JoinKind, joins *[]ast.JoinClause) (*ast.TableReference, error) {
	if p.isSubqueryStart() {
		derivedAt := len(p.rawDerivedTables)
		if err := p.parseDerivedTable(joinKind); err != nil {
			return nil, err
		}
		if len(p.rawDerivedTables) > derivedAt {
			p.recordFromItem(p.rawDerivedTables[len(p.rawDerivedTables)-1].Alias)
		}
		return nil, nil
	}
	if p.Current().Kind == tokenLeftParen {
		return p.parseParenthesisedJoin(joinKind, joins)
	}
	if p.isTableValuedFunctionStart() {
		functionAt := len(p.rawTableValuedFunctions)
		p.parseTableValuedFunction(joinKind)
		if len(p.rawTableValuedFunctions) > functionAt {
			p.recordFromItem(p.rawTableValuedFunctions[functionAt].Alias)
		}
		return nil, nil
	}
	tableReference, columnNames, err := p.parseTableReference()
	if err != nil {
		return nil, err
	}
	if columnNames != nil {
		p.recordRenamedTable(tableReference, columnNames, joinKind)
		return nil, nil
	}
	if tableReference.Alias != "" {
		p.recordFromItem(tableReference.Alias)
	} else {
		p.recordFromItem(tableReference.Name)
	}
	return &tableReference, nil
}

// recordRenamedTable represents a table with a column alias list as a derived table
// selecting every source column. The aliases rename the leading columns and hide their
// original names.
//
// Takes table (ast.TableReference) which is the table and its alias.
// Takes columnNames ([]string) which is the column alias list.
// Takes joinKind (ast.JoinKind) which classifies the join context of the item.
func (p *parser) recordRenamedTable(table ast.TableReference, columnNames []string, joinKind ast.JoinKind) {
	inner := &ast.RawQueryAnalysis{}
	inner.FromTables = []ast.TableReference{{Schema: table.Schema, Name: table.Name, Alias: ""}}
	inner.FromOrder = []string{table.Name}
	inner.OutputColumns = []ast.RawOutputColumn{{Expression: nil, Name: "", TableAlias: "", ColumnName: "", IsStar: true}}
	inner.ReadOnly = true
	p.rawDerivedTables = append(p.rawDerivedTables, ast.RawDerivedTableReference{
		Alias:       table.Alias,
		ColumnNames: columnNames,
		InnerQuery:  inner,
		JoinKind:    joinKind,
		Using:       nil,
	})
	p.recordFromItem(table.Alias)
}

// parseParenthesisedJoin parses a parenthesised FROM item, flattening unaliased items and
// grouping aliased items as derived tables. DuckDB ignores the alias and flattens the
// item when its last join uses USING.
//
// Takes joinKind (ast.JoinKind) which classifies the join context of the item.
// Takes joins (*[]ast.JoinClause) which receives the joins inside the parentheses.
//
// Returns *ast.TableReference which is the first table inside the parentheses, or nil
// when it was a derived table or table-valued function, or when the alias names it.
// Returns error when the item is malformed or not closed.
func (p *parser) parseParenthesisedJoin(joinKind ast.JoinKind, joins *[]ast.JoinClause) (*ast.TableReference, error) {
	if p.analysisDepth >= p.maxParseDepth {
		depthErr := dialectkit.DepthError(p.maxParseDepth)
		p.recordSyntaxError(depthErr)
		return nil, depthErr
	}
	p.analysisDepth++
	defer func() { p.analysisDepth-- }()

	p.Advance()
	p.MatchKeyword(keywordLATERAL)
	start := p.markJoinGroup(len(*joins))
	lead, err := p.parseTableSource(joinKind, joins)
	if err != nil {
		return nil, err
	}
	lastJoinUsing := false
	for {
		innerKind, natural, isJoin := p.parseJoinKeyword()
		if !isJoin {
			break
		}
		joinStart := p.Position
		if err := p.appendExplicitJoin(innerKind, natural, joins); err != nil {
			return nil, err
		}
		lastJoinUsing = p.joinsUsing(joinStart, p.Position)
	}
	if p.Current().Kind != tokenRightParen {
		return nil, p.syntaxErrorAt("expected ')' to close the parenthesised join")
	}
	p.Advance()
	if !p.MatchKeyword(keywordAS) && !p.isBareTableAlias() {
		return lead, nil
	}
	aliasToken := p.Advance()
	if p.Current().Kind == tokenLeftParen {
		p.skipParenthesisedOrRecord()
	}
	if lastJoinUsing {
		p.statement.warn(aliasToken, "DuckDB ignores the alias "+aliasToken.Raw+" of a parenthesised join whose last join "+
			"merges columns with USING, so the tables inside keep their own names; join ON a condition to name the join")
		return lead, nil
	}
	p.groupAliasedJoin(ast.RawDerivedTableReference{Alias: aliasToken.Value, ColumnNames: nil, InnerQuery: nil,
		JoinKind: joinKind, Using: nil}, lead, joins, start)
	return nil, nil
}

// joinsUsing reports whether the join written between start and end merges columns with
// USING, which holds outside every parenthesis it opens.
//
// Takes start (int) which is the index of the join's first token after its keywords.
// Takes end (int) which is one past the index of its last token.
//
// Returns bool which is true for a join with a USING list.
func (p *parser) joinsUsing(start, end int) bool {
	depth := 0
	for index := start; index < end && index < len(p.Tokens); index++ {
		switch p.Tokens[index].Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		default:
			if depth == 0 && p.Tokens[index].IsKeyword(tokenIdentifier, keywordUSING) {
				return true
			}
		}
	}
	return false
}

// markJoinGroup records how much the parser has gathered.
//
// Takes joins (int) which is the number of joins of the FROM clause so far.
//
// Returns joinGroupMark which holds the counts.
func (p *parser) markJoinGroup(joins int) joinGroupMark {
	mark := joinGroupMark{
		joins:               joins,
		derivedTables:       len(p.rawDerivedTables),
		tableFunctions:      len(p.rawTableValuedFunctions),
		predicateSubqueries: len(p.predicateSubqueries),
		clauseExpressions:   len(p.clauseExpressions),
		parameters:          len(p.parameterReferences),
		fromItems:           0,
	}
	if p.fromOrder != nil {
		mark.fromItems = len(*p.fromOrder)
	}
	return mark
}

// groupAliasedJoin replaces a parenthesised join with a derived table selecting all its
// columns. It moves the grouped relations, conditions and FROM order into the inner query
// and copies their parameters while retaining every parameter on the statement.
//
// Takes reference (ast.RawDerivedTableReference) which holds the alias and the join kind
// of the item; its InnerQuery is set here.
// Takes lead (*ast.TableReference) which is the first table inside the parentheses, or
// nil when it was a derived table or a table function.
// Takes joins (*[]ast.JoinClause) which holds the FROM clause's joins.
// Takes start (joinGroupMark) which is what the parser had gathered when the parentheses
// opened.
func (p *parser) groupAliasedJoin(reference ast.RawDerivedTableReference, lead *ast.TableReference,
	joins *[]ast.JoinClause, start joinGroupMark) {
	inner := &ast.RawQueryAnalysis{}
	if lead != nil {
		inner.FromTables = []ast.TableReference{*lead}
	}
	inner.OutputColumns = []ast.RawOutputColumn{{Expression: nil, Name: "", TableAlias: "", ColumnName: "", IsStar: true}}
	inner.JoinClauses = slices.Clone((*joins)[start.joins:])
	*joins = (*joins)[:start.joins]
	inner.RawDerivedTables = slices.Clone(p.rawDerivedTables[start.derivedTables:])
	p.rawDerivedTables = p.rawDerivedTables[:start.derivedTables]
	inner.RawTableValuedFunctions = slices.Clone(p.rawTableValuedFunctions[start.tableFunctions:])
	p.rawTableValuedFunctions = p.rawTableValuedFunctions[:start.tableFunctions]
	inner.PredicateSubqueries = slices.Clone(p.predicateSubqueries[start.predicateSubqueries:])
	p.predicateSubqueries = p.predicateSubqueries[:start.predicateSubqueries]
	inner.ClauseExpressions = p.takeClauseExpressions(start.clauseExpressions)
	inner.ParameterReferences = slices.Clone(p.parameterReferences[start.parameters:])
	if p.fromOrder != nil {
		inner.FromOrder = slices.Clone((*p.fromOrder)[start.fromItems:])
		*p.fromOrder = (*p.fromOrder)[:start.fromItems]
	}
	inner.ReadOnly = true
	reference.InnerQuery = inner
	p.rawDerivedTables = append(p.rawDerivedTables, reference)
	p.recordFromItem(reference.Alias)
}

// parseJoinCondition consumes an optional ON predicate or USING column list following a
// JOIN clause, recording a malformed USING list as the statement's syntax error.
//
// Takes natural (bool) which is true for a NATURAL join.
//
// Returns *ast.JoinUsing which describes the columns a USING clause or a NATURAL join
// merges, each keeping the place the left side gives it as DuckDB places it, or nil for a
// join that merges none.
func (p *parser) parseJoinCondition(natural bool) *ast.JoinUsing {
	if p.MatchKeyword(keywordON) {
		p.parseCondition(ast.ClauseJoinCondition, expressionTerminatorKeywords)
		return nil
	}
	if p.MatchKeyword(keywordUSING) {
		if p.Current().Kind != tokenLeftParen {
			p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected '(' after USING"))
			return nil
		}
		columns, err := p.parseColumnList()
		if err != nil {
			p.recordSyntaxError(err)
			return nil
		}
		return &ast.JoinUsing{Columns: columns, Natural: false, Placement: ast.JoinColumnsInPlace}
	}
	if natural {
		return &ast.JoinUsing{Columns: nil, Natural: true, Placement: ast.JoinColumnsInPlace}
	}
	return nil
}

// parseTableReference parses a schema-qualified table reference with an optional alias
// and column alias list.
//
// Returns ast.TableReference which describes the parsed schema, name, and alias.
// Returns []string which is the column alias list, or nil when the alias has none.
// Returns error when no table name is present, the name is malformed or the column alias
// list is.
func (p *parser) parseTableReference() (ast.TableReference, []string, error) {
	if p.Current().Kind != tokenIdentifier {
		return ast.TableReference{}, nil, p.syntaxErrorAt("expected a table name")
	}

	schema, name, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		p.recordSyntaxError(nameError)
		return ast.TableReference{}, nil, nameError
	}

	if p.Current().Kind == tokenStar {
		p.Advance()
	}

	alias := p.parseTableAlias()
	var columnNames []string
	if alias != "" && p.Current().Kind == tokenLeftParen {
		names, err := p.parseColumnList()
		if err != nil {
			p.recordSyntaxError(err)
			return ast.TableReference{}, nil, err
		}
		columnNames = names
	}

	return ast.TableReference{Schema: schema, Name: name, Alias: alias}, columnNames, nil
}

// parseTableAlias consumes the alias of a FROM item, written with or without AS.
//
// Returns string which is the alias, or empty when there is none.
func (p *parser) parseTableAlias() string {
	if p.MatchKeyword(keywordAS) {
		if p.Current().Kind == tokenIdentifier {
			return p.Advance().Value
		}
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected an alias after AS"))
		return ""
	}
	if p.isBareTableAlias() {
		return p.Advance().Value
	}
	return ""
}

// isBareTableAlias reports whether the current token is an alias written without AS: an
// identifier that does not start a join or the next clause.
//
// Returns bool which is true for a bare alias.
func (p *parser) isBareTableAlias() bool {
	return p.Current().Kind == tokenIdentifier && !p.isJoinKeyword() && !p.isSelectTerminator() &&
		(p.Current().Quoted || !p.IsKeyword(fromItemFollowerKeywords...))
}

// skipTableSample consumes a TABLESAMPLE clause following a FROM item, such as
// TABLESAMPLE 10% or TABLESAMPLE reservoir(50 ROWS) REPEATABLE (42).
func (p *parser) skipTableSample() {
	if !p.MatchKeyword("TABLESAMPLE") {
		return
	}
	p.skipSampleSpecification()
}

// skipSampleSpecification consumes a sample specification: a size, such as 10%, 10
// PERCENT or 50 ROWS, or a method with its size in parentheses, optionally followed by a
// parenthesised method and seed and a REPEATABLE (seed).
func (p *parser) skipSampleSpecification() {
	if p.Current().Kind == tokenIdentifier && p.Peek().Kind == tokenLeftParen {
		p.Advance()
		p.skipParenthesisedOrRecord()
	} else {
		if p.Current().Kind != tokenNumber {
			p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected the sample size"))
			return
		}
		p.Advance()
		if !p.MatchKeyword("PERCENT") && !p.MatchKeyword(keywordROWS) &&
			p.Current().Kind == tokenOperator && p.Current().Value == "%" {
			p.Advance()
		}
	}
	if p.Current().Kind == tokenLeftParen {
		p.skipParenthesisedOrRecord()
	}
	if p.MatchKeyword("REPEATABLE") {
		p.skipParenthesisedOrRecord()
	}
}

// isSubqueryStart reports whether the cursor sits on a "(" that begins a subquery: a
// SELECT, WITH, VALUES, FROM-first, PIVOT or UNPIVOT query, or a parenthesised query that
// is itself one.
//
// Returns bool which is true when the parenthesis opens a subquery.
func (p *parser) isSubqueryStart() bool {
	if p.Current().Kind != tokenLeftParen {
		return false
	}
	index := p.Position
	for index < len(p.Tokens) && p.Tokens[index].Kind == tokenLeftParen {
		index++
	}
	if index >= len(p.Tokens) {
		return false
	}
	switch p.Tokens[index].Keyword(tokenIdentifier) {
	case keywordSELECT, keywordWITH, keywordVALUES, keywordPIVOT, keywordUNPIVOT, keywordPIVOTWIDER, keywordPIVOTLONGER:
		return true
	case keywordFROM:
		return index == p.Position+1
	default:
		return false
	}
}

// isTableValuedFunctionStart reports whether the cursor sits on an identifier followed by
// "(" that introduces a table-valued function.
//
// Returns bool which is true when the next tokens form a function call that is not
// SELECT, WITH, or VALUES.
func (p *parser) isTableValuedFunctionStart() bool {
	if p.Current().Kind != tokenIdentifier ||
		p.IsKeyword(keywordSELECT, keywordWITH, keywordVALUES, keywordPIVOT, keywordUNPIVOT) {
		return false
	}
	if p.Peek().Kind == tokenLeftParen {
		return true
	}

	if p.Peek().Kind != tokenDot {
		return false
	}
	return p.Position+schemaQualifiedCallLookahead < len(p.Tokens) &&
		p.Tokens[p.Position+2].Kind == tokenIdentifier &&
		p.Tokens[p.Position+schemaQualifiedCallLookahead].Kind == tokenLeftParen
}

// parseDerivedTable parses a parenthesised subquery and records it on the derived-table
// list with its alias, its column alias list and its join kind.
//
// Takes joinKind (ast.JoinKind) which records how the derived table is joined into the
// FROM clause.
//
// Returns error when the subquery fails to parse or analyse, or its column alias list is
// malformed.
func (p *parser) parseDerivedTable(joinKind ast.JoinKind) error {
	innerAnalysis, analyseError := p.analyseSubqueryBody()
	if analyseError != nil {
		return analyseError
	}

	alias := p.parseTableAlias()
	var columnNames []string
	if alias != "" && p.Current().Kind == tokenLeftParen {
		names, err := p.parseColumnList()
		if err != nil {
			p.recordSyntaxError(err)
			return err
		}
		columnNames = names
	}

	p.rawDerivedTables = append(p.rawDerivedTables, ast.RawDerivedTableReference{
		Alias:       alias,
		ColumnNames: columnNames,
		InnerQuery:  innerAnalysis,
		JoinKind:    joinKind,
		Using:       nil,
	})

	return nil
}

// parseTableValuedFunction parses a table-valued function call along with its optional
// WITH ORDINALITY, alias, and column definition list.
//
// Takes joinKind (ast.JoinKind) which records how the call is joined into the FROM
// clause.
func (p *parser) parseTableValuedFunction(joinKind ast.JoinKind) {
	schemaName, functionName := p.consumeTVFName()
	p.Advance()

	parameterCountBefore := p.parameterCount
	var arguments []ast.Expression
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		arguments = append(arguments, p.parseExpression())
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	p.markParametersAsFunctionArguments(parameterCountBefore)

	ordinality := p.parseWithOrdinality()

	alias := functionName
	var columnDefinitions []ast.TVFColumnDefinition
	if written := p.parseTableAlias(); written != "" {
		alias = written
	}
	if p.Current().Kind == tokenLeftParen {
		columnDefinitions = p.parseTVFColumnDefinitions()
	}

	qualifiedName := functionName
	if schemaName != "" {
		qualifiedName = schemaName + "." + functionName
	}

	p.rawTableValuedFunctions = append(p.rawTableValuedFunctions, ast.RawTableValuedFunctionReference{
		FunctionName:      qualifiedName,
		Arguments:         arguments,
		Alias:             alias,
		ColumnDefinitions: columnDefinitions,
		Ordinality:        ordinality,
		Functions:         nil,
		JoinKind:          joinKind,
	})
}

// parseWithOrdinality consumes the WITH ORDINALITY of a table function reference.
//
// Returns *ast.ScopedColumn which is the column it appends after the function's columns,
// named ordinality, BIGINT and never NULL, as DuckDB gives it, or nil when the reference
// is not written WITH ORDINALITY.
func (p *parser) parseWithOrdinality() *ast.ScopedColumn {
	if !p.IsKeyword(keywordWITH) || !p.Peek().IsKeyword(tokenIdentifier, keywordORDINALITY) {
		return nil
	}
	p.Advance()
	p.Advance()
	return &ast.ScopedColumn{Name: ordinalityColumnName, SQLType: builtinTypeMap["bigint"], Nullable: false}
}

// consumeTVFName consumes the optional schema qualifier and function identifier of a
// table-valued function call, leaving the cursor on the opening '('. The qualified name
// is recorded lower-cased so it matches the EnclosingFunctionName recorded on the call's
// argument placeholders and the name the shared resolver looks the function up by.
//
// Returns schemaName (string) which is the schema qualifier, or "" when the call was
// unqualified.
// Returns functionName (string) which is the lower-cased function identifier.
func (p *parser) consumeTVFName() (schemaName string, functionName string) {
	functionName = strings.ToLower(p.Advance().Value)
	if p.Current().Kind == tokenDot && p.Peek().Kind == tokenIdentifier {
		p.Advance()
		schemaName = functionName
		functionName = strings.ToLower(p.Advance().Value)
	}
	return schemaName, functionName
}

// parseTVFColumnDefinitions parses an explicit column definition list attached to a
// table-valued function alias.
//
// Returns []ast.TVFColumnDefinition which lists the parsed name and type pairs in
// declared order.
func (p *parser) parseTVFColumnDefinitions() []ast.TVFColumnDefinition {
	p.Advance()
	var definitions []ast.TVFColumnDefinition
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.Current().Kind != tokenIdentifier {
			break
		}
		name := p.Advance().Value
		var typeName string
		if p.Current().Kind == tokenIdentifier && p.Current().Kind != tokenRightParen {
			if !p.IsKeyword(",") && p.Current().Kind != tokenComma {
				typeName = p.parseCastTypeName()
			}
		}
		definitions = append(definitions, ast.TVFColumnDefinition{
			Name:     name,
			TypeName: typeName,
		})
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	return definitions
}

// parseJoinKeyword consumes a join prefix keyword along with optional NATURAL, OUTER, and
// JOIN tokens.
//
// Returns kind (ast.JoinKind) which is the kind of the consumed join.
// Returns natural (bool) which is true for a NATURAL join.
// Returns isJoin (bool) which is true when a join keyword was consumed.
func (p *parser) parseJoinKeyword() (kind ast.JoinKind, natural bool, isJoin bool) {
	if !p.isJoinKeyword() {
		return ast.JoinKind{}, false, false
	}
	natural = p.MatchKeyword("NATURAL")

	kind = ast.JoinInner
	switch {
	case p.MatchKeyword(keywordASOF):
		kind = joinAsof
		if p.MatchKeyword("LEFT") {
			kind = joinAsofLeft
			p.MatchKeyword("OUTER")
		} else {
			p.MatchKeyword("INNER")
		}
	case p.Current().Kind == tokenIdentifier:
		if entry, exists := joinKeywordDispatch[p.Current().Keyword(tokenIdentifier)]; exists {
			p.Advance()
			kind = entry.kind
			if entry.hasOuter {
				p.MatchKeyword("OUTER")
			}
		}
	}

	if _, err := p.ExpectKeyword(keywordJOIN); err != nil {
		p.recordSyntaxError(err)
	}
	return kind, natural, true
}

// isJoinKeyword reports whether the current token introduces a join clause that would
// terminate an alias scan.
//
// Returns bool which is true for any join-prefix keyword.
func (p *parser) isJoinKeyword() bool {
	return p.IsKeyword(keywordJOIN, "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "NATURAL", keywordPOSITIONAL,
		keywordASOF, "SEMI", "ANTI")
}
