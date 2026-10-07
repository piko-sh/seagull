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
	"errors"
	"fmt"
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// checkOptionTokenCount is the number of tokens in the WITH CHECK OPTION that may close
	// a view's query; WITH CASCADED CHECK OPTION and WITH LOCAL CHECK OPTION hold one more.
	checkOptionTokenCount = 3
)

// parseCreateIndex parses a CREATE [UNIQUE] INDEX statement: the index's name and type,
// the table, an index type written after it, the key parts, and the index, ALGORITHM and
// LOCK options after them.
//
// Returns *ast.CatalogueMutation which describes the new index.
// Returns error when the statement is malformed.
func (p *parser) parseCreateIndex() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}
	p.matchOrReplace()

	p.MatchKeyword(keywordUNIQUE)

	if _, err := p.ExpectKeyword("INDEX"); err != nil {
		return nil, err
	}

	p.matchIfNotExists()

	indexName, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return nil, nameError
	}

	if p.MatchKeyword(keywordUSING) {
		p.Advance()
	}

	if _, err := p.ExpectKeyword(keywordON); err != nil {
		return nil, err
	}

	schema, tableName, tableError := p.parseSchemaQualifiedName()
	if tableError != nil {
		return nil, tableError
	}

	p.skipIndexOptions()
	if _, err := p.parseMySQLColumnList(); err != nil {
		return nil, err
	}
	if err := p.skipIndexStatementOptions(); err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(
		ast.MutationCreateIndex,
		schema,
		tableName,
		ast.WithNewName(indexName),
	), nil
}

// parseDropIndex parses a DROP INDEX ... ON table statement and the ALGORITHM and LOCK
// options after it.
//
// Returns *ast.CatalogueMutation which describes the index to drop, marked IF EXISTS when
// the statement says so.
// Returns error when the statement is malformed, including when ON names no table.
func (p *parser) parseDropIndex() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence(keywordDROP, "INDEX"); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	indexName, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return nil, nameError
	}

	if _, err := p.ExpectKeyword(keywordON); err != nil {
		return nil, err
	}
	schema, tableName, tableError := p.parseSchemaQualifiedName()
	if tableError != nil {
		return nil, tableError
	}
	if err := p.skipOnlineDDLOptions(); err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(
		ast.MutationDropIndex,
		schema,
		tableName,
		ast.WithNewName(indexName),
		ast.WithIfExists(ifExists),
	), nil
}

// parseCreateView parses a CREATE [OR REPLACE] VIEW statement, whose query runs to the
// end of the statement after AS, and may close with WITH [CASCADED | LOCAL] CHECK OPTION.
//
// Returns *ast.CatalogueMutation which describes the new view.
// Returns error when the statement is malformed.
func (p *parser) parseCreateView() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}

	p.skipOrReplace()

	p.skipViewPrefixes()

	if _, err := p.ExpectKeyword("VIEW"); err != nil {
		return nil, err
	}

	ifNotExists := p.matchIfNotExists()

	schema, viewName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nil, nameError
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

	mutation := ast.NewCatalogueMutation(ast.MutationCreateView, schema, viewName, ast.WithIfNotExists(ifNotExists))
	viewDefinition, unanalysed, err := p.analyseViewBody(columnNames, "view "+viewName)
	if err != nil {
		return nil, err
	}
	mutation.ViewDefinition = viewDefinition
	mutation.UnanalysedParts = unanalysed
	mutation.Columns = dialectkit.ColumnsFromNames(columnNames)

	return mutation, nil
}

// analyseViewBody analyses the query of a CREATE VIEW, or of CREATE TABLE ... AS, which
// runs from the current token to the end of the statement, and consumes it.
//
// A trailing WITH [CASCADED | LOCAL] CHECK OPTION and a closing semicolon are excluded
// from the view's query. The query must consume all tokens before them.
//
// A malformed query fails the statement with its syntax error. A query Seagull cannot
// analyse, a TABLE statement or one nested past the parse depth, gives no analysis and is
// reported as an unanalysed part, so the object still exists with the columns its
// declaration names. A panic in the inner analyser, or an interrupt of its walk, is left
// to ApplyDDL, which fails the statement with it.
//
// Takes columnNames ([]string) which are any explicitly declared column names.
// Takes object (string) which names the object the query defines, such as "view v", for
// the unanalysed part's message.
//
// Returns *ast.RawQueryAnalysis which is the analysed query, or nil when it is not
// analysed.
// Returns []ast.Warning which holds the unanalysed part when the query is not analysed.
// Returns error when the query is missing or malformed.
func (p *parser) analyseViewBody(columnNames []string, object string) (*ast.RawQueryAnalysis, []ast.Warning, error) {
	end := len(p.Tokens)
	for end > p.Position && p.Tokens[end-1].Kind == tokenSemicolon {
		end--
	}
	bodyTokens := trimViewCheckOption(p.Tokens[p.Position:end])
	if len(bodyTokens) == 0 {
		return nil, nil, p.syntaxErrorAt("expected a query after AS")
	}
	p.Position = len(p.Tokens)
	unanalysed := func(message string) []ast.Warning {
		return []ast.Warning{{Message: message, Span: dialectkit.TokenRangeSpan(bodyTokens, 0, len(bodyTokens))}}
	}

	if !startsQuery(bodyTokens[0]) && classifyParenthesisedStatement(bodyTokens) != statementKindSelect {
		if opensTableStatement(bodyTokens) {
			return nil, unanalysed("the TABLE statement that defines " + object +
				" is not analysed, so its columns are not known; write SELECT * FROM the table instead"), nil
		}
		err := dialectkit.NewParseError(bodyTokens[0], "expected a query after AS")
		p.recordSyntaxError(err)
		return nil, nil, err
	}

	viewParser := p.newChildParser(bodyTokens)
	viewAnalysis, err := viewParser.analyseSelect()
	if err == nil {
		err = viewParser.requireEnd()
	}
	if viewParser.syntaxError != nil {
		err = viewParser.syntaxError
	}
	if errors.Is(err, dialectkit.ErrParseDepth) {
		return nil, unanalysed(fmt.Sprintf("the query that defines %s nests more deeply than the parse depth limit of %d, "+
			"so its columns are not known", object, p.maxParseDepth)), nil
	}
	if err == nil && viewAnalysis == nil {
		err = dialectkit.NewParseError(bodyTokens[0], "expected a query after AS")
	}
	if err != nil {
		p.recordSyntaxError(err)
		return nil, nil, err
	}
	if len(columnNames) > 0 && !slices.ContainsFunc(viewAnalysis.OutputColumns, dialectkit.IsStarColumn) {
		dialectkit.OverlayViewColumnNames(viewAnalysis, columnNames, dialectkit.UndeclaredColumnsDropped)
	}
	return viewAnalysis, nil, nil
}

// skipOrReplace consumes an optional OR REPLACE clause.
func (p *parser) skipOrReplace() {
	if p.MatchKeyword("OR") {
		p.MatchKeyword("REPLACE")
	}
}

// matchOrReplace consumes the OR REPLACE of a CREATE when the dialect lets a CREATE of a
// table, index, database or trigger say it, and leaves it for the statement to refuse
// otherwise.
//
// Returns bool which is true when OR REPLACE was consumed.
func (p *parser) matchOrReplace() bool {
	if !p.createOrReplace || !p.IsKeyword("OR") || !p.Peek().IsKeyword(tokenIdentifier, "REPLACE") {
		return false
	}
	p.Advance()
	p.Advance()
	return true
}

// skipViewPrefixes consumes ALGORITHM, DEFINER, and SQL SECURITY prefixes.
func (p *parser) skipViewPrefixes() {
	if p.MatchKeyword("ALGORITHM") {
		if p.Current().Kind == tokenOperator && p.Current().Value == "=" {
			p.Advance()
		}
		p.Advance()
	}

	if p.MatchKeyword("DEFINER") {
		p.skipDefinerClause()
	}

	if p.MatchKeyword("SQL") {
		p.MatchKeyword("SECURITY")
		p.Advance()
	}
}

// skipDefinerClause consumes a DEFINER = user clause on a CREATE statement, such as
// DEFINER = `root`@`localhost`, 'app'@'%' or CURRENT_USER(), up to the next modifier or
// the object keyword.
func (p *parser) skipDefinerClause() {
	if p.Current().Kind == tokenOperator && p.Current().Value == "=" {
		p.Advance()
	}
	for !p.AtEnd() && p.Current().Kind != tokenEOF && p.Current().Kind != tokenSemicolon {
		upper := p.Current().Keyword(tokenIdentifier)
		if _, isObject := createObjectKinds[upper]; isObject || upper == keywordSQL || upper == "ALGORITHM" || upper == "EVENT" {
			return
		}
		p.Advance()
	}
}

// parseDropView parses a DROP VIEW statement, which may name several views, and the
// RESTRICT or CASCADE after them.
//
// Returns *ast.CatalogueMutation which drops the first view, carrying a drop of each
// other view as an additional mutation, each marked IF EXISTS when the statement says so.
// Returns error when the statement is malformed.
func (p *parser) parseDropView() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence(keywordDROP, "VIEW"); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	drops, err := p.parseDroppedNames(ast.MutationDropView, ifExists)
	if err != nil {
		return nil, err
	}
	if !p.MatchKeyword(keywordRESTRICT) {
		p.MatchKeyword(keywordCASCADE)
	}
	return collectAlterTableActions(drops), nil
}

// parseDroppedNames parses the comma-separated names a DROP TABLE or DROP VIEW statement
// drops, giving a drop of each.
//
// Takes kind (ast.MutationKind) which is the drop each name makes.
// Takes ifExists (bool) which is true when the statement says IF EXISTS.
//
// Returns []*ast.CatalogueMutation which holds one drop per name, in order.
// Returns error when a name is malformed.
func (p *parser) parseDroppedNames(kind ast.MutationKind, ifExists bool) ([]*ast.CatalogueMutation, error) {
	var drops []*ast.CatalogueMutation
	for {
		schema, name, nameError := p.parseSchemaQualifiedName()
		if nameError != nil {
			return nil, nameError
		}
		drops = append(drops, ast.NewCatalogueMutation(kind, schema, name, ast.WithIfExists(ifExists)))
		if p.Current().Kind != tokenComma {
			return drops, nil
		}
		p.Advance()
	}
}

// parseCreateOrDropTrigger parses a CREATE or DROP TRIGGER statement.
//
// Takes kind (statementKind) which selects between the CREATE and DROP trigger entry
// points.
//
// Returns *ast.CatalogueMutation which describes the trigger mutation.
// Returns error when the statement is malformed.
func (p *parser) parseCreateOrDropTrigger(kind statementKind) (*ast.CatalogueMutation, error) {
	if kind == statementKindCreateTrigger {
		return p.parseCreateTrigger()
	}
	return p.parseDropTrigger()
}

// parseCreateTrigger parses a CREATE TRIGGER statement up to the table it is on. The
// trigger's body, which runs to the end of the statement, is not read.
//
// Returns *ast.CatalogueMutation which describes the new trigger.
// Returns error when the statement is malformed, including when ON names no table.
func (p *parser) parseCreateTrigger() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}
	p.matchOrReplace()

	if p.MatchKeyword("DEFINER") {
		p.skipDefinerClause()
	}

	if _, err := p.ExpectKeyword("TRIGGER"); err != nil {
		return nil, err
	}

	p.matchIfNotExists()

	triggerName, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return nil, nameError
	}

	for !p.AtEnd() && !p.IsKeyword(keywordON) {
		p.Advance()
	}

	if _, err := p.ExpectKeyword(keywordON); err != nil {
		return nil, err
	}
	_, tableName, tableError := p.parseSchemaQualifiedName()
	if tableError != nil {
		return nil, tableError
	}

	p.Position = len(p.Tokens)

	return ast.NewCatalogueMutation(
		ast.MutationCreateTrigger,
		"",
		tableName,
		ast.WithTriggerName(triggerName),
	), nil
}

// parseDropTrigger parses the body of a DROP TRIGGER statement.
//
// Returns *ast.CatalogueMutation which describes the trigger to drop, marked IF EXISTS
// when the statement says so.
// Returns error when the statement is malformed.
func (p *parser) parseDropTrigger() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence(keywordDROP, "TRIGGER"); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	_, triggerName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nil, nameError
	}

	return ast.NewCatalogueMutation(
		ast.MutationDropTrigger,
		"",
		"",
		ast.WithTriggerName(triggerName),
		ast.WithIfExists(ifExists),
	), nil
}

// parseCreateDatabase parses a CREATE DATABASE/SCHEMA statement.
//
// Returns *ast.CatalogueMutation which describes the new schema.
// Returns error when the statement is malformed.
func (p *parser) parseCreateDatabase() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}
	replace := p.matchOrReplace()
	if _, expectError := p.ExpectKeyword(keywordDATABASE, keywordSCHEMA); expectError != nil {
		return nil, expectError
	}

	p.matchIfNotExists()

	databaseName, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return nil, nameError
	}

	if err := p.parseDatabaseOptions(); err != nil {
		return nil, err
	}

	created := ast.NewCatalogueMutation(ast.MutationCreateSchema, databaseName, "")
	if replace {
		return replacing(ast.NewCatalogueMutation(ast.MutationDropSchema, databaseName, ""), created), nil
	}
	return created, nil
}

// parseDropDatabase parses a DROP DATABASE/SCHEMA statement.
//
// Returns *ast.CatalogueMutation which describes the schema to drop, marked IF EXISTS
// when the statement says so.
// Returns error when the statement is malformed.
func (p *parser) parseDropDatabase() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordDROP); err != nil {
		return nil, err
	}
	if _, expectError := p.ExpectKeyword(keywordDATABASE, keywordSCHEMA); expectError != nil {
		return nil, expectError
	}

	ifExists := p.matchIfExists()

	databaseName, nameError := p.parseIdentifierOrKeyword()
	if nameError != nil {
		return nil, nameError
	}

	return ast.NewCatalogueMutation(ast.MutationDropSchema, databaseName, "", ast.WithIfExists(ifExists)), nil
}

// parseDatabaseOptions consumes the options of CREATE DATABASE: CHARACTER SET or CHARSET,
// COLLATE, ENCRYPTION and MariaDB's COMMENT, each written with or without DEFAULT before
// it and an '=' before its value.
//
// Returns error when an option names no value.
func (p *parser) parseDatabaseOptions() error {
	for {
		p.MatchKeyword(keywordDEFAULT)
		switch {
		case p.isCharacterSetStart(), p.IsKeyword(keywordCOLLATE):
			if err := p.parseCharacterSetOrCollation(); err != nil {
				return err
			}
		case p.MatchKeyword("ENCRYPTION"), p.MatchKeyword(keywordCOMMENT):
			p.matchOperator(operatorEquals)
			if err := p.expectStringLiteral("a database option"); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

// skipIndexStatementOptions consumes the options that may follow the key parts of CREATE
// INDEX: the index options skipIndexOptions reads, and the ALGORITHM, LOCK, WAIT and
// NOWAIT options skipOnlineDDLOptions reads, in any order.
//
// Returns error when an option names no value.
func (p *parser) skipIndexStatementOptions() error {
	for {
		start := p.Position
		p.skipIndexOptions()
		if err := p.skipOnlineDDLOptions(); err != nil {
			return err
		}
		if p.Position == start {
			return nil
		}
	}
}

// skipOnlineDDLOptions consumes the ALGORITHM and LOCK options of an index statement,
// each written with or without an '=', and MariaDB's WAIT n and NOWAIT.
//
// Returns error when an option names no value.
func (p *parser) skipOnlineDDLOptions() error {
	for {
		switch {
		case p.MatchKeyword("ALGORITHM"), p.MatchKeyword("LOCK"):
			p.matchOperator(operatorEquals)
			if err := p.expectOptionValue(); err != nil {
				return err
			}
		case p.MatchKeyword("WAIT"):
			if err := p.expectOptionValue(); err != nil {
				return err
			}
		case p.MatchKeyword("NOWAIT"):
		default:
			return nil
		}
	}
}

// replacing returns the mutations a CREATE OR REPLACE makes: the drop of the object the
// statement names, then the creation and anything it carries.
//
// Takes drop (*ast.CatalogueMutation) which drops the object of the same name.
// Takes created (*ast.CatalogueMutation) which creates the new one.
//
// Returns *ast.CatalogueMutation which is the drop, carrying the creation.
func replacing(drop, created *ast.CatalogueMutation) *ast.CatalogueMutation {
	drop.IfExists = true
	drop.AdditionalMutations = append([]*ast.CatalogueMutation{created}, created.AdditionalMutations...)
	created.AdditionalMutations = nil
	return drop
}

// trimViewCheckOption returns a view's query without the WITH [CASCADED | LOCAL] CHECK
// OPTION that may close it.
//
// Takes tokens ([]token) which are the tokens after AS, to the end of the statement.
//
// Returns []token which are the query's tokens.
func trimViewCheckOption(tokens []token) []token {
	clause := len(tokens) - checkOptionTokenCount
	if clause < 0 || !tokens[clause+1].IsKeyword(tokenIdentifier, keywordCHECK) ||
		!tokens[clause+2].IsKeyword(tokenIdentifier, "OPTION") {
		return tokens
	}
	if tokens[clause].IsKeyword(tokenIdentifier, keywordWITH) {
		return tokens[:clause]
	}
	if clause > 0 && (tokens[clause].IsKeyword(tokenIdentifier, "CASCADED") || tokens[clause].IsKeyword(tokenIdentifier, "LOCAL")) &&
		tokens[clause-1].IsKeyword(tokenIdentifier, keywordWITH) {
		return tokens[:clause-1]
	}
	return tokens
}

// opensTableStatement reports whether tokens open a TABLE statement, written alone or in
// parentheses, which Seagull does not analyse.
//
// Takes tokens ([]token) which are the query's tokens.
//
// Returns bool which is true for a TABLE statement.
func opensTableStatement(tokens []token) bool {
	for _, current := range tokens {
		if current.Kind != tokenLeftParen {
			return current.IsKeyword(tokenIdentifier, "TABLE")
		}
	}
	return false
}
