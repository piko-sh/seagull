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
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// schemaQualifiedCallLookahead is the index offset from the cursor at which a
	// schema-qualified table-valued function call's opening paren must sit.
	//
	// The layout is ident(0) . ident(2) ((3). The pre-check guards against tokens that fall
	// off the end of the stream.
	schemaQualifiedCallLookahead = 3

	// maxQualifiedStarNames is the most names a qualified star in a select list carries
	// before its star: the database, the schema and the table, as in db.schema.table.*.
	maxQualifiedStarNames = 3
)

var (
	// asOfSystemTimeKeywords are the keywords that open an AS OF SYSTEM TIME clause.
	asOfSystemTimeKeywords = []string{keywordAS, "OF", "SYSTEM", keywordTIME}

	// queryContinuations lists the keywords that may follow a query in parentheses that
	// starts a longer query: a set operator, or the ORDER BY, LIMIT, OFFSET or FETCH clause
	// that applies to the whole query.
	queryContinuations = map[string]struct{}{
		keywordUNION: {}, keywordINTERSECT: {}, keywordEXCEPT: {}, keywordORDER: {}, keywordLIMIT: {},
		keywordOFFSET: {}, keywordFETCH: {},
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

// joinKeywordEntry maps a join keyword to its join kind and OUTER flag.
type joinKeywordEntry struct {
	// kind classifies the join produced by the keyword.
	kind ast.JoinKind

	// hasOuter indicates whether the keyword may be followed by OUTER.
	hasOuter bool
}

var (
	// joinKeywordDispatch maps JOIN-introducer keywords to their join entries.
	joinKeywordDispatch = map[string]joinKeywordEntry{
		"INNER": {kind: ast.JoinInner, hasOuter: false},
		"LEFT":  {kind: ast.JoinLeft, hasOuter: true},
		"RIGHT": {kind: ast.JoinRight, hasOuter: true},
		"FULL":  {kind: ast.JoinFull, hasOuter: true},
		"CROSS": {kind: ast.JoinCross, hasOuter: false},
	}
)

// parseCTEList parses the WITH list of common table expressions.
//
// Returns []ast.RawCTEDefinition which holds the parsed CTE entries.
// Returns error when a CTE definition fails to parse.
func (p *parser) parseCTEList() ([]ast.RawCTEDefinition, error) {
	if err := p.requireKeyword(keywordWITH); err != nil {
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

// parseSingleCTEDefinition parses one CTE definition within a WITH list.
//
// Takes isRecursive (bool) which indicates whether WITH RECURSIVE was given.
//
// Returns ast.RawCTEDefinition which describes the parsed CTE.
// Returns error when the CTE name, column list, or body fail to parse.
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

	cteAnalysis, cteParser := p.analyseCTEBody(cteTokens)

	definition := ast.RawCTEDefinition{}
	definition.Name = cteName
	definition.IsRecursive = isRecursive

	if cteAnalysis != nil {
		p.populateCTEDefinition(&definition, cteAnalysis, columnNames)
	}

	p.parameterCount = cteParser.parameterCount
	p.parameterReferences = append(p.parameterReferences, cteParser.parameterReferences...)
	for number, name := range cteParser.argumentNames {
		p.recordArgumentName(number, name)
	}

	return definition, nil
}

// skipMaterialisationHint consumes an optional MATERIALIZED / NOT MATERIALIZED CTE hint.
func (p *parser) skipMaterialisationHint() {
	p.MatchKeyword("MATERIALIZED")
	if p.MatchKeyword(keywordNOT) {
		p.MatchKeyword("MATERIALIZED")
	}
}

// analyseCTEBody runs the appropriate analyser over a CTE token slice, in a parser that
// continues this parser's parameter numbering and shares its named parameters, so a :name
// written in several CTE bodies and the main query is one parameter.
//
// Takes cteTokens ([]token) which is the body of the CTE between parentheses.
//
// Returns *ast.RawQueryAnalysis which is the analysis result, or nil when parsing fails.
// Returns *parser which is the child parser used so callers can inherit parameter state.
func (p *parser) analyseCTEBody(cteTokens []token) (*ast.RawQueryAnalysis, *parser) {
	cteParser := p.newChildParser(cteTokens)
	cteParser.namedParameterMap = p.namedParameterMap
	var cteAnalysis *ast.RawQueryAnalysis
	var analyseErr error

	switch {
	case cteParser.IsKeyword("UPDATE", "DELETE") || cteParser.isInsertKeyword():
		p.hasDataModifyingCTE = true
		cteAnalysis, analyseErr = p.analyseCTEBodyDML(cteParser)
	default:
		cteAnalysis, analyseErr = cteParser.analyseSelect()
	}
	if analyseErr == nil {
		analyseErr = cteParser.requireEnd()
	}

	if analyseErr != nil {
		_ = p.recordSyntaxError(analyseErr)
		return nil, cteParser
	}
	return cteAnalysis, cteParser
}

// analyseCTEBodyDML dispatches the CTE body to the matching DML analyser.
//
// Takes cteParser (*parser) which holds the CTE body tokens.
//
// Returns *ast.RawQueryAnalysis which is the DML analysis result.
// Returns error when the underlying analyser fails.
func (*parser) analyseCTEBodyDML(cteParser *parser) (*ast.RawQueryAnalysis, error) {
	switch {
	case cteParser.isInsertKeyword():
		return dialectkit.ChangingRows(ast.DataChangeInsert, (*parser).analyseInsert)(cteParser)
	case cteParser.IsKeyword("UPDATE"):
		return dialectkit.ChangingRows(ast.DataChangeUpdate, (*parser).analyseUpdate)(cteParser)
	default:
		return dialectkit.ChangingRows(ast.DataChangeDelete, (*parser).analyseDelete)(cteParser)
	}
}

// parseCTEColumnNames parses the optional column-name list before AS.
//
// Returns []string which is the parsed column-name list, or nil when absent.
// Returns error when the column list fails to parse.
func (p *parser) parseCTEColumnNames() ([]string, error) {
	if p.Current().Kind != tokenLeftParen || p.IsKeyword(keywordAS) {
		return nil, nil
	}
	if p.peekForAS() {
		return nil, nil
	}
	return p.parseColumnList()
}

// populateCTEDefinition fills the CTE definition fields from its analysis.
//
// Takes definition (*ast.RawCTEDefinition) which is the target to populate.
// Takes analysis (*ast.RawQueryAnalysis) which provides the source columns and clauses.
// Takes columnNames ([]string) which optionally overrides the inferred output column
// names.
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

// peekForAS reports whether the matching ')' is followed by an AS keyword.
//
// Returns bool which is true when AS does not follow the matched ')'.
func (p *parser) peekForAS() bool {
	saved := p.Position
	depth := 0
	for index := p.Position; index < len(p.Tokens); index++ {
		candidate := p.Tokens[index]
		if candidate.Kind == tokenLeftParen {
			depth++
			continue
		}
		if candidate.Kind != tokenRightParen {
			continue
		}
		depth--
		if depth != 0 {
			continue
		}
		if index+1 < len(p.Tokens) && p.Tokens[index+1].IsKeyword(tokenIdentifier, keywordAS) {
			return false
		}
		break
	}
	p.Position = saved
	return true
}

// parseColumnList parses a parenthesised comma-separated list of names.
//
// Returns []string which is the parsed name list.
// Returns error when an opening '(' is missing or a name fails to parse.
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

	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	return names, nil
}

// parseOutputColumns parses the SELECT or RETURNING output column list.
//
// Returns []ast.RawOutputColumn which holds the parsed columns.
// Returns error when a column fails to parse.
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

// parseOneOutputColumn parses a single output column including aliases.
//
// Returns ast.RawOutputColumn which describes the parsed column.
// Returns error when alias parsing fails.
func (p *parser) parseOneOutputColumn() (ast.RawOutputColumn, error) {
	if column, ok, aliasErr := p.parseInsertProjectionParameterColumn(); ok {
		return column, aliasErr
	}

	if p.Current().Kind == tokenStar {
		p.Advance()
		return ast.RawOutputColumn{IsStar: true, Expression: nil, Name: "", TableAlias: "", ColumnName: ""}, nil
	}

	if tableAlias, isStar := p.parseQualifiedStar(); isStar {
		return ast.RawOutputColumn{
			IsStar:     true,
			TableAlias: tableAlias,
			Expression: nil,
			Name:       "",
			ColumnName: "",
		}, nil
	}

	expression := p.parseExpression()
	column := ast.RawOutputColumn{}
	p.expressionToOutputColumn(expression, &column)

	if err := p.parseOutputColumnAlias(&column); err != nil {
		return ast.RawOutputColumn{}, err
	}

	return column, nil
}

// parseQualifiedStar consumes a qualified star at the cursor, table.* or schema.table.*,
// whose relation is the one its table names, as a reference to a column written with its
// schema reads it.
//
// Returns string which is the table the star qualifies.
// Returns bool which is false, consuming nothing, when no qualified star is at the
// cursor.
func (p *parser) parseQualifiedStar() (string, bool) {
	start := p.Position
	var names []string
	for p.Current().Kind == tokenIdentifier && p.Peek().Kind == tokenDot && len(names) < maxQualifiedStarNames {
		names = append(names, p.Advance().Value)
		p.Advance()
	}
	if len(names) == 0 || p.Current().Kind != tokenStar {
		p.Position = start
		return "", false
	}
	p.Advance()
	return names[len(names)-1], true
}

// parseOutputColumnAlias attaches an explicit or implicit alias to column.
//
// Takes column (*ast.RawOutputColumn) which is the column to mutate.
//
// Returns error when the explicit alias identifier fails to parse.
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

// parseInsertProjectionParameterColumn handles an INSERT ... SELECT projection item that
// is itself a bare parameter with an optional inline cast.
//
// It attaches the positional INSERT target column reference and the Assignment context so
// the resolver types the placeholder from the target column. This mirrors the VALUES
// path, which binds a bare-parameter value element to its target column via
// columnReferenceForIndex. Items that are not a direct bare parameter, such as a literal,
// a column, or a function call wrapping a parameter, fall through to the generic
// expression parser so a nested placeholder is still classified as a function argument
// rather than an assignment.
//
// Returns ast.RawOutputColumn which is the parsed projection column when the item was a
// bare parameter.
// Returns bool which is true when a bare-parameter projection item was consumed.
// Returns error which is non-nil when the optional column alias failed to parse.
func (p *parser) parseInsertProjectionParameterColumn() (ast.RawOutputColumn, bool, error) {
	if p.insertProjectionColumns == nil || !isParameterToken(p.Current().Kind) {
		return ast.RawOutputColumn{}, false, nil
	}

	columnReference := p.columnReferenceForIndex(p.insertProjectionTable, p.insertProjectionColumns, p.insertProjectionIndex)
	if columnReference == nil {
		return ast.RawOutputColumn{}, false, nil
	}

	parameterToken := p.Current()
	p.Advance()

	var castType *sqlmodel.SQLType
	if p.Current().Kind == tokenCast {
		castType = p.consumeInlineCast()
	}

	p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextAssignment, columnReference, castType)

	column := ast.RawOutputColumn{
		Expression: &ast.UnknownExpression{},
		Name:       "",
		TableAlias: "",
		ColumnName: "",
		IsStar:     false,
	}
	if aliasErr := p.parseOutputColumnAlias(&column); aliasErr != nil {
		return ast.RawOutputColumn{}, true, aliasErr
	}
	return column, true, nil
}

// parseReturningClause parses a RETURNING column list.
//
// Returns []ast.RawOutputColumn which holds the returned columns.
// Returns error when an output column fails to parse.
func (p *parser) parseReturningClause() ([]ast.RawOutputColumn, error) {
	return p.parseOutputColumns()
}

// expressionToOutputColumn copies expression metadata onto column: a column reference and
// a function call name the column, as does a composite field selection, after its field.
//
// Takes expression (ast.Expression) which is the parsed expression.
// Takes column (*ast.RawOutputColumn) which is the column to populate.
func (*parser) expressionToOutputColumn(expression ast.Expression, column *ast.RawOutputColumn) {
	switch typed := expression.(type) {
	case *ast.ColumnRefExpression:
		column.Name = typed.ColumnName
		column.TableAlias = typed.TableAlias
		column.ColumnName = typed.ColumnName
	case *ast.FunctionCallExpression:
		column.Name = typed.FunctionName
		column.Expression = expression
	case *ast.StructFieldAccessExpression:
		column.Name = typed.FieldName
		column.Expression = expression
	default:
		column.Expression = expression
	}
}

// isSelectTerminator reports whether the current token ends a SELECT list.
//
// Returns bool which is true when the current keyword terminates the list.
func (p *parser) isSelectTerminator() bool {
	return p.IsKeyword(keywordFROM, keywordWHERE, keywordGROUP, keywordHAVING, keywordORDER, keywordLIMIT, keywordOFFSET,
		keywordFETCH, keywordFOR, keywordUNION, keywordINTERSECT, keywordEXCEPT, keywordON, keywordRETURNING, "INTO", "WINDOW")
}

// parseFromClause parses a FROM clause including comma joins and explicit joins.
//
// Returns []ast.TableReference which holds the parsed table sources.
// Returns []ast.JoinClause which holds the parsed explicit joins.
// Returns []string which holds the names the FROM items are visible under, in order.
// Returns error when a table source or join fails to parse.
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

// appendExplicitJoin parses an explicit JOIN and appends it to joins.
//
// When the joined source is a parenthesised join, its first table takes the outer join's
// kind and its own joins follow it; under a LEFT or FULL join the inner joins become
// outer too, because every table inside the parentheses can then be missing.
//
// Takes joinKind (ast.JoinKind) which classifies the join.
// Takes natural (bool) which is true for a NATURAL join.
// Takes joins (*[]ast.JoinClause) which is the slice to grow.
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

// parseTableSource parses a derived table, table-valued function, parenthesised join or
// table.
//
// Takes joinKind (ast.JoinKind) which classifies the join context.
// Takes joins (*[]ast.JoinClause) which receives the joins inside a parenthesised join.
//
// Returns *ast.TableReference which is the parsed table reference, or the first table of
// a parenthesised join, or nil when the source was a derived table or TVF and was tracked
// elsewhere.
// Returns error when the source fails to parse.
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
	if p.isRowsFromStart() {
		return nil, p.parseRowsFromSource(joinKind)
	}
	if p.isTableValuedFunctionStart() || p.isSQLValueFunctionStart() {
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

// recordFromItem records the name a FROM item is visible under, in FROM order.
//
// Takes name (string) which is the item's alias, or its name when it has none.
func (p *parser) recordFromItem(name string) {
	if p.fromOrder != nil && name != "" {
		*p.fromOrder = append(*p.fromOrder, name)
	}
}

// parseParenthesisedJoin parses a parenthesised FROM item, flattening unaliased items
// into the enclosing FROM clause. An alias hides the inner names and groups the item as a
// derived table through groupAliasedJoin.
//
// Takes joinKind (ast.JoinKind) which classifies the join context of the item.
// Takes joins (*[]ast.JoinClause) which receives the joins inside the parentheses.
//
// Returns *ast.TableReference which is the first table inside the parentheses, or nil
// when it was a derived table or TVF, or when the item has an alias.
// Returns error when the item is malformed or not closed.
func (p *parser) parseParenthesisedJoin(joinKind ast.JoinKind, joins *[]ast.JoinClause) (*ast.TableReference, error) {
	if p.analysisDepth >= p.maxParseDepth {
		return nil, p.recordSyntaxError(dialectkit.DepthError(p.maxParseDepth))
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
	for {
		innerKind, natural, isJoin := p.parseJoinKeyword()
		if !isJoin {
			break
		}
		if err := p.appendExplicitJoin(innerKind, natural, joins); err != nil {
			return nil, err
		}
	}
	if p.Current().Kind != tokenRightParen {
		return nil, p.syntaxError("expected ')' to close the parenthesised join")
	}
	p.Advance()
	alias, columnNames, err := p.parseFromItemAlias()
	if err != nil || alias == "" {
		return lead, err
	}
	p.groupAliasedJoin(ast.RawDerivedTableReference{Alias: alias, ColumnNames: columnNames, InnerQuery: nil,
		JoinKind: joinKind, Using: nil}, lead, joins, start)
	return nil, nil
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
// Takes reference (ast.RawDerivedTableReference) which holds the alias, its column list
// and the join kind of the item; its InnerQuery is set here.
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

// parseFromItemAlias consumes an optional alias, with an optional column alias list, that
// follows a parenthesised FROM item.
//
// Returns string which is the alias, or empty when there is none.
// Returns []string which holds the column alias list, or nil when there is none.
// Returns error when the column alias list is malformed.
func (p *parser) parseFromItemAlias() (string, []string, error) {
	if !p.matchFromAlias() && !p.isBareTableAlias() {
		return "", nil, nil
	}
	if p.Current().Kind != tokenIdentifier {
		return "", nil, nil
	}
	alias := p.Advance().Value
	if p.Current().Kind != tokenLeftParen {
		return alias, nil, nil
	}
	columnNames, err := p.parseColumnAliasList()
	return alias, columnNames, err
}

// parseColumnAliasList parses the column alias list written after the alias of a FROM
// item, (a, b, ...), recording a syntax error when its parentheses do not balance.
//
// Returns []string which holds the names in order.
// Returns error when the list is unbalanced or a name is malformed.
func (p *parser) parseColumnAliasList() ([]string, error) {
	start := p.Position
	if err := p.requireSkipParenthesised(); err != nil {
		return nil, err
	}
	p.Position = start
	names, err := p.parseColumnList()
	return names, p.recordSyntaxError(err)
}

// parseJoinCondition consumes an ON predicate or a USING column list.
//
// Takes natural (bool) which is true for a NATURAL join.
//
// Returns *ast.JoinUsing which describes the columns a USING clause or a NATURAL join
// merges, or nil for a join that merges none.
// Returns error when a USING column list is malformed.
func (p *parser) parseJoinCondition(natural bool) (*ast.JoinUsing, error) {
	if p.MatchKeyword(keywordON) {
		p.parseCondition(ast.ClauseJoinCondition)
		return nil, nil
	}
	if p.MatchKeyword(keywordUSING) {
		columns, err := p.parseUsingColumns()
		if err != nil {
			return nil, err
		}
		if p.matchFromAlias() {
			if _, err := p.requireIdentifierOrKeyword(); err != nil {
				return nil, err
			}
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
		return nil, p.syntaxError("expected '(' after USING")
	}
	p.Advance()
	var columns []string
	for {
		column, err := p.requireIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		columns = append(columns, column)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenRightParen {
		return nil, p.syntaxError("unmatched parenthesis")
	}
	p.Advance()
	return columns, nil
}

// parseTableReference parses a schema-qualified table name with an optional ONLY prefix
// or `*` suffix, an optional alias with a column alias list, and an optional TABLESAMPLE
// clause.
//
// Returns ast.TableReference which describes the parsed reference.
// Returns []string which holds the column alias list, or nil when there is none.
// Returns error when no table name is present or the reference is malformed.
func (p *parser) parseTableReference() (ast.TableReference, []string, error) {
	if p.IsKeyword("ONLY") && p.Peek().Kind == tokenIdentifier {
		p.Advance()
	}
	if p.Current().Kind != tokenIdentifier {
		return ast.TableReference{}, nil, p.syntaxError("expected a table name")
	}

	schema, name, err := p.requireSchemaQualifiedName()
	if err != nil {
		return ast.TableReference{}, nil, err
	}

	if p.Current().Kind == tokenStar {
		p.Advance()
	}

	alias := ""
	if p.matchFromAlias() {
		if p.Current().Kind == tokenIdentifier {
			alias = p.Advance().Value
		}
	} else if p.isBareTableAlias() {
		alias = p.Advance().Value
	}
	var columnNames []string
	if alias != "" && p.Current().Kind == tokenLeftParen {
		names, err := p.parseColumnAliasList()
		if err != nil {
			return ast.TableReference{}, nil, err
		}
		columnNames = names
	}

	if err := p.skipTableSample(); err != nil {
		return ast.TableReference{}, nil, err
	}

	return ast.TableReference{Schema: schema, Name: name, Alias: alias}, columnNames, nil
}

// matchFromAlias consumes the AS that introduces the alias of a FROM item, unless it
// starts an AS OF SYSTEM TIME clause.
//
// Returns bool which is true when an AS was consumed.
func (p *parser) matchFromAlias() bool {
	return !p.isAsOfSystemTime() && p.MatchKeyword(keywordAS)
}

// isAsOfSystemTime reports whether the cursor is on an AS OF SYSTEM TIME clause the
// dialect reads.
//
// Returns bool which is true for such a clause.
func (p *parser) isAsOfSystemTime() bool {
	if !p.asOfSystemTime || p.Position+len(asOfSystemTimeKeywords) > len(p.Tokens) {
		return false
	}
	for offset, keyword := range asOfSystemTimeKeywords {
		if !p.Tokens[p.Position+offset].IsKeyword(tokenIdentifier, keyword) {
			return false
		}
	}
	return true
}

// parseAsOfSystemTimeIfPresent consumes an AS OF SYSTEM TIME clause after a FROM clause,
// which reads the FROM clause's relations at an earlier time and changes no type. As
// CockroachDB requires, the clause stands on the top-level query and takes no parameter.
//
// Returns error when the clause stands in a nested query or its time holds a parameter.
func (p *parser) parseAsOfSystemTimeIfPresent() error {
	if !p.isAsOfSystemTime() {
		return nil
	}
	clause := p.Current()
	if p.analysisDepth > 1 {
		return p.recordSyntaxError(dialectkit.NewParseError(clause,
			"AS OF SYSTEM TIME must be written on the top-level statement, as CockroachDB requires"))
	}
	p.Position += len(asOfSystemTimeKeywords)
	parameters := len(p.parameterReferences)
	p.parseExpression()
	if len(p.parameterReferences) > parameters {
		return p.recordSyntaxError(dialectkit.NewParseError(clause,
			"AS OF SYSTEM TIME takes a constant time or a call of follower_read_timestamp, with_min_timestamp or "+
				"with_max_staleness, not a parameter, as CockroachDB requires"))
	}
	return nil
}

// isBareTableAlias reports whether the current token is an alias written without AS: an
// identifier that does not start the next clause or join.
//
// Returns bool which is true for a bare alias.
func (p *parser) isBareTableAlias() bool {
	return p.Current().Kind == tokenIdentifier && !p.isJoinKeyword() && !p.isSelectTerminator() && !p.isAsOfSystemTime() &&
		!p.IsKeyword(keywordSET, keywordVALUES, keywordDEFAULT, keywordWHERE, "INNER", "LEFT", "RIGHT",
			"FULL", "CROSS", "NATURAL", keywordJOIN, keywordON, keywordUSING, keywordLATERAL, "TABLESAMPLE")
}

// skipTableSample consumes a TABLESAMPLE method (arguments) [REPEATABLE (seed)] clause.
//
// Returns error when the clause is malformed.
func (p *parser) skipTableSample() error {
	if !p.MatchKeyword("TABLESAMPLE") {
		return nil
	}
	if _, err := p.requireIdentifierOrKeyword(); err != nil {
		return err
	}
	if err := p.requireSkipParenthesised(); err != nil {
		return err
	}
	if p.MatchKeyword("REPEATABLE") {
		return p.requireSkipParenthesised()
	}
	return nil
}

// isSubqueryStart reports whether the current position begins a subquery.
//
// Returns bool which is true when the '(' at the cursor opens a query; see opensQueryAt.
func (p *parser) isSubqueryStart() bool {
	return p.Current().Kind == tokenLeftParen && p.opensQueryAt(p.Position)
}

// opensQueryAt distinguishes a query-opening parenthesis from an expression or join
// beginning with a subquery. It recognises SELECT, WITH, VALUES and TABLE, including
// nested query arms followed by set operations or trailing query clauses.
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
	closing := p.findMatchingRightParen(next)
	if closing < 0 || closing+1 >= len(p.Tokens) {
		return false
	}
	after := p.Tokens[closing+1]
	if after.Kind == tokenRightParen {
		return true
	}
	_, continuesQuery := queryContinuations[after.Keyword(tokenIdentifier)]
	return continuesQuery
}

// isTableValuedFunctionStart reports whether the current position begins a table-valued
// function call.
//
// Returns bool which is true when an identifier is followed by '('.
func (p *parser) isTableValuedFunctionStart() bool {
	if p.Current().Kind != tokenIdentifier {
		return false
	}
	if p.IsKeyword(keywordSELECT, keywordWITH, keywordVALUES) {
		return false
	}
	if p.Peek().Kind == tokenLeftParen {
		return true
	}

	if p.Peek().Kind != tokenDot {
		return false
	}
	if p.Position+schemaQualifiedCallLookahead >= len(p.Tokens) {
		return false
	}
	return p.Tokens[p.Position+2].Kind == tokenIdentifier &&
		p.Tokens[p.Position+schemaQualifiedCallLookahead].Kind == tokenLeftParen
}

// isSQLValueFunctionStart reports whether the current position begins a SQL value
// function written without parentheses, such as CURRENT_DATE, which PostgreSQL reads as a
// call of the function when it stands as a FROM item.
//
// Returns bool which is true for an unquoted SQL value function keyword that is not
// followed by a dot or a parenthesis.
func (p *parser) isSQLValueFunctionStart() bool {
	if p.Current().Kind != tokenIdentifier || p.Peek().Kind == tokenDot || p.Peek().Kind == tokenLeftParen {
		return false
	}
	_, implicit := implicitFunctionIdentifiers[p.Current().Keyword(tokenIdentifier)]
	return implicit
}

// parseDerivedTable parses a parenthesised subquery used as a table source.
//
// Takes joinKind (ast.JoinKind) which classifies the join context.
//
// Returns error when the inner query fails to parse or analyse.
func (p *parser) parseDerivedTable(joinKind ast.JoinKind) error {
	innerAnalysis, analyseError := p.analyseSubqueryBody()
	if analyseError != nil {
		return analyseError
	}

	alias := ""
	if p.matchFromAlias() {
		if p.Current().Kind == tokenIdentifier {
			alias = p.Advance().Value
		}
	} else if p.isBareTableAlias() {
		alias = p.Advance().Value
	}

	var columnNames []string
	if alias != "" && p.Current().Kind == tokenLeftParen {
		names, err := p.parseColumnAliasList()
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

// parseTableValuedFunction parses a function used as a table reference.
//
// It applies function and column aliases and records any WITH ORDINALITY column.
//
// Takes joinKind (ast.JoinKind) which classifies the join context.
func (p *parser) parseTableValuedFunction(joinKind ast.JoinKind) {
	p.recordTableFunctionCalls([]tableFunctionCall{p.parseTableFunctionCall()}, joinKind)
}

// parseWithOrdinality consumes the WITH ORDINALITY of a table function reference.
//
// Returns *ast.ScopedColumn which is the ordinality column it appends, named ordinality,
// bigint and never NULL, as PostgreSQL types it, or nil when the reference is not written
// WITH ORDINALITY.
func (p *parser) parseWithOrdinality() *ast.ScopedColumn {
	if !p.IsKeyword(keywordWITH) || !p.Peek().IsKeyword(tokenIdentifier, keywordORDINALITY) {
		return nil
	}
	p.Advance()
	p.Advance()
	return &ast.ScopedColumn{Name: ordinalityColumnName, SQLType: p.types.normalise(ordinalityTypeName), Nullable: false}
}

// aliasNamesColumn reports whether a table-function alias also names its sole result
// column. This applies to base-type results, or all single-column results under
// WithAliasNamesSingleColumnFunctions; the analyser handles built-in scalar calls through
// dialect.Info.ScalarFunctionsInFrom.
//
// Takes qualifiedName (string) which is the function name as the reference writes it.
// Takes functionName (string) which is the function's unqualified name.
// Takes argumentCount (int) which is the number of arguments the call passes.
// Takes aliased (bool) which is true when the reference writes an alias.
//
// Returns bool which is true when the alias names the column.
func (p *parser) aliasNamesColumn(qualifiedName, functionName string, argumentCount int, aliased bool) bool {
	columns := p.tableFunctionColumns(qualifiedName)
	if takesAliasAsColumn(columns, functionName, argumentCount) {
		return true
	}
	return p.aliasNamesSingleColumnFunctions && aliased && len(columns) == 1 &&
		(functionName != multipleArrayUnnest || argumentCount <= 1)
}

// tableFunctionColumns returns the columns a function called in a FROM clause produces: a
// built-in's, or else a user-defined function's from the catalogue.
//
// Takes qualifiedName (string) which is the function name as the reference writes it.
//
// Returns []ast.ScopedColumn which holds the columns, or nil when the function is not
// known.
func (p *parser) tableFunctionColumns(qualifiedName string) []ast.ScopedColumn {
	if columns := builtinTableFunctionColumns(p.functions, qualifiedName); columns != nil {
		return columns
	}
	return catalogueTableFunctionColumns(p.catalogue, qualifiedName)
}

// consumeTVFName consumes the optional schema qualifier and function identifier of a
// table-valued function call. After this returns the cursor sits on the opening `(`.
//
// Returns schemaName (string) which is the schema qualifier, or "" when the call was
// unqualified.
// Returns functionName (string) which is the canonical identifier of the called function:
// folded to lower case when unquoted, and as written when quoted.
func (p *parser) consumeTVFName() (schemaName string, functionName string) {
	functionName = p.Advance().Value
	if p.Current().Kind == tokenDot && p.Peek().Kind == tokenIdentifier {
		p.Advance()
		schemaName = functionName
		functionName = p.Advance().Value
	}
	return schemaName, functionName
}

// skipTVFArgumentList walks past the comma-separated argument list inside a TVF call and
// consumes the closing `)`. parseExpression handles each argument so parameter references
// inside the TVF call are still captured by the parser's analysis state.
//
// Each top-level placeholder argument is tagged with the FunctionArgument context, the
// schema-qualified function name, and its zero-based argument ordinal so the resolver can
// type the placeholder from the matched TVF signature's declared argument. Only
// references still missing an enclosing function name are tagged, leaving a nested call's
// own tagging intact, and a non-placeholder argument (a literal, column, or expression)
// still consumes its ordinal so a placeholder maps to the correct argument slot.
//
// Takes qualifiedName (string) which is the canonical, optionally schema-qualified
// function name recorded on the TVF reference.
//
// Returns []ast.Expression which holds the arguments the call passes, in order.
func (p *parser) skipTVFArgumentList(qualifiedName string) []ast.Expression {
	var arguments []ast.Expression
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		referenceCountBefore := len(p.parameterReferences)
		arguments = append(arguments, p.parseExpression())
		p.tagTableValuedFunctionArguments(qualifiedName, len(arguments)-1, referenceCountBefore)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	return arguments
}

// tagTableValuedFunctionArguments tags the parameter references appended while parsing a
// single top-level table-valued function argument with the enclosing function name and
// the argument ordinal. References already carrying an enclosing function name (set by a
// nested call) are left untouched.
//
// Takes qualifiedName (string) which is the canonical, optionally schema-qualified
// function name.
// Takes ordinal (int) which is the zero-based top-level argument slot.
// Takes referenceCountBefore (int) which is the parameterReferences length captured
// before parsing the argument.
func (p *parser) tagTableValuedFunctionArguments(qualifiedName string, ordinal int, referenceCountBefore int) {
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		if p.parameterReferences[index].Context != sqlmodel.ParameterContextUnknown ||
			p.parameterReferences[index].EnclosingFunctionName != "" {
			continue
		}
		p.parameterReferences[index].Context = sqlmodel.ParameterContextFunctionArgument
		p.parameterReferences[index].EnclosingFunctionName = qualifiedName
		p.parameterReferences[index].ArgumentOrdinal = ordinal
	}
}

// parseTVFAliasAndColumns parses an optional `AS alias [(col defs)]` trailer on a
// table-valued function reference.
//
// The bare identifier form without AS, with or without its column list, is also accepted
// when it does not introduce a JOIN or another SELECT terminator. The alias defaults to
// the function name when no explicit alias is present.
//
// Takes functionName (string) which is the default alias used when the caller did not
// specify one.
//
// Returns alias (string) which is the resolved alias.
// Returns aliased (bool) which is true when the reference writes an alias.
// Returns columnDefinitions ([]ast.TVFColumnDefinition) which is the parsed column
// definition list when an explicit `(col defs)` trailer was supplied, or nil otherwise.
func (p *parser) parseTVFAliasAndColumns(functionName string) (alias string, aliased bool, columnDefinitions []ast.TVFColumnDefinition) {
	alias = functionName
	if p.matchFromAlias() {
		if p.Current().Kind == tokenIdentifier {
			alias, aliased = p.Advance().Value, true
		}
		if p.Current().Kind == tokenLeftParen {
			columnDefinitions = p.parseTVFColumnDefinitions()
		}
		return alias, aliased, columnDefinitions
	}
	if p.Current().Kind == tokenIdentifier && !p.isJoinKeyword() && !p.isSelectTerminator() {
		alias, aliased = p.Advance().Value, true
		if p.Current().Kind == tokenLeftParen {
			columnDefinitions = p.parseTVFColumnDefinitions()
		}
	}
	return alias, aliased, columnDefinitions
}

// parseTVFColumnDefinitions parses the column definition list of a TVF reference.
//
// Returns []ast.TVFColumnDefinition which holds the parsed columns.
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
			typeName = p.parseCastTypeName()
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

// parseJoinKeyword consumes a join-introducing keyword sequence.
//
// Returns kind (ast.JoinKind) which classifies the consumed join.
// Returns natural (bool) which is true for a NATURAL join.
// Returns isJoin (bool) which is true when a join keyword was consumed.
func (p *parser) parseJoinKeyword() (kind ast.JoinKind, natural bool, isJoin bool) {
	natural = p.MatchKeyword("NATURAL")

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

// isJoinKeyword reports whether the current token is a join-introducer.
//
// Returns bool which is true for JOIN, INNER, LEFT, RIGHT, FULL, CROSS, or NATURAL.
func (p *parser) isJoinKeyword() bool {
	return p.IsKeyword(keywordJOIN, "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "NATURAL")
}

// startsQuery reports whether a token is a word a query starts with: SELECT, WITH, VALUES
// or TABLE.
//
// Takes candidate (token) which is the token.
//
// Returns bool which is true for a word a query starts with.
func startsQuery(candidate token) bool {
	return candidate.IsKeyword(tokenIdentifier, keywordSELECT) || candidate.IsKeyword(tokenIdentifier, keywordWITH) ||
		candidate.IsKeyword(tokenIdentifier, keywordVALUES) || candidate.IsKeyword(tokenIdentifier, keywordTABLE)
}
