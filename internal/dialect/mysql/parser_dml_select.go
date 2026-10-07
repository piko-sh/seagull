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
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// joinKeywordEntry captures how a leading join keyword maps to a join kind and whether
// the OUTER token may follow.
type joinKeywordEntry struct {
	// kind is the join kind selected when the keyword matches.
	kind ast.JoinKind

	// hasOuter records whether OUTER may follow the leading keyword.
	hasOuter bool
}

var (
	// joinKeywordDispatch maps the leading join-keyword token to its joinKeywordEntry.
	joinKeywordDispatch = map[string]joinKeywordEntry{
		"INNER": {kind: ast.JoinInner, hasOuter: false},
		"LEFT":  {kind: ast.JoinLeft, hasOuter: true},
		"RIGHT": {kind: ast.JoinRight, hasOuter: true},
		"CROSS": {kind: ast.JoinCross, hasOuter: false},
	}
)

// parseCTEList parses the comma-separated list of common-table-expression definitions
// following a WITH keyword.
//
// Returns []ast.RawCTEDefinition which is the parsed CTE list.
// Returns error when any CTE definition fails to parse.
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

// parseSingleCTEDefinition parses one CTE definition including its optional column list
// and parenthesised body.
//
// Takes isRecursive (bool) which is true when the surrounding WITH clause carried the
// RECURSIVE keyword.
//
// Returns ast.RawCTEDefinition which is the parsed definition.
// Returns error when the CTE name, body, or AS keyword cannot be parsed.
func (p *parser) parseSingleCTEDefinition(isRecursive bool) (ast.RawCTEDefinition, error) {
	cteName, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return ast.RawCTEDefinition{}, err
	}

	columnNames, columnListError := p.parseCTEColumnNames()
	if columnListError != nil {
		return ast.RawCTEDefinition{}, columnListError
	}

	if _, err := p.ExpectKeyword(keywordAS); err != nil {
		return ast.RawCTEDefinition{}, err
	}

	cteTokens, collectError := p.collectParenthesised()
	if collectError != nil {
		return ast.RawCTEDefinition{}, collectError
	}

	cteAnalysis, cteParser := p.analyseCTEBody(cteTokens)

	definition := ast.RawCTEDefinition{}
	definition.Name = cteName
	definition.IsRecursive = isRecursive

	if cteAnalysis != nil {
		p.populateCTEDefinition(&definition, cteAnalysis, columnNames)
	}

	p.parameterCount = cteParser.parameterCount
	p.parameterReferences = append(p.parameterReferences, cteParser.parameterReferences...)

	return definition, nil
}

// analyseCTEBody analyses the inner query of a CTE definition.
//
// Takes cteTokens ([]token) which is the inner CTE token slice.
//
// Returns *ast.RawQueryAnalysis which is the analysed query, or nil when analysis failed.
// Returns *parser which is the child parser used for the CTE body so the caller can
// splice its parameter state.
func (p *parser) analyseCTEBody(cteTokens []token) (*ast.RawQueryAnalysis, *parser) {
	cteParser := p.newNestedQueryParser(cteTokens)
	var cteAnalysis *ast.RawQueryAnalysis
	var analyseError error

	switch {
	case cteParser.IsKeyword("INSERT", "UPDATE", "DELETE", keywordREPLACE):
		p.hasDataModifyingCTE = true
		cteAnalysis, analyseError = p.analyseCTEBodyDML(cteParser)
	default:
		cteAnalysis, analyseError = cteParser.analyseSelect()
	}
	if analyseError == nil {
		analyseError = cteParser.requireEnd()
	}
	p.adoptSyntaxError(cteParser)

	if analyseError != nil {
		p.recordSyntaxError(analyseError)
		return nil, cteParser
	}
	return cteAnalysis, cteParser
}

// analyseCTEBodyDML dispatches to the relevant DML analyser for the body of a
// data-modifying CTE.
//
// Takes cteParser (*parser) which is the parser positioned at the CTE body tokens.
//
// Returns *ast.RawQueryAnalysis which is the analysed query.
// Returns error when the DML body cannot be analysed.
func (*parser) analyseCTEBodyDML(cteParser *parser) (*ast.RawQueryAnalysis, error) {
	switch {
	case cteParser.IsKeyword("INSERT"), cteParser.IsKeyword(keywordREPLACE):
		return dialectkit.ChangingRows(ast.DataChangeInsert, (*parser).analyseInsert)(cteParser)
	case cteParser.IsKeyword("UPDATE"):
		return dialectkit.ChangingRows(ast.DataChangeUpdate, (*parser).analyseUpdate)(cteParser)
	default:
		return dialectkit.ChangingRows(ast.DataChangeDelete, (*parser).analyseDelete)(cteParser)
	}
}

// parseCTEColumnNames parses the optional parenthesised column list that precedes the AS
// keyword in a CTE definition.
//
// Returns []string which is the parsed column list, or nil when absent.
// Returns error when the column list is malformed.
func (p *parser) parseCTEColumnNames() ([]string, error) {
	if p.Current().Kind != tokenLeftParen || p.IsKeyword(keywordAS) {
		return nil, nil
	}
	if p.peekForAS() {
		return nil, nil
	}
	return p.parseColumnList()
}

// populateCTEDefinition fills in the structural fields of a CTE definition from the
// analysed body.
//
// Takes definition (*ast.RawCTEDefinition) which receives the populated fields.
// Takes analysis (*ast.RawQueryAnalysis) which is the analysed CTE body.
// Takes columnNames ([]string) which is the explicit column list when present.
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

// peekForAS reports whether the parenthesised group at the current position is followed
// by an AS keyword.
//
// Returns bool which is true when no trailing AS keyword was found, used to disambiguate
// column lists from subqueries.
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

// parseOutputColumns parses the comma-separated SELECT projection list.
//
// Returns []ast.RawOutputColumn which is the parsed projection.
// Returns error when an individual column fails to parse.
func (p *parser) parseOutputColumns() ([]ast.RawOutputColumn, error) {
	var columns []ast.RawOutputColumn

	if p.insertProjectionColumns != nil {
		p.insertProjectionIndex = 0
	}

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
		items = append(items, dialectkit.TokenItemSpan(p.spans, p.Tokens, first, expressionEnd, p.Position, column.IsStar))

		if p.insertProjectionColumns != nil {
			p.insertProjectionIndex++
		}

		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	p.itemSpans = items
	return columns, nil
}

// parseOneOutputColumn parses a single projection entry, handling star, qualified star
// and expression-with-alias forms.
//
// Returns ast.RawOutputColumn which is the parsed column.
// Returns error when the alias clause is malformed.
func (p *parser) parseOneOutputColumn() (ast.RawOutputColumn, error) {
	if p.Current().Kind == tokenStar {
		p.Advance()
		return ast.RawOutputColumn{IsStar: true, Expression: nil, Name: "", TableAlias: "", ColumnName: ""}, nil
	}

	if p.Current().Kind == tokenIdentifier && p.Peek().Kind == tokenDot {
		tableAlias := p.Advance().Value
		p.Advance()
		if p.Current().Kind == tokenStar {
			p.Advance()
			return ast.RawOutputColumn{
				IsStar:     true,
				TableAlias: tableAlias,
				Expression: nil,
				Name:       "",
				ColumnName: "",
			}, nil
		}
		p.Position -= 2
	}

	if err := p.requireSelectItem(); err != nil {
		return ast.RawOutputColumn{}, err
	}
	expression := p.parseExpression()
	column := ast.RawOutputColumn{}
	p.expressionToOutputColumn(expression, &column)

	if err := p.parseOutputColumnAlias(&column); err != nil {
		return ast.RawOutputColumn{}, err
	}

	return column, nil
}

// parseOutputColumnAlias parses the optional alias clause on a projection entry: AS and a
// name, or a name or string written directly after the item, as in SELECT 1 'one'.
//
// Takes column (*ast.RawOutputColumn) which is updated with the parsed alias when one is
// present.
//
// Returns error when the AS keyword is followed by an unparseable identifier.
func (p *parser) parseOutputColumnAlias(column *ast.RawOutputColumn) error {
	p.aliasStart = p.Position
	if p.MatchKeyword(keywordAS) {
		alias, aliasError := p.parseIdentifierOrKeyword()
		if aliasError != nil {
			return aliasError
		}
		column.Name = alias
		return nil
	}
	if (p.Current().Kind == tokenIdentifier || p.Current().Kind == tokenString) && !p.isSelectTerminator() {
		column.Name = p.Advance().Value
	}
	return nil
}

// parseReturningClause parses the projection list of a RETURNING clause.
//
// Returns []ast.RawOutputColumn which is the parsed projection.
// Returns error when an individual column fails to parse.
func (p *parser) parseReturningClause() ([]ast.RawOutputColumn, error) {
	return p.parseOutputColumns()
}

// expressionToOutputColumn unpacks a parsed expression into the relevant
// projection-column fields based on its concrete kind.
//
// Takes expression (ast.Expression) which is the parsed projection expression.
// Takes column (*ast.RawOutputColumn) which receives the unpacked fields.
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

// isSelectTerminator reports whether the current token starts a clause that ends a SELECT
// projection list.
//
// Returns bool reporting whether the current keyword terminates the projection.
func (p *parser) isSelectTerminator() bool {
	return p.IsKeyword(keywordFROM, keywordWHERE, keywordGROUP, keywordHAVING, keywordORDER, keywordLIMIT, keywordOFFSET,
		keywordFOR, keywordUNION, keywordINTERSECT, keywordEXCEPT, keywordON, keywordRETURNING, "INTO", "WINDOW",
		"LOCK", "PROCEDURE")
}

// parseFromClause parses the FROM clause including comma-joined tables and explicit JOIN
// clauses.
//
// Returns []ast.TableReference which is the list of base tables.
// Returns []ast.JoinClause which is the list of explicit joins.
// Returns []string which holds the names the FROM items are visible under, in order.
// Returns error when a table source or join clause fails to parse.
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

// appendExplicitJoin parses an explicit JOIN clause including its optional join
// condition, and appends it to joins.
//
// When the joined source is a parenthesised join, its first table takes the outer join's
// kind and its own joins follow it; under a LEFT or RIGHT join the inner joins take the
// outer kind too, because every table inside the parentheses can then be missing.
//
// Takes joinKind (ast.JoinKind) which is the kind of join being parsed.
// Takes natural (bool) which is true for a NATURAL join.
// Takes joins (*[]ast.JoinClause) which is the slice to append the parsed join to.
//
// Returns error when the joined table source fails to parse.
func (p *parser) appendExplicitJoin(joinKind ast.JoinKind, natural bool, joins *[]ast.JoinClause) error {
	p.MatchKeyword(keywordLATERAL)

	insertAt := len(*joins)
	derivedAt := len(p.rawDerivedTables)
	tableReference, err := p.parseTableSource(joinKind, joins)
	if err != nil {
		return err
	}
	if joinKind == ast.JoinLeft || joinKind == ast.JoinRight {
		for index := insertAt; index < len(*joins); index++ {
			if (*joins)[index].Kind == ast.JoinInner || (*joins)[index].Kind == ast.JoinCross {
				(*joins)[index].Kind = joinKind
			}
		}
	}
	if tableReference != nil {
		*joins = slices.Insert(*joins, insertAt, ast.JoinClause{Kind: joinKind, Table: *tableReference, Using: nil})
	}

	using, err := p.parseJoinCondition(natural)
	if err != nil || using == nil {
		return err
	}
	switch {
	case tableReference != nil:
		(*joins)[insertAt].Using = using
	case len(p.rawDerivedTables) > derivedAt:
		p.rawDerivedTables[derivedAt].Using = using
	}
	return nil
}

// parseTableSource parses a single source appearing in a FROM clause, covering
// subqueries, parenthesised joins, table-valued functions and base tables.
//
// Takes joinKind (ast.JoinKind) which is the join kind to record on any derived or
// table-valued source.
// Takes joins (*[]ast.JoinClause) which receives the joins inside a parenthesised join.
//
// Returns *ast.TableReference which is the parsed base-table reference, or the first
// table of a parenthesised join, or nil when the source was captured as a derived table
// or table-valued function.
// Returns error when parsing the table source fails.
func (p *parser) parseTableSource(joinKind ast.JoinKind, joins *[]ast.JoinClause) (*ast.TableReference, error) {
	if p.isSubqueryStart() {
		derivedAt := len(p.rawDerivedTables)
		if err := p.parseDerivedTable(joinKind); err != nil {
			return nil, err
		}
		if len(p.rawDerivedTables) > derivedAt {
			p.recordFromItem(p.rawDerivedTables[derivedAt].Alias)
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
	if p.Current().Kind != tokenIdentifier {
		return nil, p.syntaxErrorAt("expected a table name")
	}
	reference, err := p.parseTableReference()
	if err != nil {
		return nil, err
	}
	if err := p.skipIndexHints(); err != nil {
		return nil, err
	}
	if reference.Alias != "" {
		p.recordFromItem(reference.Alias)
	} else {
		p.recordFromItem(reference.Name)
	}
	return &reference, nil
}

// recordFromItem records the name a FROM item is visible under, in FROM order.
//
// Takes name (string) which is the item's alias, or its name when it has none.
func (p *parser) recordFromItem(name string) {
	if p.fromOrder != nil && name != "" {
		*p.fromOrder = append(*p.fromOrder, name)
	}
}

// parseParenthesisedJoin parses a parenthesised FROM item, such as (a JOIN b ON ...) or
// (a), flattening it into the enclosing FROM clause: its first table is returned and its
// joins are appended to joins.
//
// Takes joinKind (ast.JoinKind) which classifies the join context of the item.
// Takes joins (*[]ast.JoinClause) which receives the joins inside the parentheses.
//
// Returns *ast.TableReference which is the first table inside the parentheses, or nil
// when it was a derived table or table-valued function.
// Returns error when the item is malformed or not closed.
func (p *parser) parseParenthesisedJoin(joinKind ast.JoinKind, joins *[]ast.JoinClause) (*ast.TableReference, error) {
	if err := p.enterAnalysis(); err != nil {
		p.recordSyntaxError(err)
		return nil, err
	}
	defer p.leaveAnalysis()

	p.Advance()
	p.MatchKeyword(keywordLATERAL)
	lead, err := p.parseTableSource(joinKind, joins)
	if err != nil {
		return nil, err
	}
	for {
		innerKind, natural, isJoin := p.parseJoinKeyword()
		if isJoin {
			if err := p.appendExplicitJoin(innerKind, natural, joins); err != nil {
				return nil, err
			}
			continue
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
		if err := p.appendParenthesisedCommaItem(joins); err != nil {
			return nil, err
		}
	}
	if p.Current().Kind != tokenRightParen {
		return nil, p.syntaxErrorAt("expected ')' to close the parenthesised join")
	}
	p.Advance()
	return lead, nil
}

// appendParenthesisedCommaItem parses a comma-separated item inside a parenthesised join,
// as in (a, b), and appends it as a cross join so its columns join the scope.
//
// Takes joins (*[]ast.JoinClause) which receives the item.
//
// Returns error when the item fails to parse.
func (p *parser) appendParenthesisedCommaItem(joins *[]ast.JoinClause) error {
	p.MatchKeyword(keywordLATERAL)
	insertAt := len(*joins)
	tableReference, err := p.parseTableSource(ast.JoinCross, joins)
	if err != nil {
		return err
	}
	if tableReference != nil {
		*joins = slices.Insert(*joins, insertAt, ast.JoinClause{Kind: ast.JoinCross, Table: *tableReference, Using: nil})
	}
	return nil
}

// skipIndexHints consumes MySQL index hint clauses such as USE INDEX and FORCE INDEX
// following a table reference.
//
// Returns error when a hint's index list parentheses are unmatched.
func (p *parser) skipIndexHints() error {
	for p.IsKeyword("USE", "FORCE", keywordIGNORE) {
		p.Advance()
		p.MatchKeyword("INDEX")
		p.MatchKeyword(keywordKEY)
		p.MatchKeyword(keywordFOR)
		p.MatchKeyword(keywordJOIN)
		p.MatchKeyword(keywordORDER)
		p.MatchKeyword(keywordBY)
		p.MatchKeyword(keywordGROUP)
		p.MatchKeyword(keywordBY)
		if err := p.skipParenthesisedIfPresent(); err != nil {
			return err
		}
	}
	return nil
}

// parseJoinCondition consumes an optional ON or USING clause following a JOIN.
//
// Takes natural (bool) which is true for a NATURAL join.
//
// Returns *ast.JoinUsing which describes the columns a USING clause or a NATURAL join
// merges, or nil for a join that merges none.
// Returns error when the USING column list is malformed.
func (p *parser) parseJoinCondition(natural bool) (*ast.JoinUsing, error) {
	if p.MatchKeyword(keywordON) {
		p.parseCondition(ast.ClauseJoinCondition, p.parseJoinConditionExpression)
		return nil, nil
	}
	if p.MatchKeyword(keywordUSING) {
		columns, err := p.parseUsingColumns()
		if err != nil {
			return nil, err
		}
		return &ast.JoinUsing{Columns: columns, Natural: false, Placement: ast.JoinColumnsLead}, nil
	}
	if natural {
		return &ast.JoinUsing{Columns: nil, Natural: true, Placement: ast.JoinColumnsLead}, nil
	}
	return nil, nil
}

// parseUsingColumns parses the parenthesised column list of a USING clause.
//
// Returns []string which holds the column names in order.
// Returns error when the list is not a parenthesised, comma-separated list of names.
func (p *parser) parseUsingColumns() ([]string, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, p.syntaxErrorAt("expected '(' after USING")
	}
	p.Advance()
	var columns []string
	for {
		column, err := p.parseIdentifierOrKeyword()
		if err != nil {
			p.recordSyntaxError(err)
			return nil, err
		}
		columns = append(columns, column)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenRightParen {
		return nil, p.syntaxErrorAt("unmatched parenthesis")
	}
	p.Advance()
	return columns, nil
}

// parseTableReference parses a schema-qualified table name with an optional alias.
//
// Returns ast.TableReference which is the parsed reference, or the zero value when the
// current token is not an identifier.
// Returns error when the qualified name is malformed.
func (p *parser) parseTableReference() (ast.TableReference, error) {
	if p.Current().Kind != tokenIdentifier {
		return ast.TableReference{}, nil
	}

	schema, name, err := p.parseSchemaQualifiedName()
	if err != nil {
		return ast.TableReference{}, err
	}
	if err := p.skipPartitionClause(); err != nil {
		return ast.TableReference{}, err
	}

	alias := ""
	if p.MatchKeyword(keywordAS) {
		if p.Current().Kind == tokenIdentifier {
			alias = p.Advance().Value
		}
	} else if p.Current().Kind == tokenIdentifier && !p.isJoinKeyword() && !p.isSelectTerminator() &&
		!p.IsKeyword(keywordSET, keywordVALUES, keywordDEFAULT, keywordWHERE, "INNER", "LEFT", "RIGHT",
			"CROSS", "NATURAL", keywordJOIN, keywordON, keywordUSING, keywordLATERAL,
			"STRAIGHT_JOIN", "USE", "FORCE", keywordIGNORE) {
		alias = p.Advance().Value
	}

	return ast.TableReference{Schema: schema, Name: name, Alias: alias}, nil
}

// isSubqueryStart reports whether the current parenthesised group opens a subquery rather
// than a column list or an expression.
//
// Returns bool reporting whether a subquery starts at the current position.
func (p *parser) isSubqueryStart() bool {
	return p.Current().Kind == tokenLeftParen && p.opensQueryAt(p.Position)
}

// opensQueryAt distinguishes a query-opening parenthesis from an expression beginning
// with a subquery. It recognises SELECT, WITH and VALUES, including nested query arms
// followed by set operations or trailing query clauses.
//
// Takes position (int) which is the index of a `(` token.
//
// Returns bool which is true when the parenthesis opens a query.
func (p *parser) opensQueryAt(position int) bool {
	next := position + 1
	if next >= len(p.Tokens) {
		return false
	}
	if p.Tokens[next].Kind != tokenLeftParen {
		return startsQuery(p.Tokens[next])
	}
	first := next
	for first < len(p.Tokens) && p.Tokens[first].Kind == tokenLeftParen {
		first++
	}
	if first >= len(p.Tokens) || !startsQuery(p.Tokens[first]) {
		return false
	}
	closing := p.matchingRightParen(next)
	if closing < 0 || closing+1 >= len(p.Tokens) {
		return false
	}
	after := p.Tokens[closing+1]
	return after.Kind == tokenRightParen ||
		after.IsKeyword(tokenIdentifier, keywordUNION) || after.IsKeyword(tokenIdentifier, keywordINTERSECT) ||
		after.IsKeyword(tokenIdentifier, keywordEXCEPT) || after.IsKeyword(tokenIdentifier, keywordORDER) ||
		after.IsKeyword(tokenIdentifier, keywordLIMIT)
}

// matchingRightParen returns the index of the `)` that closes the `(` at a position.
//
// Takes open (int) which is the index of a `(` token.
//
// Returns int which is the index of the matching `)`, or -1 when it is unmatched.
func (p *parser) matchingRightParen(open int) int {
	depth := 0
	for index := open; index < len(p.Tokens); index++ {
		if p.Tokens[index].Kind == tokenLeftParen {
			depth++
		}
		if p.Tokens[index].Kind == tokenRightParen {
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

// isTableValuedFunctionStart reports whether the current position starts a table-valued
// function invocation.
//
// Returns bool reporting whether the lookahead matches a function call.
func (p *parser) isTableValuedFunctionStart() bool {
	return p.Current().Kind == tokenIdentifier && p.Peek().Kind == tokenLeftParen &&
		!p.IsKeyword(keywordSELECT, keywordWITH, keywordVALUES)
}

// parseDerivedTable parses a parenthesised subquery used as a derived table, including
// its optional alias and column list.
//
// Takes joinKind (ast.JoinKind) which is the join kind to record on the derived table.
//
// Returns error when the inner query fails to analyse.
func (p *parser) parseDerivedTable(joinKind ast.JoinKind) error {
	innerTokens, collectError := p.collectParenthesised()
	if collectError != nil {
		return collectError
	}

	innerAnalysis, analyseError := p.analyseNestedSelect(innerTokens)
	if analyseError != nil {
		return analyseError
	}

	alias := ""
	if p.MatchKeyword(keywordAS) {
		if p.Current().Kind == tokenIdentifier {
			alias = p.Advance().Value
		}
	} else if p.Current().Kind == tokenIdentifier && !p.isJoinKeyword() && !p.isSelectTerminator() {
		alias = p.Advance().Value
	}

	var columnNames []string
	if p.Current().Kind == tokenLeftParen {
		start := p.Position
		if err := p.skipParenthesised(); err != nil {
			return err
		}
		p.Position = start
		names, err := p.parseColumnList()
		if err != nil {
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

// parseTableValuedFunction parses a table-valued function invocation in a FROM clause
// along with any alias and column-definition list.
//
// Takes joinKind (ast.JoinKind) which is the join kind to record on the function
// reference.
func (p *parser) parseTableValuedFunction(joinKind ast.JoinKind) {
	functionName := strings.ToLower(p.Advance().Value)
	p.Advance()

	referenceCountBefore := len(p.parameterReferences)
	var argumentBoundaries []int
	var columnDefinitions []ast.TVFColumnDefinition
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		p.parseExpression()
		argumentBoundaries = append(argumentBoundaries, len(p.parameterReferences))
		if p.IsKeyword("COLUMNS") && p.Peek().Kind == tokenLeftParen {
			p.Advance()
			columnDefinitions = p.parseJSONTableColumns(columnDefinitions)
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenRightParen {
		p.reportSyntaxError("expected ')' to close the arguments of " + functionName)
	} else {
		p.Advance()
	}

	p.markParametersAsFunctionArguments(referenceCountBefore, functionName, argumentBoundaries)

	alias := functionName
	if p.MatchKeyword(keywordAS) {
		if p.Current().Kind == tokenIdentifier {
			alias = p.Advance().Value
		}
		if p.Current().Kind == tokenLeftParen {
			columnDefinitions = p.parseTVFColumnDefinitions()
		}
	} else if p.Current().Kind == tokenIdentifier && !p.isJoinKeyword() && !p.isSelectTerminator() {
		alias = p.Advance().Value
	}

	p.rawTableValuedFunctions = append(p.rawTableValuedFunctions, ast.RawTableValuedFunctionReference{
		FunctionName:      functionName,
		Arguments:         nil,
		Alias:             alias,
		ColumnDefinitions: columnDefinitions,
		Ordinality:        nil,
		Functions:         nil,
		JoinKind:          joinKind,
	})
}

// parseJSONTableColumns parses the parenthesised COLUMNS clause of JSON_TABLE, flattening
// NESTED PATH clauses, and appends a definition for every output column: a typed PATH or
// EXISTS PATH column takes its declared type, and a FOR ORDINALITY column is an unsigned
// integer.
//
// Each NESTED PATH counts one level against the parse depth limit, and a clause nested
// past it records an error wrapping dialectkit.ErrParseDepth.
//
// Takes definitions ([]ast.TVFColumnDefinition) which are the columns found so far.
//
// Returns []ast.TVFColumnDefinition which is definitions with the clause's columns
// appended.
func (p *parser) parseJSONTableColumns(definitions []ast.TVFColumnDefinition) []ast.TVFColumnDefinition {
	if err := p.enterAnalysis(); err != nil {
		p.recordSyntaxError(err)
		return definitions
	}
	defer p.leaveAnalysis()

	p.Advance()
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.MatchKeyword("NESTED") {
			p.MatchKeyword("PATH")
			p.Advance()
			if p.IsKeyword("COLUMNS") && p.Peek().Kind == tokenLeftParen {
				p.Advance()
				definitions = p.parseJSONTableColumns(definitions)
			}
		} else {
			definitions = p.parseJSONTableColumn(definitions)
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenRightParen {
		p.reportSyntaxError("expected ')' to close the COLUMNS clause")
		return definitions
	}
	p.Advance()
	return definitions
}

// parseJSONTableColumn parses one JSON_TABLE column, name FOR ORDINALITY or name type
// [EXISTS] PATH 'path' with its ON EMPTY and ON ERROR handlers, and appends it. A type's
// numeric modifiers stay in its name, as appendTypeModifiers writes them.
//
// Takes definitions ([]ast.TVFColumnDefinition) which are the columns found so far.
//
// Returns []ast.TVFColumnDefinition which is definitions with the column appended.
func (p *parser) parseJSONTableColumn(definitions []ast.TVFColumnDefinition) []ast.TVFColumnDefinition {
	if p.Current().Kind != tokenIdentifier {
		p.reportSyntaxError("expected a JSON_TABLE column name")
		return definitions
	}
	name := p.Advance().Value
	if p.MatchKeyword(keywordFOR) {
		p.MatchKeyword("ORDINALITY")
		return append(definitions, ast.TVFColumnDefinition{Name: name, TypeName: "int unsigned"})
	}
	var typeName strings.Builder
	typeName.WriteString(p.parseTypeNameWords(keywordEXISTS, "PATH"))
	if err := p.appendTypeModifiers(&typeName); err != nil {
		p.recordSyntaxError(fmt.Errorf("parsing CAST type modifiers: %w", err))
	}
	p.MatchKeyword(keywordEXISTS)
	if p.MatchKeyword("PATH") && p.Current().Kind == tokenString {
		p.Advance()
	}
	p.skipJSONValueHandlers()
	return append(definitions, ast.TVFColumnDefinition{Name: name, TypeName: typeName.String()})
}

// parseTVFColumnDefinitions parses the parenthesised column-definition list that may
// follow a table-valued function alias.
//
// Returns []ast.TVFColumnDefinition which is the parsed list.
func (p *parser) parseTVFColumnDefinitions() []ast.TVFColumnDefinition {
	p.Advance()
	var definitions []ast.TVFColumnDefinition
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.Current().Kind != tokenIdentifier {
			break
		}
		name := p.Advance().Value
		var typeName string
		if p.Current().Kind == tokenIdentifier {
			typeName = p.parseColumnListTypeName()
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

// parseJoinKeyword consumes the keyword sequence that opens a JOIN clause.
//
// Returns kind (ast.JoinKind) which is the kind of join recognised.
// Returns natural (bool) which is true for a NATURAL join.
// Returns isJoin (bool) which reports whether a join keyword sequence was consumed.
func (p *parser) parseJoinKeyword() (kind ast.JoinKind, natural bool, isJoin bool) {
	natural = p.MatchKeyword("NATURAL")

	if p.MatchKeyword("STRAIGHT_JOIN") {
		return ast.JoinInner, natural, true
	}

	if p.Current().Kind == tokenIdentifier {
		if entry, exists := joinKeywordDispatch[p.Current().Keyword(tokenIdentifier)]; exists {
			p.Advance()
			if entry.hasOuter {
				p.MatchKeyword("OUTER")
			}
			p.MatchKeyword(keywordJOIN)
			return entry.kind, natural, true
		}
	}

	if p.MatchKeyword(keywordJOIN) {
		return ast.JoinInner, natural, true
	}

	return ast.JoinKind{}, false, false
}

// isJoinKeyword reports whether the current token introduces a JOIN clause.
//
// Returns bool reporting whether the current token is a join keyword.
func (p *parser) isJoinKeyword() bool {
	return p.IsKeyword(keywordJOIN, "INNER", "LEFT", "RIGHT", "CROSS", "NATURAL", "STRAIGHT_JOIN")
}

// parseValuesFirstRow parses the first row of a VALUES statement, naming the resulting
// columns column1, column2, and so on.
//
// Returns []ast.RawOutputColumn which is the synthesised projection inferred from the
// row.
func (p *parser) parseValuesFirstRow() []ast.RawOutputColumn {
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

// startsQuery reports whether a token is a word a query starts with: SELECT, WITH or
// VALUES.
//
// Takes current (token) which is the token.
//
// Returns bool which is true for a word a query starts with.
func startsQuery(current token) bool {
	return current.IsKeyword(tokenIdentifier, keywordSELECT) || current.IsKeyword(tokenIdentifier, keywordWITH) ||
		current.IsKeyword(tokenIdentifier, keywordVALUES)
}
