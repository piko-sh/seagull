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
	"errors"
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// parseCreateView parses a CREATE VIEW statement.
//
// Returns *ast.CatalogueMutation which describes the new view.
// Returns error when the view name, column list or query cannot be parsed.
func (p *parser) parseCreateView() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}
	p.MatchKeyword("TEMP")
	p.MatchKeyword("TEMPORARY")
	if _, err := p.ExpectKeyword("VIEW"); err != nil {
		return nil, err
	}

	ifNotExists, err := p.matchIfNotExists()
	if err != nil {
		return nil, err
	}

	viewName, err := p.parseTableName()
	if err != nil {
		return nil, err
	}

	var columnNames []string
	if p.Current().Kind == tokenLeftParen {
		names, listError := p.parseColumnList()
		if listError != nil {
			return nil, listError
		}
		columnNames = names
	}

	if _, err := p.ExpectKeyword(keywordAS); err != nil {
		return nil, err
	}

	bodyStart := p.Position
	definition, unanalysed, err := p.analyseViewBody(columnNames, "view")
	if err != nil {
		return nil, err
	}

	if len(columnNames) == 0 && definition == nil {
		columnNames = inferViewColumnNames(p.Tokens, bodyStart)
	}

	mutation := ast.NewCatalogueMutation(ast.MutationCreateView, "", viewName, ast.WithIfNotExists(ifNotExists))
	mutation.ViewDefinition = definition
	mutation.Columns = dialectkit.ColumnsFromNames(columnNames)
	mutation.UnanalysedParts = unanalysed
	return mutation, nil
}

// analyseViewBody analyses the query of a CREATE VIEW or CREATE TABLE ... AS, which runs
// from the cursor to the end of the statement, so the catalogue can store typed columns,
// and consumes it.
//
// A malformed query, including one that is not a SELECT, VALUES or WITH query or does not
// read to the end of the statement, fails the statement with its syntax error. A query
// nested more deeply than the parse depth limit is consumed without being analysed and
// reported as an unanalysed part, so the statement still applies with untyped columns. A
// panic or an interrupt in the analyser is not caught here: it fails the whole statement
// through ApplyDDL's recovery.
//
// Takes columnNames ([]string) which is the declared column list overlaid onto the
// inferred projection names.
// Takes subject (string) which names what the query defines, such as view, for the
// unanalysed part's message.
//
// Returns *ast.RawQueryAnalysis which describes the query, or nil when it was not
// analysed.
// Returns []ast.Warning which holds the query when it was not analysed, saying why; nil
// otherwise.
// Returns error which is the query's syntax error.
func (p *parser) analyseViewBody(columnNames []string, subject string) (*ast.RawQueryAnalysis, []ast.Warning, error) {
	bodyStart := p.Position
	viewParser := p.newChildParser(p.Tokens[bodyStart:])
	analysis, err := viewParser.analyseSelect()
	if err == nil {
		err = viewParser.requireEnd()
	}
	if viewParser.syntaxError != nil {
		err = viewParser.syntaxError
	}
	if errors.Is(err, dialectkit.ErrParseDepth) {
		p.Position = len(p.Tokens)
		return nil, []ast.Warning{{
			Message: "the " + subject + "'s query was not analysed, so its columns have no types: " + err.Error(),
			Span:    dialectkit.TokenRangeSpan(p.Tokens, bodyStart, len(p.Tokens)),
		}}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	p.Position = len(p.Tokens)

	if len(columnNames) > 0 && !slices.ContainsFunc(analysis.OutputColumns, dialectkit.IsStarColumn) {
		dialectkit.OverlayViewColumnNames(analysis, columnNames, dialectkit.UndeclaredColumnsDropped)
	}

	return analysis, nil, nil
}

// parseDropView parses a DROP VIEW statement.
//
// Returns *ast.CatalogueMutation which describes the dropped view, marked IF EXISTS when
// the statement says so.
// Returns error when the view name cannot be parsed.
func (p *parser) parseDropView() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence(keywordDROP, "VIEW"); err != nil {
		return nil, err
	}

	ifExists, err := p.matchIfExists()
	if err != nil {
		return nil, err
	}

	viewName, err := p.parseTableName()
	if err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(ast.MutationDropView, "", viewName, ast.WithIfExists(ifExists)), nil
}

// parseCreateIndex parses a CREATE [UNIQUE] INDEX statement: the index and table names,
// the parenthesised list of indexed columns and expressions, and the condition of a
// partial index.
//
// Returns *ast.CatalogueMutation which records the indexed table.
// Returns error when a name or the indexed list is missing, the list is unmatched, or the
// condition does not read.
func (p *parser) parseCreateIndex() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}
	p.MatchKeyword(keywordUNIQUE)
	if _, err := p.ExpectKeyword("INDEX"); err != nil {
		return nil, err
	}

	if _, err := p.matchIfNotExists(); err != nil {
		return nil, err
	}

	_, err := p.parseTableName()
	if err != nil {
		return nil, err
	}

	if _, err := p.ExpectKeyword(keywordON); err != nil {
		return nil, err
	}

	tableName, tableError := p.parseTableName()
	if tableError != nil {
		return nil, tableError
	}

	if err := p.skipParenthesised(); err != nil {
		return nil, err
	}
	if p.MatchKeyword(keywordWHERE) {
		p.conditionExpressions(p.Position, len(p.Tokens))
		p.Position = len(p.Tokens)
	}

	return ast.NewCatalogueMutation(ast.MutationCreateIndex, "", tableName), nil
}

// parseDropIndex parses a DROP INDEX statement.
//
// Returns *ast.CatalogueMutation which records the drop kind, marked IF EXISTS when the
// statement says so.
// Returns error when the index name cannot be parsed.
func (p *parser) parseDropIndex() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence(keywordDROP, "INDEX"); err != nil {
		return nil, err
	}

	ifExists, err := p.matchIfExists()
	if err != nil {
		return nil, err
	}

	if _, err := p.parseTableName(); err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(ast.MutationDropIndex, "", "", ast.WithIfExists(ifExists)), nil
}

// parseCreateVirtualTable parses a CREATE VIRTUAL TABLE statement and extracts
// module-specific column shape information.
//
// Takes engine (*Dialect) which supplies type normalisation for inferred columns.
//
// Returns *ast.CatalogueMutation which describes the virtual table.
// Returns error when the table name, the USING clause or its module name is missing, or
// the module arguments are unmatched.
func (p *parser) parseCreateVirtualTable(engine *Dialect) (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence(keywordCREATE, "VIRTUAL", keywordTABLE); err != nil {
		return nil, err
	}

	if _, err := p.matchIfNotExists(); err != nil {
		return nil, err
	}

	tableName, nameError := p.parseTableName()
	if nameError != nil {
		return nil, nameError
	}

	if _, err := p.ExpectKeyword("USING"); err != nil {
		return nil, err
	}

	moduleName, moduleError := p.parseIdentifierOrKeyword()
	if moduleError != nil {
		return nil, fmt.Errorf("parsing virtual table module: %w", moduleError)
	}

	if p.Current().Kind != tokenLeftParen {
		return ast.NewCatalogueMutation(
			ast.MutationCreateTable,
			"",
			tableName,
			ast.WithVirtualModule(moduleName),
		), nil
	}

	argumentTokens, err := p.collectParenthesised()
	if err != nil {
		return nil, fmt.Errorf("parsing virtual table arguments: %w", err)
	}

	lowerModule := strings.ToLower(moduleName)
	columns, primaryKeyColumns := extractVirtualTableColumns(tableName, lowerModule, argumentTokens, engine)

	return ast.NewCatalogueMutation(
		ast.MutationCreateTable,
		"",
		tableName,
		ast.WithVirtualModule(lowerModule),
		ast.WithColumns(columns),
		ast.WithPrimaryKey(primaryKeyColumns),
	), nil
}

// parseTableName parses an optionally schema-qualified table name and returns the table
// portion.
//
// Returns string which is the bare table name.
// Returns error when no identifier is found.
func (p *parser) parseTableName() (string, error) {
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return "", err
	}

	if p.Current().Kind == tokenDot {
		p.Advance()
		tableName, tableError := p.parseIdentifierOrKeyword()
		if tableError != nil {
			return "", tableError
		}
		return tableName, nil
	}

	return name, nil
}

// parseCreateTrigger parses a CREATE TRIGGER statement up to its target table and ignores
// the action body.
//
// Returns *ast.CatalogueMutation which records the trigger name and target table.
// Returns error when the trigger name, the ON keyword or the target table cannot be
// parsed.
func (p *parser) parseCreateTrigger() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}
	p.MatchKeyword("TEMP")
	p.MatchKeyword("TEMPORARY")
	if _, err := p.ExpectKeyword("TRIGGER"); err != nil {
		return nil, err
	}

	if _, err := p.matchIfNotExists(); err != nil {
		return nil, err
	}

	triggerName, err := p.parseTableName()
	if err != nil {
		return nil, err
	}

	p.MatchKeyword("BEFORE")
	p.MatchKeyword("AFTER")
	if p.MatchKeyword("INSTEAD") {
		p.MatchKeyword("OF")
	}

	p.MatchKeyword("DELETE")
	p.MatchKeyword("INSERT")
	p.MatchKeyword("UPDATE")
	if p.MatchKeyword("OF") {
		for !p.AtEnd() {
			p.Advance()
			if p.Current().Kind != tokenComma {
				break
			}
			p.Advance()
		}
	}

	if _, err := p.ExpectKeyword(keywordON); err != nil {
		return nil, err
	}
	tableName, err := p.parseTableName()
	if err != nil {
		return nil, fmt.Errorf("parsing trigger table: %w", err)
	}

	return ast.NewCatalogueMutation(
		ast.MutationCreateTrigger,
		"",
		tableName,
		ast.WithTriggerName(triggerName),
	), nil
}

// parseDropTrigger parses a DROP TRIGGER statement.
//
// Returns *ast.CatalogueMutation which records the dropped trigger, marked IF EXISTS when
// the statement says so.
// Returns error when the trigger name cannot be parsed.
func (p *parser) parseDropTrigger() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence(keywordDROP, "TRIGGER"); err != nil {
		return nil, err
	}

	ifExists, err := p.matchIfExists()
	if err != nil {
		return nil, err
	}

	triggerName, err := p.parseTableName()
	if err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(
		ast.MutationDropTrigger,
		"",
		"",
		ast.WithTriggerName(triggerName),
		ast.WithIfExists(ifExists),
	), nil
}

// inferViewColumnNames extracts column names from the SELECT projection that follows
// `CREATE VIEW name AS`.
//
// It walks comma-separated expressions at parenthesis depth zero between the leading
// SELECT and the FROM keyword, returning the alias from `... AS alias` or the trailing
// identifier of a column reference. Expressions without a recoverable name produce an
// empty entry.
//
// Takes tokens ([]token) which is the full token stream of the containing statement.
// Takes start (int) which is the index of the first token after `AS`.
//
// Returns []string which is the inferred column name list in order.
func inferViewColumnNames(tokens []token, start int) []string {
	selectStart := findSelectStart(tokens, start)
	if selectStart < 0 {
		return nil
	}
	fromIndex := findTopLevelKeyword(tokens, selectStart, "FROM")
	if fromIndex < 0 {
		fromIndex = len(tokens)
	}
	return collectProjectionNames(tokens[selectStart:fromIndex])
}

// findSelectStart returns the index just past the outer-most SELECT keyword in the view
// body, skipping wrapping parentheses and any leading WITH ... AS (...)
// common-table-expression list.
//
// Recognised shapes are a bare `SELECT ...`, a parenthesised `(SELECT ...)`, a `WITH cte
// AS (...) SELECT ...`, a `WITH RECURSIVE cte AS (...) SELECT ...`, and any combination
// of these with redundant wrapping parens.
//
// Takes tokens ([]token) which is the token stream.
// Takes start (int) which is the starting search index (typically the token just past
// `AS`).
//
// Returns int which is the index of the first projection token, or -1 when no outer
// SELECT is found.
func findSelectStart(tokens []token, start int) int {
	for start < len(tokens) {
		switch tokens[start].Kind {
		case tokenLeftParen:
			start++
			continue
		case tokenIdentifier:
			value := tokens[start].Keyword(tokenIdentifier)
			if value == "SELECT" {
				return start + 1
			}
			if value == "WITH" {
				start = skipWithClause(tokens, start+1)
				continue
			}
			return -1
		default:
			return -1
		}
	}
	return -1
}

// skipWithClause walks past a WITH cte-list of the form `[RECURSIVE] name AS (...) [,
// name AS (...)] ...`.
//
// The first token after the last CTE body is returned. The function is conservative: any
// deviation from the expected structure leaves the cursor where it found the unexpected
// token, letting the caller fall through.
//
// Takes tokens ([]token) which is the token stream.
// Takes start (int) which is the index of the first token after WITH.
//
// Returns int which is the index of the token after the CTE list.
func skipWithClause(tokens []token, start int) int {
	if start < len(tokens) && tokens[start].IsKeyword(tokenIdentifier, "RECURSIVE") {
		start++
	}
	for {
		next, hasMore := skipSingleWithClauseEntry(tokens, start)
		if !hasMore {
			return next
		}
		start = next
	}
}

// skipSingleWithClauseEntry advances past one CTE entry within a WITH clause. Returns the
// next start index and a flag indicating whether the caller should continue iterating to
// consume more entries.
//
// Takes tokens ([]token) which is the token stream.
// Takes start (int) which is the index of the CTE name to consume.
//
// Returns int which is the index after this entry (or the position where parsing should
// stop on a malformed CTE list).
// Returns bool which is true when a trailing comma was consumed and another entry should
// follow.
func skipSingleWithClauseEntry(tokens []token, start int) (int, bool) {
	if start >= len(tokens) || tokens[start].Kind != tokenIdentifier {
		return start, false
	}
	start++
	if start < len(tokens) && tokens[start].Kind == tokenLeftParen {
		start = skipMatchingParens(tokens, start)
	}
	if start >= len(tokens) || !tokens[start].IsKeyword(tokenIdentifier, "AS") {
		return start, false
	}
	start++
	if start >= len(tokens) || tokens[start].Kind != tokenLeftParen {
		return start, false
	}
	start = skipMatchingParens(tokens, start)
	if start < len(tokens) && tokens[start].Kind == tokenComma {
		return start + 1, true
	}
	return start, false
}

// skipMatchingParens consumes a balanced parenthesised group beginning at start (which
// must reference a tokenLeftParen). Returns the index of the first token after the
// closing right paren.
//
// Takes tokens ([]token) which is the token stream.
// Takes start (int) which is the index of the opening `(`.
//
// Returns int which is the index just past the matching `)`, or len(tokens) when the
// input is unbalanced.
func skipMatchingParens(tokens []token, start int) int {
	depth := 0
	for index := start; index < len(tokens); index++ {
		switch tokens[index].Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
			if depth == 0 {
				return index + 1
			}
		default:
		}
	}
	return len(tokens)
}

// findTopLevelKeyword scans tokens for the first occurrence of the given keyword at
// parenthesis depth zero. The depth tracking lets the search skip past subqueries that
// may contain FROM, WHERE, etc.
//
// Takes tokens ([]token) which is the token stream.
// Takes start (int) which is the starting index.
// Takes keyword (string) which is the keyword to find (compared case-insensitively).
//
// Returns int which is the absolute index of the keyword, or -1 when not found at depth
// zero.
func findTopLevelKeyword(tokens []token, start int, keyword string) int {
	depth := 0
	for index := start; index < len(tokens); index++ {
		switch tokens[index].Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		case tokenIdentifier:
			if depth == 0 && tokens[index].IsKeyword(tokenIdentifier, keyword) {
				return index
			}
		default:
		}
	}
	return -1
}

// collectProjectionNames splits the SELECT projection tokens on top- level commas and
// resolves each item to a column name via projectionItemName.
//
// Takes projection ([]token) which is the slice of tokens between the SELECT keyword
// (exclusive) and the FROM keyword (exclusive).
//
// Returns []string which is the per-item column name list.
func collectProjectionNames(projection []token) []string {
	var names []string
	depth := 0
	itemStart := 0
	for index := range projection {
		switch projection[index].Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		case tokenComma:
			if depth == 0 {
				names = append(names, projectionItemName(projection[itemStart:index]))
				itemStart = index + 1
			}
		default:
		}
	}
	if itemStart < len(projection) {
		names = append(names, projectionItemName(projection[itemStart:]))
	}
	return names
}

// projectionItemName resolves a single SELECT projection item to its column name.
//
// The rules mirror SQL's own naming behaviour. An `expr AS alias` returns `alias`, an
// `expr alias` implicit alias returns `alias`, a `table.column` returns `column`, a bare
// identifier returns the identifier, and anything else returns the empty string.
//
// Takes item ([]token) which is the projection item's token slice.
//
// Returns string which is the inferred column name, or empty when no sensible name can be
// derived.
func projectionItemName(item []token) string {
	if len(item) == 0 {
		return ""
	}

	for index := len(item) - 2; index >= 0; index-- {
		if item[index].Kind != tokenIdentifier {
			continue
		}
		if !item[index].IsKeyword(tokenIdentifier, "AS") {
			continue
		}
		if item[index+1].Kind == tokenIdentifier {
			return item[index+1].Value
		}
	}

	if len(item) >= 2 {
		last := item[len(item)-1]
		secondLast := item[len(item)-2]
		if last.Kind == tokenIdentifier && secondLast.Kind == tokenIdentifier &&
			!isReservedProjectionKeyword(last.Keyword(tokenIdentifier)) && !isReservedProjectionKeyword(secondLast.Keyword(tokenIdentifier)) {
			return last.Value
		}
	}

	last := item[len(item)-1]
	if last.Kind == tokenIdentifier {
		return last.Value
	}
	return ""
}

// isReservedProjectionKeyword reports whether the identifier looks like a SQL keyword
// that should not be treated as a column alias.
//
// Takes value (string) which is the candidate identifier.
//
// Returns bool which is true for reserved keywords.
func isReservedProjectionKeyword(value string) bool {
	switch strings.ToUpper(value) {
	case "DISTINCT", "ALL", "AS", "DESC", "ASC", "NULL", "TRUE", "FALSE":
		return true
	}
	return false
}

// extractVirtualTableColumns derives a virtual table's columns from its module arguments.
//
// Takes tableName (string) which is the virtual table's name.
// Takes lowerModule (string) which is the lower-cased module name.
// Takes tokens ([]token) which are the argument tokens of the USING clause.
// Takes engine (*Dialect) which supplies type normalisation.
//
// Returns []sqlmodel.Column which is the module's column schema.
// Returns []string which lists the primary key columns, nil when the module has none.
func extractVirtualTableColumns(tableName, lowerModule string, tokens []token, engine *Dialect) ([]sqlmodel.Column, []string) {
	switch lowerModule {
	case moduleFTS5:
		return extractFTS5Columns(tableName, tokens, engine), nil
	case "rtree", "rtree_i32":
		return extractRTreeColumns(lowerModule, tokens, engine)
	default:
		return extractGenericVirtualColumns(tokens, engine), nil
	}
}

// extractFTS5Columns derives the searchable columns of an FTS5 virtual table from its
// argument list and appends FTS5's two hidden columns: `rank`, and the column named after
// the table that a full-text query matches against. SELECT * leaves both out.
//
// Takes tableName (string) which is the FTS5 table's name.
// Takes tokens ([]token) which are the argument tokens of the USING clause.
// Takes engine (*Dialect) which supplies type normalisation.
//
// Returns []sqlmodel.Column which is the FTS5 column schema including the hidden columns.
func extractFTS5Columns(tableName string, tokens []token, engine *Dialect) []sqlmodel.Column {
	segments := splitTokensOnComma(tokens)
	var columns []sqlmodel.Column

	for _, segment := range segments {
		if len(segment) == 0 {
			continue
		}

		if isFTS5Option(segment) {
			continue
		}

		columns = append(columns, sqlmodel.NewColumn(segment[0].Value, engine.NormaliseTypeName("text"), true))
	}

	rank := sqlmodel.NewColumn("rank", engine.NormaliseTypeName("real"), true)
	rank.IsGenerated = true
	rank.GeneratedKind = sqlmodel.GeneratedKindVirtual
	rank.Hidden = true
	columns = append(columns, rank)

	tableNamed := sqlmodel.NewColumn(tableName, engine.NormaliseTypeName("text"), true)
	tableNamed.Hidden = true
	columns = append(columns, tableNamed)

	return columns
}

// isFTS5Option reports whether a comma-delimited segment of an FTS5 argument list is a
// configuration option rather than a column.
//
// Takes segment ([]token) which is one comma-separated argument segment.
//
// Returns bool which is true for `name = value` pairs and well-known FTS5 option names.
func isFTS5Option(segment []token) bool {
	if len(segment) < 2 {
		return false
	}

	if segment[1].Kind == tokenOperator && segment[1].Value == "=" {
		return true
	}

	optionName := strings.ToLower(segment[0].Value)
	switch optionName {
	case "content", "content_rowid", "tokenize", "prefix", "detail", "columnsize":
		return true
	}

	return false
}

// extractRTreeColumns derives the column schema of an R*Tree virtual table.
//
// The first column is the 64-bit integer id, which is the table's rowid and so defaults
// when an INSERT leaves it out. The coordinate columns are 32-bit floats for rtree and
// 32-bit integers for rtree_i32; SQLite stores 0 for a coordinate an INSERT leaves out,
// so no id or coordinate is ever NULL. An auxiliary column, written with a leading +,
// keeps its declared type and may be NULL.
//
// Takes lowerModule (string) which is rtree or rtree_i32.
// Takes tokens ([]token) which are the argument tokens of the USING clause.
// Takes engine (*Dialect) which supplies type normalisation.
//
// Returns []sqlmodel.Column which is the R*Tree column schema.
// Returns []string which lists the primary key column names.
func extractRTreeColumns(lowerModule string, tokens []token, engine *Dialect) ([]sqlmodel.Column, []string) {
	segments := splitTokensOnComma(tokens)
	var columns []sqlmodel.Column
	var primaryKeyColumns []string

	coordinateType := engine.NormaliseTypeName("real")
	if lowerModule == "rtree_i32" {
		coordinateType = engine.NormaliseTypeName("int")
		coordinateType.Width = rtreeCoordinateWidth
	}

	for columnIndex, segment := range segments {
		if len(segment) == 0 {
			continue
		}

		if column, isAuxiliary := rtreeAuxiliaryColumn(segment, engine); isAuxiliary {
			columns = append(columns, column)
			continue
		}

		name := segment[0].Value
		if columnIndex == 0 {
			identifierType := engine.NormaliseTypeName(typeNameBigint)
			identifier := sqlmodel.NewColumn(name, identifierType, false)
			identifier.HasDefault = true
			columns = append(columns, identifier)
			primaryKeyColumns = append(primaryKeyColumns, name)
			continue
		}
		columns = append(columns, sqlmodel.NewColumn(name, coordinateType, false))
	}

	return columns, primaryKeyColumns
}

// rtreeAuxiliaryColumn reads an R*Tree auxiliary column, `+name [type]`, which stores a
// value alongside the coordinates and may be NULL.
//
// Takes segment ([]token) which is one comma-separated argument of the USING clause.
// Takes engine (*Dialect) which supplies type normalisation.
//
// Returns sqlmodel.Column which is the auxiliary column.
// Returns bool which is true when the segment is an auxiliary column.
func rtreeAuxiliaryColumn(segment []token, engine *Dialect) (sqlmodel.Column, bool) {
	if len(segment) < 2 || segment[0].Kind != tokenOperator || segment[0].Value != "+" ||
		segment[1].Kind != tokenIdentifier {
		return sqlmodel.Column{}, false
	}
	typeParts := make([]string, 0, len(segment)-2)
	for _, part := range segment[2:] {
		if part.Kind != tokenIdentifier {
			break
		}
		typeParts = append(typeParts, part.Value)
	}
	declaredType := "any"
	if len(typeParts) > 0 {
		declaredType = strings.Join(typeParts, " ")
	}
	return sqlmodel.NewColumn(segment[1].Value, engine.NormaliseTypeName(declaredType), true), true
}

// extractGenericVirtualColumns derives a best-effort text-typed column list for an
// unrecognised virtual table module.
//
// Takes tokens ([]token) which are the argument tokens of the USING clause.
// Takes engine (*Dialect) which supplies type normalisation.
//
// Returns []sqlmodel.Column which is the inferred column schema.
func extractGenericVirtualColumns(tokens []token, engine *Dialect) []sqlmodel.Column {
	segments := splitTokensOnComma(tokens)
	var columns []sqlmodel.Column

	for _, segment := range segments {
		if len(segment) == 0 {
			continue
		}

		if len(segment) >= 2 && segment[1].Kind == tokenOperator && segment[1].Value == "=" {
			continue
		}

		if segment[0].Kind != tokenIdentifier {
			continue
		}

		columns = append(columns, sqlmodel.NewColumn(segment[0].Value, engine.NormaliseTypeName("text"), true))
	}

	return columns
}

// splitTokensOnComma splits tokens on top-level commas, leaving commas inside parentheses
// alone.
//
// Takes tokens ([]token) which are the tokens to split.
//
// Returns [][]token which is one slice per top-level comma-separated segment.
func splitTokensOnComma(tokens []token) [][]token {
	var segments [][]token
	var current []token
	depth := 0

	for _, currentToken := range tokens {
		if currentToken.Kind == tokenLeftParen {
			depth++
		}
		if currentToken.Kind == tokenRightParen {
			depth--
		}
		if currentToken.Kind == tokenComma && depth == 0 {
			segments = append(segments, current)
			current = nil
			continue
		}
		current = append(current, currentToken)
	}
	if len(current) > 0 {
		segments = append(segments, current)
	}

	return segments
}
