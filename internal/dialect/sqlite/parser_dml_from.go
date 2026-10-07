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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// parseSelectFromClause parses the FROM clause of a SELECT onto its analysis, with the
// order the clause lists its items in.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the tables, joins and order.
//
// Returns error when the clause cannot be parsed.
func (p *parser) parseSelectFromClause(analysis *ast.RawQueryAnalysis) error {
	var order []string
	enclosingOrder := p.fromOrder
	p.fromOrder = &order
	defer func() { p.fromOrder = enclosingOrder }()

	fromTables, joinClauses, err := p.parseFromClause()
	if err != nil {
		return err
	}
	analysis.FromTables = fromTables
	analysis.JoinClauses = joinClauses
	analysis.FromOrder = order
	return nil
}

// recordFromItem records the name a FROM item is visible under, in FROM order.
//
// Takes name (string) which is the item's alias, or its name when it has none.
func (p *parser) recordFromItem(name string) {
	if p.fromOrder != nil && name != "" {
		*p.fromOrder = append(*p.fromOrder, name)
	}
}

// parseFromClause parses a FROM clause into a table list and a join list.
//
// Returns []ast.TableReference which are the top-level tables.
// Returns []ast.JoinClause which are the explicit joins.
// Returns error when a source, join target or condition cannot be parsed.
func (p *parser) parseFromClause() ([]ast.TableReference, []ast.JoinClause, error) {
	tables, joins, err := p.parseFromTableSource()
	if err != nil {
		return nil, nil, err
	}

	for {
		joinKind, natural, isJoin := p.parseJoinKeyword()
		if isJoin {
			joinTables, joinJoins, joinError := p.parseExplicitJoin(joinKind, natural)
			if joinError != nil {
				return nil, nil, joinError
			}
			tables = append(tables, joinTables...)
			joins = append(joins, joinJoins...)
			continue
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
		commaTables, commaJoins, commaError := p.parseFromTableSource()
		if commaError != nil {
			return nil, nil, commaError
		}
		tables = append(tables, commaTables...)
		joins = append(joins, commaJoins...)
	}

	return tables, joins, nil
}

// parseExplicitJoin parses the target and the ON or USING condition of an explicit JOIN.
//
// Takes joinKind (ast.JoinKind) which is the kind parseJoinKeyword read.
// Takes natural (bool) which is true for a NATURAL join.
//
// Returns []ast.TableReference which is always empty; it is kept so the result composes
// with parseFromTableSource's.
// Returns []ast.JoinClause which are the joins the target adds: one for a table, none for
// a derived table or table-valued function, and one per table for a parenthesised group.
// Returns error when the target or condition fails to parse.
func (p *parser) parseExplicitJoin(joinKind ast.JoinKind, natural bool) ([]ast.TableReference, []ast.JoinClause, error) {
	derivedAt := len(p.rawDerivedTables)
	tableTarget := p.isPlainJoinTarget()
	joins, err := p.parseJoinTarget(joinKind)
	if err != nil {
		return nil, nil, err
	}
	using, err := p.parseJoinCondition(natural)
	if err != nil {
		return nil, nil, err
	}
	switch {
	case using == nil:
	case tableTarget && len(joins) == 1:
		joins[0].Using = using
	case len(p.rawDerivedTables) > derivedAt:
		p.rawDerivedTables[derivedAt].Using = using
	}
	return nil, joins, nil
}

// isPlainJoinTarget reports whether the JOIN target at the cursor is a table rather than
// a derived table, a table-valued function, a parenthesised group or a built-in relation.
//
// Returns bool which is true for a table.
func (p *parser) isPlainJoinTarget() bool {
	return !p.isTableValuedFunctionStart() && !p.isSubqueryStart() && p.Current().Kind != tokenLeftParen &&
		!p.isBuiltinRelationStart()
}

// parseFromTableSource parses a single FROM source: a table, a derived table, a
// table-valued function, or a parenthesised group of sources and joins.
//
// Returns []ast.TableReference which holds the bare tables the source adds.
// Returns []ast.JoinClause which holds the joins inside a parenthesised group.
// Returns error when a derived table or parenthesised group fails to parse.
func (p *parser) parseFromTableSource() ([]ast.TableReference, []ast.JoinClause, error) {
	switch {
	case p.isTableValuedFunctionStart():
		p.parseTableValuedFunction(ast.JoinInner)
		return nil, nil, nil
	case p.isSubqueryStart():
		return nil, nil, p.parseDerivedTable(ast.JoinInner)
	case p.Current().Kind == tokenLeftParen:
		return p.parseParenthesisedFromItem()
	case p.isBuiltinRelationStart():
		p.parseBuiltinRelation(ast.JoinInner)
		return nil, nil, nil
	}
	table, err := p.parseRequiredTableReference()
	if err != nil {
		return nil, nil, err
	}
	return []ast.TableReference{table}, nil, nil
}

// parseRequiredTableReference parses a `[schema.]name [[AS] alias]` table reference that
// must be present, such as a FROM source or a JOIN target.
//
// Returns ast.TableReference which is the parsed reference.
// Returns error when no table name is at the cursor.
func (p *parser) parseRequiredTableReference() (ast.TableReference, error) {
	if p.Current().Kind != tokenIdentifier {
		return ast.TableReference{}, dialectkit.NewParseError(p.Current(), "expected a table name")
	}
	tableName, alias := p.parseTableReference()
	p.noteFullTextSource(tableName, alias)
	if alias != "" {
		p.recordFromItem(alias)
	} else {
		p.recordFromItem(tableName)
	}
	return ast.TableReference{Name: tableName, Alias: alias, Schema: ""}, nil
}

// noteFullTextSource records the name and alias of a FROM source that reads an FTS5
// table, so a full-text query on its table-named column is recognised.
//
// Takes tableName (string) which is the source's table name.
// Takes alias (string) which is the source's alias, or empty.
func (p *parser) noteFullTextSource(tableName string, alias string) {
	if !p.fullTextTables[strings.ToLower(tableName)] {
		return
	}
	if p.fullTextSources == nil {
		p.fullTextSources = make(map[string]bool)
	}
	p.fullTextSources[strings.ToLower(tableName)] = true
	if alias != "" {
		p.fullTextSources[strings.ToLower(alias)] = true
	}
}

// parseParenthesisedFromItem parses a parenthesised FROM item that is not a subquery,
// such as `(t)` or `(a JOIN b ON a.id = b.a_id)`. SQLite treats the group as the tables
// and joins it holds, so they are returned for the enclosing FROM clause to merge.
//
// Returns []ast.TableReference which holds the group's leading tables.
// Returns []ast.JoinClause which holds the group's joins.
// Returns error when the group is malformed or not closed.
func (p *parser) parseParenthesisedFromItem() ([]ast.TableReference, []ast.JoinClause, error) {
	opening := p.Advance()
	if p.expressionDepth >= p.maxParseDepth {
		return nil, nil, dialectkit.DepthError(p.maxParseDepth)
	}
	p.expressionDepth++
	defer func() { p.expressionDepth-- }()

	tables, joins, err := p.parseFromClause()
	if err != nil {
		return nil, nil, err
	}
	if p.Current().Kind != tokenRightParen {
		return nil, nil, dialectkit.NewParseError(opening, messageUnmatchedParenthesis)
	}
	p.Advance()
	return tables, joins, nil
}

// parseJoinTarget parses the target of an explicit JOIN.
//
// A parenthesised group joined in becomes one join per table it holds. A table the group
// joins with an inner join takes the outer join's kind, so the rows a LEFT JOIN of the
// group adds stay nullable.
//
// Takes joinKind (ast.JoinKind) which is the kind parseJoinKeyword read.
//
// Returns []ast.JoinClause which holds the joins the target adds, or none for a derived
// table or table-valued function.
// Returns error when the target fails to parse.
func (p *parser) parseJoinTarget(joinKind ast.JoinKind) ([]ast.JoinClause, error) {
	switch {
	case p.isTableValuedFunctionStart():
		p.parseTableValuedFunction(joinKind)
		return nil, nil
	case p.isSubqueryStart():
		return nil, p.parseDerivedTable(joinKind)
	case p.Current().Kind == tokenLeftParen:
		tables, joins, err := p.parseParenthesisedFromItem()
		if err != nil {
			return nil, err
		}
		return groupJoins(joinKind, tables, joins), nil
	case p.isBuiltinRelationStart():
		p.parseBuiltinRelation(joinKind)
		return nil, nil
	}
	table, err := p.parseRequiredTableReference()
	if err != nil {
		return nil, err
	}
	return []ast.JoinClause{{Kind: joinKind, Table: table, Using: nil}}, nil
}

// parseJoinCondition consumes an ON expression or USING column list when present after a
// JOIN target.
//
// Takes natural (bool) which is true for a NATURAL join.
//
// Returns *ast.JoinUsing which describes the columns a USING clause or a NATURAL join
// merges, each keeping the place the left side gives it as SQLite places it, or nil for a
// join that merges none.
// Returns error when the USING column list is malformed.
func (p *parser) parseJoinCondition(natural bool) (*ast.JoinUsing, error) {
	if p.MatchKeyword(keywordON) {
		start := p.Position
		p.parseJoinConditionExpression()
		p.recordClauseExpressions(ast.ClauseJoinCondition, p.conditionExpressions(start, p.Position))
		return nil, nil
	}
	if p.MatchKeyword("USING") && p.Current().Kind == tokenLeftParen {
		columns, err := p.parseUsingColumns()
		if err != nil {
			return nil, err
		}
		return &ast.JoinUsing{Columns: columns, Natural: false, Placement: ast.JoinColumnsInPlace}, nil
	}
	if natural {
		return &ast.JoinUsing{Columns: nil, Natural: true, Placement: ast.JoinColumnsInPlace}, nil
	}
	return nil, nil
}

// parseUsingColumns parses the parenthesised column list of a USING clause.
//
// Returns []string which holds the column names in order.
// Returns error when the list is not a parenthesised, comma-separated list of names.
func (p *parser) parseUsingColumns() ([]string, error) {
	opening := p.Advance()
	var columns []string
	for {
		if p.Current().Kind != tokenIdentifier {
			return nil, dialectkit.NewParseError(p.Current(), "expected a column name in the USING list")
		}
		columns = append(columns, p.Advance().Value)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenRightParen {
		return nil, dialectkit.NewParseError(opening, messageUnmatchedParenthesis)
	}
	p.Advance()
	return columns, nil
}

// isSubqueryStart reports whether the cursor is positioned at the opening of a `(SELECT
// ...)`, `(WITH ...)` or `(VALUES ...)` subquery.
//
// Returns bool which is true when `(` is followed by a keyword that starts a query.
func (p *parser) isSubqueryStart() bool {
	return p.Current().Kind == tokenLeftParen && (p.Peek().IsKeyword(tokenIdentifier, keywordSELECT) ||
		p.Peek().IsKeyword(tokenIdentifier, keywordWITH) || p.Peek().IsKeyword(tokenIdentifier, keywordVALUES))
}

// parseDerivedTable parses a `(SELECT ...) [AS] alias` derived table.
//
// Takes joinKind (ast.JoinKind) which describes how the derived table participates in the
// FROM clause.
//
// Returns error when the inner SELECT cannot be analysed or leaves tokens unparsed.
func (p *parser) parseDerivedTable(joinKind ast.JoinKind) error {
	innerTokens, collectError := p.collectParenthesised()
	if collectError != nil {
		return collectError
	}

	childParser, innerAnalysis, analyseError := p.analyseNested(innerTokens)
	if analyseError != nil {
		return analyseError
	}
	p.parameterCount = childParser.parameterCount
	p.parameterReferences = append(p.parameterReferences, childParser.parameterReferences...)

	alias := p.parseSourceAlias("")

	p.rawDerivedTables = append(p.rawDerivedTables, ast.RawDerivedTableReference{
		Alias:       alias,
		ColumnNames: nil,
		InnerQuery:  innerAnalysis,
		JoinKind:    joinKind,
		Using:       nil,
	})
	p.recordFromItem(alias)

	return nil
}

// parseSourceAlias consumes the optional `[AS] alias` after a FROM source.
//
// Takes fallback (string) which is the alias when none is written.
//
// Returns string which is the alias.
func (p *parser) parseSourceAlias(fallback string) string {
	if p.MatchKeyword(keywordAS) {
		if p.Current().Kind == tokenIdentifier {
			return p.Advance().Value
		}
		return fallback
	}
	if p.Current().Kind == tokenIdentifier && !p.isJoinKeyword() && !p.isSelectTerminator() &&
		!p.IsKeyword("USING", "WINDOW") {
		return p.Advance().Value
	}
	return fallback
}

// parseTableReference parses a `[schema.]name [[AS] alias]` table reference.
//
// Returns tableName (string) which is the bare table name.
// Returns tableAlias (string) which is the alias when present, else "".
func (p *parser) parseTableReference() (tableName string, tableAlias string) {
	if p.Current().Kind != tokenIdentifier {
		return "", ""
	}

	name := p.Advance().Value

	if p.Current().Kind == tokenDot {
		p.Advance()
		if p.Current().Kind == tokenIdentifier {
			name = p.Advance().Value
		}
	}

	alias := ""
	if p.MatchKeyword(keywordAS) {
		if p.Current().Kind == tokenIdentifier {
			alias = p.Advance().Value
		}
	} else if p.Current().Kind == tokenIdentifier && !p.isJoinKeyword() && !p.isSelectTerminator() &&
		!p.IsKeyword(keywordSET, keywordVALUES, "DEFAULT", keywordWHERE, keywordINNER, keywordLEFT, keywordRIGHT,
			keywordFULL, keywordCROSS, keywordNATURAL, keywordJOIN, keywordON, "USING", "INDEXED", "WINDOW") {
		alias = p.Advance().Value
	}
	p.skipIndexHint()

	return name, alias
}

// skipIndexHint consumes SQLite's `INDEXED BY name` or `NOT INDEXED` table hint, which
// changes only the query plan.
func (p *parser) skipIndexHint() {
	if p.MatchKeyword("INDEXED") {
		p.MatchKeyword(keywordBY)
		if p.Current().Kind == tokenIdentifier {
			p.Advance()
		}
		return
	}
	if p.IsKeyword(keywordNOT) && p.Peek().IsKeyword(tokenIdentifier, "INDEXED") {
		p.Advance()
		p.Advance()
	}
}

// isTableValuedFunctionStart reports whether the cursor is at an `identifier(` shape that
// is not the SELECT keyword.
//
// Returns bool which is true for table-valued function call syntax.
func (p *parser) isTableValuedFunctionStart() bool {
	return p.Current().Kind == tokenIdentifier && p.Peek().Kind == tokenLeftParen &&
		!p.Current().IsKeyword(tokenIdentifier, keywordSELECT)
}

// isBuiltinRelationStart reports whether the cursor is at one of SQLite's built-in
// relations written without call parentheses, such as sqlite_schema, main.sqlite_master
// or pragma_table_list, which no migration creates.
//
// Returns bool which is true when the source names a built-in relation.
func (p *parser) isBuiltinRelationStart() bool {
	name := p.Current()
	if name.Kind == tokenIdentifier && p.Peek().Kind == tokenDot && p.PeekAt(2).Kind == tokenIdentifier {
		name = p.PeekAt(2)
	}
	return name.Kind == tokenIdentifier && isBuiltinRelation(name.Value)
}

// parseBuiltinRelation parses a built-in relation source and records it as the
// table-valued source it behaves as, so its columns resolve without a CREATE TABLE.
//
// Takes joinKind (ast.JoinKind) which describes how the relation participates in the FROM
// clause.
func (p *parser) parseBuiltinRelation(joinKind ast.JoinKind) {
	name, alias := p.parseTableReference()
	if alias == "" {
		alias = name
	}
	p.rawTableValuedFunctions = append(p.rawTableValuedFunctions, ast.RawTableValuedFunctionReference{
		FunctionName:      strings.ToLower(name),
		Arguments:         nil,
		Alias:             alias,
		JoinKind:          joinKind,
		ColumnDefinitions: nil,
		Ordinality:        nil,
		Functions:         nil,
	})
	p.recordFromItem(alias)
}

// parseTableValuedFunction parses a `name(args...) [[AS] alias]` table-valued function
// call and records it on the parser.
//
// The table-valued form of an FTS5 table, `docs(?)`, takes a full-text query, and a
// pragma function, such as pragma_table_info(?), takes a schema object's name, so the
// arguments of both are typed as text.
//
// Takes joinKind (ast.JoinKind) which describes how the function participates in the FROM
// clause.
func (p *parser) parseTableValuedFunction(joinKind ast.JoinKind) {
	functionName := strings.ToLower(p.Advance().Value)
	p.Advance()

	argumentOrdinal := 0
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		referenceCountBefore := len(p.parameterReferences)
		p.parseExpression()
		switch {
		case p.fullTextTables[functionName]:
			p.markFullTextQueryParameters(referenceCountBefore)
		case pragmaFunctionColumns[functionName] != nil:
			p.markPragmaArgumentParameters(referenceCountBefore)
		default:
			p.markFunctionArgumentParameters(referenceCountBefore, functionName, argumentOrdinal)
		}
		argumentOrdinal++
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	} else {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ) after the arguments of "+functionName))
	}

	alias := p.parseSourceAlias(functionName)

	p.rawTableValuedFunctions = append(p.rawTableValuedFunctions, ast.RawTableValuedFunctionReference{
		FunctionName:      functionName,
		Arguments:         nil,
		Alias:             alias,
		JoinKind:          joinKind,
		ColumnDefinitions: nil,
		Ordinality:        nil,
		Functions:         nil,
	})
	p.recordFromItem(alias)
}

// parseJoinKeyword consumes a JOIN keyword sequence and reports the resulting join kind.
//
// Returns kind (ast.JoinKind) which is the join kind read.
// Returns natural (bool) which is true for a NATURAL join.
// Returns isJoin (bool) which is true when a join keyword sequence was consumed.
func (p *parser) parseJoinKeyword() (kind ast.JoinKind, natural bool, isJoin bool) {
	if p.IsKeyword(keywordNATURAL) && !p.Peek().IsKeyword(tokenIdentifier, keywordJOIN) &&
		!p.Peek().IsKeyword(tokenIdentifier, keywordINNER) && !p.Peek().IsKeyword(tokenIdentifier, keywordLEFT) &&
		!p.Peek().IsKeyword(tokenIdentifier, keywordRIGHT) && !p.Peek().IsKeyword(tokenIdentifier, keywordFULL) &&
		!p.Peek().IsKeyword(tokenIdentifier, keywordCROSS) {
		return ast.JoinInner, false, false
	}
	natural = p.MatchKeyword(keywordNATURAL)

	if p.MatchKeyword(keywordINNER) {
		p.MatchKeyword(keywordJOIN)
		return ast.JoinInner, natural, true
	}
	if p.MatchKeyword(keywordLEFT) {
		p.MatchKeyword("OUTER")
		p.MatchKeyword(keywordJOIN)
		return ast.JoinLeft, natural, true
	}
	if p.MatchKeyword(keywordRIGHT) {
		p.MatchKeyword("OUTER")
		p.MatchKeyword(keywordJOIN)
		return ast.JoinRight, natural, true
	}
	if p.MatchKeyword(keywordFULL) {
		p.MatchKeyword("OUTER")
		p.MatchKeyword(keywordJOIN)
		return ast.JoinFull, natural, true
	}
	if p.MatchKeyword(keywordCROSS) {
		p.MatchKeyword(keywordJOIN)
		return ast.JoinCross, natural, true
	}
	if p.MatchKeyword(keywordJOIN) {
		return ast.JoinInner, natural, true
	}

	return ast.JoinInner, false, false
}

// isJoinKeyword reports whether the current token is a JOIN-related keyword.
//
// Returns bool which is true for JOIN, INNER, LEFT, RIGHT, FULL, CROSS, or NATURAL.
func (p *parser) isJoinKeyword() bool {
	return p.IsKeyword(keywordJOIN, keywordINNER, keywordLEFT, keywordRIGHT, keywordFULL, keywordCROSS, keywordNATURAL)
}

// groupJoins turns the tables and joins of a parenthesised group that is itself joined in
// into the joins the enclosing FROM clause records.
//
// Takes joinKind (ast.JoinKind) which is the kind of the join that brings the group in.
// Takes tables ([]ast.TableReference) which are the group's leading tables.
// Takes joins ([]ast.JoinClause) which are the group's own joins.
//
// Returns []ast.JoinClause which holds one join per table of the group.
func groupJoins(joinKind ast.JoinKind, tables []ast.TableReference, joins []ast.JoinClause) []ast.JoinClause {
	result := make([]ast.JoinClause, 0, len(tables)+len(joins))
	for _, table := range tables {
		result = append(result, ast.JoinClause{Kind: joinKind, Table: table, Using: nil})
	}
	for _, join := range joins {
		if join.Kind == ast.JoinInner {
			join.Kind = joinKind
		}
		result = append(result, join)
	}
	return result
}
